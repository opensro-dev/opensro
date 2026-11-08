/*
===========================================================================

trace.go - execution-trace reduction for the tick goroutines

The trace is the second source of truth beside the zone ledger: it
sees the scheduler, so it can say how long the tick goroutines
actually ran, how long they sat runnable, and how long they blocked -
the three numbers that expose a misread profile (the 2026-11-07 hand
analysis called 17 ms of runnable wait over 51 ticks "27 ms per
tick"). Tick goroutines are the ones whose stacks pass through
Ticker.runScheduledTick or Ticker.runDivision.

Goroutine states are integrated from state-transition events: the
elapsed time between two transitions belongs to the state the first
one entered. Running and syscall time both count as running (the
state's own Executing definition); waiting counts as blocked.

===========================================================================
*/

package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"golang.org/x/exp/trace"
)

// Tick goroutine markers: a goroutine belongs to the tick when any of
// its stacks runs one of the two tick entries.
var tickStackMarkers = []string{"Ticker).runScheduledTick", "Ticker).runDivision"}

/*
================
stackCount

One stack text and how many transitions entered a state on it.
================
*/
type stackCount struct {
	Stack string
	Count int
}

/*
================
traceStats

The tick goroutines' scheduler truth over one trace, in total
milliseconds; the caller divides by the trace window's tick count.
================
*/
type traceStats struct {
	Goroutines  int
	DurationMs  float64
	RunningMs   float64
	SyscallMs   float64
	RunnableMs  float64
	BlockedMs   float64
	topBlocking []stackCount
	topRunnable []stackCount
}

/*
================
stackHasTickMarker
================
*/
func stackHasTickMarker(stack trace.Stack) bool {
	for frame := range stack.Frames() {
		for _, marker := range tickStackMarkers {
			if strings.Contains(frame.Func, marker) {
				return true
			}
		}
	}
	return false
}

/*
================
stackText

A short stack label for the top-state tables: the first few function
names, leaf first as the trace records them.
================
*/
func stackText(stack trace.Stack, limit int) string {
	var names []string
	for frame := range stack.Frames() {
		names = append(names, frame.Func)
		if len(names) >= limit {
			break
		}
	}
	return strings.Join(names, " <- ")
}

/*
================
findTickGoroutines

First pass: which goroutines ever run a tick entry, and the trace's
time span.
================
*/
func findTickGoroutines(path string) (map[trace.GoID]bool, trace.Time, trace.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	defer f.Close()
	reader, err := trace.NewReader(f)
	if err != nil {
		return nil, 0, 0, err
	}
	tick := make(map[trace.GoID]bool)
	var first, last trace.Time
	for {
		ev, err := reader.ReadEvent()
		if err != nil {
			break
		}
		if first == 0 || ev.Time() < first {
			first = ev.Time()
		}
		if ev.Time() > last {
			last = ev.Time()
		}
		if tick[ev.Goroutine()] {
			continue
		}
		if stackHasTickMarker(ev.Stack()) {
			tick[ev.Goroutine()] = true
			continue
		}
		if ev.Kind() == trace.EventStateTransition {
			if st := ev.StateTransition(); st.Resource.Kind == trace.ResourceGoroutine && stackHasTickMarker(st.Stack) {
				tick[st.Resource.Goroutine()] = true
			}
		}
	}
	return tick, first, last, nil
}

/*
================
reduceTrace

Second pass: integrate each tick goroutine's states over the trace.
The state a goroutine is in when first seen is credited from the
trace start; the state it is in when the trace ends is credited to
the trace end.
================
*/
func reduceTrace(path string) (*traceStats, error) {
	tick, first, last, err := findTickGoroutines(path)
	if err != nil {
		return nil, err
	}
	if len(tick) == 0 {
		return nil, fmt.Errorf("%s: no goroutine stack passes through a tick entry (%s)", path, strings.Join(tickStackMarkers, ", "))
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	type goroutineClock struct {
		state trace.GoState
		since trace.Time
		known bool
	}
	clocks := make(map[trace.GoID]*goroutineClock)
	blocking := make(map[string]int)
	runnable := make(map[string]int)
	stats := &traceStats{Goroutines: len(tick), DurationMs: float64(last-first) / 1e6}

	credit := func(clock *goroutineClock, until trace.Time) {
		if !clock.known || until <= clock.since {
			return
		}
		ms := float64(until-clock.since) / 1e6
		switch clock.state {
		case trace.GoRunning:
			stats.RunningMs += ms
		case trace.GoSyscall:
			stats.SyscallMs += ms
		case trace.GoRunnable:
			stats.RunnableMs += ms
		case trace.GoWaiting:
			stats.BlockedMs += ms
		}
	}

	reader, err := trace.NewReader(f)
	if err != nil {
		return nil, err
	}
	for {
		ev, err := reader.ReadEvent()
		if err != nil {
			break
		}
		if ev.Kind() != trace.EventStateTransition {
			continue
		}
		st := ev.StateTransition()
		if st.Resource.Kind != trace.ResourceGoroutine {
			continue
		}
		id := st.Resource.Goroutine()
		if !tick[id] {
			continue
		}
		from, to := st.Goroutine()
		clock := clocks[id]
		if clock == nil {
			clock = &goroutineClock{state: from, since: first, known: true}
			clocks[id] = clock
		}
		credit(clock, ev.Time())
		clock.state = to
		clock.since = ev.Time()
		switch to {
		case trace.GoWaiting:
			if text := stackText(st.Stack, 4); text != "" {
				blocking[text]++
			}
		case trace.GoRunnable:
			if text := stackText(st.Stack, 4); from != trace.GoNotExist && text != "" {
				runnable[text]++
			}
		}
	}
	for _, clock := range clocks {
		credit(clock, last)
	}
	stats.topBlocking = topStackCounts(blocking, 5)
	stats.topRunnable = topStackCounts(runnable, 5)
	return stats, nil
}

/*
================
topStackCounts
================
*/
func topStackCounts(counts map[string]int, limit int) []stackCount {
	out := make([]stackCount, 0, len(counts))
	for stack, count := range counts {
		out = append(out, stackCount{Stack: stack, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Stack < out[j].Stack
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

/*
================
runningMs

Running time with syscall time folded in: both are the goroutine
executing on a thread.
================
*/
func (s *traceStats) runningMs() float64 {
	return s.RunningMs + s.SyscallMs
}
