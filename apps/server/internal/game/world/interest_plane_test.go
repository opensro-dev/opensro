/*
===========================================================================

interest_plane_test.go - legacy-parity tests for the interest block
plane branch (#1646, #1765)

InterestBlockAt used to live in interest.go with the whole plane branch
inline; it moved verbatim into the plane owner (geometry.go) and
OutdoorMessageBlock now trusts its dungeon tag instead of testing the
region bit. The legacy copies below are the pre-move bodies verbatim.
Inverting the branch in the moved owner must fail the dungeon cases.

===========================================================================
*/
package world

import (
	"math"
	"testing"
)

/*
================
legacyInterestBlockAt

The pre-#1765 body of InterestBlockAt, from interest.go.
================
*/
func legacyInterestBlockAt(position RegionXZ) InterestBlock {
	if IsDungeonRegion(position.RegionID) {
		grid, ok := dungeonMessageGridFor(position.RegionID)
		if !ok {
			return InterestBlock{Dungeon: position.RegionID, Whole: true}
		}
		x, z, inside := grid.planarBlock(position.X, position.Z)
		if !inside {
			return InterestBlock{Dungeon: position.RegionID, Whole: true}
		}
		return InterestBlock{Dungeon: position.RegionID, X: x, Z: z}
	}
	grid := ExpandGrid(position)
	return InterestBlock{
		X: int(math.Floor(grid.X / InterestBlockSize)),
		Z: int(math.Floor(grid.Z / InterestBlockSize)),
	}
}

/*
================
legacyOutdoorMessageBlock

The pre-#1765 body of OutdoorMessageBlock, gate and all.
================
*/
func legacyOutdoorMessageBlock(position RegionXZ) (MessageBlock, bool) {
	if IsDungeonRegion(position.RegionID) {
		return MessageBlock{}, false
	}
	b := legacyInterestBlockAt(position)
	return MessageBlock{X: b.X, Z: b.Z}, true
}

/*
================
TestInterestBlocksLegacyParity

Outdoor points fold into the expanded grid, gridded dungeon points
take the grid's planar block, ungridded dungeon points and points
outside the grid keep the whole region - and the outdoor-only door
refuses every dungeon point - identically on both bodies.
================
*/
func TestInterestBlocksLegacyParity(t *testing.T) {
	restore := InstallDungeonMessageGrids(map[uint16]DungeonMessageGrid{
		0x8001: {Min: [3]float64{0, -100, 0}, Max: [3]float64{3200, 100, 3200}},
	})
	defer restore()

	cases := []struct {
		name       string
		position   RegionXZ
		dungeonArm bool
	}{
		{"outdoor origin", RegionXZ{RegionID: 0x6E4A, X: 0, Z: 0}, false},
		{"outdoor near corner", RegionXZ{RegionID: 0x6E4A, X: 1919, Z: 1919}, false},
		{"outdoor mid", RegionXZ{RegionID: 0x6E4A, X: 640, Z: 1280}, false},
		{"outdoor other region", RegionXZ{RegionID: 0x62A8, X: 100, Z: 200}, false},
		{"gridded dungeon block", RegionXZ{RegionID: 0x8001, X: 10, Z: 10}, true},
		{"gridded dungeon later block", RegionXZ{RegionID: 0x8001, X: 1000, Z: 2500}, true},
		{"dungeon outside grid", RegionXZ{RegionID: 0x8001, X: 5000, Z: 10}, true},
		{"dungeon without grid", RegionXZ{RegionID: 0x8002, X: 10, Z: 10}, true},
	}
	dungeonArms, outdoorOK := 0, 0
	for _, tc := range cases {
		if got, want := InterestBlockAt(tc.position), legacyInterestBlockAt(tc.position); got != want {
			t.Errorf("%s: block %+v, legacy %+v", tc.name, got, want)
		}
		gotBlock, gotOK := OutdoorMessageBlock(tc.position)
		wantBlock, wantOK := legacyOutdoorMessageBlock(tc.position)
		if gotBlock != wantBlock || gotOK != wantOK {
			t.Errorf("%s: outdoor block (%+v, %v), legacy (%+v, %v)", tc.name, gotBlock, gotOK, wantBlock, wantOK)
		}
		if tc.dungeonArm {
			dungeonArms++
		}
		if gotOK {
			outdoorOK++
		}
	}
	if dungeonArms == 0 || outdoorOK == 0 {
		t.Fatalf("arms unexercised: dungeon %d, outdoor-ok %d", dungeonArms, outdoorOK)
	}
}
