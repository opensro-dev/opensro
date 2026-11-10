/*
===========================================================================

arena.go - the port-only, server-clock survival programme at Event So-ok

This package owns scheduling and daily admission, not actors or persistence.
The action owner serializes each run with its division's existing lock.
No wall-clock timers survive an instance lease.

===========================================================================
*/
package sookarena

import (
	"math"
	"strings"
	"time"
)

const (
	Env                      = "SRO_SOOK_SURVIVAL_ARENA"
	Service                  = "port:sook-survival-arena"
	NPC                      = "NPC_CH_EVENT_KISAENG1"
	DurationMs         int64 = 25 * 60 * 1000
	OpeningDelayMs     int64 = 20000
	DeathReturnDelayMs int64 = 5000
	PandoraWindupMs    int64 = 1200
	WaveIntervalMs     int64 = 15000
	MaxAlive                 = 200
	StageCount               = 5
	DecisionMs         int64 = 300000
	ReadyMs            int64 = 15000
)

/*
================
KillReward - port-only end-of-match gold; no kills means no payout.
================
*/
func KillReward(kills int) uint64 {
	if kills <= 0 {
		return 0
	}
	for _, ceiling := range [...]int{10, 30, 60, 100, 200, 500, 1000, 2000, 3500, 5000} {
		if kills <= ceiling {
			return uint64(ceiling) * 1000
		}
	}
	return 5000000
}

/*
================
Enabled - port-only, not native; absent and unknown values stay native.
================
*/
func Enabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "on":
		return true
	}
	return false
}

/*
================
Day - UTC calendar day, independent of the host timezone.
================
*/
func Day(now time.Time) int32 { return int32(now.UTC().Unix() / 86400) }

/*
================
Admit - only the successful relocation transaction stores this result.
================
*/
func Admit(day int32, entries uint8, now time.Time) (int32, uint8, bool) {
	today := Day(now)
	if day != today {
		entries = 0
	}
	if entries >= 3 {
		return day, entries, false
	}
	return today, entries + 1, true
}

/*
================
Run - a single allocated world's monotonic schedule, never reused.
================
*/
type Run struct {
	CheckpointInterval, PausedAt, DecisionUntil, ResumeAt     int64
	StageStarted, ClearingAt, LastCountdown                   int64
	LastHunt                                                  int64
	Waves                                                     int
	Completed, CheckpointKills                                int
	ReflectUntil, TransferUntil, SpeedUntil                   int64
	Started, NextWave, NextPowerup, PendingWave, DeadAt       int64
	Kills                                                     int
	Unique                                                    int
	DamageUntil, UnstoppableUntil, InvincibleUntil, ManaUntil int64
	DamageMultiplier                                          uint32
	dead                                                      bool
}

/*
================
Step - bounded work for one host update; missed waves never pile up.
================
*/
type Step struct {
	Checkpoint, Resume                      bool
	StageEnd, Hunt, HuntNotice              bool
	Countdown                               int64
	Box, Wave, Powerup, Unique, Win, Return bool
	Count, Phase                            int
}

/*
================
New
================
*/
func New(now int64) Run {
	return Run{Started: now, StageStarted: now + OpeningDelayMs, CheckpointInterval: 300000, NextWave: now + OpeningDelayMs - PandoraWindupMs, NextPowerup: now + 15000}
}

/*
================
Phase
================
*/
func (r *Run) Phase(_ int64) int { return min(StageCount-1, r.Completed) }

/*
================
Kill
================
*/
func (r *Run) Kill(_ int64) {
	r.Kills++
}

