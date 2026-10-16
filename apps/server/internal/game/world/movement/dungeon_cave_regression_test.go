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
TestDonwhangCaveUpperFloorDoesNotAdmitEndpointStep

West of the corridor end the floor-1 strip along z ~ -307 stops and
only the upper floor (140 m) covers the ground past it: a step that
ends past the floor end must stop on its own floor, never arrive on
the upper floor.
================
*/
func TestDonwhangCaveUpperFloorDoesNotAdmitEndpointStep(t *testing.T) {
	v := realAuthorityValidator(t)
	got := v.clipDungeonPath(donwhangCaveSpawn(986.5, 0, -308.9), donwhangCaveSpawn(986.5, 0, -307.0))
	if got.Outcome != ClipBlocked {
		t.Fatalf("endpoint step: %s at %+v, want blocked at the floor end", got.Outcome, got.Rest)
	}
	if math.Abs(got.Rest.Y) > 0.5 {
		t.Fatalf("endpoint step rests at %+v, want the walked floor near Y 0, not the upper floor", got.Rest)
	}
}
