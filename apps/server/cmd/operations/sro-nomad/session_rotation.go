package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	nomad "github.com/hashicorp/nomad/api"
	"golang.org/x/sync/errgroup"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/platform/privatepath"
	"opensro.online/server/internal/platform/readiness"
	"opensro.online/server/internal/security/auth"
)

const (
	sessionKeyringHeader    = "X-SRO-Session-Keyring"
	sessionActiveKeyHeader  = "X-SRO-Session-Active-Key"
	sessionPublishTimeout   = 2 * time.Minute
	sessionPreflightTimeout = 10 * time.Second
)

func runRotateSessionKey(ctx context.Context, arguments []string) error {
	options, err := parseOptions("rotate-session-key", arguments)
	if err != nil {
		return err
	}
	// Rotation needs the catalog, local signing ring, Nomad Variables, and
	// live acknowledgement endpoints. It does not parse jobspecs or deploy
	// GameWorld TLS/assets, so keep those unrelated deployment prerequisites
	// outside this operation's failure surface.
	deployment, err := resolveDeployment(options, false)
	if err != nil {
		return err
	}
	if len(deployment.Shards) == 0 {
		return fmt.Errorf("shard catalog has no enabled shards")
	}
	client, err := newNomadClient(options.Namespace, options.DevStack)
	if err != nil {
		return err
	}
	defer client.close()
	if err := requireDeploymentCredential(ctx, client, options); err != nil {
		return err
	}
	return client.withFleetLock(
		ctx,
		"rotate-session-key",
		func(lockContext context.Context) error {
			return deployment.rotateSessionKey(lockContext, client)
		},
	)
}

func (deployment *deployment) rotateSessionKey(
	ctx context.Context,
	client *nomadClient,
) error {
	if deployment.AgentURL == "" {
		return fmt.Errorf(
			"-agent-url is required outside loopback so rotation can acknowledge Agent state",
		)
	}
	if err := deployment.requireSessionRotationFleetReconciled(
		ctx,
		client,
	); err != nil {
		return err
	}
	keyPath := filepath.Join(
		deployment.StateDir,
		auth.AgentSessionPrivateKeyRingFile,
	)
	localPayload, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("read local Agent session key ring: %w", err)
	}
	agentVariable, err := requiredNomadVariable(
		ctx,
		client,
		agentVariablePath,
	)
	if err != nil {
		return err
	}
	remotePayload := []byte(agentVariable.Items["agent_session_keyring"])
	if len(remotePayload) == 0 {
		return fmt.Errorf(
			"nomad variable %s has no agent_session_keyring",
			agentVariablePath,
		)
	}

	localStatus, err := auth.InspectAgentSessionKeyRing(localPayload)
	if err != nil {
		return fmt.Errorf("local Agent session key ring: %w", err)
	}
	remoteStatus, err := auth.InspectAgentSessionKeyRing(remotePayload)
	if err != nil {
		return fmt.Errorf("nomad agent session key ring: %w", err)
	}
	if err := validateSessionRotationRecovery(
		localStatus,
		remoteStatus,
	); err != nil {
		return err
	}
	if err := deployment.requireSessionRotationControlPlanesReady(
		ctx,
		localStatus,
		remoteStatus,
	); err != nil {
		return err
	}

	prepared, pendingKeyID, err := auth.PrepareAgentSessionKeyRotation(
		localPayload,
		time.Now(),
	)
	if err != nil {
		return err
	}
	if remoteStatus.ActiveKeyID == pendingKeyID {
		// The Agent activated the pending key before a previous command could
		// durably publish its local completion record. Adopt the exact remote
		// payload instead of ever switching minting back to the old key.
		if err := persistSessionKeyRing(keyPath, remotePayload); err != nil {
			return err
		}
		fmt.Printf(
			"Recovered completed session-key rotation to %s\n",
			pendingKeyID,
		)
		return deployment.publishSessionPublicRing(
			ctx,
			client,
			remotePayload,
		)
	}
	if err := persistSessionKeyRing(keyPath, prepared); err != nil {
		return fmt.Errorf("persist prepared session key: %w", err)
	}
	if err := deployment.publishSessionPublicRing(
		ctx,
		client,
		prepared,
	); err != nil {
		return err
	}

	digest, err := auth.AgentSessionPublicKeyDigest(prepared)
	if err != nil {
		return err
	}
	if err := updateNomadVariableItem(
		ctx,
		client,
		agentVariablePath,
		"agent_session_keyring",
		string(prepared),
	); err != nil {
		return err
	}
	oldActiveKeyID, err := auth.AgentSessionActiveKeyID(prepared)
	if err != nil {
		return err
	}
	if err := waitForSessionKeyAcknowledgement(
		ctx,
		deployment.AgentURL+readiness.PathReady,
		digest,
		oldActiveKeyID,
	); err != nil {
		return fmt.Errorf("agent prepared-key acknowledgement: %w", err)
	}

	activated, err := auth.ActivateAgentSessionKey(
		prepared,
		pendingKeyID,
		time.Now(),
	)
	if err != nil {
		return err
	}
	if err := updateNomadVariableItem(
		ctx,
		client,
		agentVariablePath,
		"agent_session_keyring",
		string(activated),
	); err != nil {
		return err
	}
	if err := waitForSessionKeyAcknowledgement(
		ctx,
		deployment.AgentURL+readiness.PathReady,
		digest,
		pendingKeyID,
	); err != nil {
		return fmt.Errorf("agent activation acknowledgement: %w", err)
	}
	if err := persistSessionKeyRing(keyPath, activated); err != nil {
		return fmt.Errorf(
			"agent activated key %s but local completion could not be persisted: %w; rerun the same command to recover",
			pendingKeyID,
			err,
		)
	}
	fmt.Printf(
		"Activated Agent session key %s; the former key remains accepted for %s\n",
		pendingKeyID,
		auth.AgentSessionMaxLifetime,
	)
	return nil
}

