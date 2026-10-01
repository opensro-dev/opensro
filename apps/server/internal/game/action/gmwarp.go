/*
===========================================================================

gmwarp.go - owns gmwarp behavior and its authority boundary

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// WarpGM owns relocation, transient action cleanup and ordered world re-entry.
// The GM dispatcher supplies decoded values, never writes character state.
/*
================
WarpGM
================
*/
func (rt *Runtime) WarpGM(division, name string, p wire.Position) bool {
	if rt == nil || rt.deps == nil || rt.PushCharacterFrames == nil {
		return false
	}
	authority, ok := rt.deps.(interface {
		ResolveGMWarpDestination(*enterworld.Character, simulation.Spawn) (simulation.Spawn, bool)
	})
	if !ok {
		return false
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil {
		return false
	}
	destination := simulation.Spawn{RegionID: p.RegionID, X: float64(p.X), Y: float64(p.Y), Z: float64(p.Z), Angle: p.Heading}
	var previous simulation.WorldState
	var previousWorld *enterworld.CharacterWorld
	rt.bindResidentRegion(simulation.WorldKey(division, name), rt.Now().UnixMilli())
	if !rt.deps.Update(c, "gm-warp", func() bool {
		if c.DeletePending || !c.GMPrivilege || !enterworld.CharacterAlive(c) {
			return false
		}
		// Active COS need a joint owner/follower migration; do not split poses.
		if c.ActiveCOS != nil && c.ActiveCOS.Summoned {
			return false
		}
		var admitted bool
		destination, admitted = authority.ResolveGMWarpDestination(c, destination)
		if !admitted {
			return false
		}
		previousWorld = c.World
		state := rt.Worlds.Update(simulation.WorldKey(division, name), func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
			previous = *w
			w.Spawn = destination
			w.MoveSegment = nil
			w.Sitting = false
			w.PostureTransitionUntilMs = 0
			w.SpawnSet = true
			w.MovementSourceSeeded = true
		})
		writeBackWorld(c, state)
		c.World.MoveSegment = nil
		return true
	}) {
		return false
	}
	rt.endTransformForLoading(division, c)
	previousPets := rt.relocateReturningPet(division, c, destination)
	packets, ok := rt.deps.ReentryPackets(division, name)
	if ok && len(packets) > 0 && packets[0].NativeOpcode == enterworld.OpcodeResetClient {
		rt.bindResidentRegion(simulation.WorldKey(division, name), rt.Now().UnixMilli())
		rt.retireReturnForReentry(division, c)
		frames := missionReentryFrames(packets)
		if snapshot := rt.characterSnapshot(division, c); snapshot != nil && snapshot.NativeBodyStatus != 0 {
			frames = append(frames, bodyStatusFrame(enterworld.ObjectIDForCharacter(c), snapshot.NativeBodyStatus))
		}
		rt.PushCharacterFrames(division, name, frames)
	} else {
		rt.restoreCompanionRelocation(previousPets)
		// No partial reset may escape. Restore movement if entry construction
		// refuses; never leave the actor staring at an unfinished loading screen.
		rt.deps.Update(c, "gm-warp-rollback", func() bool {
			rt.Worlds.Update(simulation.WorldKey(division, name), func() simulation.WorldState { return previous }, func(w *simulation.WorldState) { *w = previous })
			c.World = previousWorld
			return true
		})
		return false
	}
	rt.Pending.Clear(grounditem.PendingKey(division, name))
	rt.Selected.Clear(division, name)
	rt.NpcDialogs.Clear(division, name)
	rt.ClearCombatIntent(division, name)
	rt.clearSkillFinalizes(division, name)
	rt.clearCompoundJob(compoundKey{division, name})
	if rt.PushDivisionPeerFrames != nil {
		rt.PushDivisionPeerFrames(division, name, []wire.Frame{{Opcode: wire.OpObjectSourceCorrection, Payload: wire.ObjectSourceCorrection{Gid: enterworld.ObjectIDForCharacter(c), Position: wire.Position{RegionID: destination.RegionID, X: float32(destination.X), Y: float32(destination.Y), Z: float32(destination.Z), Heading: destination.Angle}}.Encode()}})
	}
	return true
}
