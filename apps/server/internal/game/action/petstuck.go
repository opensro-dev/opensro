/*
===========================================================================

petstuck.go - a pet whose moves keep stalling warps back to its owner

Native counts every stopped move of an AI actor (CGObjChar_StopMove 4A9430
raises AI event 8; CAIState_OnMoveStopped_CountStuck 559010 counts a stop
within 20 of the move's sequence origin). From the tenth such stop it raises
the stuck event (CAIState_DispatchExtendedEvent 5597B0, event 0x28), which
CAIState_OnStuck_RelocateToOwner (5599C0) answers: an attack or pickup pet
(CGObj_IsAttackOrPickupCOS 4838C0 -> 44B460) or a fellow COS
(CGObj_IsFellowCOS 483980) is relocated onto its leader (tactics +0x14C,
541110) and enters state 1; any other actor (a horse, a transport) drops
its order (53FD40) and enters state 1 where it stands. BATTLE overrides that handler (55A250). Every state change
clears the count (AI_CTactics_ChangeState 53FF30), and FOLLOW changes state
whenever the pet reaches its formation (55A960 -> 5599A0), so only a pet
that never gets there - walled in - accumulates the ten stops.

The follower counts (simulation.PetFollower); this file answers the event.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// petStuckTraceSelector is the RefTactics +79 byte BATTLE's override
// (55A250) reads. INFERENCE: COS tactics rows carry no warp-to-target trace
// mode, so a pet in BATTLE keeps fighting, as 55A250 does for selector 0.
const petStuckTraceSelector = 0

/*
================
answerPetStuck

The stuck event, if the follower raised one this step, answered as 5599C0
(or BATTLE's 55A250) answers it.
================
*/
func (rt *Runtime) answerPetStuck(key petOwnerKey, state *petSession, snapshot *enterworld.Character, pet *enterworld.CharacterCOS, tid uint16, nowMs int64) []simulation.Frame {
	if state.follower == nil || !state.follower.TakeStuck() {
		return nil
	}
	switch monster.NativeStuckResponse(state.combat != nil, petStuckTraceSelector, tid) {
	case monster.StuckWarpToControllerAndIdle:
		return rt.relocateStuckPet(key, state, snapshot, pet, nowMs)
	case monster.StuckDetachAndIdle:
		// 5599C0's other branch: CAITactics_ReleaseCommand (53FD40) drops the
		// order and the handler enters state 1 (ChangeState 53FF30), both
		// clearing the count; the next step issues a fresh order.
		frames := state.follower.Stop(nowMs)
		state.follower.ResetStuck()
		return frames
	}
	return nil
}

/*
================
relocateStuckPet

5599C0: relocate onto the leader (541110) and enter state 1. Leaving the
pet's state ends a pending pickup and the formation reservation, as any
state change does. 541110 refuses outdoor -> dungeon; the pet then only
drops its order.
================
*/
func (rt *Runtime) relocateStuckPet(key petOwnerKey, state *petSession, snapshot *enterworld.Character, pet *enterworld.CharacterCOS, nowMs int64) []simulation.Frame {
	world := rt.Worlds.Snapshot(simulation.WorldKey(key.division, snapshot.Name), func() simulation.WorldState {
		return simulation.SeedWorldState(snapshot)
	})
	from, owner := state.follower.Position(nowMs), world.LiveSpawnAt(nowMs)
	frames := state.follower.Stop(nowMs)
	resetPetStuck(state)
	if state.pickup != nil {
		result := finishPendingCosPickup(state, failureResult(wire.ErrCodeInvalidRequest))
		frames = append(frames, simFrames(result.Frames)...)
	}
	rt.releasePetFormation(key, state)
	if rt.CompanionSurfaceHeight == nil || from.RegionID&0x8000 == 0 && owner.RegionID&0x8000 != 0 ||
		!state.follower.RelocateTo(owner, nowMs, rt.CompanionSurfaceHeight) {
		return frames
	}
	return append(frames, rt.publishPetRelocation(key, state, pet, nowMs)...)
}

/*
================
publishPetRelocation

A relocated pet is a new presentation: observers get its despawn and its
spawn at the new place (the owner-follow warp and the stuck warp alike).
================
*/
func (rt *Runtime) publishPetRelocation(key petOwnerKey, state *petSession, pet *enterworld.CharacterCOS, nowMs int64) []simulation.Frame {
	state.generation++
	state.relocatedAtMs = nowMs
	state.summonedAtMs = 0
	view := rt.companionPresentation(key.division, state, pet)
	if view == nil {
		return nil
	}
	frames := []simulation.Frame{{Opcode: wire.OpObjectDespawn, Payload: wire.ObjectDespawn{Gid: pet.GID}.Encode()}}
	return append(frames, view.SpawnFrames(nowMs)...)
}

/*
================
resetPetStuck

AI_CTactics_ChangeState (53FF30): entering or leaving BATTLE or PICKITEM
clears the pet's stuck count.
================
*/
func resetPetStuck(state *petSession) {
	if state != nil && state.follower != nil {
		state.follower.ResetStuck()
	}
}
