/*
===========================================================================

placementguard.go - is a pose inside an object no walk can enter?

A tree trunk, a rock or a pillar is a placed object whose nav mesh has no
link edges and whose every outline edge blocks (flags & 0x03). The walker
never crosses such an outline (a chord that meets it is refused and rests
where it started), so a player standing inside one was put there by
something other than a walk - and can never walk out (BR-261014-1425-CE1E,
a player inside a tree at region 0x5F6A, asset 1160).

InsideClosedObject answers for one pose so the code paths that place a
player (a teleport, a rebirth, a ride) can report themselves. It reads
only; it changes nobody's position.

===========================================================================
*/
package movement

import (
	"os"
	"strings"

	"opensro.online/server/internal/game/world/simulation"
)

// EnvClosedObjectRescue turns the closed-object relog rescue on ("on", "1",
// "true"). Port-only, not native: off keeps the login rescue as it was.
const EnvClosedObjectRescue = "SRO_CLOSED_OBJECT_RESCUE"

// outlineBlocks are the outline flag bits the chord clip treats as a wall
// (objectnav_collision.go).
const outlineBlocks = 0x03

// closedObjectYSlack is how far above or below a closed mesh's height range
// a pose still counts as inside it. Inferred, not native: a stand on the
// object's footprint sits within a step of its cells (the reported trunk's
// cells are 1 unit above the terrain), while a bridge or deck over the
// footprint is tens of units higher and is not enclosed.
const closedObjectYSlack = 10

/*
================
InsideClosedObject

The asset of a closed object (no link edges, every outline edge blocking)
whose nav cells contain the pose in x/z, with the pose's height within
closedObjectYSlack of the mesh's height range, or false. Only the pose's
own region entry is consulted.
================
*/
func (v *WaterValidator) InsideClosedObject(pose simulation.Spawn) (int, bool) {
	pose = simulation.NormalizeSpawnFrame(pose)
	surface := v.surfaceForRegion(pose.RegionID)
	if surface == nil {
		return 0, false
	}
	set := v.objectNavSetForOffset(surface, 0, 0)
	for i := range set {
		object := &set[i]
		if object.placement.linkEdgeCount != 0 {
			continue
		}
		// Object-local x/z: the inverse of the placement's yaw rotation.
		dx, dz := pose.X-object.placement.x, pose.Z-object.placement.z
		lx := dx*object.cosYaw + dz*object.sinYaw
		lz := -dx*object.sinYaw + dz*object.cosYaw
		for _, mesh := range object.meshes {
			if lx < mesh.minX || lx > mesh.maxX || lz < mesh.minZ || lz > mesh.maxZ {
				continue
			}
			if ly := pose.Y - object.placement.y; ly < mesh.minY-closedObjectYSlack || ly > mesh.maxY+closedObjectYSlack {
				continue
			}
			if closedMesh(mesh) && meshCellContains(mesh, lx, lz) {
				return object.placement.assetID, true
			}
		}
	}
	return 0, false
}

/*
================
ClosedObjectRescueFromEnv
================
*/
func ClosedObjectRescueFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvClosedObjectRescue))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
WaterValidator.SetClosedObjectRescue

Turns the closed-object rescue in RelocateStrandedSpawn on or off
(EnvClosedObjectRescue). Set once at boot, before the first entry.
================
*/
func (v *WaterValidator) SetClosedObjectRescue(on bool) {
	v.closedObjectRescue = on
}

/*
================
closedMesh

Whether every outline edge of the mesh blocks.
================
*/
func closedMesh(mesh *objectNavMesh) bool {
	if len(mesh.outline.flags) == 0 {
		return false
	}
	for _, flags := range mesh.outline.flags {
		if flags&outlineBlocks == 0 {
			return false
		}
	}
	return true
}

/*
================
meshCellContains

Whether an object-local x/z lies strictly inside one of the mesh's cell
triangles.
================
*/
func meshCellContains(mesh *objectNavMesh, x, z float64) bool {
	vertex := func(i uint16) (float64, float64) {
		return float64(mesh.vertices[int(i)*3]), float64(mesh.vertices[int(i)*3+2])
	}
	for c := range mesh.cellA {
		ax, az := vertex(mesh.cellA[c])
		bx, bz := vertex(mesh.cellB[c])
		cx, cz := vertex(mesh.cellC[c])
		d1 := (x-bx)*(az-bz) - (ax-bx)*(z-bz)
		d2 := (x-cx)*(bz-cz) - (bx-cx)*(z-cz)
		d3 := (x-ax)*(cz-az) - (cx-ax)*(z-az)
		negative := d1 < 0 || d2 < 0 || d3 < 0
		positive := d1 > 0 || d2 > 0 || d3 > 0
		if !(negative && positive) && d1 != 0 && d2 != 0 && d3 != 0 {
			return true
		}
	}
	return false
}
