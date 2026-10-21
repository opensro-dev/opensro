/*
===========================================================================

dungeon_grid_test.go - the dungeon message grid and dungeon interest blocks

===========================================================================
*/
package world

import (
	"encoding/binary"
	"math"
	"testing"
)

/*
================
dofWithBounds
================
*/
func dofWithBounds(bounds [6]float32) []byte {
	raw := make([]byte, 128)
	copy(raw, "JMXVDOF 0101")
	binary.LittleEndian.PutUint32(raw[40:], 64)
	for i, v := range bounds {
		binary.LittleEndian.PutUint32(raw[64+i*4:], math.Float32bits(v))
	}
	return raw
}

/*
================
TestDungeonMessageGridUsesAuthoredOriginAndOneLayer

99A520: (position - origin) / 320 on x and z, / -3200000 on y.
================
*/
func TestDungeonMessageGridUsesAuthoredOriginAndOneLayer(t *testing.T) {
	g, err := ParseDungeonMessageGrid(dofWithBounds([6]float32{-500, -4000000, 125, 1100, 4000000, 1725}))
	if err != nil {
		t.Fatal(err)
	}
	b, ok := g.BlockAt(0x8001, -180, 0, 445)
	if !ok || b != (MessageBlock{Region: 0x8001, X: 1, Y: -1, Z: 1}) {
		t.Fatalf("wrong grid projection: %+v %v", b, ok)
	}
	if b, ok = g.BlockAt(0x8001, -180, -3000000, 445); !ok || b.Y != 0 {
		t.Fatalf("a height within one 3200000 slab of the origin is layer 0: %+v %v", b, ok)
	}
	if b, ok = g.BlockAt(0x8001, -180.01, 0, 445); !ok || b.X != 0 {
		t.Fatalf("wrong seam side: %+v", b)
	}
	if _, ok = g.BlockAt(0x8001, -501, 0, 445); ok {
		t.Fatal("outside bounds admitted")
	}
	if _, err = ParseDungeonMessageGrid(dofWithBounds([6]float32{0, 0, 0, -1, 0, 0})); err == nil {
		t.Fatal("inverted bounds admitted")
	}
}

/*
================
TestDungeonInterestIsTheBlockNeighbourhood

With a grid installed a dungeon viewer holds its 3x3 block neighbourhood
(538670 links each block to 99A600's neighbours), never the whole region.
================
*/
func TestDungeonInterestIsTheBlockNeighbourhood(t *testing.T) {
	restore := InstallDungeonMessageGrids(map[uint16]DungeonMessageGrid{
		0x8001: {Min: [3]float64{0, -100, 0}, Max: [3]float64{3200, 100, 3200}},
	})
	defer restore()
	at := func(x, z float64) RegionXZ { return RegionXZ{RegionID: 0x8001, X: x, Z: z} }
	cases := []struct {
		name           string
		viewer, object RegionXZ
		visible        bool
	}{
		{"same block", at(10, 10), at(300, 300), true},
		{"neighbour block", at(330, 330), at(10, 10), true},
		{"diagonal neighbour", at(650, 650), at(330, 330), true},
		{"two blocks away", at(10, 10), at(650, 10), false},
		{"far side of the cave", at(10, 10), at(3000, 3000), false},
		{"another dungeon", at(10, 10), RegionXZ{RegionID: 0x8002, X: 10, Z: 10}, false},
		{"outdoors", at(10, 10), RegionXZ{RegionID: 0x6e4a, X: 10, Z: 10}, false},
	}
	for _, c := range cases {
		if got := InterestVisible(c.viewer, c.object); got != c.visible {
			t.Errorf("%s: visible %v, want %v", c.name, got, c.visible)
		}
		if got := InterestVisible(c.object, c.viewer); got != c.visible {
			t.Errorf("%s (reversed): visible %v, want %v", c.name, got, c.visible)
		}
	}
}

/*
================
TestDungeonWithoutGridStaysOneBlock

No installed grid, or a position outside it, keeps the whole region: data
that is missing never hides an object.
================
*/
func TestDungeonWithoutGridStaysOneBlock(t *testing.T) {
	restore := InstallDungeonMessageGrids(map[uint16]DungeonMessageGrid{
		0x8001: {Min: [3]float64{0, -100, 0}, Max: [3]float64{640, 100, 640}},
	})
	defer restore()
	if !InterestVisible(RegionXZ{RegionID: 0x8002, X: 10, Z: 10}, RegionXZ{RegionID: 0x8002, X: 9000, Z: 9000}) {
		t.Error("a dungeon without a grid must stay one block")
	}
	if !InterestVisible(RegionXZ{RegionID: 0x8001, X: 10, Z: 10}, RegionXZ{RegionID: 0x8001, X: 5000, Z: 5000}) {
		t.Error("a position outside the grid must keep the whole region")
	}
}
