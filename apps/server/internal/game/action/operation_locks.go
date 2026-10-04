/*
===========================================================================

operation_locks.go - one ordered lane per division

Ordinary gameplay in one division runs under that division's lane; lanes of
different divisions run concurrently. The maintenance barrier lets the
all-division TTL sweep stop every lane briefly without acquiring an
open-ended set of locks.

===========================================================================
*/

package action

import (
	"sync"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
divisionLane

A division's lock and its release (the lane, then the maintenance
barrier), made once with the lane: lockDivision hands out that same func
value, where building a closure per call allocated twice for every monster
action and every command.
================
*/
type divisionLane struct {
	sync.Mutex
	release func()
	// monsterAttack is the lane's monster attack capability, made at the
	// first monster action and only ever used under the lane
	// (RunMonsterAction).
	monsterAttack simulation.MonsterAttackOperation
}

/*
================
divisionOperationLocks

The short map lock owns lane discovery only; gameplay runs under the
returned lane.
================
*/
type divisionOperationLocks struct {
	mu       sync.Mutex
	division map[string]*divisionLane
}

/*
================
lane

The lane of divisionID, created on first use with release bound to
maintenance.
================
*/
func (locks *divisionOperationLocks) lane(divisionID string, maintenance *sync.RWMutex) *divisionLane {
	locks.mu.Lock()
	defer locks.mu.Unlock()
	if locks.division == nil {
		locks.division = make(map[string]*divisionLane)
	}
	lane := locks.division[divisionID]
	if lane == nil {
		lane = &divisionLane{}
		lane.release = func() {
			lane.Unlock()
			maintenance.RUnlock()
		}
		locks.division[divisionID] = lane
	}
	return lane
}

/*
================
lockDivision

Admit ordinary work concurrently across divisions while keeping one
division's ground, pending, world and character transitions in order.
================
*/
func (rt *Runtime) lockDivision(divisionID string) func() {
	rt.maintenance.RLock()
	lane := rt.operations.lane(divisionID, &rt.maintenance)
	lane.Lock()
	return lane.release
}
