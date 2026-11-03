/*
===========================================================================

tick_watchdog_test.go - a stalled tick is reported once, by phase and hook

===========================================================================
*/

package simulation

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/testsupport/wait"
)

// emptySessionSource and discardPusher give the ticker no sessions and no
// delivery, so only the hooks run.
type emptySessionSource struct{}

func (emptySessionSource) SnapshotSessions() []SessionSnapshot { return nil }

type discardPusher struct{}

func (discardPusher) PushToSession(string, []Frame)          {}
func (discardPusher) PushToDivision(string, []Frame, string) {}

/*
================
stalledTickHook

A named hook that blocks until released, standing in for a tick hook
waiting on a lock or a disk.
================
*/
func stalledTickHook(release <-chan struct{}) TickHook {
	return func(int64) []DivisionFrames {
		<-release
		return nil
	}
}

/*
================
TestTickWatchdogReportsAStalledHookOnce
================
*/
func TestTickWatchdogReportsAStalledHookOnce(t *testing.T) {
	release := make(chan struct{})
	ticker := &Ticker{Source: emptySessionSource{}, Push: discardPusher{}, Hooks: []NamedTickHook{{Name: "stalled-hook", Hook: stalledTickHook(release)}}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker.RunTick(0)
	}()

	wait.Eventually(t, 5*time.Second, "the stalled hook to start", func() bool {
		name, _ := ticker.watch.hookName.Load().(string)
		return name != ""
	})
	report := ticker.watch.check(time.Now().Add(2*time.Second), time.Second)
	if !strings.Contains(report, "in hooks (hook stalled-hook)") {
		t.Fatalf("report does not name the phase and hook: %.200s", report)
	}
	if !strings.Contains(report, "goroutine ") {
		t.Fatalf("report carries no goroutine stacks: %.200s", report)
	}
	if again := ticker.watch.check(time.Now().Add(3*time.Second), time.Second); again != "" {
		t.Fatal("the same stall was reported twice")
	}

	close(release)
	<-done
	if after := ticker.watch.check(time.Now().Add(time.Hour), time.Second); after != "" {
		t.Fatal("a finished tick was reported")
	}
}

/*
================
TestTickWatchdogIgnoresATickWithinTheThreshold
================
*/
func TestTickWatchdogIgnoresATickWithinTheThreshold(t *testing.T) {
	var watch tickWatch
	now := time.Now()
	watch.begin(now)
	if report := watch.check(now.Add(500*time.Millisecond), time.Second); report != "" {
		t.Fatalf("a 500 ms tick was reported: %.120s", report)
	}
}

/*
================
TestTickWatchdogDumpsAndExitsOnADeadlock

A tick stuck past StallExit writes the dump (phase, hook, the goroutines
parked on locks, every stack) and exits non-zero for the supervisor.
================
*/
func TestTickWatchdogDumpsAndExitsOnADeadlock(t *testing.T) {
	var held sync.RWMutex
	held.Lock()
	reads := 0
	go func() {
		held.RLock()
		defer held.RUnlock()
		reads++
	}()
	defer held.Unlock()
	wait.Eventually(t, 5*time.Second, "the reader to park on the held lock", func() bool {
		return strings.Contains(string(allStacks()), "[sync.RWMutex.RLock]")
	})

	dir := t.TempDir()
	code := -1
	ticker := &Ticker{StallExit: time.Second, StallDumpDir: dir, exit: func(c int) { code = c }}
	now := time.Now()
	ticker.watch.begin(now.Add(-2 * time.Second))
	ticker.watch.phase.Store(tickPhaseHooks)
	if !ticker.stallExpired(now) || code != tickStallExitCode {
		t.Fatalf("stuck tick: expired with code %d, want %d", code, tickStallExitCode)
	}
	dumps, _ := filepath.Glob(filepath.Join(dir, "tick-stall-*.txt"))
	if len(dumps) != 1 {
		t.Fatalf("dumps = %v, want one", dumps)
	}
	body, err := os.ReadFile(dumps[0])
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	waiters := text[strings.Index(text, "== goroutines waiting on locks =="):strings.Index(text, "== all goroutines ==")]
	if !strings.Contains(text, "phase:   hooks") || !strings.Contains(waiters, "[sync.RWMutex.RLock]") ||
		!strings.Contains(waiters, "TestTickWatchdogDumpsAndExitsOnADeadlock") {
		t.Fatalf("dump lacks the stalled phase or the parked reader: %.2000s", text)
	}
}

