/*
===========================================================================

variables.go - the Nomad variables a deployment owns

Secrets reach the jobs only as Nomad variables (the Agent's signing ring,
provisioning token, account catalog chunks and, when configured, the bug
report webhook; each GameWorld's public verifier ring). Writes use
check-and-set so two deployers never silently overwrite each other.

===========================================================================
*/
package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	nomad "github.com/hashicorp/nomad/api"
	"opensro.online/server/internal/agent/bugreport"
)

const (
	agentVariablePath        = "nomad/jobs/sro-agent/agent/agent"
	accountChunkPathPrefix   = agentVariablePath + "/accounts/"
	accountChunkIndexDigits  = 6
	gameWorldVariablePrefix  = "nomad/jobs/"
	gameWorldVariableSuffix  = "/gameworld/gameworld"
	accountChunkVariableItem = "payload"
	bugReportWebhookItem     = "bug_report_discord_webhook"
	nomadVariablePathBytes   = 128
	nomadVariableItemsBytes  = 64 << 10
)

/*
================
desiredVariable
================
*/
type desiredVariable struct {
	jobID string
	path  string
	items nomad.VariableItems
}

/*
================
variablePlan
================
*/
type variablePlan struct {
	desired            []desiredVariable
	current            map[string]*nomad.Variable
	staleAccountChunks []*nomad.VariableMetadata
}

/*
================
putVariables
================
*/
func (deployment *deployment) putVariables(
	ctx context.Context,
	client *nomadClient,
) error {
	plan, err := deployment.planVariables(ctx, client)
	if err != nil {
		return err
	}
	for _, variable := range plan.desired {
		existing := plan.current[variable.path]
		if existing != nil &&
			stringMapsEqual(existing.Items, variable.items) {
			fmt.Printf("Nomad variable %s is unchanged\n", variable.path)
			continue
		}
		value := nomad.NewVariable(variable.path)
		value.Namespace = client.namespace
		value.Items = variable.items
		if existing == nil {
			_, _, err := client.api.Variables().CheckedCreate(
				value,
				(&nomad.WriteOptions{}).WithContext(ctx),
			)
			if err != nil {
				return fmt.Errorf(
					"create Nomad variable %s with CAS: %w",
					variable.path,
					err,
				)
			}
			fmt.Printf("Created Nomad variable %s\n", variable.path)
			continue
		}
		value.ModifyIndex = existing.ModifyIndex
		_, _, err := client.api.Variables().CheckedUpdate(
			value,
			(&nomad.WriteOptions{}).WithContext(ctx),
		)
		if err != nil {
			return fmt.Errorf(
				"update Nomad variable %s with CAS: %w",
				variable.path,
				err,
			)
		}
		fmt.Printf("Updated Nomad variable %s\n", variable.path)
	}
	for _, variable := range plan.staleAccountChunks {
		_, err := client.api.Variables().CheckedDelete(
			variable.Path,
			variable.ModifyIndex,
			(&nomad.WriteOptions{}).WithContext(ctx),
		)
		if err != nil {
			return fmt.Errorf(
				"delete stale account variable %s with CAS: %w",
				variable.Path,
				err,
			)
		}
		fmt.Printf("Deleted stale account variable %s\n", variable.Path)
	}
	return nil
}

/*
================
validateVariables
================
*/
func (deployment *deployment) validateVariables(
	ctx context.Context,
	client *nomadClient,
) error {
	plan, err := deployment.planVariables(ctx, client)
	if err != nil {
		return err
	}
	fmt.Printf(
		"Validated %d Nomad Variable input(s) without mutation\n",
		len(plan.desired),
	)
	return nil
}

