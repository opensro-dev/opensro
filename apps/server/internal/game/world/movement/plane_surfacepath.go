/*
===========================================================================

plane_surfacepath.go - the object-surface path on the planes (#1673)

The retained object-cell path along a movement chord, as the two
planes answer it. WaterValidator.objectSurfacePath (in
object_surface_path.go) asks the pair's plane here instead of testing
the dungeon region bit itself; the pair's plane is picked through
pairIsOutdoor (plane_clip.go), so a chord with any cave end is the
cave plane's.

===========================================================================
*/
package movement

import (
	"math"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

//============================================================================
// outdoor

/*
================
outdoorPlane.objectSurfacePath

client 403D20 chooses the initial surface by nearest |delta Y|. 428930 then
retains connected cells; it does NOT resolve ownership from chord Y at
each terrain tile. That chord cuts below a stair even with legal endpoints.
This is a source-owner walk, not the outside-object probe in 428300.
Collision remains responsible for edge flags. Ownership ends at an outline
or an unconnected cell; it cannot jump onto an overlapping deck.
================
*/
func (o outdoorPlane) objectSurfacePath(from, to simulation.Spawn) *objectOwnedPath {
	surface := o.water.surfaceForRegion(from.RegionID)
	if surface == nil {
		return nil
	}
	bx := from.X + float64(simulation.SectorX(from.RegionID)-simulation.SectorX(surface.seedRegionID))*surface.regionSize
	bz := from.Z + float64(simulation.SectorY(from.RegionID)-simulation.SectorY(surface.seedRegionID))*surface.regionSize
	terrain, ok := surface.terrainHeightAt(bx, bz)
	if !ok {
		return nil
	}
	stand := o.water.spawnObjectDeckStand(surface, bx, bz, from.Y, terrain)
	if stand == nil {
		return nil
	}
	a := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: from.RegionID, X: from.X, Z: from.Z})
	b := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: to.RegionID, X: to.X, Z: to.Z})
	p := stand.placement
	c, s := math.Cos(p.yaw), math.Sin(p.yaw)
	local := func(x, z float64) (float64, float64) {
		x -= stand.anchorX + p.x
		z -= stand.anchorZ + p.z
		return c*x + s*z, -s*x + c*z
	}
	x0, z0 := local(a.X, a.Z)
	x1, z1 := local(b.X, b.Z)
	return traceObjectCells(stand, x0, z0, x1, z1)
}

//============================================================================
// cave

/*
================
cavePlane.objectSurfacePath

A cave chord retains no object-surface path: the cave floor's cells
own cave movement (the dungeon walk), never a deck surface.
================
*/
func (cavePlane) objectSurfacePath(simulation.Spawn, simulation.Spawn) *objectOwnedPath {
	return nil
}
