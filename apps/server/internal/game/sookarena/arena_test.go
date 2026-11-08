/*
===========================================================================

arena_test.go - deterministic admission, pacing, deadline and reward checks

===========================================================================
*/
package sookarena

import (
	"math"
	"testing"
	"time"
)

/*
================
TestAdmission
================
*/
func TestAdmission(t *testing.T) {
	for _, value := range []string{"", "off", "0", "false", "garbage"} {
		if Enabled(value) {
			t.Fatalf("enabled %q", value)
		}
	}
	for _, value := range []string{"on", "1", "TRUE"} {
		if !Enabled(value) {
			t.Fatalf("disabled %q", value)
		}
	}
	now := time.Date(2026, 11, 6, 23, 59, 59, 0, time.UTC)
	day, count := int32(0), uint8(0)
	for range 3 {
		var ok bool
		day, count, ok = Admit(day, count, now)
		if !ok {
			t.Fatal("refused daily slot")
		}
	}
	if _, _, ok := Admit(day, count, now); ok {
		t.Fatal("fourth entry admitted")
	}
	if _, count, ok := Admit(day, count, now.Add(time.Second)); !ok || count != 1 {
		t.Fatal("UTC reset")
	}
}

/*
================
TestScheduleCapsAndLeaseLocalState
================
*/
func TestScheduleCapsAndLeaseLocalState(t *testing.T) {
	a, b := New(1000), New(1000)
	if !a.Advance(19800, 0, false).Box || a.Advance(20999, 0, false).Wave {
		t.Fatal("box windup")
	}
	if step := a.Advance(21000, 99, false); !step.Wave || step.Count != 1 {
		t.Fatalf("cap %+v", step)
	}
	for range 15 {
		a.Kill(21100)
	}
	if a.Kills != 15 || b.Kills != 0 {
		t.Fatal("run isolation")
	}
	if a.Advance(23500, 100, false).Box {
		t.Fatal("live cap")
	}
	if a.Advance(23500, 1, false).Box || a.NextWave != 36000 {
		t.Fatal("kills accelerated the next wave")
	}
	if !a.Advance(36000, 0, false).Box {
		t.Fatal("scheduled next wave missing")
	}
	if a.Advance(1000+DurationMs, 0, true).Win {
		t.Fatal("death must beat deadline")
	}
	if a.Advance(1000+DurationMs+DeathReturnDelayMs-1, 0, true).Return {
		t.Fatal("death return before five seconds")
	}
	if !a.Advance(1000+DurationMs+DeathReturnDelayMs, 0, true).Return {
		t.Fatal("death return")
	}
	if !b.Advance(1000+DurationMs, 0, false).Win {
		t.Fatal("victory")
	}
}

/*
================
TestClearingArenaStartsOnePandoraWithoutWaiting
================
*/
func TestClearingArenaStartsOnePandoraWithoutWaiting(t *testing.T) {
	r := New(1000)
	if r.Advance(1001, 0, false).Box {
		t.Fatal("empty initial arena skipped its opening delay")
	}
	r.Advance(19800, 0, false)
	if wave := r.Advance(21000, 0, false); wave.Count != 3 {
		t.Fatal("opening wave missing")
	}
	for range 3 {
		r.Kill(21100)
	}
	if !r.Advance(21100, 0, false).Box {
		t.Fatal("clear did not start the next Pandora immediately")
	}
	if step := r.Advance(21101, 0, false); step.Box || step.Wave {
		t.Fatal("clear queued duplicate Pandoras or skipped the windup")
	}
	if step := r.Advance(22300, 0, false); !step.Wave || step.Count != 3 {
		t.Fatal("next wave did not follow the Pandora windup")
	}
	if r.Advance(22301, 3, false).Box {
		t.Fatal("living enemies bypassed ordinary pacing")
	}
}

/*
================
TestRewardAndKillProgression
================
*/
func TestRewardAndKillProgression(t *testing.T) {
	if Tenfold(math.MaxInt64) != math.MaxInt64 || Tenfold(12) != 120 {
		t.Fatal("reward overflow")
	}
	for _, tc := range []struct {
		level   int
		elapsed int64
		want    int
	}{{90, 0, 84}, {90, -1, 84}, {90, 300000, 88}, {90, 600000, 92}, {90, 899999, 95}, {90, 900000, 96}, {90, 1199999, 96}, {90, DurationMs, 96}, {1, 0, 1}} {
		if got := EncounterLevel(tc.level, tc.elapsed); got != tc.want {
			t.Fatalf("level=%d elapsed=%d: got %d want %d", tc.level, tc.elapsed, got, tc.want)
		}
	}
	for _, kills := range []int{0, 10, 30, 90, 1000} {
		r := New(1000)
		r.Kills = kills
		r.Advance(19800, 0, false)
		if step := r.Advance(21000, 0, false); step.Count != 3+min(3, kills/30) {
			t.Fatalf("kills=%d wave=%+v", kills, step)
		}
	}
}

