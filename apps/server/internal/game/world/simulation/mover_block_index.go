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
segment, and a segment row is re-placed at its precomputed
block-crossing instant, not on every tick (#2988). Generations and
membership live in one flat open-addressed table indexed by the
packed block coordinate, and membership per block is a gid slice the
slot addresses by position, so neither the stamp a viewer keys on nor
the member set a scan enumerates passes through a keyed map on the
per-leg path.

===========================================================================
*/

package simulation

import (
	"math"

	worldgeom "opensro.online/server/internal/game/world"
)

// maxMoverBlockDirty bounds the notes held for a population nobody views.
// Past it the next scan rebuilds, which costs less than replaying them.
const maxMoverBlockDirty = 4096

/*
================
moverBlockIndex

A block's generation advances whenever a mover enters or leaves that
block, and dungeonGeneration per dungeon on the same events, so a
memoized viewer answer keys on the blocks its own neighbourhood reads
(#2134): a crossing in a far neighbourhood no longer retires every
viewer's stamp. The counters only advance, never reset - a rebuild
re-enters every mover, which advances them again.

Generations and membership share one flat open-addressed table
indexed by the packed block coordinate (packBlockKey): keys holds
the packed key (biased by one, so zero is the empty slot), blocks
the block it came from, gens its generation and lists its member
list in blockLists. Entries are insert-only - a generation must
never reset, or a stamp could repeat - so the table needs no
tombstones and membership leave is a swap-remove in the block's own
slice, addressed by the slot's memberPos.
================
*/
type moverBlockIndex struct {
	built bool
	dirty []blockNote
	nowMs int64

	keys   []uint64
	blocks []worldgeom.InterestBlock
	gens   []uint64
	lists  []int32
	used   int

	blockLists [][]uint32

	dungeonGeneration map[uint16]uint64

	// Per-slot membership, parallel to moverStorage.slots and moved
	// with its rows by moveSlot: blockBySlot/inBlockBySlot say where
	// a slot's row stands, segBySlot marks the rows time can move,
	// placeDueMs is the instant a segment row can next change block,
	// and memberPos is the row's position in its block's list.
	blockBySlot   []worldgeom.InterestBlock
	inBlockBySlot []bool
	segBySlot     []bool
	placeDueMs    []int64
	memberPos     []int32
	dungeons      map[uint16]map[uint32]struct{}

	// Segment rows with a finite crossing instant, as a min-heap
	// over placeDueMs addressed by duePos (-1: not in the heap).
	// The reconcile drains the rows whose instant has passed
	// instead of scanning every slot to find them (#2988).
	dueHeap    []uint32
	duePos     []int32
	dueScratch []uint32

	// rosterGeneration advances when a row is added or removed: only
	// then can a static entry's "has no mover" answer change.
	rosterGeneration uint64
}

/*
================
packBlockKey

One block as one integer: dungeon above, the Whole bit under it,
then the two cell coordinates at 22 bits each - far past any grid
the world builds (1536 outdoor blocks per axis, dungeon cells in
the hundreds). Biased by one so the zero key stays the table's
empty slot. Injective over every reachable block, which is all the
table asks of it.
================
*/
func packBlockKey(block worldgeom.InterestBlock) uint64 {
	key := uint64(block.Dungeon) << 45
	if block.Whole {
		key |= 1 << 44
	}
	key |= uint64(uint32(block.X)) << 22
	key |= uint64(uint32(block.Z))
	return key + 1
}

/*
================
blockKeyHash

splitmix64's finalizer: one multiply-xorshift round pair, enough to
spread packed coordinates (which cluster on grid multiples) over a
power-of-two table.
================
*/
func blockKeyHash(key uint64) uint64 {
	key ^= key >> 30
	key *= 0xBF58476D1CE4E5B9
	key ^= key >> 27
	key *= 0x94D049BB133111EB
	key ^= key >> 31
	return key
}

/*
================
moverBlockIndex.find

The table slot of block, or -1. Linear probing over the packed key;
the table never holds a tombstone, so an empty slot ends the walk.
================
*/
func (x *moverBlockIndex) find(block worldgeom.InterestBlock) int {
	if len(x.keys) == 0 {
		return -1
	}
	key := packBlockKey(block)
	mask := uint64(len(x.keys) - 1)
	at := blockKeyHash(key) & mask
	for {
		stored := x.keys[at]
		if stored == 0 {
			return -1
		}
		if stored == key {
			return int(at)
		}
		at = (at + 1) & mask
	}
}

/*
================
moverBlockIndex.intern

find, inserting block with a zero generation and no member list
when it is new. Grows the table first when the load would pass
three quarters, so the probe in find stays short.
================
*/
func (x *moverBlockIndex) intern(block worldgeom.InterestBlock) int {
	if len(x.keys) == 0 || (x.used+1)*4 > len(x.keys)*3 {
		x.growTable()
	}
	key := packBlockKey(block)
	mask := uint64(len(x.keys) - 1)
	at := blockKeyHash(key) & mask
	for {
		stored := x.keys[at]
		if stored == key {
			return int(at)
		}
		if stored == 0 {
			x.keys[at] = key
			x.blocks[at] = block
			x.gens[at] = 0
			x.lists[at] = -1
			x.used++
			return int(at)
		}
		at = (at + 1) & mask
	}
}

/*
================
moverBlockIndex.growTable

Double the table (1024 first) and rehash by the stored keys. The
blocks, generations and list indices move with their key; the
member lists themselves are untouched.
================
*/
func (x *moverBlockIndex) growTable() {
	size := 1024
	if len(x.keys) > 0 {
		size = len(x.keys) * 2
	}
	keys := make([]uint64, size)
	blocks := make([]worldgeom.InterestBlock, size)
	gens := make([]uint64, size)
	lists := make([]int32, size)
	mask := uint64(size - 1)
	for i, stored := range x.keys {
		if stored == 0 {
			continue
		}
		at := blockKeyHash(stored) & mask
		for keys[at] != 0 {
			at = (at + 1) & mask
		}
		keys[at] = stored
		blocks[at] = x.blocks[i]
		gens[at] = x.gens[i]
		lists[at] = x.lists[i]
	}
	x.keys, x.blocks, x.gens, x.lists = keys, blocks, gens, lists
}

/*
================
moverBlockIndex.generationOf

The block's generation, zero when no mover ever entered it.
================
*/
func (x *moverBlockIndex) generationOf(block worldgeom.InterestBlock) uint64 {
	if at := x.find(block); at >= 0 {
		return x.gens[at]
	}
	return 0
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
		x.placeDueMs = append(x.placeDueMs, 0)
		x.memberPos = append(x.memberPos, -1)
		x.duePos = append(x.duePos, -1)
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
			x.placeDueMs[slot] = x.placeDueMs[last]
			x.memberPos[slot] = x.memberPos[last]
			x.duePos[slot] = x.duePos[last]
			if x.duePos[slot] >= 0 {
				x.dueHeap[x.duePos[slot]] = slot
			}
		} else if int(slot) < len(x.blockBySlot) {
			x.blockBySlot[slot] = worldgeom.InterestBlock{}
			x.inBlockBySlot[slot] = false
			x.segBySlot[slot] = false
			x.placeDueMs[slot] = 0
			x.memberPos[slot] = -1
			x.duePos[slot] = -1
		}
	}
	if int(last) < len(x.blockBySlot) {
		x.blockBySlot = x.blockBySlot[:last]
		x.inBlockBySlot = x.inBlockBySlot[:last]
		x.segBySlot = x.segBySlot[:last]
		x.placeDueMs = x.placeDueMs[:last]
		x.memberPos = x.memberPos[:last]
		x.duePos = x.duePos[:last]
	}
}

