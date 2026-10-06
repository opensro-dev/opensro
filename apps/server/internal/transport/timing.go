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
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// Upper bounds of the histogram buckets; one more bucket holds the rest.
const (
	timingBucket0Ms = 16
	timingBucket1Ms = 50
	timingBucket2Ms = 100
	timingBucket3Ms = 250
	timingBucket4Ms = 1000
	timingBuckets   = 6
)

/*
================
Histogram

Counts per bucket (<=16, <=50, <=100, <=250, <=1000, >1000 ms) and the
largest sample; the snapshot form served on /transport/metrics.
================
*/
type Histogram struct {
	Buckets [timingBuckets]uint64 `json:"buckets"`
	MaxMs   float64               `json:"max_ms"`
}

/*
================
liveHistogram

The recording form: atomic counters, so recording on every inbound frame
takes no lock. maxBits holds the largest sample's float64 bits.
================
*/
type liveHistogram struct {
	buckets [timingBuckets]atomic.Uint64
	maxBits atomic.Uint64
}

/*
================
timingBucket
================
*/
func timingBucket(ms float64) int {
	switch {
	case ms <= timingBucket0Ms:
		return 0
	case ms <= timingBucket1Ms:
		return 1
	case ms <= timingBucket2Ms:
		return 2
	case ms <= timingBucket3Ms:
		return 3
	case ms <= timingBucket4Ms:
		return 4
	}
	return timingBuckets - 1
}

/*
================
liveHistogram.add
================
*/
func (h *liveHistogram) add(elapsed time.Duration) {
	ms := float64(elapsed) / float64(time.Millisecond)
	h.buckets[timingBucket(ms)].Add(1)
	for {
		old := h.maxBits.Load()
		if ms <= math.Float64frombits(old) || h.maxBits.CompareAndSwap(old, math.Float64bits(ms)) {
			return
		}
	}
}

/*
================
liveHistogram.snapshot
================
*/
func (h *liveHistogram) snapshot() Histogram {
	var out Histogram
	for i := range h.buckets {
		out.Buckets[i] = h.buckets[i].Load()
	}
	out.MaxMs = math.Float64frombits(h.maxBits.Load())
	return out
}

/*
================
SlowHook

One tick hook or division the simulation ticker reported as slow
(>= 100 ms).
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
	SlowHooks, SlowDivisions             []SlowHook
}

/*
================
SlowHookStats
================
*/
type SlowHookStats struct {
	Count uint64  `json:"count"`
	MaxMs float64 `json:"max_ms"`
	// When it was first and last slow, by wall clock and by tick_count, so
	// a boot-time cluster can be told from a recurring stall.
	FirstAt   time.Time `json:"first_at"`
	LastAt    time.Time `json:"last_at"`
	FirstTick uint64    `json:"first_tick"`
	LastTick  uint64    `json:"last_tick"`
}

/*
================
timingStats

Handler histograms are created once per opcode (sync.Map) and then
recorded without a lock; tick phases are fixed. Slow hooks and divisions
are rare and take the mutex.
================
*/
type timingStats struct {
	handlers sync.Map // uint16 -> *liveHistogram
	phases   [4]liveHistogram
	mu       sync.Mutex
	slow     map[string]*SlowHookStats
	slowDiv  map[string]*SlowHookStats
}

/*
================
timingStats.recordHandler
================
*/
func (t *timingStats) recordHandler(opcode uint16, elapsed time.Duration) {
	h, ok := t.handlers.Load(opcode)
	if !ok {
		h, _ = t.handlers.LoadOrStore(opcode, &liveHistogram{})
	}
	h.(*liveHistogram).add(elapsed)
}

/*
================
countSlow
================
*/
func countSlow(into map[string]*SlowHookStats, slow []SlowHook, at time.Time, tick uint64) {
	for _, hook := range slow {
		stats := into[hook.Name]
		if stats == nil {
			stats = &SlowHookStats{FirstAt: at, FirstTick: tick}
			into[hook.Name] = stats
		}
		stats.Count++
		stats.LastAt, stats.LastTick = at, tick
		if ms := float64(hook.Elapsed) / float64(time.Millisecond); ms > stats.MaxMs {
			stats.MaxMs = ms
		}
	}
}

/*
================
RecordTickPhases

The simulation ticker's per-tick seam (wired by worldsession.NewTicker).
================
*/
func (h *Hub) RecordTickPhases(phases TickPhases) {
	t := &h.metrics.timing
	for i, elapsed := range [4]time.Duration{phases.BeforeHooks, phases.Divisions, phases.Hooks, phases.Total} {
		t.phases[i].add(elapsed)
	}
	if len(phases.SlowHooks) == 0 && len(phases.SlowDivisions) == 0 {
		return
	}
	at, tick := time.Now(), h.tickCount()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ensureSlow()
	countSlow(t.slow, phases.SlowHooks, at, tick)
	countSlow(t.slowDiv, phases.SlowDivisions, at, tick)
}

/*
================
RecordSlowStep

A named step inside a hook (simulation.StepTimer) that ran at or above the
slow threshold. It lands in slow_hooks beside the hook that contains it.
================
*/
func (h *Hub) RecordSlowStep(name string, elapsed time.Duration) {
	t := &h.metrics.timing
	at, tick := time.Now(), h.tickCount()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ensureSlow()
	countSlow(t.slow, []SlowHook{{Name: name, Elapsed: elapsed}}, at, tick)
}

/*
================
timingStats.ensureSlow
================
*/
func (t *timingStats) ensureSlow() {
	if t.slow == nil {
		t.slow, t.slowDiv = make(map[string]*SlowHookStats), make(map[string]*SlowHookStats)
	}
}

/*
================
Hub.tickCount

The ticks recorded so far; a slow sample's position in the server's life.
================
*/
func (h *Hub) tickCount() uint64 {
	t := &h.metrics.ticks
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.count
}

/*
================
timingStats.snapshotInto
================
*/
func (t *timingStats) snapshotInto(m *Metrics) {
	m.HandlerMs = make(map[string]Histogram)
	t.handlers.Range(func(key, value any) bool {
		m.HandlerMs[fmt.Sprintf("0x%04X", key.(uint16))] = value.(*liveHistogram).snapshot()
		return true
	})
	m.TickPhaseMs = map[string]Histogram{
		"before_hooks": t.phases[0].snapshot(), "divisions": t.phases[1].snapshot(),
		"hooks": t.phases[2].snapshot(), "total": t.phases[3].snapshot(),
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	m.SlowHooks = make(map[string]SlowHookStats, len(t.slow))
	for name, stats := range t.slow {
		m.SlowHooks[name] = *stats
	}
	m.SlowDivisions = make(map[string]SlowHookStats, len(t.slowDiv))
	for name, stats := range t.slowDiv {
		m.SlowDivisions[name] = *stats
	}
}