/*
================
TestTickWatchdogExitNeedsAStuckTick

No exit within StallExit, between ticks, or with the exit disabled.
================
*/
func TestTickWatchdogExitNeedsAStuckTick(t *testing.T) {
	exited := false
	stub := func(int) { exited = true }
	now := time.Now()
	short := &Ticker{StallExit: time.Minute, StallDumpDir: t.TempDir(), exit: stub}
	short.watch.begin(now.Add(-2 * time.Second))
	idle := &Ticker{StallExit: time.Second, StallDumpDir: t.TempDir(), exit: stub}
	off := &Ticker{StallDumpDir: t.TempDir(), exit: stub}
	off.watch.begin(now.Add(-time.Hour))
	for _, ticker := range []*Ticker{short, idle, off} {
		if ticker.stallExpired(now) || exited {
			t.Fatal("exited without a tick stuck past StallExit")
		}
	}
}

/*
================
TestTickWatchdogSparesAProgressingTick

A starved tick that still finishes steps outlives StallExit; once its steps
stop for StallExit it is exited like a deadlock.
================
*/
func TestTickWatchdogSparesAProgressingTick(t *testing.T) {
	code := -1
	ticker := &Ticker{StallExit: time.Second, StallDumpDir: t.TempDir(), exit: func(c int) { code = c }}
	start := time.Now()
	ticker.watch.begin(start)
	at := func(d time.Duration) bool { return ticker.stallExpired(start.Add(d)) }
	for d := 500 * time.Millisecond; d <= 3*time.Second; d += 500 * time.Millisecond {
		ticker.watch.step()
		if at(d) {
			t.Fatalf("progressing tick exited at %v", d)
		}
	}
	if at(3*time.Second + 900*time.Millisecond) {
		t.Fatal("exited within StallExit of the last step")
	}
	if !at(4*time.Second+100*time.Millisecond) || code != tickStallExitCode {
		t.Fatalf("tick with no step for StallExit: code %d, want %d", code, tickStallExitCode)
	}
}

/*
================
TestTickWatchdogExitsARunawayAtTheCeiling

Steps that never stop cannot hold a tick past stallCeilingFactor periods.
================
*/
func TestTickWatchdogExitsARunawayAtTheCeiling(t *testing.T) {
	code := -1
	ticker := &Ticker{StallExit: time.Second, StallDumpDir: t.TempDir(), exit: func(c int) { code = c }}
	start := time.Now()
	ticker.watch.begin(start)
	ceiling := stallCeilingFactor * ticker.StallExit
	for d := 250 * time.Millisecond; d < ceiling; d += 250 * time.Millisecond {
		ticker.watch.step()
		if ticker.stallExpired(start.Add(d)) {
			t.Fatalf("runaway exited at %v, before the ceiling %v", d, ceiling)
		}
	}
	ticker.watch.step()
	if !ticker.stallExpired(start.Add(ceiling)) || code != tickStallExitCode {
		t.Fatalf("runaway at the ceiling: code %d, want %d", code, tickStallExitCode)
	}
}

/*
================
TestTickWatchdogStepsBeforeTheFirstLookDoNotRestartTheClock

The first look at a tick counts from its start, so a tick that parked
before the watchdog saw it is not granted a fresh StallExit.
================
*/
func TestTickWatchdogStepsBeforeTheFirstLookDoNotRestartTheClock(t *testing.T) {
	code := -1
	ticker := &Ticker{StallExit: time.Second, StallDumpDir: t.TempDir(), exit: func(c int) { code = c }}
	start := time.Now()
	ticker.watch.begin(start)
	ticker.watch.step()
	if !ticker.stallExpired(start.Add(1500*time.Millisecond)) || code != tickStallExitCode {
		t.Fatalf("parked tick first seen past StallExit: code %d, want %d", code, tickStallExitCode)
	}
}

