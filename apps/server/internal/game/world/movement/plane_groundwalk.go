/*
===========================================================================

plane_groundwalk.go - ground-walk admission on the planes (#1674)

Whether a ground walk's intent stays inside one navigation space, as
the two planes answer it. AdmitGroundWalk (groundwalk.go) asks here
instead of testing the dungeon region bit itself; the refusal, the
accepted intent and the stepping stay with it. The pair's plane is
picked through pairIsOutdoor (plane_clip.go), so a pair with any cave
end is the cave plane's.

===========================================================================
*/
package movement

import "opensro.online/server/internal/game/world/simulation"

/*
================
groundWalkAdmittedOnPlane

Whether the pair's plane admits a ground walk between the two ends:
wholly inside one navigation space.
================
*/
func groundWalkAdmittedOnPlane(from, to simulation.Spawn) bool {
	if pairIsOutdoor(from.RegionID, to.RegionID) {
		return outdoorPlane{}.admitsGroundWalk(from, to)
	}
	return cavePlane{}.admitsGroundWalk(from, to)
}

//============================================================================
// outdoor

/*
================
outdoorPlane.admitsGroundWalk

The open world is one navigation space: a ground walk between any two
outdoor regions is admitted.
================
*/
func (outdoorPlane) admitsGroundWalk(simulation.Spawn, simulation.Spawn) bool { return true }

//============================================================================
// cave

/*
================
cavePlane.admitsGroundWalk

A cave region is its own navigation space: admitted inside the region
and refused across regions. A pair with any cave end lands here, so a
walk into or out of a cave is refused too - the boundary is crossed
only by teleport.
================
*/
func (cavePlane) admitsGroundWalk(from, to simulation.Spawn) bool {
	return from.RegionID == to.RegionID
}
