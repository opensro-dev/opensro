/*
===========================================================================

plane_height.go - a monster path's height sampler on the planes (#1675)

The height a monster path reports at a sample, as a method of each plane,
with the dispatcher that picks the plane. Outdoors a sample stands on the
surface the walk owns there - a deck or bridge under the chord, else the
terrain - so the sampler reads the owner walk first. A cave has no surface
owners: its sample stands on the floor nearest the sample's own height.

===========================================================================
*/
package movement

import (
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
pathHeightOnPlane

A path sample's height at chord parameter t, by the sample's plane.
================
*/
func pathHeightOnPlane(v *WaterValidator, t float64, p monster.Pose, walk *navWalk) (float64, bool) {
	services := planeServices{water: v}
	if simulation.PlaneKindOf(p.RegionID) == simulation.PlaneCave {
		return cavePlane{services}.pathHeightAt(t, p, walk)
	}
	return outdoorPlane{services}.pathHeightAt(t, p, walk)
}

/*
================
outdoorPlane.pathHeightAt

The owner walk's surface at t (a deck or bridge the chord crosses), and
the terrain where the walk owns nothing there.
================
*/
func (o outdoorPlane) pathHeightAt(t float64, p monster.Pose, walk *navWalk) (float64, bool) {
	if y, ok := walk.heightAt(t); ok {
		return y, true
	}
	return o.water.TerrainHeightAt(p.RegionID, p.X, p.Z)
}

/*
================
cavePlane.pathHeightAt

The cave floor nearest the sample's own height (dungeonSpawnHeightAt);
a cave chord walks no owners, so t and the walk are unused.
================
*/
func (c cavePlane) pathHeightAt(_ float64, p monster.Pose, _ *navWalk) (float64, bool) {
	return c.water.dungeonSpawnHeightAt(p.RegionID, p.X, p.Y, p.Z)
}
