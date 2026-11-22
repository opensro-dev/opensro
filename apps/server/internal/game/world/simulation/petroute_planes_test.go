/*
===========================================================================

petroute_planes_test.go - a pet routes through the plane that owns both ends

PetRouteFromPlanes asks the same plane a monster asks (the cave's region
route on a dungeon floor), and has no route across planes.

===========================================================================
*/

package simulation

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

// routeFakePlane answers NextWaypoint with a fixed two-point route; the
// rest of the interface is never called here.
type routeFakePlane struct {
	Plane
	asked int
}

func (p *routeFakePlane) NextWaypoint(from monster.Pose, _ NavOwner, goal monster.Pose) *monster.NavigationRoute {
	p.asked++
	return monster.NewNavigationRoute(goal, []monster.Pose{from, goal})
}

type routeFakePlanes struct {
	plane *routeFakePlane
	same  bool
}

func (p routeFakePlanes) For(uint16) Plane { return p.plane }
func (p routeFakePlanes) Between(a, b uint16) (Plane, bool) {
	if !p.same {
		return nil, false
	}
	return p.plane, true
}

/*
================
TestPetRoutesThroughTheOwningPlane
================
*/
func TestPetRoutesThroughTheOwningPlane(t *testing.T) {
	plane := &routeFakePlane{}
	from, goal := Spawn{RegionID: 0x8001, X: 10, Z: 10}, Spawn{RegionID: 0x8001, X: 90, Z: 10}
	route := PetRouteFromPlanes(routeFakePlanes{plane: plane, same: true})(from, goal)
	if plane.asked != 1 || len(route) != 2 || route[1].X != goal.X {
		t.Fatalf("the pet did not take the owning plane's route: asked %d, route %+v", plane.asked, route)
	}
	if route := PetRouteFromPlanes(routeFakePlanes{plane: plane})(from, goal); route != nil {
		t.Fatalf("ends no one plane owns gave a route: %+v", route)
	}
	if PetRouteFromPlanes(nil) != nil {
		t.Fatal("no planes gave a planner")
	}
}
