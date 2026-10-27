/*
===========================================================================

mostwanted_test.go - a saved streak's level, progress and damage percent

Port-only, not native (action/mostwanted.go owns the rule): the projection
the stat writes, the bounty and the wanted-state frame (#1849) all read,
pinned at its boundaries.

===========================================================================
*/
package domain

import "testing"

/*
================
TestMostWantedProjection
================
*/
func TestMostWantedProjection(t *testing.T) {
	for streak, want := range map[int64][2]int64{
		0: {0, 0}, 1: {0, 1}, 9: {0, 9}, 10: {1, 0}, 11: {1, 1}, 25: {2, 5},
		99: {9, 9}, 100: {10, 10}, 101: {10, 10}, 250: {10, 10},
	} {
		m := &MostWanted{Streak: streak}
		if got := [2]int64{m.Level(), m.KillsIntoLevel()}; got != want {
			t.Fatalf("streak %d: level/into %v, want %v", streak, got, want)
		}
	}
	var none *MostWanted
	if none.Level() != 0 || none.KillsIntoLevel() != 0 {
		t.Fatal("a nil streak projects a level")
	}
	for level, want := range map[int64]float32{0: 0, 2: 0, 3: 1, 4: 1, 5: 2, 9: 2, 10: 3} {
		if got := MostWantedDamagePercent(level); got != want {
			t.Fatalf("level %d: %v%%, want %v%%", level, got, want)
		}
	}
}
