/*
===========================================================================

dungeon_cave_regression_test.go - Donwhang Stone Cave (0x8001) ramp walks

Live reports BR-261015-0653-7758 and BR-261015-0700-482F: inside the cave
the server kept the ramp-top height while the client descended, so later
clips and line-of-sight checks started over 2 m off the floor and refused
both walking and attacks ("Cannot attack due to an obstacle"), and a long
chord down the single-cell ramp failed its one midpoint floor sample.

===========================================================================
*/
package movement

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

const donwhangCaveRegion = 0x8001

/*
================
donwhangCaveSpawn
================
*/
func donwhangCaveSpawn(x, y, z float64) simulation.Spawn {
	return simulation.Spawn{RegionID: donwhangCaveRegion, X: x, Y: y, Z: z}
}

/*
================
TestDonwhangCaveRampWalkKeepsServerHeightOnTheFloor

Ground steps down the ramp south of (629, 74), starting from the stale
ramp-top height the server used to retain: every applied step must stand
on the floor it reached, ending on the cave floor at -9.984.
================
*/
func TestDonwhangCaveRampWalkKeepsServerHeightOnTheFloor(t *testing.T) {
	v := realAuthorityValidator(t)
	v.dungeonSpawnOnce.Do(func() { v.dungeonSpawnSurfaces = v.loadDungeonSpawnSurfaces() })
	surface := v.dungeonSpawnSurfaces[donwhangCaveRegion]
	if surface == nil {
		t.Fatal("fixture: no Donwhang Cave surface")
	}
	clip := &ClientClip{Mode: ClipApply, Validator: v}
	pose := donwhangCaveSpawn(629, -0.150344, 74)
	owner := simulation.NavOwner{}
	for i := 0; i < 10; i++ {
		to := donwhangCaveSpawn(pose.X+0.1, pose.Y, pose.Z+5)
		next, nextOwner, blocked := clip.processOwnedStep("cave-ramp", pose, owner, to)
		if blocked {
			t.Fatalf("step %d from %+v blocked, want a walk down the ramp", i, pose)
		}
		floor, ok := surface.heightAt(next.X, next.Y, next.Z)
		if !ok || math.Abs(floor-next.Y) > 0.01 {
			t.Fatalf("step %d stands at %+v, floor is %.3f (found %v)", i, next, floor, ok)
		}
		pose, owner = next, nextOwner
	}
	if math.Abs(pose.Y-(-9.984)) > 0.01 {
		t.Fatalf("walk ends at %+v, want the cave floor at -9.984", pose)
	}
}

/*
================
TestDonwhangCaveRampClipArrivesAtFloorHeight

One long chord down the ramp (a click, or a line-of-sight check between
two cave floors) arrives at the destination floor height even though the
chord's own Y is 0 and the ramp drops 7.7 m over its length.
================
*/
func TestDonwhangCaveRampClipArrivesAtFloorHeight(t *testing.T) {
	v := realAuthorityValidator(t)
	for _, leg := range [][2]simulation.Spawn{
		{donwhangCaveSpawn(629, -2.220184, 74), donwhangCaveSpawn(630, 0, 125)},
		{donwhangCaveSpawn(629, -2.220184, 74), donwhangCaveSpawn(621, 0, 131)},
		{donwhangCaveSpawn(620, 0, 60), donwhangCaveSpawn(650, 0, 110)},
	} {
		got := v.clipDungeonPath(leg[0], leg[1])
		if got.Outcome != ClipArrived {
			t.Fatalf("clip %+v -> %+v: %s, want arrived", leg[0], leg[1], got.Outcome)
		}
		if math.Abs(got.Rest.Y-(-9.984)) > 0.01 {
			t.Fatalf("clip %+v -> %+v rests at %+v, want the cave floor at -9.984", leg[0], leg[1], got.Rest)
		}
	}
}

