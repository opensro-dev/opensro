package world

import "testing"

func TestInterestBlockSelectsNative320UnitGrid(t *testing.T) {
	region := RegionIDForSectors(100, 97)
	for _, test := range []struct {
		x, z   float64
		bx, bz int
	}{
		{0, 0, 600, 582},
		{319.999, 319.999, 600, 582},
		{320, 0, 601, 582},
		{1919.9, 1919.9, 605, 587},
		// The reference server's grid fold never sees an unfolded local; the
		// continuous grid folds 1920 into
		// the next region's first block, exactly like NormalizeOutdoor would.
		{1920, 640, 606, 584},
	} {
		got := InterestBlockAt(RegionXZ{RegionID: region, X: test.x, Z: test.z})
		if got != (InterestBlock{X: test.bx, Z: test.bz}) {
			t.Fatalf("(%v,%v) -> %+v, want block (%d,%d)", test.x, test.z, got, test.bx, test.bz)
		}
	}
}

func TestInterestVisibilityIsTheNativeThreeByThreeBlockNeighbourhood(t *testing.T) {
	region := RegionIDForSectors(100, 97)
	east := RegionIDForSectors(101, 97)
	north := RegionIDForSectors(100, 98)
	at := func(regionID uint16, x, z float64) RegionXZ { return RegionXZ{RegionID: regionID, X: x, Z: z} }
	for _, test := range []struct {
		name           string
		viewer, object RegionXZ
		visible        bool
	}{
		{"same block", at(region, 10, 10), at(region, 300, 300), true},
		{"edge neighbour", at(region, 319, 100), at(region, 321, 100), true},
		{"diagonal neighbour", at(region, 10, 10), at(region, 630, 630), true},
		// The former 3x3-region ring made this same-region monster visible.
		{"two blocks away inside one region", at(region, 100, 100), at(region, 700, 100), false},
		{"knight's move", at(region, 10, 10), at(region, 650, 330), false},
		{"across the east seam", at(region, 1900, 100), at(east, 10, 100), true},
		{"two blocks across the east seam", at(region, 1600, 100), at(east, 330, 100), false},
		{"across the north seam", at(region, 100, 1900), at(north, 100, 300), true},
		{"far corner of the same region", at(region, 0, 0), at(region, 1919, 1919), false},
	} {
		if got := InterestVisible(test.viewer, test.object); got != test.visible {
			t.Fatalf("%s: visible=%v, want %v", test.name, got, test.visible)
		}
		if got := InterestVisible(test.object, test.viewer); got != test.visible {
			t.Fatalf("%s: relation is not symmetric", test.name)
		}
	}
}

func TestDungeonInterestRemainsOneRegionBlock(t *testing.T) {
	dungeon := DungeonRegionBit | 0x0102
	other := DungeonRegionBit | 0x0103
	outdoor := RegionIDForSectors(0x02, 0x01)
	if !InterestVisible(RegionXZ{RegionID: dungeon, X: -5000, Z: 0}, RegionXZ{RegionID: dungeon, X: 9000, Z: 700}) {
		t.Fatal("a dungeon region must remain one interest block until CRgnObject cells are recovered")
	}
	if InterestVisible(RegionXZ{RegionID: dungeon}, RegionXZ{RegionID: other}) {
		t.Fatal("distinct dungeon regions must not share interest")
	}
	if InterestVisible(RegionXZ{RegionID: dungeon, X: 10, Z: 10}, RegionXZ{RegionID: outdoor, X: 10, Z: 10}) {
		t.Fatal("the dungeon plane must never neighbour an outdoor block")
	}
}
