/*
===========================================================================

playeraggression.go - transient player hostility and its scheduled countdown

Character.Aggressions owns the native target/count map. The runtime keeps only
the next scheduled tick, so reconnects and snapshots cannot recreate hostility
from presentation state. Combat callers hold the division and character doors.

===========================================================================
*/
package action

import (
	"sort"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	playerAggressionTicks = 20
	// The native state tick also advances the timed actions by 1.0 s per callback.
	playerAggressionTickMs = 1000
)

/*
================
playerAggressionClock
================
*/
type playerAggressionClock struct {
	division, name string
	next           int64
}

/*
================
playerPVPStateFrame
================
*/
func playerPVPStateFrame(c *enterworld.Character) wire.Frame {
	state := wire.ObjectStateRefresh{Gid: enterworld.ObjectIDForCharacter(c), StateType: wire.StateChannelPvp, Value: c.PVPState()}
	return wire.Frame{Opcode: wire.OpObjectStateRefresh, Payload: state.Encode()}
}

/*
================
refreshPlayerAggression

Natively both registrations refresh the same target/count entry. A refresh does not move
the actor's periodic tick; other opponents retain their own remaining counts.
================
*/
func (rt *Runtime) refreshPlayerAggression(division string, c, target *enterworld.Character, now int64) []wire.Frame {
	before := c.PVPState()
	if c.Aggressions == nil {
		c.Aggressions = make(map[uint32]uint32)
	}
	c.Aggressions[enterworld.ObjectIDForCharacter(target)] = playerAggressionTicks
	rt.aggressionActors.LoadOrStore(simulation.WorldKey(division, c.Name), playerAggressionClock{division, c.Name, now + playerAggressionTickMs})
	if before != c.PVPState() {
		return []wire.Frame{playerPVPStateFrame(c)}
	}
	return nil
}

/*
================
registerPlayerAttack

Natively legal opponents do not make the attacker criminal. An attack on a
neutral player, or on an existing aggressor, retains the reciprocal target
for twenty state ticks. Same-party membership removes that target instead.
================
*/
func (rt *Runtime) registerPlayerAttack(division string, caster, target *enterworld.Character, now int64) []wire.Frame {
	if caster == nil || target == nil || caster.ID == target.ID {
		return nil
	}
	if rt.sharePartyObject(division, caster, target) {
		before := caster.PVPState()
		delete(caster.Aggressions, enterworld.ObjectIDForCharacter(target))
		if before != caster.PVPState() {
			return []wire.Frame{playerPVPStateFrame(caster)}
		}
		return nil
	}
	if rt.hostilePlayerEquipment(caster, target) || rt.guildsAtWar(division, caster, target) || target.PVPState() == 2 {
		return nil
	}
	return rt.refreshPlayerAggression(division, caster, target, now)
}

/*
================
registerPlayerAttacked

Natively the attacking player is retained only when the recipient is already grey.
This runs before the attacker's registration, matching the native hit.
================
*/
func (rt *Runtime) registerPlayerAttacked(division string, target, caster *enterworld.Character, now int64) []wire.Frame {
	if target.PVPState() != 1 {
		return nil
	}
	return rt.refreshPlayerAggression(division, target, caster, now)
}

/*
================
advancePlayerAggressions

The native state timers remove expired entries and opponents that joined the same party.
Catch up elapsed ticks once, without changing the phase after a delayed frame.
================
*/
func (rt *Runtime) advancePlayerAggressions(now int64) []simulation.DivisionFrames {
	var keys []string
	rt.aggressionActors.Range(func(key, value any) bool {
		if value.(playerAggressionClock).next <= now {
			keys = append(keys, key.(string))
		}
		return true
	})
	sort.Strings(keys)
	var out []simulation.DivisionFrames
	for _, key := range keys {
		value, ok := rt.aggressionActors.Load(key)
		if !ok {
			continue
		}
		job := value.(playerAggressionClock)
		sourceGID, frames := rt.advancePlayerAggressionInDivision(key, job, now)
		if len(frames) > 0 {
			batch := simulation.DivisionFrames{DivisionID: job.division, SourceGID: sourceGID}
			for _, frame := range frames {
				batch.Frames = append(batch.Frames, simulation.Frame{Opcode: frame.Opcode, Payload: frame.Payload})
			}
			out = append(out, batch)
		}
	}
	return out
}

/*
================
advancePlayerAggressionInDivision
================
*/
func (rt *Runtime) advancePlayerAggressionInDivision(key string, job playerAggressionClock, now int64) (uint32, []wire.Frame) {
	unlock := rt.lockDivision(job.division)
	defer unlock()
	var sourceGID uint32
	var frames []wire.Frame
	c := rt.findCharacter(job.division, job.name)
	if c != nil {
		sourceGID = enterworld.ObjectIDForCharacter(c)
		party := rt.auraParty(job.division, c)
		rt.deps.UpdateDeferred(c, "player-aggression-tick", func() bool {
			before := c.PVPState()
			ticks := uint64((now-job.next)/playerAggressionTickMs + 1)
			for gid, remaining := range c.Aggressions {
				if uint64(remaining) <= ticks || party[gid] {
					delete(c.Aggressions, gid)
				} else {
					c.Aggressions[gid] = remaining - uint32(ticks)
				}
			}
			if before != c.PVPState() {
				frames = append(frames, playerPVPStateFrame(c))
			}
			return true
		})
	}
	if c == nil || len(c.Aggressions) == 0 {
		rt.aggressionActors.Delete(key)
	} else {
		job.next += ((now-job.next)/playerAggressionTickMs + 1) * playerAggressionTickMs
		rt.aggressionActors.Store(key, job)
	}
	if len(frames) > 0 && rt.PushCharacterFrames != nil && rt.PushDivisionPeerFrames != nil {
		rt.publishBodyStatus(job.division, job.name, frames)
		frames = nil
	}
	return sourceGID, frames
}
