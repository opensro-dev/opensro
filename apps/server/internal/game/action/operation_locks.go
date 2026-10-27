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
	"sort"
	"sync"
	"time"

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
	release            func()
	publication        sync.Mutex
	releasePublication func()
	// monsterAttack is the lane's monster attack capability, made at the
	// first monster action and only ever used under the lane
	// (RunMonsterAction).
	monsterAttack simulation.MonsterAttackOperation
}

/*
================
divisionOperationLocks

The short map lock owns lane discovery only; gameplay runs under the
returned lane. onRelease, set once by NewRuntime, runs after every
lane release with nothing held: instance lifecycle delivery hangs off
it (instance_events.go), so queued occurrences reach their handlers
with no division lock held, whatever lock context produced them.
================
*/
type divisionOperationLocks struct {
	mu        sync.Mutex
	division  map[string]*divisionLane
	onRelease func()
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
		lane.releasePublication = lane.publication.Unlock
		lane.release = func() {
			lane.Unlock()
			maintenance.RUnlock()
			if locks.onRelease != nil {
				locks.onRelease()
			}
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

/*
================
LockPublication

Request adapters, entry bootstraps and delayed pickups retain this outer
gate from admission through receipt enqueue. Their operation releases the
division/store locks before sending: a queue failure may synchronously run
cleanup that takes the division lock again. Never acquire this gate inside
an operation, transport callback, or cleanup hook.

The label names the holder for the gate-hold report: every caller passes
its own ("enterWorld", "requestOp", ...), so a stall can be attributed
to the kind of work that kept the gate. With a sink wired
(rt.PublicationHold), a hold at or above simulation.SlowHookThreshold is
reported under its label after the gate is released; shorter holds and
unwired runtimes take the bare release, exactly as before.
================
*/
func (rt *Runtime) LockPublication(divisionID, label string) func() {
	lane := rt.operations.lane(divisionID, &rt.maintenance)
	lane.publication.Lock()
	sink := rt.PublicationHold
	if sink == nil {
		return lane.releasePublication
	}
	acquired := time.Now()
	release := lane.releasePublication
	return func() {
		held := time.Since(acquired)
		release()
		if held >= simulation.SlowHookThreshold {
			sink(label, held)
		}
	}
}

/*
================
lockTickPublication

Takes every known division's publication gate, in division order, for a
span of tick work that commits and then captures private frames
(sealTickPrivate). Request handlers and entry bootstraps hold one gate at a
time, so the fixed order cannot deadlock against them. A lane created after
this snapshot belongs to a division whose first world entry is still in
progress, so no character of it has tick work yet.
================
*/
func (rt *Runtime) lockTickPublication() func() {
	rt.operations.mu.Lock()
	ids := make([]string, 0, len(rt.operations.division))
	for id := range rt.operations.division {
		ids = append(ids, id)
	}
	rt.operations.mu.Unlock()
	sort.Strings(ids)
	releases := make([]func(), 0, len(ids))
	for _, id := range ids {
		releases = append(releases, rt.LockPublication(id, "tick"))
	}
	return func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}
}
