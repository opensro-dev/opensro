/*
===========================================================================

objectnav_reach.go - where one resolved placement can matter to a chord

The two chord scans (objectChordFirstContact and terrainOutlineEntry) visit
every placement of the object sets around a chord, and nearly all of them
are far from it. A placement's reach is a conservative box, in its anchor
region's frame, around everything those scans consult for it: the rotated
bounds of each of its nav meshes (every outline and internal edge, every
cell and contact point lies inside its mesh's vertex bounds) and the union
box of the terrain cells it is registered in. A chord whose own box misses
the reach cannot cross, enter, exit or contact any of the placement's
meshes, so skipping it leaves both scans' results unchanged.

===========================================================================
*/
package movement

import "math"

// objectReachMargin widens every reach box. The scans work in float32-rounded
// object frames and nudge cell entries by 0.2 units (nativeCellEntry); a whole
// unit keeps the box conservative against both.
const objectReachMargin = 1.0

/*
==================
objectReach

minX, minZ, maxX, maxZ in the anchor region's frame. An empty reach
(no meshes and no registration) is never skipped: it is an unknown, not a miss.
==================
*/
type objectReach struct {
	minX, minZ, maxX, maxZ float64
	known                  bool
}

/*
==================
newObjectReach

The reach of one resolved placement: each mesh's vertex bounds turned by the
placement's yaw and moved to its position (the inverse of the scans' object
frame: x = cos*ox - sin*oz + px, z = sin*ox + cos*oz + pz), united with the
registered terrain cells' box, widened by objectReachMargin.
==================
*/
func newObjectReach(entry *resolvedObjectNav) objectReach {
	reach := objectReach{minX: math.Inf(1), minZ: math.Inf(1), maxX: math.Inf(-1), maxZ: math.Inf(-1)}
	add := func(x, z float64) {
		reach.minX, reach.maxX = math.Min(reach.minX, x), math.Max(reach.maxX, x)
		reach.minZ, reach.maxZ = math.Min(reach.minZ, z), math.Max(reach.maxZ, z)
	}
	p := entry.placement
	for _, mesh := range entry.meshes {
		if math.IsInf(mesh.minX, 1) {
			return objectReach{} // a mesh without vertices: never skip this placement
		}
		for _, corner := range [4][2]float64{{mesh.minX, mesh.minZ}, {mesh.maxX, mesh.minZ}, {mesh.minX, mesh.maxZ}, {mesh.maxX, mesh.maxZ}} {
			add(entry.cosYaw*corner[0]-entry.sinYaw*corner[1]+p.x, entry.sinYaw*corner[0]+entry.cosYaw*corner[1]+p.z)
		}
	}
	if cells := p.terrainCells; cells != nil && len(cells.cells) != 0 {
		add(float64(cells.box[0]), float64(cells.box[1]))
		add(float64(cells.box[2]), float64(cells.box[3]))
	}
	if math.IsInf(reach.minX, 1) {
		return objectReach{}
	}
	reach.minX -= objectReachMargin
	reach.minZ -= objectReachMargin
	reach.maxX += objectReachMargin
	reach.maxZ += objectReachMargin
	reach.known = true
	return reach
}

/*
==================
unionReach

The reach of a whole set: the union of its placements' reaches. Unknown when
any placement's is (it must never be skipped) or the set is empty.
==================
*/
func unionReach(set []resolvedObjectNav) objectReach {
	if len(set) == 0 {
		return objectReach{}
	}
	union := objectReach{minX: math.Inf(1), minZ: math.Inf(1), maxX: math.Inf(-1), maxZ: math.Inf(-1), known: true}
	for i := range set {
		r := set[i].reach
		if !r.known {
			return objectReach{}
		}
		union.minX, union.minZ = math.Min(union.minX, r.minX), math.Min(union.minZ, r.minZ)
		union.maxX, union.maxZ = math.Max(union.maxX, r.maxX), math.Max(union.maxZ, r.maxZ)
	}
	return union
}

/*
==================
objectReach.missedBy

Whether the chord x0,z0 -> x1,z1 (anchor frame) lies wholly outside the
reach. Strict, as terrainCellSet.missedBy: touching is not a miss.
==================
*/
func (r *objectReach) missedBy(x0, z0, x1, z1 float64) bool {
	if !r.known {
		return false
	}
	return max(x0, x1) < r.minX || min(x0, x1) > r.maxX || max(z0, z1) < r.minZ || min(z0, z1) > r.maxZ
}
