/*
===========================================================================

objectnav_links.go - authored placement links and collision passage queries

===========================================================================
*/
package movement

import "math"

// A reachable link is not itself an exit: rescue must traverse its destination
// cell and prove a terrain exit there. The visited set terminates linked cycles.
/*
================
objectLinkedLaneSealed
================
*/
func objectLinkedLaneSealed(set []resolvedObjectNav, source int, mesh *objectNavMesh, cell int) bool {
	type node struct {
		object int
		mesh   *objectNavMesh
		cell   int
	}
	queue := []node{{source, mesh, cell}}
	seen := map[node]bool{}
	indices := map[int]int{}
	for i, p := range set {
		indices[p.placement.ordinal] = i
	}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if seen[n] {
			continue
		}
		seen[n] = true
		if len(seen) > 65536 {
			return false
		}
		m := n.mesh
		if n.cell < 0 || n.cell >= m.cellCount() {
			return false
		}
		for i, flags := range m.internal.flags {
			src, dst := int(m.internal.srcCell[i]), int(m.internal.dstCell[i])
			if src == n.cell && flags&2 == 0 {
				queue = append(queue, node{n.object, m, dst})
			}
			if dst == n.cell && flags&1 == 0 {
				queue = append(queue, node{n.object, m, src})
			}
		}
		for edge, flags := range m.outline.flags {
			if int(m.outline.srcCell[edge]) != n.cell {
				continue
			}
			if flags == 0 {
				return false
			}
			if flags&8 == 0 || len(set[n.object].meshes) != 1 {
				continue
			}
			for _, link := range set[n.object].placement.links {
				if link.edge != edge {
					continue
				}
				target, ok := indices[link.target]
				if !ok || len(set[target].meshes) != 1 {
					break
				}
				other := set[target].meshes[0]
				if link.targetEdge >= 0 && link.targetEdge < len(other.outline.srcCell) {
					queue = append(queue, node{target, other, int(other.outline.srcCell[link.targetEdge])})
				}
				break
			}
		}
	}
	return true
}

// Native 403fb0 indexes the region's original placement vector, never the
// filtered list of successfully resolved meshes. Missing/ambiguous targets block.
/*
================
objectPassage
================
*/
type objectPassage struct {
	source, target, edge, targetEdge int
	from, to                         float64
	leave, enter                     float64
}

/*
================
objectPassages
================
*/
type objectPassages []objectPassage

/*
================
permits
================
*/
func (p objectPassages) permits(source, edge int) bool {
	for _, link := range p {
		if link.source == source && link.edge == edge || link.target == source && link.targetEdge == edge {
			return true
		}
	}
	return false
}

/*
================
covers
================
*/
func (p objectPassages) covers(t float64) bool {
	for _, link := range p {
		if t >= link.from && t <= link.to {
			return true
		}
	}
	return false
}

/*
================
resolveObjectPassages
================
*/
func resolveObjectPassages(set []resolvedObjectNav, x0, y0, z0, x1, y1, z1 float64) objectPassages {
	return resolveSurfacePassages(set, [3]float64{x0, y0, z0}, [3]float64{x1, y1, z1}, false)
}

