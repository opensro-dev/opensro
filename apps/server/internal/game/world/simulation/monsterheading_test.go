package simulation

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

// Monster and player producers must encode the same wire bearings, including
// the model-yaw offset applied by native spawn decoder 8535A0.
func TestHeadingWordTowardUsesNativeYawConvention(t *testing.T) {
	const quarter = 0xffff / 4.0
	cases := []struct {
		name    string
		dx, dz  float64
		wantRad float64
	}{
		{"north/-z is yaw 0", 0, -10, 0},
		{"east/+x is yaw pi/2", 10, 0, math.Pi / 2},
		{"south/+z is yaw pi", 0, 10, math.Pi},
		{"west/-x is yaw 3pi/2", -10, 0, 3 * math.Pi / 2},
	}
	for _, tc := range cases {
		from := monster.Pose{RegionID: monsterTestRegion, X: 1000, Y: 20, Z: 1000}
		to := monster.Pose{RegionID: monsterTestRegion, X: from.X + tc.dx, Y: 20, Z: from.Z + tc.dz}
		want := uint16(int(math.Round(math.Mod(tc.wantRad+3*math.Pi/2, twoPi)/twoPi*0xffff)) & 0xffff)
		got := headingWordToward(from, to)
		// One unit of slack: the wire unit is 1/65535 of a circle, so a
		// half-unit rounding boundary must not fail a correct convention.
		if diff := int(got) - int(want); diff < -1 || diff > 1 {
			t.Errorf("%s: headingWord = %d (%.1f deg), want %d (%.1f deg)",
				tc.name, got, float64(got)/quarter*90, want, float64(want)/quarter*90)
		}
	}

	// Compare independent movement producers across cardinal and diagonal legs.
	for _, d := range []struct{ dx, dz float64 }{
		{10, 0}, {0, 10}, {-10, 0}, {0, -10},
		{7, 7}, {-7, 7}, {7, -7}, {-7, -7},
		{13, 5}, {-3, 17},
	} {
		from := monster.Pose{RegionID: monsterTestRegion, X: 1000, Y: 20, Z: 1000}
		to := monster.Pose{RegionID: monsterTestRegion, X: from.X + d.dx, Y: 20, Z: from.Z + d.dz}
		monster := headingWordToward(from, to)
		player, ok := HeadingFromMovement(
			Spawn{RegionID: from.RegionID, X: from.X, Y: from.Y, Z: from.Z},
			Spawn{RegionID: to.RegionID, X: to.X, Y: to.Y, Z: to.Z},
		)
		if !ok {
			t.Fatalf("player-plane heading not ok for delta (%v, %v)", d.dx, d.dz)
		}
		if diff := int(monster) - int(player); diff < -1 || diff > 1 {
			t.Errorf("delta (%v, %v): monster heading %d disagrees with player-plane HeadingFromMovement %d",
				d.dx, d.dz, monster, player)
		}
	}

	// +Z is a quarter-circle wire bearing, not zero or model yaw pi.
	from := monster.Pose{RegionID: monsterTestRegion, X: 1000, Y: 20, Z: 1000}
	to := monster.Pose{RegionID: monsterTestRegion, X: 1000, Y: 20, Z: 1100}
	if got := headingWordToward(from, to); got < 0x3000 || got > 0x5000 {
		t.Errorf("southward travel produced heading %d; the mirrored Atan2(dx, dz) form returns ~0 here (BUG-11)", got)
	}
}
