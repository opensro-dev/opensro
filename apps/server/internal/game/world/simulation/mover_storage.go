/*
===========================================================================

mover_storage.go - compact mover records and their spatial candidate owner

===========================================================================
*/

package simulation

import (
	"iter"
	"math"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"sort"
	"unique"
)

// Only the population owner writes these records. Pending values discard only
// completed navigation caches; expanding a snapshot never creates a new actor.
/*
================
moverRecord
================
*/
type moverRecord struct {
	live    *monster.MoverState
	pending *residentPendingMover
}

// Positions and activity clocks remain per actor. All other immutable pending
// fields commonly repeat across thousands of actors; handles share those values
// without sharing mutable state. Float speed bits preserve signed zero exactly.
/*
================
pendingMoverShared
================
*/
type pendingMoverShared struct {
	row       monster.PendingMover
	speedBits uint64
}

/*
================
residentPendingMover
================
*/
type residentPendingMover struct {
	pose     monster.Pose
	activity monster.ActivityCadence
	shared   unique.Handle[pendingMoverShared]
}

/*
================
set
================
*/
func (p *residentPendingMover) set(row monster.PendingMover) {
	p.pose, p.activity = row.Pose, row.Activity
	shared := pendingMoverShared{row: row, speedBits: math.Float64bits(row.NavigationSpeed)}
	shared.row.Pose = monster.Pose{}
	shared.row.Activity = monster.ActivityCadence{}
	shared.row.NavigationSpeed = 0
	if p.shared == (unique.Handle[pendingMoverShared]{}) || p.shared.Value() != shared {
		p.shared = unique.Make(shared)
	}
}

/*
================
value
================
*/
func (p *residentPendingMover) value() monster.MoverState {
	shared := p.shared.Value()
	row := shared.row
	row.Pose, row.Activity = p.pose, p.activity
	row.NavigationSpeed = math.Float64frombits(shared.speedBits)
	return row.Expand()
}

/*
================
moverSlot

One mover row in dense slot order. The slot is the row's index in
moverStorage.slots; byGID is the only map left, and it maps a gid to
its slot. Slot order is insertion order except that remove swaps the
last row into the hole, so a walk sees an order that is stable only
within one mutation-free span: consumers that need an order sort or
count (monstersquad does both), they never inherit one.
================
*/
type moverSlot struct {
	gid uint32
	rec moverRecord
}

/*
================
moverStorage
================
*/
type moverStorage struct {
	slots   []moverSlot
	byGID   map[uint32]uint32
	spatial moverInterestIndex

	// blocks keeps interest-block membership between scans
	// (mover_block_index.go).
	blocks moverBlockIndex
}

/*
================
slotOf

The dense slot of gid, when the storage holds one.
================
*/
func (s *moverStorage) slotOf(gid uint32) (uint32, bool) {
	slot, ok := s.byGID[gid]
	return slot, ok
}

/*
================
newMoverStorage

Seeds in ascending gid order, so a fresh storage's slot order is the
same on every build; runtime additions append in arrival order.
================
*/
func newMoverStorage(rows map[uint32]monster.MoverState) *moverStorage {
	gids := make([]uint32, 0, len(rows))
	for gid := range rows {
		gids = append(gids, gid)
	}
	sort.Slice(gids, func(i, j int) bool { return gids[i] < gids[j] })
	s := &moverStorage{byGID: make(map[uint32]uint32, len(rows))}
	for _, gid := range gids {
		s.set(gid, rows[gid])
	}
	return s
}

/*
================
set
================
*/
func (s *moverStorage) set(gid uint32, row monster.MoverState) {
	if s.byGID == nil {
		s.byGID = make(map[uint32]uint32)
	}
	s.blocks.note(gid)
	slot, exists := s.slotOf(gid)
	if !exists {
		s.blocks.rosterGeneration++
		slot = uint32(len(s.slots))
		s.slots = append(s.slots, moverSlot{gid: gid})
		s.byGID[gid] = slot
	}
	old := s.slots[slot].rec
	after := moverInterestBoundsFor(row)
	if !exists {
		s.spatial.add(gid, after)
	} else if before := old.interestBounds(); before != after {
		s.spatial.remove(gid, before)
		s.spatial.add(gid, after)
	}
	if p, ok := row.PendingSnapshot(); ok {
		if old.pending != nil {
			old.pending.set(p)
			return
		}
		resident := &residentPendingMover{}
		resident.set(p)
		s.slots[slot].rec = moverRecord{pending: resident}
		return
	}
	if old.live != nil {
		*old.live = row
		return
	}
	live := new(monster.MoverState)
	*live = row
	s.slots[slot].rec = moverRecord{live: live}
}

