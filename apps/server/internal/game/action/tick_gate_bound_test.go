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

	"opensro.online/server/internal/game/world/simulation"
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
	if rt.tryLockTickPublication(time.Millisecond) {
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

/*
================
TestTickGateHoldAllocatesNothing

The gate is taken once per TickHook step, dozens of times a tick, so a
single allocation here multiplies into the tick budget (#2143). The first
pass grows the buffers; every later one must reuse them. The hold sink is
wired, as in production, so the report path is what gets measured.
================
*/
func TestTickGateHoldAllocatesNothing(t *testing.T) {
	rt, _, _, _ := newCombatTestRuntime(t, 100)
	rt.PublicationHold = func(string, time.Duration) {}
	rt.LockPublication(testDivision, "test")() // make the lane
	hold := func() {
		if !rt.acquireTickGate() {
			t.Fatal("a free gate cut the step")
		}
		rt.releaseTickGate()
	}
	hold()
	if allocs := testing.AllocsPerRun(100, hold); allocs != 0 {
		t.Fatalf("one gate hold allocated %.0f times, want 0", allocs)
	}
}

/*
================
TestTickGateReportsALongHold

The tick's hold names itself in the gate-hold report, as every other holder
does; a stall report that lost the tick would blame only the waiters.
================
*/
func TestTickGateReportsALongHold(t *testing.T) {
	rt, _, _, _ := newCombatTestRuntime(t, 100)
	var labels []string
	rt.PublicationHold = func(label string, elapsed time.Duration) {
		labels = append(labels, label)
	}
	rt.LockPublication(testDivision, "test")() // make the lane
	labels = nil
	if !rt.acquireTickGate() {
		t.Fatal("a free gate cut the step")
	}
	rt.releaseTickGate()
	if len(labels) != 0 {
		t.Fatalf("a short hold reported %v", labels)
	}
	if !rt.acquireTickGate() {
		t.Fatal("a free gate cut the step")
	}
	rt.tickGate.held[0].acquired = time.Now().Add(-2 * simulation.SlowHookThreshold)
	rt.releaseTickGate()
	if len(labels) != 1 || labels[0] != "tick" {
		t.Fatalf("a long hold reported %v, want one tick report", labels)
	}
}

/*
================
TestTickGateCutLeavesQueuedWorkForNextTick

The cut is sticky: once a step loses the gate, the steps after it do not
run later in the same hook when the holder lets go - their work waits for
the next tick, in order. The monster leg's queued recipient frames are
the witness: they survive the cut hook untouched, though the gate frees
while it runs, and drain on the next served one.
================
*/
func TestTickGateCutLeavesQueuedWorkForNextTick(t *testing.T) {
	rt, clock, _, _ := newCombatTestRuntime(t, 100)
	saved := tickGateBound
	tickGateBound = 5 * time.Millisecond
	defer func() { tickGateBound = saved }()
	rt.monsterLegRecipientsMu.Lock()
	rt.monsterLegRecipients = append(rt.monsterLegRecipients, simulation.DivisionFrames{
		DivisionID: testDivision,
		Frames:     []simulation.Frame{{Opcode: 1}},
	})
	rt.monsterLegRecipientsMu.Unlock()
	hook := rt.TickHook()
	release := rt.LockPublication(testDivision, "test")
	released := false
	// The holder lets go the moment the hook reports its cut, with the
	// remaining steps still ahead: a non-sticky hook would run them here.
	rt.Steps.Slow = func(name string, _ time.Duration) {
		if name == "tickGateCut" && !released {
			released = true
			release()
		}
	}
	hook(clock.NowMs())
	if !released {
		t.Fatal("the hook never cut, so the test proved nothing")
	}
	rt.monsterLegRecipientsMu.Lock()
	queued := len(rt.monsterLegRecipients)
	rt.monsterLegRecipientsMu.Unlock()
	if queued != 1 {
		t.Fatalf("a cut hook drained the queued work: %d batches left", queued)
	}
	out := hook(clock.NowMs() + 1)
	rt.monsterLegRecipientsMu.Lock()
	queued = len(rt.monsterLegRecipients)
	rt.monsterLegRecipientsMu.Unlock()
	if queued != 0 {
		t.Fatalf("the served hook left %d batches queued", queued)
	}
	drained := 0
	for _, batch := range out {
		if batch.DivisionID == testDivision {
			drained++
		}
	}
	if drained != 1 {
		t.Fatalf("the served hook published %d drained batches, want 1", drained)
	}
}
