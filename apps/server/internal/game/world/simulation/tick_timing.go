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
	"sync"
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
	// Divisions whose own work ran >= SlowHookThreshold; scheduled ticks run
	// divisions in parallel shards, so the phase alone cannot name them.
	SlowDivisions []SlowHookTiming
}

/*
================
StepTimer

Names the slow steps inside one hook that owns many subsystems (the action
runtime's TickHook runs ~45): the hook's own slow_hooks entry says that it
was slow, the steps say where. The zero value times against the wall clock
and reports nothing. Written and read only on the tick goroutine.
================
*/
type StepTimer struct {
	Now  func() time.Time
	Slow func(name string, elapsed time.Duration)
}

/*
================
StepTimer.Time

Runs one step and reports it at or above SlowHookThreshold, including when
it panics (the hook's recover boundary is the ticker's).
================
*/
func (s *StepTimer) Time(name string, run func()) {
	if s.Slow == nil {
		run()
		return
	}
	now := s.Now
	if now == nil {
		now = time.Now
	}
	started := now()
	defer func() {
		if elapsed := now().Sub(started); elapsed >= SlowHookThreshold {
			s.Slow(name, elapsed)
		}
	}()
	run()
}

/*
================
tickClock

Coordinator-owned: written and read only by the goroutine running the tick.
A phase that was never entered (a cancelled or panicking tick) ends with
the tick, so no phase duration is negative.
================
*/
type tickClock struct {
	start, divisionsStart, hooksStart time.Time
	inDivisions, inHooks              bool
	slow                              []SlowHookTiming
	// Shard workers report divisions concurrently.
	divisionsMu   sync.Mutex
	slowDivisions []SlowHookTiming
}

/*
================
tickClock.begin
================
*/
func (c *tickClock) begin(now time.Time) {
	c.start = now
	c.inDivisions, c.inHooks = false, false
	c.slow = c.slow[:0]
	c.divisionsMu.Lock()
	c.slowDivisions = c.slowDivisions[:0]
	c.divisionsMu.Unlock()
}

/*
================
tickClock.timeDivision

Called by each division's worker when its work for the tick ends.
================
*/
func (c *tickClock) timeDivision(divisionID string, elapsed time.Duration) {
	if elapsed < SlowHookThreshold {
		return
	}
	c.divisionsMu.Lock()
	c.slowDivisions = append(c.slowDivisions, SlowHookTiming{Name: divisionID, Elapsed: elapsed})
	c.divisionsMu.Unlock()
}

/*
================
tickClock.enterDivisions
================
*/
func (c *tickClock) enterDivisions(now time.Time) {
	c.divisionsStart, c.inDivisions = now, true
}

/*
================
tickClock.enterHooks
================
*/
func (c *tickClock) enterHooks(now time.Time) {
	c.hooksStart, c.inHooks = now, true
}

/*
================
tickClock.timeHook

Runs one hook and keeps it when it was slow, including when it panics: the
record is deferred inside the caller's recover boundary.
================
*/
func (c *tickClock) timeHook(hook TickHook, now func() time.Time, run func()) {
	started := now()
	defer func() {
		if elapsed := now().Sub(started); elapsed >= SlowHookThreshold {
			name := "unknown"
			if fn := runtime.FuncForPC(reflect.ValueOf(hook).Pointer()); fn != nil {
				name = fn.Name()
			}
			c.slow = append(c.slow, SlowHookTiming{Name: name, Elapsed: elapsed})
		}
	}()
	run()
}

/*
================
tickClock.timing
================
*/
func (c *tickClock) timing(end time.Time) TickTiming {
	divisionsStart, hooksStart := end, end
	if c.inDivisions {
		divisionsStart = c.divisionsStart
	}
	if c.inHooks {
		hooksStart = c.hooksStart
	}
	if !c.inDivisions && c.inHooks {
		divisionsStart = hooksStart
	}
	timing := TickTiming{
		BeforeHooks: divisionsStart.Sub(c.start),
		Divisions:   hooksStart.Sub(divisionsStart),
		Hooks:       end.Sub(hooksStart),
		Total:       end.Sub(c.start),
	}
	if len(c.slow) > 0 {
		timing.SlowHooks = append([]SlowHookTiming(nil), c.slow...)
	}
	c.divisionsMu.Lock()
	if len(c.slowDivisions) > 0 {
		timing.SlowDivisions = append([]SlowHookTiming(nil), c.slowDivisions...)
	}
	c.divisionsMu.Unlock()
	return timing
}

/*
================
Ticker.timingNow

The tick-timing clock: Now when a test supplies one, else the wall clock.
================
*/
func (t *Ticker) timingNow() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

/*
================
Ticker.observePhases
================
*/
func (t *Ticker) observePhases() {
	if t.PhaseObserver != nil {
		t.PhaseObserver(t.clock.timing(t.timingNow()))
	}
}
