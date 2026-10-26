/*
===========================================================================

owned_start.go - the owned start of a mover's first step

ownedCellStart computes the cell-edge entry a start outside its
retained cell is replaced by; WaterValidator.ownedStart dispatches
that adjustment through the planes (#1674, plane_ownedstart.go),
picking the plane by region kind so the clip, the owner walk and the
path guard never test the region bit to start a mover.

===========================================================================
*/
package movement

import "opensro.online/server/internal/game/world/simulation"

/*
================
ownedCellStart

Native (client 428F40; the server's owned-start routine does the same):
an outside retained-cell start is replaced by the proper intersection of
start->centroid with a cell edge, THEN nudged inward by the nav-cell
centroid nudge (client 45C1B0). The intersection output aliases the
temporary start. Extending ownership over an unmodified outside chord is
incorrect.
================
*/
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

/*
================
ownedStart

The start's plane owns the owned-start adjustment (#1674): picked by
region kind here, so no caller tests the region bit.
================
*/
func (v *WaterValidator) ownedStart(from simulation.Spawn, owner simulation.NavOwner) simulation.Spawn {
	services := planeServices{water: v}
	if simulation.PlaneKindOf(from.RegionID) == simulation.PlaneCave {
		return cavePlane{services}.ownedStart(from, owner)
	}
	return outdoorPlane{services}.ownedStart(from, owner)
}
