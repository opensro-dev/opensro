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
	DurationMs         int64 = 20 * 60 * 1000
	UltraAtMs          int64 = 15 * 60 * 1000
	OpeningDelayMs     int64 = 20000
	DeathReturnDelayMs int64 = 5000
	PandoraWindupMs    int64 = 1200
	PreUltraWaveMs     int64 = 12000
	// Include the unchanged Pandora windup when multiplying the actual spawn rate.
	UltraWaveMs int64 = (PreUltraWaveMs+PandoraWindupMs)/5 - PandoraWindupMs
	MaxAlive          = 100
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
	ReflectUntil                                              int64
	Started, NextWave, NextPowerup, PendingWave, DeadAt       int64
	Kills                                                     int
	Unique                                                    int
	DamageUntil, UnstoppableUntil, InvincibleUntil, ManaUntil int64
	DamageMultiplier                                          uint32
	milestone                                                 int
	dead                                                      bool
	ultra                                                     bool
}

/*
================
Step - bounded work for one host update; missed waves never pile up.
================
*/
type Step struct {
	Box, Wave, Powerup, Unique, Win, Return, Ultra bool
	Count, Phase, Reinforcements                   int
	RemainingSeconds                               int64
}

/*
================
New
================
*/
func New(now int64) Run {
	return Run{Started: now, NextWave: now + OpeningDelayMs - PandoraWindupMs, NextPowerup: now + 15000}
}

/*
================
Phase
================
*/
func (r *Run) Phase(now int64) int { return int(max(0, min(3, (now-r.Started)/300000))) }

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
	if now >= r.Started+DurationMs {
		return Step{Win: true, Return: true}
	}
	p := r.Phase(now)
	// Timed waves may reinforce survivors; retain only the hard population limit.
	limit := MaxAlive
	step := Step{Phase: p}
	if !r.ultra && now >= r.Started+UltraAtMs {
		r.ultra, step.Ultra = true, true
		r.NextWave = min(r.NextWave, now)
		step.Reinforcements = min(40, max(0, limit-alive))
		alive += step.Reinforcements
	}
	threshold := [...]int64{300000, 600000, 900000}
	for r.milestone < len(threshold) {
		if now-r.Started < threshold[r.milestone] {
			break
		}
		step.RemainingSeconds = (DurationMs - (now - r.Started)) / 1000
		r.milestone++
	}
	if now >= r.NextPowerup {
		step.Powerup = true
		r.NextPowerup = now + 15000
	}
	uniqueTimes := [...]int64{270000, 570000, 870000, 1170000}
	if r.Unique < len(uniqueTimes) && now-r.Started >= uniqueTimes[r.Unique] && alive < limit {
		step.Unique = true
		r.Unique++
		alive++
	}
	if r.PendingWave != 0 {
		if now >= r.PendingWave {
			step.Wave = true
			step.Count = max(0, min(limit-alive, 3+min(3, r.Kills/30)))
			r.PendingWave = 0
			interval := [...]int64{15000, 13500, PreUltraWaveMs, UltraWaveMs}
			r.NextWave = now + interval[p]
		}
	} else if alive < limit && (now >= r.NextWave || alive == 0 && r.Kills > 0) {
		step.Box = true
		r.PendingWave = now + PandoraWindupMs
	}
	return step
}

/*
================
EncounterLevel - port-only timed progression, reaching player +6 with ultra mode.
================
*/
func EncounterLevel(playerLevel int, elapsedMs int64) int {
	return max(1, playerLevel-6+int(min(UltraAtMs, max(0, elapsedMs))*12/UltraAtMs))
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
Tenfold - saturate before reward arithmetic can wrap.
================
*/
func Tenfold(value int64) int64 {
	if value <= 0 {
		return value
	}
	if value > math.MaxInt64/10 {
		return math.MaxInt64
	}
	return value * 10
}
