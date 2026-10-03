/*
===========================================================================

spawn_surface_test.go - a spawn stands on the surface its walk reached

5F6EB0 creates a population monster at the position the region manager's
move test wrote while walking from the nest centre to the generated
candidate. The Simos Ladon nest in 0x684B sits 40 m from a stone bridge
(euro_stbridge01) over a gorge; grounding candidates by the nest's authored
height put a champion on the bridge's edge cells, where it could neither
move nor be reached.

===========================================================================
*/
package movement

import (
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
stuckAt

How many of eight short moves from p are refused.
================
*/
func stuckAt(v *WaterValidator, p simulation.Spawn) int {
	owner, _, _ := v.ResolveNavOwner(p, simulation.NavOwner{})
	blocked := 0
	for _, d := range [][2]float64{{15, 0}, {-15, 0}, {0, 15}, {0, -15}, {11, 11}, {-11, -11}, {11, -11}, {-11, 11}} {
		to := simulation.Spawn{RegionID: p.RegionID, X: p.X + d[0], Y: p.Y, Z: p.Z + d[1]}
		if v.ClipMovementPathFrom(p, owner, to).Outcome != ClipArrived {
			blocked++
		}
	}
	return blocked
}

/*
================
TestGeneratedSpawnsNearABridgeStandWhereTheWalkReached
================
*/
func TestGeneratedSpawnsNearABridgeStandWhereTheWalkReached(t *testing.T) {
	v := realAuthorityValidator(t)
	const region, nestY = uint16(0x684b), -132.65
	centre := simulation.Spawn{RegionID: region, X: 424.27, Y: nestY, Z: 347.16}
	strandedByAuthoredY, admitted := 0, 0
	for x := 520.0; x <= 700; x += 10 {
		for z := 610.0; z <= 800; z += 10 {
			candidate := simulation.Spawn{RegionID: region, X: x, Y: nestY, Z: z}
			if guessed, ok := v.WalkableSpawnHeightAt(region, x, nestY, z); ok {
				if stuckAt(v, simulation.Spawn{RegionID: region, X: x, Y: guessed, Z: z}) >= 6 {
					strandedByAuthoredY++
				}
			}
			move := v.SpawnMoveTest(centre, candidate)
			if move.Result != 0 {
				continue
			}
			y, ok := v.WalkableSpawnHeightAt(region, x, move.Rest.Y, z)
			if !ok {
				continue
			}
			admitted++
			if blocked := stuckAt(v, simulation.Spawn{RegionID: region, X: x, Y: y, Z: z}); blocked >= 6 {
				t.Fatalf("(%v,%v): a walked spawn stands at %.1f with %d/8 moves refused", x, z, y, blocked)
			}
		}
	}
	if strandedByAuthoredY == 0 {
		t.Fatal("fixture: no candidate here is stranded by authored-height grounding")
	}
	t.Logf("%d walked candidates admitted, %d would strand by authored height", admitted, strandedByAuthoredY)
}
