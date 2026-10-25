/*
===========================================================================

cave_route_test.go - the cave region route over the real floors

The Donwhang cave and the tomb floors load from the shipped data:
across each floor the route must answer with a corridor a mover can
actually walk (every leg arrives), while the straight chord - what
the mover was handed before #1645 - provably does not. The control
in each test is today's answer over the same pair.

===========================================================================
*/
package movement

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
caveStandpoint

A standable pose at a block's middle cell.
================
*/
func caveStandpoint(surface *dungeonSpawnSurface, region uint16, block int) monster.Pose {
	mesh := surface.blocks[block].meshes[0]
	x, y, z := surface.cellCentroid(caveCell{block: block, mesh: 0, cell: mesh.cellCount() / 2})
	return monster.Pose{RegionID: region, X: x, Y: y, Z: z}
}

/*
================
caveBlockHops

The block adjacency, as the resolved portal links state it.
================
*/
func caveBlockHops(surface *dungeonSpawnSurface) [][]int {
	hops := make([][]int, len(surface.blocks))
	for i := range surface.blocks {
		for _, link := range surface.objects[i].placement.links {
			if across, ok := surface.ordinalIndex[link.target]; ok {
				hops[i] = append(hops[i], across)
			}
		}
	}
	return hops
}

/*
================
caveFarthestPair

The two blocks of a floor farthest apart in portal hops, first in
authored order on ties, so the test pair is stable.
================
*/
func caveFarthestPair(t *testing.T, surface *dungeonSpawnSurface) (int, int, int) {
	t.Helper()
	hops := caveBlockHops(surface)
	bestFrom, bestTo, bestHops := 0, 0, -1
	for start := range surface.blocks {
		distance := map[int]int{start: 0}
		queue := []int{start}
		for len(queue) > 0 {
			at := queue[0]
			queue = queue[1:]
			for _, next := range hops[at] {
				if _, seen := distance[next]; !seen {
					distance[next] = distance[at] + 1
					queue = append(queue, next)
				}
			}
		}
		for block, d := range distance {
			if d > bestHops {
				bestFrom, bestTo, bestHops = start, block, d
			}
		}
	}
	if bestHops < 2 {
		t.Fatalf("the floor's blocks span only %d portal hops", bestHops)
	}
	return bestFrom, bestTo, bestHops
}

/*
================
routeArrives

Whether a route walks start to finish the way the mover consumes
it: each waypoint adopted in order, each leg a chord the path
oracle lets through. That is exactly the input the stuck counter
feeds on, so arriving legs are the whole claim.
================
*/
func routeArrives(v *WaterValidator, from monster.Pose, route *monster.NavigationRoute) bool {
	if route == nil || route.Status() != monster.NavigationRouteReady || route.Len() == 0 {
		return false
	}
	current := from
	for i := 0; i < route.Len(); i++ {
		if !routeClear(v.PlanMonsterPath(current, route.Point(i)), route.Point(i)) {
			return false
		}
		current = route.Point(i)
	}
	return current == route.Goal()
}

/*
================
TestCaveRouteFollowsTheCorridor
================
*/
func TestCaveRouteFollowsTheCorridor(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	surface := v.caveSurface(planeCaveRegion)
	if surface == nil {
		t.Fatal("the Donwhang cave surface is not loaded")
	}
	fromBlock, toBlock, hops := caveFarthestPair(t, surface)
	from := caveStandpoint(surface, planeCaveRegion, fromBlock)
	goal := caveStandpoint(surface, planeCaveRegion, toBlock)
	goal.Heading = 0

	// The control: the straight chord the mover was handed before
	// the region route exists does not arrive, and neither does
	// today's open-terrain answer over this pair.
	if routeClear(v.PlanMonsterPath(from, goal), goal) {
		t.Fatal("the far pair's straight chord arrives; pick a harder pair")
	}
	if today := v.PlanMonsterRoute(from, goal); routeArrives(v, from, today) {
		t.Fatal("today's route already walks the corridor; the premise is gone")
	}

	route := v.planCaveRoute(from, goal)
	if !routeArrives(v, from, route) {
		t.Fatalf("route across %d portal hops does not arrive: status %v, %d points",
			hops, route.Status(), route.Len())
	}
	if route.Len() < 2 {
		t.Fatalf("route across %d portal hops has %d points", hops, route.Len())
	}

	// Every waypoint but the goal stands on the floor (the cave's
	// levels stack in plan view, so this is checked by height, and
	// the corridor itself is proven by the arriving legs above).
	for i := 0; i < route.Len()-1; i++ {
		point := route.Point(i)
		if _, ok := surface.heightAt(point.X, point.Y, point.Z); !ok {
			t.Fatalf("waypoint %d %+v stands on no floor", i, point)
		}
	}
}

