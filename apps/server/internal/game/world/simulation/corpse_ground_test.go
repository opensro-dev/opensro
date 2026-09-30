/*
===========================================================================

corpse_ground_test.go - a monster killed over a rise lands on the ground

===========================================================================
*/
package simulation

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestGroundedLivePoseLeavesTheChord
================
*/
func TestGroundedLivePoseLeavesTheChord(t *testing.T) {
	mover := monster.MoverState{
		From:     monster.Pose{RegionID: 0x6969, X: 0, Y: 10, Z: 0},
		To:       monster.Pose{RegionID: 0x6969, X: 100, Y: 10, Z: 0},
		DepartMs: 1000,
		ArriveMs: 2000,
	}
	// A hill between two equal endpoints: the chord stays at 10.
	hill := func(regionID uint16, x, authoredY, z float64) (float64, bool) { return 25, true }
	if chord := groundedLivePose(mover, 1500, nil); chord.Y != 10 {
		t.Fatalf("chord height %v, want the lerped 10", chord.Y)
	}
	if pose := groundedLivePose(mover, 1500, hill); pose.Y != 25 || pose.X != 50 {
		t.Fatalf("grounded pose %+v, want x 50 on the hill top 25", pose)
	}
}
