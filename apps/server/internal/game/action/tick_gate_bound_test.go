/*
===========================================================================

tick_gate_bound_test.go - a held publication gate cuts the tick, briefly

A request stuck behind the disk holds one division's publication gate.
The tick's wait for it is bounded (#2143): the hook gives back what it took
and drops its remaining steps, at most maxTickGateCuts ticks running.

===========================================================================
*/
package action

import (
	"testing"
	"time"
)

/*
================
TestTickGateCutsAHeldTickThenWaits

While a holder keeps the gate the hook returns within its bound for
maxTickGateCuts ticks; the next tick waits for the holder, so the action
steps cannot starve behind a stream of holders.
================
*/
func TestTickGateCutsAHeldTickThenWaits(t *testing.T) {
	rt, clock, _, _ := newCombatTestRuntime(t, 100)
	saved := tickGateBound
	tickGateBound = time.Millisecond
	defer func() { tickGateBound = saved }()
	cuts := 0
	rt.Steps.Slow = func(name string, _ time.Duration) {
		if name == "tickGateCut" {
			cuts++
		}
	}
	hook := rt.TickHook()

	release := rt.LockPublication(testDivision, "test")
	for i := 1; i <= maxTickGateCuts; i++ {
		hook(clock.NowMs())
		if rt.tickGateCuts != i {
			t.Fatalf("tick %d: cuts = %d", i, rt.tickGateCuts)
		}
	}

	if cuts != maxTickGateCuts {
		t.Fatalf("reported %d cuts, want %d", cuts, maxTickGateCuts)
	}
	done := make(chan struct{})
	go func() {
		hook(clock.NowMs())
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("the tick past the cut limit did not wait for the holder")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting tick never finished after the holder released")
	}
	if rt.tickGateCuts != 0 {
		t.Fatalf("a served tick left cuts = %d", rt.tickGateCuts)
	}
}

/*
================
TestTickGateGivesBackWhatItTook

A division whose gate was free must not stay held behind a cut: the
holder's own division is the only one a request could still be waiting on.
================
*/
func TestTickGateGivesBackWhatItTook(t *testing.T) {
	rt, _, _, _ := newCombatTestRuntime(t, 100)
	rt.LockPublication("aaa-first", "test")() // make the lane; sorts first
	release := rt.LockPublication(testDivision, "test")
	defer release()
	if _, ok := rt.tryLockTickPublication(time.Millisecond); ok {
		t.Fatal("took a held gate")
	}
	lane := rt.operations.lane("aaa-first", &rt.maintenance)
	if !lane.publication.TryLock() {
		t.Fatal("a cut left the free division's gate held")
	}
	lane.publication.Unlock()
}

/*
================
TestTickGateFreeServesEveryStep

With no holder the bound never fires: a free gate must not count as a cut.
================
*/
func TestTickGateFreeServesEveryStep(t *testing.T) {
	rt, clock, _, _ := newCombatTestRuntime(t, 100)
	rt.TickHook()(clock.NowMs())
	if rt.tickGateCuts != 0 {
		t.Fatalf("a free gate cut %d ticks", rt.tickGateCuts)
	}
}
