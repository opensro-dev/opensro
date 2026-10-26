/*
===========================================================================

plane_ownedstart.go - the owned start on the planes (#1674)

A mover's first step starts from the surface that owns it. The outdoor
plane replaces a start that stands outside its owning cell with the
cell-edge entry (ownedCellStart, owned_start.go); the cave plane owns
no surface starts, so a cave start stands where it is.
WaterValidator.ownedStart (owned_start.go) picks the plane by region
kind, so its callers never test the region bit.

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
outdoorPlane.ownedStart

The start the owning surface gives the mover: the caller-held owner
wins, an unresolved one is resolved here, and a start outside its
owning cell is replaced by the cell-edge entry, turned into the
region frame by the cell's yaw. A start already inside its cell, or
one no surface owns, stands where it is.
================
*/
func (o outdoorPlane) ownedStart(from simulation.Spawn, owner simulation.NavOwner) simulation.Spawn {
	if !owner.Resolved() {
		owner, _, _ = o.water.ResolveNavOwner(from, simulation.NavOwner{})
	}
	stand := o.water.standForOwner(owner)
	if stand == nil {
		return from
	}
	grid := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: from.RegionID, X: from.X, Z: from.Z})
	x, z := stand.local(grid.X, grid.Z)
	nx, nz := ownedCellStart(stand.mesh, stand.cellIndex, x, z)
	if nx == x && nz == z {
		return from
	}
	c, s := math.Cos(stand.placement.yaw), math.Sin(stand.placement.yaw)
	from.X += c*(nx-x) - s*(nz-z)
	from.Z += s*(nx-x) + c*(nz-z)
	return from
}

//============================================================================
// cave

/*
================
cavePlane.ownedStart

A cave start stands where it is: the cave plane owns no surface
starts, and the dungeon walk owns cave movement from the start's own
cell.
================
*/
func (cavePlane) ownedStart(from simulation.Spawn, _ simulation.NavOwner) simulation.Spawn {
	return from
}
