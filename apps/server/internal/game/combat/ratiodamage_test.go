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

The percent of the target's maximum hit points, truncated as the
native x87 conversion truncates (control word | 0xC00): the tower tiers (5..14), the fort stone tiers (5..22),
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
		{14, 999, 139}, // 139.86 truncates to 139
		{22, 999, 219}, // 219.78 truncates to 219
		{5, 10, 0},     // 0.5 truncates to 0
		{5, 39, 1},     // 1.95 truncates to 1
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
