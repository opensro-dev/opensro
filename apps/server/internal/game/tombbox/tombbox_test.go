/*
===========================================================================

tombbox_test.go - tests for tombbox.go

===========================================================================
*/
package tombbox

import (
	"math"
	"testing"
)

/*
================
TestBoxWeightsCoverTheRoll

Each box's nine weights total 100 percent, so the walk ends inside them.
================
*/
func TestBoxWeightsCoverTheRoll(t *testing.T) {
	for _, box := range Boxes {
		sum := 0.0
		for _, weight := range box.Weights {
			sum += weight
		}
		if math.Abs(sum-100) > 1e-9 {
			t.Fatalf("%s weights total %v", box.Npc, sum)
		}
	}
}

/*
================
TestPickOutcomeWalksTheRunningSum

r = draw/32767*100 picks the first outcome whose running sum is not below
it: box 1's damage weight is 9.2818, so draw 3041 (r 9.2807) is damage
and 3042 (r 9.2837) the guards; the top draw lands on the last outcome.
================
*/
func TestPickOutcomeWalksTheRunningSum(t *testing.T) {
	box := Boxes[0]
	for _, tc := range []struct {
		draw uint32
		want int
	}{
		{0, OutcomeDamage},
		{3041, OutcomeDamage},
		{3042, OutcomeGuards},
		{randMax / 2, OutcomeGold},
		{randMax, OutcomeSealRepair},
	} {
		if got := PickOutcome(box, tc.draw); got != tc.want {
			t.Fatalf("draw %d: outcome %d, want %d", tc.draw, got, tc.want)
		}
	}
}

/*
================
TestKeyDropsAtTheNativeChance

Courage's key drops at 0.5 percent: r = 0.005 drops, one step more does
not, r = 0 never does, and a party monster's eightfold 4 percent drops at
r = 0.04.
================
*/
func TestKeyDropsAtTheNativeChance(t *testing.T) {
	box := Boxes[0]
	if KeyDrops(box, false, 0, 0) {
		t.Fatal("a zero roll dropped a key")
	}
	if !KeyDrops(box, false, 0, 5000) || KeyDrops(box, false, 0, 5001) {
		t.Fatal("the 0.5 percent edge is wrong")
	}
	// hi 1, lo 7232: (1 << 15 | 7232) = 40000, r = 0.04.
	if !KeyDrops(box, true, 1, 7232) || KeyDrops(box, false, 1, 7232) {
		t.Fatal("the party factor is wrong")
	}
}

/*
================
TestSwitchAndLookups
================
*/
func TestSwitchAndLookups(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "on": true, " TRUE ": true, "1": true, "yes": false} {
		if Enabled(value) != want {
			t.Fatalf("Enabled(%q) = %v", value, !want)
		}
	}
	if box, ok := BoxAt("NPC_TQ_TREASUREBOX4"); !ok || box.Gold != 400000 {
		t.Fatal("box 4 not found")
	}
	if _, ok := BoxAt("NPC_CH_SMITH"); ok {
		t.Fatal("a smith is not a box")
	}
	if !IsTombMonster("MOB_TQ_TOMBARCHER") || IsTombMonster("MOB_QT_TQ_GENERAL") {
		t.Fatal("tomb prefix test is wrong")
	}
}
