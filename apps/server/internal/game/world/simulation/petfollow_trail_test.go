/*
===========================================================================

petfollow_trail_test.go - a blocked follower walks its owner's trail

A trader's transport stood at a town wall where two regions meet while
the trader went out by the gate. A following transport (port-only,
SetOwnerTrail) whose straight segment meets a wall takes the owner's own
walk through the doorway, with or without a route planner; a pet
approaching a target does not, and a follower without the trail keeps
the native posture (it stands at the wall and raises the stuck event).

===========================================================================
*/

package simulation

import (
	"testing"
)

// trailTestStepMs is the follow tick of the trail tests.
const trailTestStepMs = 100

/*
================
trailTestOwnerPath

The owner's walk from beside the pet, up through the doorway above the
fixture wall (detourWall) and down the far side.
================
*/
func trailTestOwnerPath() []Spawn {
	return []Spawn{
		{RegionID: detourRegion, X: 100, Z: 100},
		{RegionID: detourRegion, X: 100, Z: 175},
		{RegionID: detourRegion, X: 160, Z: 175},
		{RegionID: detourRegion, X: 160, Z: 60},
		{RegionID: detourRegion, X: 260, Z: 60},
	}
}

/*
================
trailTestOwnerAt

Where the owner walking the path at speed stands at a time.
================
*/
func trailTestOwnerAt(path []Spawn, speed float64, ms int64) Spawn {
	left := speed * float64(ms) / 1000
	for i := 1; i < len(path); i++ {
		leg := WorldDistance2D(path[i-1], path[i])
		if left <= leg {
			t := left / leg
			return Spawn{RegionID: detourRegion, X: path[i-1].X + (path[i].X-path[i-1].X)*t,
				Z: path[i-1].Z + (path[i].Z-path[i-1].Z)*t}
		}
		left -= leg
	}
	return path[len(path)-1]
}

/*
================
TestFollowerWalksTheOwnersTrailThroughTheDoorway

The pet stands at the wall's near side; the owner walks round through the
doorway and away on the far side. Without a planner the pet follows the
owner's trail and ends in follow reach; it never crosses the wall.
================
*/
func TestFollowerWalksTheOwnersTrailThroughTheDoorway(t *testing.T) {
	for _, planned := range []bool{false, true} {
		p := NewPetFollower(55, Spawn{RegionID: detourRegion, X: 90, Z: 100})
		p.SetOwnerTrail(true)
		if planned {
			p.SetRoutePlanner(func(from, goal Spawn) []Spawn { return nil })
		}
		path := trailTestOwnerPath()
		crossed := false
		before := p.Position(0)
		for now := int64(trailTestStepMs); now <= 20000; now += trailTestStepMs {
			p.Advance(trailTestOwnerAt(path, detourSpeed/2, now), detourSpeed, now, detourWall)
			after := p.Position(now)
			if (before.X-detourWallX)*(after.X-detourWallX) < 0 {
				z := before.Z + (after.Z-before.Z)*(detourWallX-before.X)/(after.X-before.X)
				crossed = crossed || z >= detourWallLowZ && z <= detourWallTopZ
			}
			before = after
		}
		owner := path[len(path)-1]
		if d := WorldDistance2D(p.Position(20000), owner); d > PetFollowDistance+1 {
			t.Fatalf("planner %v: the pet stopped %.1f from its owner at %+v", planned, d, p.Position(20000))
		}
		if crossed {
			t.Fatalf("planner %v: the pet crossed the wall", planned)
		}
	}
}

/*
================
TestApproachTakesNoOwnerTrail

Approach (a pet going for a target, not its owner) never walks the trail
that following recorded: blocked with no planner, it stays at the wall.
================
*/
func TestApproachTakesNoOwnerTrail(t *testing.T) {
	p := NewPetFollower(55, Spawn{RegionID: detourRegion, X: 90, Z: 100})
	p.SetOwnerTrail(true)
	path := trailTestOwnerPath()
	for now := int64(trailTestStepMs); now <= 1500; now += trailTestStepMs {
		p.Advance(trailTestOwnerAt(path, detourSpeed/2, now), detourSpeed, now, detourWall)
	}
	target := Spawn{RegionID: detourRegion, X: 200, Z: 100}
	for now := int64(1600); now <= 8000; now += trailTestStepMs {
		p.Approach(target, detourSpeed, now, detourReach, detourWall)
	}
	if at := p.Position(8000); at.X > detourWallX {
		t.Fatalf("an approach took the owner's trail across the wall to %+v", at)
	}
}

/*
================
TestTrailKeepsItsSpacingAndBound
================
*/
func TestTrailKeepsItsSpacingAndBound(t *testing.T) {
	p := NewPetFollower(55, Spawn{RegionID: detourRegion, X: 0, Z: 0})
	p.SetOwnerTrail(true)
	for i := 0; i < 1000; i++ {
		p.noteOwner(Spawn{RegionID: detourRegion, X: float64(i) * 5, Z: 0})
	}
	if len(p.trail) != petTrailMax {
		t.Fatalf("trail holds %d points, want %d", len(p.trail), petTrailMax)
	}
	for i := 1; i < len(p.trail); i++ {
		if d := WorldDistance2D(p.trail[i-1], p.trail[i]); d < petTrailSpacing {
			t.Fatalf("points %d and %d are %.1f apart", i-1, i, d)
		}
	}
}

