/*
===========================================================================

plane.go - the outdoor and cave navigation planes (#1646)

simulation.Plane's two implementations. Step 1 of #1646 wraps today's code
unchanged, so each method calls exactly what callers reach now; later steps
move the remaining region-bit forks inside these two types, one area at a
time, and #1645 gives the cave its native region-route waypoint as
cavePlane.NextWaypoint (cave_route.go). Callers hold a simulation.Planes
and never test the region bit themselves (check_plane_branches).

===========================================================================
*/
package movement

import (
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
planeServices

What both planes navigate with: the movement runtime (the clip and the
line test) and the asset-backed validator (heights, paths and routes).
================
*/
type planeServices struct {
	runtime *Runtime
	water   *WaterValidator
}

/*
================
outdoorPlane
================
*/
type outdoorPlane struct{ planeServices }

/*
================
cavePlane
================
*/
type cavePlane struct{ planeServices }

/*
================
planes
================
*/
type planes struct {
	outdoor outdoorPlane
	cave    cavePlane
}

var _ simulation.Planes = (*planes)(nil)

/*
================
NewPlanes

The plane owner over one runtime and its validator.
================
*/
func NewPlanes(runtime *Runtime, water *WaterValidator) simulation.Planes {
	services := planeServices{runtime: runtime, water: water}
	return &planes{outdoor: outdoorPlane{services}, cave: cavePlane{services}}
}

/*
================
For
================
*/
func (p *planes) For(regionID uint16) simulation.Plane {
	if simulation.PlaneKindOf(regionID) == simulation.PlaneCave {
		return p.cave
	}
	return p.outdoor
}

/*
================
Between
================
*/
func (p *planes) Between(a, b uint16) (simulation.Plane, bool) {
	if simulation.PlaneKindOf(a) != simulation.PlaneKindOf(b) {
		return nil, false
	}
	return p.For(a), true
}

//============================================================================
// outdoor

/*
================
outdoorPlane.Kind
================
*/
func (outdoorPlane) Kind() simulation.PlaneKind { return simulation.PlaneOutdoor }

/*
================
outdoorPlane.Clip
================
*/
func (o outdoorPlane) Clip(
	characterName string,
	from simulation.Spawn,
	fromOwner simulation.NavOwner,
	to simulation.Spawn,
) (simulation.Spawn, simulation.NavWalk, *simulation.MoveError) {
	return o.runtime.ConstrainMovementFrom(characterName, from, fromOwner, to)
}

/*
================
outdoorPlane.LineOfSight
================
*/
func (o outdoorPlane) LineOfSight(from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) bool {
	return o.runtime.LineOfSight(from, fromOwner, to)
}

/*
================
outdoorPlane.PlanPath
================
*/
func (o outdoorPlane) PlanPath(
	from monster.Pose,
	fromOwner simulation.NavOwner,
	goal monster.Pose,
) *monster.NavigationPath {
	return o.water.PlanMonsterPathFrom(from, fromOwner, goal)
}

/*
================
outdoorPlane.NextWaypoint

The direction-gate detour (the outdoor pursuit's open-terrain search).
================
*/
func (o outdoorPlane) NextWaypoint(from monster.Pose, _ simulation.NavOwner, goal monster.Pose) *monster.NavigationRoute {
	return o.water.PlanMonsterRoute(from, goal)
}

/*
================
outdoorPlane.PureRoutes

The outdoor search ignores the owner outright and walks geometry
preloaded at boot (PreloadOutdoorNavigation), never invalidated.
================
*/
func (outdoorPlane) PureRoutes() bool { return true }

/*
================
outdoorPlane.Relative
================
*/
func (outdoorPlane) Relative(from, to worldgeom.RegionXZ) (float64, float64) {
	return worldgeom.Delta(from, to)
}

/*
================
outdoorPlane.SameNeighbourhood
================
*/
func (outdoorPlane) SameNeighbourhood(a, b uint16) bool {
	return worldgeom.SamePlaneAdjacent(a, b)
}

//============================================================================
// cave

/*
================
cavePlane.Kind
================
*/
func (cavePlane) Kind() simulation.PlaneKind { return simulation.PlaneCave }

/*
================
cavePlane.Clip
================
*/
func (c cavePlane) Clip(
	characterName string,
	from simulation.Spawn,
	fromOwner simulation.NavOwner,
	to simulation.Spawn,
) (simulation.Spawn, simulation.NavWalk, *simulation.MoveError) {
	return c.runtime.ConstrainMovementFrom(characterName, from, fromOwner, to)
}

/*
================
cavePlane.LineOfSight
================
*/
func (c cavePlane) LineOfSight(from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) bool {
	return c.runtime.LineOfSight(from, fromOwner, to)
}

/*
================
cavePlane.PlanPath
================
*/
func (c cavePlane) PlanPath(
	from monster.Pose,
	fromOwner simulation.NavOwner,
	goal monster.Pose,
) *monster.NavigationPath {
	return c.water.PlanMonsterPathFrom(from, fromOwner, goal)
}

/*
================
cavePlane.NextWaypoint

The cave's region route (cave_route.go) from the mover's retained cell:
straight at the goal when the line walks, otherwise the native route
tables where the dungeon has them (cave_native_route.go), else the port's
portal route.
================
*/
func (c cavePlane) NextWaypoint(from monster.Pose, fromOwner simulation.NavOwner, goal monster.Pose) *monster.NavigationRoute {
	return c.water.planCaveRouteFrom(from, fromOwner, goal)
}

/*
================
cavePlane.PureRoutes

The cave search starts from the mover's retained cell and walks the
floor surfaces loaded once (dungeonSpawnOnce). The owner is an input,
not a variable: the failed-route memo keys it exactly (#1820 part B).
================
*/
func (cavePlane) PureRoutes() bool { return true }

/*
================
cavePlane.Relative
================
*/
func (cavePlane) Relative(from, to worldgeom.RegionXZ) (float64, float64) {
	return worldgeom.Delta(from, to)
}

/*
================
cavePlane.SameNeighbourhood
================
*/
func (cavePlane) SameNeighbourhood(a, b uint16) bool {
	return worldgeom.SamePlaneAdjacent(a, b)
}
