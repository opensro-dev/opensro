/*
===========================================================================

contact_response.go - native object-edge contact responses (client 45C1B0 / 428300)

===========================================================================
*/
package movement

import "math"

/*
================
objectContactPoint
================
*/
type objectContactPoint struct {
	x, y, z      float64
	valid        bool
	outside      bool
	continuation bool
	// outline marks a contact on an outline edge: the reflect branch whose
	// failing leg returns 0x10000000 (client 403FB0). Internal side-block contacts
	// commit the clipped point instead (428930 bit 0).
	outline bool
	// edgeFlags are the contacted edge's flags. The dungeon clip reads
	// them to tell a wall from a doorway (dungeonOutlineResult).
	edgeFlags byte
}

/*
================
contactF32
================
*/
func contactF32(n float64) float64 { return float64(float32(n)) }

// Original client 45c1b0/40efd0: honor each stored float, including reciprocal.
/*
================
nativeCellEntry
================
*/
func nativeCellEntry(cx, cz, x, z float64) (float64, float64) {
	f := contactF32
	dx, dz := f(f(cx)-f(x)), f(f(cz)-f(z))
	length := f(math.Sqrt(f(dx*dx + dz*dz)))
	const limit = 0.19999998807907104
	if length > limit {
		inverse := f(1 / length)
		dx = f(f(dx*inverse) * limit)
		dz = f(f(dz*inverse) * limit)
	}
	return f(f(x) + dx), f(f(z) + dz)
}

/*
================
nativeObjectContact

Resolves the native rest point for an edge contact on mesh: outsideEdgeStart
(428300) when the blocked approach is from outside an outline edge
(cell == 65535), or nativeCellEntry (client 45C1B0) into the approached cell.
================
*/
func nativeObjectContact(mesh *objectNavMesh, edges *objectNavEdges, edge int, source bool, x0, z0, y0, x1, z1 float64, reflection ...bool) objectContactPoint {
	cell := int(edges.srcCell[edge])
	if !source {
		cell = int(edges.dstCell[edge])
	}
	f := contactF32
	a, b := int(edges.vertA[edge])*3, int(edges.vertB[edge])*3
	x0, z0 = f(x0), f(z0)
	dx, dz := f(f(x1)-x0), f(f(z1)-z0)
	ex, ez := f(float64(mesh.vertices[b])-float64(mesh.vertices[a])), f(float64(mesh.vertices[b+2])-float64(mesh.vertices[a+2]))
	nx, nz := f(x0-float64(mesh.vertices[a])), f(z0-float64(mesh.vertices[a+2]))
	t := f(f(ex*nz-ez*nx) / f(ez*dx-ex*dz))
	if cell == 65535 {
		if len(mesh.vertexDirections) != len(mesh.vertices)/3 || edges.flags[edge]&1 == 0 {
			return objectContactPoint{}
		}
		ax, az := float64(mesh.vertices[a]), float64(mesh.vertices[a+2])
		bx, bz := float64(mesh.vertices[b]), float64(mesh.vertices[b+2])
		x, z := outsideEdgeStart(x0, z0, f(x0+dx*t), f(z0+dz*t), ax, az, bx, bz, mesh.vertexDirections[a/3], mesh.vertexDirections[b/3])
		if src := int(edges.srcCell[edge]); src < mesh.cellCount() {
			// Port-only, not native: client 0x004287A8..0x004288A6 adds 0.01 * vertexDirection
			// to the outside start without checking whether the vertex's shared 1-byte
			// direction points inward across the hit outline edge (e.g. v[0] dir=88 and
			// v[3] dir=148 on rock_m_smalltree05_m_01.bms out[2]). When the start is
			// within 0.01u of the outline edge, that inward nudge crosses the blocking
			// outline into srcCell while keeping terrain ownership, letting the next
			// click either stop inside the obstacle without crossing an outline edge or
			// drift past the outline margin and capture NavOwnerObject. Keep the rest
			// point on the exterior side of the hit outline edge whenever the 0.01u
			// bias moves inward into srcCell.
			cx, cz := objectCellCentroid2D(mesh, src)
			sX, sZ := bx-ax, bz-az
			edgeLen := math.Hypot(sX, sZ)
			srcSide := sX*(cz-az) - sZ*(cx-ax)
			if edgeLen > 0 && srcSide != 0 {
				outX, outZ := sZ/edgeLen, -sX/edgeLen
				if srcSide < 0 {
					outX, outZ = -outX, -outZ
				}
				const minOutsideDist = 1e-3
				outsideDist0 := (x0-ax)*outX + (z0-az)*outZ
				outsideDist := (x-ax)*outX + (z-az)*outZ
				if outsideDist < minOutsideDist {
					x, z = x0, z0
					if outsideDist0 >= 0 && outsideDist0 < minOutsideDist {
						shift := minOutsideDist - outsideDist0
						x, z = f(x0+outX*shift), f(z0+outZ*shift)
					}
				}
			}
		}
		return objectContactPoint{x: x, y: f(y0), z: z, valid: true, outside: true}
	}
	cx, cz := objectCellCentroid2D(mesh, cell)
	x, z := nativeCellEntry(f(cx), f(cz), f(x0+dx*t), f(z0+dz*t))
	continued := len(reflection) > 0 && reflection[0]
	if continued {
		x = f(2*f(x0+dx*t) - x)
		z = f(2*f(z0+dz*t) - z)
	}
	y, ok := objectCellPlaneYAt(mesh, cell, x, z)
	if !ok {
		y = y0
	}
	return objectContactPoint{x: x, y: f(y), z: z, valid: true, continuation: continued}
}