/*
================
moverBlockIndex.bumpBlock

Advance the generations one membership change under block moves: the
block's own, and its dungeon's when it has one.
================
*/
func (x *moverBlockIndex) bumpBlock(block worldgeom.InterestBlock) int {
	at := x.intern(block)
	x.gens[at]++
	if block.Dungeon != 0 {
		if x.dungeonGeneration == nil {
			x.dungeonGeneration = make(map[uint16]uint64)
		}
		x.dungeonGeneration[block.Dungeon]++
	}
	return at
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
stamp. The mix folds straight from the flat table on every read
(#2988): nine probes of packed integers cost less than the keyed
cache that used to stand here, whose single global membership
version retired every viewer's cached mix whenever any mover
crossed any block.
================
*/
func (x *moverBlockIndex) stampBlocks(block worldgeom.InterestBlock) uint64 {
	if block.Whole {
		return x.dungeonGeneration[block.Dungeon]
	}
	stamp := uint64(1469598103934665603)
	for dz := -1; dz <= 1; dz++ {
		for dx := -1; dx <= 1; dx++ {
			neighbour := worldgeom.InterestBlock{Dungeon: block.Dungeon, X: block.X + dx, Z: block.Z + dz}
			stamp = (stamp ^ (x.generationOf(neighbour) + 1)) * 1099511628211
		}
	}
	if block.Dungeon != 0 {
		whole := worldgeom.InterestBlock{Dungeon: block.Dungeon, Whole: true}
		stamp = (stamp ^ (x.generationOf(whole) + 1)) * 1099511628211
	}
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
func (x *moverBlockIndex) leaveSlot(slot, gid uint32, slotOf func(uint32) (uint32, bool)) {
	if !x.built || int(slot) >= len(x.blockBySlot) || !x.inBlockBySlot[slot] {
		return
	}
	block := x.blockBySlot[slot]
	x.inBlockBySlot[slot] = false
	x.segBySlot[slot] = false
	x.dueRemove(slot)
	x.placeDueMs[slot] = 0
	x.bumpBlock(block)
	// The row leaves its block's list by position: the last gid
	// takes its place and that row's position follows it.
	if at := x.find(block); at >= 0 {
		if list := x.lists[at]; list >= 0 {
			members := x.blockLists[list]
			pos := x.memberPos[slot]
			last := members[len(members)-1]
			members[pos] = last
			x.blockLists[list] = members[:len(members)-1]
			if last != gid {
				if moved, ok := slotOf(last); ok {
					x.memberPos[moved] = pos
				}
			}
		}
	}
	x.memberPos[slot] = -1
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
	at := x.bumpBlock(block)
	list := x.lists[at]
	if list < 0 {
		list = int32(len(x.blockLists))
		x.blockLists = append(x.blockLists, nil)
		x.lists[at] = list
	}
	x.memberPos[slot] = int32(len(x.blockLists[list]))
	x.blockLists[list] = append(x.blockLists[list], gid)
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
moverBlockIndex.dueSiftUp
================
*/
func (x *moverBlockIndex) dueSiftUp(at int) {
	for at > 0 {
		parent := (at - 1) / 2
		slot, other := x.dueHeap[at], x.dueHeap[parent]
		if x.placeDueMs[other] <= x.placeDueMs[slot] {
			return
		}
		x.dueHeap[at], x.dueHeap[parent] = other, slot
		x.duePos[slot], x.duePos[other] = int32(parent), int32(at)
		at = parent
	}
}

/*
================
moverBlockIndex.dueSiftDown
================
*/
func (x *moverBlockIndex) dueSiftDown(at int) {
	for {
		child := at*2 + 1
		if child >= len(x.dueHeap) {
			return
		}
		if child+1 < len(x.dueHeap) && x.placeDueMs[x.dueHeap[child+1]] < x.placeDueMs[x.dueHeap[child]] {
			child++
		}
		slot, other := x.dueHeap[at], x.dueHeap[child]
		if x.placeDueMs[slot] <= x.placeDueMs[other] {
			return
		}
		x.dueHeap[at], x.dueHeap[child] = other, slot
		x.duePos[slot], x.duePos[other] = int32(child), int32(at)
		at = child
	}
}

/*
================
moverBlockIndex.dueRemove

Take slot out of the due heap; a no-op when it is not in it.
================
*/
func (x *moverBlockIndex) dueRemove(slot uint32) {
	at := x.duePos[slot]
	if at < 0 {
		return
	}
	last := x.dueHeap[len(x.dueHeap)-1]
	x.dueHeap = x.dueHeap[:len(x.dueHeap)-1]
	x.duePos[slot] = -1
	if last != slot {
		x.dueHeap[at] = last
		x.duePos[last] = at
		x.dueSiftDown(int(at))
		x.dueSiftUp(int(x.duePos[last]))
	}
}

/*
================
moverBlockIndex.dueTrack

Bring slot's heap entry in line with its placeDueMs after a
placement: a finite instant is in the heap under that key, a
settled row is out of it. Placement order between rows cannot
change an answer - enter and leave only advance generations,
which commute, and member order is sorted away by the caller.
================
*/
func (x *moverBlockIndex) dueTrack(slot uint32) {
	if x.placeDueMs[slot] == math.MaxInt64 {
		x.dueRemove(slot)
		return
	}
	if at := x.duePos[slot]; at >= 0 {
		x.dueSiftUp(int(at))
		x.dueSiftDown(int(x.duePos[slot]))
		return
	}
	x.duePos[slot] = int32(len(x.dueHeap))
	x.dueHeap = append(x.dueHeap, slot)
	x.dueSiftUp(len(x.dueHeap) - 1)
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
		x.leaveSlot(slot, gid, s.slotOf)
		x.enter(slot, gid, block)
	}
	// leave drops the segment mark, so it is set after the move.
	seg := rec.live != nil && rec.live.ArriveMs > rec.live.DepartMs
	x.segBySlot[slot] = seg
	if seg {
		x.placeDueMs[slot] = segmentBlockDue(rec, block, nowMs)
	} else {
		x.placeDueMs[slot] = math.MaxInt64
	}
	x.dueTrack(slot)
}

/*
================
segmentBlockDue

The first instant a segment row can change block, one tick early.
The placement itself stays the oracle: reconcile re-places a row
whose due has passed and recomputes the block from the pose, so an
early due costs one confirming placement and the due can never run
late - the exit is bisected on LivePoseAt's own arithmetic, not
re-derived. A segment that ends inside its block is due at its
arrival (the settle check), one that has not started at its
departure, a settled row never. Segments that cross between the
outdoor plane and a dungeon, or between two dungeons, keep the
per-tick placement: their pose path is not one straight line in
one block plane.
================
*/
func segmentBlockDue(rec moverRecord, block worldgeom.InterestBlock, nowMs int64) int64 {
	live := rec.live
	if live == nil || live.ArriveMs <= live.DepartMs || nowMs >= live.ArriveMs {
		return math.MaxInt64
	}
	if !worldgeom.SameFloor(live.From.RegionID, live.To.RegionID) {
		return nowMs
	}
	if nowMs < live.DepartMs {
		return live.DepartMs
	}
	end := live.LivePoseAt(live.ArriveMs, nil)
	if worldgeom.InterestBlockAt(worldgeom.RegionXZ{RegionID: end.RegionID, X: end.X, Z: end.Z}) == block {
		return live.ArriveMs
	}
	// The pose path is one straight line in the block's plane, so
	// the in-block instants form one interval: bisect its end.
	lo, hi := nowMs, live.ArriveMs
	for hi-lo > 1 {
		mid := lo + (hi-lo)/2
		pose := rec.livePoseAt(mid, nil)
		if worldgeom.InterestBlockAt(worldgeom.RegionXZ{RegionID: pose.RegionID, X: pose.X, Z: pose.Z}) == block {
			lo = mid
		} else {
			hi = mid
		}
	}
	due := lo - 100
	if due < nowMs {
		due = nowMs
	}
	return due
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
		for at, stored := range x.keys {
			if stored != 0 && x.lists[at] >= 0 && len(x.blockLists[x.lists[at]]) > 0 {
				x.bumpBlock(x.blocks[at])
			}
		}
		x.blockBySlot, x.inBlockBySlot, x.segBySlot = nil, nil, nil
		x.placeDueMs, x.memberPos = nil, nil
		x.dueHeap, x.duePos = nil, nil
		x.blockLists = nil
		for at := range x.lists {
			x.lists[at] = -1
		}
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
	// A backward query stays exact the way it always was - every
	// segment row re-placed. Forward, a segment row is re-placed
	// only once its precomputed block crossing can have passed;
	// between crossings its block provably stands. The due rows
	// are drained from the heap before any placement, so a row is
	// placed at most once per reconcile however its new instant
	// lands - the scan this replaces had the same bound.
	if nowMs < x.nowMs {
		x.nowMs = nowMs
		for slot := range x.segBySlot {
			if x.segBySlot[slot] {
				s.placeInBlockAt(uint32(slot), s.slots[slot].gid, nowMs)
			}
		}
		return
	}
	x.nowMs = nowMs
	x.dueScratch = x.dueScratch[:0]
	for len(x.dueHeap) > 0 && x.placeDueMs[x.dueHeap[0]] <= nowMs {
		slot := x.dueHeap[0]
		x.dueRemove(slot)
		x.dueScratch = append(x.dueScratch, slot)
	}
	for _, slot := range x.dueScratch {
		s.placeInBlockAt(slot, s.slots[slot].gid, nowMs)
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
			if at := x.find(neighbour); at >= 0 {
				if list := x.lists[at]; list >= 0 {
					out = append(out, x.blockLists[list]...)
				}
			}
		}
	}
	if block.Dungeon != 0 {
		whole := worldgeom.InterestBlock{Dungeon: block.Dungeon, Whole: true}
		if at := x.find(whole); at >= 0 {
			if list := x.lists[at]; list >= 0 {
				out = append(out, x.blockLists[list]...)
			}
		}
	}
	return out
}
