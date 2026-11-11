/*
===========================================================================

tick_flight.go - the tick flight recorder: the last ticks, dumped on an over-budget tick (#3226)

A slow tick on the live server used to leave only histogram buckets
(transport/timing.go) and a "tick slow" line (tick_watchdog.go): by
the time anyone looked, which step ate the tick was gone. This
recorder keeps the last flightRingSize ticks in a preallocated ring -
each tick's phase durations, its individually named slow steps (the
hooks and divisions tick_timing.go already times at or above
SlowHookThreshold), the player count and the GC pause spent in the
tick - and when a tick's total exceeds the tick budget it writes the
whole window, the slow tick last, to the GameWorld log as one
structured line. Dumps are rate-limited, so a storm of slow ticks
cannot flood the log.

Recording is a few stores into the ring slot the tick reuses: no
allocation on the steady path (BenchmarkTickFlightRecord pins 0
allocs/op, GC sample included). Formatting happens only on the dump
path. Every step's exact cumulative total stays the zone ledger's
job (zone_ledger.go, #2824); the ring keeps what a dump must name.

Port-only diagnostics: no native rule applies, and the recorder is
always on because the benchmark shows it costs nothing measurable,
so no server flag gates it (#3226).

Written and read only on the tick goroutine (observePhases), so the
ring needs no lock.

===========================================================================
*/

package simulation

import (
	"fmt"
	"math"
	"runtime/metrics"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	// flightRingSize is how many ticks the recorder keeps: enough to
	// show the run-up to a slow tick, small enough that a dump stays
	// one readable log line.
	flightRingSize = 32
	// flightSlowStepCap bounds the named slow steps kept per tick;
	// a tick with more slow steps than this is already named by its
	// slowest ones, which sort first in TickTiming's lists.
	flightSlowStepCap = 8
	// flightDumpMinInterval rate-limits dumps: at most one window
	// per interval however many ticks go over budget inside it.
	flightDumpMinInterval = 10 * time.Second
)

/*
================
flightTick

One recorded tick: its phase durations, the GC pause spent in it,
how many players it served and the slow steps it named. A plain
value in the ring, overwritten in place each time its slot comes
round.
================
*/
type flightTick struct {
	seq          int64
	total        time.Duration
	before       time.Duration
	divisions    time.Duration
	hooks        time.Duration
	commit       time.Duration
	gcPause      time.Duration
	players      int
	steps        [flightSlowStepCap]SlowHookTiming
	stepCount    int
	stepsDropped int
}

/*
================
tickFlightRecorder

The rolling recorder. The zero value is ready: the ring is inline,
the GC sample is armed on first use and the dump goes to the log
unless a test installs emit.
================
*/
type tickFlightRecorder struct {
	ring     [flightRingSize]flightTick
	next     int
	filled   int
	seq      int64
	lastDump time.Time
	emit     func(string)

	gcSamples [1]metrics.Sample
	gcArmed   bool
	gcPrev    float64
}

/*
================
tickFlightRecorder.gcPauseDelta

The GC pause spent since the previous call, from the same runtime
histogram the monster leg prices its legs with (monsterleg_stats.go).
The sample slice is the recorder's own, reused every tick, so the
steady path allocates nothing. The first call only arms the
baseline and reports zero.
================
*/
func (r *tickFlightRecorder) gcPauseDelta() time.Duration {
	if !r.gcArmed {
		r.gcSamples[0].Name = gcPausesMetric
		r.gcArmed = true
		metrics.Read(r.gcSamples[:])
		r.gcPrev = gcPauseSeconds(r.gcSamples[0])
		return 0
	}
	metrics.Read(r.gcSamples[:])
	cur := gcPauseSeconds(r.gcSamples[0])
	r.gcPrev, cur = cur, cur-r.gcPrev
	if cur < 0 {
		cur = 0
	}
	return time.Duration(cur * float64(time.Second))
}

