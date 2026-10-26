/*
===========================================================================

dungeon_ramp_doorway_test.go - Donwhang's two ramp doorways (#1731)

From the cell next to a ramp, the doorways from Donwhang block 11 to
block 73 and from block 87 to block 149 refuse the first step outright:
blocked with result bit 0 and no movement, although the step's endpoint
is still short of the doorway edge. The original walks doorways by link
(block and cell), not by height, so both crossings are native-legal from
each side.

This test is the re-test harness for the cell-edge walk (#1728). It pins
today's measured behaviour exactly:

  - 11 -> 73 and 87 -> 149 (the y-0 side): step 0 refuses, bit 0, rest
    at the start. Any change to that signature fails this test.
  - 73 -> 11 and 149 -> 87: the walk crosses today and must keep
    crossing; the cell-edge walk may not break it.

When #1728 lands, the two pinned walks either cross (this test passes
unchanged and #1731 closes with it) or keep refusing (the pin holds, and
the failing step log says where the new walk still stops). Anything in
between goes red here first.

===========================================================================
*/
package movement

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// rampDoorwayPairs are the two Donwhang ramp doorways, by block ordinal:
// the y-0 block whose cell next to the ramp holds the doorway, and the
// block on the ramp's other level (placement y -24) it links to.
var rampDoorwayPairs = [][2]int{{11, 73}, {87, 149}}

/*
================
rampDoorwayWorld

A mesh-local point in a resolved block's world frame.
================
*/
func rampDoorwayWorld(obj resolvedObjectNav, lx, ly, lz float64) simulation.Spawn {
	p := obj.placement
	return simulation.Spawn{
		RegionID: donwhangCaveRegion,
		X:        obj.cosYaw*lx - obj.sinYaw*lz + p.x,
		Y:        ly + p.y,
		Z:        obj.sinYaw*lx + obj.cosYaw*lz + p.z,
	}
}

/*
================
rampDoorwayCellCentroid

The centre of one cell of a block's mesh, in the world frame.
================
*/
func rampDoorwayCellCentroid(obj resolvedObjectNav, m *objectNavMesh, cell int) simulation.Spawn {
	ia, ib, ic := m.cellA[cell], m.cellB[cell], m.cellC[cell]
	lx := (float64(m.vertices[ia*3]) + float64(m.vertices[ib*3]) + float64(m.vertices[ic*3])) / 3
	ly := (float64(m.vertices[ia*3+1]) + float64(m.vertices[ib*3+1]) + float64(m.vertices[ic*3+1])) / 3
	lz := (float64(m.vertices[ia*3+2]) + float64(m.vertices[ib*3+2]) + float64(m.vertices[ic*3+2])) / 3
	return rampDoorwayWorld(obj, lx, ly, lz)
}

/*
================
rampDoorwayCells

The standing points of one doorway walk: the centroid of the doorway
edge's own cell on the from-block, and the centroid of the landing-side
cell the far block's link back names. The walk runs from the cell next
to the ramp, as a mover standing in that cell steps it.
================
*/
func rampDoorwayCells(t *testing.T, surface *dungeonSpawnSurface, fromOrdinal, toOrdinal int) (from, to simulation.Spawn) {
	t.Helper()
	byOrdinal := map[int]resolvedObjectNav{}
	for _, obj := range surface.objects {
		byOrdinal[obj.placement.ordinal] = obj
	}
	src, ok := byOrdinal[fromOrdinal]
	if !ok || len(src.meshes) != 1 {
		t.Fatalf("fixture: Donwhang block %d unresolved", fromOrdinal)
	}
	dst, ok := byOrdinal[toOrdinal]
	if !ok || len(dst.meshes) != 1 {
		t.Fatalf("fixture: Donwhang block %d unresolved", toOrdinal)
	}
	var fromCell, toCell int = -1, -1
	for _, link := range src.placement.links {
		if link.target == toOrdinal {
			fromCell = int(src.meshes[0].outline.srcCell[link.edge])
		}
	}
	for _, link := range dst.placement.links {
		if link.target == fromOrdinal {
			toCell = int(dst.meshes[0].outline.srcCell[link.edge])
		}
	}
	if fromCell < 0 || toCell < 0 {
		t.Fatalf("fixture: doorway %d<->%d no longer resolves (cells %d, %d)", fromOrdinal, toOrdinal, fromCell, toCell)
	}
	return rampDoorwayCellCentroid(src, src.meshes[0], fromCell),
		rampDoorwayCellCentroid(dst, dst.meshes[0], toCell)
}