/*
================
moverRecord.interestBounds

moverInterestBoundsFor(r.value()) without expanding a pending row:
PendingSnapshot admits only DepartMs == ArriveMs == 0, so a pending row is
never in flight and its bounds are its pose region alone.
================
*/
func (r moverRecord) interestBounds() moverInterestBounds {
	if r.live != nil {
		return moverInterestBoundsFor(*r.live)
	}
	if r.pending != nil {
		pose := moverInterestRegionAt(worldgeom.RegionXZ{
			RegionID: r.pending.pose.RegionID,
			X:        r.pending.pose.X,
			Z:        r.pending.pose.Z,
		})
		return moverInterestBounds{pose: pose, low: pose, high: pose}
	}
	return moverInterestBoundsFor(monster.MoverState{})
}

/*
================
value
================
*/
func (r moverRecord) value() monster.MoverState {
	if r.live != nil {
		return *r.live
	}
	if r.pending != nil {
		return r.pending.value()
	}
	return monster.MoverState{}
}

/*
================
lookup
================
*/
func (s *moverStorage) lookup(gid uint32) (monster.MoverState, bool) {
	if s == nil {
		return monster.MoverState{}, false
	}
	slot, ok := s.slotOf(gid)
	if !ok {
		return monster.MoverState{}, false
	}
	return s.slots[slot].rec.value(), true
}

/*
================
setActivity

Stamps only the activity cadence of an existing row, in place. The
interest bounds never read the cadence (moverInterestBoundsFor) and
a pending row's shared handle excludes it, so the stored value
afterwards equals a full set of a row that differs only there.
commitActivity's cadence-only commits (#2136) still arm from the new
stamp; the row is not rebuilt for a clock tick. No-op when the row
is gone: the caller's guard has just seen it.
================
*/
func (s *moverStorage) setActivity(gid uint32, activity monster.ActivityCadence) {
	if s == nil {
		return
	}
	slot, ok := s.slotOf(gid)
	if !ok {
		return
	}
	rec := &s.slots[slot].rec
	if rec.live != nil {
		rec.live.Activity = activity
		return
	}
	if rec.pending != nil {
		rec.pending.activity = activity
	}
}

/*
================
lookupAt

lookup through a slot carried from the schedule read, verified
against the gid before use. remove swaps the last row into the
hole, so a carried slot can name another actor by dispatch time;
the caller falls back to lookup on a miss (#2135). No fallback
here: the miss is the signal.
================
*/
func (s *moverStorage) lookupAt(slot uint32, gid uint32) (monster.MoverState, bool) {
	if s == nil || int(slot) >= len(s.slots) || s.slots[slot].gid != gid {
		return monster.MoverState{}, false
	}
	return s.slots[slot].rec.value(), true
}

/*
================
has

Whether gid has a row, without building its MoverState.
================
*/
func (s *moverStorage) has(gid uint32) bool {
	if s == nil {
		return false
	}
	_, ok := s.slotOf(gid)
	return ok
}

/*
================
livePoseAt

lookup(gid).LivePoseAt(nowMs, ground) without expanding a pending row. A
pending row is exact as stored: PendingSnapshot admits only DepartMs ==
ArriveMs == 0, where LivePoseAt neither interpolates nor resolves ground and
returns Pose. Visibility reads one pose per candidate per viewer per tick,
so this keeps them off the full expansion.
================
*/
func (s *moverStorage) livePoseAt(gid uint32, nowMs int64, ground monster.GroundResolver) (monster.Pose, bool) {
	if s == nil {
		return monster.Pose{}, false
	}
	slot, ok := s.slotOf(gid)
	if !ok {
		return monster.Pose{}, false
	}
	return s.slots[slot].rec.livePoseAt(nowMs, ground), true
}

/*
================
livePoses

Every row's livePoseAt, in slot order: a population scan that reads
only where each mover stands, without expanding a single row.
================
*/
func (s *moverStorage) livePoses(nowMs int64, ground monster.GroundResolver) iter.Seq2[uint32, monster.Pose] {
	return func(yield func(uint32, monster.Pose) bool) {
		for gid, r := range s.records() {
			if !yield(gid, r.livePoseAt(nowMs, ground)) {
				return
			}
		}
	}
}

/*
================
moverRecord.livePoseAt

r.value().LivePoseAt(nowMs, ground) without expanding a pending row. A
pending row is exact as stored: PendingSnapshot admits only DepartMs ==
ArriveMs == 0, where LivePoseAt neither interpolates nor resolves ground and
returns Pose.
================
*/
func (r moverRecord) livePoseAt(nowMs int64, ground monster.GroundResolver) monster.Pose {
	switch {
	case r.live != nil:
		return r.live.LivePoseAt(nowMs, ground)
	case r.pending != nil:
		return r.pending.pose
	}
	// A record with neither form reads as moverRecord.value() does: the
	// zero MoverState. set never stores one; this keeps the two reads equal.
	return monster.MoverState{}.LivePoseAt(nowMs, ground)
}

