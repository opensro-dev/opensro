/*
===========================================================================

ratiodamage_test.go - the ratio arm's arithmetic

===========================================================================
*/

package combat

import "testing"

/*
================
TestHPRatioDamage

The percent of the target's maximum hit points, rounded to the
nearest point: the tower tiers (5..14), the fort stone tiers (5..22),
and the dead inputs.
================
*/
func TestHPRatioDamage(t *testing.T) {
	for _, tc := range []struct {
		percent uint32
		maxHP   int64
		want    uint32
	}{
		{14, 1000, 140},
		{5, 1000, 50},
		{22, 1000, 220},
		{14, 999, 140}, // 139.86 rounds to 140
		{22, 999, 220}, // 219.78 rounds to 220
		{5, 10, 1},     // 0.5 rounds to 1
		{0, 1000, 0},
		{14, 0, 0},
		{14, -5, 0},
		{22, 1 << 26, 14763950},
		{100, 1 << 30, 0xFFFFFF}, // the wire ceiling
		{22, 1 << 40, 0xFFFFFF},  // the wire ceiling
	} {
		if got := HPRatioDamage(tc.percent, tc.maxHP); got != tc.want {
			t.Fatalf("HPRatioDamage(%d, %d) = %d, want %d", tc.percent, tc.maxHP, got, tc.want)
		}
	}
}
