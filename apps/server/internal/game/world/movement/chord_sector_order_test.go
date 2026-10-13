/*
===========================================================================

chord_sector_order_test.go - a tied placement wins under one identity

A placement reachable through two surface identities (a multi-region
bundle and the neighbour's own bundle) ties exactly with itself in the
chord scans. The scans keep the first candidate, so the sectors' order
decides the winner; it is the walker's order (chordSectors), never a Go
map's, and so the same on every call.

===========================================================================
*/
package movement

import (
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestTiedPlacementWinsUnderOneIdentity

A Mengzi border clip whose rest stands on such a placement (object 40):
before chordSectors its owner alternated between {26958 DX 0} and
{26959 DX -1} run to run. A fresh validator per round, so no cache
carries the answer over.
================
*/
func TestTiedPlacementWinsUnderOneIdentity(t *testing.T) {
	from := simulation.Spawn{RegionID: 0x694F, X: 20, Z: 260}
	to := simulation.Spawn{RegionID: 0x694F, X: -40, Z: 260}
	var first simulation.NavOwner
	for round := 0; round < 12; round++ {
		v := realAuthorityValidator(t)
		y, ok := v.TerrainHeightAt(from.RegionID, from.X, from.Z)
		if !ok {
			t.Fatal("fixture: no ground at the Mengzi border point")
		}
		from.Y, to.Y = y, y
		got := v.ClipMovementPathFrom(from, simulation.NavOwner{}, to).RestOwner
		if got.Kind != simulation.NavOwnerObject || got.Object.Object != 40 {
			t.Fatalf("round %d: rest owner %+v, want object 40", round, got)
		}
		if round == 0 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("round %d: rest owner %+v, round 0 had %+v", round, got, first)
		}
	}
}
