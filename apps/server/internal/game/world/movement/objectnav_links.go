package movement

import "math"

// A reachable link is not itself an exit: rescue must traverse its destination
// cell and prove a terrain exit there. The visited set terminates linked cycles.
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
type objectPassage struct {
	source, target, edge, targetEdge int
	from, to                         float64
}
type objectPassages []objectPassage

func (p objectPassages) permits(source, edge int) bool {
	for _, link := range p {
		if link.source == source && link.edge == edge || link.target == source && link.targetEdge == edge {
			return true
		}
	}
	return false
}
func (p objectPassages) covers(t float64) bool {
	for _, link := range p {
		if t >= link.from && t <= link.to {
			return true
		}
	}
	return false
}
func resolveObjectPassages(set []resolvedObjectNav, x0, y0, z0, x1, y1, z1 float64) objectPassages {
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
		sx, sz := b[0]-a[0], b[2]-a[2]
		den := dx*sz - dz*sx
		if math.Abs(den) < 1e-12 {
			return 0, false
		}
		t, u := ((a[0]-x0)*sz-(a[2]-z0)*sx)/den, ((a[0]-x0)*dz-(a[2]-z0)*dx)/den
		if t < 0 || t > 1 || u < 0 || u > 1 || math.Abs(y0+(y1-y0)*t-(a[1]+(b[1]-a[1])*u)) > 2 {
			return 0, false
		}
		cx, cz := objectCellCentroid2D(m, int(m.outline.srcCell[edge]))
		p := obj.placement
		c, s := math.Cos(p.yaw), math.Sin(p.yaw)
		cx, cz = c*cx-s*cz+p.x, s*cx+c*cz+p.z
		side, approach := sx*(cz-a[2])-sz*(cx-a[0]), sx*(z0-a[2])-sz*(x0-a[0])
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
		return ok && math.Abs(h+p.y-(y0+(y1-y0)*t)) <= 2
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
			passages = append(passages, objectPassage{i, j, link.edge, link.targetEdge, math.Max(0, math.Min(a, b)-0.19999998807907104/math.Max(math.Hypot(dx, dz), 1e-12)), math.Min(1, math.Max(a, b)+0.19999998807907104/math.Max(math.Hypot(dx, dz), 1e-12))})
		}
	}
	return passages
}
func objectLinkCrossing(set []resolvedObjectNav, source int, mesh *objectNavMesh, x0, y0, z0, x1, y1, z1 float64) func(int, bool, float64) bool {
	passages := resolveObjectPassages(set, x0, y0, z0, x1, y1, z1)
	return func(edge int, _ bool, _ float64) bool { return passages.permits(source, edge) }
}
