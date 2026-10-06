/*
===========================================================================

tick_timing.go - per-phase tick durations and slow hooks, every tick

The watchdog (tick_watchdog.go) reports ticks past a second; a 100-300 ms
tick already shows as a snap at 60 Hz movement and never trips it. The
coordinator records when each phase ends and how long each hook ran, and
hands one TickTiming per tick to an optional observer (the transport's
histograms, wired by worldsession). Hook names are resolved only for hooks
at or above SlowHookThreshold.

===========================================================================
*/

package simulation

import (
	"reflect"
	"runtime"
	"time"
)

// SlowHookThreshold is the hook duration named in TickTiming.SlowHooks.
const SlowHookThreshold = 100 * time.Millisecond

/*
================
SlowHookTiming
================
*/
type SlowHookTiming struct {
	Name    string
	Elapsed time.Duration
}

/*
================
TickTiming

One coordinated tick: the time spent in each phase and in total, and the
hooks that ran at or above SlowHookThreshold.
================
*/
type TickTiming struct {
	BeforeHooks, Divisions, Hooks, Total time.Duration
	SlowHooks                            []SlowHookTiming
}

/*
================
tickClock

Coordinator-owned: written and read only by the goroutine running the tick.
================
*/
type tickClock struct {
	start, beforeHooksEnd, divisionsEnd time.Time
	slow                                []SlowHookTiming
}

/*
================
tickClock.begin
================
*/
func (c *tickClock) begin(now time.Time) {
	c.start, c.beforeHooksEnd, c.divisionsEnd = now, now, now
	c.slow = c.slow[:0]
}

/*
================
tickClock.timeHook

Runs one hook and keeps it when it was slow.
================
*/
func (c *tickClock) timeHook(hook TickHook, run func()) {
	started := time.Now()
	run()
	if elapsed := time.Since(started); elapsed >= SlowHookThreshold {
		name := "unknown"
		if fn := runtime.FuncForPC(reflect.ValueOf(hook).Pointer()); fn != nil {
			name = fn.Name()
		}
		c.slow = append(c.slow, SlowHookTiming{Name: name, Elapsed: elapsed})
	}
}

/*
================
tickClock.timing
================
*/
func (c *tickClock) timing(end time.Time) TickTiming {
	timing := TickTiming{
		BeforeHooks: c.beforeHooksEnd.Sub(c.start),
		Divisions:   c.divisionsEnd.Sub(c.beforeHooksEnd),
		Hooks:       end.Sub(c.divisionsEnd),
		Total:       end.Sub(c.start),
	}
	if len(c.slow) > 0 {
		timing.SlowHooks = append([]SlowHookTiming(nil), c.slow...)
	}
	return timing
}

/*
================
Ticker.observePhases
================
*/
func (t *Ticker) observePhases() {
	if t.PhaseObserver != nil {
		t.PhaseObserver(t.clock.timing(time.Now()))
	}
}