/*
================
get
================
*/
func (s *moverStorage) get(gid uint32) monster.MoverState {
	row, _ := s.lookup(gid)
	return row
}

/*
================
values
================
*/
func (s *moverStorage) values() iter.Seq2[uint32, monster.MoverState] {
	return func(yield func(uint32, monster.MoverState) bool) {
		for gid, r := range s.records() {
			if !yield(gid, r.value()) {
				return
			}
		}
	}
}

/*
================
records
================
*/
func (s *moverStorage) records() iter.Seq2[uint32, moverRecord] {
	return func(yield func(uint32, moverRecord) bool) {
		if s == nil {
			return
		}
		for _, slot := range s.slots {
			if !yield(slot.gid, slot.rec) {
				return
			}
		}
	}
}

/*
================
moverSchedule

What the behaviour queue reads of a mover (scheduleBehavior, tryDormant).
================
*/
type moverSchedule struct {
	mode     monster.MoverMode
	activity monster.ActivityCadence
	arriveMs int64
	regionID uint16
}

/*
================
schedule

lookup(gid)'s mode, activity cadence, arrival and region without building
its MoverState. A pending row expands to its stored mode (Spawning or
Pending), pose and cadence with no segment (PendingSnapshot admits only
ArriveMs == 0); the behaviour queue reads one per due actor per tick.
================
*/
func (s *moverStorage) schedule(gid uint32) (moverSchedule, bool) {
	m, _, ok := s.scheduleSlot(gid)
	return m, ok
}

/*
================
scheduleSlot

schedule plus the dense slot the row sits in. The behaviour batch
build carries the slot to the dispatch read (lookupAt), which
verifies it before use: remove swaps the last row into the hole, so
a slot carried across a despawn can name another actor (#2135).
================
*/
func (s *moverStorage) scheduleSlot(gid uint32) (moverSchedule, uint32, bool) {
	if s == nil {
		return moverSchedule{}, 0, false
	}
	slot, ok := s.slotOf(gid)
	if !ok {
		return moverSchedule{}, 0, false
	}
	r := s.slots[slot].rec
	if r.live != nil {
		return moverSchedule{mode: r.live.Mode(), activity: r.live.Activity, arriveMs: r.live.ArriveMs,
			regionID: r.live.Pose.RegionID}, slot, true
	}
	if r.pending != nil {
		mode := monster.MoverPending
		if r.pending.shared.Value().row.Spawning {
			mode = monster.MoverSpawning
		}
		return moverSchedule{mode: mode, activity: r.pending.activity, regionID: r.pending.pose.RegionID}, slot, true
	}
	return moverSchedule{}, slot, true
}

/*
================
catchUpActivity

Rewrites one actor's activity cadence in place to the phase it would
hold had it never slept (monster.ActivityCadence.CatchUp). The dormancy
wake calls this before rescheduling, so the scheduler's own cadence
branch lands the actor's next due time on its native phase.
================
*/
func (s *moverStorage) catchUpActivity(gid uint32, now uint32) {
	if s == nil {
		return
	}
	slot, ok := s.slotOf(gid)
	if !ok {
		return
	}
	r := s.slots[slot].rec
	if r.live != nil {
		r.live.Activity = r.live.Activity.CatchUp(now)
	}
	if r.pending != nil {
		r.pending.activity = r.pending.activity.CatchUp(now)
	}
}

/*
================
compact
================
*/
func (s *moverStorage) compact(gid uint32) bool {
	if s == nil {
		return false
	}
	slot, ok := s.slotOf(gid)
	return ok && s.slots[slot].rec.pending != nil
}

/*
================
len
================
*/
func (s *moverStorage) len() int {
	if s == nil {
		return 0
	}
	return len(s.slots)
}

/*
================
remove

Swap-removes the slot: the last row takes the hole and its byGID
entry follows it, so removal stays O(1) and slot order is the only
casualty (see moverSlot).
================
*/
func (s *moverStorage) remove(gid uint32) {
	if s == nil {
		return
	}
	slot, exists := s.slotOf(gid)
	if !exists {
		return
	}
	s.blocks.note(gid)
	s.blocks.rosterGeneration++
	s.spatial.remove(gid, s.slots[slot].rec.interestBounds())
	last := uint32(len(s.slots) - 1)
	if slot != last {
		s.slots[slot] = s.slots[last]
		s.byGID[s.slots[slot].gid] = slot
	}
	s.slots[last] = moverSlot{}
	s.slots = s.slots[:last]
	delete(s.byGID, gid)
}
