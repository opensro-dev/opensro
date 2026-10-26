package world

// Native outdoor interest area. The v1.150 client never culls server objects,
// so the interest radius is reference-server evidence,
// machine-verified:
//
//   - A region terrain builds 36 message blocks (count 0x24,
//     stride 0x98).
//   - The block lookup bounds local x and z by 1920.0, divides
//     both by 320.0 and selects block (z/320)*6 + x/320.
//   - The link step registers the eight neighbours, (-1,-1)..(1,1)
//     without (0,0), through the neighbour resolution that steps
//     one region across a seam.
//   - Admission and removal notify that neighbour list and the
//     block itself; an adjacent transfer notifies only the
//     per-neighbour difference list.
//
// An object is therefore visible exactly when its block lies in the 3x3 block
// neighbourhood of the viewer's block.
const (
	// InterestBlockSize is one message-block edge in world units.
	InterestBlockSize = 320.0
	// InterestBlocksPerRegion is the per-axis block count of one outdoor region.
	InterestBlocksPerRegion = 6
)

/*
================
InterestBlock

One native message block. Outdoor blocks are addressed in the continuous
block grid (sector*6 + local/320), which removes region seams from
neighbour arithmetic the way the native neighbour resolution does. Dungeon blocks are cells of the
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
InterestBlock.Adjacent

Whether other is block or one of its eight neighbours (the native
link step outdoors, the dungeon object link inside). Dungeon and outdoor blocks never neighbour; a Whole
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
