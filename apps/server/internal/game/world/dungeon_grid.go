/*
===========================================================================

dungeon_grid.go - the native dungeon message grid and its installed table

A dungeon region is cut into message blocks exactly as the outdoors is:
320-unit squares, one layer high. CRTNavMeshDungeon_MessageBlockAt
(v1.188 99A520) takes the DOF navigation bounds' origin, truncates
(position - origin) / 320 on x and z and / -3200000 on y (one layer for any
real height), and CRTNavMeshDungeon_NeighborMessageBlocks (99A600) lists
the blocks at -1..1 on each axis inside the grid. CRgnObject links each
block to those neighbours (538670 -> CMsgBlock_LinkNeighbor 537CB0), and
object admission and removal (534340 / 5344B0, shared with the outdoors)
notify a block and its linked neighbours. A dungeon viewer therefore holds
the 3x3 block neighbourhood, as outdoors, never the whole region.

This file owns the grid arithmetic and the one table of grids. The movement
package parses each dungeon's DOF bounds and installs the table once when
the GameWorld boots; every interest and activity query reads it here.
Without an installed grid (tests, a host without dungeon data) a dungeon
region stays one block, the behaviour before the grid was recovered.

===========================================================================
*/
package world

import (
	"encoding/binary"
	"fmt"
	"math"
	"sync/atomic"
)

const (
	// dungeonMessageBlockWidth is the x and z cell edge (qword 0xB45AE8).
	dungeonMessageBlockWidth = 320.0
	// dungeonMessageBlockHeight is the y divisor (qword 0xB45FA0): one layer.
	dungeonMessageBlockHeight = -3200000.0
	// dofMessageGridHeaderBytes covers the JMXVDOF 0101 header through the
	// bounds offset at byte 40 (998E76 loads it as navigation-host mode 8000).
	dofMessageGridHeaderBytes  = 44
	dofMessageGridBoundsOffset = 40
	dofMessageGridBoundsBytes  = 24
)

/*
================
DungeonMessageGrid

The DOF navigation bounds one dungeon's message grid is cut from: the first
bounding box at the header offset (998EA7 reads it into host+90..A4).
================
*/
type DungeonMessageGrid struct {
	Min, Max [3]float64
}

/*
================
ParseDungeonMessageGrid
================
*/
func ParseDungeonMessageGrid(raw []byte) (DungeonMessageGrid, error) {
	var g DungeonMessageGrid
	if len(raw) < dofMessageGridHeaderBytes || string(raw[:12]) != "JMXVDOF 0101" {
		return g, fmt.Errorf("invalid DOF message-grid header")
	}
	offset := uint64(binary.LittleEndian.Uint32(raw[dofMessageGridBoundsOffset:dofMessageGridHeaderBytes]))
	if offset+dofMessageGridBoundsBytes > uint64(len(raw)) {
		return g, fmt.Errorf("truncated DOF message-grid bounds")
	}
	for i := 0; i < 6; i++ {
		v := float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[int(offset)+i*4:])))
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return g, fmt.Errorf("nonfinite DOF message-grid bounds")
		}
		if i < 3 {
			g.Min[i] = v
		} else {
			g.Max[i-3] = v
		}
	}
	for i := range g.Min {
		if g.Max[i] < g.Min[i] {
			return g, fmt.Errorf("inverted DOF message-grid bounds")
		}
	}
	return g, nil
}

/*
================
DungeonMessageGrid.cell

One axis of 99A520: float32 position and difference, truncated quotient.
A position outside the bounds has no block.
================
*/
func (g DungeonMessageGrid) cell(axis int, v, width float64) (int, bool) {
	v = float64(float32(v))
	if math.IsNaN(v) || math.IsInf(v, 0) || v < g.Min[axis] || v > g.Max[axis] {
		return 0, false
	}
	return int(float64(float32(v-g.Min[axis])) / width), true
}

/*
================
DungeonMessageGrid.BlockAt

The message block holding a dungeon position (99A520).
================
*/
func (g DungeonMessageGrid) BlockAt(regionID uint16, x, y, z float64) (MessageBlock, bool) {
	cx, okX := g.cell(0, x, dungeonMessageBlockWidth)
	cy, okY := g.cell(1, y, dungeonMessageBlockHeight)
	cz, okZ := g.cell(2, z, dungeonMessageBlockWidth)
	if !okX || !okY || !okZ {
		return MessageBlock{}, false
	}
	return MessageBlock{Region: regionID, X: cx, Y: cy, Z: cz}, true
}

/*
================
DungeonMessageGrid.planarBlock

The x/z cell of a position known only in the plane. Interest positions
carry no height, and the one-layer y division makes every in-bounds height
the same layer, so only x and z decide the block.
================
*/
func (g DungeonMessageGrid) planarBlock(x, z float64) (int, int, bool) {
	cx, okX := g.cell(0, x, dungeonMessageBlockWidth)
	cz, okZ := g.cell(2, z, dungeonMessageBlockWidth)
	return cx, cz, okX && okZ
}

// dungeonMessageGrids is the installed table, keyed by dungeon region id.
// It is written once at GameWorld boot and only read afterwards.
var dungeonMessageGrids atomic.Pointer[map[uint16]DungeonMessageGrid]

/*
================
InstallDungeonMessageGrids

Replaces the grid table. The GameWorld wiring calls it once, before the
first tick, with every resident dungeon's grid; tests install their own and
restore the previous table with the returned function.
================
*/
func InstallDungeonMessageGrids(grids map[uint16]DungeonMessageGrid) (restore func()) {
	table := make(map[uint16]DungeonMessageGrid, len(grids))
	for region, grid := range grids {
		table[region] = grid
	}
	previous := dungeonMessageGrids.Swap(&table)
	return func() { dungeonMessageGrids.Store(previous) }
}

/*
================
dungeonMessageGridFor
================
*/
func dungeonMessageGridFor(regionID uint16) (DungeonMessageGrid, bool) {
	table := dungeonMessageGrids.Load()
	if table == nil {
		return DungeonMessageGrid{}, false
	}
	grid, ok := (*table)[regionID]
	return grid, ok
}
