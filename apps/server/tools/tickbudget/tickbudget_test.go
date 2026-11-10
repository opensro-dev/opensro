/*
===========================================================================

tickbudget_test.go - fixture-driven tests for the budget reads

The consistent set is built from real sources: a live execution trace
of a fake Ticker, a CPU profile whose total CPU equals the trace's
running time, and metrics snapshots whose zone totals tell the same
story per tick. The doctored set then breaks two agreements - a child
larger than its parent, and a profile that disagrees with the trace -
and must print BLIND SPOT lines naming both sources.

===========================================================================
*/

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime/trace"
	"strings"
	"testing"
	"time"

	"github.com/google/pprof/profile"
)

/*
================
fakeTicker

Stands in for the server's Ticker: the trace reducer identifies tick
goroutines by this method's name on the stack, exactly as it finds
Ticker).runScheduledTick in a server trace.
================
*/
type fakeTicker struct {
	sink float64
}

/*
================
fakeTicker.runScheduledTick

One fake tick: a CPU burn the trace can time, then a short sleep so
the goroutine also shows blocked time.
================
*/
func (f *fakeTicker) runScheduledTick() {
	start := time.Now()
	x := 1.0
	for time.Since(start) < 20*time.Millisecond {
		for i := 0; i < 10000; i++ {
			x = x*1.0000001 + 0.5
		}
	}
	f.sink = x
	time.Sleep(5 * time.Millisecond)
}

