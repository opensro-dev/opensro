/*
===========================================================================

tick_spikes_test.go - spike bursts coalesce, mirror, drop, rotate, serve

===========================================================================
*/

package transport

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

/*
================
newSpikeJournalForTest

A journal on a temp file with a hand-driven clock and a fixed context
snapshot, so burst timing is exact. Move time by writing through the
returned clock pointer.
================
*/
func newSpikeJournalForTest(t *testing.T) (*tickSpikeJournal, *time.Time) {
	t.Helper()
	clock := time.Date(2026, 10, 17, 8, 40, 0, 0, time.UTC)
	journal := newTickSpikeJournal(filepath.Join(t.TempDir(), TickSpikeFileName))
	journal.now = func() time.Time { return clock }
	journal.context = func() tickSpikeContext {
		return tickSpikeContext{tickCount: 100, overruns: 7, p99Ms: 42.5, sessions: 3}
	}
	if err := journal.start(); err != nil {
		t.Fatalf("start spike journal: %v", err)
	}
	t.Cleanup(journal.Close)
	return journal, &clock
}

/*
================
readSpikeLines
================
*/
func readSpikeLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read spike journal: %v", err)
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

/*
================
TestTickSpikeBurstsCoalesceAndClose

Two spiked ticks a second apart are one burst; a quiet tick a gap later
closes it, and the closed record lands on disk byte for byte.
================
*/
func TestTickSpikeBurstsCoalesceAndClose(t *testing.T) {
	journal, clock := newSpikeJournalForTest(t)
	journal.observeTick(300*time.Millisecond, 100*time.Millisecond)
	*clock = clock.Add(time.Second)
	journal.observeTick(400*time.Millisecond, 100*time.Millisecond)
	*clock = clock.Add(6 * time.Second)
	journal.observeTick(50*time.Millisecond, 100*time.Millisecond)

	mirror, dropped, writeErrors := journal.snapshot()
	if len(mirror) != 1 || dropped != 0 || writeErrors != 0 {
		t.Fatalf("mirror = %+v, dropped = %d, writeErrors = %d", mirror, dropped, writeErrors)
	}
	spike := mirror[0]
	if spike.Ticks != 2 || spike.MaxMs != 400 || spike.MeanMs != 350 {
		t.Fatalf("burst shape = %+v", spike)
	}
	if spike.IntervalMs != 100 || spike.ThresholdMs != 250 {
		t.Fatalf("burst cadence = %+v", spike)
	}
	if spike.TickCountEnd != 100 || spike.OverrunsEnd != 7 || spike.P99MsEnd != 42.5 || spike.SessionsEnd != 3 {
		t.Fatalf("burst context = %+v", spike)
	}
	if !spike.StartTime.Equal(time.Date(2026, 10, 17, 8, 40, 0, 0, time.UTC)) ||
		!spike.EndTime.Equal(time.Date(2026, 10, 17, 8, 40, 1, 0, time.UTC)) {
		t.Fatalf("burst window = %v..%v", spike.StartTime, spike.EndTime)
	}

	journal.Close()
	lines := readSpikeLines(t, journal.path)
	const golden = `{"v":1,"ts_start":"2026-10-17T08:40:00Z","ts_end":"2026-10-17T08:40:01Z","ticks_in_burst":2,"max_ms":400,"mean_ms":350,"interval_ms":100,"threshold_ms":250,"tick_count_end":100,"overruns_end":7,"p99_ms_end":42.5,"sessions_end":3}`
	if len(lines) != 1 || lines[0] != golden {
		t.Fatalf("journal lines = %v", lines)
	}
}

