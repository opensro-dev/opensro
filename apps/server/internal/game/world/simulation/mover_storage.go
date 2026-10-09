/*
===========================================================================

mover_storage.go - compact mover records and their spatial candidate owner

===========================================================================
*/

package simulation

import (
	"iter"
	"math"
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
	before, after := moverInterestBoundsFor(old.value()), moverInterestBoundsFor(row)
	if !exists || before != after {
		if exists {
			s.spatial.remove(gid, before)
		}
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
		s.spatial.remove(gid, moverInterestBoundsFor(row.value()))
		delete(s.rows, gid)
	}
}