/*
================
Advance - death beats victory, including a fatal blow at the deadline.
================
*/
func (r *Run) Advance(now int64, alive int, dead bool) Step {
	if dead {
		if !r.dead {
			r.dead = true
			r.DeadAt = now
		}
		return Step{Return: now >= r.DeadAt+DeathReturnDelayMs}
	}
	if r.dead {
		return Step{Return: true}
	}
	if r.PausedAt != 0 {
		if r.ResumeAt != 0 {
			left := max(int64(0), (r.ResumeAt-now+999)/1000)
			if now >= r.ResumeAt {
				r.Shift(now - r.PausedAt)
				r.PausedAt, r.DecisionUntil, r.ResumeAt = 0, 0, 0
				r.StageStarted = now
				r.ClearingAt = 0
				r.LastHunt = 0
				r.Waves = 1
				r.NextWave = now + WaveIntervalMs
				return Step{Resume: true, Wave: true, Count: WaveSize(r.Completed, 0), Phase: r.Completed, Powerup: true}
			}
			step := Step{}
			if left != r.LastCountdown {
				r.LastCountdown = left
				step.Countdown = left
			}
			return step
		}
		return Step{Return: now >= r.DecisionUntil, Win: true}
	}
	step := Step{Phase: r.Completed}
	if now < r.Started+OpeningDelayMs && r.Completed == 0 {
		left := (r.Started + OpeningDelayMs - now + 999) / 1000
		if left != r.LastCountdown {
			r.LastCountdown = left
			step.Countdown = left
		}
	}
	if now >= r.NextPowerup {
		step.Powerup = true
		r.NextPowerup = now + 8000
	}
	if now >= r.StageStarted+r.CheckpointInterval || r.Waves >= r.WaveLimit() && r.Unique > r.Completed {
		r.PendingWave = 0
		if r.ClearingAt == 0 {
			r.ClearingAt = now
			step.StageEnd = true
		}
		if alive == 0 {
			r.Completed++
			r.CheckpointKills = r.Kills
			r.PausedAt = now
			r.DecisionUntil = now + DecisionMs
			step.Checkpoint = true
			return step
		}
		if now >= r.ClearingAt+60000 && (r.LastHunt == 0 || now >= r.LastHunt+10000) {
			step.HuntNotice = r.LastHunt == 0
			r.LastHunt = now
			step.Hunt = true
		}
		return step
	}
	if r.Unique <= r.Completed && (now >= r.StageStarted+r.CheckpointInterval-30000 || r.Waves >= r.WaveLimit()-1) && alive < MaxAlive {
		step.Unique = true
		r.Unique++
		alive++
	}
	if r.PendingWave != 0 {
		if now >= r.PendingWave {
			step.Count = max(0, min(MaxAlive-alive, WaveSize(r.Completed, r.Waves)))
			step.Wave = step.Count > 0
			if step.Wave {
				r.Waves++
			}
			r.PendingWave = 0
			r.NextWave = now + WaveIntervalMs
		}
	} else if r.Waves < r.WaveLimit() && alive < MaxAlive && (now >= r.NextWave || alive == 0 && r.Kills > r.CheckpointKills) {
		step.Box = true
		r.PendingWave = now + PandoraWindupMs
	}
	return step
}

/*
================
Shift - exclude checkpoint/ready-screen time from the arena's deadlines.
================
*/
func (r *Run) Shift(delta int64) {
	r.Started += delta
	for _, at := range []*int64{&r.NextWave, &r.NextPowerup, &r.PendingWave, &r.DamageUntil, &r.UnstoppableUntil, &r.InvincibleUntil, &r.ManaUntil, &r.ReflectUntil, &r.TransferUntil, &r.SpeedUntil} {
		if *at != 0 {
			*at += delta
		}
	}
}

/*
================
WaveRarity - port-only mix: 70% normal, 20% champion, 10% elite (native grade 6).
================
*/
func WaveRarity(sequence int) uint8 {
	switch sequence % 10 {
	case 7, 8:
		return 1
	case 9:
		return 6
	}
	return 0
}

/*
================
Fivefold - saturate before reward arithmetic can wrap.
================
*/
func Fivefold(value int64) int64 {
	if value <= 0 {
		return value
	}
	if value > math.MaxInt64/5 {
		return math.MaxInt64
	}
	return value * 5
}

/*
================
WaveSize - retain three fifths of each stage's original wave budget.
================
*/
func WaveSize(stage, wave int) int {
	base := 3 << stage
	return ((wave+1)*base*3+4)/5 - (wave*base*3+4)/5
}

/*
================
WaveLimit - retain the timed stage's wave budget when clears accelerate it.
================
*/
func (r *Run) WaveLimit() int {
	return 1 + int(max(int64(0), r.CheckpointInterval-1)/(WaveIntervalMs+PandoraWindupMs))
}
