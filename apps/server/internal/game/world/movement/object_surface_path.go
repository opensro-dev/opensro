/*
===========================================================================

object_surface_path.go - retains connected object cells along a movement chord

Decoded adjacency is immutable; each query owns only its cuts and result.

===========================================================================
*/
package movement

import (
	"math"
	"sort"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
objectCellSpan
================
*/
type objectCellSpan struct {
	from, to float64
	cell     int
}

/*
================
objectOwnedPath
================
*/
type objectOwnedPath struct {
	stand          *objectDeckStand
	x0, z0, x1, z1 float64 // object local chord; Y follows the owned cell plane
	spans          []objectCellSpan
}

// 403D20 chooses the initial surface by nearest |delta Y|. 428930 then
// retains connected cells; it does NOT resolve ownership from chord Y at
// each terrain tile. That chord cuts below a stair even with legal endpoints.
// This is a source-owner walk, not the outside-object probe in 428300.
// Collision remains responsible for edge flags. Ownership ends at an outline
// or an unconnected cell; it cannot jump onto an overlapping deck.
/*
================
objectSurfacePath
================
*/
func (v *WaterValidator) objectSurfacePath(from, to simulation.Spawn) *objectOwnedPath {
	if simulation.IsDungeonRegion(from.RegionID) || simulation.IsDungeonRegion(to.RegionID) {
		return nil
	}
	surface := v.surfaceForRegion(from.RegionID)
	if surface == nil {
		return nil
	}
	bx := from.X + float64(simulation.SectorX(from.RegionID)-simulation.SectorX(surface.seedRegionID))*surface.regionSize
	bz := from.Z + float64(simulation.SectorY(from.RegionID)-simulation.SectorY(surface.seedRegionID))*surface.regionSize
	terrain, ok := surface.terrainHeightAt(bx, bz)
	if !ok {
		return nil
	}
	stand := v.spawnObjectDeckStand(surface, bx, bz, from.Y, terrain)
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

// Pure geometry owner shared by terrain clipping and pathguard. Partition at
// triangle edges, then retain the current cell or its edge-connected neighbor.
// The existing mesh limits bound this work; no arbitrary height window is used.
/*
================
traceObjectCells
================
*/
func traceObjectCells(stand *objectDeckStand, x0, z0, x1, z1 float64) *objectOwnedPath {
	return traceObjectCellsFrom(stand, x0, z0, x1, z1, 0)
}

// traceObjectCellsFrom traces from chord fraction tStart, where stand's cell
// owns the walker (an object entered part-way along the chord). Spans keep the
// global chord parameter so several owners can share one chord.
/*
================
traceObjectCellsFrom
================
*/
func traceObjectCellsFrom(stand *objectDeckStand, x0, z0, x1, z1, tStart float64) *objectOwnedPath {
	if tStart == 0 {
		x, z := x0+(x1-x0)*tStart, z0+(z1-z0)*tStart
		nx, nz := ownedCellStart(stand.mesh, stand.cellIndex, x, z)
		x0 += (nx - x) / (1 - tStart)
		z0 += (nz - z) / (1 - tStart)
	}
	path := &objectOwnedPath{stand: stand, x0: x0, z0: z0, x1: x1, z1: z1}
	m := stand.mesh
	neighbors := m.cellNeighbors
	if neighbors == nil {
		// Hand-built geometry can omit decoded metadata. Keep it local so
		// concurrent readers never race to populate an immutable mesh.
		neighbors = buildObjectCellNeighbors(m)
	}
	dx, dz := x1-x0, z1-z0
	cuts := []float64{tStart, 1}
	for cell := 0; cell < m.cellCount(); cell++ {
		vertices := [3]uint16{m.cellA[cell], m.cellB[cell], m.cellC[cell]}
		for k, a := range vertices {
			b := vertices[(k+1)%3]
			ax, az := float64(m.vertices[a*3]), float64(m.vertices[a*3+2])
			sx, sz := float64(m.vertices[b*3])-ax, float64(m.vertices[b*3+2])-az
			den := dx*sz - dz*sx
			if math.Abs(den) < 1e-12 {
				continue
			}
			t, u := ((ax-x0)*sz-(az-z0)*sx)/den, ((ax-x0)*dz-(az-z0)*dx)/den
			if t > tStart && t < 1 && u >= 0 && u <= 1 {
				cuts = append(cuts, t)
			}
		}
	}
	sort.Float64s(cuts)
	owner := stand.cellIndex
	// Span midpoints sit between consecutive triangle-edge cuts. When a chord
	// passes ~0.002u from a shared vertex across a narrow fan of internal and
	// outline edges (region 23661 localUid 3073), objectCellPlaneYAt's -1e-4
	// endpoint tolerance can still claim the previous cell on the next span's
	// midpoint (barycentric ~-9e-5) and skip the adjacent cell's blocking
	// outline edge. Match client navHeight's -1e-6 barycentric tolerance here.
	containsSpan := func(cell int, lo, hi float64) bool {
		const eps = -1e-6
		t := (lo + hi) / 2
		lx, lz := x0+dx*t, z0+dz*t
		a, b, cc := m.cellA[cell], m.cellB[cell], m.cellC[cell]
		ax, az := float64(m.vertices[a*3]), float64(m.vertices[a*3+2])
		bx, bz := float64(m.vertices[b*3]), float64(m.vertices[b*3+2])
		cx, cz := float64(m.vertices[cc*3]), float64(m.vertices[cc*3+2])
		den := (bz-cz)*(ax-cx) + (cx-bx)*(az-cz)
		if math.Abs(den) < 1e-9 {
			return false
		}
		l0 := ((bz-cz)*(lx-cx) + (cx-bx)*(lz-cz)) / den
		l1 := ((cz-az)*(lx-cx) + (ax-cx)*(lz-cz)) / den
		l2 := 1 - l0 - l1
		return l0 >= eps && l1 >= eps && l2 >= eps
	}
	for i := 1; i < len(cuts); i++ {
		lo, hi := cuts[i-1], cuts[i]
		if hi-lo < 1e-9 {
			continue
		}
		if !containsSpan(owner, lo, hi) {
			next := -1
			for _, cell := range neighbors[owner] {
				if containsSpan(cell, lo, hi) {
					next = cell
					break
				}
			}
			if next < 0 {
				break
			}
			owner = next
		}
		n := len(path.spans)
		if n > 0 && path.spans[n-1].cell == owner {
			path.spans[n-1].to = hi
		} else {
			path.spans = append(path.spans, objectCellSpan{lo, hi, owner})
		}
	}
	return path
}

/*
================
buildObjectCellNeighbors
================
*/
func buildObjectCellNeighbors(mesh *objectNavMesh) [][]int {
	neighbors := make([][]int, mesh.cellCount())
	for i := range mesh.internal.flags {
		a, b := int(mesh.internal.srcCell[i]), int(mesh.internal.dstCell[i])
		if a >= mesh.cellCount() || b >= mesh.cellCount() {
			continue
		}
		// Preserve both directions, duplicates and input order: the first
		// containing neighbor wins when triangles share a boundary.
		neighbors[a] = append(neighbors[a], b)
		neighbors[b] = append(neighbors[b], a)
	}
	return neighbors
}

/*
================
cellAt
================
*/
func (p *objectOwnedPath) cellAt(t float64) (int, bool) {
	if p != nil {
		for _, span := range p.spans {
			if t >= span.from-1e-9 && t <= span.to+1e-9 {
				return span.cell, true
			}
		}
	}
	return 0, false
}

/*
================
heightAt
================
*/
func (p *objectOwnedPath) heightAt(t float64) (float64, bool) {
	cell, ok := p.cellAt(t)
	if !ok {
		return 0, false
	}
	y, ok := objectCellPlaneYAt(p.stand.mesh, cell, p.x0+(p.x1-p.x0)*t, p.z0+(p.z1-p.z0)*t)
	return y + p.stand.placement.y, ok
}
