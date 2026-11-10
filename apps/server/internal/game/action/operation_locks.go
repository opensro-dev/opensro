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

// The tick's bounded wait for the publication gate (#2143). Past
// tickGateBound the tick drops the rest of its action steps, and it may do
// so maxTickGateCuts ticks running: after that it waits as it always did,
// so a held gate delays these steps by about half a second, never forever.
// A var so a test can shrink the bound.
var tickGateBound = 5 * time.Millisecond

const maxTickGateCuts = 5

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
	return rt.publicationRelease(lane, label)
}

/*
================
publicationRelease

The release of a publication gate just taken: the bare one, or with a
sink wired the one that reports a long hold under label.
================
*/
func (rt *Runtime) publicationRelease(lane *divisionLane, label string) func() {
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
	ids := rt.publicationDivisions(nil)
	releases := make([]func(), 0, len(ids))
	for _, id := range ids {
		releases = append(releases, rt.LockPublication(id, "tick"))
	}
	return releaseTickPublication(releases)
}

/*
================
publicationDivisions

Every division with a lane, in the one order the tick takes their gates,
written over ids so the per-step tick gate allocates nothing.
================
*/
func (rt *Runtime) publicationDivisions(ids []string) []string {
	ids = ids[:0]
	rt.operations.mu.Lock()
	for id := range rt.operations.division {
		ids = append(ids, id)
	}
	rt.operations.mu.Unlock()
	sort.Strings(ids)
	return ids
}

/*
================
releaseTickPublication
================
*/
func releaseTickPublication(releases []func()) func() {
	return func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}
}

/*
================
tickGateHold

One publication gate the tick holds, and when it was taken.
================
*/
type tickGateHold struct {
	lane     *divisionLane
	acquired time.Time
}

/*
================
tryLockTickPublication

lockTickPublication that gives up after bound (#2143), holding in rt.tickGate
for releaseTickGate. A request stuck on the disk holds one division's gate
for seconds; the tick gives back whatever it took, so nothing waits behind a
tick that is itself waiting.
================
*/
func (rt *Runtime) tryLockTickPublication(bound time.Duration) bool {
	deadline := time.Now().Add(bound)
	rt.tickGate.ids = rt.publicationDivisions(rt.tickGate.ids)
	for _, id := range rt.tickGate.ids {
		lane := rt.operations.lane(id, &rt.maintenance)
		if !lane.publication.TryLock() && !waitTickGate(lane, time.Until(deadline)) {
			rt.releaseTickGate()
			return false
		}
		rt.tickGate.held = append(rt.tickGate.held, tickGateHold{lane: lane, acquired: time.Now()})
	}
	return true
}

/*
================
waitTickGate

Takes lane's publication gate, waiting at most bound for the holder to
release it (#3102). sync.Mutex has no timed lock, so a waiter goroutine
parks on Lock and the caller races it against the deadline.

The wait this replaces polled TryLock on a timer: the tick never queued
on the gate, so a handler arriving while the tick polled could always
take a released gate first - the tick only ever won a gate nobody else
was reaching for. The parked waiter keeps the substance of that
fairness with the mutex's own: it queues behind every earlier waiter,
handlers included, and is served in arrival order, so a handler that
was already waiting is never barged past, and the tick's wait is
bounded by the holds ahead of it rather than by a sleep quantum.
A handler arriving after the tick queues behind it, exactly as it
would behind waitTickPublication's plain Lock.

Ownership is decided at the caller's select alone: the waiter owns
the gate from the moment Lock returns, so when the deadline wins a
reaper takes that ownership only to give the gate straight back. The
gate is therefore held for one handoff after a lost wait, and never
by a tick that has already given up. The waiter starts only on a held
gate - the free gate costs no goroutine, which the gate's
zero-allocation contract (TestTickGateHoldAllocatesNothing) rests on.
================
*/
func waitTickGate(lane *divisionLane, bound time.Duration) bool {
	if bound <= 0 {
		return false
	}
	acquired := make(chan struct{})
	go func() {
		lane.publication.Lock()
		close(acquired)
	}()
	timer := time.NewTimer(bound)
	defer timer.Stop()
	select {
	case <-acquired:
		return true
	case <-timer.C:
		go func() {
			<-acquired
			lane.publication.Unlock()
		}()
		return false
	}
}

/*
================
waitTickPublication

The unbounded form, for a tick past its cut limit, holding in rt.tickGate.
================
*/
func (rt *Runtime) waitTickPublication() {
	rt.tickGate.ids = rt.publicationDivisions(rt.tickGate.ids)
	for _, id := range rt.tickGate.ids {
		lane := rt.operations.lane(id, &rt.maintenance)
		lane.publication.Lock()
		rt.tickGate.held = append(rt.tickGate.held, tickGateHold{lane: lane, acquired: time.Now()})
	}
}

/*
================
releaseTickGate

Gives back what rt.tickGate holds, last taken first, and keeps the buffer.
A long hold is reported as publicationRelease reports one, without the
closure it would build per division per step.
================
*/
func (rt *Runtime) releaseTickGate() {
	held := rt.tickGate.held
	for i := len(held) - 1; i >= 0; i-- {
		elapsed := time.Since(held[i].acquired)
		held[i].lane.releasePublication()
		if rt.PublicationHold != nil && elapsed >= simulation.SlowHookThreshold {
			rt.PublicationHold("tick", elapsed)
		}
		held[i] = tickGateHold{}
	}
	rt.tickGate.held = held[:0]
}