/*
================
TestCaveRouteCrossesTheTombFloor

One tomb floor the same way: its farthest rooms are joined by a
route whose legs all arrive, where the straight chord cannot go.
================
*/
func TestCaveRouteCrossesTheTombFloor(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	const tomb = 0x8004
	surface := v.caveSurface(tomb)
	if surface == nil {
		t.Skip("this tomb floor is not in the shipped set")
	}
	fromBlock, toBlock, hops := caveFarthestPair(t, surface)
	from := caveStandpoint(surface, tomb, fromBlock)
	goal := caveStandpoint(surface, tomb, toBlock)
	if routeClear(v.PlanMonsterPath(from, goal), goal) {
		t.Fatal("the tomb pair's straight chord arrives; pick a harder pair")
	}
	route := v.planCaveRoute(from, goal)
	if !routeArrives(v, from, route) {
		t.Fatalf("tomb route across %d portal hops does not arrive: status %v, %d points",
			hops, route.Status(), route.Len())
	}
}

/*
================
TestCaveRouteSameRoomStaysDirect

Inside one room the native answer is the goal itself; the route
must not invent a corridor where the chord already walks.
================
*/
func TestCaveRouteSameRoomStaysDirect(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	surface := v.caveSurface(planeCaveRegion)
	if surface == nil {
		t.Fatal("the Donwhang cave surface is not loaded")
	}
	from := caveStandpoint(surface, planeCaveRegion, 0)
	block, mesh, cell, ok := surface.floorCellAt(from.X, from.Y, from.Z)
	if !ok {
		t.Fatal("the room standpoint is in no cell")
	}
	// The centre of a neighbouring cell in the same room: a short
	// chord that the oracle must let through.
	neighbours := surface.blocks[block].meshes[mesh].cellNeighbors[cell]
	if len(neighbours) == 0 {
		t.Fatal("the room's cell has no neighbours")
	}
	x, y, z := surface.cellCentroid(caveCell{block: block, mesh: mesh, cell: neighbours[0]})
	goal := monster.Pose{RegionID: planeCaveRegion, X: x, Y: y, Z: z}
	if !routeClear(v.PlanMonsterPath(from, goal), goal) {
		t.Fatal("the in-room control chord does not arrive")
	}
	route := v.planCaveRoute(from, goal)
	if route.Status() != monster.NavigationRouteReady || route.Len() != 1 || route.Point(0) != goal {
		t.Fatalf("same-room route = status %v, %d points; want the goal alone", route.Status(), route.Len())
	}
}

/*
================
TestCaveRouteUnresolvedWithoutLinks

A tomb floor whose blocks carry no resolved links cannot be
crossed; the planner must say so (the mover waits and replans)
instead of handing out a chord through the walls. A goal in the
room it stands in still routes.
================
*/
func TestCaveRouteUnresolvedWithoutLinks(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	const tomb = 0x8002
	surface := v.caveSurface(tomb)
	if surface == nil {
		t.Skip("this tomb floor is not in the shipped set")
	}
	if len(surface.blocks) < 2 {
		t.Skip("the floor has a single block")
	}
	for _, block := range surface.objects {
		if len(block.placement.links) != 0 {
			t.Skip("the floor grew links; pick a link-less one")
		}
	}
	from := caveStandpoint(surface, tomb, 0)
	goal := caveStandpoint(surface, tomb, len(surface.blocks)-1)
	if block, _, _, _ := surface.floorCellAt(goal.X, goal.Y, goal.Z); block == 0 {
		t.Skip("the last standpoint landed in the first block")
	}
	if route := v.planCaveRoute(from, goal); route.Status() == monster.NavigationRouteReady {
		t.Fatalf("a route across link-less blocks came back ready: %d points", route.Len())
	}
	room := v.planCaveRoute(from, from)
	if !routeArrives(v, from, room) {
		t.Fatal("standing still must route")
	}
}

/*
================
TestCaveRouteCrossesRegionsNever

A route never spans regions: the clip refuses the same move, and
the planner refuses with it.
================
*/
func TestCaveRouteCrossesRegionsNever(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	from := monster.Pose{RegionID: planeCaveRegion, X: 993.197, Y: 0.00034, Z: -307.099}
	goal := monster.Pose{RegionID: 0x8002, X: 993.197, Y: 0.00034, Z: -307.099}
	if route := v.planCaveRoute(from, goal); route.Status() == monster.NavigationRouteReady {
		t.Fatal("a cross-region route came back ready")
	}
}
