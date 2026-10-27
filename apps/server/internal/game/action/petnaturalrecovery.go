/*
===========================================================================

petnaturalrecovery.go - the summoned companions' native natural recovery

Native runs one natural-recovery tick on every living character object:
the object reset registers it on a 4.0-second period, and the v1.150
client carries the same tick on the same timer. Each pulse heals 1.6% of
the maximum health and mana, truncated after the native float32
multiply, unless the object is dead, Zombie, or in motion other than
standing and sitting. Players got their richer port of the same rule in
naturalrecovery.go; companions take the plain tick.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// petNaturalRecoveryPeriodMs is the native pulse period: the reset
	// registers the natural-recovery timer with a 4.0-second interval.
	petNaturalRecoveryPeriodMs int64 = 4_000
	// petNaturalRecoveryRate is the native pulse share of each maximum
	// (the native 0.016f constant).
	petNaturalRecoveryRate float32 = 0.016
)

/*
================
petNaturalRecoveryAmount

One pulse's amount: the maximum times 1.6% in float32, truncated toward
zero (6858 -> 109, 1000 -> 16, 100 -> 1).
================
*/
func petNaturalRecoveryAmount(maximum int64) int64 {
	if maximum <= 0 {
		return 0
	}
	return int64(float32(maximum) * petNaturalRecoveryRate)
}

/*
================
petNaturalRecoveryMotion

The companion's native motion byte from the plane that moves it: the
follower's world, or the transport world when it has no follower. Pets
never sit in this port, so the sitting byte (4) simply joins standing
(0); walk and run refuse the pulse, as the native tick refuses every
motion other than 0 and 4.
================
*/
func petNaturalRecoveryMotion(petState *petSession, nowMs int64) uint8 {
	if petState == nil {
		return simulation.MotionNone
	}
	if petState.follower != nil {
		world, _ := petState.follower.Presentation()
		return world.MotionStateAt(nowMs)
	}
	return petState.transportWorld.MotionStateAt(nowMs)
}

/*
================
advancePetNaturalRecovery

One companion's natural recovery. The session clock sounds every four
seconds from its bind, and a pulse the gates refuse is spent, never
queued: the native timer fires either way. A pulse heals 1.6% of the
maximum health and of the maximum mana, reduced by the standard
recovery-parameter offset and clamped at the maximum
(combat.RecoverVital, the same door the pet potions use). The owner
gets the pet's refreshed vitals; viewers get the health refresh.
================
*/
func (rt *Runtime) advancePetNaturalRecovery(key petOwnerKey, nowMs int64) []simulation.Frame {
	rt.petMu.Lock()
	petState := rt.petSessions[key]
	rt.petMu.Unlock()
	if petState == nil || petState.character == nil ||
		petState.naturalRecoveryAtMs == 0 || nowMs < petState.naturalRecoveryAtMs {
		return nil
	}
	c := petState.character
	var ownerFrames []wire.Frame
	changed := false
	committed := rt.deps.UpdateDeferred(c, "cos-natural-recovery", func() bool {
		petState.naturalRecoveryAtMs = nowMs + petNaturalRecoveryPeriodMs
		pet := c.CompanionByGID(key.gid)
		if pet == nil || !pet.Summoned || pet.BroodStage != 0 || pet.CurrentHP == 0 {
			return false
		}
		if motion := petNaturalRecoveryMotion(petState, nowMs); motion != simulation.MotionNone &&
			motion != simulation.MotionSitting {
			return false
		}
		owner := rt.newCosAbnormalOwnerForPet(key.division, c, pet, nowMs)
		if owner.block.Mask&abnormal.Zombie.Bit() != 0 {
			return false
		}
		beforeHP, beforeMP := int64(pet.CurrentHP), int64(pet.CurrentMP)
		pet.CurrentHP = uint32(combat.RecoverVital(beforeHP, int64(owner.MaxHP()),
			petNaturalRecoveryAmount(int64(owner.MaxHP())), owner.Param(combat.HPRecoveryReductionParameter)))
		pet.CurrentMP = uint32(combat.RecoverVital(beforeMP, int64(owner.MaxMP()),
			petNaturalRecoveryAmount(int64(owner.MaxMP())), owner.Param(combat.MPRecoveryReductionParameter)))
		if int64(pet.CurrentHP) == beforeHP && int64(pet.CurrentMP) == beforeMP {
			return false
		}
		ownerFrames = append(ownerFrames, wire.Frame{
			Opcode: simulation.OpVitalsUpdate,
			Payload: simulation.VitalsRefreshPayload(key.gid, simulation.Vitals{
				CurrentHP: pet.CurrentHP, CurrentMP: pet.CurrentMP,
			}),
		})
		if int64(pet.CurrentHP) != beforeHP {
			petState.public = append(petState.public, wire.Frame{
				Opcode:  simulation.OpVitalsUpdate,
				Payload: simulation.HPRefreshPayload(key.gid, 0, pet.CurrentHP),
			})
		}
		changed = true
		return true
	})
	if !committed || !changed {
		return nil
	}
	return simFrames(ownerFrames)
}