/*
================
TestTickSpikeBurstClosedByNextSpike

Nit #1122: when the gap passes with no quiet tick, the next spiked tick
itself seals the stale burst - at its last spike, not at the new tick -
and opens a fresh burst. This is the one observeTick close branch the
quiet-tick close does not share.
================
*/
func TestTickSpikeBurstClosedByNextSpike(t *testing.T) {
	journal, clock := newSpikeJournalForTest(t)
	journal.observeTick(300*time.Millisecond, 100*time.Millisecond)
	*clock = clock.Add(6 * time.Second)
	journal.observeTick(500*time.Millisecond, 100*time.Millisecond)

	mirror, _, _ := journal.snapshot()
	if len(mirror) != 1 {
		t.Fatalf("mirror after second spike = %+v", mirror)
	}
	first := mirror[0]
	wantFirst := time.Date(2026, 10, 17, 8, 40, 0, 0, time.UTC)
	if first.Ticks != 1 || first.MaxMs != 300 ||
		!first.StartTime.Equal(wantFirst) || !first.EndTime.Equal(wantFirst) {
		t.Fatalf("first burst = %+v", first)
	}

	*clock = clock.Add(6 * time.Second)
	journal.observeTick(50*time.Millisecond, 100*time.Millisecond)
	mirror, _, _ = journal.snapshot()
	if len(mirror) != 2 {
		t.Fatalf("mirror after quiet tick = %+v", mirror)
	}
	second := mirror[1]
	wantSecond := time.Date(2026, 10, 17, 8, 40, 6, 0, time.UTC)
	if second.Ticks != 1 || second.MaxMs != 500 ||
		!second.StartTime.Equal(wantSecond) || !second.EndTime.Equal(wantSecond) {
		t.Fatalf("second burst = %+v", second)
	}
}

/*
================
TestTickSpikeBelowThresholdIgnored

Ticks under the threshold open nothing, and a slow step outside a burst
leaks into no record.
================
*/
func TestTickSpikeBelowThresholdIgnored(t *testing.T) {
	journal, clock := newSpikeJournalForTest(t)
	journal.observeStep("world", time.Second)
	journal.observeTick(249*time.Millisecond, 100*time.Millisecond)
	*clock = clock.Add(6 * time.Second)
	journal.observeTick(100*time.Millisecond, 100*time.Millisecond)
	journal.Close()

	if mirror, _, _ := journal.snapshot(); len(mirror) != 0 {
		t.Fatalf("mirror = %+v", mirror)
	}
	if data, err := os.ReadFile(journal.path); err == nil && len(strings.TrimSpace(string(data))) != 0 {
		t.Fatalf("journal file = %q", data)
	}
}

/*
================
TestTickSpikeSlowestStepRecorded

The slowest named step inside the burst rides the record.
================
*/
func TestTickSpikeSlowestStepRecorded(t *testing.T) {
	journal, clock := newSpikeJournalForTest(t)
	journal.observeTick(300*time.Millisecond, 100*time.Millisecond)
	journal.observeStep("world", 120*time.Millisecond)
	journal.observeStep("db", 80*time.Millisecond)
	*clock = clock.Add(6 * time.Second)
	journal.observeTick(50*time.Millisecond, 100*time.Millisecond)
	journal.Close()

	lines := readSpikeLines(t, journal.path)
	if len(lines) != 1 {
		t.Fatalf("journal lines = %v", lines)
	}
	var spike TickSpike
	if err := json.Unmarshal([]byte(lines[0]), &spike); err != nil {
		t.Fatalf("decode burst: %v", err)
	}
	if spike.SlowestStep != "world" || spike.SlowestStepMs != 120 {
		t.Fatalf("slowest step = %+v", spike)
	}
}

