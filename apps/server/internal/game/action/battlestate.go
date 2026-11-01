/*
===========================================================================

battlestate.go - the player battle state

The reference server's battle-state setter stores state+0xD (state channel 8)
and, on entry, arms the 20-count timer at +0x203C; a change of value publishes
0x30BF through its state-channel broadcast, 0x3122 channel 8
on the v1.150 wire (client 777E84 -> localPlayer +0x781).
Its state-timer tick counts the timer down and leaves battle at
zero. A player enters battle by striking (vtable +0x508), by being struck
(from the hit path) and by a hit on their pet (the pet's hit path). Death
leaves battle before the life change.

The tick behind +0x680 is the per-second state callback: each call
advances the character's timed-action clock by exactly one second, so
the 20 counts are 20 seconds from the last entry. The entry call
re-arms the count on every call, not only on the change of value, so
each hit restarts the wait. The pet arm is monsterHitSummonedCOS.

===========================================================================
*/

package action

import (
	"sort"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// battleStateMs is the +0x203C count at one tick per second.
const battleStateMs = 20 * 1000

// battleExpiry is the tick index entry; the character store owns the state.
type battleExpiry struct {
	division, name string
	until          int64
}

// battleStateFrame is 0x3122 channel 8 with the new state+0xD value.
func battleStateFrame(gid uint32, inBattle bool) wire.Frame {
	value := uint8(0)
	if inBattle {
		value = 1
	}
	refresh := wire.ObjectStateRefresh{Gid: gid, StateType: wire.StateChannelBattle, Value: value}
	return wire.Frame{Opcode: wire.OpObjectStateRefresh, Payload: refresh.Encode()}
}

// inBattleState reports state+0xD at nowMs.
func inBattleState(c *enterworld.Character, nowMs int64) bool {
	return nowMs < c.BattleUntilMs
}

/*
==================
enterBattleState

Restarts the countdown. The caller holds the character update door and
publishes the returned frames after it: only a player who was out of
battle changes state+0xD, so only then is there a frame.
==================
*/
func (rt *Runtime) enterBattleState(division string, c *enterworld.Character, nowMs int64) []wire.Frame {
	entered := !inBattleState(c, nowMs)
	c.BattleUntilMs = nowMs + battleStateMs
	rt.battleActors.Store(simulation.WorldKey(division, c.Name), battleExpiry{division, c.Name, c.BattleUntilMs})
	if !entered {
		return nil
	}
	return []wire.Frame{battleStateFrame(enterworld.ObjectIDForCharacter(c), true)}
}

/*
==================
leaveBattleState

Clears the state (death). The caller holds the character door and
publishes the returned frame, if any, before the life change.
==================
*/
func (rt *Runtime) leaveBattleState(division string, c *enterworld.Character, nowMs int64) []wire.Frame {
	was := inBattleState(c, nowMs)
	c.BattleUntilMs = 0
	rt.battleActors.Delete(simulation.WorldKey(division, c.Name))
	if !was {
		return nil
	}
	return []wire.Frame{battleStateFrame(enterworld.ObjectIDForCharacter(c), false)}
}

/*
==================
advanceBattleStates

The native state-timer countdown: players whose timer ran out leave battle and every
observer sees channel 8 return to 0. A restarted timer or a cleared state
leaves the stale index entry without effect.
==================
*/
func (rt *Runtime) advanceBattleStates(nowMs int64) []simulation.DivisionFrames {
	var keys []string
	rt.battleActors.Range(func(k, v any) bool {
		if v.(battleExpiry).until <= nowMs {
			keys = append(keys, k.(string))
		}
		return true
	})
	sort.Strings(keys)

	var out []simulation.DivisionFrames
	for _, key := range keys {
		value, ok := rt.battleActors.Load(key)
		if !ok {
			continue
		}
		job := value.(battleExpiry)
		sourceGID, frames := rt.expireBattleStateInDivision(key, job, nowMs)
		if len(frames) > 0 {
			batch := simulation.DivisionFrames{DivisionID: job.division, SourceGID: sourceGID}
			for _, f := range frames {
				batch.Frames = append(batch.Frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload})
			}
			out = append(out, batch)
		}
	}
	return out
}

/*
================
expireBattleStateInDivision
================
*/
func (rt *Runtime) expireBattleStateInDivision(key string, job battleExpiry, nowMs int64) (uint32, []wire.Frame) {
	unlock := rt.lockDivision(job.division)
	defer unlock()
	var sourceGID uint32
	var frames []wire.Frame
	if c := rt.findCharacter(job.division, job.name); c != nil {
		sourceGID = enterworld.ObjectIDForCharacter(c)
		rt.deps.UpdateDeferred(c, "battle-state-expire", func() bool {
			if c.BattleUntilMs != job.until || nowMs < c.BattleUntilMs {
				return false
			}
			c.BattleUntilMs = 0
			frames = append(frames, battleStateFrame(sourceGID, false))
			return true
		})
	}
	rt.battleActors.CompareAndDelete(key, job)
	if len(frames) > 0 && rt.PushCharacterFrames != nil && rt.PushDivisionPeerFrames != nil {
		rt.publishBodyStatus(job.division, job.name, frames)
		frames = nil
	}
	return sourceGID, frames
}
