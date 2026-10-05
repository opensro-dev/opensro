/*
===========================================================================

hawk_test.go - the attacking hawk's strike formulas (40F1B0 / 40F3D0)

===========================================================================
*/

package combat

import "testing"

/*
================
fixedRoll
================
*/
func fixedRoll(value uint32) Roll32767 {
	return func() (uint32, error) { return value, nil }
}

/*
================
TestHawkDamage

An open target takes the base over its parry, less its defense, scaled by
absorption and the incoming factor; a target that would take under 5% of
the base takes a draw between one and 10% of it; a zero sum still deals
one; the magical lane runs only for a nonzero word.
================
*/
func TestHawkDamage(t *testing.T) {
	open := Stats{PhysicalDefense: 20, ParryRate: 100}
	if got, _ := HawkDamage(open, 300, 0, 0, fixedRoll(0)); got != 130 {
		t.Fatalf("open target took %d, want 300/2-20 = 130", got)
	}
	if got, _ := HawkDamage(open, 300, 0, 10, fixedRoll(0)); got != 145 {
		t.Fatalf("rank 10 took %d, want 330/2-20 = 145", got)
	}
	scaled := Stats{PhysicalDefense: 20, ParryRate: 100, PhysicalBasicTaken: 0.5, PhysicalIncoming: 2}
	if got, _ := HawkDamage(scaled, 300, 0, 0, fixedRoll(0)); got != 130 {
		t.Fatalf("absorption 0.5 and incoming 2 gave %d, want 130", got)
	}
	armoured := Stats{PhysicalDefense: 1000}
	if got, _ := HawkDamage(armoured, 300, 0, 0, fixedRoll(0)); got != 1 {
		t.Fatalf("floor at roll 0 gave %d, want 1", got)
	}
	if got, _ := HawkDamage(armoured, 300, 0, 0, fixedRoll(32767)); got != 30 {
		t.Fatalf("floor at the top roll gave %d, want 10%% of 300", got)
	}
	both := Stats{PhysicalDefense: 20, ParryRate: 100, MagicalDefense: 10}
	if got, _ := HawkDamage(both, 300, 110, 0, fixedRoll(0)); got != 230 {
		t.Fatalf("both lanes gave %d, want 130 + 100", got)
	}
	if got, _ := HawkDamage(Stats{}, 0, 0, 0, fixedRoll(0)); got != 1 {
		t.Fatalf("a zero sum dealt %d, want 1", got)
	}
}
