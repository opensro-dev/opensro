/*
===========================================================================

cosabnormal_movement.go - status consequences on the owned COS mover

Mounted transport uses the rider world; independent pets use PetFollower.
Callbacks run under the division/character transaction and collect packets.

===========================================================================
*/

package action

import (
	"strings"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
cosLiveSpawn

Read the independent follower or transport anchor under its owner. A newly
summoned COS starts at its rider's admitted spawn before follower creation.
================
*/
func (rt *Runtime) cosLiveSpawn(division string, character *enterworld.Character, now int64) simulation.Spawn {
	return rt.companionLiveSpawn(division, character, character.ActiveCOS, now)
}

/*
================
companionLiveSpawn

Each actor has its own movement owner; a sibling mount cannot replace it.
================
*/
func (rt *Runtime) companionLiveSpawn(division string, character *enterworld.Character, pet *enterworld.CharacterCOS, now int64) simulation.Spawn {
	if pet == nil {
		return rt.liveSpawn(simulation.WorldKey(division, character.Name), character, now)
	}
	if pose, ok := rt.companionOwnPose(division, character, pet, now); ok {
		return pose
	}
	return rt.liveSpawn(simulation.WorldKey(division, character.Name), character, now)
}

/*
================
companionOwnPose

The companion's own movement owner, read under petMu. The unlock is
deferred: a panic while reading a pose must not leave petMu held, which
would stall every pet operation on the server.
================
*/
func (rt *Runtime) companionOwnPose(division string, character *enterworld.Character, pet *enterworld.CharacterCOS, now int64) (simulation.Spawn, bool) {
	rt.petMu.Lock()
	defer rt.petMu.Unlock()
	state := rt.petSessions[petOwnerKey{division: division, name: strings.ToLower(character.Name), gid: pet.GID}]
	if state == nil || pet.Mounted {
		return simulation.Spawn{}, false
	}
	if state.follower != nil && state.follower.GID() == pet.GID {
		return state.follower.Position(now), true
	}
	if state.transportCOS != nil && state.transportCOS.GID == pet.GID {
		return state.transportWorld.LiveSpawnAt(now), true
	}
	return simulation.Spawn{}, false
}

/*
================
cosMovementBlocked
================
*/
func (rt *Runtime) cosMovementBlocked(division string, character *enterworld.Character) bool {
	if character == nil {
		return false
	}
	return rt.companionMovementBlocked(division, character, character.ActiveCOS)
}

/*
================
cosMovementBlockedFor

The blocked-mask question by owner name and COS GID. The lookup only
ever weighed those two values, so callers that already hold them as
facts answer without a record copy (#1943).
================
*/
func (rt *Runtime) cosMovementBlockedFor(division, name string, petGID uint32) bool {
	block := rt.cosAbnormal(division, name, petGID)
	return block != nil && block.Mask&(abnormal.Freeze.Bit()|abnormal.Sleep.Bit()|abnormal.Root.Bit()|abnormal.Stun.Bit()) != 0
}

/*
================
companionMovementBlocked
================
*/
func (rt *Runtime) companionMovementBlocked(division string, character *enterworld.Character, pet *enterworld.CharacterCOS) bool {
	if character == nil || pet == nil {
		return false
	}
	return rt.cosMovementBlockedFor(division, character.Name, pet.GID)
}

/*
================
stopCosMovement
================
*/
func (rt *Runtime) stopCosMovement(division string, character *enterworld.Character, now int64) []wire.Frame {
	if character == nil {
		return nil
	}
	return rt.stopCompanionMovement(division, character, character.ActiveCOS, now)
}

/*
================
stopCompanionMovement
================
*/
func (rt *Runtime) stopCompanionMovement(division string, character *enterworld.Character, pet *enterworld.CharacterCOS, now int64) []wire.Frame {
	if character == nil || pet == nil {
		return nil
	}
	if pet.Mounted && rt.Worlds != nil {
		moving := false
		world := rt.Worlds.Update(simulation.WorldKey(division, character.Name),
			func() simulation.WorldState { return simulation.SeedWorldState(character) },
			func(state *simulation.WorldState) {
				moving = state.MoveSegment.Valid()
				if moving {
					state.SettleLive(now)
				}
			})
		if !moving {
			return nil
		}
		writeBackWorld(character, world)
		pose := world.Spawn
		return []wire.Frame{{Opcode: wire.OpObjectSourceCorrection,
			Payload: wire.ObjectSourceCorrection{Gid: pet.GID, Position: wire.Position{
				RegionID: pose.RegionID, X: float32(pose.X), Y: float32(pose.Y), Z: float32(pose.Z), Heading: pose.Angle,
			}}.Encode()}}
	}
	rt.petMu.Lock()
	defer rt.petMu.Unlock()
	state := rt.petSessions[petOwnerKey{division: division, name: strings.ToLower(character.Name), gid: pet.GID}]
	if state == nil || state.follower == nil || state.follower.GID() != pet.GID {
		return nil
	}
	var frames []wire.Frame
	for _, frame := range state.follower.Stop(now) {
		frames = append(frames, wire.Frame{Opcode: frame.Opcode, Payload: frame.Payload})
	}
	return frames
}

/*
================
refreshCosAbnormalSpeed

Keep an in-flight segment continuous when its duration changes. The same
speed pair goes to existing observers and the late-join presentation owner.
================
*/
func (rt *Runtime) refreshCosAbnormalSpeed(owner *cosAbnormalOwner) []wire.Frame {
	if owner.ref == nil || owner.pet == nil {
		return nil
	}
	pet := owner.pet
	walk, run := owner.Param(movementWalkParameter), rt.cosPacedRun(owner.ref, owner.Param(movementRunParameter))
	if pet.Mounted && rt.Worlds != nil {
		rt.Worlds.Update(simulation.WorldKey(owner.division, owner.c.Name),
			func() simulation.WorldState { return simulation.SeedWorldState(owner.c) },
			func(world *simulation.WorldState) { world.UpdateMovementSpeeds(walk, run, owner.now) })
	}
	rt.setPetFollowerMovementSpeeds(owner.division, owner.c.Name, pet.GID, walk, run, owner.now)
	return []wire.Frame{{Opcode: movementSpeedOpcode,
		Payload: wire.NewWriter(movementSpeedBytes).U32(pet.GID).F32(walk).F32(run).Payload()}}
}

/*
================
setPetFollowerMovementSpeeds
================
*/
func (rt *Runtime) setPetFollowerMovementSpeeds(division, ownerName string, petGID uint32, walk, run float32, now int64) {
	rt.petMu.Lock()
	defer rt.petMu.Unlock()
	state := rt.petSessions[petOwnerKey{division: division, name: strings.ToLower(ownerName), gid: petGID}]
	if state != nil && state.follower != nil && state.follower.GID() == petGID {
		state.follower.SetMovementSpeeds(walk, run, now)
	}
}
