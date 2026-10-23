/*
===========================================================================

battlestate.go - the player battle state

The native battle state is state channel 8; on entry it arms a 20-count
timer, and a change of value publishes the later protocol's 0x30BF state
broadcast, 0x3122 channel 8 on the v1.150 wire (client 777E84 ->
localPlayer +0x781). The native state tick counts the timer down and leaves
battle at zero. A player enters battle by striking, by being struck and by
a hit on their pet. Death leaves battle before the life change.

The native tick is a scheduled callback whose period was not recovered; this
port counts the 20 ticks as seconds. The pet arm is monsterHitSummonedCOS.

===========================================================================
*/

package action

import (
	"sort"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// battleStateMs is the native 20-count at one tick per second.
const battleStateMs = 20 * 1000

// battleExpiry is the tick index entry; the character store owns the state.
type battleExpiry struct {
	division, name string
	until          int64
}

// battleStateFrame is 0x3122 channel 8 with the new battle state.
func battleStateFrame(gid uint32, inBattle bool) wire.Frame {
	value := uint8(0)
	if inBattle {
		value = 1
	}
	refresh := wire.ObjectStateRefresh{Gid: gid, StateType: wire.StateChannelBattle, Value: value}
	return wire.Frame{Opcode: wire.OpObjectStateRefresh, Payload: refresh.Encode()}
}

// inBattleState reports the battle state at nowMs.
func inBattleState(c *enterworld.Character, nowMs int64) bool {
	return nowMs < c.BattleUntilMs
}

/*
==================
enterBattleState

Restarts the countdown. The caller holds the character update door and
publishes the returned frames after it: only a player who was out of
battle changes the state, so only then is there a frame.
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

Clears the state (death, as natively). The caller holds the character door and
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

The native countdown: players whose timer ran out leave battle and every
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
