package movement

import (
	"math"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

// Native (client 428F40): an outside retained-cell start is replaced by
// the proper intersection of start->centroid with a cell edge, THEN nudged
// inward by the nav-cell centroid nudge (45C1B0). The intersection output
// aliases the temporary start. Extending ownership over an unmodified
// outside chord is incorrect.
func ownedCellStart(mesh *objectNavMesh, cell int, x, z float64) (float64, float64) {
	f := contactF32
	ids := [3]uint16{mesh.cellA[cell], mesh.cellB[cell], mesh.cellC[cell]}
	cx, cz := objectCellCentroid2D(mesh, cell)
	cx, cz = f(cx), f(cz)
	outside, boundary := false, false
	for i, a := range ids {
		b := ids[(i+1)%3]
		ax, az := float64(mesh.vertices[int(a)*3]), float64(mesh.vertices[int(a)*3+2])
		sx, sz := f(float64(mesh.vertices[int(b)*3])-ax), f(float64(mesh.vertices[int(b)*3+2])-az)
		side, inside := f(sz*f(x-ax)-sx*f(z-az)), sz*(cx-ax)-sx*(cz-az)
		outside = outside || side*inside < 0
		boundary = boundary || side == 0
	}
	if !outside {
		if boundary {
			return nativeCellEntry(cx, cz, x, z)
		}
		return x, z
	}
	dx, dz := f(cx-f(x)), f(cz-f(z))
	for i, a := range ids {
		b := ids[(i+1)%3]
		ax, az := float64(mesh.vertices[int(a)*3]), float64(mesh.vertices[int(a)*3+2])
		sx, sz := f(float64(mesh.vertices[int(b)*3])-ax), f(float64(mesh.vertices[int(b)*3+2])-az)
		den := f(dx*sz - dz*sx)
		if den == 0 {
			continue
		}
		t, u := f(f((ax-x)*sz-(az-z)*sx)/den), f(f((ax-x)*dz-(az-z)*dx)/den)
		// Seg2D_Intersect code 2 uses half-open oriented endpoint tests.
		if t > 0 && t <= 1 && u >= 0 && u < 1 {
			return nativeCellEntry(cx, cz, f(f(x)+dx*t), f(f(z)+dz*t))
		}
	}
	return x, z
}

func (v *WaterValidator) ownedStart(from simulation.Spawn, owner simulation.NavOwner) simulation.Spawn {
	if simulation.IsDungeonRegion(from.RegionID) {
		return from
	}
	if !owner.Resolved() {
		owner, _, _ = v.ResolveNavOwner(from, simulation.NavOwner{})
	}
	stand := v.standForOwner(owner)
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
