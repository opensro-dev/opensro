/*
===========================================================================

skillobjects_steps_test.go - the skill-object hook names its steps

The skill-objects hook used to report only its own total: at 23 ms a
tick on the live shard it named nothing, and only a CPU profile found
the field scan inside it. Each object's pass and the viewer scope are
now step rows in the always-on zone ledger.

===========================================================================
*/
package action

import (
	"testing"
	"time"
)

/*
================
TestSkillObjectHookNamesItsSteps

A live field's pass is timed under skillObjectField and the viewer
scope under skillObjectScope, each once per pass.
================
*/
func TestSkillObjectHookNamesItsSteps(t *testing.T) {
	rt, clock, c, row := harmonyFixture(t)
	releaseHarmony(t, rt, clock, c, row)
	seen := map[string]int{}
	rt.Steps.Total = func(name string, _ time.Duration) { seen[name]++ }
	fieldPass(rt, clock)
	if seen[skillObjectFieldStep] != 1 {
		t.Fatalf("field pass recorded %d time(s) under %q, want 1 (steps %v)", seen[skillObjectFieldStep], skillObjectFieldStep, seen)
	}
	if seen[skillObjectScopeStep] != 1 {
		t.Fatalf("viewer scope recorded %d time(s) under %q, want 1 (steps %v)", seen[skillObjectScopeStep], skillObjectScopeStep, seen)
	}
}
