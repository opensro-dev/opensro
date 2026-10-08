/*
===========================================================================

object_owner_links.go - transfers the walked surface through authored portals

Native 9A0309..9A045B chooses the linked placement and its receiving edge's
triangle. Overlapping triangles and the terrain below cannot take ownership
instead. Collision still decides whether the requested walk reaches the edge.

===========================================================================
*/
package movement

import (
	"math"
	"opensro.online/server/internal/game/world/simulation"
)

// nativeLinkedResult is the owned outline's linked-object result bit.
const nativeLinkedResult uint32 = 2
const nativeContinueResult uint32 = 0x10

/*
================
objectTransfer
================
*/
type objectTransfer struct {
	at    float64
	rest  simulation.Spawn
	owner simulation.NavOwner
}

/*
================
linkedTransfer

9A0397..9A045B resolves the authored placement ordinal and receiving edge.
9B5800 places the reached point inside that edge's cell, even if a finite
step ends in the small gap between the two meshes. No second crossing or
endpoint-height test participates in this native transfer.
================
*/
func (w *navWalk) linkedTransfer(to simulation.Spawn) *objectTransfer {
	if w == nil {
		return nil
	}
	var best *objectTransfer
	for _, path := range w.paths {
		s := path.stand
		if len(s.set[s.objectIndex].meshes) != 1 {
			continue
		}
		m, p := s.mesh, s.placement
		dx, dz := path.x1-path.x0, path.z1-path.z0
		for _, link := range p.links {
			e := link.edge
			if link.target == 65535 || e < 0 || e >= len(m.outline.flags) || m.outline.flags[e]&8 == 0 || m.outline.flags[e]&0x12 != 0 {
				continue
			}
			va, vb := int(m.outline.vertA[e]), int(m.outline.vertB[e])
			ax, az := float64(m.vertices[va*3]), float64(m.vertices[va*3+2])
			bx, bz := float64(m.vertices[vb*3]), float64(m.vertices[vb*3+2])
			sx, sz := bx-ax, bz-az
			den := dx*sz - dz*sx
			if math.Abs(den) < 1e-12 {
				continue
			}
			t, u := ((ax-path.x0)*sz-(az-path.z0)*sx)/den, ((ax-path.x0)*dz-(az-path.z0)*dx)/den
			if t <= 0 || t > 1 || u < 0 || u > 1 || best != nil && t >= best.at {
				continue
			}
			cell, owned := path.cellAt(t)
			if !owned || cell != int(m.outline.srcCell[e]) {
				continue
			}
			cx, cz := objectCellCentroid2D(m, cell)
			if (sx*(cz-az)-sz*(cx-ax))*(sx*(path.z0-az)-sz*(path.x0-ax)) <= 0 {
				continue
			}
			for index, target := range s.set {
				if target.placement.ordinal != link.target || len(target.meshes) != 1 {
					continue
				}
				mesh := target.meshes[0]
				if link.targetEdge < 0 || link.targetEdge >= len(mesh.outline.srcCell) {
					break
				}
				next := *s
				next.objectIndex, next.meshIndex, next.mesh, next.placement = index, 0, mesh, target.placement
				next.cellIndex = int(mesh.outline.srcCell[link.targetEdge])
				if next.cellIndex >= mesh.cellCount() {
					break
				}
				x, z := contactF32(path.x0+dx*t), contactF32(path.z0+dz*t)
				// 9B553E..9B56EA adds the nearer vertex's authored .01 bias.
				if len(m.vertexDirections) == len(m.vertices)/3 {
					x, z = outsideEdgeStart(x, z, x, z, ax, az, bx, bz, m.vertexDirections[va], m.vertexDirections[vb])
				}
				c, sn := math.Cos(p.yaw), math.Sin(p.yaw)
				gx, gz := c*x-sn*z+s.anchorX+p.x, sn*x+c*z+s.anchorZ+p.z
				x, z = next.local(gx, gz)
				x, z = ownedCellStart(mesh, next.cellIndex, x, z)
				y, ok := objectCellPlaneYAt(mesh, next.cellIndex, x, z)
				if !ok {
					break
				}
				c, sn = math.Cos(next.placement.yaw), math.Sin(next.placement.yaw)
				gx, gz = c*x-sn*z+next.anchorX+next.placement.x, sn*x+c*z+next.anchorZ+next.placement.z
				rest := to
				rest.X, rest.Y, rest.Z = gx-float64(simulation.SectorX(to.RegionID))*simulation.NativeRegionSize, y+next.placement.y, gz-float64(simulation.SectorY(to.RegionID))*simulation.NativeRegionSize
				best = &objectTransfer{t, simulation.NormalizeSpawnFrame(rest), next.navOwner(next.cellIndex)}
				break
			}
		}
	}
	return best
}

/*
================
linkedContinuation
================
*/
func (p *objectOwnedPath) linkedContinuation(start float64) (*objectDeckStand, float64, float64) {
	s := p.stand
	// Trace repairs a boundary start in the retained cell. Query the same
	// repaired chord: using the unmodified input can put its portal crossing
	// just beyond the source path's last span.
	c, sn := math.Cos(s.placement.yaw), math.Sin(s.placement.yaw)
	ax, az := c*p.x0-sn*p.z0+s.placement.x, sn*p.x0+c*p.z0+s.placement.z
	bx, bz := c*p.x1-sn*p.z1+s.placement.x, sn*p.x1+c*p.z1+s.placement.z
	passages := resolveSurfacePassages(s.set, [3]float64{ax, 0, az}, [3]float64{bx, 0, bz}, true)
	var next *objectDeckStand
	leave, enter := math.Inf(1), 0.0
	for _, link := range passages {
		if link.source != s.objectIndex || link.leave < start || link.leave >= leave {
			continue
		}
		cell, ok := p.cellAt(link.leave)
		if !ok || int(s.mesh.outline.srcCell[link.edge]) != cell || s.mesh.outline.flags[link.edge]&8 == 0 {
			continue
		}
		target := s.set[link.target]
		mesh := target.meshes[0]
		copy := *s
		copy.objectIndex, copy.meshIndex, copy.mesh = link.target, 0, mesh
		copy.placement = target.placement
		copy.cellIndex = int(mesh.outline.srcCell[link.targetEdge])
		next, leave, enter = &copy, link.leave, math.Max(link.leave, link.enter)
	}
	return next, leave, enter
}

/*
================
truncateAt
================
*/
func (p *objectOwnedPath) truncateAt(t float64) {
	for i, span := range p.spans {
		if span.from >= t {
			p.spans = p.spans[:i]
			return
		}
		if span.to >= t {
			p.spans[i].to = t
			p.spans = p.spans[:i+1]
			return
		}
	}
}
