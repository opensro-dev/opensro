/*
===========================================================================

performance_ring_plane_test.go - legacy-parity tests for the audit's
plane reads in performance_ring.go (#1646, #1765)

ringWraps and the candidate-centre expansion used to test the dungeon
region bit themselves; they now ask simulation.PlaneKindOf and let
simulation.RegionScopeRing scope a region. The legacy copies below are
the pre-move bodies verbatim. Dropping the cave guard from ringWraps
or the expansion must fail here.

===========================================================================
*/
package main

import (
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
legacyRingWraps

The pre-#1765 body of ringWraps.
================
*/
func legacyRingWraps(center uint16) bool {
	if simulation.IsDungeonRegion(center) {
		return false
	}
	sx, sy := simulation.SectorX(center), simulation.SectorY(center)
	return sx == 0 || sx == 0xff || sy == 0 || sy == 0xff
}

/*
================
legacyRingCandidates

The pre-#1765 candidate-centre expansion from runPerformanceRing.
================
*/
func legacyRingCandidates(spawnRegions map[uint16]bool) map[uint16]bool {
	candidates := map[uint16]bool{}
	for regionID := range spawnRegions {
		candidates[regionID] = true
		if simulation.IsDungeonRegion(regionID) {
			continue
		}
		sx, sy := simulation.SectorX(regionID), simulation.SectorY(regionID)
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				candidates[simulation.RegionIDForSectors(sx+dx, sy+dy)] = true
			}
		}
	}
	return candidates
}

/*
================
TestRingWrapsAndCandidatesLegacyParity

The edge test and the candidate set agree with the legacy bit read on
field regions (interior and sector-edge) and dungeon regions alike,
and the expansion still scopes a dungeon region to itself.
================
*/
func TestRingWrapsAndCandidatesLegacyParity(t *testing.T) {
	centers := []uint16{0x62A8, 0x0000, 0x00FF, 0xFF00, 0x6E4A, 0x8001, 0xFFFF}
	caveCenters := 0
	for _, center := range centers {
		if got, want := ringWraps(center), legacyRingWraps(center); got != want {
			t.Errorf("center 0x%04X: wraps %v, legacy %v", center, got, want)
		}
		spawnRegions := map[uint16]bool{center: true}
		got, want := ringCandidates(spawnRegions), legacyRingCandidates(spawnRegions)
		if len(got) != len(want) {
			t.Errorf("center 0x%04X: %d candidates, legacy %d", center, len(got), len(want))
			continue
		}
		for regionID := range want {
			if !got[regionID] {
				t.Errorf("center 0x%04X: candidate 0x%04X dropped", center, regionID)
			}
		}
		if simulation.PlaneKindOf(center) == simulation.PlaneCave {
			caveCenters++
			if len(got) != 1 || !got[center] {
				t.Errorf("center 0x%04X: dungeon candidate set %v, want self only", center, got)
			}
		}
	}
	if caveCenters == 0 {
		t.Fatal("cave arm unexercised")
	}
}