/*
================
TestTickSpikePhaseAndHookRecorded

#2295: the burst's time is attributed to the phase and hook that took
it, folded from the burst ticks' TickPhases, with the named step kept
as finer detail. A later, quieter phase report in the same burst does
not displace the maxima, and a slow division names itself as the hook.
================
*/
func TestTickSpikePhaseAndHookRecorded(t *testing.T) {
	journal, clock := newSpikeJournalForTest(t)
	journal.observeTick(900*time.Millisecond, 100*time.Millisecond)
	journal.observeStep("tickGateCut", 5*time.Millisecond)
	journal.observePhases(TickPhases{
		BeforeHooks: 40 * time.Millisecond,
		Divisions:   30 * time.Millisecond,
		Hooks:       820 * time.Millisecond,
		Total:       900 * time.Millisecond,
		SlowHooks:   []SlowHook{{Name: "actionTick", Elapsed: 489 * time.Millisecond}},
	})
	*clock = clock.Add(time.Second)
	journal.observeTick(300*time.Millisecond, 100*time.Millisecond)
	journal.observePhases(TickPhases{
		Hooks: 120 * time.Millisecond,
		Total: 300 * time.Millisecond,
		SlowDivisions: []SlowHook{
			{Name: "global-official", Elapsed: 120 * time.Millisecond},
		},
	})
	*clock = clock.Add(6 * time.Second)
	journal.observeTick(50*time.Millisecond, 100*time.Millisecond)
	journal.Close()

	lines := readSpikeLines(t, journal.path)
	if len(lines) != 1 {
		t.Fatalf("journal lines = %v", lines)
	}
	var spike TickSpike
	if err := json.Unmarshal([]byte(lines[0]), &spike); err != nil {
		t.Fatalf("decode burst: %v", err)
	}
	if spike.SlowestPhase != "hooks" || spike.SlowestPhaseMs != 820 {
		t.Fatalf("slowest phase = %+v", spike)
	}
	if spike.SlowestHook != "actionTick" || spike.SlowestHookMs != 489 {
		t.Fatalf("slowest hook = %+v", spike)
	}
	if spike.SlowestStep != "tickGateCut" || spike.SlowestStepMs != 5 {
		t.Fatalf("slowest step = %+v", spike)
	}
}

/*
================
TestTickSpikeUnattributedBurstStaysBlank

A burst no phase report reached carries no phase or hook fields: the
record stays unattributed rather than blaming the one step it saw.
Phase reports outside a burst are ignored, like steps.
================
*/
func TestTickSpikeUnattributedBurstStaysBlank(t *testing.T) {
	journal, clock := newSpikeJournalForTest(t)
	journal.observePhases(TickPhases{Hooks: time.Second, Total: time.Second})
	journal.observeTick(300*time.Millisecond, 100*time.Millisecond)
	journal.observeStep("tickGateCut", 5*time.Millisecond)
	*clock = clock.Add(6 * time.Second)
	journal.observeTick(50*time.Millisecond, 100*time.Millisecond)
	journal.Close()

	lines := readSpikeLines(t, journal.path)
	if len(lines) != 1 {
		t.Fatalf("journal lines = %v", lines)
	}
	var spike TickSpike
	if err := json.Unmarshal([]byte(lines[0]), &spike); err != nil {
		t.Fatalf("decode burst: %v", err)
	}
	if spike.SlowestPhase != "" || spike.SlowestHook != "" {
		t.Fatalf("unattributed burst = %+v", spike)
	}
	if strings.Contains(lines[0], "slowest_phase") || strings.Contains(lines[0], "slowest_hook") {
		t.Fatalf("unattributed burst line = %s", lines[0])
	}
}

/*
================
TestTickSpikeMirrorKeepsLast32

The live mirror keeps exactly the newest 32 bursts, oldest first.
================
*/
func TestTickSpikeMirrorKeepsLast32(t *testing.T) {
	journal, clock := newSpikeJournalForTest(t)
	for i := range 35 {
		journal.observeTick(time.Duration(250+i)*time.Millisecond, 100*time.Millisecond)
		*clock = clock.Add(6 * time.Second)
		journal.observeTick(50*time.Millisecond, 100*time.Millisecond)
	}
	mirror, _, _ := journal.snapshot()
	if len(mirror) != 32 {
		t.Fatalf("mirror length = %d", len(mirror))
	}
	if mirror[0].MaxMs != 253 || mirror[31].MaxMs != 284 {
		t.Fatalf("mirror window = %v..%v", mirror[0].MaxMs, mirror[31].MaxMs)
	}
}

