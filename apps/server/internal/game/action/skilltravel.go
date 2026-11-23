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

The reference server queries movement from the caster's own cell. Navigation refusal leaves
all authority unchanged; clipping retains the surface actually reached.
================
*/
func (rt *Runtime) planSkillTravel(name string, from simulation.Spawn, owner simulation.NavOwner, to simulation.Spawn) (skillTravelPlan, bool) {
	return rt.planSkillMove(name, from, owner, to, false)
}

/*
================
planSkillTeleport

tel2 lands at once: the destination is seated by FindNavCell at the point
itself (nearest |deltaY|, terrain wins ties): the v1.188 server checks a
teleport point afresh, with no owner hint, and keeps an object cell only
when its height difference is strictly smaller than the terrain's. It is
the same rule movement.ts applies to a kind-2 displacement. Never by the
walk's resting surface. Seating with that surface as the hint kept a
terrain owner and dropped a Wizard who teleported across a deck onto the
ground under it (region 0x6687, y -30 -> -111, #3927).
================
*/
func (rt *Runtime) planSkillTeleport(name string, from simulation.Spawn, owner simulation.NavOwner, to simulation.Spawn) (skillTravelPlan, bool) {
	return rt.planSkillMove(name, from, owner, to, true)
}

// landingExitSteps is how many seats planSkillMove tries along the chord,
// the requested one first, before it refuses a landing with no exit.
const landingExitSteps = 8

// landingExitProbe is the length of each exit leg landingHasExit walks
// from a seat; landingExitProgress is how far one must get to count.
const landingExitProbe = 12.0
const landingExitProgress = 2.0

// landingExitDirections is the number of exit legs, evenly spaced.
const landingExitDirections = 8

/*
================
planSkillMove

The shared planner. The walk still refuses or clips the destination; only
the landing seat differs between travel and teleport.

Port-only, not native; the default by owner decision 2026-11-24 (#2807: "we
dont need to copy native bugs"). The v1.188 server commits a clipped stop on
any standable cell, so Aerial Teleport could seat a character on a tree's
object cell it can never walk off. A landing must have a walkable exit: the
requested seat is kept when it has one, otherwise the stop is pulled back
along the chord to the furthest seat that does, and a cast with no such
seat is refused (the caster keeps its place and its resources).
================
*/
func (rt *Runtime) planSkillMove(name string, from simulation.Spawn, owner simulation.NavOwner, to simulation.Spawn, teleport bool) (skillTravelPlan, bool) {
	if rt.ConstrainMovement == nil && rt.ConstrainWalk == nil {
		return skillTravelPlan{}, false
	}
	plan, ok := rt.planSkillSeat(name, from, owner, to, teleport)
	if !ok {
		return skillTravelPlan{}, false
	}
	if rt.landingHasExit(name, plan) {
		return plan, true
	}
	a := worldgeom.RegionXZ{RegionID: from.RegionID, X: from.X, Z: from.Z}
	b := worldgeom.RegionXZ{RegionID: plan.to.RegionID, X: plan.to.X, Z: plan.to.Z}
	for step := 1; step < landingExitSteps; step++ {
		fraction := 1 - float64(step)/landingExitSteps
		p := worldgeom.Interpolate(a, b, fraction)
		back := simulation.Spawn{RegionID: p.RegionID, X: p.X, Y: from.Y + (to.Y-from.Y)*fraction, Z: p.Z, Angle: to.Angle}
		if candidate, ok := rt.planSkillSeat(name, from, owner, back, teleport); ok && rt.landingHasExit(name, candidate) {
			return candidate, true
		}
	}
	return skillTravelPlan{}, false
}

/*
================
landingHasExit

Whether a planned seat can be walked away from: some short leg from it, in
the seat's own navigation owner, gets anywhere. A tree's standable object
cell clips every leg at its edge.
================
*/
func (rt *Runtime) landingHasExit(name string, plan skillTravelPlan) bool {
	for i := 0; i < landingExitDirections; i++ {
		angle := 2 * math.Pi * float64(i) / landingExitDirections
		seat := worldgeom.RegionXZ{RegionID: plan.to.RegionID, X: plan.to.X, Z: plan.to.Z}
		// A leg near a region edge crosses the seam into the next region.
		end := worldgeom.NormalizeOutdoor(worldgeom.RegionXZ{RegionID: seat.RegionID, X: seat.X + landingExitProbe*math.Cos(angle), Z: seat.Z + landingExitProbe*math.Sin(angle)})
		probe := simulation.Spawn{RegionID: end.RegionID, X: end.X, Y: plan.to.Y, Z: end.Z, Angle: plan.to.Angle}
		walked, _, err := rt.constrainWalk(name, plan.to, plan.owner, probe)
		if err != nil {
			continue
		}
		dx, dz := worldgeom.Delta(seat, worldgeom.RegionXZ{RegionID: walked.RegionID, X: walked.X, Z: walked.Z})
		if math.Hypot(dx, dz) >= landingExitProgress {
			return true
		}
	}
	return false
}

/*
================
planSkillSeat

One destination: the walk refuses or clips it, then the landing is seated.
================
*/
func (rt *Runtime) planSkillSeat(name string, from simulation.Spawn, owner simulation.NavOwner, to simulation.Spawn, teleport bool) (skillTravelPlan, bool) {
	requestedY := to.Y
	to, walk, err := rt.constrainWalk(name, from, owner, to)
	if err != nil {
		return skillTravelPlan{}, false
	}
	to.X, to.Z = math.Trunc(to.X), math.Trunc(to.Z)
	owner = walk.Rest
	if rt.ResolveNavOwner != nil {
		seat, hint := to, walk.Rest
		if teleport {
			seat.Y, hint = requestedY, simulation.NavOwner{}
		}
		if resolved, y, ok := rt.ResolveNavOwner(seat, hint); ok {
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
	rt.notePlacement(character, "skill-travel")
	character.World.MoveSegment = nil
}

/*
================
chargeSkillGoal

The reference server's tel3 branch caps the three-dimensional displacement at its range.
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
