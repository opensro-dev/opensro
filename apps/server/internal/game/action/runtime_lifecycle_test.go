/*
===========================================================================

runtime_lifecycle_test.go - the action tick names its sub-steps

===========================================================================
*/

package action

import (
	"testing"
	"time"
)

/*
================
TestTickHookTimesEveryStepByName

A clock that advances past the slow threshold on every read makes each
step slow, so the report lists every timed step in tick order. Each
step's wait for the publication gate is timed first, under its own name
(tickPublicationGate, #1820 E); advancePendingPickups is the exception -
it takes the gate per pickup itself (publishPendingPickup), so no gate
wait precedes it.
================
*/
func TestTickHookTimesEveryStepByName(t *testing.T) {
	rt, _ := newActiveEffectTestRuntime(t, staticSkillSource{})
	clock := time.UnixMilli(0)
	var names []string
	rt.Steps.Now = func() time.Time {
		clock = clock.Add(150 * time.Millisecond)
		return clock
	}
	rt.Steps.Slow = func(name string, _ time.Duration) { names = append(names, name) }
	rt.TickHook()(1_000_000)
	if len(names) == 0 || names[0] != "tickPublicationGate" {
		t.Fatalf("timed steps = %v", names)
	}
	var steps []string
	for i, name := range names {
		if name == "tickPublicationGate" {
			continue
		}
		if name != "advancePendingPickups" && (i == 0 || names[i-1] != "tickPublicationGate") {
			t.Fatalf("step %s was not preceded by its gate wait: %v", name, names)
		}
		steps = append(steps, name)
	}
	if len(steps) < 41 || steps[0] != "retireGroundApproaches" || steps[1] != "advanceResidentRegions" || steps[len(steps)-1] != "drainAnnouncements" {
		t.Fatalf("timed steps = %v", names)
	}
	seen := map[string]bool{}
	for _, name := range steps {
		if seen[name] {
			t.Fatalf("step %s timed twice", name)
		}
		seen[name] = true
	}
	for _, want := range []string{"drainMonsterDefeats", "checkpointOnlineSkillJobs", "advanceBasicAttackIntents"} {
		if !seen[want] {
			t.Fatalf("step %s was not timed: %v", want, names)
		}
	}
}
