/*
===========================================================================

plane_navowner.go - surface ownership and the path guard on the planes
(#1672)

Owner resolution, the owner walk along a chord, and the path guard's
classification, as methods of the two planes, with the dispatchers that
pick the plane (pairs through pairIsOutdoor, plane_clip.go). The cave
plane has no surface owners: its floor is resolved by height, so a cave
point resolves unowned, a cave chord walks no owners, and the dungeon
clip rather than the outdoor tile guard owns cave movement.

===========================================================================
*/
package movement

import "opensro.online/server/internal/game/world/simulation"

/*
================
resolveNavOwnerOnPlane

The owner and surface height of a point, resolved by its plane.
================
*/
func resolveNavOwnerOnPlane(v *WaterValidator, p simulation.Spawn, hint simulation.NavOwner) (simulation.NavOwner, float64, bool) {
	services := planeServices{water: v}
	if simulation.PlaneKindOf(p.RegionID) == simulation.PlaneCave {
		return cavePlane{services}.resolveNavOwner(p, hint)
	}
	return outdoorPlane{services}.resolveNavOwner(p, hint)
}

/*
================
ownerWalkOnPlane

The owner walk along an owned chord, on the pair's plane.
================
*/
func ownerWalkOnPlane(v *WaterValidator, from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) *navWalk {
	services := planeServices{water: v}
	if pairIsOutdoor(from.RegionID, to.RegionID) {
		return outdoorPlane{services}.ownerWalk(from, fromOwner, to)
	}
	return cavePlane{services}.ownerWalk(from, fromOwner, to)
}

/*
================
validatePathOnPlane

The path guard's classification of an owned chord, on the pair's plane.
================
*/
func validatePathOnPlane(v *WaterValidator, from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) PathReport {
	services := planeServices{water: v}
	if pairIsOutdoor(from.RegionID, to.RegionID) {
		return outdoorPlane{services}.validatePath(from, fromOwner, to)
	}
	return cavePlane{services}.validatePath(from, fromOwner, to)
}

//============================================================================
// outdoor

/*
================
outdoorPlane.resolveNavOwner

The native FindNavCell rule over the region's terrain and object
surfaces (navowner.go).
================
*/
func (o outdoorPlane) resolveNavOwner(p simulation.Spawn, hint simulation.NavOwner) (simulation.NavOwner, float64, bool) {
	return o.water.resolveOutdoorNavOwner(p, hint)
}

/*
================
outdoorPlane.ownerWalk
================
*/
func (o outdoorPlane) ownerWalk(from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) *navWalk {
	return o.water.outdoorOwnerWalk(from, fromOwner, to)
}

/*
================
outdoorPlane.validatePath
================
*/
func (o outdoorPlane) validatePath(from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) PathReport {
	return o.water.validateOutdoorPath(from, fromOwner, to)
}

//============================================================================
// cave

/*
================
cavePlane.resolveNavOwner

The dungeon cell that owns p (dungeonOwnerAt): the hint while its cell
still holds p, otherwise the cell under p nearest its height - the
original's relocation rule. The height is returned unchanged: owning the
cell does not move the position (#1727). ok is false where no cell lies
under p.
================
*/
func (c cavePlane) resolveNavOwner(p simulation.Spawn, hint simulation.NavOwner) (simulation.NavOwner, float64, bool) {
	surface := c.water.caveSurface(p.RegionID)
	if surface == nil {
		return simulation.NavOwner{}, p.Y, false
	}
	owner, ok := surface.dungeonOwnerAt(p.RegionID, p.X, p.Y, p.Z, hint)
	return owner, p.Y, ok
}

/*
================
cavePlane.ownerWalk

A cave chord walks no surface owners.
================
*/
func (cavePlane) ownerWalk(simulation.Spawn, simulation.NavOwner, simulation.Spawn) *navWalk {
	return nil
}

/*
================
cavePlane.validatePath

A cave chord is exempt from the outdoor tile guard: the dungeon clip
owns cave movement.
================
*/
func (cavePlane) validatePath(from simulation.Spawn, _ simulation.NavOwner, to simulation.Spawn) PathReport {
	return PathReport{Verdict: PathDungeonExempt, DistanceUnits: simulation.WorldDistance2D(from, to)}
}
