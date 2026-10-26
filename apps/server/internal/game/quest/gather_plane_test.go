/*
===========================================================================

gather_plane_test.go - legacy-parity tests for the quest gather plane
gate in gather.go (#1646, #1765)

withinQuestArea used to test the dungeon region bit itself; the gate
now reads the plane's feature row
(simulation.PlaneFeatures.QuestGatherAreas). The legacy copy below is
the pre-move body verbatim. Turning the cave column's row on must make
the cave case here pass where the legacy refuses it.

===========================================================================
*/
package quest

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
legacyWithinQuestArea

The pre-#1765 body of withinQuestArea, with the region bit as the gate.
================
*/
func legacyWithinQuestArea(at, center simulation.Spawn, radius float64) bool {
	distance := simulation.WorldDistance2D(at, center)
	return !simulation.IsDungeonRegion(at.RegionID) && !math.IsNaN(distance) && !math.IsInf(distance, 0) && distance < radius
}

/*
================
TestWithinQuestAreaLegacyParity

Outdoor points inside the radius gather, outdoor points beyond it and
every cave point do not - identically on the old bit gate and the new
feature gate.
================
*/
func TestWithinQuestAreaLegacyParity(t *testing.T) {
	centre := simulation.Spawn{RegionID: 0x60A0, X: 253, Y: 85, Z: 449}
	caveA := simulation.Spawn{RegionID: 0x8001, X: 10, Y: 0, Z: 10}
	caveB := simulation.Spawn{RegionID: 0x8001, X: 11, Y: 0, Z: 10}

	cases := []struct {
		name       string
		at, centre simulation.Spawn
		radius     float64
		caveArm    bool
	}{
		{"at the centre", centre, centre, 500, false},
		{"inside the radius", simulation.Spawn{RegionID: 0x60A0, X: 300, Y: 85, Z: 449}, centre, 500, false},
		{"beyond the radius", simulation.Spawn{RegionID: 0x60A0, X: 1800, Y: 85, Z: 449}, centre, 500, false},
		{"zero radius", centre, centre, 0, false},
		{"cave point at gather distance", caveA, caveB, 500, true},
		{"cave point at wider radius", caveA, caveB, 5000, true},
	}
	caveArms, gathered := 0, 0
	for _, tc := range cases {
		legacyGot := legacyWithinQuestArea(tc.at, tc.centre, tc.radius)
		got := withinQuestArea(tc.at, tc.centre, tc.radius)
		if got != legacyGot {
			t.Errorf("%s: within=%v, legacy %v", tc.name, got, legacyGot)
		}
		if tc.caveArm {
			caveArms++
		}
		if got {
			gathered++
		}
	}
	if caveArms == 0 || gathered == 0 {
		t.Fatalf("arms unexercised: cave gates %d, gathers %d", caveArms, gathered)
	}
}
