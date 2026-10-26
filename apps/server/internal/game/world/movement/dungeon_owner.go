/*
===========================================================================

dungeon_owner.go - the dungeon cell a mover stands in (#1727)

The original keeps every mover's nav cell in its position record and walks
each move from it; only a relocation (teleport, spawn, entering the world)
finds the cell again, by position: among the cells under the point, the
one nearest its height. A dungeon owner names that cell by value - the
region, the block's DOF ordinal, the mesh and the cell - so movers carry it
the way they carry an outdoor surface owner. The browser client keeps the
same owner (dungeon-ownership.ts).

===========================================================================
*/
package movement

import (
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
dungeonOwner
================
*/
func dungeonOwner(region uint16, ordinal, mesh, cell int) simulation.NavOwner {
	return simulation.NavOwner{Kind: simulation.NavOwnerDungeon, Object: simulation.NavObjectCell{
		Surface: region, Object: int32(ordinal), Mesh: int32(mesh), Cell: int32(cell),
	}}
}

/*
================
dungeonSpawnSurface.dungeonOwnerAt

The dungeon cell owning a point: the hint, while it is a dungeon owner of
this region whose cell still holds the point's XZ, otherwise the cell
under the point nearest its height (floorCellAt, the relocation rule).
false where no cell lies under the point.
================
*/
func (s *dungeonSpawnSurface) dungeonOwnerAt(region uint16, x, y, z float64, hint simulation.NavOwner) (simulation.NavOwner, bool) {
	if s.ownerHolds(region, x, z, hint) {
		return hint, true
	}
	block, mesh, cell, ok := s.floorCellAt(x, y, z)
	if !ok {
		return simulation.NavOwner{}, false
	}
	return dungeonOwner(region, s.blocks[block].ordinal, mesh, cell), true
}

/*
================
dungeonSpawnSurface.ownerHolds

Whether a dungeon owner names a cell of this region's surface whose
triangle holds the point's XZ.
================
*/
func (s *dungeonSpawnSurface) ownerHolds(region uint16, x, z float64, owner simulation.NavOwner) bool {
	if owner.Kind != simulation.NavOwnerDungeon || owner.Object.Surface != region {
		return false
	}
	// objects and blocks share one order (newDungeonSpawnSurface).
	index, ok := s.ordinalIndex[int(owner.Object.Object)]
	if !ok {
		return false
	}
	block := &s.blocks[index]
	m := int(owner.Object.Mesh)
	if m < 0 || m >= len(block.meshes) {
		return false
	}
	mesh := block.meshes[m]
	cell := int(owner.Object.Cell)
	if cell < 0 || cell >= mesh.cellCount() {
		return false
	}
	dx, dz := x-block.x, z-block.z
	localX := block.cosYaw*dx + block.sinYaw*dz
	localZ := -block.sinYaw*dx + block.cosYaw*dz
	_, inside := objectCellPlaneYAt(mesh, cell, localX, localZ)
	return inside
}
