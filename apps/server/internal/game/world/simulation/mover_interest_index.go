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
//
// Each entry records whether its mover is listed in more than one region
// (a segment that crosses a boundary): only those can reach a scan twice.
type moverInterestIndex map[moverInterestRegion]map[uint32]bool

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
	regions := 0
	for range bounds.regions() {
		regions++
	}
	for region := range bounds.regions() {
		if (*index)[region] == nil {
			(*index)[region] = make(map[uint32]bool)
		}
		(*index)[region][gid] = regions > 1
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
moverCandidateRegions

The viewer's region and, outdoors, its eight neighbours: the regions whose
movers candidates and the population interest scan visit.
================
*/
func moverCandidateRegions(viewer worldgeom.RegionXZ) moverInterestBounds {
	region := moverInterestRegionAt(viewer)
	bounds := moverInterestBounds{pose: region, low: region, high: region}
	if region.dungeon == 0 {
		bounds.low.x--
		bounds.low.z--
		bounds.high.x++
		bounds.high.z++
	}
	return bounds
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
		seen := make(map[uint32]struct{})
		for candidateRegion := range moverCandidateRegions(viewer).regions() {
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

const maxOutdoorReachSectorBuckets = 64

/*
================
outdoorReachSectorWindow
================
*/
func outdoorReachSectorWindow(center Spawn, reach float64) (lowX, highX, lowZ, highZ int, ok bool) {
	if worldgeom.IsDungeonRegion(center.RegionID) || reach < 0 || math.IsNaN(reach) || math.IsInf(reach, 0) {
		return 0, 0, 0, 0, false
	}
	grid := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: center.RegionID, X: center.X, Z: center.Z})
	if math.IsNaN(grid.X) || math.IsInf(grid.X, 0) || math.IsNaN(grid.Z) || math.IsInf(grid.Z, 0) {
		return 0, 0, 0, 0, false
	}
	pad := reach + 1.0
	if pad > float64(maxOutdoorReachSectorBuckets)*worldgeom.OutdoorRegionSize {
		return 0, 0, 0, 0, false
	}
	lowX = int(math.Floor((grid.X - pad) / worldgeom.OutdoorRegionSize))
	highX = int(math.Floor((grid.X + pad) / worldgeom.OutdoorRegionSize))
	lowZ = int(math.Floor((grid.Z - pad) / worldgeom.OutdoorRegionSize))
	highZ = int(math.Floor((grid.Z + pad) / worldgeom.OutdoorRegionSize))
	spanX := highX - lowX + 1
	spanZ := highZ - lowZ + 1
	if spanX <= 0 || spanZ <= 0 || spanX > maxOutdoorReachSectorBuckets || spanZ > maxOutdoorReachSectorBuckets || spanX*spanZ > maxOutdoorReachSectorBuckets {
		return 0, 0, 0, 0, false
	}
	return lowX, highX, lowZ, highZ, true
}

/*
================
livePosesWithinOutdoorReach

Visits movers whose live pose at nowMs can lie within reach of an outdoor
center, falling back to the full table when center is in a dungeon or the
sector window exceeds maxOutdoorReachSectorBuckets. Only movers flagged as
spanning multiple regions in a multi-sector window consult deduplication.
================
*/
func (s *moverStorage) livePosesWithinOutdoorReach(center Spawn, reach float64, nowMs int64, ground monster.GroundResolver) iter.Seq2[uint32, monster.Pose] {
	return func(yield func(uint32, monster.Pose) bool) {
		if s == nil {
			return
		}
		lowX, highX, lowZ, highZ, ok := outdoorReachSectorWindow(center, reach)
		if !ok {
			for _, slot := range s.slots {
				if !yield(slot.gid, slot.rec.livePoseAt(nowMs, ground)) {
					return
				}
			}
			return
		}
		multiSector := lowX != highX || lowZ != highZ
		var seenStack [16]uint32
		seenCount := 0
		var seenMap map[uint32]struct{}
		for x := lowX; x <= highX; x++ {
			for z := lowZ; z <= highZ; z++ {
				bucket := s.spatial[moverInterestRegion{x: x, z: z}]
				for gid, spans := range bucket {
					if gid == 0 {
						continue
					}
					if multiSector && spans {
						duplicate := false
						for i := 0; i < seenCount; i++ {
							if seenStack[i] == gid {
								duplicate = true
								break
							}
						}
						if !duplicate && seenMap != nil {
							_, duplicate = seenMap[gid]
						}
						if duplicate {
							continue
						}
						if seenCount < len(seenStack) {
							seenStack[seenCount] = gid
							seenCount++
						} else {
							if seenMap == nil {
								seenMap = make(map[uint32]struct{})
							}
							seenMap[gid] = struct{}{}
						}
					}
					slot, exists := s.slotOf(gid)
					if !exists {
						continue
					}
					if !yield(gid, s.slots[slot].rec.livePoseAt(nowMs, ground)) {
						return
					}
				}
			}
		}
	}
}