/*
================
TestDonwhangCaveUpperFloorDoesNotAdmitCorridorLeg

North of (993, -307) the entrance corridor's floor ends and only an upper
floor (140 m) covers the point: the leg must stop at the corridor end on
its own floor, not arrive on the upper floor.
================
*/
func TestDonwhangCaveUpperFloorDoesNotAdmitCorridorLeg(t *testing.T) {
	v := realAuthorityValidator(t)
	got := v.clipDungeonPath(donwhangCaveSpawn(998.3, 0, -471.2), donwhangCaveSpawn(963.4, 0, 528.1))
	if got.Outcome != ClipBlocked {
		t.Fatalf("corridor leg: %s at %+v, want blocked at the floor end", got.Outcome, got.Rest)
	}
	if math.Abs(got.Rest.Y) > 0.5 || math.Abs(got.Rest.Z-(-307.1)) > 1 {
		t.Fatalf("corridor leg rests at %+v, want the corridor end near (993, 0, -307)", got.Rest)
	}
}

/*
================
TestDonwhangCaveFloorEndStepStaysOnWalkedFloor

West of the corridor end the floor-1 strip along z ~ -307 stops and
only the upper floor (140 m) covers the ground past it: a step that
ends past the floor end must stop on its own floor, never arrive on
the upper floor. This pins the outcome, not one branch: the cave's
floor ends are also sealed by wall-object contact (disabling the
endpoint guard in clipDungeonPath changes no result here - no
branch-isolating geometry was found in the cave's scanned area), and
the guard remains as the endpoint check for a floor end a wall does
not seal (pinned synthetically in
TestDungeonClipEndpointGuardRefusesUpperFloor below).
================
*/
func TestDonwhangCaveFloorEndStepStaysOnWalkedFloor(t *testing.T) {
	v := realAuthorityValidator(t)
	got := v.clipDungeonPath(donwhangCaveSpawn(986.5, 0, -308.9), donwhangCaveSpawn(986.5, 0, -307.0))
	if got.Outcome != ClipBlocked {
		t.Fatalf("endpoint step: %s at %+v, want blocked at the floor end", got.Outcome, got.Rest)
	}
	if math.Abs(got.Rest.Y) > 0.5 {
		t.Fatalf("endpoint step rests at %+v, want the walked floor near Y 0, not the upper floor", got.Rest)
	}
}

/*
================
TestDungeonClipEndpointGuardRefusesUpperFloor

Synthetic two-floor surface (no resolved objects, so no cuts or
contacts): the lower floor ends at z 0.5 and only the 140 m upper
floor covers the ground past it. The step (50,0,-6) -> (50,0,1) puts
every floor-walk sample on the lower floor - the last one at z 0.125 -
so the walk arrives at the endpoint check with curY 0, and only that
check keeps the step from arriving on the upper floor. Disabling the
endpoint guard in clipDungeonPath turns this into a ClipArrived at
Y 140; the real cave has no branch-isolating geometry for it (see
TestDonwhangCaveFloorEndStepStaysOnWalkedFloor above).
================
*/
func TestDungeonClipEndpointGuardRefusesUpperFloor(t *testing.T) {
	lower := &objectNavMesh{
		vertices: []float32{0, 0, -100, 100, 0, -100, 100, 0, 0.5, 0, 0, 0.5},
		cellA:    []uint16{0, 0}, cellB: []uint16{1, 2}, cellC: []uint16{2, 3},
		minX: 0, maxX: 100, minY: 0, maxY: 0, minZ: -100, maxZ: 0.5,
	}
	upper := &objectNavMesh{
		vertices: []float32{0, 140, 0.5, 100, 140, 0.5, 100, 140, 100, 0, 140, 100},
		cellA:    []uint16{0, 0}, cellB: []uint16{1, 2}, cellC: []uint16{2, 3},
		minX: 0, maxX: 100, minY: 140, maxY: 140, minZ: 0.5, maxZ: 100,
	}
	lower.buildCellBoxes()
	upper.buildCellBoxes()
	surface := &dungeonSpawnSurface{blocks: []dungeonSpawnBlock{
		{meshes: []*objectNavMesh{lower}, cosYaw: 1},
		{meshes: []*objectNavMesh{upper}, cosYaw: 1},
	}}
	v := &WaterValidator{dungeonSpawnSurfaces: map[uint16]*dungeonSpawnSurface{donwhangCaveRegion: surface}}
	v.dungeonSpawnOnce.Do(func() {})
	got := v.clipDungeonPath(donwhangCaveSpawn(50, 0, -6), donwhangCaveSpawn(50, 0, 1))
	if got.Outcome != ClipBlocked {
		t.Fatalf("endpoint guard: %s at %+v, want blocked at the floor end", got.Outcome, got.Rest)
	}
	if math.Abs(got.Rest.Y) > 0.5 || math.Abs(got.Rest.Z-(-6)) > 0.01 {
		t.Fatalf("endpoint guard rests at %+v, want the walked start (50, 0, -6)", got.Rest)
	}
}

