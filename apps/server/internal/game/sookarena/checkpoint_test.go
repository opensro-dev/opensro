/*
===========================================================================
checkpoint_test.go - stage gates, long decisions and exact continuation timing.
===========================================================================
*/
package sookarena

import "testing"

/*
================
TestCheckpointClearingAndResume
================
*/
func TestCheckpointClearingAndResume(t *testing.T) {
	r := New(1000)
	end := r.StageStarted + r.CheckpointInterval
	if s := r.Advance(end, 2, false); s.Checkpoint || s.Wave || !s.StageEnd {
		t.Fatal("live mobs must clear")
	}
	if s := r.Advance(end+59999, 2, false); s.Hunt {
		t.Fatal("early hunt")
	}
	if s := r.Advance(end+60000, 2, false); !s.Hunt {
		t.Fatal("missing hunt")
	}
	if s := r.Advance(end+60001, 0, false); !s.Checkpoint || r.Completed != 1 {
		t.Fatal("checkpoint")
	}
	paused := r.PausedAt
	r.DamageUntil = paused + 10000
	r.ResumeAt = paused + ReadyMs
	if s := r.Advance(r.ResumeAt-1, 0, false); s.Resume {
		t.Fatal("early continuation")
	}
	if s := r.Advance(r.ResumeAt, 0, false); !s.Resume || s.Count != 4 || r.DamageUntil != paused+10000+ReadyMs {
		t.Fatal("resume count or power deadline")
	}
}

/*
================
TestCheckpointTimeoutAndDeath
================
*/
func TestCheckpointTimeoutAndDeath(t *testing.T) {
	r := New(1000)
	end := r.StageStarted + r.CheckpointInterval
	r.Advance(end, 0, false)
	if r.DecisionUntil != end+300000 {
		t.Fatal("decision must allow five minutes")
	}
	if s := r.Advance(r.DecisionUntil-1, 0, false); s.Return {
		t.Fatal("early extraction")
	}
	if s := r.Advance(r.DecisionUntil, 0, false); !s.Return || !s.Win {
		t.Fatal("safe extraction")
	}
	r = New(1000)
	r.Advance(end, 0, false)
	if s := r.Advance(end+1, 0, true); s.Return || s.Win {
		t.Fatal("death bypassed return delay")
	}
	if s := r.Advance(end+5001, 0, true); !s.Return || s.Win {
		t.Fatal("death must forfeit")
	}
}

/*
================
TestStagePacingAndRepeatedHunt
================
*/
func TestStagePacingAndRepeatedHunt(t *testing.T) {
	for stage := 0; stage < StageCount; stage++ {
		total := 0
		for wave := 0; wave < 5; wave++ {
			total += WaveSize(stage, wave)
		}
		if total != 9<<stage {
			t.Fatalf("stage %d five-wave budget %d", stage+1, total)
		}
	}
	r := New(1000)
	end := r.StageStarted + 300000
	if s := r.Advance(end-1, 1, false); s.StageEnd {
		t.Fatal("stage stopped before five minutes")
	}
	if s := r.Advance(end, 1, false); !s.StageEnd || s.Box || s.Wave {
		t.Fatal("stage did not stop at five minutes")
	}
	for _, elapsed := range []int64{59999, 60000, 60001, 69999, 70000, 80000} {
		s := r.Advance(end+elapsed, 1, false)
		want := elapsed == 60000 || elapsed == 70000 || elapsed == 80000
		if s.Hunt != want || s.HuntNotice != (elapsed == 60000) {
			t.Fatalf("hunt at %d: %+v", elapsed, s)
		}
	}
}

