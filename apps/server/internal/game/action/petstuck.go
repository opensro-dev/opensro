/*
===========================================================================

petstuck.go - a pet whose moves keep stalling warps back to its owner

Native counts every stopped move of an AI actor (a stopped move
raises AI event 8; the AI state counts a stop
within 20 of the move's sequence origin). From the tenth such stop it raises
the stuck event (extended event 0x28), which
the stuck handler answers: an attack or pickup pet
or a fellow COS
is relocated onto its leader (tactics +0x14C)
and enters state 1; any other actor (a horse, a transport) drops
its order and enters state 1 where it stands. BATTLE overrides that handler. Every state change
clears the count, and FOLLOW changes state
whenever the pet reaches its formation, so only a pet
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
// reads. INFERENCE: COS tactics rows carry no warp-to-target trace
// mode, so a pet in BATTLE keeps fighting, as the native override does for selector 0.
const petStuckTraceSelector = 0

/*
================
answerPetStuck

The stuck event, if the follower raised one this step, answered as the
native stuck handler (or BATTLE's override) answers it.
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
		// The native stuck handler's other branch: the tactics release drops the
		// order and the handler enters state 1 (a state change), both
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

Natively: relocate onto the leader and enter state 1. Leaving the
pet's state ends a pending pickup and the formation reservation, as any
state change does. The native relocation refuses outdoor -> dungeon; the pet then only
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

A native state change: entering or leaving BATTLE or PICKITEM
clears the pet's stuck count.
================
*/
func resetPetStuck(state *petSession) {
	if state != nil && state.follower != nil {
		state.follower.ResetStuck()
	}
}
