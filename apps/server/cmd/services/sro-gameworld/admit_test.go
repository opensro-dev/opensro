/*
===========================================================================

admit_test.go - admission waits for the boot fill, within a bound

===========================================================================
*/

package main

import (
	"context"
	"errors"
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
TestAdmissionStartsThenOpensAndNeverAfterCancel

The transport starts before readiness opens; a start failure opens
nothing; a run cancelled between the settle check and the start starts and
opens nothing (Run then drains a transport that never listened).
================
*/
func TestAdmissionStartsThenOpensAndNeverAfterCancel(t *testing.T) {
	var order []string
	record := func(step string) func() { return func() { order = append(order, step) } }
	base := admission{
		settled: func() bool { return true },
		start:   func() error { record("start")(); return nil },
		open:    record("open"),
		poll:    time.Millisecond,
		limit:   time.Minute,
	}
	if err := admitWhenSettled(context.Background(), base); err != nil || len(order) != 2 || order[0] != "start" || order[1] != "open" {
		t.Fatalf("admission order %v err %v", order, err)
	}

	order = nil
	failing := base
	failing.start = func() error { return errors.New("listen failed") }
	if err := admitWhenSettled(context.Background(), failing); err == nil || len(order) != 0 {
		t.Fatalf("a failed start returned %v and ran %v", err, order)
	}

	order = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancelled := base
	cancelled.settled = func() bool { cancel(); return true }
	if err := admitWhenSettled(ctx, cancelled); err != nil || len(order) != 0 {
		t.Fatalf("a run cancelled at the settle check returned %v and ran %v", err, order)
	}
}
