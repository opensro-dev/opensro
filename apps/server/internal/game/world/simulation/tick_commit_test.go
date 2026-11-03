/*
===========================================================================

tick_commit_test.go - the ticker opens and flushes one batch per tick

===========================================================================
*/

package simulation

import (
	"context"
	"testing"
	"time"
)

/*
================
commitRecorder

Records the order of batch edges and hook runs.
================
*/
type commitRecorder struct{ events []string }

/*
================
commitRecorder.commit
================
*/
func (r *commitRecorder) commit() TickCommit {
	return TickCommit{
		Begin: func() { r.events = append(r.events, "begin") },
		Flush: func() TickCommitStats {
			r.events = append(r.events, "flush")
			return TickCommitStats{Deferred: 3, Rows: 2, Elapsed: time.Millisecond, Flushed: true}
		},
	}
}

/*
================
TestTickFlushesItsBatchAfterEveryHook

The batch opens before the first hook and flushes after the last, once
per tick, and the flush reaches the tick's timing. A panicking hook or a
cancelled tick still flushes.
================
*/
func TestTickFlushesItsBatchAfterEveryHook(t *testing.T) {
	clock := &fakeTickClock{now: time.UnixMilli(1000)}
	rec := &commitRecorder{}
	hook := func(name string) TickHook {
		return func(int64) []DivisionFrames {
			rec.events = append(rec.events, name)
			return nil
		}
	}
	panics := func(int64) []DivisionFrames {
		rec.events = append(rec.events, "panics")
		panic("hook failure")
	}
	ticker, got := timedTicker(clock, NamedTickHook{Hook: hook("hook")}, NamedTickHook{Hook: panics})
	ticker.BeforeHooks = []NamedTickHook{{Hook: hook("before")}}
	ticker.Commit = rec.commit()
	ticker.RunTick(0)
	want := []string{"begin", "before", "hook", "panics", "flush"}
	if len(rec.events) != len(want) {
		t.Fatalf("events = %v, want %v", rec.events, want)
	}
	for i := range want {
		if rec.events[i] != want[i] {
			t.Fatalf("events = %v, want %v", rec.events, want)
		}
	}
	if len(*got) != 1 || (*got)[0].Commit != (TickCommitStats{Deferred: 3, Rows: 2, Elapsed: time.Millisecond, Flushed: true}) {
		t.Fatalf("timing = %+v, want the flush's stats", *got)
	}

	rec.events = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ticker.runScheduledTick(ctx, 0, nil)
	if len(rec.events) != 3 || rec.events[0] != "begin" || rec.events[2] != "flush" {
		t.Fatalf("cancelled tick events = %v, want begin, before, flush", rec.events)
	}
}
