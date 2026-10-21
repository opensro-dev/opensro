package world

import "math"

// Native outdoor interest area. The v1.150 client never culls server objects,
// so the interest radius is server evidence (v1.188 SR_GameServer,
// machine-verified):
//
//   - CRgnTerrain 53A850 builds 36 CMsgBlockTerrain blocks (vector
//     constructor 53A8A1: count 0x24, stride 0x98).
//   - 53AD30 bounds local x and z by 1920.0 (qword 0xB45AD0), divides both by
//     320.0 (qword 0xB45AE8) and selects block (z/320)*6 + x/320.
//   - 53AB90 registers the eight neighbours at C64090, (-1,-1)..(1,1) without
//     (0,0), through 53AC20, which steps one region across a seam.
//   - Admission 534340 and removal 5344B0 notify that neighbour list and the
//     block itself; an adjacent transfer notifies only the per-neighbour
//     difference list keyed at block+0x1C (5336E0).
//
// An object is therefore visible exactly when its block lies in the 3x3 block
// neighbourhood of the viewer's block.
const (
	// InterestBlockSize is one CMsgBlockTerrain edge in world units.
	InterestBlockSize = 320.0
	// InterestBlocksPerRegion is the per-axis block count of one outdoor region.
	InterestBlocksPerRegion = 6
)

/*
================
InterestBlock

One native message block. Outdoor blocks are addressed in the continuous
block grid (sector*6 + local/320), which removes region seams from
neighbour arithmetic the way 53AC20 does. Dungeon blocks are cells of the
region's DOF message grid (dungeon_grid.go), the same 320-unit squares
measured from the grid origin.

A dungeon position the installed grid cannot place (no grid for the region,
or outside its bounds) is Whole: the whole region, as before the grid was
recovered, so an object is never hidden by missing data.
================
*/
type InterestBlock struct {
	// Dungeon is the dungeon region word; zero for outdoor blocks.
	Dungeon uint16
	// Whole marks a dungeon position without a grid cell.
	Whole bool
	// X and Z are continuous outdoor block-grid coordinates, or a dungeon
	// grid cell.
	X, Z int
}

/*
================
InterestBlockAt

The message block containing position.
================
*/
func InterestBlockAt(position RegionXZ) InterestBlock {
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
InterestBlock.Adjacent

Whether other is block or one of its eight neighbours (53AB90 outdoors,
538670 in a dungeon). Dungeon and outdoor blocks never neighbour; a Whole
dungeon block neighbours every block of its region.
================
*/
func (block InterestBlock) Adjacent(other InterestBlock) bool {
	if block.Dungeon != other.Dungeon {
		return false
	}
	if block.Whole || other.Whole {
		return true
	}
	return abs(block.X-other.X) <= 1 && abs(block.Z-other.Z) <= 1
}

// InterestVisible reports whether an object at object is inside the native
// interest area of a viewer at viewer. The relation is symmetric.
func InterestVisible(viewer, object RegionXZ) bool {
	return InterestBlockAt(viewer).Adjacent(InterestBlockAt(object))
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