/*
================
rampDoorwayStep

One stepped leg of a doorway walk, kept for the failure log.
================
*/
type rampDoorwayStep struct {
	from, to simulation.Spawn
	got      ClipReport
}

/*
================
rampDoorwayWalk

Steps from one doorway cell's centroid to the other's in legs of at
most 5 units through the clip, chaining each leg's rest, exactly as a
mover walks it. Stops at the first leg that does not arrive.
================
*/
func rampDoorwayWalk(v *WaterValidator, surface *dungeonSpawnSurface, from, to simulation.Spawn) (steps []rampDoorwayStep, arrived bool) {
	pose := from
	for i := 0; i < 24; i++ {
		dx, dz := to.X-pose.X, to.Z-pose.Z
		if math.Hypot(dx, dz) < 0.5 {
			return steps, true
		}
		scale := math.Min(1, 5/math.Hypot(dx, dz))
		next := simulation.Spawn{RegionID: donwhangCaveRegion, X: pose.X + dx*scale, Y: pose.Y, Z: pose.Z + dz*scale}
		got := v.clipDungeonPath(pose, next)
		steps = append(steps, rampDoorwayStep{from: pose, to: next, got: got})
		if got.Outcome != ClipArrived {
			return steps, false
		}
		// Every rest stands on the floor it reached.
		floor, ok := surface.heightAt(got.Rest.X, got.Rest.Y, got.Rest.Z)
		if !ok || math.Abs(floor-got.Rest.Y) > 0.05 {
			return steps, false
		}
		pose = simulation.Spawn{RegionID: donwhangCaveRegion, X: got.Rest.X, Y: got.Rest.Y, Z: got.Rest.Z}
	}
	return steps, false
}

/*
================
rampDoorwayPinned

Whether a failed walk is exactly today's pinned refusal: the first
step blocked with result bit 0 and the rest at the start.
================
*/
func rampDoorwayPinned(steps []rampDoorwayStep) bool {
	if len(steps) != 1 {
		return false
	}
	got := steps[0].got
	return got.Outcome == ClipBlocked &&
		got.NativeResult&monster.NavResultClipped != 0 &&
		got.Rest.X == steps[0].from.X && got.Rest.Z == steps[0].from.Z
}

/*
================
TestDonwhangCaveRampDoorwaysCross

The native route crosses both ramp doorways, from each side. Today's
clip crosses from the ramp's lower level and refuses from the y-0 cell
next to each ramp; the refusal is pinned step for step until the
cell-edge walk (#1728) frees it. See the file banner.
================
*/
func TestDonwhangCaveRampDoorwaysCross(t *testing.T) {
	v := realAuthorityValidator(t)
	v.dungeonSpawnOnce.Do(func() { v.dungeonSpawnSurfaces = v.loadDungeonSpawnSurfaces() })
	surface := v.dungeonSpawnSurfaces[donwhangCaveRegion]
	if surface == nil {
		t.Fatal("fixture: no Donwhang Cave surface")
	}
	for _, pair := range rampDoorwayPairs {
		for _, dir := range [][2]int{pair, {pair[1], pair[0]}} {
			from, to := rampDoorwayCells(t, surface, dir[0], dir[1])
			steps, arrived := rampDoorwayWalk(v, surface, from, to)
			// The two y-0 walks (11 -> 73, 87 -> 149) are the pinned
			// refusals; the reverse walks cross today.
			pinned := dir[0] == pair[0]
			switch {
			case arrived:
				last := steps[len(steps)-1].got.Rest
				if math.Hypot(last.X-to.X, last.Z-to.Z) > 1 || math.Abs(last.Y-to.Y) > 0.5 {
					t.Errorf("doorway %d -> %d: arrived at %+v, want the far cell %+v", dir[0], dir[1], last, to)
				}
			case pinned && rampDoorwayPinned(steps):
				t.Logf("doorway %d -> %d: pinned refusal (step 0, bit 0, no movement) - #1728 frees this", dir[0], dir[1])
			default:
				for i, step := range steps {
					t.Errorf("doorway %d -> %d step %d: (%0.2f, %0.2f) -> (%0.2f, %0.2f): %s result %#x rest %+v",
						dir[0], dir[1], i, step.from.X, step.from.Z, step.to.X, step.to.Z,
						step.got.Outcome, uint32(step.got.NativeResult), step.got.Rest)
				}
			}
		}
	}
}
