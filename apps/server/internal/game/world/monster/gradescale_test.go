/*
===========================================================================

gradescale_test.go - tests for gradescale.go: a graded monster's speeds
and BCRadius scale by 4C1550's grade factor

===========================================================================
*/

package monster

import "testing"

/*
================
TestGradeScaleSpeedsAndRadius
================
*/
func TestGradeScaleSpeedsAndRadius(t *testing.T) {
	base := Instance{Ref: MonsterRef{BodyRadius: 7, WalkSpeed: 16, RunSpeed: 50}}
	for _, tc := range []struct {
		rarity       uint8
		radius, walk float64
	}{
		{0, 7, 16},
		{1, 10, 24},   // champion: trunc(7 * 1.5)
		{4, 21, 48},   // giant
		{6, 11, 27.2}, // elite: trunc(7 * 1.7)
		{0x13, 7, 16}, // a party unique keeps its body
	} {
		i := base
		i.Ref.MonsterType = tc.rarity
		if got := i.BodyRadius(); got != tc.radius {
			t.Fatalf("grade %#x radius %v, want %v", tc.rarity, got, tc.radius)
		}
		if got := i.WalkSpeed(); float32(got) != float32(tc.walk) {
			t.Fatalf("grade %#x walk %v, want %v", tc.rarity, got, tc.walk)
		}
	}
}
