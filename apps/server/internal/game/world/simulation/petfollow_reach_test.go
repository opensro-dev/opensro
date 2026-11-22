/*
===========================================================================

petfollow_reach_test.go - the follower says when nothing leads to its goal

PetFollower.Reach is the fact the action owner acts on (petrecovery.go):
true when the straight segment stops short and no detour leads past, false
when a route is known, in reach, or after a relocation.

===========================================================================
*/

package simulation

import "testing"

/*
================
TestBlockedWhenTheWallHasNoRoute
================
*/
func TestBlockedWhenTheWallHasNoRoute(t *testing.T) {
	p := NewPetFollower(55, Spawn{RegionID: detourRegion, X: 100, Z: 100})
	owner := Spawn{RegionID: detourRegion, X: 130, Z: 100}
	p.SetRoutePlanner(func(from, goal Spawn) []Spawn { return nil })
	p.Approach(owner, detourSpeed, 100, detourReach, detourWall)
	reach := p.Reach()
	if !reach.Blocked {
		t.Fatal("a wall with no route did not leave the follower blocked")
	}
	if reach.Reached.X != detourWallX-1 || reach.Goal.X >= owner.X {
		t.Fatalf("blocked at %+v toward %+v, want the clip point one unit before the wall, toward the stand-off short of the owner", reach.Reached, reach.Goal)
	}
}

/*
================
TestBlockedWhenEverySegmentIsRefused
================
*/
func TestBlockedWhenEverySegmentIsRefused(t *testing.T) {
	start := Spawn{RegionID: detourRegion, X: 100, Z: 100}
	p := NewPetFollower(55, start)
	refuse := func(from, _ Spawn) (Spawn, *MoveError) { return from, &MoveError{Reason: "walled in"} }
	p.Approach(Spawn{RegionID: detourRegion, X: 300, Z: 100}, detourSpeed, 100, detourReach, refuse)
	if reach := p.Reach(); !reach.Blocked || reach.Reached != start {
		t.Fatalf("a refused segment left %+v, want blocked where the pet stands", reach)
	}
}

/*
================
TestNotBlockedWhileARouteLeadsOn
================
*/
func TestNotBlockedWhileARouteLeadsOn(t *testing.T) {
	p := NewPetFollower(55, Spawn{RegionID: detourRegion, X: 100, Z: 100})
	owner := Spawn{RegionID: detourRegion, X: 130, Z: 100}
	p.SetRoutePlanner(func(from, goal Spawn) []Spawn {
		return []Spawn{{RegionID: detourRegion, X: 100, Z: 170}, {RegionID: detourRegion, X: 130, Z: 170}, goal}
	})
	for now := int64(100); now <= 5000; now += 100 {
		p.Approach(owner, detourSpeed, now, detourReach, detourWall)
		if p.Reach().Blocked {
			t.Fatalf("blocked at %d ms while the detour led around the wall", now)
		}
	}
}

/*
================
TestReachAndRelocationClearTheVerdict
================
*/
func TestReachAndRelocationClearTheVerdict(t *testing.T) {
	p := NewPetFollower(55, Spawn{RegionID: detourRegion, X: 100, Z: 100})
	owner := Spawn{RegionID: detourRegion, X: 130, Z: 100}
	p.Approach(owner, detourSpeed, 100, detourReach, detourWall)
	if !p.Reach().Blocked {
		t.Fatal("fixture: the wall did not block")
	}
	p.Displace(owner, 200)
	if p.Reach().Blocked {
		t.Fatal("a relocation kept the old blocked verdict")
	}
	p.Approach(Spawn{RegionID: detourRegion, X: 300, Z: 100}, detourSpeed, 300, detourReach, detourWall)
	p.Approach(p.Position(300), detourSpeed, 400, detourReach, detourWall)
	if p.Reach().Blocked {
		t.Fatal("a pet in reach of its goal stayed blocked")
	}
}
