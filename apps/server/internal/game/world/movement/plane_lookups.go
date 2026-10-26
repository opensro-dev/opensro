/*
===========================================================================

plane_lookups.go - the per-region data lookups, one arm per plane (#1676)

The lookups a region's data answers - the walkable spawn height, spawn
region residency, the boot preload and the message block - each have an
outdoor form and a cave form. The forms live here as methods on the two
plane types; the shared entry points on WaterValidator only pick the
plane (validatorPlanes) and dispatch. No entry point tests the dungeon
region bit any more (check_plane_branches).

All four are plane methods, not PlaneFeatures rows: each arm does
different work (a cave has no water table, its spawn residency is the
resident surface, its surfaces load on first use instead of at boot),
rather than one feature being present or absent per plane.

===========================================================================
*/
package movement

import (
	"math"
	"slices"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
validatorPlanes

The planes over this validator alone, for the data lookups. The
movement runtime stays nil: a lookup answers from the validator's
surfaces and never reaches the clip or the line test, so the lookup
methods must not touch the runtime field.
================
*/
func (v *WaterValidator) validatorPlanes() *planes {
	services := planeServices{water: v}
	return &planes{outdoor: outdoorPlane{services}, cave: cavePlane{services}}
}

//============================================================================
// Dispatch: the plane that owns the region answers the lookup.
//============================================================================

/*
================
planes.walkableSpawnHeightAt
================
*/
func (p *planes) walkableSpawnHeightAt(regionID uint16, x, authoredY, z float64) (float64, bool) {
	if p.For(regionID).Kind() == simulation.PlaneCave {
		return p.cave.walkableSpawnHeightAt(regionID, x, authoredY, z)
	}
	return p.outdoor.walkableSpawnHeightAt(regionID, x, authoredY, z)
}

/*
================
planes.spawnRegionAvailable
================
*/
func (p *planes) spawnRegionAvailable(regionID uint16) bool {
	if p.For(regionID).Kind() == simulation.PlaneCave {
		return p.cave.spawnRegionAvailable(regionID)
	}
	return p.outdoor.spawnRegionAvailable(regionID)
}

/*
================
planes.preloadRegion

True when the plane counts the region in the boot preload set.
================
*/
func (p *planes) preloadRegion(regionID uint16, seen map[*groundSurface]bool) bool {
	if p.For(regionID).Kind() == simulation.PlaneCave {
		return p.cave.preloadRegion(regionID, seen)
	}
	return p.outdoor.preloadRegion(regionID, seen)
}

/*
================
planes.messageBlockAt
================
*/
func (p *planes) messageBlockAt(at simulation.Spawn) (worldgeom.MessageBlock, bool) {
	if p.For(at.RegionID).Kind() == simulation.PlaneCave {
		return p.cave.messageBlockAt(at)
	}
	return p.outdoor.messageBlockAt(at)
}

//============================================================================
// The walkable spawn height
//============================================================================

/*
================
outdoorPlane.walkableSpawnHeightAt

Terrain is only eligible when its nav tile is walkable; a containing
object-nav cell may win the same nearest-Y arbitration used by the
terrain owner even when the terrain below it is blocked (bridges,
platforms, and elevated quest arenas).
================
*/
func (o outdoorPlane) walkableSpawnHeightAt(regionID uint16, x, authoredY, z float64) (float64, bool) {
	v := o.water
	surface := v.surfaceForRegion(regionID)
	if surface == nil {
		return 0, false
	}
	tilesPerAxis, tileSize, ok := v.gridParamsForRegion(regionID)
	if !ok || x < 0 || z < 0 ||
		x >= float64(tilesPerAxis)*tileSize ||
		z >= float64(tilesPerAxis)*tileSize {
		return 0, false
	}

	baseX := x + float64(simulation.SectorX(regionID)-simulation.SectorX(surface.seedRegionID))*surface.regionSize
	baseZ := z + float64(simulation.SectorY(regionID)-simulation.SectorY(surface.seedRegionID))*surface.regionSize
	terrainY, terrainCovered := surface.terrainHeightAt(baseX, baseZ)
	if !terrainCovered {
		return 0, false
	}
	if stand := v.spawnObjectDeckStand(surface, baseX, baseZ, authoredY, terrainY); stand != nil {
		return stand.planeY, true
	}

	tile := globalTile{
		x: simulation.SectorX(regionID)*tilesPerAxis + int(math.Floor(x/tileSize)),
		z: simulation.SectorY(regionID)*tilesPerAxis + int(math.Floor(z/tileSize)),
	}
	if walkable, covered := v.globalTileWalkable(tile, tilesPerAxis); !covered || !walkable {
		return 0, false
	}
	return terrainY, true
}

/*
================
cavePlane.walkableSpawnHeightAt

A cave region has no water table and no sector math: the resident
dungeon-nav host answers, against the floor nearest the authored Y.
================
*/
func (c cavePlane) walkableSpawnHeightAt(regionID uint16, x, authoredY, z float64) (float64, bool) {
	return c.water.dungeonSpawnHeightAt(regionID, x, authoredY, z)
}

//============================================================================
// Spawn region residency
//============================================================================

/*
================
outdoorPlane.spawnRegionAvailable

A resolvable outdoor region is the equivalent of native resident
state 1: its sector grid resolves.
================
*/
func (o outdoorPlane) spawnRegionAvailable(regionID uint16) bool {
	_, _, available := o.water.gridParamsForRegion(regionID)
	return available
}

/*
================
cavePlane.spawnRegionAvailable

A cave spawn region is resident when its dungeon surface is.
================
*/
func (c cavePlane) spawnRegionAvailable(regionID uint16) bool {
	v := c.water
	v.dungeonSpawnOnce.Do(func() {
		v.dungeonSpawnSurfaces = v.loadDungeonSpawnSurfaces()
	})
	return v.dungeonSpawnSurfaces[regionID] != nil
}

//============================================================================
// The boot preload
//============================================================================

/*
================
outdoorPlane.preloadRegion

Warms one outdoor region's surface and object-nav offsets before the
world ticks, so no first-use region loads inside the simulation. The
region counts in the preload set even when its surface is absent:
missing optional assets retain their cached nil.
================
*/
func (o outdoorPlane) preloadRegion(regionID uint16, seen map[*groundSurface]bool) bool {
	v := o.water
	surface := v.surfaceForRegion(regionID)
	if surface == nil || seen[surface] {
		return true
	}
	seen[surface] = true
	offsets := make([]int64, 0, len(surface.objectPlacementsByOffset))
	for offset := range surface.objectPlacementsByOffset {
		offsets = append(offsets, offset)
	}
	slices.Sort(offsets)
	for _, offset := range offsets {
		// offsetKey stores signed sector differences as dx*65536+dz.
		// Recover the signed low word before dividing out the high word.
		dz := int(int16(offset))
		dx := int((offset - int64(dz)) / sectorOffsetStride)
		v.objectNavSetForOffset(surface, dx, dz)
	}
	return true
}

/*
================
cavePlane.preloadRegion

Nothing to warm and not counted: cave surfaces load on first use
through the validator's dungeon once, as they do today.
================
*/
func (c cavePlane) preloadRegion(regionID uint16, seen map[*groundSurface]bool) bool {
	return false
}

//============================================================================
// The message block
//============================================================================

/*
================
outdoorPlane.messageBlockAt
================
*/
func (o outdoorPlane) messageBlockAt(at simulation.Spawn) (worldgeom.MessageBlock, bool) {
	return worldgeom.OutdoorMessageBlock(worldgeom.RegionXZ{RegionID: at.RegionID, X: at.X, Z: at.Z})
}

/*
================
cavePlane.messageBlockAt

The cave's block from the resident surface's message grid, after the
nav-cell admission a native query starts from: a position with no
admitted floor has no bucket, never whole-dungeon activity.
================
*/
func (c cavePlane) messageBlockAt(at simulation.Spawn) (worldgeom.MessageBlock, bool) {
	v := c.water
	v.dungeonSpawnOnce.Do(func() {
		v.dungeonSpawnSurfaces = v.loadDungeonSpawnSurfaces()
	})
	s := v.dungeonSpawnSurfaces[at.RegionID]
	if s == nil {
		return worldgeom.MessageBlock{}, false
	}
	// A native query begins with an admitted nav-cell/resident pair. Reuse
	// nearest-plane admission; do not manufacture a bucket outside the mesh.
	if _, ok := s.heightAt(at.X, at.Y, at.Z); !ok {
		return worldgeom.MessageBlock{}, false
	}
	return s.messageGrid.BlockAt(at.RegionID, at.X, at.Y, at.Z)
}
