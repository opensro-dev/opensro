/*
===========================================================================

murdererdrain.go - the murderer status HP drain

A murderer's natural recovery pulse takes HP instead of giving it. Outside
a safe region, a living character with a positive PK penalty (murderer(c))
loses pk.DrainAmount of its maximum HP on the pulse and does not regenerate;
inside one, the pulse recovers as usual. The rule therefore has no clock of
its own: recoverResident owns the period and the residency, so an offline
record cannot be drained.

Motion, posture and an open cast gate only the recovery half of a pulse, so
none of them stops a drain.

INFERENCE: the safe-region test is the _RefRegion no-battlefield reading the
town rules use (rt.inTown). The v1.150 vitals causes carry no bit for the
drain (simulation/gameready.go), so a surviving pulse is a plain vitals
refresh.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
murdererDrainApplies

Whether this pulse is a drain. Decided inside the character's door, after
the caller has refused a corpse.
================
*/
func (rt *Runtime) murdererDrainApplies(division string, c *enterworld.Character) bool {
	return murderer(c) && !rt.inTown(division, c)
}

/*
================
drainPulse

What one committed drain pulse publishes. fatal asks the caller to rebind
the resident's region once the door has closed.
================
*/
type drainPulse struct {
	actor, public []wire.Frame
	fatal         bool
}

/*
================
drainMurdererInDoor

Applies one drain pulse inside the character's door and reports whether the
record changed. A maximum HP too small to lose a whole point changes nothing,
but the pulse still replaced recovery.

A surviving pulse publishes a plain vitals refresh (source zero) to the
victim and an HP refresh to observers, the pair every HP-over-time change
sends. A lethal pulse stays silent here: the shared fatal transition publishes
the one 0-HP baseline and the dead state for everyone.
================
*/
func (rt *Runtime) drainMurdererInDoor(division string, c *enterworld.Character, nowMs int64, pulse *drainPulse) bool {
	stats, _, err := rt.playerCombatStats(division, c)
	if err != nil {
		return false
	}
	maxHP, _, currentHP, currentMP := keeperVitalsFrom(c, stats, nil)
	nextHP := max(currentHP-pk.DrainAmount(maxHP), 0)
	if nextHP == currentHP {
		return false
	}
	c.CurrentHP = &nextHP
	gid := enterworld.ObjectIDForCharacter(c)
	if nextHP != 0 {
		vitals := simulation.Vitals{CurrentHP: uint32(nextHP), CurrentMP: uint32(currentMP)}
		pulse.actor = append(pulse.actor, wire.Frame{Opcode: simulation.OpVitalsUpdate,
			Payload: simulation.VitalsRefreshPayload(gid, vitals)})
		pulse.public = append(pulse.public, wire.Frame{Opcode: simulation.OpVitalsUpdate,
			Payload: simulation.HPRefreshPayload(gid, 0, uint32(nextHP))})
		return true
	}
	pulse.fatal = true
	effects, progression := rt.settlePlayerDeathInDoor(division, c, deathKiller{murdererDrain: true}, nowMs)
	pulse.public = append(pulse.public, effects...)
	pulse.actor = append(pulse.actor, progression...)
	if owner := rt.clearPlayerAbnormalInDoor(division, c, nowMs); owner != nil {
		owner.fatal = true
		pulse.public = append(pulse.public, rt.playerAbnormalPublication(division, c, owner).public...)
		return true
	}
	life := beginFatalLifePublication(gid)
	baseline := life.publishDeathBaseline()
	dead := life.publishDead()
	pulse.public = append(pulse.public, wire.Frame{Opcode: baseline.opcode, Payload: baseline.payload},
		wire.Frame{Opcode: dead.opcode, Payload: dead.payload})
	return true
}

/*
================
publishDrainPulse

Routes a committed pulse after the door has closed.
================
*/
func (rt *Runtime) publishDrainPulse(key recoveryKey, c *enterworld.Character, nowMs int64, pulse *drainPulse) []simulation.DivisionFrames {
	if pulse.fatal {
		rt.bindResidentRegion(simulation.WorldKey(key.division, c.Name), nowMs)
	}
	var out []simulation.DivisionFrames
	if len(pulse.public) != 0 {
		out = append(out, simulation.DivisionFrames{DivisionID: key.division,
			SourceGID: enterworld.ObjectIDForCharacter(c), Frames: simFrames(pulse.public)})
	}
	if len(pulse.actor) != 0 {
		out = append(out, simulation.DivisionFrames{DivisionID: key.division,
			OnlyCharacterID: c.ID, Frames: simFrames(pulse.actor)})
	}
	return out
}
