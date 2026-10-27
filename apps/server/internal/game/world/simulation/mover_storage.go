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
moverStorage
================
*/
type moverStorage struct {
	rows    map[uint32]moverRecord
	spatial moverInterestIndex
}

/*
================
newMoverStorage
================
*/
func newMoverStorage(rows map[uint32]monster.MoverState) *moverStorage {
	s := &moverStorage{rows: make(map[uint32]moverRecord, len(rows))}
	for gid, row := range rows {
		s.set(gid, row)
	}
	return s
}

/*
================
set
================
*/
func (s *moverStorage) set(gid uint32, row monster.MoverState) {
	if s.rows == nil {
		s.rows = make(map[uint32]moverRecord)
	}
	old, exists := s.rows[gid]
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
		s.rows[gid] = moverRecord{pending: resident}
		return
	}
	if old.live != nil {
		*old.live = row
		return
	}
	live := new(monster.MoverState)
	*live = row
	s.rows[gid] = moverRecord{live: live}
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
	r, ok := s.rows[gid]
	return r.value(), ok
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
	_, ok := s.rows[gid]
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
	r, ok := s.rows[gid]
	if !ok {
		return monster.Pose{}, false
	}
	return r.livePoseAt(nowMs, ground), true
}

/*
================
livePoses

Every row's livePoseAt, in the rows' (map) order: a population scan that
reads only where each mover stands, without expanding a single row.
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
		for gid, record := range s.rows {
			if !yield(gid, record) {
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
	if s == nil {
		return moverSchedule{}, false
	}
	r, ok := s.rows[gid]
	if !ok {
		return moverSchedule{}, false
	}
	if r.live != nil {
		return moverSchedule{mode: r.live.Mode(), activity: r.live.Activity, arriveMs: r.live.ArriveMs,
			regionID: r.live.Pose.RegionID}, true
	}
	if r.pending != nil {
		mode := monster.MoverPending
		if r.pending.shared.Value().row.Spawning {
			mode = monster.MoverSpawning
		}
		return moverSchedule{mode: mode, activity: r.pending.activity, regionID: r.pending.pose.RegionID}, true
	}
	return moverSchedule{}, true
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
	r, ok := s.rows[gid]
	if !ok {
		return
	}
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
	return s != nil && s.rows[gid].pending != nil
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
	return len(s.rows)
}

/*
================
remove
================
*/
func (s *moverStorage) remove(gid uint32) {
	if s == nil {
		return
	}
	if row, exists := s.rows[gid]; exists {
		s.spatial.remove(gid, row.interestBounds())
		delete(s.rows, gid)
	}
}
