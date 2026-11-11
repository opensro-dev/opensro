/*
===========================================================================

nomad_stop.go - stopping a deployment's jobs and waiting them out

sro-nomad stop deregisters every job of a deployment and waits until
each allocation is terminal, so the next start never meets a process
that still holds the shard. Split from nomad.go on that boundary:
nomad.go places and watches jobs, this file takes them down.

===========================================================================
*/

package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	nomad "github.com/hashicorp/nomad/api"
)

/*
================
stop
================
*/
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

/*
================
stopJob
================
*/
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

/*
================
waitForAllocationTerminal
================
*/
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
