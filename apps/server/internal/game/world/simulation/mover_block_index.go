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

blockGeneration advances per block whenever a mover enters or leaves
that block, and dungeonGeneration per dungeon on the same events, so a
memoized viewer answer keys on the blocks its own neighbourhood reads
(#2134): a crossing in a far neighbourhood no longer retires every
viewer's stamp. The counters only advance, never reset - a rebuild
re-enters every mover, which advances them again.
================
*/
type moverBlockIndex struct {
	built bool
	dirty []uint32
	nowMs int64

	blockGeneration   map[worldgeom.InterestBlock]uint64
	dungeonGeneration map[uint16]uint64
	// membership advances on every enter/leave; it versions stampCache.
	membership uint64
	stampCache map[worldgeom.InterestBlock]moverBlockStamp

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
moverBlockIndex.bumpBlock

Advance the generations one membership change under block moves: the
block's own, and its dungeon's when it has one.
================
*/
func (x *moverBlockIndex) bumpBlock(block worldgeom.InterestBlock) {
	if x.blockGeneration == nil {
		x.blockGeneration = make(map[worldgeom.InterestBlock]uint64)
		x.dungeonGeneration = make(map[uint16]uint64)
	}
	x.blockGeneration[block]++
	x.membership++
	if block.Dungeon != 0 {
		x.dungeonGeneration[block.Dungeon]++
	}
}

/*
================
moverBlockStamp

A cached stampBlocks mix and the membership count it was folded
under. Generations change only through bumpBlock, which advances
membership, so a matching membership proves the mix is current.
================
*/
type moverBlockStamp struct {
	membership uint64
	stamp      uint64
}

/*
================
moverBlockIndex.stampBlocks

The blocks input of a viewer answer for block, as one positional mix
of the generations of the blocks whose membership decides it: the 3x3
neighbourhood, plus the Whole block for a dungeon block (Whole movers
neighbour every cell of their dungeon). A Whole block's answer is the
whole dungeon, so it keys on the dungeon-wide generation alone.

A single changed generation provably changes the mix: each step
xor-folds the next generation in, then multiplies by an odd
constant - both bijections on uint64. Several generations changing
at once collide with probability about 2^-64, the width of the
stamp. The mix is folded once per
block per membership change and cached in stampCache: a leg's many
viewers mostly share blocks, and re-folding nine map reads per
viewer per leg cost more than the leg it was meant to cheapen.
================
*/
func (x *moverBlockIndex) stampBlocks(block worldgeom.InterestBlock) uint64 {
	if cached, ok := x.stampCache[block]; ok && cached.membership == x.membership {
		return cached.stamp
	}
	var stamp uint64
	if block.Whole {
		stamp = x.dungeonGeneration[block.Dungeon]
	} else {
		stamp = 1469598103934665603
		fold := func(block worldgeom.InterestBlock) {
			stamp = (stamp ^ (x.blockGeneration[block] + 1)) * 1099511628211
		}
		for dz := -1; dz <= 1; dz++ {
			for dx := -1; dx <= 1; dx++ {
				fold(worldgeom.InterestBlock{Dungeon: block.Dungeon, X: block.X + dx, Z: block.Z + dz})
			}
		}
		if block.Dungeon != 0 {
			fold(worldgeom.InterestBlock{Dungeon: block.Dungeon, Whole: true})
		}
	}
	if x.stampCache == nil {
		x.stampCache = make(map[worldgeom.InterestBlock]moverBlockStamp)
	}
	x.stampCache[block] = moverBlockStamp{membership: x.membership, stamp: stamp}
	return stamp
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
	x.bumpBlock(block)
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
	x.bumpBlock(block)
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
and the scan never yields it. The slot is resolved once and read for both
the pose and the segment mark: a moving actor is placed every leg (#2133).
================
*/
func (s *moverStorage) placeInBlock(gid uint32, nowMs int64) {
	x := &s.blocks
	slot, ok := s.slotOf(gid)
	if !ok || gid == 0 {
		x.leave(gid)
		return
	}
	rec := s.slots[slot].rec
	pose := rec.livePoseAt(nowMs, nil)
	block := worldgeom.InterestBlockAt(worldgeom.RegionXZ{RegionID: pose.RegionID, X: pose.X, Z: pose.Z})
	if old, ok := x.blockOf[gid]; !ok {
		x.enter(gid, block)
	} else if old != block {
		x.leave(gid)
		x.enter(gid, block)
	}
	// leave drops the segment mark, so it is set after the move.
	if live := rec.live; live != nil && live.ArriveMs > live.DepartMs {
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
		// A rebuild discards the old membership instead of leaving
		// it, so bump every block it held first: a mover that crossed
		// during the dirty overflow would otherwise leave its old
		// block's generation - and every viewer stamp keyed on it -
		// standing still (#2134 review).
		for block := range x.members {
			x.bumpBlock(block)
		}
		x.blockOf = make(map[uint32]worldgeom.InterestBlock, len(s.slots))
		x.members = make(map[worldgeom.InterestBlock]map[uint32]struct{})
		x.dungeons = make(map[uint16]map[uint32]struct{})
		x.segments = make(map[uint32]struct{})
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
