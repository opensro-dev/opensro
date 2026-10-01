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
	rt.petMu.Lock()
	state := rt.petSessions[petOwnerKey{division, strings.ToLower(character.Name)}]
	if state != nil && character.ActiveCOS != nil && !character.ActiveCOS.Mounted {
		if state.follower != nil && state.follower.GID() == character.ActiveCOS.GID {
			pose := state.follower.Position(now)
			rt.petMu.Unlock()
			return pose
		}
		if state.transportCOS != nil && state.transportCOS.GID == character.ActiveCOS.GID {
			pose := state.transportWorld.LiveSpawnAt(now)
			rt.petMu.Unlock()
			return pose
		}
	}
	rt.petMu.Unlock()
	return rt.liveSpawn(simulation.WorldKey(division, character.Name), character, now)
}

/*
================
cosMovementBlocked
================
*/
func (rt *Runtime) cosMovementBlocked(division string, character *enterworld.Character) bool {
	if character == nil || character.ActiveCOS == nil {
		return false
	}
	block := rt.cosAbnormal(division, character.Name, character.ActiveCOS.GID)
	return block != nil && block.Mask&(abnormal.Freeze.Bit()|abnormal.Sleep.Bit()|abnormal.Root.Bit()|abnormal.Stun.Bit()) != 0
}

/*
================
stopCosMovement
================
*/
func (rt *Runtime) stopCosMovement(division string, character *enterworld.Character, now int64) []wire.Frame {
	if character == nil || character.ActiveCOS == nil {
		return nil
	}
	pet := character.ActiveCOS
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
	state := rt.petSessions[petOwnerKey{division, strings.ToLower(character.Name)}]
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
	if owner.ref == nil || owner.c.ActiveCOS == nil {
		return nil
	}
	pet := owner.c.ActiveCOS
	walk, run := owner.Param(movementWalkParameter), owner.Param(movementRunParameter)
	if pet.Mounted && rt.Worlds != nil {
		rt.Worlds.Update(simulation.WorldKey(owner.division, owner.c.Name),
			func() simulation.WorldState { return simulation.SeedWorldState(owner.c) },
			func(world *simulation.WorldState) { world.UpdateMovementSpeeds(walk, run, owner.now) })
	}
	rt.petMu.Lock()
	state := rt.petSessions[petOwnerKey{owner.division, strings.ToLower(owner.c.Name)}]
	if state != nil && state.follower != nil && state.follower.GID() == pet.GID {
		state.follower.SetMovementSpeeds(walk, run, owner.now)
	}
	rt.petMu.Unlock()
	return []wire.Frame{{Opcode: movementSpeedOpcode,
		Payload: wire.NewWriter(movementSpeedBytes).U32(pet.GID).F32(walk).F32(run).Payload()}}
}
