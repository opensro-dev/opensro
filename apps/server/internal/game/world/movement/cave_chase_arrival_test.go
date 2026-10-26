/*
===========================================================================

cave_chase_arrival_test.go - cave chases walk Donwhang (#1732, #1742)

A chasing monster plans from the cell it retains, walks the plan, and plans
again from where it stands. These walks pin how far that gets under each
setting of the cave chase switches (cave_chase.go): over 60 seeded pairs
each setting arrives at least as often as measured when #1742 landed, and
the original's line trap at block 134 stays the original's alone.

===========================================================================
*/
package movement

import (
	"math"
	"math/rand"
	"testing"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/tier"
)

const (
	// caveWalkStepBudget bounds one walk's plans; a walk still going after
	// this many steps is pacing, not arriving.
	caveWalkStepBudget = 3000
	// caveArrivalPairs is the seeded pair count, and the floors are what
	// each setting arrived at when #1742 landed.
	caveArrivalPairs             = 60
	caveNativeArrivalFloor       = 49
	caveNativeEscapeArrivalFloor = 53
	cavePortalArrivalFloor       = 55
	caveModernArrivalFloor       = 59
	caveArrivalSeed              = 1698
)

/*
================
walkCaveChase

Walks a monster from -> goal on the cave plane as the runtime does: plan
from the retained owner (fromOwner at the start), walk the route's waypoints in order while each
arrives, carry the reached owner, replan. Returns whether it arrived
within the step budget.
================
*/
func walkCaveChase(v *WaterValidator, from monster.Pose, fromOwner simulation.NavOwner, goal monster.Pose) bool {
	at, owner := from, fromOwner
	for step := 0; step < caveWalkStepBudget; step++ {
		route := v.planCaveRouteFrom(at, owner, goal)
		if route.Status() != monster.NavigationRouteReady || route.Len() == 0 {
			return false
		}
		for i := 0; i < route.Len(); i++ {
			point := route.Point(i)
			report := v.ClipMovementPathFrom(simulation.Spawn{RegionID: at.RegionID, X: at.X, Y: at.Y, Z: at.Z}, owner,
				simulation.Spawn{RegionID: point.RegionID, X: point.X, Y: point.Y, Z: point.Z})
			at = monster.Pose{RegionID: report.Rest.RegionID, X: report.Rest.X, Y: report.Rest.Y, Z: report.Rest.Z}
			owner = report.RestOwner
			if math.Hypot(at.X-point.X, at.Z-point.Z) > .01 {
				break
			}
		}
		if math.Hypot(at.X-goal.X, at.Z-goal.Z) < 1 {
			return true
		}
	}
	return false
}

/*
================
TestCaveRouteArrivalFloors

Over 60 seeded Donwhang pairs, each setting of the two cave chase
switches (cave_chase.go) arrives at least as often as measured when
#1742 landed. The modern planner with the obstacle escape is the shipped
default; with both off the chase is the original's (cave_native_route.go);
the planner without the escape is #1658's route, which the default must
never fall below.
================
*/
func TestCaveRouteArrivalFloors(t *testing.T) {
	tier.Long(t, "60 seeded Donwhang chases under every cave chase setting")
	v, _, _ := dungeonValidators(t)
	v.SetCaveSeamStep(true)
	surface := v.caveSurface(planeCaveRegion)
	if surface == nil || surface.routeTable == nil {
		t.Fatal("Donwhang's native route table did not load")
	}
	settings := []struct {
		name           string
		modern, escape bool
		floor          int
	}{
		{"modern planner, obstacle escape (default)", true, true, caveModernArrivalFloor},
		{"modern planner, native obstacles (#1658)", true, false, cavePortalArrivalFloor},
		{"native route, obstacle escape", false, true, caveNativeEscapeArrivalFloor},
		{"native route, native obstacles", false, false, caveNativeArrivalFloor},
	}
	for _, setting := range settings {
		v.SetCaveModernChase(setting.modern)
		v.SetCaveObstacleEscape(setting.escape)
		random := rand.New(rand.NewSource(caveArrivalSeed))
		arrived := 0
		for i := 0; i < caveArrivalPairs; i++ {
			a, b := random.Intn(len(surface.blocks)), random.Intn(len(surface.blocks))
			from, goal := caveStandpoint(surface, planeCaveRegion, a), caveStandpoint(surface, planeCaveRegion, b)
			if walkCaveChase(v, from, simulation.NavOwner{}, goal) {
				arrived++
			}
		}
		t.Logf("%d pairs, %s: %d arrive", caveArrivalPairs, setting.name, arrived)
		if arrived < setting.floor {
			t.Errorf("%s: %d arrive, floor %d", setting.name, arrived, setting.floor)
		}
	}
}

/*
================
TestCaveModernChaseLeavesTheNativeTrap

Donwhang block 134 cell 7: the original's cell route takes a point 0.05
units off the line z = -1635, which whole-unit steps along that line never
reach, so the native chase paces there for good. The modern planner walks
the same chase home.
================
*/
func TestCaveModernChaseLeavesTheNativeTrap(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	v.SetCaveSeamStep(true)
	surface := v.caveSurface(planeCaveRegion)
	if surface == nil || surface.routeTable == nil {
		t.Fatal("Donwhang's native route table did not load")
	}
	from := monster.Pose{RegionID: planeCaveRegion, X: 1357, Y: 0.02, Z: -1635}
	owner := simulation.NavOwner{Kind: simulation.NavOwnerDungeon, Object: simulation.NavObjectCell{Surface: planeCaveRegion, Object: 134, Cell: 7}}
	goal := caveStandpoint(surface, planeCaveRegion, 147)
	if walkCaveChase(v, from, owner, goal) {
		t.Fatal("the native route left block 134's line trap; the trap this test pins is gone")
	}
	v.SetCaveModernChase(true)
	if !walkCaveChase(v, from, owner, goal) {
		t.Fatal("the modern planner did not walk the chase out of block 134")
	}
}
