/*
===========================================================================

plane_clip.go - the clip family on the planes (#1671)

The segment clip and the applied-clip arrival rule, as methods of the
two planes, and the two dispatchers that pick the plane for them. The
movement package's own clip reaches the planes without a Planes value:
a segment clip runs on the validator alone, often with no runtime, so
the dispatchers pick the plane by region kind (simulation.PlaneKindOf)
and call its method. Each #1646 family keeps its plane methods in its
own plane_<family>.go, so parallel steps do not edit one file.

===========================================================================
*/
package movement

import "opensro.online/server/internal/game/world/simulation"

/*
================
clipSegmentOnPlane

One owned segment, clipped by its plane. A pair wholly outdoors is the
outdoor plane's; a pair touching a cave is the cave's, whose clip
refuses a chord across regions - so a chord between the open world and
a cave is refused, as the boundary is crossed only by teleport.
================
*/
func clipSegmentOnPlane(v *WaterValidator, from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) (ClipReport, *navWalk) {
	services := planeServices{water: v}
	if simulation.PlaneKindOf(from.RegionID) == simulation.PlaneOutdoor && simulation.PlaneKindOf(to.RegionID) == simulation.PlaneOutdoor {
		return outdoorPlane{services}.clipSegment(from, fromOwner, to)
	}
	return cavePlane{services}.clipSegment(from, fromOwner, to)
}

/*
================
arrivalKeepsRestOnPlane

Whether an applied clip that arrived keeps the clip's walked rest on
the plane of regionID (ClientClip.Constrain), rather than the requested
goal.
================
*/
func arrivalKeepsRestOnPlane(regionID uint16) bool {
	if simulation.PlaneKindOf(regionID) == simulation.PlaneCave {
		return cavePlane{}.arrivalKeepsRest()
	}
	return outdoorPlane{}.arrivalKeepsRest()
}

/*
================
outdoorPlane.clipSegment

The world tile grid walk and the owned object surfaces (clip.go).
================
*/
func (o outdoorPlane) clipSegment(from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) (ClipReport, *navWalk) {
	return o.water.clipOutdoorSegment(from, fromOwner, to)
}

/*
================
outdoorPlane.arrivalKeepsRest

An outdoor arrival stands on the requested goal: the tile walk reports
the goal as its rest, and only a continued step (handled by the caller)
stands short of it.
================
*/
func (outdoorPlane) arrivalKeepsRest() bool { return false }

/*
================
cavePlane.clipSegment

The dungeon clip (dungeon_clip.go): object contacts, edge cuts sampled
for floor, and obstacle circles. It refuses a chord whose endpoints sit
in different regions. The start owner is unused: a cave start resolves
by its floor height.
================
*/
func (c cavePlane) clipSegment(from simulation.Spawn, _ simulation.NavOwner, to simulation.Spawn) (ClipReport, *navWalk) {
	return c.water.clipDungeonPath(from, to), nil
}

/*
================
cavePlane.arrivalKeepsRest

A cave arrival stands where the clip's walk reached: clipDungeonPath
reports it with NativeResult 0, and its Rest.Y is the floor the step
reached - taking the goal instead kept the server at the ramp-top
height while the client descended, so every later cave clip started
over 2 m off its floor.
================
*/
func (cavePlane) arrivalKeepsRest() bool { return true }