/*
================
gcPauseSeconds

One cumulative GC pause total, in seconds: the histogram's midpoint
estimate, the top bucket priced at its lower edge. It attributes a
tick, it does not audit the collector.
================
*/
func gcPauseSeconds(sample metrics.Sample) float64 {
	if sample.Value.Kind() != metrics.KindFloat64Histogram {
		return 0
	}
	h := sample.Value.Float64Histogram()
	var total float64
	for i, count := range h.Counts {
		if count == 0 {
			continue
		}
		mid := h.Buckets[i]
		if hi := h.Buckets[i+1]; !math.IsInf(hi, 1) {
			mid = (mid + hi) / 2
		}
		total += float64(count) * mid
	}
	return total
}

/*
================
tickFlightRecorder.record

Stores one finished tick in the ring and, when the tick ran over
budget and the rate limit allows, dumps the window. Called from
observePhases on the tick goroutine. Stores only: nothing here
allocates unless a dump fires.
================
*/
func (r *tickFlightRecorder) record(timing TickTiming, players int, gcPause time.Duration, now time.Time, budget time.Duration) {
	slot := &r.ring[r.next]
	r.next = (r.next + 1) % flightRingSize
	if r.filled < flightRingSize {
		r.filled++
	}
	r.seq++
	slot.seq = r.seq
	slot.total = timing.Total
	slot.before = timing.BeforeHooks
	slot.divisions = timing.Divisions
	slot.hooks = timing.Hooks
	slot.commit = timing.Commit.Elapsed
	slot.gcPause = gcPause
	slot.players = players
	slot.stepCount = 0
	slot.stepsDropped = 0
	for _, step := range timing.SlowHooks {
		slot.addStep(step)
	}
	for _, step := range timing.SlowDivisions {
		// A division's name is its id; say so, the way the tick
		// timeline names it (ticktrace.go).
		slot.addStep(SlowHookTiming{Name: "division:" + step.Name, Elapsed: step.Elapsed})
	}

	if timing.Total <= budget {
		return
	}
	if !r.lastDump.IsZero() && now.Sub(r.lastDump) < flightDumpMinInterval {
		return
	}
	r.lastDump = now
	emit := r.emit
	if emit == nil {
		emit = flightLogDump
	}
	emit(r.formatWindow(budget))
}

/*
================
flightTick.addStep

Keeps one named slow step while the slot has room; the rest are
counted, so the dump can say it dropped some.
================
*/
func (s *flightTick) addStep(step SlowHookTiming) {
	if s.stepCount == flightSlowStepCap {
		s.stepsDropped++
		return
	}
	s.steps[s.stepCount] = step
	s.stepCount++
}

/*
================
tickFlightRecorder.formatWindow

The dump itself: one line, the recorded window oldest first, the
slow tick last. Runs only when a dump fires, so it may allocate.
================
*/
func (r *tickFlightRecorder) formatWindow(budget time.Duration) string {
	slow := r.ring[(r.next+flightRingSize-1)%flightRingSize]
	var b strings.Builder
	fmt.Fprintf(&b, "simulation: tick flight dump: tick #%d total=%v over budget=%v; last %d ticks:",
		slow.seq, slow.total.Round(time.Microsecond), budget, r.filled)
	for i := 0; i < r.filled; i++ {
		slot := &r.ring[(r.next-r.filled+i+flightRingSize)%flightRingSize]
		fmt.Fprintf(&b, " [#%d total=%v players=%d gc=%v before=%v divisions=%v hooks=%v commit=%v",
			slot.seq, slot.total.Round(time.Microsecond), slot.players,
			slot.gcPause.Round(time.Microsecond), slot.before.Round(time.Microsecond),
			slot.divisions.Round(time.Microsecond), slot.hooks.Round(time.Microsecond),
			slot.commit.Round(time.Microsecond))
		if slot.stepCount > 0 {
			b.WriteString(" slow={")
			for j := 0; j < slot.stepCount; j++ {
				if j > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(&b, "%s:%v", slot.steps[j].Name, slot.steps[j].Elapsed.Round(time.Microsecond))
			}
			if slot.stepsDropped > 0 {
				fmt.Fprintf(&b, ", +%d more", slot.stepsDropped)
			}
			b.WriteString("}")
		}
		b.WriteString("]")
	}
	return b.String()
}

/*
================
flightLogDump

The production dump: one structured line to the GameWorld log, in
the voice of the watchdog's slow-tick line (tick_watchdog.go).
================
*/
func flightLogDump(line string) {
	log.Warn(line)
}
