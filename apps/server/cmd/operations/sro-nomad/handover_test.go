/*
===========================================================================

handover_test.go - a deploy is done when the new version serves, not at
promotion

The allocation lists below are the shapes Nomad reported for r203 on
2026-11-20: a promoted canary beside the live allocation, the old one
stopping, the candidate's task waiting out its restart delay after it
died taking over, and the auto-revert's allocation.

===========================================================================
*/
package main

import (
	"testing"
	"time"

	nomad "github.com/hashicorp/nomad/api"
)

const (
	handoverOldVersion = 2
	handoverNewVersion = 3
	// Nomad's other task states (structs.TaskStatePending, TaskStateDead).
	taskStatePending = "pending"
	taskStateDead    = "dead"
)

/*
================
handoverAllocation

One allocation stub: its version, Nomad's wish and the client's state,
whether its deployment calls it healthy, and its task's state and
restarts.
================
*/
func handoverAllocation(
	version uint64,
	desired, client string,
	healthy bool,
	task string,
	restarts uint64,
) *nomad.AllocationListStub {
	return &nomad.AllocationListStub{
		JobVersion:       version,
		DesiredStatus:    desired,
		ClientStatus:     client,
		DeploymentStatus: &nomad.AllocDeploymentStatus{Healthy: &healthy},
		TaskStates: map[string]*nomad.TaskState{
			"gameworld": {State: task, Restarts: restarts},
		},
	}
}

func handoverLive() *nomad.AllocationListStub {
	return handoverAllocation(handoverOldVersion, nomad.AllocDesiredStatusRun,
		nomad.AllocClientStatusRunning, true, taskStateRunning, 0)
}

func handoverStopping() *nomad.AllocationListStub {
	return handoverAllocation(handoverOldVersion, nomad.AllocDesiredStatusStop,
		nomad.AllocClientStatusRunning, true, taskStateRunning, 0)
}

func handoverGone() *nomad.AllocationListStub {
	return handoverAllocation(handoverOldVersion, nomad.AllocDesiredStatusStop,
		nomad.AllocClientStatusComplete, true, taskStateDead, 0)
}

func handoverCandidate(task string, restarts uint64) *nomad.AllocationListStub {
	return handoverAllocation(handoverNewVersion, nomad.AllocDesiredStatusRun,
		nomad.AllocClientStatusRunning, true, task, restarts)
}

/*
================
TestHandoverIsNotDoneAtPromotion

r203 at 11:28:41: the canary is healthy and promoted while the old
allocation still serves. The old wait returned here.
================
*/
func TestHandoverIsNotDoneAtPromotion(t *testing.T) {
	for name, old := range map[string]*nomad.AllocationListStub{
		"live":     handoverLive(),
		"stopping": handoverStopping(),
	} {
		verdict := judgeAllocations(
			[]*nomad.AllocationListStub{old, handoverCandidate(taskStateRunning, 0)},
			handoverNewVersion, 1)
		if verdict.complete() {
			t.Fatalf("%s old allocation: %s counted as done", name, verdict)
		}
	}
}

/*
================
TestHandoverWaitsOutAFailedTakeover

r203 at 11:28:49: the candidate died taking over and waits out its
restart delay, then the auto-revert's allocation (a newer job version
with the old spec) runs. None of it is the new version serving, so the
wait never returns and the deploy fails at its deadline.
================
*/
func TestHandoverWaitsOutAFailedTakeover(t *testing.T) {
	restarting := judgeAllocations(
		[]*nomad.AllocationListStub{handoverGone(), handoverCandidate(taskStatePending, 1)},
		handoverNewVersion, 1)
	if restarting.complete() || restarting.serving != 0 {
		t.Fatalf("a candidate waiting out its restart delay serves: %s", restarting)
	}
	reverted := handoverAllocation(handoverNewVersion+1, nomad.AllocDesiredStatusRun,
		nomad.AllocClientStatusRunning, true, taskStateRunning, 0)
	failed := handoverAllocation(handoverNewVersion, nomad.AllocDesiredStatusStop,
		nomad.AllocClientStatusFailed, false, taskStateDead, 2)
	verdict := judgeAllocations([]*nomad.AllocationListStub{handoverGone(), failed, reverted},
		handoverNewVersion, 1)
	if verdict.complete() || verdict.stale != 1 {
		t.Fatalf("the auto-revert's allocation read as the release serving: %s", verdict)
	}
}

/*
================
TestHandoverHoldsForTheJobsMinHealthyTime

Done only after the complete verdict has lasted the hold; a restart or
an incomplete look in between starts it again.
================
*/
func TestHandoverHoldsForTheJobsMinHealthyTime(t *testing.T) {
	serving := judgeAllocations(
		[]*nomad.AllocationListStub{handoverGone(), handoverCandidate(taskStateRunning, 0)},
		handoverNewVersion, 1)
	if !serving.complete() {
		t.Fatalf("the new version alone and running is not complete: %s", serving)
	}
	start := time.Unix(1_800_000_000, 0)
	watch := handoverWatch{hold: 10 * time.Second}
	if watch.observe(serving, start) {
		t.Fatal("done on the first complete look")
	}
	if watch.observe(serving, start.Add(9*time.Second)) {
		t.Fatal("done before the hold")
	}
	restarted := serving
	restarted.restarts++
	if watch.observe(restarted, start.Add(10*time.Second)) {
		t.Fatal("a restart inside the hold still counted the old start")
	}
	if !watch.observe(restarted, start.Add(20*time.Second)) {
		t.Fatal("not done after a full hold since the restart")
	}
	if watch.observe(handoverVerdict{expected: 1, version: handoverNewVersion}, start.Add(21*time.Second)) {
		t.Fatal("an incomplete look counted as done")
	}
	if watch.observe(serving, start.Add(25*time.Second)) {
		t.Fatal("an incomplete look did not restart the hold")
	}
}

/*
================
TestHandoverHoldIsTheGroupsMinHealthyTime
================
*/
func TestHandoverHoldIsTheGroupsMinHealthyTime(t *testing.T) {
	jobHold, groupHold := 5*time.Second, 15*time.Second
	job := &nomad.Job{
		Update:     &nomad.UpdateStrategy{MinHealthyTime: &jobHold},
		TaskGroups: []*nomad.TaskGroup{{Update: &nomad.UpdateStrategy{MinHealthyTime: &groupHold}}},
	}
	if got := handoverHold(job); got != groupHold {
		t.Fatalf("hold = %s, want the group's %s", got, groupHold)
	}
	if got := handoverHold(&nomad.Job{}); got != defaultHandoverHold {
		t.Fatalf("hold = %s, want Nomad's default %s", got, defaultHandoverHold)
	}
}