func (deployment *deployment) requireSessionRotationFleetReconciled(
	ctx context.Context,
	client *nomadClient,
) error {
	agent, _, err := client.api.Jobs().Info(
		agentJobName,
		(&nomad.QueryOptions{}).WithContext(ctx),
	)
	if isNotFound(err) {
		return fmt.Errorf(
			"session-key rotation requires a running %s job; run sro-nomad deploy",
			agentJobName,
		)
	}
	if err != nil {
		return fmt.Errorf("inspect Agent job before session-key rotation: %w", err)
	}
	if agent == nil ||
		agent.Stop == nil || *agent.Stop ||
		agent.Status == nil || *agent.Status != "running" {
		return fmt.Errorf(
			"session-key rotation requires %s to be registered and running; run sro-nomad deploy",
			agentJobName,
		)
	}

	jobs, _, err := client.api.Jobs().List(
		(&nomad.QueryOptions{
			Prefix: gameWorldJobPrefix,
		}).WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf(
			"list GameWorld jobs before session-key rotation: %w",
			err,
		)
	}
	return validateSessionRotationGameWorldJobs(deployment.Shards, jobs)
}

func validateSessionRotationGameWorldJobs(
	shards []shardDeployment,
	jobs []*nomad.JobListStub,
) error {
	expected := make(map[string]struct{}, len(shards))
	for _, game := range shards {
		expected[gameWorldJobPrefix+game.Definition.ID] = struct{}{}
	}

	var disabled []string
	var notRunning []string
	for _, job := range jobs {
		if job == nil || !strings.HasPrefix(job.ID, gameWorldJobPrefix) {
			continue
		}
		if _, enabled := expected[job.ID]; !enabled {
			disabled = append(disabled, job.ID)
			continue
		}
		delete(expected, job.ID)
		if job.Stop || job.Status != "running" {
			notRunning = append(notRunning, job.ID)
		}
	}

	missing := make([]string, 0, len(expected))
	for jobID := range expected {
		missing = append(missing, jobID)
	}
	sort.Strings(disabled)
	sort.Strings(missing)
	sort.Strings(notRunning)
	if len(disabled) == 0 &&
		len(missing) == 0 &&
		len(notRunning) == 0 {
		return nil
	}
	return fmt.Errorf(
		"session-key rotation requires the registered GameWorld jobs to exactly match the enabled catalog and be running (disabled registered=%v, missing enabled=%v, not running=%v); run sro-nomad deploy to reconcile the fleet",
		disabled,
		missing,
		notRunning,
	)
}

