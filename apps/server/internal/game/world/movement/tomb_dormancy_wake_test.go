/*
===========================================================================

tomb_dormancy_wake_test.go - how much of Ch'in Tomb Floor 3 one player wakes

Cave monsters sleep by the native 320-unit message block and a player wakes
the blocks within simulation.CaveWakeBlocks of their own (monster_dormancy.go).
Floor 3 (region 0x8005) is the densest cave floor: 6,866 slots. Before caves
slept by block, the first player to arrive would have woken all of them in
one tick. This test fills the floor on the real cave geometry, installs the
production message grids, and counts what a player standing at any camp
wakes.

===========================================================================
*/

package movement

import (
	"sort"
	"testing"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/tier"
)

// tombWakeMaxShare is the largest share of the floor one arrival may wake:
// the block square must cut the floor's wake down by at least this much.
const tombWakeMaxPercent = 15

/*
================
TestTombFloor3ArrivalWakesItsBlockSquare

Every Floor 3 monster stands on a message block (so it can sleep at all),
and a player standing on any monster's block wakes only the monsters in the
CaveWakeBlocks square around it.
================
*/
func TestTombFloor3ArrivalWakesItsBlockSquare(t *testing.T) {
	tier.Long(t, "Floor 3 of the Ch'in Tomb wakes one block square per arrival on the real cave geometry")
	template, _ := floor3Template(t, 0)
	s, v := fillFloor3State(t, template)
	t.Cleanup(worldgeom.InstallDungeonMessageGrids(v.DungeonMessageGrids()))

	perBlock := make(map[[2]int]int)
	total := 0
	for _, instance := range s.MaterializedInstances("tomb") {
		if instance.Spawn.RegionID != tombFloor3Region {
			continue
		}
		block := worldgeom.InterestBlockAt(worldgeom.RegionXZ{RegionID: instance.Spawn.RegionID, X: instance.Spawn.X, Z: instance.Spawn.Z})
		if block.Whole {
			t.Fatalf("Floor 3 monster at (%.0f, %.0f) is on no message block and could never sleep", instance.Spawn.X, instance.Spawn.Z)
		}
		perBlock[[2]int{block.X, block.Z}]++
		total++
	}
	if total == 0 {
		t.Fatal("Floor 3 did not fill")
	}

	var woken []int
	for at := range perBlock {
		count := 0
		for dz := -simulation.CaveWakeBlocks; dz <= simulation.CaveWakeBlocks; dz++ {
			for dx := -simulation.CaveWakeBlocks; dx <= simulation.CaveWakeBlocks; dx++ {
				count += perBlock[[2]int{at[0] + dx, at[1] + dz}]
			}
		}
		woken = append(woken, count)
	}
	sort.Ints(woken)
	worst, median := woken[len(woken)-1], woken[len(woken)/2]
	t.Logf("Floor 3: %d monsters on %d blocks; one arrival wakes median %d, worst %d (whole floor %d)", total, len(perBlock), median, worst, total)
	if worst*100 > total*tombWakeMaxPercent {
		t.Errorf("the worst arrival wakes %d of %d monsters, over %d%%", worst, total, tombWakeMaxPercent)
	}
}