/*
================
TestEggsContinueThroughClearingButPauseForDecision
================
*/
func TestEggsContinueThroughClearingButPauseForDecision(t *testing.T) {
	r := New(1000)
	end := r.StageStarted + r.CheckpointInterval
	if s := r.Advance(end, 2, false); !s.Powerup {
		t.Fatal("no stage-end egg")
	}
	if s := r.Advance(end+7999, 2, false); s.Powerup {
		t.Fatal("egg cadence too fast")
	}
	if s := r.Advance(end+8000, 2, false); !s.Powerup {
		t.Fatal("clearing stopped eggs")
	}
	r.Advance(end+9000, 0, false)
	if s := r.Advance(end+20000, 0, false); s.Powerup {
		t.Fatal("decision spawned eggs")
	}
}

/*
================
TestFastClearsFinishAllStagesBeforeFiveMinutes
================
*/
func TestFastClearsFinishAllStagesBeforeFiveMinutes(t *testing.T) {
	r := New(1000)
	now := int64(1000)
	for stage := 0; stage < StageCount; stage++ {
		start := r.StageStarted
		if r.WaveLimit() != 15 {
			t.Fatal("stage budget should have fifteen waves")
		}
		waves, bosses := r.Waves, 0
		for r.PausedAt == 0 && now < start+300000 {
			step := r.Advance(now, 0, false)
			if step.Wave {
				waves++
			}
			if step.Unique {
				bosses++
				r.Kill(now)
			}
			// A committed kill is required before an empty arena can accelerate waves.
			for i := 0; i < step.Count; i++ {
				r.Kill(now)
			}
			if step.Checkpoint {
				break
			}
			now += 100
		}
		if r.PausedAt == 0 || r.Completed != stage+1 || waves != 15 || bosses != 1 {
			t.Fatalf("stage %d: waves=%d boss=%d run=%+v", stage+1, waves, bosses, r)
		}
		if now-start >= 300000 {
			t.Fatal("fast clear waited five minutes")
		}
		if step := r.Advance(now+100, 0, false); step.Box || step.Wave {
			t.Fatal("wave escaped checkpoint")
		}
		if stage < StageCount-1 {
			r.ResumeAt = now + ReadyMs
			now = r.ResumeAt
			step := r.Advance(now, 0, false)
			if !step.Wave {
				t.Fatal("next stage opening")
			}
			for i := 0; i < step.Count; i++ {
				r.Kill(now)
			}
		}
	}
}

/*
================
TestUnkilledStageStillUsesTimerAndFiveMinuteStop
================
*/
func TestUnkilledStageStillUsesTimerAndFiveMinuteStop(t *testing.T) {
	r := New(1000)
	r.Advance(19800, 0, false)
	first := r.Advance(21000, 0, false)
	if !first.Wave {
		t.Fatal("first wave")
	}
	if r.Advance(35999, first.Count, false).Box {
		t.Fatal("survivors accelerated the timer")
	}
	if !r.Advance(36000, first.Count, false).Box {
		t.Fatal("survivors blocked timed reinforcement")
	}
	if step := r.Advance(r.StageStarted+300000, first.Count, false); !step.StageEnd || step.Checkpoint {
		t.Fatal("uncleared stage did not stop spawning at five minutes")
	}
}

/*
================
TestCappedPendingWaveDoesNotSpendBudget
================
*/
func TestCappedPendingWaveDoesNotSpendBudget(t *testing.T) {
	r := New(1000)
	if !r.Advance(19800, MaxAlive-1, false).Box {
		t.Fatal("pending wave")
	}
	// An ambush can fill the last slot during the Pandora windup.
	if step := r.Advance(21000, MaxAlive, false); step.Wave || step.Count != 0 || r.Waves != 0 {
		t.Fatal("full arena spent an empty wave")
	}
}

/*
================
TestStageWaveCapRespectsShortenedTimers
================
*/
func TestStageWaveCapRespectsShortenedTimers(t *testing.T) {
	r := New(1000)
	for _, tc := range []struct {
		ms    int64
		waves int
	}{{30000, 2}, {300000, 15}, {600000, 15}} {
		r.CheckpointInterval = tc.ms
		if r.WaveLimit() != tc.waves {
			t.Fatalf("timer=%d waves=%d want=%d", tc.ms, r.WaveLimit(), tc.waves)
		}
	}
}
