/*
===========================================================================

tick_watchdog.go - names the work behind a stalled simulation tick

A tick that runs for seconds freezes every client's replies at once (moves,
heartbeats and broadcasts arrive together when it ends), and the tick metric
can only say that it happened. The coordinator stamps the phase it is in and
the hook it is running; a watchdog goroutine reports a tick that outlives
the stall threshold exactly once, with every goroutine's stack, so the lock
or I/O holding the tick is in the log the first time it happens.

The stamps are three atomic stores per hook. Nothing is formatted unless a
tick has already stalled.

===========================================================================
*/

package simulation

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	// DefaultTickStallReport is ten tick budgets: far past any overrun the
	// metric already counts, short enough to catch a stall while it lasts.
	DefaultTickStallReport = time.Second
	tickStallPoll          = 250 * time.Millisecond
	tickStallStackBytes    = 1 << 20
)

// Phases of one coordinated tick, in execution order.
const (
	tickPhaseIdle int32 = iota
	tickPhaseBeforeHooks
	tickPhaseDivisions
	tickPhaseHooks
)

var tickPhaseNames = [...]string{"idle", "before-hooks", "divisions", "hooks"}

/*
================
tickWatch

Written by the coordinator, read by the watchdog. startNs is zero between
ticks; reported holds the startNs of the last tick already reported, so a
stall logs once however long it lasts.
================
*/
type tickWatch struct {
	startNs  atomic.Int64
	phase    atomic.Int32
	hook     atomic.Uintptr
	reported atomic.Int64
}

/*
================
tickWatch.begin
================
*/
func (w *tickWatch) begin(now time.Time) {
	w.hook.Store(0)
	w.phase.Store(tickPhaseBeforeHooks)
	w.startNs.Store(now.UnixNano())
}

/*
================
tickWatch.end
================
*/
func (w *tickWatch) end() {
	w.startNs.Store(0)
	w.phase.Store(tickPhaseIdle)
	w.hook.Store(0)
}

/*
================
tickWatch.enterHook

The hook's code pointer names it later; closures resolve to their
enclosing constructor (for example action.(*Runtime).TickHook.func1).
================
*/
func (w *tickWatch) enterHook(hook TickHook) {
	w.hook.Store(reflect.ValueOf(hook).Pointer())
}

/*
================
tickWatch.leaveHook
================
*/
func (w *tickWatch) leaveHook() {
	w.hook.Store(0)
}

/*
================
tickWatch.check

Returns the report for a tick running longer than after at now, once per
tick, or "" when there is nothing to report.
================
*/
func (w *tickWatch) check(now time.Time, after time.Duration) string {
	start := w.startNs.Load()
	if start == 0 {
		return ""
	}
	elapsed := time.Duration(now.UnixNano() - start)
	if elapsed < after || w.reported.Load() == start {
		return ""
	}
	w.reported.Store(start)
	phase := w.phase.Load()
	name := "none"
	if phase >= 0 && int(phase) < len(tickPhaseNames) {
		name = tickPhaseNames[phase]
	}
	hook := "none"
	if pc := w.hook.Load(); pc != 0 {
		if fn := runtime.FuncForPC(pc); fn != nil {
			hook = fn.Name()
		}
	}
	stacks := make([]byte, tickStallStackBytes)
	stacks = stacks[:runtime.Stack(stacks, true)]
	return fmt.Sprintf("simulation: tick stalled %v in %s (hook %s); goroutines:\n%s",
		elapsed.Round(time.Millisecond), name, hook, stacks)
}

/*
================
watchStalls

The ticker's watchdog goroutine. It never touches tick state beyond the
atomics, so a stalled coordinator cannot stall the report.
================
*/
func (t *Ticker) watchStalls(ctx context.Context) {
	after := t.StallReport
	if after <= 0 {
		after = DefaultTickStallReport
	}
	poll := time.NewTicker(tickStallPoll)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-poll.C:
			if report := t.watch.check(now, after); report != "" {
				log.Warn(report)
			}
		}
	}
}
