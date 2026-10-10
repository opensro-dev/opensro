/*
===========================================================================

groundwalk.go - native finite ground steps and authoritative stop delivery

Admission retains the requested intent. The shared world store runs pure
geometry on each actual elapsed step before any gameplay snapshot can read
it. Persistence and wire publication happen outside its lock.

===========================================================================
*/
package movement

import (
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
EnableGroundWalk
================
*/
func (rt *Runtime) EnableGroundWalk() {
	rt.groundEnabled = true
	rt.Worlds.ConfigureGroundWalk(simulation.GroundWalkConfig{
		Now:  func() int64 { return rt.Now().UnixMilli() },
		Step: rt.groundStep,
	})
}

/*
================
AdmitGroundWalk

An intent is not a geometric movement. The original single-query clipping
seam remains available for skills and path queries; player movement queries
that seam only from the actual source of each finite step.
================
*/
func (rt *Runtime) AdmitGroundWalk(name string, from simulation.Spawn, owner simulation.NavOwner, to simulation.Spawn) (simulation.Spawn, simulation.NavWalk, *simulation.MoveError) {
	if !rt.groundEnabled {
		return rt.ConstrainMovementFrom(name, from, owner, to)
	}
	if simulation.IsDungeonRegion(from.RegionID) != simulation.IsDungeonRegion(to.RegionID) ||
		simulation.IsDungeonRegion(from.RegionID) && from.RegionID != to.RegionID {
		return from, simulation.NavWalk{}, &simulation.MoveError{NativeErrorCode: simulation.NativeErrorInvalidRequest,
			Reason: "ground movement cannot cross disconnected navigation spaces"}
	}
	return simulation.NormalizeSpawnFrame(to), simulation.NavWalk{}, nil
}

/*
================
groundStep

No character or world-store callbacks may be introduced here: the shared
world store already owns its lock and may be called inside a read door.
================
*/
func (rt *Runtime) groundStep(from simulation.Spawn, owner simulation.NavOwner, to simulation.Spawn) (simulation.Spawn, simulation.NavOwner, bool) {
	requested := to
	blocked := false
	var acceptedOwner simulation.NavOwner
	if rt.ClientClip != nil {
		to, acceptedOwner, blocked = rt.ClientClip.processOwnedStep("ground-step", from, owner, to)
	}
	if rt.PathGuard != nil && rt.PathGuard.InspectMoveFrom("ground-step", from, owner, to) != nil {
		return from, owner, true
	}
	if acceptedOwner.Resolved() {
		return to, acceptedOwner, blocked
	}
	to, walk := rt.walkOwners(from, owner, to)
	blocked = blocked || to.RegionID != requested.RegionID || to.X != requested.X || to.Z != requested.Z
	return to, walk.Rest, blocked
}

/*
================
GroundTickHook

Snapshot publication already advances connected actors. This drain commits
only accepted poses and publishes native B2F5 stops, after the 0x342F
notice of a tether refusal. Revision fencing drops a stop superseded by a
later input, teleport or life transition.
================
*/
func (rt *Runtime) GroundTickHook() simulation.TickHook {
	return func(_ int64) []simulation.DivisionFrames {
		var frames []simulation.DivisionFrames
		for _, update := range rt.Worlds.DrainGroundUpdates() {
			division, name, ok := strings.Cut(update.Key, ":")
			if !ok {
				continue
			}
			var character *enterworld.Character
			for _, candidate := range rt.deps.CharactersForDivision(division) {
				if candidate != nil && strings.EqualFold(candidate.Name, name) {
					character = candidate
					break
				}
			}
			if character == nil {
				continue
			}
			unlock := rt.lockCharacter(division, character.Name)
			var state simulation.WorldState
			committed := rt.deps.Update(character, "ground-step", func() bool {
				if character.DeletePending {
					return false
				}
				state = rt.Worlds.Snapshot(update.Key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
				if state.GroundRevision() != update.Revision {
					return false
				}
				writeBackWorld(character, state)
				return true
			})
			walk, directional := rt.directions.get(update.Key)
			continuing := directional && !walk.blocked && walk.current(state)
			if committed && update.Tether != 0 {
				// 4F1230 answers the refused step before it stops the player.
				frames = append(frames, simulation.DivisionFrames{DivisionID: division, OnlyCharacterID: character.ID,
					Frames: []simulation.Frame{{Opcode: wire.OpCosDistanceError, Payload: []byte{update.Tether}}}})
			}
			if committed && (update.Stopped || update.Arrived && !continuing) {
				if update.Stopped {
					rt.directions.clear(update.Key)
				}
				frame := directionCorrectionFrame(enterworld.ObjectIDForCharacter(character), state.PersistedSpawn())
				key, revision := update.Key, update.Revision
				frames = append(frames, simulation.DivisionFrames{DivisionID: division, SourceGID: enterworld.ObjectIDForCharacter(character),
					Frames: []simulation.Frame{{Opcode: frame.Opcode, Payload: frame.Payload,
						Current: func() bool { return rt.Worlds.GroundRevisionCurrent(key, revision) }}},
				})
			}
			if committed && update.Stopped && rt.GroundBlocked != nil {
				rt.GroundBlocked(division, character.Name, update.Revision)
			}
			unlock()
		}
		return frames
	}
}
