/*
===========================================================================

threat_arithmetic_test.go - the threat percent is applied on the FPU

AccumulateThreat (5903EC..5904D3) forms (percent / 100.0 + 1.0) * acc and
SplitLinkedThreat (5A042D..5A0452) percent / 100.0 * aggression, both
truncated. percent / 100 is inexact in binary, so integer
acc * (100 + percent) / 100 and aggression * percent / 100 differ from the
native result; the vectors below are those differences.

===========================================================================
*/

package combat

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestAccumulateThreatDividesOnTheFPU
================
*/
func TestAccumulateThreatDividesOnTheFPU(t *testing.T) {
	for _, tc := range []struct {
		percent, damage, flat, want uint32
	}{
		{13, 100, 0, 112},   // integer: 113
		{36, 75, 0, 101},    // integer: 102
		{41, 2900, 0, 4088}, // integer: 4089
		{41, 300, 7, 430},   // 423 + flat 7
	} {
		threat := enterworld.SkillThreat{Present: true, Percent: tc.percent, Flat: tc.flat}
		if got := AccumulateThreat(0, tc.damage, threat); got != tc.want {
			t.Errorf("AccumulateThreat(%d %%, %d, flat %d) = %d, want %d", tc.percent, tc.damage, tc.flat, got, tc.want)
		}
	}
}

/*
================
TestSplitLinkedThreatDividesOnTheFPU
================
*/
func TestSplitLinkedThreatDividesOnTheFPU(t *testing.T) {
	for _, tc := range []struct {
		aggression, percent, transferred uint32
	}{
		{300, 41, 122},   // integer: 123
		{600, 41, 245},   // integer: 246
		{4700, 41, 1926}, // integer: 1927
		{100, 36, 36},
	} {
		remaining, transferred := SplitLinkedThreat(tc.aggression, tc.percent)
		if transferred != tc.transferred || remaining != tc.aggression-tc.transferred {
			t.Errorf("SplitLinkedThreat(%d, %d %%) = %d / %d, want %d / %d", tc.aggression, tc.percent,
				remaining, transferred, tc.aggression-tc.transferred, tc.transferred)
		}
	}
}
