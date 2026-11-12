/*
===========================================================================

arena_test.go - deterministic admission, pacing, deadline and reward checks

===========================================================================
*/
package sookarena

import (
	"math"
	"opensro.online/server/internal/game/world/monster"
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
TestRefundGivesBackOnlyTheSpentDaysEntry

A failed entry refunds one entry of the day it spent, never more than
the count holds, and nothing once the count has rolled to a later day
(#3322).
================
*/
func TestRefundGivesBackOnlyTheSpentDaysEntry(t *testing.T) {
	cases := []struct {
		day      int32
		entries  uint8
		spentDay int32
		want     uint8
	}{
		{day: 7, entries: 2, spentDay: 7, want: 1},
		{day: 7, entries: 0, spentDay: 7, want: 0},
		{day: 8, entries: 1, spentDay: 7, want: 1},
	}
	for _, tc := range cases {
		if got := Refund(tc.day, tc.entries, tc.spentDay); got != tc.want {
			t.Fatalf("Refund(%d, %d, %d) = %d, want %d", tc.day, tc.entries, tc.spentDay, got, tc.want)
		}
	}
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	if Used(Day(now), 2, now) != 2 || Used(Day(now)-1, 3, now) != 0 {
		t.Fatal("Used counts another day's entries")
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
	if step := a.Advance(21000, MaxAlive-1, false); !step.Wave || step.Count != 1 {
		t.Fatalf("cap %+v", step)
	}
	for range 15 {
		a.Kill(21100)
	}
	if a.Kills != 15 || b.Kills != 0 {
		t.Fatal("run isolation")
	}
	if a.Advance(23500, MaxAlive, false).Box {
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
	if b.Advance(1000+DurationMs, 1, false).Win {
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
	if Fivefold(math.MaxInt64) != math.MaxInt64 || Fivefold(12) != 60 {
		t.Fatal("reward overflow")
	}

	for _, kills := range []int{0, 10, 30, 90, 1000} {
		r := New(1000)
		r.Kills = kills
		r.Advance(19800, 0, false)
		if step := r.Advance(21000, 0, false); step.Count != 3 {
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
	for stage := 0; stage < 5; stage++ {
		now := r.StageStarted + r.CheckpointInterval
		if step := r.Advance(now, 1, false); !step.StageEnd || step.Checkpoint || step.Wave {
			t.Fatal("stage must clear before rewards")
		}
		if step := r.Advance(now+60000, 1, false); !step.Hunt {
			t.Fatal("survivors did not hunt after one minute")
		}
		if step := r.Advance(now+60001, 0, false); !step.Checkpoint || r.Completed != stage+1 {
			t.Fatal("cleared stage missing")
		}
		if stage < 4 {
			r.ResumeAt = now + 60001 + ReadyMs
			step := r.Advance(r.ResumeAt, 0, false)
			if !step.Resume || !step.Wave || step.Count != WaveSize(stage+1, 0) {
				t.Fatal("next stage did not use its scheduled wave size")
			}
		}
	}
	if step := r.Advance(r.DecisionUntil, 0, false); !step.Return || !step.Win {
		t.Fatal("final extraction")
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
	r.Completed = 4
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
TestStageMonsterLevelBands
================
*/
func TestStageMonsterLevelBands(t *testing.T) {
	pool := Pool{}
	for level := 1; level <= 110; level++ {
		pool.Normal = append(pool.Normal, monster.MonsterRef{Level: uint8(level)})
	}
	for stage, band := range [][2]int{{82, 85}, {84, 87}, {87, 90}, {89, 92}, {91, 94}} {
		low, high := StageBounds(90, stage)
		refs := pool.StageNormals(90, stage)
		if low != band[0] || high != band[1] || len(refs) != band[1]-band[0]+1 || int(refs[0].Level) != band[0] || int(refs[len(refs)-1].Level) != band[1] {
			t.Fatalf("stage %d: bounds %d..%d, refs %+v; want %v", stage+1, low, high, refs, band)
		}
		if floor, ceiling := StageBounds(1, stage); floor < 1 || ceiling < floor {
			t.Fatal("low-level bounds exceed the level floor", floor, ceiling)
		}
	}
}