func (deployment *deployment) requireSessionRotationControlPlanesReady(
	ctx context.Context,
	local auth.AgentSessionKeyRingStatus,
	remote auth.AgentSessionKeyRingStatus,
) error {
	gameWorldDigests := []string{remote.PublicDigest}
	if local.PublicDigest != remote.PublicDigest {
		// A resumable command can have published the prepared public ring to
		// only some GameWorlds before failing. Both the remote Agent ring and
		// the durable local pending ring are compatible preflight states.
		gameWorldDigests = append(gameWorldDigests, local.PublicDigest)
	}

	group, groupContext := errgroup.WithContext(ctx)
	group.SetLimit(4)
	group.Go(func() error {
		endpoint := strings.TrimSuffix(
			deployment.AgentURL,
			"/",
		) + readiness.PathReady
		if err := waitForSessionKeyState(
			groupContext,
			endpoint,
			[]string{remote.PublicDigest},
			remote.ActiveKeyID,
			sessionPreflightTimeout,
		); err != nil {
			return fmt.Errorf(
				"agent current-key preflight at %s: %w",
				endpoint,
				err,
			)
		}
		return nil
	})
	for _, game := range deployment.Shards {
		group.Go(func() error {
			endpoint := strings.TrimSuffix(
				game.Definition.ControlURL,
				"/",
			) + readiness.PathReady
			if err := waitForSessionKeyState(
				groupContext,
				endpoint,
				gameWorldDigests,
				"",
				sessionPreflightTimeout,
			); err != nil {
				return fmt.Errorf(
					"GameWorld %s current-key preflight at %s: %w",
					game.Definition.ID,
					endpoint,
					err,
				)
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return fmt.Errorf(
			"session-key rotation preflight failed before any key state changed; run deploy to repair unhealthy allocations and run this command from a host that can reach every private control URL: %w",
			err,
		)
	}
	fmt.Printf(
		"Session-key preflight confirmed Agent and %d GameWorld control plane(s)\n",
		len(deployment.Shards),
	)
	return nil
}

func validateSessionRotationRecovery(
	local auth.AgentSessionKeyRingStatus,
	remote auth.AgentSessionKeyRingStatus,
) error {
	if local.PendingKeyID == "" {
		if local.PublicDigest != remote.PublicDigest ||
			local.ActiveKeyID != remote.ActiveKeyID {
			return fmt.Errorf(
				"local and Nomad Agent session key rings differ outside a resumable rotation; refusing to overwrite either authority",
			)
		}
		return nil
	}
	if remote.ActiveKeyID != local.ActiveKeyID &&
		remote.ActiveKeyID != local.PendingKeyID {
		return fmt.Errorf(
			"nomad active session key %q is neither local active %q nor pending %q",
			remote.ActiveKeyID,
			local.ActiveKeyID,
			local.PendingKeyID,
		)
	}
	localIDs := make(map[string]struct{}, len(local.KeyIDs))
	for _, keyID := range local.KeyIDs {
		localIDs[keyID] = struct{}{}
	}
	for _, keyID := range remote.KeyIDs {
		if _, found := localIDs[keyID]; !found {
			return fmt.Errorf(
				"nomad session key %q is absent from the local resumable ring",
				keyID,
			)
		}
	}
	if len(remote.KeyIDs) != len(local.KeyIDs) &&
		len(remote.KeyIDs) != len(local.KeyIDs)-1 {
		return fmt.Errorf(
			"nomad session key set cannot be reconciled with local pending rotation",
		)
	}
	if len(remote.KeyIDs) == len(local.KeyIDs)-1 {
		for _, keyID := range remote.KeyIDs {
			if keyID == local.PendingKeyID {
				return fmt.Errorf(
					"nomad partial key set contains pending key %q but omits another key",
					local.PendingKeyID,
				)
			}
		}
	}
	return nil
}

func (deployment *deployment) publishSessionPublicRing(
	ctx context.Context,
	client *nomadClient,
	privatePayload []byte,
) error {
	publicPayload, err := auth.PublicAgentSessionKeyRing(privatePayload)
	if err != nil {
		return err
	}
	digest, err := auth.AgentSessionPublicKeyDigest(privatePayload)
	if err != nil {
		return err
	}
	group, groupContext := errgroup.WithContext(ctx)
	group.SetLimit(4)
	for _, game := range deployment.Shards {
		group.Go(func() error {
			path := gameWorldVariablePrefix +
				gameWorldJobPrefix + game.Definition.ID +
				gameWorldVariableSuffix
			if err := updateNomadVariableItem(
				groupContext,
				client,
				path,
				"agent_session_public_keys",
				string(publicPayload),
			); err != nil {
				return err
			}
			endpoint := strings.TrimSuffix(
				game.Definition.ControlURL,
				"/",
			) + readiness.PathReady
			if err := waitForSessionKeyAcknowledgement(
				groupContext,
				endpoint,
				digest,
				"",
			); err != nil {
				return fmt.Errorf(
					"GameWorld %s key acknowledgement: %w",
					game.Definition.ID,
					err,
				)
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return err
	}
	fmt.Printf(
		"All %d GameWorld shard(s) acknowledged session key ring %s\n",
		len(deployment.Shards),
		digest,
	)
	return nil
}

func requiredNomadVariable(
	ctx context.Context,
	client *nomadClient,
	path string,
) (*nomad.Variable, error) {
	value, _, err := client.api.Variables().Peek(
		path,
		(&nomad.QueryOptions{}).WithContext(ctx),
	)
	if err != nil {
		return nil, fmt.Errorf("read Nomad variable %s: %w", path, err)
	}
	if value == nil {
		return nil, fmt.Errorf(
			"nomad variable %s is absent; deploy the fleet before rotating",
			path,
		)
	}
	return value, nil
}

func updateNomadVariableItem(
	ctx context.Context,
	client *nomadClient,
	path string,
	item string,
	value string,
) error {
	current, err := requiredNomadVariable(ctx, client, path)
	if err != nil {
		return err
	}
	if current.Items[item] == value {
		return nil
	}
	updated := nomad.NewVariable(path)
	updated.Namespace = client.namespace
	updated.ModifyIndex = current.ModifyIndex
	updated.Items = make(nomad.VariableItems, len(current.Items))
	for key, currentValue := range current.Items {
		updated.Items[key] = currentValue
	}
	updated.Items[item] = value
	if _, _, err := client.api.Variables().CheckedUpdate(
		updated,
		(&nomad.WriteOptions{}).WithContext(ctx),
	); err != nil {
		return fmt.Errorf("update Nomad variable %s with CAS: %w", path, err)
	}
	return nil
}

func waitForSessionKeyAcknowledgement(
	parent context.Context,
	endpoint string,
	digest string,
	activeKeyID string,
) error {
	return waitForSessionKeyState(
		parent,
		endpoint,
		[]string{digest},
		activeKeyID,
		sessionPublishTimeout,
	)
}

func waitForSessionKeyState(
	parent context.Context,
	endpoint string,
	acceptedDigests []string,
	activeKeyID string,
	timeout time.Duration,
) error {
	if _, err := url.ParseRequestURI(endpoint); err != nil {
		return fmt.Errorf("invalid readiness URL %q: %w", endpoint, err)
	}
	if timeout <= 0 {
		return fmt.Errorf("readiness timeout must be positive")
	}
	digests := make(map[string]struct{}, len(acceptedDigests))
	for _, digest := range acceptedDigests {
		if digest == "" {
			return fmt.Errorf("accepted session-key digest is empty")
		}
		digests[digest] = struct{}{}
	}
	if len(digests) == 0 {
		return fmt.Errorf("at least one session-key digest is required")
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	client := &http.Client{Timeout: 3 * time.Second}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var lastState string
	for {
		request, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			endpoint,
			nil,
		)
		if err != nil {
			return err
		}
		response, requestErr := client.Do(request)
		if requestErr == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
			_ = response.Body.Close()
			gotDigest := response.Header.Get(sessionKeyringHeader)
			gotActive := response.Header.Get(sessionActiveKeyHeader)
			lastState = fmt.Sprintf(
				"HTTP %d digest=%q active=%q",
				response.StatusCode,
				gotDigest,
				gotActive,
			)
			_, digestAccepted := digests[gotDigest]
			if response.StatusCode == http.StatusOK &&
				digestAccepted &&
				(activeKeyID == "" || gotActive == activeKeyID) {
				return nil
			}
		} else {
			lastState = requestErr.Error()
		}
		select {
		case <-ctx.Done():
			expected := make([]string, 0, len(digests))
			for digest := range digests {
				expected = append(expected, digest)
			}
			sort.Strings(expected)
			return fmt.Errorf(
				"%s did not acknowledge an accepted digest %v (%s): %w",
				endpoint,
				expected,
				lastState,
				ctx.Err(),
			)
		case <-ticker.C:
		}
	}
}

func persistSessionKeyRing(path string, payload []byte) error {
	if err := store.WriteFileAtomic(path, payload); err != nil {
		return err
	}
	if err := privatepath.ProtectFile(path); err != nil {
		return fmt.Errorf("protect Agent session key ring: %w", err)
	}
	return nil
}