/*
================
planVariables
================
*/
func (deployment *deployment) planVariables(
	ctx context.Context,
	client *nomadClient,
) (variablePlan, error) {
	desired, err := deployment.desiredVariables()
	if err != nil {
		return variablePlan{}, err
	}
	if err := validateDesiredVariables(desired); err != nil {
		return variablePlan{}, err
	}
	current := make(map[string]*nomad.Variable, len(desired))
	changedJobs := make(map[string]string)
	desiredPaths := make(map[string]struct{}, len(desired))

	for index, variable := range desired {
		desiredPaths[variable.path] = struct{}{}
		existing, _, err := client.api.Variables().Peek(
			variable.path,
			(&nomad.QueryOptions{}).WithContext(ctx),
		)
		if err != nil {
			return variablePlan{}, fmt.Errorf(
				"read Nomad variable %s: %w",
				variable.path,
				err,
			)
		}
		if variable.path == agentVariablePath {
			variable.items = keepBugReportWebhook(variable.items, existing, deployment.BugReports.Off)
			desired[index] = variable
		}
		current[variable.path] = existing
		if existing == nil ||
			!stringMapsEqual(existing.Items, variable.items) && !onlyBugReportWebhookDiffers(existing.Items, variable.items) {
			changedJobs[variable.jobID] = variable.path
		}
	}

	staleAccountChunks, err := client.staleAccountChunkVariables(
		ctx,
		desiredPaths,
	)
	if err != nil {
		return variablePlan{}, err
	}
	if len(staleAccountChunks) != 0 {
		changedJobs[agentJobName] = staleAccountChunks[0].Path
	}
	for jobID, changedPath := range changedJobs {
		live, err := client.jobExists(ctx, jobID)
		if err != nil {
			return variablePlan{}, err
		}
		if live {
			return variablePlan{}, fmt.Errorf(
				"nomad variable %s differs while job %s is registered; "+
					"run `sro-nomad stop`, replace the operator-owned "+
					"credential input, then run `sro-nomad deploy`",
				changedPath,
				jobID,
			)
		}
	}

	return variablePlan{
		desired:            desired,
		current:            current,
		staleAccountChunks: staleAccountChunks,
	}, nil
}

/*
================
keepBugReportWebhook

The bug report webhook is set once by an operator and then lives only in
the Agent's credential variable. A deploy that names none (every release:
deploy.py runs with a clean environment) keeps the stored one; before, it
rewrote the variable without it, which turned bug reports off on every
release (or refused the release while the Agent ran). Only an explicit
SRO_BUG_REPORT_DISCORD_WEBHOOK=off removes it.
================
*/
func keepBugReportWebhook(items nomad.VariableItems, existing *nomad.Variable, off bool) nomad.VariableItems {
	if off {
		delete(items, bugReportWebhookItem)
		return items
	}
	if _, named := items[bugReportWebhookItem]; named || existing == nil {
		return items
	}
	if stored := existing.Items[bugReportWebhookItem]; bugreport.ValidWebhookURL(stored) {
		items[bugReportWebhookItem] = stored
	}
	return items
}

/*
================
onlyBugReportWebhookDiffers

Whether the two Agent credential sets differ in the bug report webhook
alone. That item reaches the Agent through its own template (change_mode
restart), so Nomad restarts the Agent with it: unlike the session keyring
or the account catalog it needs no stop, and a release may set, change or
remove it while the Agent runs.
================
*/
func onlyBugReportWebhookDiffers(current, desired nomad.VariableItems) bool {
	strip := func(items nomad.VariableItems) map[string]string {
		out := make(map[string]string, len(items))
		for name, value := range items {
			if name != bugReportWebhookItem {
				out[name] = value
			}
		}
		return out
	}
	return stringMapsEqual(strip(current), strip(desired))
}

/*
================
validateDesiredVariables
================
*/
func validateDesiredVariables(variables []desiredVariable) error {
	seen := make(map[string]struct{}, len(variables))
	for _, variable := range variables {
		if err := validateNomadVariablePath(variable.path); err != nil {
			return fmt.Errorf(
				"nomad variable for job %s: %w",
				variable.jobID,
				err,
			)
		}
		if _, exists := seen[variable.path]; exists {
			return fmt.Errorf(
				"nomad variable path %q is generated more than once",
				variable.path,
			)
		}
		seen[variable.path] = struct{}{}

		itemBytes := 0
		for key, value := range variable.items {
			itemBytes += len(key) + len(value)
		}
		if itemBytes > nomadVariableItemsBytes {
			return fmt.Errorf(
				"nomad variable %s items total %d bytes; limit is %d",
				variable.path,
				itemBytes,
				nomadVariableItemsBytes,
			)
		}
	}
	return nil
}

/*
================
validateNomadVariablePath
================
*/
func validateNomadVariablePath(path string) error {
	if len(path) < 1 || len(path) > nomadVariablePathBytes {
		return fmt.Errorf(
			"path %q is %d bytes; limit is 1..%d",
			path,
			len(path),
			nomadVariablePathBytes,
		)
	}
	for _, character := range []byte(path) {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '-' ||
			character == '_' ||
			character == '~' ||
			character == '/' {
			continue
		}
		return fmt.Errorf(
			"path %q contains non-Nomad character %q",
			path,
			character,
		)
	}
	return nil
}

