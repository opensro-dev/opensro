/*
===========================================================================

dungeon_links.go - portal links between a dungeon's blocks

Resolves which outline edges of connected blocks meet (client 453940, 453020,
client 45D180) and builds the object-nav set the chord scans walk, each
placement with its rotation and reach.

===========================================================================
*/
package movement

import "math"

// 453940 scans declared connected blocks, then outline edges in authored order.
// client 453020/45d180 compare world endpoints with a strict five-unit distance.
func portalEndpoint(p objectNavPlacement, m *objectNavMesh, edge, end int) [3]float64 {
	at := m.outline.vertA[edge]
	if end != 0 {
		at = m.outline.vertB[edge]
	}
	x, y, z := float64(m.vertices[at*3]), float64(m.vertices[at*3+1]), float64(m.vertices[at*3+2])
	c, s := math.Cos(p.yaw), math.Sin(p.yaw)
	return [3]float64{float64(float32(c*x - s*z + p.x)), float64(float32(y + p.y)), float64(float32(s*x + c*z + p.z))}
}
func portalNear(a, b [3]float64) bool {
	x, y, z := float64(float32(a[0]-b[0])), float64(float32(a[1]-b[1])), float64(float32(a[2]-b[2]))
	return float32(math.Sqrt(float64(float32(x*x+y*y+z*z)))) < 5
}
func portalMatches(p objectNavPlacement, m *objectNavMesh, e int, q objectNavPlacement, n *objectNavMesh, f int) bool {
	a, b, c, d := portalEndpoint(p, m, e, 0), portalEndpoint(p, m, e, 1), portalEndpoint(q, n, f, 0), portalEndpoint(q, n, f, 1)
	if portalNear(a, c) {
		return portalNear(b, d)
	}
	return portalNear(b, c) && portalNear(a, d)
}
func resolveDungeonLinks(blocks []dungeonSpawnBlock) []resolvedObjectNav {
	set := make([]resolvedObjectNav, len(blocks))
	indices := map[int]int{}
	for i, b := range blocks {
		set[i] = resolvedObjectNav{placement: objectNavPlacement{x: b.x, y: b.y, z: b.z, yaw: b.yaw, ordinal: b.ordinal}, meshes: b.meshes,
			cosYaw: math.Cos(b.yaw), sinYaw: math.Sin(b.yaw)}
		// The reach lets every chord scan skip a block it cannot touch
		// (objectnav_reach.go): dungeon blocks register no terrain cells.
		set[i].reach = newObjectReach(&set[i])
		indices[b.ordinal] = i
	}
	for i, b := range blocks {
		if len(b.meshes) != 1 {
			continue
		}
		m := b.meshes[0]
		for edge, flags := range m.outline.flags {
			if flags&8 == 0 {
				continue
			}
		search:
			for _, ordinal := range b.connected {
				j, ok := indices[ordinal]
				if !ok || len(set[j].meshes) != 1 {
					continue
				}
				n := set[j].meshes[0]
				for other, flags := range n.outline.flags {
					if flags&8 != 0 && portalMatches(set[i].placement, m, edge, set[j].placement, n, other) {
						set[i].placement.links = append(set[i].placement.links, objectNavLink{ordinal, other, edge})
						break search
					}
				}
			}
		}
	}
	return set
}
