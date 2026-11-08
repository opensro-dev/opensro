/*
===========================================================================

hawk_test.go - the attacking hawk's strike formulas (physical and magical lanes)

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

/*
================
TestHawkDamageNativeRows

Strikes on real v1.150 rows (#2500): MOB_RM_WINGTRIBE (level 80, physical
defense 838, parry 80) and MOB_RM_ROCKY_CLON2 (level 90, physical defense
1078, parry 90) against the hawk summ words (SKILL_CH_BOW_CALL_B_12 = 905,
D_01 = 1297, D_05 = 1883; the magical word is zero on every hawk row). At
these defenses the weaker rows land under 5% of their scaled base and draw
between one and 10% of it instead - a reported 106 from the rank 1
Lightning Hawk is one such draw (its band is 1..129 at mastery rank 0).
================
*/
func TestHawkDamageNativeRows(t *testing.T) {
	wingTribe := Stats{PhysicalDefense: 838, ParryRate: 80}
	if got, _ := HawkDamage(wingTribe, 1883, 0, 0, fixedRoll(32767)); got != 208 {
		t.Fatalf("D_05 on a level 80 monster took %d, want 1883/1.8-838 = 208", got)
	}
	if got, _ := HawkDamage(wingTribe, 1297, 0, 0, fixedRoll(0)); got != 1 {
		t.Fatalf("D_01 floor draw gave %d, want 1", got)
	}
	if got, _ := HawkDamage(wingTribe, 1297, 0, 0, fixedRoll(16384)); got != 65 {
		t.Fatalf("D_01 mid draw gave %d, want 65", got)
	}
	if got, _ := HawkDamage(wingTribe, 1297, 0, 0, fixedRoll(32767)); got != 129 {
		t.Fatalf("D_01 top draw gave %d, want 129", got)
	}
	if got, _ := HawkDamage(wingTribe, 905, 0, 0, fixedRoll(32767)); got != 90 {
		t.Fatalf("B_12 top draw gave %d, want 90", got)
	}
	if got, _ := HawkDamage(wingTribe, 905, 0, 90, fixedRoll(0)); got != 117 {
		t.Fatalf("B_12 at caster mastery 90 took %d, want 905*1.9/1.8-838 = 117", got)
	}
	rocky := Stats{PhysicalDefense: 1078, ParryRate: 90}
	if got, _ := HawkDamage(rocky, 1883, 0, 90, fixedRoll(0)); got != 805 {
		t.Fatalf("D_05 at caster mastery 90 took %d, want 1883*1.9/1.9-1078 = 805", got)
	}
}