/*
================
desiredVariables
================
*/
func (deployment *deployment) desiredVariables() ([]desiredVariable, error) {
	agentItems := nomad.VariableItems{
		"agent_session_keyring":    deployment.Secrets.SessionPrivate,
		"agent_provisioning_token": deployment.Secrets.ProvisioningToken,
	}
	// Absent, not empty, when bug reports are off: the job template renders
	// SRO_BUG_REPORT_DISCORD_WEBHOOK only when the item exists.
	if webhook := deployment.BugReports.WebhookURL; webhook != "" {
		agentItems[bugReportWebhookItem] = webhook
	}
	desired := []desiredVariable{{
		jobID: agentJobName,
		path:  agentVariablePath,
		items: agentItems,
	}}
	for index, chunk := range deployment.Secrets.AccountChunks {
		desired = append(desired, desiredVariable{
			jobID: agentJobName,
			path:  accountChunkVariablePath(index),
			items: nomad.VariableItems{
				accountChunkVariableItem: chunk,
			},
		})
	}
	for _, game := range deployment.Shards {
		jobID := gameWorldJobPrefix + game.Definition.ID
		desired = append(desired, desiredVariable{
			jobID: jobID,
			path: gameWorldVariablePrefix + jobID +
				gameWorldVariableSuffix,
			items: nomad.VariableItems{
				"agent_session_public_keys": deployment.Secrets.SessionPublic,
			},
		})
	}
	return desired, nil
}

/*
================
accountChunkVariablePath
================
*/
func accountChunkVariablePath(index int) string {
	return accountChunkPathPrefix + fmt.Sprintf(
		"%0*d",
		accountChunkIndexDigits,
		index,
	)
}

/*
================
staleAccountChunkVariables
================
*/
func (client *nomadClient) staleAccountChunkVariables(
	ctx context.Context,
	desiredPaths map[string]struct{},
) ([]*nomad.VariableMetadata, error) {
	variables, _, err := client.api.Variables().PrefixList(
		accountChunkPathPrefix,
		(&nomad.QueryOptions{}).WithContext(ctx),
	)
	if err != nil {
		return nil, fmt.Errorf("list Agent account variables: %w", err)
	}
	stale := make([]*nomad.VariableMetadata, 0)
	for _, variable := range variables {
		if !managedAccountChunkPath(variable.Path) {
			continue
		}
		if _, keep := desiredPaths[variable.Path]; !keep {
			stale = append(stale, variable)
		}
	}
	return stale, nil
}

/*
================
managedAccountChunkPath
================
*/
func managedAccountChunkPath(path string) bool {
	index := strings.TrimPrefix(path, accountChunkPathPrefix)
	if index == path || len(index) != accountChunkIndexDigits {
		return false
	}
	_, err := strconv.ParseUint(index, 10, 32)
	return err == nil
}

/*
================
stringMapsEqual
================
*/
func stringMapsEqual(
	left map[string]string,
	right map[string]string,
) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

/*
================
deleteDisabledGameWorldVariables
================
*/
func (client *nomadClient) deleteDisabledGameWorldVariables(
	ctx context.Context,
	enabled map[string]struct{},
) error {
	variables, _, err := client.api.Variables().PrefixList(
		gameWorldVariablePrefix+gameWorldJobPrefix,
		(&nomad.QueryOptions{}).WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("list managed GameWorld variables: %w", err)
	}
	for _, variable := range variables {
		jobID, managed := managedGameWorldVariableJobID(variable.Path)
		if !managed {
			continue
		}
		if _, keep := enabled[jobID]; keep {
			continue
		}
		_, err := client.api.Variables().CheckedDelete(
			variable.Path,
			variable.ModifyIndex,
			(&nomad.WriteOptions{}).WithContext(ctx),
		)
		if err != nil {
			return fmt.Errorf(
				"delete disabled GameWorld variable %s with CAS: %w",
				variable.Path,
				err,
			)
		}
		fmt.Printf(
			"Deleted disabled GameWorld variable %s\n",
			variable.Path,
		)
	}
	return nil
}

/*
================
managedGameWorldVariableJobID
================
*/
func managedGameWorldVariableJobID(path string) (string, bool) {
	if !strings.HasPrefix(path, gameWorldVariablePrefix) ||
		!strings.HasSuffix(path, gameWorldVariableSuffix) {
		return "", false
	}
	jobID := strings.TrimSuffix(
		strings.TrimPrefix(path, gameWorldVariablePrefix),
		gameWorldVariableSuffix,
	)
	if !strings.HasPrefix(jobID, gameWorldJobPrefix) ||
		path != gameWorldVariablePrefix+jobID+gameWorldVariableSuffix {
		return "", false
	}
	return jobID, true
}
