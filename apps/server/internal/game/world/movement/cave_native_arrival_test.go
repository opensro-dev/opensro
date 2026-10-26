/*
===========================================================================

cave_native_arrival_test.go - the native cave route walks Donwhang (#1732)

A monster on the native route plans one waypoint at a time from the cell it
retains, walks the step, and plans again from where it stands. These walks
pin how far that gets: over 60 seeded pairs the native route arrives at
least as often as measured when #1732 landed. The
port's portal route keeps its own floor. #1742 (the original's 100 ms
re-evaluation) raises the native floor.

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
	// each route arrived at when #1732 landed.
	caveArrivalPairs       = 60
	caveNativeArrivalFloor = 49
	cavePortalArrivalFloor = 55
	caveArrivalSeed        = 1698
)

/*
================
walkCaveChase

Walks a monster from -> goal on the cave plane as the runtime does: plan
from the retained owner, walk the route's waypoints in order while each
arrives, carry the reached owner, replan. Returns whether it arrived
within the step budget.
================
*/
func walkCaveChase(v *WaterValidator, from, goal monster.Pose) bool {
	at, owner := from, simulation.NavOwner{}
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

Over 60 seeded Donwhang pairs, the native route arrives at least
caveNativeArrivalFloor times and the port's portal route (the native
table set aside) at least cavePortalArrivalFloor.
================
*/
func TestCaveRouteArrivalFloors(t *testing.T) {
	tier.Long(t, "60 seeded Donwhang chases on both cave routes")
	if testing.Short() {
		t.Skip("long: real-data cave chases")
	}
	v, _, _ := dungeonValidators(t)
	v.SetCaveSeamStep(true)
	surface := v.caveSurface(planeCaveRegion)
	if surface == nil || surface.routeTable == nil {
		t.Fatal("Donwhang's native route table did not load")
	}
	table := surface.routeTable
	random := rand.New(rand.NewSource(caveArrivalSeed))
	native, portal := 0, 0
	for i := 0; i < caveArrivalPairs; i++ {
		a, b := random.Intn(len(surface.blocks)), random.Intn(len(surface.blocks))
		from, goal := caveStandpoint(surface, planeCaveRegion, a), caveStandpoint(surface, planeCaveRegion, b)
		if walkCaveChase(v, from, goal) {
			native++
		}
		surface.routeTable = nil
		if walkCaveChase(v, from, goal) {
			portal++
		}
		surface.routeTable = table
	}
	t.Logf("%d pairs: the native route arrives %d, the portal route %d", caveArrivalPairs, native, portal)
	if native < caveNativeArrivalFloor || portal < cavePortalArrivalFloor {
		t.Fatalf("arrivals: native %d (floor %d), portal %d (floor %d)", native, caveNativeArrivalFloor, portal, cavePortalArrivalFloor)
	}
}
