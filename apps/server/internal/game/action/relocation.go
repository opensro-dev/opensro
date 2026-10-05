/*
===========================================================================

relocation.go - moving a character elsewhere, possibly into another world

The server-driven teleport (the PC world teleport, CGObjPC vtable +0x378)
that a GM warp and the fortress war's expulsions share: settle the pose at
the destination, move the world membership, carry the returning pet, then
rebuild the scene with an ordered re-entry. Anything that fails before the
re-entry is published is rolled back; a client never sees half a move.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
relocateCharacter

admit runs inside the character update and returns the destination, or
false to refuse. The caller holds the division lock; the character's own
frames go through PushCharacterFrames and its peers see the correction.
================
*/
func (rt *Runtime) relocateCharacter(division string, c *enterworld.Character, label string, admit func() (travelPoint, bool)) bool {
	if c == nil || rt.PushCharacterFrames == nil {
		return false
	}
	name, key := c.Name, simulation.WorldKey(division, c.Name)
	var previous simulation.WorldState
	var previousWorld *enterworld.CharacterWorld
	var arrival travelPoint
	currentWorld := instance.ID(domain.CharacterWorldInstance(c))
	rt.bindResidentRegion(key, rt.Now().UnixMilli())
	if !rt.deps.Update(c, label, func() bool {
		if c.DeletePending {
			return false
		}
		var admitted bool
		if arrival, admitted = admit(); !admitted {
			return false
		}
		previousWorld = c.World
		state := rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
			previous = *w
			w.Spawn = arrival.spawn
			w.MoveSegment = nil
			w.Sitting = false
			w.PostureTransitionUntilMs = 0
			w.SpawnSet = true
			w.MovementSourceSeeded = true
		})
		writeBackWorld(c, state)
		c.World.MoveSegment = nil
		setCharacterWorld(c, arrival.world)
		return true
	}) {
		return false
	}
	rollback := func() {
		rt.deps.Update(c, label+"-rollback", func() bool {
			rt.Worlds.Update(key, func() simulation.WorldState { return previous }, func(w *simulation.WorldState) { *w = previous })
			c.World = previousWorld
			return true
		})
	}
	membership, moved := rt.moveWorldMembership(division, c, currentWorld, arrival.world)
	if !moved {
		rollback()
		return false
	}
	destination := arrival.spawn
	rt.endTransformForLoading(division, c)
	rt.endPartyAurasForLoading(division, c)
	previousPets := rt.relocateReturningPet(division, c, destination)
	packets, ok := rt.deps.ReentryPackets(division, name)
	if !ok || len(packets) == 0 || packets[0].NativeOpcode != enterworld.OpcodeResetClient {
		if currentWorld != arrival.world {
			rt.restorePopulationSession(membership)
		}
		rt.restoreCompanionRelocation(previousPets)
		// No partial reset may escape: never leave the actor staring at an
		// unfinished loading screen.
		rollback()
		return false
	}
	rt.bindResidentRegion(key, rt.Now().UnixMilli())
	rt.retireReturnForReentry(division, c)
	corpses, _ := rt.retireCompanionCorpses(division, c)
	frames := append(missionReentryFrames(packets), corpses...)
	if snapshot := rt.characterSnapshot(division, c); snapshot != nil && snapshot.NativeBodyStatus != 0 {
		frames = append(frames, bodyStatusFrame(enterworld.ObjectIDForCharacter(c), snapshot.NativeBodyStatus))
	}
	rt.PushCharacterFrames(division, name, frames)
	rt.Pending.Clear(grounditem.PendingKey(division, name))
	rt.Selected.Clear(division, name)
	rt.NpcDialogs.Clear(division, name)
	rt.ClearCombatIntent(division, name)
	rt.clearSkillFinalizes(division, name)
	rt.clearCompoundJob(compoundKey{division, name})
	rt.AbandonExchange(division, name)
	if rt.PushDivisionPeerFrames != nil {
		rt.PushDivisionPeerFrames(division, name, []wire.Frame{{Opcode: wire.OpObjectSourceCorrection, Payload: wire.ObjectSourceCorrection{Gid: enterworld.ObjectIDForCharacter(c), Position: wire.Position{RegionID: destination.RegionID, X: float32(destination.X), Y: float32(destination.Y), Z: float32(destination.Z), Heading: destination.Angle}}.Encode()}})
	}
	return true
}
