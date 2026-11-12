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
	// Commit is the tick's batched authority commit (tick_commit.go).
	Commit TickCommitStats
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
	// Zone, when set, records each step as a zone on the tick's
	// timeline (ticktrace.go, #2301); nil - the default - records
	// none and costs one nil check per step.
	Zone func(name string) func()
	// Total, when set, receives every step's elapsed time, slow or
	// not: the always-on zone ledger's step rows (zone_ledger.go,
	// #2824). Nil - the default - reports none.
	Total func(name string, elapsed time.Duration)
}

/*
================
StepTimer.Time

Runs one step, totals it when Total is set and reports it at or above
SlowHookThreshold, including when it panics (the hook's recover boundary
is the ticker's).
================
*/
func (s *StepTimer) Time(name string, run func()) {
	if s.Zone != nil {
		defer s.Zone(name)()
	}
	if s.Slow == nil && s.Total == nil {
		run()
		return
	}
	now := s.Now
	if now == nil {
		now = time.Now
	}
	started := now()
	defer func() {
		elapsed := now().Sub(started)
		if s.Total != nil {
			s.Total(name, elapsed)
		}
		if s.Slow != nil && elapsed >= SlowHookThreshold {
			s.Slow(name, elapsed)
		}
	}()
	run()
}

/*
================
StepSpan

One step opened by Begin: the timing Time would record, carried in a
plain value instead of behind a func(). Time's run parameter boxes its
captures on every call - inside the action TickHook that was two heap
boxes per step per tick, the tick profile's largest single flat root
(#2030) - while a span begun and ended by value allocates nothing.
The records are identical: Total hears every step, Slow hears the ones
at or above SlowHookThreshold, the Zone opens at Begin and closes
after the record, exactly Time's deferred order, and an End deferred
by the caller keeps a panicking step's record the way Time's own
defer does.
================
*/
type StepSpan struct {
	timer   *StepTimer
	name    string
	now     func() time.Time
	started time.Time
	zoneEnd func()
	live    bool
}

/*
================
StepTimer.Begin

Opens a step span the caller ends once with End. A timer with nothing
wired (or only a Zone) records no timing; End still closes the zone.
================
*/
func (s *StepTimer) Begin(name string) StepSpan {
	span := StepSpan{timer: s, name: name}
	if s.Zone != nil {
		span.zoneEnd = s.Zone(name)
	}
	if s.Slow == nil && s.Total == nil {
		return span
	}
	now := s.Now
	if now == nil {
		now = time.Now
	}
	span.now, span.started, span.live = now, now(), true
	return span
}

/*
================
StepSpan.End

Closes the span: the timing record first, then the zone, Time's order.
End records at most once, so a caller may defer it as a panic backstop
beside an explicit end - the deferred call then keeps a panicking
step's record (and closes its zone) exactly as Time's own defer did,
and the explicit end on the calm path keeps the wait out of the spans
that follow.
================
*/
func (sp *StepSpan) End() {
	if sp.timer == nil {
		return
	}
	if sp.live {
		sp.live = false
		elapsed := sp.now().Sub(sp.started)
		if sp.timer.Total != nil {
			sp.timer.Total(sp.name, elapsed)
		}
		if sp.timer.Slow != nil && elapsed >= SlowHookThreshold {
			sp.timer.Slow(sp.name, elapsed)
		}
	}
	if sp.zoneEnd != nil {
		zoneEnd := sp.zoneEnd
		sp.zoneEnd = nil
		zoneEnd()
	}
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
	commit        TickCommitStats
}

/*
================
tickClock.begin
================
*/
func (c *tickClock) begin(now time.Time) {
	c.start = now
	c.inDivisions, c.inHooks = false, false
	c.commit = TickCommitStats{}
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
record is deferred inside the caller's recover boundary. The recorded
name is the hook's registered one (NamedTickHook); an unnamed hook
keeps the runtime name it had before hooks carried names.
================
*/
func (c *tickClock) timeHook(hook NamedTickHook, now func() time.Time, run func()) {
	started := now()
	defer func() {
		if elapsed := now().Sub(started); elapsed >= SlowHookThreshold {
			name := hook.Name
			if name == "" {
				name = "unknown"
				if fn := runtime.FuncForPC(reflect.ValueOf(hook.Hook).Pointer()); fn != nil {
					name = fn.Name()
				}
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
		Commit:      c.commit,
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
	timing := t.clock.timing(t.timingNow())
	if t.PhaseObserver != nil {
		t.PhaseObserver(timing)
	}
	t.traceFinish(timing)
	budget := t.Interval
	if budget <= 0 {
		budget = DefaultTickInterval
	}
	t.flight.record(timing, t.tickPlayers, t.flight.gcPauseDelta(), t.timingNow(), budget)
}
