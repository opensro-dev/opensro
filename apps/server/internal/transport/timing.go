/*
===========================================================================

timing.go - how long requests and simulation ticks hold the server

A session's frames are read and dispatched on one goroutine, PING included,
so a handler that waits on a lock delays every later frame and heartbeat of
that client; a slow simulation tick delays every client's replies. The
watchdog (simulation/tick_watchdog.go) names ticks longer than a second;
these histograms show the 16-1000 ms band a player already sees as a snap
at 60 Hz movement. Buckets are fixed and maps are bounded: handlers by the
registered opcodes, slow hooks by hook function names.

===========================================================================
*/

package transport

import (
	"fmt"
	"sync"
	"time"
)

// Upper bounds of the histogram buckets; one more bucket holds the rest.
var timingBucketsMs = [...]float64{16, 50, 100, 250, 1000}

/*
================
Histogram

Counts per bucket (<=16, <=50, <=100, <=250, <=1000, >1000 ms) and the
largest sample.
================
*/
type Histogram struct {
	Buckets [len(timingBucketsMs) + 1]uint64 `json:"buckets"`
	MaxMs   float64                          `json:"max_ms"`
}

/*
================
Histogram.add
================
*/
func (h *Histogram) add(elapsed time.Duration) {
	ms := float64(elapsed) / float64(time.Millisecond)
	slot := len(timingBucketsMs)
	for i, bound := range timingBucketsMs {
		if ms <= bound {
			slot = i
			break
		}
	}
	h.Buckets[slot]++
	if ms > h.MaxMs {
		h.MaxMs = ms
	}
}

/*
================
SlowHook

One tick hook the simulation ticker reported as slow (>= 100 ms).
================
*/
type SlowHook struct {
	Name    string
	Elapsed time.Duration
}

/*
================
TickPhases

One tick's phase durations as the simulation ticker measured them.
================
*/
type TickPhases struct {
	BeforeHooks, Divisions, Hooks, Total time.Duration
	SlowHooks                            []SlowHook
}

/*
================
SlowHookStats
================
*/
type SlowHookStats struct {
	Count uint64  `json:"count"`
	MaxMs float64 `json:"max_ms"`
}

/*
================
timingStats

Recorded at most once per frame and once per 100 ms tick; one mutex keeps
each snapshot coherent.
================
*/
type timingStats struct {
	mu        sync.Mutex
	handlers  map[uint16]*Histogram
	phases    [4]Histogram
	slowHooks map[string]*SlowHookStats
}

/*
================
timingStats.recordHandler
================
*/
func (t *timingStats) recordHandler(opcode uint16, elapsed time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.handlers == nil {
		t.handlers = make(map[uint16]*Histogram)
	}
	h := t.handlers[opcode]
	if h == nil {
		h = &Histogram{}
		t.handlers[opcode] = h
	}
	h.add(elapsed)
}

/*
================
RecordTickPhases

The simulation ticker's per-tick seam (wired by worldsession.NewTicker).
================
*/
func (h *Hub) RecordTickPhases(phases TickPhases) {
	t := &h.metrics.timing
	t.mu.Lock()
	defer t.mu.Unlock()
	for i, elapsed := range [4]time.Duration{phases.BeforeHooks, phases.Divisions, phases.Hooks, phases.Total} {
		t.phases[i].add(elapsed)
	}
	if len(phases.SlowHooks) > 0 && t.slowHooks == nil {
		t.slowHooks = make(map[string]*SlowHookStats)
	}
	for _, hook := range phases.SlowHooks {
		stats := t.slowHooks[hook.Name]
		if stats == nil {
			stats = &SlowHookStats{}
			t.slowHooks[hook.Name] = stats
		}
		stats.Count++
		if ms := float64(hook.Elapsed) / float64(time.Millisecond); ms > stats.MaxMs {
			stats.MaxMs = ms
		}
	}
}

/*
================
timingStats.snapshotInto
================
*/
func (t *timingStats) snapshotInto(m *Metrics) {
	t.mu.Lock()
	defer t.mu.Unlock()
	m.HandlerMs = make(map[string]Histogram, len(t.handlers))
	for opcode, h := range t.handlers {
		m.HandlerMs[fmt.Sprintf("0x%04X", opcode)] = *h
	}
	m.TickPhaseMs = map[string]Histogram{
		"before_hooks": t.phases[0], "divisions": t.phases[1], "hooks": t.phases[2], "total": t.phases[3],
	}
	m.SlowHooks = make(map[string]SlowHookStats, len(t.slowHooks))
	for name, stats := range t.slowHooks {
		m.SlowHooks[name] = *stats
	}
}
