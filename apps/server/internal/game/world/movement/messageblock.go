/*
===========================================================================

messageblock.go - the movement owner's answer to "which message block"

The dungeon grid arithmetic and the installed grid table live in the world
package (dungeon_grid.go); this file feeds that table from the resident
dungeon surfaces and answers activity queries with the same grid, after
the nav-cell admission a native query starts from.

===========================================================================
*/
package movement

import (
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
WaterValidator.MessageBlockAt

The activity bucket of a position, from the same resident navigation owner
as monster movement. Missing navigation is an unresolved query, never
whole-dungeon activity.
================
*/
func (v *WaterValidator) MessageBlockAt(p simulation.Spawn) (worldgeom.MessageBlock, bool) {
	if !worldgeom.IsDungeonRegion(p.RegionID) {
		return worldgeom.OutdoorMessageBlock(worldgeom.RegionXZ{RegionID: p.RegionID, X: p.X, Z: p.Z})
	}
	v.dungeonSpawnOnce.Do(func() { v.dungeonSpawnSurfaces = v.loadDungeonSpawnSurfaces() })
	s := v.dungeonSpawnSurfaces[p.RegionID]
	if s == nil {
		return worldgeom.MessageBlock{}, false
	}
	// A native query begins with an admitted nav-cell/resident pair. Reuse
	// nearest-plane admission; do not manufacture a bucket outside the mesh.
	if _, ok := s.heightAt(p.X, p.Y, p.Z); !ok {
		return worldgeom.MessageBlock{}, false
	}
	return s.messageGrid.BlockAt(p.RegionID, p.X, p.Y, p.Z)
}

/*
================
WaterValidator.DungeonMessageGrids

Every resident dungeon's message grid, for worldgeom's interest table. The
GameWorld wiring installs it once at boot (InstallDungeonMessageGrids).
================
*/
func (v *WaterValidator) DungeonMessageGrids() map[uint16]worldgeom.DungeonMessageGrid {
	v.dungeonSpawnOnce.Do(func() { v.dungeonSpawnSurfaces = v.loadDungeonSpawnSurfaces() })
	grids := make(map[uint16]worldgeom.DungeonMessageGrid, len(v.dungeonSpawnSurfaces))
	for region, surface := range v.dungeonSpawnSurfaces {
		if surface != nil {
			grids[region] = surface.messageGrid
		}
	}
	return grids
}
