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

/*
================
TestAFailedSearchSpeaksOnlyForItsGoal

opensro-cursor2's probe on #4083: a search that found no way to goal A
must not call goal B unreachable inside the retry pause, when nobody has
searched for B. B is unknown until its own search runs, and then that
search decides.
================
*/
func TestAFailedSearchSpeaksOnlyForItsGoal(t *testing.T) {
	start := Spawn{RegionID: detourRegion, X: 100, Z: 100}
	p := NewPetFollower(55, start)
	plans := 0
	p.SetRoutePlanner(func(from, goal Spawn) []Spawn {
		plans++
		if goal.X > 500 {
			return nil
		}
		return []Spawn{from, {RegionID: detourRegion, X: from.X, Z: from.Z + 50}, goal}
	})
	// Every straight segment stops one unit after it starts.
	clip := func(from, to Spawn) (Spawn, *MoveError) {
		return Spawn{RegionID: from.RegionID, X: from.X + 1, Y: from.Y, Z: from.Z}, nil
	}
	p.Approach(Spawn{RegionID: detourRegion, X: 900, Z: 100}, detourSpeed, 100, detourReach, clip)
	if !p.Reach().Blocked || plans != 1 {
		t.Fatalf("fixture: goal A was not searched and found blocked (plans %d, %+v)", plans, p.Reach())
	}
	p.Displace(start, 150)
	p.Approach(Spawn{RegionID: detourRegion, X: 900, Z: 100}, detourSpeed, 160, detourReach, clip)
	p.Approach(Spawn{RegionID: detourRegion, X: 300, Z: 100}, detourSpeed, 200, detourReach, clip)
	if reach := p.Reach(); reach.Blocked {
		t.Fatalf("goal B, never searched, was called unreachable from goal A's search: %+v (plans %d)", reach, plans)
	}
}
