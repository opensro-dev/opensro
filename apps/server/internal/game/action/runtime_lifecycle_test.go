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
step slow, so the report lists every timed step in tick order.
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
	if len(names) < 41 || names[0] != "retireGroundApproaches" || names[1] != "advanceResidentRegions" || names[len(names)-1] != "retireActionSessions" {
		t.Fatalf("timed steps = %v", names)
	}
	seen := map[string]bool{}
	for _, name := range names {
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
