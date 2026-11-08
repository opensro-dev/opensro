/*
===========================================================================

mover_block_index.go - which movers stand in which interest block

The visibility rescan used to walk every mover of the viewer's candidate
regions, per viewer, per leg (#2134). A viewer's answer is the union of at
most nine interest blocks, and most movers stay in one block across many
ticks, so moverBlockIndex keeps that membership between scans instead.

moverStorage owns one index. set notes the (slot, gid) pair its write
landed on; remove leaves the index at once and moves the swapped row's
entry with its slot. The scan reconciles under the same
MonsterState.mu, so a burst of commits between scans costs one
recompute per gid. Time moves a mover only while its row bears a
segment, so an advancing clock recomputes only those. Membership by
block is keyed by slot (#2133): the per-leg placement reads arrays,
not gid maps; only the viewer-facing member sets stay gid-keyed,
because a scan enumerates gids.

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
	dirty []blockNote
	nowMs int64

	blockGeneration   map[worldgeom.InterestBlock]uint64
	dungeonGeneration map[uint16]uint64
	// membership advances on every enter/leave; it versions stampCache.
	membership uint64
	stampCache map[worldgeom.InterestBlock]moverBlockStamp

	// Per-slot membership, parallel to moverStorage.slots and moved
	// with its rows by moveSlot: blockBySlot/inBlockBySlot say where
	// a slot's row stands, segBySlot marks the rows time can move.
	blockBySlot   []worldgeom.InterestBlock
	inBlockBySlot []bool
	segBySlot     []bool
	members       map[worldgeom.InterestBlock]map[uint32]struct{}
	dungeons      map[uint16]map[uint32]struct{}

	// rosterGeneration advances when a row is added or removed: only
	// then can a static entry's "has no mover" answer change.
	rosterGeneration uint64
}

/*
================
blockNote

One noted row write: the slot the write landed on and the gid it
named. The slot is a hint by reconcile time (a remove may have
swapped another row into it); placeInBlockAt verifies it.
================
*/
type blockNote struct {
	slot uint32
	gid  uint32
}

/*
================
moverBlockIndex.note

Record that the row at slot changed. Called on every row write, so
it only appends.
================
*/
func (x *moverBlockIndex) note(slot, gid uint32) {
	if !x.built {
		return
	}
	if len(x.dirty) >= maxMoverBlockDirty {
		x.built, x.dirty = false, x.dirty[:0]
		return
	}
	x.dirty = append(x.dirty, blockNote{slot, gid})
}

/*
================
moverBlockIndex.growSlot

Size the per-slot membership arrays to hold slot.
================
*/
func (x *moverBlockIndex) growSlot(slot uint32) {
	for uint32(len(x.blockBySlot)) <= slot {
		x.blockBySlot = append(x.blockBySlot, worldgeom.InterestBlock{})
		x.inBlockBySlot = append(x.inBlockBySlot, false)
		x.segBySlot = append(x.segBySlot, false)
	}
}

/*
================
moverBlockIndex.moveSlot

Follow moverStorage.remove's swap: the last slot's row moves into
slot, and its index entries move with it; the arrays then shrink
with the slots. The departed row's own entry was already dropped
by leaveSlot. A slot past the arrays was never placed, so it moves
as empty.
================
*/
func (x *moverBlockIndex) moveSlot(slot, last uint32) {
	if !x.built {
		return
	}
	if slot != last {
		if int(last) < len(x.blockBySlot) {
			x.blockBySlot[slot] = x.blockBySlot[last]
			x.inBlockBySlot[slot] = x.inBlockBySlot[last]
			x.segBySlot[slot] = x.segBySlot[last]
		} else if int(slot) < len(x.blockBySlot) {
			x.blockBySlot[slot] = worldgeom.InterestBlock{}
			x.inBlockBySlot[slot] = false
			x.segBySlot[slot] = false
		}
	}
	if int(last) < len(x.blockBySlot) {
		x.blockBySlot = x.blockBySlot[:last]
		x.inBlockBySlot = x.inBlockBySlot[:last]
		x.segBySlot = x.segBySlot[:last]
	}
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
moverBlockIndex.leaveSlot

Drop slot's row from the index. The departed row leaves at remove
time (its slot is gone by reconcile), a crossing leaves at place
time; both read the block from the slot's own entry.
================
*/
func (x *moverBlockIndex) leaveSlot(slot, gid uint32) {
	if !x.built || int(slot) >= len(x.blockBySlot) || !x.inBlockBySlot[slot] {
		return
	}
	block := x.blockBySlot[slot]
	x.inBlockBySlot[slot] = false
	x.segBySlot[slot] = false
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
func (x *moverBlockIndex) enter(slot, gid uint32, block worldgeom.InterestBlock) {
	x.growSlot(slot)
	x.blockBySlot[slot] = block
	x.inBlockBySlot[slot] = true
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
moverStorage.placeInBlockAt

Put the row at slot in the block of its live pose at nowMs. The
slot is the note's hint, verified against the gid before use: a
remove may have swapped another row into it since the note, and
a departed row already left the index at remove time, so a stale
hint resolves by gid and a departed one places nothing. The pose,
the block compare and the segment mark all read the slot's own
entries: a moving actor is placed every leg (#2133).
================
*/
func (s *moverStorage) placeInBlockAt(slot, gid uint32, nowMs int64) {
	x := &s.blocks
	if gid == 0 {
		return
	}
	if int(slot) >= len(s.slots) || s.slots[slot].gid != gid {
		resolved, ok := s.slotOf(gid)
		if !ok {
			return
		}
		slot = resolved
	}
	rec := s.slots[slot].rec
	pose := rec.livePoseAt(nowMs, nil)
	block := worldgeom.InterestBlockAt(worldgeom.RegionXZ{RegionID: pose.RegionID, X: pose.X, Z: pose.Z})
	if int(slot) >= len(x.blockBySlot) || !x.inBlockBySlot[slot] {
		x.enter(slot, gid, block)
	} else if x.blockBySlot[slot] != block {
		x.leaveSlot(slot, gid)
		x.enter(slot, gid, block)
	}
	// leave drops the segment mark, so it is set after the move.
	x.segBySlot[slot] = rec.live != nil && rec.live.ArriveMs > rec.live.DepartMs
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
		x.blockBySlot, x.inBlockBySlot, x.segBySlot = nil, nil, nil
		x.members = make(map[worldgeom.InterestBlock]map[uint32]struct{})
		x.dungeons = make(map[uint16]map[uint32]struct{})
		x.built, x.nowMs = true, nowMs
		for i := range s.slots {
			s.placeInBlockAt(uint32(i), s.slots[i].gid, nowMs)
		}
		return
	}
	for _, n := range x.dirty {
		s.placeInBlockAt(n.slot, n.gid, nowMs)
	}
	x.dirty = x.dirty[:0]
	if x.nowMs == nowMs {
		return
	}
	x.nowMs = nowMs
	for slot := range x.segBySlot {
		if x.segBySlot[slot] {
			s.placeInBlockAt(uint32(slot), s.slots[slot].gid, nowMs)
		}
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
