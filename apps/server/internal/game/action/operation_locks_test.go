/*
===========================================================================

operation_locks_test.go - division operation and publication lock tests

Verifies per-division isolation and panic-safe release of division and
publication locks across recovered tick hook panics.

===========================================================================
*/

package action

import (
	"sync"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/wait"
)

/*
================
TestDivisionOperationLocksIsolateUnrelatedWorlds
================
*/
func TestDivisionOperationLocksIsolateUnrelatedWorlds(t *testing.T) {
	rt := &Runtime{}
	unlockFirst := rt.lockDivision("division-a")

	otherEntered := make(chan struct{})
	otherRelease := make(chan struct{})
	go func() {
		unlock := rt.lockDivision("division-b")
		close(otherEntered)
		<-otherRelease
		unlock()
	}()
	select {
	case <-otherEntered:
	case <-time.After(time.Second):
		t.Fatal("an unrelated division blocked behind active item work")
	}

	sameEntered := make(chan struct{})
	go func() {
		unlock := rt.lockDivision("division-a")
		close(sameEntered)
		unlock()
	}()
	select {
	case <-sameEntered:
		t.Fatal("two item operations entered the same division concurrently")
	case <-time.After(25 * time.Millisecond):
	}
	close(otherRelease)
	unlockFirst()
	select {
	case <-sameEntered:
	case <-time.After(time.Second):
		t.Fatal("same-division work did not enter after its predecessor returned")
	}
}

/*
================
panickingActionDeps
================
*/
type panickingActionDeps struct {
	Dependencies
	panicNow bool
}

/*
================
CharactersForDivision
================
*/
func (d *panickingActionDeps) CharactersForDivision(division string) []*enterworld.Character {
	if d.panicNow {
		panic("simulated tick panic under lockDivision")
	}
	return d.Dependencies.CharactersForDivision(division)
}

/*
================
TestDivisionOperationLocksReleaseOnRecoveredTickPanic

Regression test for #1210: when a step inside TickHook panics while holding
rt.lockDivision(divisionID) (such as advanceProjectileCasts resolving a
combat target) and the ticker recovers the panic, both lockDivision and
lockTickPublication must unwind via defer so incoming packet handlers
(LockPublication -> lockDivision) and subsequent ticks do not deadlock.
================
*/
func TestDivisionOperationLocksReleaseOnRecoveredTickPanic(t *testing.T) {
	character := testCharacter()
	rt, _ := newTestRuntime(character, testItems())
	deps := &panickingActionDeps{
		Dependencies: rt.deps,
		panicNow:     true,
	}
	rt.deps = deps

	// Seed the division lane before the tick so lockTickPublication includes it.
	unlockSeed := rt.lockDivision(testDivision)
	unlockSeed()

	rt.pendingProjectileCasts = append(rt.pendingProjectileCasts, pendingProjectileCast{
		divisionID:    testDivision,
		characterName: character.Name,
		ownerKey:      simulation.WorldKey(testDivision, character.Name),
		characterID:   character.ID,
		token:         1,
		releaseAtMs:   100,
		cast:          wire.SkillAction{ActionId: 1, TargetGid: 999},
	})

	hook := rt.TickHook()
	panicked := false
	func() {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		hook(200)
	}()
	if !panicked {
		t.Fatal("expected simulated panic inside advanceProjectileCasts")
	}

	// Simulate an incoming packet handler (LockPublication -> lockDivision)
	// and a subsequent TickHook after the panic was recovered.
	deps.panicNow = false
	done := make(chan struct{})
	go func() {
		unlockPub := rt.LockPublication(testDivision, "test")
		unlockDiv := rt.lockDivision(testDivision)
		unlockDiv()
		unlockPub()
		hook(300)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("division or publication lock leaked after recovered tick panic")
	}
}

/*
================
TestLockPublicationReportsSlowHoldsByLabel

A hold at or above the slow threshold reports once, under the caller's
label, and only once the gate is released; a shorter hold stays silent
(#1820 E). This is the holder side of the tickPublicationGate wait.
================
*/
func TestLockPublicationReportsSlowHoldsByLabel(t *testing.T) {
	rt := &Runtime{}
	type hold struct {
		label   string
		elapsed time.Duration
	}
	var mu sync.Mutex
	var holds []hold
	rt.PublicationHold = func(label string, elapsed time.Duration) {
		mu.Lock()
		holds = append(holds, hold{label, elapsed})
		mu.Unlock()
	}
	reported := func() []hold {
		mu.Lock()
		defer mu.Unlock()
		return append([]hold(nil), holds...)
	}

	rt.LockPublication(testDivision, "enterWorld")()
	if got := reported(); len(got) != 0 {
		t.Fatalf("a fast hold was reported: %v", got)
	}
	release := rt.LockPublication(testDivision, "enterWorld")
	// While the gate is held nothing is reported; the report lands on
	// release, once the next holder can already proceed.
	wait.Consistently(t, 150*time.Millisecond, "no hold is reported while the gate is held", func() bool {
		return len(reported()) == 0
	})
	release()
	if got := reported(); len(got) != 1 || got[0].label != "enterWorld" || got[0].elapsed < simulation.SlowHookThreshold {
		t.Fatalf("holds = %+v, want one slow enterWorld hold", got)
	}
}