/*
================
TestTickSpikeDropsWhenQueueFull

With no writer draining, a full queue drops records and counts them;
the tick path never blocks.
================
*/
func TestTickSpikeDropsWhenQueueFull(t *testing.T) {
	clock := time.Date(2026, 10, 17, 8, 40, 0, 0, time.UTC)
	journal := newTickSpikeJournal(filepath.Join(t.TempDir(), TickSpikeFileName))
	journal.queue = make(chan TickSpike, 2)
	journal.now = func() time.Time { return clock }
	journal.started = true // the writer goroutine deliberately never runs
	for range 3 {
		journal.observeTick(300*time.Millisecond, 100*time.Millisecond)
		clock = clock.Add(6 * time.Second)
		journal.observeTick(50*time.Millisecond, 100*time.Millisecond)
	}
	mirror, dropped, _ := journal.snapshot()
	if len(mirror) != 3 || dropped != 1 {
		t.Fatalf("mirror = %d bursts, dropped = %d", len(mirror), dropped)
	}
	journal.Close()
}

/*
================
TestTickSpikeRotatesAtCap

Past the file cap the live file rotates to ".1" and starts fresh.
================
*/
func TestTickSpikeRotatesAtCap(t *testing.T) {
	journal, clock := newSpikeJournalForTest(t)
	journal.fileCap = 200
	for range 5 {
		journal.observeTick(300*time.Millisecond, 100*time.Millisecond)
		*clock = clock.Add(6 * time.Second)
		journal.observeTick(50*time.Millisecond, 100*time.Millisecond)
	}
	journal.Close()

	if lines := readSpikeLines(t, journal.path); len(lines) != 1 {
		t.Fatalf("live file lines = %v", lines)
	}
	if lines := readSpikeLines(t, journal.path+".1"); len(lines) != 1 {
		t.Fatalf("rotated file lines = %v", lines)
	}
}

/*
================
TestTickSpikeHubIntegration

The hub feeds the journal from real tick and slow-step records, serves
the mirror on Metrics, and shutdown flushes the open burst to disk.
================
*/
func TestTickSpikeHubIntegration(t *testing.T) {
	hub := newHub(testCfg())
	path := filepath.Join(t.TempDir(), TickSpikeFileName)
	if err := hub.EnableTickSpikeJournal(path); err != nil {
		t.Fatalf("enable spike journal: %v", err)
	}
	hub.RecordTickDuration(300*time.Millisecond, 100*time.Millisecond)
	hub.RecordTickDuration(400*time.Millisecond, 100*time.Millisecond)
	hub.RecordSlowStep("world", 120*time.Millisecond)
	hub.shutdown(context.Background())

	metrics := hub.Metrics()
	if len(metrics.TickSpikes) != 1 || metrics.TickSpikeDropped != 0 || metrics.TickSpikeWriteErrors != 0 {
		t.Fatalf("metrics spikes = %+v", metrics.TickSpikes)
	}
	lines := readSpikeLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("journal lines = %v", lines)
	}
	var spike TickSpike
	if err := json.Unmarshal([]byte(lines[0]), &spike); err != nil {
		t.Fatalf("decode burst: %v", err)
	}
	if spike.Ticks != 2 || spike.MaxMs != 400 || spike.SlowestStep != "world" ||
		spike.TickCountEnd != 2 || spike.OverrunsEnd != 2 {
		t.Fatalf("burst = %+v", spike)
	}
}

/*
================
TestTickSpikeJournalKillSwitch

Only the exact value "off" disables the journal.
================
*/
func TestTickSpikeJournalKillSwitch(t *testing.T) {
	t.Setenv(EnvTickSpikeJournal, "off")
	if TickSpikeJournalEnabled() {
		t.Fatal("journal enabled with SRO_TICK_SPIKE_JOURNAL=off")
	}
	t.Setenv(EnvTickSpikeJournal, "")
	if !TickSpikeJournalEnabled() {
		t.Fatal("journal disabled with SRO_TICK_SPIKE_JOURNAL empty")
	}
	t.Setenv(EnvTickSpikeJournal, "on")
	if !TickSpikeJournalEnabled() {
		t.Fatal("journal disabled with SRO_TICK_SPIKE_JOURNAL=on")
	}
}
