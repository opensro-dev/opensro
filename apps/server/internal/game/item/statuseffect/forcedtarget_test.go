/*
===========================================================================

forcedtarget_test.go - one live forced target across effect families

===========================================================================
*/
package statuseffect

import "testing"

/*
================
TestForcedTargetReplacementAndStaleRetirement
================
*/
func TestForcedTargetReplacementAndStaleRetirement(t *testing.T) {
	r := NewRegistry()
	first := Effect{DivisionID: "g", CharacterName: "a", SkillID: 1, SkillGroup: 10,
		InstanceToken: 1, State: StateActive, ForcedTargetGID: 100,
		DurationPresent: true, ExpiresAtMs: 50}
	if !r.Apply(first) || r.ForcedTarget("g", "a", 50) != 100 || r.ForcedTarget("g", "a", 51) != 0 {
		t.Fatal("forced target lifetime lost its strict boundary")
	}
	second := first
	second.SkillID, second.SkillGroup, second.InstanceToken, second.ForcedTargetGID = 2, 20, 2, 200
	if !r.Apply(second) || r.ForcedTarget("g", "a", 1) != 200 {
		t.Fatal("different-family replacement left the old target active")
	}
	r.StopForcedTarget(first)
	if r.ForcedTarget("g", "a", 1) != 200 {
		t.Fatal("stale source retirement stopped the replacement")
	}
	if _, ok := r.RequestVoluntaryStop("g", "a", 2, 2); ok {
		t.Fatal("client canceled the forced target")
	}
	r.StopForcedTarget(second)
	if r.ForcedTarget("g", "a", 1) != 0 {
		t.Fatal("stop-requested target remained authoritative")
	}
	ended := r.DrainStopRequested()
	if len(r.ForcedTargets()) != 0 || len(r.Snapshot("g", "a")) != 0 || len(ended) != 1 || len(ended[0].Effects) != 2 {
		t.Fatalf("retirement retained instances or lost teardown: %+v", ended)
	}
}
