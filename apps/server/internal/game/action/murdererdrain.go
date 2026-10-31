/*
===========================================================================

murdererdrain.go - the murderer status HP drain

A living murderer (a positive PK penalty: murderer(c)) loses
pk.MurdererDrainPercent of its maximum HP every pk.MurdererDrainPeriodMs.
The PK record's sweep (pkrecord.go) drives it: the same tick that runs the
penalty keeper asks whether the drain is due, so the rule adds no goroutine
and no package state.

INFERENCE: the original server binary is not on this machine; the owner
played the original and gives 5% per 10 s as the estimate, so the period is
an owner estimate rather than a read constant (pk/drain.go).

The drain can kill. A lethal pulse settles through the shared fatal
transition (settlePlayerDeathInDoor) with no killer object and a
pk.DeathPlayer classification, so the existing murderer relief (-200 penalty,
-1 daily) and drop roll run unchanged.

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
murdererDrainDue

The per-player drain clock. Its first sight of a murderer starts a full
period; the caller consumes a due period even when the pulse is skipped, so
a corpse waits a full period after its rebirth instead of draining at once.
================
*/
func (rt *Runtime) murdererDrainDue(key petOwnerKey, nowMs int64) bool {
	if rt.murdererDrains == nil {
		rt.murdererDrains = make(map[petOwnerKey]int64)
	}
	next, tracked := rt.murdererDrains[key]
	if !tracked {
		rt.murdererDrains[key] = nowMs + pk.MurdererDrainPeriodMs
		return false
	}
	return nowMs >= next
}

/*
================
playerOnline

The drain is HP pressure on a body in the world, so it must not run for an
offline record the PK keeper still decays (an offline murderer would be
bled to death and drop without ever playing). A recovery session exists
exactly while the player is a world resident (naturalrecovery.go).
================
*/
func (rt *Runtime) playerOnline(key petOwnerKey) bool {
	return rt.residentOnline(key.division, key.name)
}

/*
================
forgetMurdererDrain

Only the tick's sweep calls this; a record that stops being a murderer,
dies, or leaves the world starts a fresh period when murderer status and
life return.
================
*/
func (rt *Runtime) forgetMurdererDrain(key petOwnerKey) {
	delete(rt.murdererDrains, key)
}

/*
================
drainMurderer

One due pulse for one murderer. The due-ness check and the deadline move
happen before the division lock: the deadline map is touched only by the
serial tick sweep, so a not-due owner costs no lock. The division door then
keeps the HP debit, the possible death and the frames that describe them
one transaction. A late tick drops the pulses it missed rather than
replaying them, so a stalled server cannot kill in a burst.

A surviving pulse publishes a plain vitals refresh (source zero) to the
victim and an HP refresh to observers - the pair every HP-over-time change
sends, with no cause bit because v1.150 authors none for this rule
(simulation/gameready.go). A lethal pulse stays silent here: the shared
fatal transition publishes the one 0-HP baseline and the dead state for
everyone (beginFatalLifePublication, as other killer-less deaths use it).
================
*/
func (rt *Runtime) drainMurderer(key petOwnerKey, nowMs int64) []simulation.DivisionFrames {
	if !rt.murdererDrainDue(key, nowMs) {
		return nil
	}
	rt.murdererDrains[key] = nowMs + pk.MurdererDrainPeriodMs
	unlock := rt.lockDivision(key.division)
	defer unlock()
	c := rt.findCharacter(key.division, key.name)
	if c == nil {
		return nil
	}
	var actor, public []wire.Frame
	fatal := false
	committed := rt.deps.UpdateDeferred(c, "murderer-drain", func() bool {
		// The sweep forgets a corpse's clock, so a revival arms a fresh
		// period; the check still guards a death between the read and here.
		if c.DeletePending || !enterworld.CharacterAlive(c) || !murderer(c) {
			return false
		}
		stats, _, err := rt.playerCombatStats(key.division, c)
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
		if nextHP == 0 {
			fatal = true
			effects, progression := rt.settlePlayerDeathInDoor(key.division, c, deathKiller{murdererDrain: true}, nowMs)
			public = append(public, effects...)
			actor = append(actor, progression...)
			if owner := rt.clearPlayerAbnormalInDoor(key.division, c, nowMs); owner != nil {
				owner.fatal = true
				public = append(public, rt.playerAbnormalPublication(key.division, c, owner).public...)
			} else {
				life := beginFatalLifePublication(gid)
				baseline := life.publishDeathBaseline()
				dead := life.publishDead()
				public = append(public, wire.Frame{Opcode: baseline.opcode, Payload: baseline.payload},
					wire.Frame{Opcode: dead.opcode, Payload: dead.payload})
			}
			return true
		}
		actor = append(actor, wire.Frame{Opcode: simulation.OpVitalsUpdate,
			Payload: simulation.VitalsRefreshPayload(gid, simulation.Vitals{CurrentHP: uint32(nextHP), CurrentMP: uint32(currentMP)})})
		public = append(public, wire.Frame{Opcode: simulation.OpVitalsUpdate,
			Payload: simulation.HPRefreshPayload(gid, 0, uint32(nextHP))})
		return true
	})
	if !committed {
		return nil
	}
	if fatal {
		rt.bindResidentRegion(simulation.WorldKey(key.division, c.Name), nowMs)
	}
	var out []simulation.DivisionFrames
	if len(public) != 0 {
		out = append(out, simulation.DivisionFrames{DivisionID: key.division, SourceGID: enterworld.ObjectIDForCharacter(c), Frames: simFrames(public)})
	}
	if len(actor) != 0 {
		out = append(out, simulation.DivisionFrames{DivisionID: key.division, OnlyCharacterID: c.ID, Frames: simFrames(actor)})
	}
	return out
}
