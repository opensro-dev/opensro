package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	nomad "github.com/hashicorp/nomad/api"
	"golang.org/x/sync/errgroup"
)

const (
	gameDeploymentParallelism = 4
	reconciliationMargin      = time.Minute
	stopTimeout               = 90 * time.Second
	fleetLockTTL              = 2 * time.Minute
	fleetLockDelay            = 15 * time.Second
	fleetLockPath             = "sro/operations/fleet"
	defaultNomadNamespace     = "default"
)

type nomadClient struct {
	api       *nomad.Client
	namespace string
}

func newNomadClient(namespace string, stack devStack) (*nomadClient, error) {
	config := nomad.DefaultConfig()
	// NOMAD_ADDR names any cluster; without it the client talks to this
	// checkout's development agent, wherever SRO_DEV_PORT_OFFSET put it.
	if strings.TrimSpace(os.Getenv("NOMAD_ADDR")) == "" {
		config.Address = stack.nomadAddress()
	}
	config.Namespace = namespace
	client, err := nomad.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("configure Nomad API client: %w", err)
	}
	return &nomadClient{
		api:       client,
		namespace: namespace,
	}, nil
}

func (client *nomadClient) close() {
	client.api.Close()
}

func (client *nomadClient) withFleetLock(
	ctx context.Context,
	purpose string,
	protected func(context.Context) error,
) error {
	lock, err := client.api.Locks(
		*(&nomad.WriteOptions{}).WithContext(ctx),
		nomad.Variable{
			Namespace: client.namespace,
			Path:      fleetLockPath,
			Items: nomad.VariableItems{
				"purpose": purpose,
			},
			Lock: &nomad.VariableLock{
				TTL:       fleetLockTTL.String(),
				LockDelay: fleetLockDelay.String(),
			},
		},
	)
	if err != nil {
		return fmt.Errorf("configure Nomad fleet lock: %w", err)
	}
	leaser := client.api.NewLockLeaser(
		lock,
		nomad.LockLeaserOptionWithEarlyReturn(true),
	)
	var acquired atomic.Bool
	err = leaser.Start(ctx, func(lockContext context.Context) error {
		acquired.Store(true)
		return protected(lockContext)
	})
	if err != nil {
		return fmt.Errorf("nomad fleet lock: %w", err)
	}
	if acquired.Load() {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf(
		"another fleet operation holds Nomad variable lock %s",
		fleetLockPath,
	)
}

func (deployment *deployment) validateJobs(
	ctx context.Context,
	client *nomadClient,
) error {
	jobs, err := deployment.parseJobs(ctx, client)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err := client.validateAndPlan(ctx, job); err != nil {
			return err
		}
	}
	return nil
}

