/*
===========================================================================

skilltravel.go - navigation plans shared by ground travel and target charges

Planning is read-only. The cast owner commits a validated destination with
its resource debit and damage, then publishes that same quantized X/Z pair.
Surface height remains precise so travel cannot bury actors under geometry.

===========================================================================
*/

package action

import (
	"math"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
skillTravelPlan

Position, retained navigation cell and wire point describe one destination.
================
*/
type skillTravelPlan struct {
	to    simulation.Spawn
	owner simulation.NavOwner
	point wire.SkillCastFacingPoint
}

/*
================
planSkillTravel

5862E0 queries movement from the caster's own cell. Navigation refusal leaves
all authority unchanged; clipping retains the surface actually reached.
================
*/
func (rt *Runtime) planSkillTravel(name string, from simulation.Spawn, owner simulation.NavOwner, to simulation.Spawn) (skillTravelPlan, bool) {
	if rt.ConstrainMovement == nil && rt.ConstrainWalk == nil {
		return skillTravelPlan{}, false
	}
	to, walk, err := rt.constrainWalk(name, from, owner, to)
	if err != nil {
		return skillTravelPlan{}, false
	}
	to.X, to.Z = math.Trunc(to.X), math.Trunc(to.Z)
	owner = walk.Rest
	if rt.ResolveNavOwner != nil {
		if resolved, y, ok := rt.ResolveNavOwner(to, walk.Rest); ok {
			owner, to.Y = resolved, y
		}
	}
	point, ok := wire.NewSkillCastFacingPoint(to.RegionID, to.X, to.Y, to.Z)
	if !ok {
		return skillTravelPlan{}, false
	}
	return skillTravelPlan{to: to, owner: owner, point: point}, true
}

/*
================
commitSkillTravel

The caller owns the character write door and has completed all refusal checks.
Replacing the movement segment makes the arrival the next authoritative pose.
================
*/
func (rt *Runtime) commitSkillTravel(key string, character *enterworld.Character, plan skillTravelPlan) {
	state := rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(character) }, func(w *simulation.WorldState) {
		w.Spawn = plan.to
		w.MoveSegment = nil
		w.SetGoalOwner(plan.owner)
		w.SpawnSet = true
		w.MovementSourceSeeded = true
		w.LifeRevision++
	})
	writeBackWorld(character, state)
	character.World.MoveSegment = nil
}

/*
================
chargeSkillGoal

5862E0's tel3 branch caps the three-dimensional displacement at its range.
Within range it backs off the target by both body radii along that vector,
preserving target height before the navigation query resolves the surface.
================
*/
func chargeSkillGoal(from, target simulation.Spawn, limit uint32, contactRadius float64) (simulation.Spawn, bool) {
	goal, valid := positionSkillGoal(from, target, limit)
	if !valid || contactRadius < 0 || math.IsNaN(contactRadius) || math.IsInf(contactRadius, 0) {
		return simulation.Spawn{}, false
	}
	a := worldgeom.RegionXZ{RegionID: from.RegionID, X: from.X, Z: from.Z}
	b := worldgeom.RegionXZ{RegionID: target.RegionID, X: target.X, Z: target.Z}
	dx, dz := worldgeom.Delta(a, b)
	dy := target.Y - from.Y
	distance := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if distance > float64(limit) || distance == 0 {
		return goal, true
	}
	point := worldgeom.Interpolate(a, b, 1-contactRadius/distance)
	return simulation.Spawn{RegionID: point.RegionID, X: point.X, Y: target.Y, Z: point.Z, Angle: from.Angle}, true
}
