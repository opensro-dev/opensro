/*
===========================================================================

seam_rescue_test.go - tests for seam_rescue.go

===========================================================================
*/
package movement

import (
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

// gateRegion is the Jangan south gate's region (25256): the gate
// structure and its two lion statues stand here.
const gateRegion = 25256

func gatePose(t *testing.T, v *WaterValidator, x, z float64) simulation.Spawn {
	t.Helper()
	ground, ok := v.TerrainHeightAt(gateRegion, x, z)
	if !ok {
		t.Fatalf("no terrain height at the gate pose (%.0f, %.0f)", x, z)
	}
	return simulation.Spawn{RegionID: gateRegion, X: x, Y: ground, Z: z}
}

/*
================
TestSeamRescueRelocatesTheGateLionPoses

#2359: the two lion seams at the Jangan south gate. Each pose stands
on a walkable mainland tile inside no object's cells, so every legacy
test passes it; the walk nevertheless has no chord out. With the rule
on the rescue must strand it and land somewhere with an exit of its
own, keeping the angle, near the gate. With the rule off the pose
passes through untouched, as before the widening.
================
*/
func TestSeamRescueRelocatesTheGateLionPoses(t *testing.T) {
	v := realAuthorityValidator(t)
	for _, wedge := range [][2]float64{{913, 258}, {1007, 253}} {
		pose := gatePose(t, v, wedge[0], wedge[1])
		pose.Angle = 77
		if _, inside := v.InsideClosedObject(pose); inside {
			t.Fatalf("wedge %v starts enclosed; the seam test would not be what strands it", wedge)
		}
		if _, stranded, _ := v.RelocateStrandedSpawn(pose); stranded {
			t.Fatalf("with the rule off, the wedge %v must pass through", wedge)
		}
		v.SetClosedObjectRescue(true)
		if v.poseHasLegalExit(pose) {
			t.Fatalf("wedge %v has a legal exit; the seam verdict missed the pocket", wedge)
		}
		rescued, stranded, found := v.RelocateStrandedSpawn(pose)
		if !stranded || !found {
			t.Fatalf("wedge %v: stranded=%v found=%v, want both", wedge, stranded, found)
		}
		if _, stranded, _ := v.RelocateStrandedSpawn(rescued); stranded {
			t.Fatalf("the rescue point for wedge %v is itself stranded: %+v", wedge, rescued)
		}
		if !v.poseHasLegalExit(rescued) {
			t.Fatalf("the rescue point for wedge %v has no legal exit: %+v", wedge, rescued)
		}
		if dist := simulation.WorldDistance2D(pose, rescued); dist > 100 {
			t.Fatalf("wedge %v: rescue moved %.1f units, want a landing near the gate", wedge, dist)
		}
		if rescued.Angle != pose.Angle {
			t.Fatalf("rescue must keep the facing, got %d", rescued.Angle)
		}
		v.SetClosedObjectRescue(false)
	}
}

/*
================
TestSeamRescueLeavesOpenGateGroundAlone

The guard poses around the same gate: against the wall mid-road,
between the two lion footprints, and two steps south of the west
wedge. All are legal today and must stay legal with the rule on -
legal ground hugging closed geometry is where a buggy exit search
would over-fire.
================
*/
func TestSeamRescueLeavesOpenGateGroundAlone(t *testing.T) {
	v := realAuthorityValidator(t)
	v.SetClosedObjectRescue(true)
	for _, open := range [][2]float64{{960, 245}, {935, 258}, {913, 240}} {
		pose := gatePose(t, v, open[0], open[1])
		if !v.poseHasLegalExit(pose) {
			t.Fatalf("open pose %v reported no legal exit", open)
		}
		if _, stranded, _ := v.RelocateStrandedSpawn(pose); stranded {
			t.Fatalf("open pose %v stranded by the widened rescue", open)
		}
	}
}

/*
================
TestSeamRescueGatehouseGrid

The blast-radius map of the widening: every pose on a 2-unit grid
over the gatehouse, classified with the rule off (today's verdict)
and on. A pose legal today may flip only inside the two lion seam
pockets, and both wedge poses must flip. Everything else - the road,
the gate passage, the ground against both walls - stays legal.
================
*/
func TestSeamRescueGatehouseGrid(t *testing.T) {
	v := realAuthorityValidator(t)
	wedges := [][2]float64{{913, 258}, {1007, 253}}
	nearWedge := func(x, z float64) bool {
		for _, w := range wedges {
			dx, dz := x-w[0], z-w[1]
			if dx*dx+dz*dz <= 12*12 {
				return true
			}
		}
		return false
	}
	flipped := 0
	for _, w := range wedges {
		if !nearWedge(w[0], w[1]) {
			t.Fatal("wedge outside its own pocket radius")
		}
	}
	for x := 860.0; x <= 1070; x += 2 {
		for z := 230.0; z <= 340; z += 2 {
			ground, ok := v.TerrainHeightAt(gateRegion, x, z)
			if !ok {
				continue
			}
			pose := simulation.Spawn{RegionID: gateRegion, X: x, Y: ground, Z: z}
			_, strandedToday, _ := v.RelocateStrandedSpawn(pose)
			v.SetClosedObjectRescue(true)
			_, strandedNow, _ := v.RelocateStrandedSpawn(pose)
			v.SetClosedObjectRescue(false)
			if strandedToday || !strandedNow {
				continue
			}
			flipped++
			if !nearWedge(x, z) {
				t.Fatalf("pose (%.0f, %.0f) flipped to stranded outside the lion pockets", x, z)
			}
		}
	}
	for _, w := range wedges {
		pose := gatePose(t, v, w[0], w[1])
		v.SetClosedObjectRescue(true)
		_, stranded, _ := v.RelocateStrandedSpawn(pose)
		v.SetClosedObjectRescue(false)
		if !stranded {
			t.Fatalf("wedge %v did not flip to stranded", w)
		}
	}
	if flipped == 0 {
		t.Fatal("no grid pose flipped; the widening sees nothing at the gatehouse")
	}
	t.Logf("gatehouse grid: %d poses flipped to stranded, all inside the lion pockets", flipped)
}
