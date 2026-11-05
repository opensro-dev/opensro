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
	"math"
	"os"

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

// The seam probe's bounds. It runs at enter-world, and only for a spawn
// InsideClosedObject already cleared, so it must stay cheap on open
// ground and bounded everywhere:
//
//   - seamEscapeRadius is how far a legal walk must reach before the
//     pose counts as having an exit. The lion pocket (#2359) is ~4 units
//     across with its mouth a few units away; open ground reaches 20
//     units unimpeded.
//   - seamProbeStep is the lattice step: fine enough that a 3-unit gate
//     or gap holds a lattice point, coarse enough that the flood below
//     is a few hundred clip probes, not thousands.
//   - seamProbeMaxCells caps the flood. Exhausting it with the frontier
//     still open fails open (not sealed): unknown never strands, the
//     same doctrine as every gate in this package.
const (
	seamEscapeRadius  = 20.0
	seamProbeStep     = 2.0
	seamProbeMaxCells = 1024
	seamProbeRays     = 16
)

// seamArriveEpsilon is how close the clip's rest must land to a probe
// goal for the step to count as walked (a blocked chord rests at its
// contact, units short; an arrived one stands on the goal).
const seamArriveEpsilon = 0.5

/*
================
poseSealedInSeam

Whether a pose on walkable ground, inside no single closed object,
still has no legal walk out: the pocket a seam between two objects
forms (a statue footprint against a wall, #2359), which the strictly
per-object InsideClosedObject cannot see. The probe runs the walker's
own clip (ClipMovementPathFrom) - first as straight chords to the
escape radius, then as a bounded lattice flood - so "no exit" means
exactly what movement in play finds. Only a flood that closes (its
frontier empties inside the cell budget) seals the pose.
================
*/
func (v *WaterValidator) poseSealedInSeam(pose simulation.Spawn) bool {
	pose = simulation.NormalizeSpawnFrame(pose)
	if v.surfaceForRegion(pose.RegionID) == nil {
		return false
	}
	// Fast path: one straight chord that arrives at the escape radius
	// (or leaves the region frame, itself an exit) proves a way out.
	for i := 0; i < seamProbeRays; i++ {
		angle := float64(i) * (2 * math.Pi / seamProbeRays)
		to := v.seamProbePose(pose, pose.X+math.Cos(angle)*seamEscapeRadius, pose.Z+math.Sin(angle)*seamEscapeRadius)
		if to == nil || v.seamStepArrives(pose, *to) {
			return false
		}
	}
	type cell struct{ x, z int }
	seen := map[cell]bool{{0, 0}: true}
	type node struct {
		at   simulation.Spawn
		cell cell
	}
	queue := []node{{pose, cell{0, 0}}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for i := 0; i < seamProbeRays; i++ {
			angle := float64(i) * (2 * math.Pi / seamProbeRays)
			next := cell{
				cur.cell.x + int(math.Round(math.Cos(angle))),
				cur.cell.z + int(math.Round(math.Sin(angle))),
			}
			if seen[next] {
				continue
			}
			to := v.seamProbePose(pose,
				pose.X+float64(next.x)*seamProbeStep, pose.Z+float64(next.z)*seamProbeStep)
			if to == nil {
				return false
			}
			if !v.seamStepArrives(cur.at, *to) {
				continue
			}
			if math.Hypot(to.X-pose.X, to.Z-pose.Z) >= seamEscapeRadius {
				return false
			}
			// Only a walked-to cell is seen: a cell one chord refuses
			// may still be reached around the obstacle's end.
			seen[next] = true
			if len(seen) > seamProbeMaxCells {
				return false
			}
			queue = append(queue, node{*to, next})
		}
	}
	return true
}

/*
================
seamProbePose

A probe goal at (x, z) in the pose's region frame, standing on the
terrain there; nil when the point leaves the frame, which the caller
counts as an exit, not a wall.
================
*/
func (v *WaterValidator) seamProbePose(pose simulation.Spawn, x, z float64) *simulation.Spawn {
	if x < 0 || z < 0 || x >= simulation.NativeRegionSize || z >= simulation.NativeRegionSize {
		return nil
	}
	to := simulation.Spawn{RegionID: pose.RegionID, X: x, Y: pose.Y, Z: z}
	if terrainY, ok := v.TerrainHeightAt(pose.RegionID, x, z); ok {
		to.Y = terrainY
	}
	return &to
}

/*
================
seamStepArrives

Whether the walk clip walks from one probe pose to the other: its rest
stands on the goal. A blocked chord rests at its contact, short of it.
================
*/
func (v *WaterValidator) seamStepArrives(from, to simulation.Spawn) bool {
	report := v.ClipMovementPathFrom(from, simulation.NavOwner{}, to)
	if report.Outcome == ClipBlocked {
		return false
	}
	dx, dz := report.Rest.X-to.X, report.Rest.Z-to.Z
	return dx*dx+dz*dz <= seamArriveEpsilon*seamArriveEpsilon
}

/*
================
ClosedObjectRescueFromEnv
================
*/
func ClosedObjectRescueFromEnv() bool {
	return switchOn(os.Getenv(EnvClosedObjectRescue))
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