/*
================
TestUltraBoundaryCadenceAndTwentyMinuteFinish
================
*/
func TestUltraBoundaryCadenceAndTwentyMinuteFinish(t *testing.T) {
	r := New(1000)
	r.NextWave = r.Started + UltraAtMs + 10000
	if step := r.Advance(r.Started+UltraAtMs-1, 10, false); step.Ultra || step.Box {
		t.Fatal("ultra mode began early")
	}
	if step := r.Advance(r.Started+UltraAtMs, 10, false); !step.Ultra || !step.Box {
		t.Fatal("ultra mode did not announce and begin immediately")
	}
	if step := r.Advance(r.Started+UltraAtMs+1, 10, false); step.Ultra || step.Box {
		t.Fatal("ultra announcement or Pandora duplicated")
	}
	at := r.Started + UltraAtMs + PandoraWindupMs
	if step := r.Advance(at, 10, false); !step.Wave || step.Count == 0 {
		t.Fatal("ultra wave missing")
	}
	if (r.NextWave-at+PandoraWindupMs)*5 != PreUltraWaveMs+PandoraWindupMs {
		t.Fatal("ultra spawn cycle is not five times faster")
	}
	if r.Advance(r.Started+DurationMs-1, 100, false).Return {
		t.Fatal("match ended early")
	}
	if step := r.Advance(r.Started+DurationMs, 100, false); !step.Return || !step.Win || step.Box || step.Wave {
		t.Fatal("match did not end at exactly twenty minutes")
	}
}

/*
================
TestWaveRarityMix
================
*/
func TestWaveRarityMix(t *testing.T) {
	counts := map[uint8]int{}
	for sequence := range 100 {
		counts[WaveRarity(sequence)]++
	}
	if counts[0] != 70 || counts[1] != 20 || counts[6] != 10 || len(counts) != 3 {
		t.Fatalf("wave grades: %+v", counts)
	}
}

/*
================
TestTimedWavesContinueWithSurvivorsAndRespectHardCap
================
*/
func TestTimedWavesContinueWithSurvivorsAndRespectHardCap(t *testing.T) {
	r := New(1000)
	alive := 0
	for now := int64(1000); now < 1000+DurationMs; now += 100 {
		step := r.Advance(now, alive, false)
		alive += step.Count
		if step.Unique {
			alive++
		}
		if alive > MaxAlive {
			t.Fatalf("crowd exceeded hard cap: %d", alive)
		}
		if now == 100000 && alive <= 6 {
			t.Fatal("timed waves stopped after the first six survivors")
		}
	}
	if alive != MaxAlive {
		t.Fatalf("timed waves stopped early: alive=%d", alive)
	}
}

/*
================
TestKillRewardBrackets
================
*/
func TestKillRewardBrackets(t *testing.T) {
	for _, tc := range []struct {
		kills int
		gold  uint64
	}{
		{-1, 0}, {0, 0}, {1, 10000}, {10, 10000}, {11, 30000}, {30, 30000},
		{31, 60000}, {60, 60000}, {61, 100000}, {100, 100000},
		{101, 200000}, {200, 200000}, {201, 500000}, {500, 500000},
		{501, 1000000}, {1000, 1000000}, {1001, 2000000}, {2000, 2000000},
		{2001, 3500000}, {3500, 3500000}, {3501, 5000000}, {5000, 5000000}, {5001, 5000000},
	} {
		if got := KillReward(tc.kills); got != tc.gold {
			t.Fatalf("%d kills: gold=%d want=%d", tc.kills, got, tc.gold)
		}
	}
}

/*
================
TestDoomsDayAndFiveMinuteReports
================
*/
func TestDoomsDayAndFiveMinuteReports(t *testing.T) {
	for _, alive := range []int{0, 90, MaxAlive} {
		r := New(1000)
		for _, minutes := range []int64{5, 10, 15} {
			step := r.Advance(r.Started+minutes*60000, alive, false)
			if step.RemainingSeconds != (20-minutes)*60 {
				t.Fatalf("minute %d: report %+v", minutes, step)
			}
			if minutes == 15 && (!step.Ultra || step.Reinforcements != min(40, MaxAlive-alive)) {
				t.Fatalf("dooms day: %+v", step)
			}
			if again := r.Advance(r.Started+minutes*60000+1, alive, false); again.RemainingSeconds != 0 || again.Reinforcements != 0 {
				t.Fatal("duplicate milestone")
			}
		}
	}
}
