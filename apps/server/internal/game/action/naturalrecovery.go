/*
===========================================================================

naturalrecovery.go - natural HP / MP recovery ticks (4A6DF0 / 4A9D00 / 4E2990)

Authenticated residents own the recovery clock. Each pulse reads equipment,
active effects and posture through the ordinary keeper before committing HP
and MP. Quest minute callbacks share residency, not the recovery cadence.

===========================================================================
*/

package action

import (
	"sort"
	"strings"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/recovery"
	"opensro.online/server/internal/game/paramkeeper"
	"opensro.online/server/internal/game/world/simulation"
)

// Later GameServer: 4A6DF0 registers a four-second callback, 4A9D00
// selects 0.8/8 percent for standing/sitting, and 4E2990 applies it.
const (
	naturalRecoveryIntervalMs int64   = 4000
	questMinuteIntervalMs     int64   = 60000
	questItemIntervalMs       int64   = 1000
	standingRecoveryRate      float32 = 0.8
	sittingRecoveryRate       float32 = 8
	recoveryPostureSource     uint32  = 2
)

/*
================
recoveryKey
================
*/
type recoveryKey struct{ division, name string }

/*
================
recoverySession

Replacement sessions start fresh clocks; repeat admission preserves phase.
================
*/
type recoverySession struct {
	session                    uint64
	character                  *enterworld.Character
	nextMs                     int64
	questNextMs                int64
	questItemNextMs            int64
	potions                    recovery.Queue
	potionNextMs, potionLastMs int64
}

/*
================
BindRecoverySession

Only authenticated world residents receive natural recovery and quest pulses.
================
*/
func (rt *Runtime) BindRecoverySession(division string, c *enterworld.Character, session uint64) {
	if c == nil || session == 0 {
		return
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	key := recoveryKey{division, strings.ToLower(c.Name)}
	rt.recoveryMu.Lock()
	defer rt.recoveryMu.Unlock()
	if old := rt.recoverySessions[key]; old != nil && old.session == session && old.character == c {
		return
	}
	if rt.recoverySessions == nil {
		rt.recoverySessions = make(map[recoveryKey]*recoverySession)
	}
	now := rt.Now().UnixMilli()
	rt.recoverySessions[key] = &recoverySession{session: session, character: c, nextMs: now + naturalRecoveryIntervalMs, questNextMs: now + questMinuteIntervalMs}
	rt.recoverySessions[key].questItemNextMs = now + questItemIntervalMs
	rt.recoverySessions[key].potionNextMs = now + potionRecoveryIntervalMs
}

/*
================
forgetRecoverySession

The caller holds the division door before taking the collection lock.
================
*/
func (rt *Runtime) forgetRecoverySession(division, name string) {
	rt.recoveryMu.Lock()
	key := recoveryKey{division, strings.ToLower(name)}
	state := rt.recoverySessions[key]
	delete(rt.recoverySessions, key)
	rt.recoveryMu.Unlock()
	if state != nil && rt.ForgetQuestItem != nil {
		rt.ForgetQuestItem(state.character)
	}
}

/*
================
naturalRecoveryAmount

4E2AA0 reads the evaluated recovery rate, truncates the gauge product and
clamps each pulse to half the maximum. Posture and buffs are already composed.
================
*/
func naturalRecoveryAmount(maximum int64, rate float32) int64 {
	if maximum <= 0 {
		return 0
	}
	// Native converts the unsigned maximum to float32 before x87 multiply
	// and truncation. Do not replace this with rounded integer percentages.
	cap := float64(float32(maximum))
	return min(max(int64(cap*float64(rate)/100), 1), int64(cap*0.5))
}

/*
================
advanceNaturalRecovery

Snapshot due keys before entering division doors so collection readers never
hold the recovery lock while waiting for a gameplay operation.
================
*/
func (rt *Runtime) advanceNaturalRecovery(nowMs int64) []simulation.DivisionFrames {
	rt.recoveryMu.Lock()
	var keys []recoveryKey
	for key, state := range rt.recoverySessions {
		if nowMs >= state.potionNextMs || nowMs >= state.nextMs || rt.AdvanceQuestMinute != nil && nowMs >= state.questNextMs ||
			rt.AdvanceQuestItem != nil && nowMs >= state.questItemNextMs {
			keys = append(keys, key)
		}
	}
	rt.recoveryMu.Unlock()
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].division != keys[j].division {
			return keys[i].division < keys[j].division
		}
		return keys[i].name < keys[j].name
	})
	var out []simulation.DivisionFrames
	for _, key := range keys {
		unlock := rt.lockDivision(key.division)
		out = append(out, rt.recoverPotionResident(key, nowMs)...)
		out = append(out, rt.advanceResidentQuestItem(key, nowMs)...)
		out = append(out, rt.advanceResidentQuestMinute(key, nowMs)...)
		frames := rt.recoverResident(key, nowMs)
		out = append(out, frames...)
		unlock()
	}
	return out
}

