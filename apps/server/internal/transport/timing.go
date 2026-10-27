/*
===========================================================================

timing.go - how long requests and simulation ticks hold the server

A session's frames are read and dispatched on one goroutine, PING included,
so a handler that waits on a lock delays every later frame and heartbeat of
that client; a slow simulation tick delays every client's replies. The
watchdog (simulation/tick_watchdog.go) names ticks longer than a second;
these histograms show the 1-1000 ms band, fine below 16 ms (a frame at
60 Hz) so a handler or a tick phase can be held to a few milliseconds.
Buckets are fixed and maps are bounded: handlers by the registered opcodes,
slow hooks by hook function names.

===========================================================================
*/

package transport

import (
	"fmt"
	"sync"
	"time"

	"opensro.online/server/internal/platform/latency"
)

// Histogram is the snapshot form served on /transport/metrics, and
// liveHistogram its lock-free recording form; both live in
// platform/latency, which the authority store's lock timing shares.
type Histogram = latency.Histogram

type liveHistogram = latency.Live

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
	Commit                               TickCommit
}

/*
================
TickCommit

One tick's batched authority commit: the deferred door calls, whether a
flush transaction ran, the rows it wrote, its duration and whether it
failed.
================
*/
type TickCommit struct {
	Deferred, Rows  int
	Elapsed         time.Duration
	Flushed, Failed bool
}

/*
================
TickCommitStats

The batched commits since start, served on /transport/metrics: flushes that
ran a transaction, the rows and deferred door calls they covered, the
largest batch, and failed flushes.
================
*/
type TickCommitStats struct {
	Flushes  uint64 `json:"flushes"`
	Rows     uint64 `json:"rows"`
	Deferred uint64 `json:"deferred"`
	MaxRows  int    `json:"max_rows"`
	Failures uint64 `json:"failures"`
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
	// a boot-time cluster can be told from a recurring stall. The tick value
	// is tick_count at the moment of recording: a step inside a hook is
	// recorded mid-tick, before that tick's duration sample counts it, and
	// the hook itself at the tick's end, after it. A step and its hook from
	// the same tick therefore read N-1 and N.
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
	commitMs liveHistogram
	mu       sync.Mutex
	commit   TickCommitStats
	slow     map[string]*SlowHookStats
	slowDiv  map[string]*SlowHookStats
	pubHolds map[string]*SlowHookStats
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
	h.(*liveHistogram).Add(elapsed)
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
		t.phases[i].Add(elapsed)
	}
	t.recordCommit(phases.Commit)
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
timingStats.recordCommit

A tick that deferred nothing ran no flush and is not counted.
================
*/
func (t *timingStats) recordCommit(commit TickCommit) {
	if commit.Deferred == 0 && !commit.Flushed {
		return
	}
	if commit.Flushed {
		t.commitMs.Add(commit.Elapsed)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.commit.Deferred += uint64(commit.Deferred)
	if !commit.Flushed {
		return
	}
	t.commit.Flushes++
	t.commit.Rows += uint64(commit.Rows)
	t.commit.MaxRows = max(t.commit.MaxRows, commit.Rows)
	if commit.Failed {
		t.commit.Failures++
	}
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
	t.ensureSlow()
	countSlow(t.slow, []SlowHook{{Name: name, Elapsed: elapsed}}, at, tick)
	t.mu.Unlock()
	if journal := h.metrics.spikes; journal != nil {
		journal.observeStep(name, elapsed)
	}
}

/*
================
RecordPublicationHold

One publication-gate hold at or above the slow threshold, under the
label of the holder that kept it (action's LockPublication callers: an
entry bootstrap, a request op, ...). It lands in publication_holds
beside slow_hooks, so a tick step that merely waited on the gate can be
told from the work that kept it - the wait itself is timed as the
tickPublicationGate slow step.
================
*/
func (h *Hub) RecordPublicationHold(label string, elapsed time.Duration) {
	t := &h.metrics.timing
	at, tick := time.Now(), h.tickCount()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ensureSlow()
	countSlow(t.pubHolds, []SlowHook{{Name: label, Elapsed: elapsed}}, at, tick)
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
	if t.pubHolds == nil {
		t.pubHolds = make(map[string]*SlowHookStats)
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
		m.HandlerMs[fmt.Sprintf("0x%04X", key.(uint16))] = value.(*liveHistogram).Snapshot()
		return true
	})
	m.TickPhaseMs = map[string]Histogram{
		"before_hooks": t.phases[0].Snapshot(), "divisions": t.phases[1].Snapshot(),
		"hooks": t.phases[2].Snapshot(), "total": t.phases[3].Snapshot(),
		"commit": t.commitMs.Snapshot(),
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	m.TickCommit = t.commit
	m.SlowHooks = make(map[string]SlowHookStats, len(t.slow))
	for name, stats := range t.slow {
		m.SlowHooks[name] = *stats
	}
	m.SlowDivisions = make(map[string]SlowHookStats, len(t.slowDiv))
	for name, stats := range t.slowDiv {
		m.SlowDivisions[name] = *stats
	}
	m.PublicationHolds = make(map[string]SlowHookStats, len(t.pubHolds))
	for label, stats := range t.pubHolds {
		m.PublicationHolds[label] = *stats
	}
}