/*
================
TestDonwhangCaveInBoulderBlocksAsNative

Report BR-261015-0700-482F ("Cannot walk, edge is blocking here"): every
click east of (1193, -336) is refused. The server is native there. Block
4 places a cave-in boulder pile, a DOF flags&2 collision object
(out_obj_stone, radius squared 1962.5, r 44.3, centred at (1237.45,
-338.12)), and the native dungeon move clips it as a 2D XZ circle
(CRTNavMeshDungeonBlock_ClipObstacleCircles 998440; obstacle.y is never
read). The player stood 0.2 outside its rim, so walking into the pile
stops at once and walking away does not. The wall was invisible only
because the client did not draw DOF block objects (0xAD1B70); the takeover
of #1251 draws them.
================
*/
func TestDonwhangCaveInBoulderBlocksAsNative(t *testing.T) {
	v := realAuthorityValidator(t)
	from := donwhangCaveSpawn(1193, 0.013342, -336)
	into := v.ClipMovementPath(from, donwhangCaveSpawn(1236, 0, -341))
	if into.Outcome != ClipBlocked || math.Abs(into.Rest.X-from.X) > 0.5 {
		t.Fatalf("into the boulder pile: %v at x %.3f, want blocked at the rim near x %.0f", into.Outcome, into.Rest.X, from.X)
	}
	away := v.ClipMovementPath(from, donwhangCaveSpawn(1153, 0, -336))
	if away.Outcome != ClipArrived {
		t.Fatalf("away from the boulder pile: %v at %+v, want arrived", away.Outcome, away.Rest)
	}
}

/*
================
TestDonwhangCaveSeamStepStaysOnGroundFloor

BR-261020-1126-03CF: a ground step across x 0 near z -6.5 crosses the
passage between two ground-floor blocks, where a hairline gap leaves only
the floors at 140, 280 and 420 under one sample. The step must arrive on
the ground floor, not climb to 280.
================
*/
func TestDonwhangCaveSeamStepStaysOnGroundFloor(t *testing.T) {
	v := realAuthorityValidator(t)
	got := v.clipDungeonPath(donwhangCaveSpawn(2.785, 0, -6.629), donwhangCaveSpawn(-2.212, 0, -6.462))
	if got.Outcome != ClipArrived || math.Abs(got.Rest.Y) > 0.5 {
		t.Fatalf("seam step: %s at %+v, want arrived on the ground floor", got.Outcome, got.Rest)
	}
}

/*
================
TestDonwhangCaveReportedWalkKeepsGroundFloor

The reported walk itself, stepped as the server walker steps it: from
(87.7, 0, -9.5) to (-76, 0, -4) every step stays on the ground floor.
================
*/
func TestDonwhangCaveReportedWalkKeepsGroundFloor(t *testing.T) {
	v := realAuthorityValidator(t)
	clip := &ClientClip{Mode: ClipApply, Validator: v}
	pose, owner := donwhangCaveSpawn(87.738, 0, -9.463), simulation.NavOwner{}
	goal := donwhangCaveSpawn(-76, 0, -4)
	for i := 0; i < 40; i++ {
		dx, dz := goal.X-pose.X, goal.Z-pose.Z
		length := math.Hypot(dx, dz)
		if length < 0.5 {
			break
		}
		scale := math.Min(1, 5/length)
		next, nextOwner, blocked := clip.processOwnedStep("cave-seam", pose, owner, donwhangCaveSpawn(pose.X+dx*scale, pose.Y, pose.Z+dz*scale))
		if blocked || math.Abs(next.Y) > 0.5 {
			t.Fatalf("step %d from %+v -> %+v (blocked %v), want the ground floor", i, pose, next, blocked)
		}
		pose, owner = next, nextOwner
	}
	if math.Hypot(goal.X-pose.X, goal.Z-pose.Z) > 0.5 {
		t.Fatalf("walk ends at %+v, want %+v", pose, goal)
	}
}
