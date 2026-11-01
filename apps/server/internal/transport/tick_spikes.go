/*
===========================================================================

tick_spikes.go - the tick-overrun spike journal: burst records on disk

The live tick aggregates on /transport/metrics only describe the recent
past, so a stall that happened minutes ago - mid gameplay, reported after
the fact - cannot be reconstructed. This journal keeps the evidence:
every tick at or above the spike threshold is coalesced into a burst and
one JSON line per burst is appended to a file in the GameWorld state root.

The tick never blocks on disk. Burst records go through a bounded queue
to one writer goroutine; a full queue drops the record and counts the
drop, and a failed write counts instead of retrying. Both counters and a
mirror of the last bursts ride the existing metrics snapshot, so the
journal can be watched live as well as read later. Diagnostics only: no
wire, store or protocol surface. SRO_TICK_SPIKE_JOURNAL=off disables it.

Attribution (#2295): a record names the phase (before_hooks, divisions,
hooks, commit - the metrics' vocabulary) and the slow hook or division
that took the burst's time, folded from the same per-tick TickPhases the
histograms eat. The named step inside one hook is finer detail beside
them, never the headline: it is reported selectively (a gate cut reports
a nominal 5 ms), so a burst that spent its seconds in hook work must not
read as caused by a 5 ms step. A burst no phase report ever reached
carries neither field - unattributed, never blamed on a step.

===========================================================================
*/

package transport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// EnvTickSpikeJournal is the spike journal kill switch: "off" disables
// the journal, anything else (including unset) leaves it on.
const EnvTickSpikeJournal = "SRO_TICK_SPIKE_JOURNAL"

// TickSpikeFileName is the journal file inside the GameWorld state root.
const TickSpikeFileName = "tick-spikes.jsonl"

const (
	// A tick at or above this is a spike: 2.5x the 100 ms tick budget,
	// and the top interior edge of the live tick histogram.
	tickSpikeThreshold = 250 * time.Millisecond
	// A burst closes once no spiked tick has landed for this long.
	tickSpikeGap = 5 * time.Second
	// Bursts kept on the live /transport/metrics mirror.
	tickSpikeMirrorMax = 32
	// Burst records buffered for the writer before drops are counted.
	tickSpikeQueueCap = 64
	// The live file rotates to ".1" past this size; the pair of files
	// together stays under the 8 MiB journal budget.
	tickSpikeFileCap = 4 << 20
)

/*
================
TickSpike

One coalesced burst of tick overruns. StartTime is the first spiked
tick, EndTime the last one. The *_end fields snapshot the live tick
aggregates at the moment the burst closed, so a reader can place the
burst in the server's life without a second source.
================
*/
type TickSpike struct {
	Version       int       `json:"v"`
	StartTime     time.Time `json:"ts_start"`
	EndTime       time.Time `json:"ts_end"`
	Ticks         uint64    `json:"ticks_in_burst"`
	MaxMs         float64   `json:"max_ms"`
	MeanMs        float64   `json:"mean_ms"`
	IntervalMs    float64   `json:"interval_ms"`
	ThresholdMs   float64   `json:"threshold_ms"`
	TickCountEnd  uint64    `json:"tick_count_end"`
	OverrunsEnd   uint64    `json:"overruns_end"`
	P99MsEnd      float64   `json:"p99_ms_end"`
	SlowestStep   string    `json:"slowest_step,omitempty"`
	SlowestStepMs float64   `json:"slowest_step_ms,omitempty"`
	// The phase and hook that took the burst's time (#2295), folded from
	// the burst ticks' TickPhases; both absent when no phase report
	// reached the burst.
	SlowestPhase   string  `json:"slowest_phase,omitempty"`
	SlowestPhaseMs float64 `json:"slowest_phase_ms,omitempty"`
	SlowestHook    string  `json:"slowest_hook,omitempty"`
	SlowestHookMs  float64 `json:"slowest_hook_ms,omitempty"`
	SessionsEnd    int     `json:"sessions_end"`
}

/*
================
tickSpikeContext

The live aggregates sampled when a burst closes; supplied by the hub so
the journal never reaches into tick state itself.
================
*/
type tickSpikeContext struct {
	tickCount uint64
	overruns  uint64
	p99Ms     float64
	sessions  int
}

/*
================
tickSpikeJournal

Owns burst coalescing, the last-bursts mirror and the append-only
writer. Producers (the tick lane) only touch the mutex and, per closed
burst, one non-blocking queue send; all file I/O happens on the writer
goroutine.
================
*/
type tickSpikeJournal struct {
	path      string
	now       func() time.Time
	threshold time.Duration
	gap       time.Duration
	fileCap   int64
	context   func() tickSpikeContext

	queue    chan TickSpike
	wait     sync.WaitGroup
	dropped  atomic.Uint64
	writeErr atomic.Uint64

	mu          sync.Mutex
	started     bool
	closed      bool
	mirror      []TickSpike
	burstOpen   bool
	burstStart  time.Time
	burstLast   time.Time
	burstTicks  uint64
	burstSum    time.Duration
	burstMax    time.Duration
	burstTickMs float64
	stepName    string
	stepMax     time.Duration
	phaseName   string
	phaseMax    time.Duration
	hookName    string
	hookMax     time.Duration
}

