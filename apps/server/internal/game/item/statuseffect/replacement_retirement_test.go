/*
===========================================================================

replacement_retirement_test.go - ordered replacement of parameter owners

Old contributions and casting states retire before a new instance installs;
failed admission must leave the queued retirement available to the tick.

===========================================================================
*/
package statuseffect

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/paramkeeper"
)

/*
================
replacementHPFixture
================
*/
func replacementHPFixture(t *testing.T) (*Registry, Effect) {
	t.Helper()
	r := NewRegistry()
	m, err := NewModifiers([]paramkeeper.Write{{Parameter: 3, Channel: paramkeeper.PercentSum, Value: -50}})
	if err != nil {
		t.Fatal(err)
	}
	e := Effect{DivisionID: "d", CharacterName: "c", SkillID: 1, SkillGroup: 1, InstanceToken: 7,
		ClientCancelable: true, Modifiers: m, InstalledStates: [2]uint32{7}, RetirementStates: [2]uint32{7}}
	if !r.Apply(e) {
		t.Fatal("initial effect refused")
	}
	if _, ok := r.RequestVoluntaryStop("d", "c", 1, 7); !ok {
		t.Fatal("stop refused")
	}
	e.InstanceToken = 8
	return r, e
}

/*
================
TestReplacementRetiresParametersBeforeInstallingAndKeepsNewStates
================
*/
func TestReplacementRetiresParametersBeforeInstallingAndKeepsNewStates(t *testing.T) {
	r, e := replacementHPFixture(t)
	ended, ok := r.ApplyAfterRetirement(e)
	if !ok || len(ended) != 1 || ended[0].InstanceToken != 7 {
		t.Fatal("old instance not retired", ended, ok)
	}
	writes := r.ModifierWrites("d", "c")
	if len(writes) != 1 || writes[0].Value != -50 {
		t.Fatal("replacement stacked its HP penalty", writes)
	}
	if !r.CastingStates("d", "c").Conflicts(7) {
		t.Fatal("old retirement cleared the replacement's casting state")
	}
	if got := r.DrainStopRequested(); len(got) != 0 {
		t.Fatal("old instance retired twice", got)
	}
	if _, ok := r.RequestVoluntaryStop("d", "c", 1, 8); !ok {
		t.Fatal("new instance cannot stop")
	}
	if got := r.DrainStopRequested(); len(got) != 1 || got[0].Effects[0].InstanceToken != 8 {
		t.Fatal("later retirement lost", got)
	}
}

/*
================
TestFailedReplacementKeepsQueuedRetirement
================
*/
func TestFailedReplacementKeepsQueuedRetirement(t *testing.T) {
	r, e := replacementHPFixture(t)
	r.nextModifierSource = uint64(math.MaxUint32) + 1
	if ended, ok := r.ApplyAfterRetirement(e); ok || len(ended) != 0 {
		t.Fatal("exhausted installation committed", ended, ok)
	}
	if len(r.ModifierWrites("d", "c")) != 1 {
		t.Fatal("failure removed the old contribution before retirement")
	}
	if got := r.DrainStopRequested(); len(got) != 1 || got[0].Effects[0].InstanceToken != 7 {
		t.Fatal("failure lost the old retirement", got)
	}
}
