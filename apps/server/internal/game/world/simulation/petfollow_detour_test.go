/*
===========================================================================

petfollow_detour_test.go - a blocked pet takes the monster AI's detour

A pet whose straight segment toward its owner stops at a wall follows the
planner's waypoints through the gap instead of standing at the wall, and a
failed plan is retried at the monsters' pace, not every tick.

===========================================================================
*/

package simulation

import (
	"testing"
)

const (
	detourRegion = 0x62a8
	// The wall runs along x = detourWallX from z = 50 to z = 150; above it
	// is the doorway.
	detourWallX    = 110.0
	detourWallTopZ = 150.0
	detourWallLowZ = 50.0
	detourSpeed    = 100.0
	detourReach    = 2.0
)

/*
================
detourWall

The collision owner of the fixture: a segment that crosses the wall stops
one unit short of it.
================
*/
func detourWall(from, to Spawn) (Spawn, *MoveError) {
	if (from.X-detourWallX)*(to.X-detourWallX) >= 0 {
		return to, nil
	}
	t := (detourWallX - from.X) / (to.X - from.X)
	z := from.Z + (to.Z-from.Z)*t
	if z < detourWallLowZ || z > detourWallTopZ {
		return to, nil
	}
	stop := detourWallX - 1
	if to.X < from.X {
		stop = detourWallX + 1
	}
	return Spawn{RegionID: from.RegionID, X: stop, Y: to.Y, Z: from.Z + (to.Z-from.Z)*(stop-from.X)/(to.X-from.X)}, nil
}

/*
================
TestBlockedPetFollowsTheDetourThroughTheDoorway
================
*/
func TestBlockedPetFollowsTheDetourThroughTheDoorway(t *testing.T) {
	p := NewPetFollower(55, Spawn{RegionID: detourRegion, X: 100, Z: 100})
	owner := Spawn{RegionID: detourRegion, X: 130, Z: 100}
	plans := 0
	p.SetRoutePlanner(func(from, goal Spawn) []Spawn {
		plans++
		return []Spawn{
			{RegionID: detourRegion, X: 100, Z: 170},
			{RegionID: detourRegion, X: 130, Z: 170},
			goal,
		}
	})
	for now := int64(100); now <= 5000; now += 100 {
		p.Approach(owner, detourSpeed, now, detourReach, detourWall)
	}
	at := p.Position(5000)
	if d := WorldDistance2D(at, owner); d > detourReach {
		t.Fatalf("the pet stopped %.1f from its owner at %+v; it never went around the wall", d, at)
	}
	if plans != 1 {
		t.Fatalf("planned %d routes, want one route followed to its end", plans)
	}
}

/*
================
TestUnplannedPetRetriesAtTheMonsterPace
================
*/
func TestUnplannedPetRetriesAtTheMonsterPace(t *testing.T) {
	p := NewPetFollower(55, Spawn{RegionID: detourRegion, X: 100, Z: 100})
	owner := Spawn{RegionID: detourRegion, X: 130, Z: 100}
	plans := 0
	p.SetRoutePlanner(func(from, goal Spawn) []Spawn {
		plans++
		return nil
	})
	for now := int64(100); now <= 1000; now += 100 {
		p.Approach(owner, detourSpeed, now, detourReach, detourWall)
	}
	if plans != 1 {
		t.Fatalf("a failed plan ran %d times in one retry window, want 1", plans)
	}
	if at := p.Position(1000); at.X > detourWallX {
		t.Fatalf("an unrouted pet crossed the wall to %+v", at)
	}
}
