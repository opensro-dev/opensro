/*
===========================================================================

mover_interest_index.go - conservative live mover candidates for interest

Storage commits index the settled pose and every region crossed by the active
segment bounds. Viewer queries inspect local buckets without expanding dormant
movers. The population owner holds its mutex throughout updates and iteration.

===========================================================================
*/

package simulation

import (
	"iter"
	"math"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
moverInterestRegion

Continuous outdoor grid coordinates preserve original-frame local overflow.
Dungeon locals are unbounded, so their complete region word owns the bucket.
================
*/
type moverInterestRegion struct {
	dungeon uint16
	x, z    int
}

/*
================
moverInterestBounds

Pose remains indexed independently of the segment. The rectangle includes
interior crossings, and its conservative corners are filtered by LivePoseAt.
================
*/
type moverInterestBounds struct {
	pose, low, high moverInterestRegion
}

/*
================
moverInterestIndex
================
*/
type moverInterestIndex map[moverInterestRegion]map[uint32]struct{}

/*
================
moverInterestRegionAt
================
*/
func moverInterestRegionAt(position worldgeom.RegionXZ) moverInterestRegion {
	if worldgeom.IsDungeonRegion(position.RegionID) {
		return moverInterestRegion{dungeon: position.RegionID}
	}
	grid := worldgeom.ExpandGrid(position)
	return moverInterestRegion{
		x: int(math.Floor(grid.X / worldgeom.OutdoorRegionSize)),
		z: int(math.Floor(grid.Z / worldgeom.OutdoorRegionSize)),
	}
}

/*
================
moverInterestBoundsFor

Segment validity matches LivePoseAt, including elapsed segments whose To pose
remains authoritative until a later commit clears the movement channel.
================
*/
func moverInterestBoundsFor(row monster.MoverState) moverInterestBounds {
	pose := moverInterestRegionAt(worldgeom.RegionXZ{RegionID: row.Pose.RegionID, X: row.Pose.X, Z: row.Pose.Z})
	bounds := moverInterestBounds{pose: pose, low: pose, high: pose}
	if row.ArriveMs <= row.DepartMs {
		return bounds
	}
	from := moverInterestRegionAt(worldgeom.RegionXZ{RegionID: row.From.RegionID, X: row.From.X, Z: row.From.Z})
	to := moverInterestRegionAt(worldgeom.RegionXZ{RegionID: row.To.RegionID, X: row.To.X, Z: row.To.Z})
	bounds.low, bounds.high = from, to
	if from.dungeon == 0 && to.dungeon == 0 {
		bounds.low.x, bounds.high.x = min(from.x, to.x), max(from.x, to.x)
		bounds.low.z, bounds.high.z = min(from.z, to.z), max(from.z, to.z)
	}
	return bounds
}

/*
================
regions

Each region is yielded once, even when Pose overlaps the segment rectangle.
Dungeon transitions are admitted elsewhere; retain both endpoint regions.
================
*/
func (bounds moverInterestBounds) regions() iter.Seq[moverInterestRegion] {
	return func(yield func(moverInterestRegion) bool) {
		if !yield(bounds.pose) {
			return
		}
		if bounds.low.dungeon != 0 || bounds.high.dungeon != 0 {
			if bounds.low != bounds.pose && !yield(bounds.low) {
				return
			}
			if bounds.high != bounds.pose && bounds.high != bounds.low {
				yield(bounds.high)
			}
			return
		}
		for x := bounds.low.x; x <= bounds.high.x; x++ {
			for z := bounds.low.z; z <= bounds.high.z; z++ {
				region := moverInterestRegion{x: x, z: z}
				if region != bounds.pose && !yield(region) {
					return
				}
			}
		}
	}
}

/*
================
add
================
*/
func (index *moverInterestIndex) add(gid uint32, bounds moverInterestBounds) {
	if *index == nil {
		*index = make(moverInterestIndex)
	}
	for region := range bounds.regions() {
		if (*index)[region] == nil {
			(*index)[region] = make(map[uint32]struct{})
		}
		(*index)[region][gid] = struct{}{}
	}
}

/*
================
remove
================
*/
func (index moverInterestIndex) remove(gid uint32, bounds moverInterestBounds) {
	for region := range bounds.regions() {
		delete(index[region], gid)
		if len(index[region]) == 0 {
			delete(index, region)
		}
	}
}

/*
================
candidates

Visit only the viewer region and its immediate neighbours. Membership is a
broad phase: the caller must check actor existence/liveness and InterestVisible
against LivePoseAt(nowMs), never assume the segment bounds imply visibility.
Iteration neither expands records nor performs archive I/O. Storage and its
index must not change until iteration completes.
================
*/
func (s *moverStorage) candidates(viewer worldgeom.RegionXZ) iter.Seq[uint32] {
	return func(yield func(uint32) bool) {
		if s == nil {
			return
		}
		region := moverInterestRegionAt(viewer)
		bounds := moverInterestBounds{pose: region, low: region, high: region}
		if region.dungeon == 0 {
			bounds.low.x--
			bounds.low.z--
			bounds.high.x++
			bounds.high.z++
		}
		seen := make(map[uint32]struct{})
		for candidateRegion := range bounds.regions() {
			for gid := range s.spatial[candidateRegion] {
				if gid == 0 {
					continue
				}
				if _, duplicate := seen[gid]; duplicate {
					continue
				}
				seen[gid] = struct{}{}
				if !yield(gid) {
					return
				}
			}
		}
	}
}
