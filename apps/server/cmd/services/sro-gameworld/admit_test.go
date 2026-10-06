/*
===========================================================================

admit_test.go - admission waits for the boot fill, within a bound

===========================================================================
*/

package main

import (
	"context"
	"testing"
	"time"
)

/*
================
TestAwaitBootFillOpensOnSettleAndAtTheBound

Settling opens admission; a fill that never settles still opens at the
bound (false, so the caller warns); a cancelled run stops waiting.
================
*/
func TestAwaitBootFillOpensOnSettleAndAtTheBound(t *testing.T) {
	polls := 0
	if !awaitBootFill(context.Background(), func() bool { polls++; return polls >= 3 }, time.Millisecond, time.Minute) {
		t.Fatal("a fill that settled was reported unsettled")
	}
	if polls != 3 {
		t.Fatalf("polled %d times, want 3", polls)
	}
	started := time.Now()
	if awaitBootFill(context.Background(), func() bool { return false }, time.Millisecond, 20*time.Millisecond) {
		t.Fatal("a fill that never settled was reported settled")
	}
	if waited := time.Since(started); waited < 20*time.Millisecond || waited > 5*time.Second {
		t.Fatalf("the bound held admission for %s", waited)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if awaitBootFill(ctx, func() bool { return false }, time.Millisecond, time.Minute) {
		t.Fatal("a cancelled run was reported settled")
	}
}

/*
================
TestAdmissionOpensOnceAndNeverAfterCancel

A settled fill opens readiness once; a run cancelled inside the settle
check opens nothing (Run then drains).
================
*/
func TestAdmissionOpensOnceAndNeverAfterCancel(t *testing.T) {
	opened := 0
	base := admission{
		settled: func() bool { return true },
		open:    func() { opened++ },
		poll:    time.Millisecond,
		limit:   time.Minute,
	}
	admitWhenSettled(context.Background(), base)
	if opened != 1 {
		t.Fatalf("opened %d times, want 1", opened)
	}
	opened = 0
	ctx, cancel := context.WithCancel(context.Background())
	cancelled := base
	cancelled.settled = func() bool { cancel(); return true }
	admitWhenSettled(ctx, cancelled)
	if opened != 0 {
		t.Fatal("a run cancelled at the settle check opened readiness")
	}
}