// Once a cell owns the mover, its plane supplies height. The endpoint-Y
// chord is not an admission test for its authored links (9A0309..9A045B).
/*
================
resolveSurfacePassages
================
*/
func resolveSurfacePassages(set []resolvedObjectNav, from, to [3]float64, owned bool) objectPassages {
	x0, y0, z0 := from[0], from[1], from[2]
	x1, y1, z1 := to[0], to[1], to[2]
	dx, dz := x1-x0, z1-z0
	var passages objectPassages
	// Built at the first link: most placements have none, and building it
	// for every monster step allocated about 60 MB a minute.
	var indices map[int]int
	crossing := func(obj resolvedObjectNav, edge int, leaving bool) (float64, bool) {
		if len(obj.meshes) != 1 {
			return 0, false
		}
		m := obj.meshes[0]
		if edge < 0 || edge >= len(m.outline.flags) {
			return 0, false
		}
		a, b := portalEndpoint(obj.placement, m, edge, 0), portalEndpoint(obj.placement, m, edge, 1)
		startX, startZ, deltaX, deltaZ := x0, z0, dx, dz
		cx, cz := objectCellCentroid2D(m, int(m.outline.srcCell[edge]))
		p := obj.placement
		c, s := math.Cos(p.yaw), math.Sin(p.yaw)
		if owned {
			// The mesh walker intersects in object space. The rounded world
			// endpoints belong to link matching, not to this cell's exit.
			x, z := x0-p.x, z0-p.z
			startX, startZ = c*x+s*z, -s*x+c*z
			deltaX, deltaZ = c*dx+s*dz, -s*dx+c*dz
			va, vb := int(m.outline.vertA[edge])*3, int(m.outline.vertB[edge])*3
			a = [3]float64{float64(m.vertices[va]), float64(m.vertices[va+1]), float64(m.vertices[va+2])}
			b = [3]float64{float64(m.vertices[vb]), float64(m.vertices[vb+1]), float64(m.vertices[vb+2])}
		} else {
			cx, cz = c*cx-s*cz+p.x, s*cx+c*cz+p.z
		}
		sx, sz := b[0]-a[0], b[2]-a[2]
		den := deltaX*sz - deltaZ*sx
		if math.Abs(den) < 1e-12 {
			return 0, false
		}
		t, u := ((a[0]-startX)*sz-(a[2]-startZ)*sx)/den, ((a[0]-startX)*deltaZ-(a[2]-startZ)*deltaX)/den
		if t < 0 || t > 1 || u < 0 || u > 1 || !owned && math.Abs(y0+(y1-y0)*t-(a[1]+(b[1]-a[1])*u)) > 2 {
			return 0, false
		}
		side, approach := sx*(cz-a[2])-sz*(cx-a[0]), sx*(startZ-a[2])-sz*(startX-a[0])
		return t, (approach*side > 0) == leaving
	}
	inside := func(obj resolvedObjectNav, edge int, t float64) bool {
		if len(obj.meshes) != 1 {
			return false
		}
		m := obj.meshes[0]
		if edge < 0 || edge >= len(m.outline.srcCell) {
			return false
		}
		p := obj.placement
		c, s := math.Cos(p.yaw), math.Sin(p.yaw)
		x, z := x0+dx*t-p.x, z0+dz*t-p.z
		h, ok := objectCellPlaneYAt(m, int(m.outline.srcCell[edge]), c*x+s*z, -s*x+c*z)
		return ok && (owned || math.Abs(h+p.y-(y0+(y1-y0)*t)) <= 2)
	}
	for i, p := range set {
		for _, link := range p.placement.links {
			if indices == nil {
				indices = make(map[int]int, len(set))
				for k, q := range set {
					indices[q.placement.ordinal] = k
				}
			}
			j, ok := indices[link.target]
			if !ok || link.target == 65535 {
				continue
			}
			a, oka := crossing(p, link.edge, true)
			b, okb := crossing(set[j], link.targetEdge, false)
			if oka && !okb && inside(set[j], link.targetEdge, a) {
				b = a
				okb = true
			} else if okb && !oka && inside(p, link.edge, b) {
				a = b
				oka = true
			}
			if !oka || !okb || math.Abs(a-b)*math.Sqrt(dx*dx+dz*dz+(y1-y0)*(y1-y0)) >= 5 {
				continue
			}
			passages = append(passages, objectPassage{i, j, link.edge, link.targetEdge, math.Max(0, math.Min(a, b)-0.19999998807907104/math.Max(math.Hypot(dx, dz), 1e-12)), math.Min(1, math.Max(a, b)+0.19999998807907104/math.Max(math.Hypot(dx, dz), 1e-12)), a, b})
		}
	}
	return passages
}

/*
================
objectLinkCrossing
================
*/
func objectLinkCrossing(set []resolvedObjectNav, source int, mesh *objectNavMesh, x0, y0, z0, x1, y1, z1 float64) func(int, bool, float64) bool {
	passages := resolveObjectPassages(set, x0, y0, z0, x1, y1, z1)
	return func(edge int, _ bool, _ float64) bool { return passages.permits(source, edge) }
}