/*
================
writeTraceFixture

Run a few fake ticks under the execution tracer and write the trace.
Returns the number of ticks run.
================
*/
func writeTraceFixture(t *testing.T, dir string, ticks int) int {
	t.Helper()
	var buf bytes.Buffer
	if err := trace.Start(&buf); err != nil {
		t.Fatalf("trace.Start: %v", err)
	}
	ticker := &fakeTicker{}
	for i := 0; i < ticks; i++ {
		ticker.runScheduledTick()
	}
	trace.Stop()
	if err := os.WriteFile(filepath.Join(dir, "trace.out"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return ticks
}

/*
================
writeProfileFixture

A CPU profile whose samples stack through the fake tick entry and
whose total CPU is the given nanoseconds.
================
*/
func writeProfileFixture(t *testing.T, dir string, totalNs int64) {
	t.Helper()
	fnTick := &profile.Function{ID: 1, Name: "opensro.test/simulation.(*fakeTicker).runScheduledTick"}
	fnStep := &profile.Function{ID: 2, Name: "opensro.test/action.advancePets"}
	fnLeaf := &profile.Function{ID: 3, Name: "opensro.test/action.petStepWork"}
	locLeaf := &profile.Location{ID: 1, Line: []profile.Line{{Function: fnLeaf}}}
	locStep := &profile.Location{ID: 2, Line: []profile.Line{{Function: fnStep}}}
	locTick := &profile.Location{ID: 3, Line: []profile.Line{{Function: fnTick}}}
	const samples = 50
	per := totalNs / samples
	prof := &profile.Profile{
		SampleType: []*profile.ValueType{{Type: "samples", Unit: "count"}, {Type: "cpu", Unit: "nanoseconds"}},
		PeriodType: &profile.ValueType{Type: "cpu", Unit: "nanoseconds"},
		Period:     10000000,
		Function:   []*profile.Function{fnTick, fnStep, fnLeaf},
		Location:   []*profile.Location{locLeaf, locStep, locTick},
	}
	for i := 0; i < samples; i++ {
		prof.Sample = append(prof.Sample, &profile.Sample{
			Location: []*profile.Location{locLeaf, locStep, locTick},
			Value:    []int64{1, per},
		})
	}
	var buf bytes.Buffer
	if err := prof.Write(&buf); err != nil {
		t.Fatalf("profile write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cpu.pb.gz"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

/*
================
snapshotJSON
================
*/
func snapshotJSON(t *testing.T, dir, name string, snap metricsSnapshot) {
	t.Helper()
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

/*
================
fixtureSnapshots

Two snapshots `ticks` ticks apart. The zone story: phase:total is
`totalMsPerTick` per tick, split across the phases; the division and
its sub-step, the action hook and its step, nest inside.
================
*/
func fixtureSnapshots(ticks int64, totalMsPerTick float64) (from, to metricsSnapshot) {
	zones := func(scale float64) map[string]zoneTotal {
		row := func(ms float64) zoneTotal {
			return zoneTotal{Count: uint64(ticks), TotalMs: ms * scale}
		}
		return map[string]zoneTotal{
			"phase:total":              row(totalMsPerTick),
			"phase:before_hooks":       row(totalMsPerTick * 0.1),
			"phase:divisions":          row(totalMsPerTick * 0.5),
			"phase:hooks":              row(totalMsPerTick * 0.3),
			"phase:commit":             row(totalMsPerTick * 0.1),
			"division:global-official": row(totalMsPerTick * 0.5),
			"sub:monsterLeg":           row(totalMsPerTick * 0.25),
			"hook:action":              row(totalMsPerTick * 0.3),
			"step:advancePets":         row(totalMsPerTick * 0.15),
		}
	}
	from.TickCount = 1000
	from.TickMeanMs = 40
	from.LiveSessions = 99
	to.TickCount = 1000 + uint64(ticks)
	// Cumulative mean whose difference of sums is 50 ms of wall/tick.
	to.TickMeanMs = (from.TickMeanMs*float64(from.TickCount) + 50*float64(ticks)) / float64(to.TickCount)
	to.LiveSessions = 101
	to.Runtime.AllocatedBytes = uint64(ticks) * 2048
	from.TickZones = zones(1000) // arbitrary cumulative base
	to.TickZones = zones(1000 + float64(ticks))
	// The base rows above already scale by their own count; rebuild the
	// "to" rows as base + one window's worth.
	for key, row := range from.TickZones {
		toRow := to.TickZones[key]
		toRow.TotalMs = row.TotalMs + perTickOf(key, totalMsPerTick)*float64(ticks)
		toRow.Count = row.Count + uint64(ticks)
		to.TickZones[key] = toRow
	}
	return from, to
}

/*
================
perTickOf
================
*/
func perTickOf(key string, totalMsPerTick float64) float64 {
	switch key {
	case "phase:before_hooks", "phase:commit":
		return totalMsPerTick * 0.1
	case "phase:divisions", "division:global-official":
		return totalMsPerTick * 0.5
	case "phase:hooks", "hook:action":
		return totalMsPerTick * 0.3
	case "sub:monsterLeg":
		return totalMsPerTick * 0.25
	case "step:advancePets":
		return totalMsPerTick * 0.15
	default: // phase:total
		return totalMsPerTick
	}
}

/*
================
buildConsistentDir

Assemble one capture directory whose three sources agree, and return
its path.
================
*/
func buildConsistentDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ticks := writeTraceFixture(t, dir, 8)
	stats, err := reduceTrace(filepath.Join(dir, "trace.out"))
	if err != nil {
		t.Fatalf("reduceTrace on own fixture: %v", err)
	}
	runningPerTick := stats.runningMs() / float64(ticks)
	if runningPerTick <= 0 {
		t.Fatalf("trace fixture ran %.3f ms/tick, want positive", runningPerTick)
	}
	writeProfileFixture(t, dir, int64(runningPerTick*float64(ticks)*1e6))
	from, to := fixtureSnapshots(int64(ticks), runningPerTick)
	snapshotJSON(t, dir, "metrics-1.json", from)
	snapshotJSON(t, dir, "metrics-2.json", to)
	return dir
}

/*
================
TestDiffWindowIsExact

The window reads exact ticks and exact wall from mean x count, and
exact per-zone milliseconds per tick - never a bucket midpoint.
================
*/
func TestDiffWindowIsExact(t *testing.T) {
	from, to := fixtureSnapshots(200, 12.5)
	w, err := diffWindow(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if w.Ticks != 200 {
		t.Fatalf("ticks = %d, want 200", w.Ticks)
	}
	if w.MeanMsPerTick != 50 {
		t.Fatalf("mean = %v ms/tick, want exactly 50", w.MeanMsPerTick)
	}
	if got := w.ZoneMsPerTick["phase:total"]; got != 12.5 {
		t.Fatalf("phase:total = %v ms/tick, want 12.5", got)
	}
	if got := w.ZoneMsPerTick["step:advancePets"]; got != 12.5*0.15 {
		t.Fatalf("step:advancePets = %v ms/tick, want %v", got, 12.5*0.15)
	}
	if w.AllocBytesTick != 2048 {
		t.Fatalf("alloc = %v bytes/tick, want 2048", w.AllocBytesTick)
	}
}

/*
================
TestAnalyzeConsistentSetPasses

Metrics, profile and trace telling the same story reconcile clean:
PASS lines, no BLIND SPOT, and a budget.json on disk.
================
*/
func TestAnalyzeConsistentSetPasses(t *testing.T) {
	dir := buildConsistentDir(t)
	var out bytes.Buffer
	blind, err := analyzeDir(dir, "", &out)
	if err != nil {
		t.Fatal(err)
	}
	if blind != 0 {
		t.Fatalf("consistent set printed %d BLIND SPOT lines:\n%s", blind, out.String())
	}
	for _, want := range []string{"phase:total", "unattributed", "PASS", "runnable"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "budget.json")); err != nil {
		t.Fatalf("budget.json not written: %v", err)
	}
}

/*
================
TestAnalyzeDoctoredSetShowsBlindSpots

A child larger than its parent and a profile that disagrees with the
trace must both surface as BLIND SPOT lines naming the two sources.
================
*/
func TestAnalyzeDoctoredSetShowsBlindSpots(t *testing.T) {
	dir := buildConsistentDir(t)
	// Doctor the profile: double the CPU the trace saw.
	stats, err := reduceTrace(filepath.Join(dir, "trace.out"))
	if err != nil {
		t.Fatal(err)
	}
	writeProfileFixture(t, dir, int64(stats.runningMs()*2*1e6))
	// Doctor the zones: the step now exceeds every hook above it.
	from, to := fixtureSnapshots(8, stats.runningMs()/8)
	row := to.TickZones["step:advancePets"]
	row.TotalMs = from.TickZones["step:advancePets"].TotalMs + stats.runningMs()
	to.TickZones["step:advancePets"] = row
	snapshotJSON(t, dir, "metrics-1.json", from)
	snapshotJSON(t, dir, "metrics-2.json", to)

	var out bytes.Buffer
	blind, err := analyzeDir(dir, "", &out)
	if err != nil {
		t.Fatal(err)
	}
	if blind < 2 {
		t.Fatalf("doctored set printed %d BLIND SPOT lines, want at least 2:\n%s", blind, out.String())
	}
	text := out.String()
	if !strings.Contains(text, "BLIND SPOT: profile CPU vs trace running") {
		t.Fatalf("missing profile-vs-trace BLIND SPOT:\n%s", text)
	}
	if !strings.Contains(text, "BLIND SPOT: children within parents") {
		t.Fatalf("missing child-over-parent BLIND SPOT:\n%s", text)
	}
}

/*
================
TestAnalyzeFiveSnapshotCaptureScalesEachSourceByItsPair

A capture's five snapshots pair by file order (#3103): the profile
scales by metrics-1..2 (301 ticks), the trace by metrics-3..4 (50),
the zone window by the whole span (954) - even though metrics-2 and
metrics-3 share a tick counter, the tie the old re-sort dropped.
================
*/
func TestAnalyzeFiveSnapshotCaptureScalesEachSourceByItsPair(t *testing.T) {
	dir := t.TempDir()
	counts := []uint64{1000, 1301, 1301, 1351, 1954}
	for i, count := range counts {
		snap := metricsSnapshot{TickCount: count, TickMeanMs: 60, LiveSessions: 105}
		snapshotJSON(t, dir, "metrics-"+string(rune('1'+i))+".json", snap)
	}
	var out bytes.Buffer
	if _, err := analyzeDir(dir, "", &out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"window: 954 ticks", "windows for profile and trace: cpu 301 ticks, trace 50 ticks"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
}

/*
================
TestInterTickWaitClassification

The tick machinery's wait for its next unit of work is idle, never
blocked (#3103): the coordinator in Ticker.run's select, a shard
worker in runShard's select, and the coordinator on a batch's done
channel inside runScheduledTick. Real waits inside the tick stay
blocked.
================
*/
func TestInterTickWaitClassification(t *testing.T) {
	tick := "opensro.online/server/internal/game/world/simulation.(*Ticker)."
	cases := []struct {
		name  string
		funcs []string
		want  bool
	}{
		{"coordinator select", []string{"runtime.selectgo", tick + "run", "main.main"}, true},
		{"shard worker select", []string{"runtime.selectgo", tick + "runShard", tick + "run"}, true},
		{"coordinator batch done", []string{"runtime.chanrecv", tick + "runScheduledTick", tick + "run"}, true},
		{"lock wait inside the tick", []string{"runtime.semacquire", "sync.(*Mutex).Lock", tick + "runScheduledTick", tick + "run"}, false},
		{"sleep inside the tick", []string{"runtime.gopark", "time.Sleep", "example.hook", tick + "runScheduledTick", tick + "run"}, false},
		{"channel wait inside the tick", []string{"runtime.chanrecv", "example.hook", tick + "runScheduledTick", tick + "run"}, false},
		{"select inside the tick", []string{"runtime.selectgo", "example.hook", tick + "runScheduledTick", tick + "run"}, false},
		{"no frames", nil, false},
	}
	for _, tc := range cases {
		if got := isInterTickWait(tc.funcs); got != tc.want {
			t.Fatalf("%s: isInterTickWait = %v, want %v", tc.name, got, tc.want)
		}
	}
}

/*
================
TestChildrenWithinParentsHooksTolerance

The hooks' sum may exceed phase:hooks by the per-hook timer overhead
(under 3%, #3103); beyond it the check still fails.
================
*/
func TestChildrenWithinParentsHooksTolerance(t *testing.T) {
	hooksCheck := func(t *testing.T, hookMs float64) check {
		t.Helper()
		zones := map[string]float64{
			"phase:total":        100,
			"phase:before_hooks": 70,
			"phase:hooks":        30,
			"hook:action":        hookMs,
		}
		for _, c := range reconcile(&measurements{zones: zones}) {
			if c.Name == "children within parents (metrics zones)" {
				return c
			}
		}
		t.Fatal("children-within-parents check missing")
		return check{}
	}
	if c := hooksCheck(t, 30.6); c.Status != "PASS" { // +2% over the parent
		t.Fatalf("hooks at +2%%: status = %s (%s), want PASS", c.Status, c.Detail)
	}
	if c := hooksCheck(t, 32); c.Status != "FAIL" { // +6.7% over the parent
		t.Fatalf("hooks at +6.7%%: status = %s (%s), want FAIL", c.Status, c.Detail)
	}
}

/*
================
TestCaptureRefusesNonLoopback

Capture refuses a routable pprof or metrics address before any ssh
happens; loopback spellings pass the guard.
================
*/
func TestCaptureRefusesNonLoopback(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8792", "localhost:8792", "[::1]:8792", "127.0.0.2:8788"} {
		if err := requireLoopback(addr, "pprof"); err != nil {
			t.Fatalf("requireLoopback(%q) = %v, want pass", addr, err)
		}
	}
	for _, addr := range []string{"10.0.0.5:8792", "192.168.1.9:8788", "example.invalid:8792", "8792"} {
		if err := requireLoopback(addr, "pprof"); err == nil {
			t.Fatalf("requireLoopback(%q) passed, want refusal", addr)
		}
	}
	err := runCapture(captureOptions{sshTarget: "user@host", pprofAddr: "203.0.113.7:8792", metricsAddr: "127.0.0.1:8788", outDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("runCapture with routable pprof = %v, want refusal", err)
	}
}

/*
================
TestExtractTarGzRefusesEscape

Capture members are created through an os.Root on the output
directory: a member named to climb out of it refuses the capture,
and a plain "./name" member lands inside.
================
*/
func TestExtractTarGzRefusesEscape(t *testing.T) {
	writeTar := func(t *testing.T, names ...string) []byte {
		t.Helper()
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		for _, name := range names {
			if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: 2, Typeflag: tar.TypeReg}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write([]byte("{}")); err != nil {
				t.Fatal(err)
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	out := t.TempDir()
	if err := extractTarGz(bytes.NewReader(writeTar(t, "../evil.json")), out); err == nil {
		t.Fatal("member ../evil.json extracted without refusal")
	}
	if _, err := os.Stat(filepath.Join(out, "evil.json")); !os.IsNotExist(err) {
		t.Fatalf("evil.json exists in the output directory: %v", err)
	}
	if err := extractTarGz(bytes.NewReader(writeTar(t, "./metrics-1.json")), out); err != nil {
		t.Fatalf("plain member refused: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "metrics-1.json")); err != nil {
		t.Fatalf("metrics-1.json not extracted: %v", err)
	}
}

/*
================
TestAnalyzeMissingZonesSkipsNotFails

The 2026-11-07 hand capture predates tick_zones: analyze must still
read its windows and skip the zone checks, never fail them.
================
*/
func TestAnalyzeMissingZonesSkipsNotFails(t *testing.T) {
	dir := t.TempDir()
	from := metricsSnapshot{TickCount: 500}
	from.TickMeanMs = 56
	to := metricsSnapshot{TickCount: 550}
	to.TickMeanMs = (56*500 + 55*50) / 550
	snapshotJSON(t, dir, "m0.json", from)
	snapshotJSON(t, dir, "m1.json", to)
	var out bytes.Buffer
	blind, err := analyzeDir(dir, "", &out)
	if err != nil {
		t.Fatal(err)
	}
	if blind != 0 {
		t.Fatalf("zone-less capture printed BLIND SPOT lines:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "SKIP") || !strings.Contains(out.String(), "50 ticks") {
		t.Fatalf("zone-less capture not read as a 50-tick window with SKIP checks:\n%s", out.String())
	}
}
