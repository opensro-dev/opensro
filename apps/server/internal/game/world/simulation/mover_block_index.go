/*
===========================================================================

mover_block_index.go - which movers stand in which interest block

The visibility rescan used to walk every mover of the viewer's candidate
regions, per viewer, per leg (#2134). A viewer's answer is the union of at
most nine interest blocks, and most movers stay in one block across many
ticks, so moverBlockIndex keeps that membership between scans instead.

moverStorage owns one index. set and remove only note the gid; the scan
reconciles under the same MonsterState.mu, so a burst of commits between
scans costs one recompute per gid. Time moves a mover only while its row
bears a segment, so an advancing clock recomputes only those.

===========================================================================
*/

package simulation

import worldgeom "opensro.online/server/internal/game/world"

// maxMoverBlockDirty bounds the notes held for a population nobody views.
// Past it the next scan rebuilds, which costs less than replaying them.
const maxMoverBlockDirty = 4096

/*
================
moverBlockIndex

generation advances whenever a mover changes block or leaves or enters
the index, so a memoized viewer answer stays valid across ticks in which
nothing crossed a block edge.
================
*/
type moverBlockIndex struct {
	built      bool
	dirty      []uint32
	nowMs      int64
	generation uint64

	blockOf  map[uint32]worldgeom.InterestBlock
	members  map[worldgeom.InterestBlock]map[uint32]struct{}
	dungeons map[uint16]map[uint32]struct{}
	segments map[uint32]struct{}

	// rosterGeneration advances when a row is added or removed: only
	// then can a static entry's "has no mover" answer change.
	rosterGeneration uint64
}

/*
================
moverBlockIndex.note

Record that gid's row changed. Called on every row write, so it only
appends.
================
*/
func (x *moverBlockIndex) note(gid uint32) {
	if !x.built {
		return
	}
	if len(x.dirty) >= maxMoverBlockDirty {
		x.built, x.dirty = false, x.dirty[:0]
		return
	}
	x.dirty = append(x.dirty, gid)
}

/*
================
moverBlockIndex.leave
================
*/
func (x *moverBlockIndex) leave(gid uint32) {
	block, ok := x.blockOf[gid]
	if !ok {
		return
	}
	delete(x.blockOf, gid)
	delete(x.segments, gid)
	x.generation++
	members := x.members[block]
	delete(members, gid)
	if len(members) == 0 {
		delete(x.members, block)
	}
	if block.Dungeon != 0 {
		dungeon := x.dungeons[block.Dungeon]
		delete(dungeon, gid)
		if len(dungeon) == 0 {
			delete(x.dungeons, block.Dungeon)
		}
	}
}

/*
================
moverBlockIndex.enter
================
*/
func (x *moverBlockIndex) enter(gid uint32, block worldgeom.InterestBlock) {
	x.blockOf[gid] = block
	x.generation++
	members := x.members[block]
	if members == nil {
		members = make(map[uint32]struct{})
		x.members[block] = members
	}
	members[gid] = struct{}{}
	if block.Dungeon != 0 {
		dungeon := x.dungeons[block.Dungeon]
		if dungeon == nil {
			dungeon = make(map[uint32]struct{})
			x.dungeons[block.Dungeon] = dungeon
		}
		dungeon[gid] = struct{}{}
	}
}

/*
================
moverStorage.placeInBlock

Put gid in the block of its live pose at nowMs. Gid 0 is the absent actor
and the scan never yields it.
================
*/
func (s *moverStorage) placeInBlock(gid uint32, nowMs int64) {
	x := &s.blocks
	if _, ok := s.slotOf(gid); !ok || gid == 0 {
		x.leave(gid)
		return
	}
	pose, _ := s.livePoseAt(gid, nowMs, nil)
	block := worldgeom.InterestBlockAt(worldgeom.RegionXZ{RegionID: pose.RegionID, X: pose.X, Z: pose.Z})
	if old, ok := x.blockOf[gid]; !ok {
		x.enter(gid, block)
	} else if old != block {
		x.leave(gid)
		x.enter(gid, block)
	}
	// leave drops the segment mark, so it is set after the move.
	if live := s.slots[s.byGID[gid]].rec.live; live != nil && live.ArriveMs > live.DepartMs {
		x.segments[gid] = struct{}{}
	} else {
		delete(x.segments, gid)
	}
}

/*
================
moverStorage.reconcileBlocks

Bring the index to nowMs. Clocks are not monotonic here: a query for an
earlier instant is exact too, because every row that time can move is
recomputed whenever the instant differs.
================
*/
func (s *moverStorage) reconcileBlocks(nowMs int64) {
	x := &s.blocks
	if !x.built {
		x.blockOf = make(map[uint32]worldgeom.InterestBlock, len(s.slots))
		x.members = make(map[worldgeom.InterestBlock]map[uint32]struct{})
		x.dungeons = make(map[uint16]map[uint32]struct{})
		x.segments = make(map[uint32]struct{})
		x.generation++
		x.built, x.nowMs = true, nowMs
		for i := range s.slots {
			s.placeInBlock(s.slots[i].gid, nowMs)
		}
		return
	}
	for _, gid := range x.dirty {
		s.placeInBlock(gid, nowMs)
	}
	x.dirty = x.dirty[:0]
	if x.nowMs == nowMs {
		return
	}
	x.nowMs = nowMs
	for gid := range x.segments {
		s.placeInBlock(gid, nowMs)
	}
}

/*
================
moverStorage.blockMembers

Every indexed mover in or neighbouring block, per
worldgeom.InterestBlock.Adjacent. The caller filters by actor existence
and sorts; the index is not ordered.
================
*/
func (s *moverStorage) blockMembers(block worldgeom.InterestBlock, out []uint32) []uint32 {
	x := &s.blocks
	if block.Whole {
		for gid := range x.dungeons[block.Dungeon] {
			out = append(out, gid)
		}
		return out
	}
	for dz := -1; dz <= 1; dz++ {
		for dx := -1; dx <= 1; dx++ {
			neighbour := worldgeom.InterestBlock{Dungeon: block.Dungeon, X: block.X + dx, Z: block.Z + dz}
			for gid := range x.members[neighbour] {
				out = append(out, gid)
			}
		}
	}
	if block.Dungeon != 0 {
		whole := worldgeom.InterestBlock{Dungeon: block.Dungeon, Whole: true}
		for gid := range x.members[whole] {
			out = append(out, gid)
		}
	}
	return out
}