/*
================
advanceResidentQuestItem

Short tool timers share authenticated residency but keep their one-second
cadence separate from the capture-minute and natural recovery clocks.
================
*/
func (rt *Runtime) advanceResidentQuestItem(key recoveryKey, nowMs int64) []simulation.DivisionFrames {
	if rt.AdvanceQuestItem == nil {
		return nil
	}
	rt.recoveryMu.Lock()
	state := rt.recoverySessions[key]
	if state == nil || nowMs < state.questItemNextMs {
		rt.recoveryMu.Unlock()
		return nil
	}
	state.questItemNextMs += questItemIntervalMs
	c := state.character
	rt.recoveryMu.Unlock()
	frames := rt.AdvanceQuestItem(c, nowMs)
	if len(frames) == 0 {
		return nil
	}
	return []simulation.DivisionFrames{{DivisionID: key.division, OnlyCharacterID: c.ID, Frames: simFrames(frames)}}
}

/*
==================
advanceResidentQuestMinute

The same admitted actor owns recovery and quest pulses. Native 4A7050
registers 4AC700 at 60 seconds -> 4EB7C0 -> 605400 -> 52B030 (event 14).
Preserve one callback per update and residual phase (4AB700).
==================
*/
func (rt *Runtime) advanceResidentQuestMinute(key recoveryKey, nowMs int64) []simulation.DivisionFrames {
	if rt.AdvanceQuestMinute == nil {
		return nil
	}
	rt.recoveryMu.Lock()
	state := rt.recoverySessions[key]
	if state == nil || nowMs < state.questNextMs {
		rt.recoveryMu.Unlock()
		return nil
	}
	state.questNextMs += questMinuteIntervalMs
	c := state.character
	rt.recoveryMu.Unlock()
	frames := rt.AdvanceQuestMinute(c)
	if len(frames) == 0 {
		return nil
	}
	out := simulation.DivisionFrames{DivisionID: key.division, OnlyCharacterID: c.ID}
	for _, frame := range frames {
		out.Frames = append(out.Frames, simulation.Frame{Opcode: frame.Opcode, Payload: frame.Payload, Current: frame.Current, Scope: frame.Scope})
	}
	return []simulation.DivisionFrames{out}
}

/*
================
recoverResident

An overrun consumes one period per update, including blocked pulses. A failed
stat projection leaves stored gauges untouched rather than healing at a
guessed rate that ignores equipped options or active effects.
================
*/
func (rt *Runtime) recoverResident(key recoveryKey, nowMs int64) []simulation.DivisionFrames {
	rt.recoveryMu.Lock()
	state := rt.recoverySessions[key]
	if state == nil || nowMs < state.nextMs {
		rt.recoveryMu.Unlock()
		return nil
	}
	// Native 4AB729 subtracts one period and invokes once per owner update.
	// Keep the residual phase, including pulses skipped while moving/dead.
	state.nextMs += naturalRecoveryIntervalMs
	rt.recoveryMu.Unlock()
	c := state.character
	if rt.hasOpenSkillCast(key.division, c.Name) {
		return nil
	}
	var frames []simulation.DivisionFrames
	committed := rt.deps.Update(c, "natural-recovery", func() bool {
		if c.DeletePending || !enterworld.CharacterAlive(c) {
			return false
		}
		world := rt.Worlds.Snapshot(simulation.WorldKey(key.division, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) })
		if nowMs < world.PostureTransitionUntilMs || world.MovingAt(nowMs) {
			return false
		}
		rate := standingRecoveryRate
		if world.Sitting {
			rate = sittingRecoveryRate
		}
		// 4A9D00 writes source 2 to the flat channel. Keeping the base in
		// the common graph also admits equipment recovery magic options.
		writes := rt.effects.ModifierWrites(key.division, c.Name)
		writes = append(writes,
			paramkeeper.Write{Parameter: itemParamHPRecovery, Source: recoveryPostureSource, Value: rate},
			paramkeeper.Write{Parameter: itemParamMPRecovery, Source: recoveryPostureSource, Value: rate},
		)
		stats, _, err := combat.PlayerStatsWithModifiers(c, rt.statCatalogs(), writes, rt.playerAbnormal(key.division, c.Name))
		if err != nil {
			return false
		}
		hpLimit, _ := stats.Param(itemParamMaxHP)
		mpLimit, _ := stats.Param(itemParamMaxMP)
		hpRate, _ := stats.Param(itemParamHPRecovery)
		mpRate, _ := stats.Param(itemParamMPRecovery)
		maxHP, maxMP := int64(hpLimit), int64(mpLimit)
		hp, mp := clampKeeperVital(c.CurrentHP, maxHP), clampKeeperVital(c.CurrentMP, maxMP)
		nextHP, nextMP := hp, mp
		if hp < maxHP {
			nextHP = min(maxHP, hp+naturalRecoveryAmount(maxHP, hpRate))
		}
		if mp < maxMP {
			nextMP = min(maxMP, mp+naturalRecoveryAmount(maxMP, mpRate))
		}
		if nextHP == hp && nextMP == mp {
			return false
		}
		c.CurrentHP, c.CurrentMP = &nextHP, &nextMP
		frames = []simulation.DivisionFrames{{DivisionID: key.division, OnlyCharacterID: c.ID, Frames: []simulation.Frame{{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshWithSourcePayload(enterworld.ObjectIDForCharacter(c), simulation.VitalsSourceNaturalRecovery, simulation.Vitals{CurrentHP: uint32(nextHP), CurrentMP: uint32(nextMP)})}}}}
		return true
	})
	if !committed {
		return nil
	}
	return frames
}