func (deployment *deployment) parseJobs(
	ctx context.Context,
	client *nomadClient,
) ([]*nomad.Job, error) {
	agentTemplate, gameTemplate, err := deployment.jobTemplates()
	if err != nil {
		return nil, err
	}
	agentTemplate, err = renderAgentTemplate(
		agentTemplate,
		len(deployment.Secrets.AccountChunks),
	)
	if err != nil {
		return nil, err
	}
	agent, err := client.parseJob(
		ctx,
		agentTemplate,
		deployment.agentVariables(),
	)
	if err != nil {
		return nil, fmt.Errorf("parse Agent job: %w", err)
	}
	jobs := []*nomad.Job{agent}
	for _, game := range deployment.Shards {
		rendered := renderGameWorldTemplate(
			gameTemplate,
			game.Definition.ID,
		)
		job, err := client.parseJob(
			ctx,
			rendered,
			deployment.gameVariables(game),
		)
		if err != nil {
			return nil, fmt.Errorf(
				"parse GameWorld %q job: %w",
				game.Definition.ID,
				err,
			)
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (client *nomadClient) parseJob(
	ctx context.Context,
	template []byte,
	variables map[string]any,
) (*nomad.Job, error) {
	encodedVariables, err := jobVariablesJSON(variables)
	if err != nil {
		return nil, err
	}
	job, err := client.api.Jobs().ParseHCLOpts(&nomad.JobsParseRequest{
		JobHCL:       string(template),
		Variables:    encodedVariables,
		Canonicalize: true,
	})
	if err != nil {
		return nil, err
	}
	if job.ID == nil || strings.TrimSpace(*job.ID) == "" {
		return nil, fmt.Errorf("parsed job has no ID")
	}
	return job, nil
}

func jobVariablesJSON(values map[string]any) (string, error) {
	payload, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("encode Nomad job variables: %w", err)
	}
	return string(payload), nil
}

func (client *nomadClient) validateAndPlan(
	ctx context.Context,
	job *nomad.Job,
) error {
	write := (&nomad.WriteOptions{}).WithContext(ctx)
	validated, _, err := client.api.Jobs().Validate(job, write)
	if err != nil {
		return fmt.Errorf("validate Nomad job %s: %w", *job.ID, err)
	}
	if validated.Error != "" || len(validated.ValidationErrors) != 0 {
		return fmt.Errorf(
			"validate Nomad job %s: %s %s",
			*job.ID,
			validated.Error,
			strings.Join(validated.ValidationErrors, "; "),
		)
	}
	plan, _, err := client.api.Jobs().Plan(job, true, write)
	if err != nil {
		return fmt.Errorf("plan Nomad job %s: %w", *job.ID, err)
	}
	if len(plan.FailedTGAllocs) != 0 {
		groups := make([]string, 0, len(plan.FailedTGAllocs))
		for group := range plan.FailedTGAllocs {
			groups = append(groups, group)
		}
		sort.Strings(groups)
		return fmt.Errorf(
			"plan Nomad job %s cannot place task groups: %s",
			*job.ID,
			strings.Join(groups, ", "),
		)
	}
	if strings.TrimSpace(plan.Warnings) != "" {
		fmt.Printf("Nomad plan warning for %s: %s\n", *job.ID, plan.Warnings)
	}
	return nil
}

func (deployment *deployment) deploy(
	ctx context.Context,
	client *nomadClient,
) error {
	jobs, err := deployment.parseJobs(ctx, client)
	if err != nil {
		return err
	}
	if err := client.reconcileDisabledShards(ctx, deployment); err != nil {
		return err
	}
	if len(jobs) == 0 {
		return fmt.Errorf("deployment has no Nomad jobs")
	}
	if err := client.registerJob(
		ctx,
		jobs[0],
	); err != nil {
		return err
	}

	// Shards are independent authority boundaries. Reconcile them in a
	// bounded group so one unhealthy world does not prevent later worlds
	// from being attempted. Do not cancel siblings on the first error:
	// Nomad owns each job's rollback and retained history independently.
	var worlds errgroup.Group
	worlds.SetLimit(gameDeploymentParallelism)
	for _, job := range jobs[1:] {
		worlds.Go(func() error {
			return client.registerJob(
				ctx,
				job,
			)
		})
	}
	return worlds.Wait()
}

func (client *nomadClient) pruneReleases(
	ctx context.Context,
	deployment *deployment,
) error {
	retained := map[string]map[string]struct{}{
		"agent": {
			deployment.AgentReleaseID: {},
		},
		"gameworld": {
			deployment.GameReleaseID: {},
		},
	}
	jobRoles := map[string]string{
		agentJobName: "agent",
	}
	for _, game := range deployment.Shards {
		jobRoles[gameWorldJobPrefix+game.Definition.ID] = "gameworld"
	}
	for jobID, role := range jobRoles {
		versions, _, _, err := client.api.Jobs().Versions(
			jobID,
			false,
			(&nomad.QueryOptions{}).WithContext(ctx),
		)
		if err != nil {
			return fmt.Errorf(
				"read Nomad job history for %s before release pruning: %w",
				jobID,
				err,
			)
		}
		for releaseID := range jobReleaseIDs(versions) {
			retained[role][releaseID] = struct{}{}
		}
	}
	return deployment.pruneReleases(retained)
}

func jobReleaseIDs(versions []*nomad.Job) map[string]struct{} {
	releases := make(map[string]struct{})
	for _, job := range versions {
		for _, group := range job.TaskGroups {
			for _, task := range group.Tasks {
				if releaseID := strings.TrimSpace(
					task.Env["SRO_RELEASE_ID"],
				); releaseID != "" {
					releases[releaseID] = struct{}{}
				}
			}
		}
	}
	return releases
}

func (client *nomadClient) registerJob(
	parent context.Context,
	job *nomad.Job,
) error {
	timeout, err := reconciliationTimeout(job)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	fmt.Printf(
		"Reconciling Nomad job %s (whole-operation deadline %s)\n",
		*job.ID,
		timeout,
	)
	write := (&nomad.WriteOptions{}).WithContext(ctx)
	plan, _, err := client.api.Jobs().Plan(job, true, write)
	if err != nil {
		return fmt.Errorf("plan Nomad job %s: %w", *job.ID, err)
	}
	if len(plan.FailedTGAllocs) != 0 {
		return fmt.Errorf(
			"plan Nomad job %s has %d unplaceable task group(s)",
			*job.ID,
			len(plan.FailedTGAllocs),
		)
	}
	var previousVersion *uint64
	existing, _, err := client.api.Jobs().Info(
		*job.ID,
		(&nomad.QueryOptions{}).WithContext(ctx),
	)
	if err == nil {
		previousVersion = existing.Version
	} else if !isNotFound(err) {
		return fmt.Errorf("inspect Nomad job %s: %w", *job.ID, err)
	}
	response, _, err := client.api.Jobs().EnforceRegister(
		job,
		plan.JobModifyIndex,
		write,
	)
	if err != nil {
		return fmt.Errorf(
			"register Nomad job %s with plan index %d: %w",
			*job.ID,
			plan.JobModifyIndex,
			err,
		)
	}
	if strings.TrimSpace(response.Warnings) != "" {
		fmt.Printf(
			"Nomad registration warning for %s: %s\n",
			*job.ID,
			response.Warnings,
		)
	}
	registered, _, err := client.api.Jobs().Info(
		*job.ID,
		(&nomad.QueryOptions{}).WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("inspect registered Nomad job %s: %w", *job.ID, err)
	}
	if registered.Version == nil {
		return fmt.Errorf("registered Nomad job %s has no version", *job.ID)
	}
	if previousVersion != nil && *previousVersion == *registered.Version {
		fmt.Printf(
			"Nomad job %s is unchanged at version %d\n",
			*job.ID,
			*registered.Version,
		)
		return client.waitForJobAllocations(
			ctx,
			registered,
			*registered.Version,
		)
	}
	fmt.Printf(
		"Waiting for Nomad job %s deployment version %d\n",
		*job.ID,
		*registered.Version,
	)
	if err := client.waitForDeployment(ctx, registered); err != nil {
		return err
	}
	fmt.Printf(
		"Nomad job %s reached healthy deployment version %d\n",
		*job.ID,
		*registered.Version,
	)
	return client.waitForJobAllocations(
		ctx,
		registered,
		*registered.Version,
	)
}

func (client *nomadClient) waitForDeployment(
	ctx context.Context,
	job *nomad.Job,
) error {
	if job == nil || job.ID == nil || job.Version == nil ||
		job.CreateIndex == nil {
		return fmt.Errorf("registered Nomad job identity is incomplete")
	}
	jobID := *job.ID
	version := *job.Version
	createIndex := *job.CreateIndex

	var waitIndex uint64
	var lastStatus string
	for {
		deployments, metadata, err := client.api.Jobs().Deployments(
			jobID,
			true,
			(&nomad.QueryOptions{
				WaitIndex: waitIndex,
				WaitTime:  15 * time.Second,
			}).WithContext(ctx),
		)
		if err != nil {
			if contextError := ctx.Err(); contextError != nil {
				if lastStatus == "" {
					lastStatus = "no deployment status observed"
				}
				return fmt.Errorf(
					"nomad job %s reconciliation ended while waiting "+
						"for deployment version %d (%s): %w",
					jobID,
					version,
					lastStatus,
					contextError,
				)
			}
			return fmt.Errorf(
				"wait for Nomad deployment %s version %d: %w",
				jobID,
				version,
				err,
			)
		}
		if metadata != nil && metadata.LastIndex > waitIndex {
			waitIndex = metadata.LastIndex
		}
		deployment, newerVersion := deploymentForJobVersion(
			deployments,
			createIndex,
			version,
		)
		if deployment == nil && newerVersion > version {
			return fmt.Errorf(
				"nomad job %s advanced to version %d while waiting "+
					"for deployment version %d",
				jobID,
				newerVersion,
				version,
			)
		}
		if deployment == nil {
			continue
		}
		switch deployment.Status {
		case nomad.DeploymentStatusSuccessful:
			return nil
		case nomad.DeploymentStatusFailed,
			nomad.DeploymentStatusCancelled:
			return fmt.Errorf(
				"nomad deployment %s version %d %s: %s",
				jobID,
				version,
				deployment.Status,
				deployment.StatusDescription,
			)
		}
		status := fmt.Sprintf(
			"%s: %s",
			deployment.Status,
			deployment.StatusDescription,
		)
		if status != lastStatus {
			fmt.Printf(
				"Nomad job %s deployment version %d: %s\n",
				jobID,
				version,
				status,
			)
			lastStatus = status
		}
	}
}

func deploymentForJobVersion(
	deployments []*nomad.Deployment,
	jobCreateIndex uint64,
	version uint64,
) (*nomad.Deployment, uint64) {
	var selected *nomad.Deployment
	var newerVersion uint64
	for _, deployment := range deployments {
		if deployment == nil ||
			deployment.JobCreateIndex != jobCreateIndex {
			continue
		}
		if deployment.JobVersion > newerVersion {
			newerVersion = deployment.JobVersion
		}
		if deployment.JobVersion == version &&
			(selected == nil ||
				deployment.CreateIndex > selected.CreateIndex) {
			selected = deployment
		}
	}
	return selected, newerVersion
}

func (client *nomadClient) waitForJobAllocations(
	ctx context.Context,
	job *nomad.Job,
	version uint64,
) error {
	expected := desiredAllocationCount(job)
	if expected < 1 {
		return fmt.Errorf("nomad job %s has no desired allocations", *job.ID)
	}
	var waitIndex uint64
	var lastStatus string
	for {
		allocations, metadata, err := client.api.Jobs().Allocations(
			*job.ID,
			false,
			(&nomad.QueryOptions{
				WaitIndex: waitIndex,
				WaitTime:  10 * time.Second,
			}).WithContext(ctx),
		)
		if err != nil {
			if contextError := ctx.Err(); contextError != nil {
				if lastStatus == "" {
					lastStatus = "no allocation status observed"
				}
				return fmt.Errorf(
					"nomad job %s reconciliation ended while waiting "+
						"for desired allocations (%s): %w",
					*job.ID,
					lastStatus,
					contextError,
				)
			}
			return fmt.Errorf(
				"wait for Nomad job %s allocations: %w",
				*job.ID,
				err,
			)
		}
		if metadata != nil && metadata.LastIndex > waitIndex {
			waitIndex = metadata.LastIndex
		}
		healthy := 0
		for _, allocation := range allocations {
			if allocation.JobVersion != version ||
				allocation.DesiredStatus != nomad.AllocDesiredStatusRun ||
				allocation.ClientStatus != nomad.AllocClientStatusRunning ||
				allocation.DeploymentStatus == nil ||
				allocation.DeploymentStatus.Healthy == nil ||
				!*allocation.DeploymentStatus.Healthy {
				continue
			}
			healthy++
		}
		status := fmt.Sprintf(
			"%d/%d healthy running allocation(s) at version %d",
			healthy,
			expected,
			version,
		)
		if status != lastStatus {
			fmt.Printf("Nomad job %s: %s\n", *job.ID, status)
			lastStatus = status
		}
		if healthy == expected {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf(
				"nomad job %s did not reach desired allocations: %s: %w",
				*job.ID,
				lastStatus,
				err,
			)
		}
	}
}

func reconciliationTimeout(job *nomad.Job) (time.Duration, error) {
	if job == nil || job.ID == nil || strings.TrimSpace(*job.ID) == "" {
		return 0, fmt.Errorf("nomad job identity is incomplete")
	}
	if job.Update == nil ||
		job.Update.ProgressDeadline == nil ||
		*job.Update.ProgressDeadline <= 0 {
		return 0, fmt.Errorf(
			"nomad job %s must define a positive progress_deadline",
			*job.ID,
		)
	}
	return *job.Update.ProgressDeadline + reconciliationMargin, nil
}

func desiredAllocationCount(job *nomad.Job) int {
	count := 0
	if job == nil {
		return count
	}
	for _, group := range job.TaskGroups {
		if group != nil && group.Count != nil {
			count += *group.Count
		}
	}
	return count
}

func (client *nomadClient) reconcileDisabledShards(
	ctx context.Context,
	deployment *deployment,
) error {
	enabled := make(map[string]struct{}, len(deployment.Shards))
	for _, game := range deployment.Shards {
		enabled[gameWorldJobPrefix+game.Definition.ID] = struct{}{}
	}
	jobs, _, err := client.api.Jobs().List(
		(&nomad.QueryOptions{
			Prefix: gameWorldJobPrefix,
		}).WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("list managed GameWorld jobs: %w", err)
	}
	for _, job := range jobs {
		if !strings.HasPrefix(job.ID, gameWorldJobPrefix) {
			continue
		}
		if _, keep := enabled[job.ID]; keep {
			continue
		}
		fmt.Printf("Stopping disabled GameWorld job %s\n", job.ID)
		if err := client.stopJob(ctx, job.ID); err != nil {
			return err
		}
	}
	return client.deleteDisabledGameWorldVariables(ctx, enabled)
}

func (deployment *deployment) stop(
	ctx context.Context,
	client *nomadClient,
) error {
	jobs, _, err := client.api.Jobs().List(
		(&nomad.QueryOptions{
			Prefix: gameWorldJobPrefix,
		}).WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("list managed GameWorld jobs: %w", err)
	}
	sort.Slice(jobs, func(left, right int) bool {
		return jobs[left].ID > jobs[right].ID
	})
	for _, job := range jobs {
		if !strings.HasPrefix(job.ID, gameWorldJobPrefix) {
			continue
		}
		if err := client.stopJob(ctx, job.ID); err != nil {
			return err
		}
	}
	return client.stopJob(ctx, agentJobName)
}

func (client *nomadClient) stopJob(
	parent context.Context,
	jobID string,
) error {
	exists, err := client.jobExists(parent, jobID)
	if err != nil || !exists {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, stopTimeout)
	defer cancel()

	_, _, err = client.api.Jobs().DeregisterOpts(
		jobID,
		&nomad.DeregisterOptions{
			Purge:           false,
			NoShutdownDelay: false,
		},
		(&nomad.WriteOptions{}).WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("deregister Nomad job %s: %w", jobID, err)
	}
	// Deregistration evaluations may legitimately remain blocked on a busy
	// cluster. Allocation terminal state is the authoritative drain outcome,
	// so wait on every original allocation instead of treating scheduler
	// bookkeeping as process-lifecycle failure.
	allocations, _, err := client.api.Jobs().Allocations(
		jobID,
		true,
		(&nomad.QueryOptions{}).WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("list allocations for %s: %w", jobID, err)
	}
	for _, allocation := range allocations {
		if err := client.waitForAllocationTerminal(ctx, allocation.ID); err != nil {
			return fmt.Errorf("stop Nomad job %s: %w", jobID, err)
		}
	}
	_, _, err = client.api.Jobs().DeregisterOpts(
		jobID,
		&nomad.DeregisterOptions{
			Purge:           true,
			NoShutdownDelay: false,
		},
		(&nomad.WriteOptions{}).WithContext(ctx),
	)
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("purge stopped Nomad job %s: %w", jobID, err)
	}
	fmt.Printf("Nomad job %s stopped cleanly\n", jobID)
	return nil
}

func (client *nomadClient) waitForAllocationTerminal(
	ctx context.Context,
	allocationID string,
) error {
	var waitIndex uint64
	for {
		allocation, metadata, err := client.api.Allocations().Info(
			allocationID,
			(&nomad.QueryOptions{
				WaitIndex: waitIndex,
				WaitTime:  10 * time.Second,
			}).WithContext(ctx),
		)
		if err != nil {
			if isNotFound(err) {
				return nil
			}
			return err
		}
		if allocation.ClientTerminalStatus() {
			return nil
		}
		if metadata != nil && metadata.LastIndex > waitIndex {
			waitIndex = metadata.LastIndex
		}
	}
}

func (deployment *deployment) status(
	ctx context.Context,
	client *nomadClient,
) error {
	configured := map[string]bool{agentJobName: true}
	for _, game := range deployment.Shards {
		configured[gameWorldJobPrefix+game.Definition.ID] = true
	}
	registered, _, err := client.api.Jobs().List(
		(&nomad.QueryOptions{
			Prefix: gameWorldJobPrefix,
		}).WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("list managed GameWorld jobs: %w", err)
	}
	for _, job := range registered {
		if strings.HasPrefix(job.ID, gameWorldJobPrefix) {
			if _, known := configured[job.ID]; !known {
				configured[job.ID] = false
			}
		}
	}
	jobIDs := make([]string, 0, len(configured))
	for jobID := range configured {
		jobIDs = append(jobIDs, jobID)
	}
	sort.Strings(jobIDs)
	for _, jobID := range jobIDs {
		job, _, err := client.api.Jobs().Info(
			jobID,
			(&nomad.QueryOptions{}).WithContext(ctx),
		)
		if isNotFound(err) {
			fmt.Printf(
				"%-38s configured=%-5t missing\n",
				jobID,
				configured[jobID],
			)
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect Nomad job %s: %w", jobID, err)
		}
		version := uint64(0)
		status := ""
		if job.Version != nil {
			version = *job.Version
		}
		if job.Status != nil {
			status = *job.Status
		}
		allocations, _, err := client.api.Jobs().Allocations(
			jobID,
			false,
			(&nomad.QueryOptions{}).WithContext(ctx),
		)
		if err != nil {
			return fmt.Errorf(
				"inspect allocations for %s: %w",
				jobID,
				err,
			)
		}
		running := 0
		for _, allocation := range allocations {
			if allocation.ClientStatus == nomad.AllocClientStatusRunning &&
				allocation.DesiredStatus == nomad.AllocDesiredStatusRun {
				running++
			}
		}
		fmt.Printf(
			"%-38s configured=%-5t status=%-8s version=%d running=%d\n",
			jobID,
			configured[jobID],
			status,
			version,
			running,
		)
	}
	return nil
}

func (client *nomadClient) jobExists(
	ctx context.Context,
	jobID string,
) (bool, error) {
	_, _, err := client.api.Jobs().Info(
		jobID,
		(&nomad.QueryOptions{}).WithContext(ctx),
	)
	if isNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect Nomad job %s: %w", jobID, err)
	}
	return true, nil
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var response nomad.UnexpectedResponseError
	return errors.As(err, &response) &&
		response.HasStatusCode() &&
		response.StatusCode() == http.StatusNotFound
}
