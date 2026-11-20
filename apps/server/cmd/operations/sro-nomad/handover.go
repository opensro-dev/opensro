/*
===========================================================================

handover.go - when a deployed job is really serving its new version

waitForJobAllocations asks this file whether a job's deployment is done.
"Nomad marked the new allocations healthy" is not enough under the
canary standby handover (opensro-ops#281): Nomad promotes the canary on
its standby check, while the old allocation still serves, and only then
stops the old process; the candidate takes the shard over after that.
On 2026-11-20 r203's deploy returned at promotion (11:28:41), the old
process stopped at 11:28:46, and the candidate died taking over at
11:28:49 - after the deploy had reported it healthy with 0.0 s downtime,
and the release had moved on to verifying.

A deployment is done here only when every desired allocation of the new
version is running with every task running, no allocation of another
version is still alive, and that has held, with no task restart, for
the group's own min_healthy_time - the window Nomad itself holds a
candidate to before promotion, now applied to the takeover after it.

===========================================================================
*/
package main

import (
	"fmt"
	"time"

	nomad "github.com/hashicorp/nomad/api"
)

const (
	// Nomad's own default when an update block names no min_healthy_time.
	defaultHandoverHold = 10 * time.Second
	// A task's State while its process runs (Nomad's structs.TaskStateRunning;
	// the api package names no task states).
	taskStateRunning = "running"
)

/*
================
handoverVerdict

One look at a job's allocations: how many of the new version serve, how
many of any other version are still alive, and the serving allocations'
task restarts (a restart during the hold means the takeover failed).
================
*/
type handoverVerdict struct {
	serving  int
	expected int
	stale    int
	restarts uint64
	version  uint64
}

/*
================
judgeAllocations
================
*/
func judgeAllocations(
	allocations []*nomad.AllocationListStub,
	version uint64,
	expected int,
) handoverVerdict {
	verdict := handoverVerdict{expected: expected, version: version}
	for _, allocation := range allocations {
		if allocation == nil {
			continue
		}
		alive := allocation.ClientStatus == nomad.AllocClientStatusRunning ||
			allocation.ClientStatus == nomad.AllocClientStatusPending
		if allocation.JobVersion != version {
			// An old allocation told to stop is still alive until its
			// process exits: it may hold the shard's guard or port.
			if alive {
				verdict.stale++
			}
			continue
		}
		if !allocationServes(allocation) {
			continue
		}
		verdict.serving++
		for _, task := range allocation.TaskStates {
			if task != nil {
				verdict.restarts += task.Restarts
			}
		}
	}
	return verdict
}

/*
================
allocationServes

A new-version allocation Nomad wants running, runs, marked healthy by
its deployment, with every task running (a task waiting out its restart
delay is not serving).
================
*/
func allocationServes(allocation *nomad.AllocationListStub) bool {
	if allocation.DesiredStatus != nomad.AllocDesiredStatusRun ||
		allocation.ClientStatus != nomad.AllocClientStatusRunning ||
		allocation.DeploymentStatus == nil ||
		allocation.DeploymentStatus.Healthy == nil ||
		!*allocation.DeploymentStatus.Healthy ||
		len(allocation.TaskStates) == 0 {
		return false
	}
	for _, task := range allocation.TaskStates {
		if task == nil || task.State != taskStateRunning {
			return false
		}
	}
	return true
}

/*
================
handoverVerdict.complete
================
*/
func (verdict handoverVerdict) complete() bool {
	return verdict.expected > 0 && verdict.serving == verdict.expected && verdict.stale == 0
}

/*
================
handoverVerdict.String

The progress line the deploy prints when it changes.
================
*/
func (verdict handoverVerdict) String() string {
	status := fmt.Sprintf(
		"%d/%d serving allocation(s) at version %d",
		verdict.serving,
		verdict.expected,
		verdict.version,
	)
	if verdict.stale > 0 {
		status += fmt.Sprintf(", %d older allocation(s) still alive", verdict.stale)
	}
	return status
}

/*
================
handoverHold

The job's min_healthy_time: the task group's update block wins over the
job's, and Nomad's default stands when neither names one.
================
*/
func handoverHold(job *nomad.Job) time.Duration {
	hold := time.Duration(0)
	if job != nil && job.Update != nil && job.Update.MinHealthyTime != nil {
		hold = *job.Update.MinHealthyTime
	}
	if job != nil {
		for _, group := range job.TaskGroups {
			if group != nil && group.Update != nil && group.Update.MinHealthyTime != nil &&
				*group.Update.MinHealthyTime > hold {
				hold = *group.Update.MinHealthyTime
			}
		}
	}
	if hold <= 0 {
		return defaultHandoverHold
	}
	return hold
}

/*
================
handoverWatch

Holds complete verdicts until they have lasted the hold with the
serving allocations' restart count unchanged. Any incomplete verdict,
or a restart, starts the hold again.
================
*/
type handoverWatch struct {
	hold     time.Duration
	since    time.Time
	restarts uint64
}

/*
================
handoverWatch.observe

True once the handover has held for the whole hold.
================
*/
func (watch *handoverWatch) observe(verdict handoverVerdict, now time.Time) bool {
	if !verdict.complete() {
		watch.since = time.Time{}
		return false
	}
	if watch.since.IsZero() || verdict.restarts != watch.restarts {
		watch.since = now
		watch.restarts = verdict.restarts
	}
	return now.Sub(watch.since) >= watch.hold
}