/*
================
TestFollowerWithoutTheTrailStaysNative

The default (a native follower, or a transport with SRO_TRANSPORT_FOLLOW
off): no trail is kept, the walled-in follower stands at the wall, and
its stops raise the native stuck event (petstuck.go answers it).
================
*/
func TestFollowerWithoutTheTrailStaysNative(t *testing.T) {
	p := NewPetFollower(55, Spawn{RegionID: detourRegion, X: 90, Z: 100})
	path := trailTestOwnerPath()
	stuck := false
	for now := int64(trailTestStepMs); now <= 20000; now += trailTestStepMs {
		p.Advance(trailTestOwnerAt(path, detourSpeed/2, now), detourSpeed, now, detourWall)
		stuck = stuck || p.TakeStuck()
	}
	if len(p.trail) != 0 {
		t.Fatalf("a follower without the trail kept %d points", len(p.trail))
	}
	if at := p.Position(20000); at.X > detourWallX {
		t.Fatalf("a native follower crossed to %+v", at)
	}
	if !stuck {
		t.Fatal("the walled-in follower never raised the stuck event")
	}
}

/*
================
openGround

No wall: every step lands where it aims.
================
*/
func openGround(_, to Spawn) (Spawn, *MoveError) { return to, nil }

/*
================
TestZigzaggingOwnerDoesNotLengthenTheFollow

On open ground the follower never walks the trail: the owner zigzags 40
units left and right every 40 forward, the trail is that zigzag, but the
straight segment to the owner is always clear, so no route is taken and
the follower's walk stays near the straight distance, well short of the
owner's.
================
*/
func TestZigzaggingOwnerDoesNotLengthenTheFollow(t *testing.T) {
	p := NewPetFollower(55, Spawn{RegionID: detourRegion, X: 100, Z: 100})
	p.SetOwnerTrail(true)
	path := []Spawn{{RegionID: detourRegion, X: 100, Z: 100}}
	for i := 1; i <= 10; i++ {
		side := 40.0
		if i%2 == 0 {
			side = -40
		}
		path = append(path, Spawn{RegionID: detourRegion, X: 100 + side, Z: 100 + float64(i)*40})
	}
	ownerLength := 0.0
	for i := 1; i < len(path); i++ {
		ownerLength += WorldDistance2D(path[i-1], path[i])
	}
	walked, before := 0.0, p.Position(0)
	for now := int64(trailTestStepMs); now <= 20000; now += trailTestStepMs {
		p.Advance(trailTestOwnerAt(path, detourSpeed/2, now), detourSpeed, now, openGround)
		if len(p.detour.points) > 0 {
			t.Fatalf("a route was taken on open ground at %d ms: %+v", now, p.detour.points)
		}
		after := p.Position(now)
		walked += WorldDistance2D(before, after)
		before = after
	}
	straight := WorldDistance2D(path[0], path[len(path)-1])
	t.Logf("follower walked %.0f; straight %.0f; owner walked %.0f", walked, straight, ownerLength)
	if len(p.trail) < 10 || walked > straight*1.2 || walked > ownerLength*0.75 {
		t.Fatalf("walked %.0f; straight %.0f, owner %.0f, trail %d points", walked, straight, ownerLength, len(p.trail))
	}
}

/*
================
loopWalls

The fixture wall (detourWall) and a second one along z = 160 west of
x = 90, which hides the owner's loop on the near side from below.
================
*/
func loopWalls(from, to Spawn) (Spawn, *MoveError) {
	if (from.Z-160)*(to.Z-160) < 0 {
		if x := from.X + (to.X-from.X)*(160-from.Z)/(to.Z-from.Z); x < 90 {
			return from, nil
		}
	}
	return detourWall(from, to)
}

/*
================
TestTrailRouteSkipsAheadToANewerPointInReach

The owner went up past the near side of the wall, round a loop behind a
second wall and over the doorway, and stands on the far side below it,
out of follow reach. The follower reaches the trail at its first point
(the loop is hidden from below); there the loop's last point is already
in straight reach, so it skips the loop instead of walking it. While on
the loop the owner stays out of straight reach, so only the skip can
shorten it.
================
*/
func TestTrailRouteSkipsAheadToANewerPointInReach(t *testing.T) {
	for _, skip := range []bool{true, false} {
		p := NewPetFollower(55, Spawn{RegionID: detourRegion, X: 100, Z: 100})
		p.SetOwnerTrail(true)
		p.trail = []Spawn{
			{RegionID: detourRegion, X: 100, Z: 170},
			{RegionID: detourRegion, X: 60, Z: 240},
			{RegionID: detourRegion, X: 40, Z: 250},
			{RegionID: detourRegion, X: 130, Z: 175},
		}
		owner := Spawn{RegionID: detourRegion, X: 130, Z: 10}
		walls := loopWalls
		walked, before := 0.0, p.Position(0)
		for now := int64(trailTestStepMs); now <= 15000; now += trailTestStepMs {
			if !skip {
				// The comparison run: the route's trail mark cleared each
				// step, so its points are walked in order, unskipped.
				p.detour.trail = false
			}
			p.Advance(owner, detourSpeed, now, walls)
			after := p.Position(now)
			walked += WorldDistance2D(before, after)
			before = after
		}
		t.Logf("skip %v: follower walked %.0f", skip, walked)
		if d := WorldDistance2D(p.Position(15000), owner); d > PetFollowDistance+1 {
			t.Fatalf("skip %v: the follower stopped %.0f from its owner at %+v", skip, d, p.Position(15000))
		}
		// Up to the first point (70), across to the loop's end (30), down
		// toward the owner (about 105): about 205. The loop adds about 140.
		if skip && walked > 260 || !skip && walked < 300 {
			t.Fatalf("skip %v: walked %.0f", skip, walked)
		}
	}
}