/*
================
TestSchedLatencyReadsThePollPeriod

The quantile comes from the counts added since the previous sample, not
from the process lifetime, and a sample pair that does not line up says
nothing.
================
*/
func TestSchedLatencyReadsThePollPeriod(t *testing.T) {
	buckets := []float64{math.Inf(-1), 0, 0.001, 0.1, 1, math.Inf(1)}
	prev := schedSample{counts: []uint64{0, 1000, 0, 0, 0}, buckets: buckets}
	starved := schedSample{counts: []uint64{0, 1010, 0, 0, 990}, buckets: buckets}
	if latency, wakeups := schedLatency(prev, starved); latency != time.Second || wakeups != 1000 {
		t.Fatalf("starved period: %v over %d, want 1s over 1000", latency, wakeups)
	}
	idle := schedSample{counts: []uint64{0, 2000, 0, 0, 0}, buckets: buckets}
	if latency, _ := schedLatency(prev, idle); latency != 0 {
		t.Fatalf("an unstarved period read %v", latency)
	}
	if _, wakeups := schedLatency(schedSample{}, starved); wakeups != 0 {
		t.Fatal("mismatched samples reported wake-ups")
	}
	if !strings.Contains(new(tickWatch).schedReport(), "scheduler") {
		t.Fatal("no scheduler line")
	}
}

/*
================
TestMonsterLegStepsTheWatch

A monster leg steps the stall watch every monsterLegStepActors behaviours,
so a long leg on a starved host shows progress.
================
*/
func TestMonsterLegStepsTheWatch(t *testing.T) {
	const actors = 3*monsterLegStepActors + 1
	refs := map[uint32]monster.MonsterRef{
		1933: {RefObjID: 1933, TidWord: 0x00C6, Codename: "MOB_CH_MANGNYANG", WalkSpeed: 8, RunSpeed: 22, ScaleDenom: 100, BodyRadius: 6, MaxHP: 54},
	}
	rows := make([]monster.NestRow, actors)
	for i := range rows {
		rows[i] = monster.NestRow{SpawnPoint: monster.SpawnPoint{RefObjID: 1933, RegionID: monsterTestRegion,
			X: 100 + float64(i%32)*50, Y: 20, Z: 100 + float64(i/32)*50}}
	}
	registry := NewMonsterState(monster.TemplateFromParts(refs, rows))
	registry.StartDivision(monsterTestDivision)
	now := time.Now().UnixMilli()
	registry.AdvancePopulation(now)
	ops := &MonsterMoverOps{
		Monsters:   registry,
		TacticsFor: fixedTactics(passiveTactics()),
		Rand:       func() float64 { return 0.5 },
		divisionID: monsterTestDivision,
		watch:      new(tickWatch),
	}
	ops.RunMonsterLeg(now+60_000, nil, &fakePusher{})
	if got := ops.watch.steps.Load(); got < 1 || got > actors/monsterLegStepActors {
		t.Fatalf("%d actors stepped the watch %d times, want 1..%d", actors, got, actors/monsterLegStepActors)
	}
}

/*
================
TestTickStallExitFromEnv
================
*/
func TestTickStallExitFromEnv(t *testing.T) {
	for _, c := range []struct {
		text string
		want time.Duration
	}{{"", DefaultTickStallExit}, {"0", 0}, {"45", 45 * time.Second}, {"soon", DefaultTickStallExit}, {"-1", DefaultTickStallExit}} {
		t.Setenv(EnvTickStallExit, c.text)
		if got := TickStallExitFromEnv(); got != c.want {
			t.Errorf("%q = %v, want %v", c.text, got, c.want)
		}
	}
}

/*
================
TestTickProgressReportsAStallAndRecovers

Readiness's tick signal (#570), on synthetic clocks: not ready before the
first tick, ready while ticks finish, not ready when one runs past the
bound or none finishes within it, and ready again once one finishes.
================
*/
func TestTickProgressReportsAStallAndRecovers(t *testing.T) {
	const bound = 5 * time.Second
	var watch tickWatch
	at := time.Unix(1000, 0)
	if watch.progress(at, bound) == nil {
		t.Fatal("ready before any tick finished")
	}
	watch.begin(at)
	watch.end(at.Add(100 * time.Millisecond))
	if err := watch.progress(at.Add(time.Second), bound); err != nil {
		t.Fatalf("finishing ticks reported %v", err)
	}
	// A tick stuck in a hook.
	watch.begin(at.Add(2 * time.Second))
	if err := watch.progress(at.Add(4*time.Second), bound); err != nil {
		t.Fatalf("a short tick reported %v", err)
	}
	if watch.progress(at.Add(8*time.Second), bound) == nil {
		t.Fatal("a tick running 6 s left the world ready")
	}
	watch.end(at.Add(9 * time.Second))
	if err := watch.progress(at.Add(9*time.Second), bound); err != nil {
		t.Fatalf("the finished tick did not recover readiness: %v", err)
	}
	// The ticker stopped ticking altogether.
	if watch.progress(at.Add(15*time.Second), bound) == nil {
		t.Fatal("no tick for 6 s left the world ready")
	}
}
