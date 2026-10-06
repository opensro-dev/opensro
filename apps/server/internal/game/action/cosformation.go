/*
===========================================================================

cosformation.go - native owner FOLLOW state on the existing COS mover

Owner sessions own CPositioner slots. Child sessions own the AI cadence and
remembered slot; the canonical companion owns its movement parameter source.

===========================================================================
*/
package action

import (
	"math"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const cosFollowIntervalMs = 100

/*
================
releasePetFormation

Leaving FOLLOW, dying, dismissal and session retirement share the reservation
owner. The remembered index survives a release, as at 5554B0.
================
*/
func (rt *Runtime) releasePetFormation(key petOwnerKey, state *petSession) {
	rt.petMu.Lock()
	owner := rt.petSessions[petOwnerKey{division: key.division, name: key.name}]
	if owner != nil {
		owner.formationSlots.Release(key.gid)
	}
	rt.petMu.Unlock()
	state.formationActive = false
	if state.combat == nil {
		state.formationBattle = false
	}
}

/*
================
advanceOwnerFormation

55A930 arms timer 0 on FOLLOW entry. 55A960 gates the callback at 100 ms;
549F80 checks timer 3 separately. Keep the existing native timer arithmetic.
================
*/
func (rt *Runtime) advanceOwnerFormation(step petCombatStep) []simulation.Frame {
	state := step.state
	state.formationBattle = false
	if !state.formationActive {
		if rt.CombatRoll == nil {
			return nil
		}
		sample, err := rt.CombatRoll()
		if err != nil {
			return nil
		}
		if state.formationTimers == nil {
			state.formationTimers = monster.NewAITimeManager()
		}
		state.formationActive = true
		state.formationTimers.SetTimer(0, cosFollowIntervalMs, 0, uint32(step.nowMs), false, func() uint32 { return sample })
	}
	if !state.formationTimers.CheckTimer(0, uint32(step.nowMs)) || !state.formationTimers.CheckTimer(3, uint32(step.nowMs)) {
		return nil
	}
	rt.petMu.Lock()
	owner := rt.petSessions[petOwnerKey{division: step.key.division, name: step.key.name}]
	rt.petMu.Unlock()
	if owner == nil {
		return nil
	}
	world := rt.Worlds.Snapshot(simulation.WorldKey(step.key.division, step.snapshot.Name), func() simulation.WorldState {
		return simulation.SeedWorldState(step.snapshot)
	})
	from, target := state.follower.Position(step.nowMs), world.LiveSpawnAt(step.nowMs)
	distance := monster.NativeOwnerFollowDistance(monster.Pose{RegionID: from.RegionID, X: from.X, Z: from.Z}, monster.Pose{RegionID: target.RegionID, X: target.X, Z: target.Z})
	var frames []simulation.Frame
	if distance > monster.OwnerFollowDistance && monster.FollowLocationCompatible(from.RegionID, target.RegionID) {
		walk, run := world.MovementSpeeds()
		current := walk
		if world.MovementMode == simulation.RunMode {
			current = run
		}
		if step.ref.RunSpeed > 0 {
			factor := monster.NativeOwnerFollowRunFactor(monster.OwnerFollowSpeed{Distance: distance, OwnerCurrent: current, OwnerRun: run, AuthoredRun: step.ref.RunSpeed, OwnerRunning: world.MovementMode == simulation.RunMode})
			frames = rt.setCompanionFollowSpeed(step, factor)
			step.run = cosParameter(step.ref, step.pet, rt.cosAbnormal(step.key.division, step.snapshot.Name, step.pet.GID), movementRunParameter)
		}
	}
	relocated := false
	frames = append(frames, state.follower.FollowFormation(simulation.PetFormationStep{
		Relocated: &relocated, Owner: world, Slots: &owner.formationSlots, Slot: &state.formationSlot,
		BodyRadius: float32(uint16(step.ref.Parameters.BodyRadius)), Speed: step.run, Now: step.nowMs,
		Surface: rt.CompanionSurfaceHeight, Constrain: step.constraint,
	})...)
	if relocated {
		state.generation++
		state.relocatedAtMs = step.nowMs
		state.summonedAtMs = 0
		if view := rt.companionPresentation(step.key.division, state, step.pet); view != nil {
			frames = append(frames, simulation.Frame{Opcode: wire.OpObjectDespawn, Payload: wire.ObjectDespawn{Gid: step.pet.GID}.Encode()})
			frames = append(frames, view.SpawnFrames(step.nowMs)...)
		}
	}
	return frames
}

/*
================
setCompanionFollowSpeed

548A30 compares projected authored speed before installing source zero. The
existing parameter owner retains status factors and feeds late peer spawns.
================
*/
func (rt *Runtime) setCompanionFollowSpeed(step petCombatStep, factor float32) []simulation.Frame {
	if int64(factor) == 100000 || math.Abs(float64(float32(step.run-float32(float64(factor)*float64(step.ref.RunSpeed)/100)))) < float64(float32(0.000001)) {
		return nil
	}
	var frames []simulation.Frame
	rt.deps.Mutate(step.state.character, "mercenary-follow-speed", func() {
		pet := step.state.character.CompanionByGID(step.pet.GID)
		if pet == nil || !pet.Summoned {
			return
		}
		pet.FollowRunFactor, pet.FollowRunSet = factor, true
		step.pet.FollowRunFactor, step.pet.FollowRunSet = factor, true
		block := rt.cosAbnormal(step.key.division, step.snapshot.Name, pet.GID)
		walk, run := cosParameter(step.ref, pet, block, movementWalkParameter), cosParameter(step.ref, pet, block, movementRunParameter)
		step.state.follower.SetMovementSpeeds(walk, run, step.nowMs)
		frames = []simulation.Frame{{Opcode: movementSpeedOpcode, Payload: wire.NewWriter(movementSpeedBytes).U32(pet.GID).F32(walk).F32(run).Payload()}}
		step.state.public = append(step.state.public, wire.Frame{Opcode: frames[0].Opcode, Payload: frames[0].Payload})
	})
	return frames
}

/*
================
advanceCompanionBattleFollowTimer

559FB0 arms the movement callback timer on BATTLE entry. Retain that slot
across FOLLOW re-entry instead of replacing the native timer bank.
================
*/
func (rt *Runtime) advanceCompanionBattleFollowTimer(step petCombatStep) {
	state := step.state
	if state.formationTimers == nil {
		state.formationTimers = monster.NewAITimeManager()
	}
	if !state.formationBattle {
		if rt.CombatRoll == nil {
			return
		}
		sample, err := rt.CombatRoll()
		if err != nil {
			return
		}
		state.formationTimers.SetTimer(3, cosFollowIntervalMs, 0, uint32(step.nowMs), false, func() uint32 { return sample })
		state.formationBattle = true
	}
	state.formationTimers.CheckTimer(3, uint32(step.nowMs))
}
