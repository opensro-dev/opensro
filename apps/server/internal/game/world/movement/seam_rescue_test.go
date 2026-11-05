/*
===========================================================================

seam_rescue_test.go - tests for seam_rescue.go

The gate-lion poses are the live incident of post-cut/stuck-lion-trace.md;
the sweeps are the false-positive net of post-cut/stuck-lion-plan.md.

===========================================================================
*/
package movement

import (
	"math"
	"math/rand"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
seamGroundPose

A pose on the terrain at (x, z) in region, at the height entry would
stand it at.
================
*/
func seamGroundPose(t *testing.T, v *WaterValidator, region uint16, x, z float64) simulation.Spawn {
	t.Helper()
	pose, ok := seamGroundPoseOK(v, region, x, z)
	if !ok {
		t.Fatalf("no terrain height at 0x%04X (%.1f, %.1f)", region, x, z)
	}
	return pose
}

/*
================
seamGroundPoseOK

seamGroundPose for sweeps, where a pose without terrain is a skip,
not a failure.
================
*/
func seamGroundPoseOK(v *WaterValidator, region uint16, x, z float64) (simulation.Spawn, bool) {
	if x < 20 || x > 1900 || z < 20 || z > 1900 {
		return simulation.Spawn{}, false
	}
	ground, ok := v.TerrainHeightAt(region, x, z)
	if !ok {
		return simulation.Spawn{}, false
	}
	return simulation.Spawn{RegionID: region, X: x, Y: ground, Z: z}, true
}

/*
================
seamClosedNearCount

How many closed-mesh placements (linkless, at least one fully
blocking outline) have a reach box within `within` units of the pose.
A seam pocket is a two-body trap, so the negative sweeps keep only
poses with at most one closed neighbour: single-object ground is what
they guard, while two-body pockets are pinned by name.
================
*/
func seamClosedNearCount(v *WaterValidator, pose simulation.Spawn, within float64) int {
	surface := v.surfaceForRegion(pose.RegionID)
	if surface == nil {
		return 0
	}
	count := 0
	for dz := -1; dz <= 1; dz++ {
		for dx := -1; dx <= 1; dx++ {
			set := v.objectNavSetForOffset(surface, dx, dz)
			anchorX := simulation.SectorX(surface.seedRegionID) + dx
			anchorZ := simulation.SectorY(surface.seedRegionID) + dz
			localX := pose.X + float64(simulation.SectorX(pose.RegionID)-anchorX)*surface.regionSize
			localZ := pose.Z + float64(simulation.SectorY(pose.RegionID)-anchorZ)*surface.regionSize
			for i := range set {
				object := &set[i]
				if object.placement.linkEdgeCount != 0 {
					continue
				}
				closed := false
				for _, mesh := range object.meshes {
					closed = closed || closedMesh(mesh)
				}
				if !closed {
					continue
				}
				reach := object.reach
				if !reach.known {
					count++
					continue
				}
				gapX := math.Max(reach.minX-localX, math.Max(0, localX-reach.maxX))
				gapZ := math.Max(reach.minZ-localZ, math.Max(0, localZ-reach.maxZ))
				if math.Hypot(gapX, gapZ) <= within {
					count++
				}
			}
		}
	}
	return count
}

/*
================
seamGrid

The tilesPerAxis/tileSize RelocateStrandedSpawn resolves for a region,
so tests can call the exit search with the rescue's own grid.
================
*/
func seamGrid(t *testing.T, v *WaterValidator, region uint16) (int, float64) {
	t.Helper()
	surface := v.surfaceForRegion(region)
	if surface == nil {
		t.Fatalf("no surface for region 0x%04X", region)
	}
	grid := surface.blockedByOffset[offsetKey(
		simulation.SectorX(region)-simulation.SectorX(surface.seedRegionID),
		simulation.SectorY(region)-simulation.SectorY(surface.seedRegionID),
	)]
	if grid == nil || grid.tileSize <= 0 {
		t.Fatalf("no blocked grid for region 0x%04X", region)
	}
	return grid.tilesPerAxis, grid.tileSize
}

/*
================
seamTodayLegal

Whether today's verdicts (everything except the closed-object
extension) call a pose legal: a surface and grid exist, the pose's
tile is walkable mainland, and it stands on no sealed deck. The sweep
keep-filter: poses passing this are the ones the seam branch must
never strand.
================
*/
func seamTodayLegal(v *WaterValidator, pose simulation.Spawn, verdicts map[uint64]bool) bool {
	surface := v.surfaceForRegion(pose.RegionID)
	if surface == nil {
		return false
	}
	grid := surface.blockedByOffset[offsetKey(
		simulation.SectorX(pose.RegionID)-simulation.SectorX(surface.seedRegionID),
		simulation.SectorY(pose.RegionID)-simulation.SectorY(surface.seedRegionID),
	)]
	if grid == nil || grid.tileSize <= 0 {
		return false
	}
	origin := seamTileOf(pose, grid.tilesPerAxis, grid.tileSize)
	walkable, ok := v.globalTileWalkable(origin, grid.tilesPerAxis)
	if !ok || !walkable {
		return false
	}
	if !v.tileOnMainland(origin, grid.tilesPerAxis, verdicts) {
		return false
	}
	baseX := pose.X + float64(simulation.SectorX(pose.RegionID)-simulation.SectorX(surface.seedRegionID))*surface.regionSize
	baseZ := pose.Z + float64(simulation.SectorY(pose.RegionID)-simulation.SectorY(surface.seedRegionID))*surface.regionSize
	return v.spawnObjectDeckVerdict(surface, baseX, baseZ, pose.Y) != objectDeckSealed
}

/*
================
TestSeamRescueFreesTheGateLionPocket

The live incident: region 25256 (913, 258), wedged between the west
gate lion's footprint (asset 87: a closed mesh the pose is outside of)
and the gate wall. InsideClosedObject is blind to it; the widened
verdict must strand it, rescue it nearby, and land it on a stand that
itself has an exit.
================
*/
func TestSeamRescueFreesTheGateLionPocket(t *testing.T) {
	v := realAuthorityValidator(t)
	v.SetClosedObjectRescue(true)
	pose := seamGroundPose(t, v, 25256, 913, 258)
	pose.Angle = 7
	if asset, inside := v.InsideClosedObject(pose); inside {
		t.Fatalf("precondition: the seam pose must be invisible to InsideClosedObject, got asset %d", asset)
	}
	tilesPerAxis, tileSize := seamGrid(t, v, pose.RegionID)
	if v.poseHasLegalExit(pose, tilesPerAxis, tileSize, map[uint64]bool{}) {
		t.Fatal("the wedged pose must have no legal exit")
	}
	rescued, stranded, found := v.RelocateStrandedSpawn(pose)
	if !stranded || !found {
		t.Fatalf("seam pose: stranded=%v found=%v, want both", stranded, found)
	}
	if dist := simulation.WorldDistance2D(pose, rescued); dist > 100 {
		t.Fatalf("rescue moved %.1f units, want the nearest legal stand (<=100)", dist)
	}
	if rescued.Angle != pose.Angle {
		t.Fatalf("rescue must keep the facing, got %d", rescued.Angle)
	}
	if !v.poseHasLegalExit(rescued, tilesPerAxis, tileSize, map[uint64]bool{}) {
		t.Fatalf("the rescue landed on a stand without an exit: %+v", rescued)
	}
	if _, again, _ := v.RelocateStrandedSpawn(rescued); again {
		t.Fatalf("the rescue point is itself stranded: %+v", rescued)
	}
}

/*
================
TestSeamRescueLeavesTheGateLionPocketAloneWhenOff

With SRO_CLOSED_OBJECT_RESCUE off the seam pose passes through, as
every pose did before the rule: the widening lives behind the flag.
================
*/
func TestSeamRescueLeavesTheGateLionPocketAloneWhenOff(t *testing.T) {
	v := realAuthorityValidator(t)
	pose := seamGroundPose(t, v, 25256, 913, 258)
	same, stranded, found := v.RelocateStrandedSpawn(pose)
	if stranded || found || same != pose {
		t.Fatalf("flag off: got (%+v, %v, %v), want the pose passed through", same, stranded, found)
	}
}

/*
================
TestSeamRescueFreesTheEastGateLionPocket

The mirror seam at the east lion (asset 87 at (1023.5, 270.6)): the
pose at (1007, 253) is inside no object's cells on a walkable tile,
with the gate structure's blocking outline 0.7-3.6 units away.
================
*/
func TestSeamRescueFreesTheEastGateLionPocket(t *testing.T) {
	v := realAuthorityValidator(t)
	v.SetClosedObjectRescue(true)
	pose := seamGroundPose(t, v, 25256, 1007, 253)
	if asset, inside := v.InsideClosedObject(pose); inside {
		t.Fatalf("precondition: the seam pose must be invisible to InsideClosedObject, got asset %d", asset)
	}
	rescued, stranded, found := v.RelocateStrandedSpawn(pose)
	if !stranded || !found {
		t.Fatalf("east seam pose: stranded=%v found=%v, want both", stranded, found)
	}
	if _, again, _ := v.RelocateStrandedSpawn(rescued); again {
		t.Fatalf("the rescue point is itself stranded: %+v", rescued)
	}
}

/*
================
TestSeamRescueLeavesLegalGateGroundAlone

The false-positive guards around the trap itself: against the gate
wall, between the lions, and two steps south of the wedged pose - all
ordinary walkable ground the widened verdict must leave alone.
================
*/
func TestSeamRescueLeavesLegalGateGroundAlone(t *testing.T) {
	v := realAuthorityValidator(t)
	v.SetClosedObjectRescue(true)
	tilesPerAxis, tileSize := seamGrid(t, v, 25256)
	for _, at := range [][2]float64{{960, 245}, {935, 258}, {913, 240}} {
		pose := seamGroundPose(t, v, 25256, at[0], at[1])
		if !v.poseHasLegalExit(pose, tilesPerAxis, tileSize, map[uint64]bool{}) {
			t.Errorf("(%v, %v): legal gate ground must have an exit", at[0], at[1])
		}
		if _, stranded, _ := v.RelocateStrandedSpawn(pose); stranded {
			t.Errorf("(%v, %v): legal gate ground must not strand", at[0], at[1])
		}
	}
}

/*
================
TestSeamExitSweepRandomLegalMainland

Sweep A of the plan: 1024 fixed-seed random poses that today's
verdicts call legal (walkable mainland, no sealed deck, outside every
closed object) sampled across the outdoor world. The widened verdict
must strand none of them.
================
*/
func TestSeamExitSweepRandomLegalMainland(t *testing.T) {
	v := realAuthorityValidator(t)
	v.SetClosedObjectRescue(true)
	rng := rand.New(rand.NewSource(2311))
	verdicts := map[uint64]bool{}
	kept := 0
	for attempts := 0; kept < 1024; attempts++ {
		if attempts > 200000 {
			t.Fatalf("only %d legal poses found in %d attempts", kept, attempts)
		}
		region := simulation.RegionIDForSectors(rng.Intn(256), rng.Intn(256))
		if simulation.PlaneKindOf(region) == simulation.PlaneCave {
			continue
		}
		x, z := rng.Float64()*1920, rng.Float64()*1920
		ground, ok := v.TerrainHeightAt(region, x, z)
		if !ok {
			continue
		}
		pose := simulation.Spawn{RegionID: region, X: x, Y: ground, Z: z}
		if _, inside := v.InsideClosedObject(pose); inside {
			continue
		}
		if !seamTodayLegal(v, pose, verdicts) {
			continue
		}
		if seamClosedNearCount(v, pose, 20) > 1 {
			// Two closed neighbours can form a genuine seam pocket;
			// those are pinned by name, not by this negative sweep.
			continue
		}
		kept++
		tilesPerAxis, tileSize := seamGrid(t, v, region)
		if !v.poseHasLegalExit(pose, tilesPerAxis, tileSize, verdicts) {
			t.Errorf("legal mainland pose %+v has no exit under the widened verdict", pose)
		}
	}
}

/*
================
TestSeamExitSweepAroundClosedObjects

Sweep B of the plan: ring poses hugging every closed-mesh placement
(linkless, fully blocking outline) in the region entries the existing
real-data tests already pin - Jangan's gate and walkway regions,
Constantinople's harbor, Hotan's palace. Legal ring poses are the
walls/fences/rails guard in bulk.
================
*/
func TestSeamExitSweepAroundClosedObjects(t *testing.T) {
	v := realAuthorityValidator(t)
	v.SetClosedObjectRescue(true)
	verdicts := map[uint64]bool{}
	kept := 0
	for _, region := range []uint16{25256, 0x61A7, 0x6850, 0x5E87} {
		surface := v.surfaceForRegion(region)
		if surface == nil {
			t.Fatalf("no surface for region 0x%04X", region)
		}
		tilesPerAxis, tileSize := seamGrid(t, v, region)
		for _, object := range v.objectNavSetForOffset(surface, 0, 0) {
			if object.placement.linkEdgeCount != 0 || len(object.meshes) == 0 {
				continue
			}
			closed := false
			for _, mesh := range object.meshes {
				closed = closed || closedMesh(mesh)
			}
			if !closed {
				continue
			}
			for _, ring := range []float64{1.5, 3, 6} {
				for k := 0; k < 8; k++ {
					ang := float64(k) * math.Pi / 4
					pose, ok := seamGroundPoseOK(v, region,
						object.placement.x+math.Cos(ang)*(objectReachHalf(object, true)+ring),
						object.placement.z+math.Sin(ang)*(objectReachHalf(object, false)+ring))
					if !ok {
						continue
					}
					if _, inside := v.InsideClosedObject(pose); inside {
						continue
					}
					if !seamTodayLegal(v, pose, verdicts) {
						continue
					}
					if seamClosedNearCount(v, pose, 20) > 1 {
						continue
					}
					kept++
					if !v.poseHasLegalExit(pose, tilesPerAxis, tileSize, verdicts) {
						t.Errorf("ring pose %+v around asset %d has no exit", pose, object.placement.assetID)
					}
				}
			}
		}
	}
	if kept == 0 {
		t.Fatal("the ring sweep kept no poses; the guard is vacuous")
	}
	t.Logf("ring sweep kept %d legal poses", kept)
}

/*
================
objectReachHalf

Half-extent of a placement's meshes along one axis, for ring sampling.
================
*/
func objectReachHalf(object resolvedObjectNav, xAxis bool) float64 {
	half := 0.0
	for _, mesh := range object.meshes {
		if xAxis {
			half = math.Max(half, math.Max(math.Abs(mesh.minX), math.Abs(mesh.maxX)))
		} else {
			half = math.Max(half, math.Max(math.Abs(mesh.minZ), math.Abs(mesh.maxZ)))
		}
	}
	return half
}

/*
================
TestSeamExitGatehouseGrid

Sweep C of the plan: a 2-unit grid over the gatehouse band. Every
pose today's verdicts call legal must keep an exit, except the poses
inside the two lion seam pockets - every flipped pose must lie within
15 units of one of the two known seam points, and both pockets must
actually flip.
================
*/
func TestSeamExitGatehouseGrid(t *testing.T) {
	v := realAuthorityValidator(t)
	v.SetClosedObjectRescue(true)
	tilesPerAxis, tileSize := seamGrid(t, v, 25256)
	verdicts := map[uint64]bool{}
	seams := [][2]float64{{913, 258}, {1007, 253}}
	flipped := 0
	for x := 860.0; x <= 1070; x += 2 {
		for z := 230.0; z <= 340; z += 2 {
			pose, ok := seamGroundPoseOK(v, 25256, x, z)
			if !ok {
				continue
			}
			if _, inside := v.InsideClosedObject(pose); inside {
				continue
			}
			if !seamTodayLegal(v, pose, verdicts) {
				continue
			}
			if v.poseHasLegalExit(pose, tilesPerAxis, tileSize, verdicts) {
				continue
			}
			flipped++
			near := false
			for _, seam := range seams {
				if math.Hypot(x-seam[0], z-seam[1]) <= 15 {
					near = true
				}
			}
			if !near {
				t.Errorf("pose (%.0f, %.0f) flipped to stranded outside the known pockets", x, z)
			}
		}
	}
	if flipped == 0 {
		t.Fatal("no grid pose flipped; the pockets are not detected")
	}
	t.Logf("gatehouse grid flipped %d poses, all inside the two pockets", flipped)
}