/*
================
TickSpikeJournalEnabled

The kill switch: SRO_TICK_SPIKE_JOURNAL=off disables the journal.
================
*/
func TickSpikeJournalEnabled() bool {
	return os.Getenv(EnvTickSpikeJournal) != "off"
}

/*
================
newTickSpikeJournal

A journal appending to path with the production thresholds. The clock
and the file cap are fields so tests can drive them.
================
*/
func newTickSpikeJournal(path string) *tickSpikeJournal {
	return &tickSpikeJournal{
		path:      path,
		now:       time.Now,
		threshold: tickSpikeThreshold,
		gap:       tickSpikeGap,
		fileCap:   tickSpikeFileCap,
		queue:     make(chan TickSpike, tickSpikeQueueCap),
	}
}

/*
================
tickSpikeJournal.start

Creates the state directory and launches the writer goroutine.
================
*/
func (j *tickSpikeJournal) start() error {
	if dir := filepath.Dir(j.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	j.mu.Lock()
	j.started = true
	j.mu.Unlock()
	j.wait.Add(1)
	go j.run()
	return nil
}

/*
================
tickSpikeJournal.run

The writer goroutine: one open-append-close per burst line, so a crash
loses at most the record in flight and no handle outlives its line.
================
*/
func (j *tickSpikeJournal) run() {
	defer j.wait.Done()
	for spike := range j.queue {
		line, err := json.Marshal(spike)
		if err == nil {
			err = j.appendLine(line)
		}
		if err != nil {
			j.writeErr.Add(1)
		}
	}
}

/*
================
tickSpikeJournal.appendLine

Appends one line, rotating the live file to ".1" first when the line
would push it past the cap. Called only from the writer goroutine.
================
*/
func (j *tickSpikeJournal) appendLine(line []byte) error {
	if info, err := os.Stat(j.path); err == nil && info.Size()+int64(len(line))+1 > j.fileCap {
		// Windows refuses to rename over an existing file; drop the old
		// backup first. Rotation is best-effort beside the append.
		_ = os.Remove(j.path + ".1")
		_ = os.Rename(j.path, j.path+".1")
	}
	file, err := os.OpenFile(j.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

/*
================
tickSpikeJournal.observeTick

Feeds one tick sample into burst coalescing. A spiked tick opens or
extends the burst; any tick landing a gap after the last spike closes
it. Called on the tick lane: mutex and arithmetic only.
================
*/
func (j *tickSpikeJournal) observeTick(elapsed, interval time.Duration) {
	now := j.now()
	j.mu.Lock()
	defer j.mu.Unlock()
	if elapsed >= j.threshold {
		if j.burstOpen && now.Sub(j.burstLast) >= j.gap {
			j.closeBurstLocked()
		}
		if !j.burstOpen {
			j.burstOpen = true
			j.burstStart = now
			j.burstTicks = 0
			j.burstSum = 0
			j.burstMax = 0
			j.burstTickMs = float64(interval) / float64(time.Millisecond)
			j.stepName = ""
			j.stepMax = 0
			j.phaseName = ""
			j.phaseMax = 0
			j.hookName = ""
			j.hookMax = 0
		}
		j.burstLast = now
		j.burstTicks++
		j.burstSum += elapsed
		if elapsed > j.burstMax {
			j.burstMax = elapsed
		}
		return
	}
	if j.burstOpen && now.Sub(j.burstLast) >= j.gap {
		j.closeBurstLocked()
	}
}

/*
================
tickSpikeJournal.observeStep

Notes a slow named step inside the open burst; the slowest one is
recorded on the burst record. Steps outside a burst are ignored - the
live slow-hook stats already own them.
================
*/
func (j *tickSpikeJournal) observeStep(name string, elapsed time.Duration) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.burstOpen && elapsed > j.stepMax {
		j.stepName = name
		j.stepMax = elapsed
	}
}

/*
================
tickSpikeJournal.observePhases

Folds one tick's phase durations and slow hooks into the open burst's
attribution (#2295): the slowest phase under its TickPhaseMs key
(commit counts only when a flush ran), and the slowest slow hook or
division by name. Phases outside a burst are ignored, like steps - the
histograms already own every tick; the journal only attributes bursts.
================
*/
func (j *tickSpikeJournal) observePhases(phases TickPhases) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.burstOpen {
		return
	}
	for _, phase := range []struct {
		name    string
		elapsed time.Duration
	}{
		{"before_hooks", phases.BeforeHooks},
		{"divisions", phases.Divisions},
		{"hooks", phases.Hooks},
		{"commit", phases.Commit.Elapsed},
	} {
		if phase.elapsed > j.phaseMax {
			j.phaseName = phase.name
			j.phaseMax = phase.elapsed
		}
	}
	for _, slow := range [2][]SlowHook{phases.SlowHooks, phases.SlowDivisions} {
		for _, hook := range slow {
			if hook.Elapsed > j.hookMax {
				j.hookName = hook.Name
				j.hookMax = hook.Elapsed
			}
		}
	}
}

/*
================
tickSpikeJournal.closeBurstLocked

Seals the open burst into a record and emits it. Caller holds mu.
================
*/
func (j *tickSpikeJournal) closeBurstLocked() {
	var ctx tickSpikeContext
	if j.context != nil {
		ctx = j.context()
	}
	spike := TickSpike{
		Version:      1,
		StartTime:    j.burstStart,
		EndTime:      j.burstLast,
		Ticks:        j.burstTicks,
		MaxMs:        float64(j.burstMax) / float64(time.Millisecond),
		IntervalMs:   j.burstTickMs,
		ThresholdMs:  float64(j.threshold) / float64(time.Millisecond),
		TickCountEnd: ctx.tickCount,
		OverrunsEnd:  ctx.overruns,
		P99MsEnd:     ctx.p99Ms,
		SessionsEnd:  ctx.sessions,
	}
	if j.burstTicks > 0 {
		spike.MeanMs = float64(j.burstSum) / float64(time.Millisecond) / float64(j.burstTicks)
	}
	if j.stepName != "" {
		spike.SlowestStep = j.stepName
		spike.SlowestStepMs = float64(j.stepMax) / float64(time.Millisecond)
	}
	if j.phaseName != "" {
		spike.SlowestPhase = j.phaseName
		spike.SlowestPhaseMs = float64(j.phaseMax) / float64(time.Millisecond)
	}
	if j.hookName != "" {
		spike.SlowestHook = j.hookName
		spike.SlowestHookMs = float64(j.hookMax) / float64(time.Millisecond)
	}
	j.burstOpen = false
	j.emitLocked(spike)
}

/*
================
tickSpikeJournal.emitLocked

Mirrors the record and hands it to the writer without blocking: a full
queue drops the record and counts the drop. Caller holds mu.
================
*/
func (j *tickSpikeJournal) emitLocked(spike TickSpike) {
	j.mirror = append(j.mirror, spike)
	if len(j.mirror) > tickSpikeMirrorMax {
		j.mirror = append(j.mirror[:0], j.mirror[len(j.mirror)-tickSpikeMirrorMax:]...)
	}
	if !j.started || j.closed {
		return
	}
	select {
	case j.queue <- spike:
	default:
		j.dropped.Add(1)
	}
}

/*
================
tickSpikeJournal.snapshot

The metrics view: a copy of the mirror (oldest first) plus the drop and
write-error counters.
================
*/
func (j *tickSpikeJournal) snapshot() ([]TickSpike, uint64, uint64) {
	j.mu.Lock()
	mirror := append([]TickSpike(nil), j.mirror...)
	j.mu.Unlock()
	return mirror, j.dropped.Load(), j.writeErr.Load()
}

/*
================
tickSpikeJournal.Close

Flushes any open burst, then closes the queue and waits for the writer
to drain it. Safe to call more than once.
================
*/
func (j *tickSpikeJournal) Close() {
	j.mu.Lock()
	if j.burstOpen {
		j.closeBurstLocked()
	}
	if j.closed {
		j.mu.Unlock()
		return
	}
	j.closed = true
	started := j.started
	j.mu.Unlock()
	if started {
		close(j.queue)
		j.wait.Wait()
	}
}

/*
================
Hub.EnableTickSpikeJournal

Starts the spike journal appending to path. Diagnostics only: the
caller logs a failure and runs without the journal - recording must
never stop the server booting.
================
*/
func (h *Hub) EnableTickSpikeJournal(path string) error {
	journal := newTickSpikeJournal(path)
	journal.context = func() tickSpikeContext {
		ticks := &h.metrics.ticks
		ticks.mu.Lock()
		ctx := tickSpikeContext{
			tickCount: ticks.count,
			overruns:  ticks.overruns,
			p99Ms:     tickHistogramSnapshot(&ticks.buckets, ticks.count, ticks.max).P99Ms,
		}
		ticks.mu.Unlock()
		h.mu.RLock()
		ctx.sessions = len(h.sessions)
		h.mu.RUnlock()
		return ctx
	}
	if err := journal.start(); err != nil {
		return err
	}
	h.metrics.spikes = journal
	return nil
}
