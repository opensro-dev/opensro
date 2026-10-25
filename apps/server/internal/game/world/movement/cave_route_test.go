/*
===========================================================================

cave_route_test.go - the cave region route over the real floors

The Donwhang cave and the tomb floors load from the shipped data:
across each floor the route must answer with a corridor a mover
can actually walk (every leg arrives), while the straight chord -
what the mover was handed before #1645 - provably does not. The
control in each test is today's answer over the same pair. Plans
run at doorway grain (#1658), so the corridor tests consume them
the way the mover does: adopt a plan's waypoints, replan from
where it ended, and require the walk to reach the goal within the
floor's doorway count. Cost, proof budget and leg grain are
pinned on the most expensive plans the floors offer.

===========================================================================
*/
package movement

import (
	"math"
	"sort"
	"testing"
	"time"

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
caveBiggestBlock

The block carrying the most cells (ties keep the first).
================
*/
func caveBiggestBlock(surface *dungeonSpawnSurface) int {
	best, bestCells := 0, -1
	for i := range surface.blocks {
		cells := 0
		for _, mesh := range surface.blocks[i].meshes {
			cells += mesh.cellCount()
		}
		if cells > bestCells {
			best, bestCells = i, cells
		}
	}
	return best
}

/*
================
caveSpreadCells

Sampled cells of one block spread across it (every step-th cell),
as standpoints for the cost and budget probes.
================
*/
func caveSpreadCells(surface *dungeonSpawnSurface, block, step int) []caveCell {
	mesh := surface.blocks[block].meshes[0]
	cells := []caveCell{}
	for cell := 0; cell < mesh.cellCount(); cell += step {
		cells = append(cells, caveCell{block: block, mesh: 0, cell: cell})
	}
	return cells
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
walkCaveRoute

Walks from a standpoint to a goal the way the mover consumes
plans: adopt each plan's waypoints in order, then replan from
wherever the plan ended (the next doorway's landing) until a plan
ends at the goal. Every leg must arrive and stand on the floor,
and every leg but the same-room step onto the goal itself stays
within the pull's room grain. The walk must finish within
maxPlans plans - one per doorway crossed, plus slack - or the
route is sending the mover in circles. Returns the plan count.
================
*/
func walkCaveRoute(t *testing.T, v *WaterValidator, surface *dungeonSpawnSurface, from, goal monster.Pose, maxPlans int) int {
	t.Helper()
	at := from
	for plans := 1; ; plans++ {
		if plans > maxPlans {
			t.Fatalf("walk from %+v toward %+v still planning after %d plans", from, goal, maxPlans)
		}
		route := v.planCaveRoute(at, goal)
		if route == nil || route.Status() != monster.NavigationRouteReady || route.Len() == 0 {
			status := monster.NavigationRouteBlocked
			if route != nil {
				status = route.Status()
			}
			t.Fatalf("plan %d from %+v toward %+v is not ready: %v", plans, at, goal, status)
		}
		if route.Goal() != goal {
			t.Fatalf("plan %d answers goal %+v, want %+v", plans, route.Goal(), goal)
		}
		for i := 0; i < route.Len(); i++ {
			point := route.Point(i)
			if _, ok := surface.heightAt(point.X, point.Y, point.Z); !ok {
				t.Fatalf("plan %d waypoint %d %+v stands on no floor", plans, i, point)
			}
			if !routeClear(v.PlanMonsterPath(at, point), point) {
				t.Fatalf("plan %d leg %d from %+v to %+v does not arrive", plans, i, at, point)
			}
			if point != goal {
				if d := routeDistance(at, point); d > maxCaveLegUnits {
					t.Fatalf("plan %d leg %d runs %0.1f units, past the room grain", plans, i, d)
				}
			}
			at = point
		}
		if at == goal {
			return plans
		}
	}
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

	plans := walkCaveRoute(t, v, surface, from, goal, hops+2)
	if plans < 2 {
		t.Fatalf("walk across %d portal hops took %d plans; the doorway grain is gone", hops, plans)
	}
}

/*
================
TestCaveRouteCrossesTheTombFloor

One tomb floor the same way: its farthest rooms are joined by a
walk whose legs all arrive, where the straight chord cannot go.
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
	plans := walkCaveRoute(t, v, surface, from, goal, hops+2)
	if plans < 2 {
		t.Fatalf("tomb walk across %d portal hops took %d plans; the doorway grain is gone", hops, plans)
	}
}

/*
================
TestCaveRoutePlanCostStaysTickCheap

A plan runs inside the tick and is re-asked as the target moves
(#1658), so the most expensive plans the floors offer - the
doorway plan across their farthest pair, and the longest pull
inside their biggest block - must stay tick-cheap. The pinned
bound is the plan's deterministic work: cell expansions, oracle
probes and the geometric pull's strip steps (#1663), which no
machine load can inflate. Wall clock is only a loose 20 ms guard
on top, and skipped under the race detector, whose
instrumentation prices nothing like the tick does.
================
*/
func TestCaveRoutePlanCostStaysTickCheap(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	type subject struct {
		name       string
		from, goal monster.Pose
	}
	subjects := []subject{}
	for _, region := range []uint16{planeCaveRegion, 0x8004} {
		surface := v.caveSurface(region)
		if surface == nil {
			t.Skipf("floor 0x%04X is not in the shipped set", region)
		}
		fromBlock, toBlock, _ := caveFarthestPair(t, surface)
		subjects = append(subjects, subject{
			name: "farthest doorway plan",
			from: caveStandpoint(surface, region, fromBlock),
			goal: caveStandpoint(surface, region, toBlock),
		})
		// The longest single-block pull: the biggest block's two
		// sampled cells farthest apart.
		big := caveBiggestBlock(surface)
		cells := caveSpreadCells(surface, big, 7)
		var a, b caveCell
		best := -1.0
		for i := 0; i < len(cells); i++ {
			ax, _, az := surface.cellCentroid(cells[i])
			for j := i + 1; j < len(cells); j++ {
				bx, _, bz := surface.cellCentroid(cells[j])
				if d := (ax-bx)*(ax-bx) + (az-bz)*(az-bz); d > best {
					best, a, b = d, cells[i], cells[j]
				}
			}
		}
		subjects = append(subjects, subject{
			name: "biggest-block pull",
			from: surface.cellPose(region, 0, a),
			goal: surface.cellPose(region, 0, b),
		})
	}
	// planWorkBudget bounds one plan's deterministic work. The
	// worst subject below - the biggest-block pull on the tomb
	// floor - spends 5258 (1724 expansions, 3534 strip steps), so
	// the budget leaves it about half again as headroom and fails a
	// plan whose search or walk grows by that much. The oracle
	// route this design replaced is not on the same scale: it
	// spends its whole 256-probe budget on these pairs and gives
	// up, each probe a floor scan (13-53 ms a plan), where a strip
	// step is one plane read. The plan head's one direct-chord
	// probe still counts 1 while costing most of the tomb doorway
	// plan's time; weighting it is a separate change.
	const planWorkBudget = 8192
	planned := 0
	for _, s := range subjects {
		for i := 0; i < 3; i++ {
			v.planCaveRoute(s.from, s.goal)
		}
		// The average of 21 warm plans, not a median of single
		// samples: a coarse system clock (some Windows hosts tick
		// at ~1 ms) makes any one plan read 0 or a whole tick,
		// while the total over the loop still resolves.
		samples := make([]time.Duration, 21)
		var route *monster.NavigationRoute
		started := time.Now()
		for i := range samples {
			one := time.Now()
			route = v.planCaveRoute(s.from, s.goal)
			samples[i] = time.Since(one)
			planned += route.Len()
		}
		average := time.Since(started) / time.Duration(len(samples))
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		median := samples[len(samples)/2]
		t.Logf("%s (0x%04X): work %d, average plan %v (median sample %v)", s.name, s.from.RegionID, route.Probes(), average, median)
		if route.Probes() > planWorkBudget {
			t.Fatalf("%s (0x%04X): plan work %d (expansions, probes and strip steps) exceeds the %d budget; a plan must stay inside one block",
				s.name, s.from.RegionID, route.Probes(), planWorkBudget)
		}
		if !raceDetectorOn && average > 20*time.Millisecond {
			t.Fatalf("%s (0x%04X): average plan %v exceeds the loose 20 ms wall-clock guard", s.name, s.from.RegionID, average)
		}
	}
	if planned == 0 {
		t.Fatal("the timed plans produced no waypoints")
	}
}

/*
================
TestCaveRouteProbeBudgetEndsThePull

The proving pull's budget is 256 oracle probes, the outdoor
route's budget: a path long enough to want more must still end at
its terminal with the budget spent exactly - the tail goes out
unproven at a stride rather than the pull running on. No shipped
block spends that much proving one doorway plan (the geometric
pull spends nothing), so the path is a tour of the one-block tomb
floor's real corridors, stitched until the proving pull runs out:
the shape a larger authored block presents to it.
================
*/
func TestCaveRouteProbeBudgetEndsThePull(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	const tomb = 0x8007
	surface := v.caveSurface(tomb)
	if surface == nil || len(surface.blocks) != 1 {
		t.Skip("the one-block tomb floor is not in the shipped set")
	}
	cells := caveSpreadCells(surface, 0, 103)
	if len(cells) < 6 {
		t.Fatalf("the floor sampled only %d standpoints", len(cells))
	}
	// Chain five of the floor's longest real corridors end to
	// end, each new standpoint at least half the floor's sampled
	// spread from the start, so the terminal stays too far for
	// the pull's direct-chord shortcut and every corridor is
	// proven the expensive way, bend by bend.
	from := surface.cellPose(tomb, 0, cells[0])
	spread := 0.0
	for _, candidate := range cells {
		cx, _, cz := surface.cellCentroid(candidate)
		if d := math.Hypot(cx-from.X, cz-from.Z); d > spread {
			spread = d
		}
	}
	path := []caveCell{}
	terminal := from
	at := cells[0]
	for legs := 0; legs < 5; legs++ {
		var segment []caveCell
		end := at
		for _, candidate := range cells {
			if candidate == at {
				continue
			}
			cx, _, cz := surface.cellCentroid(candidate)
			if math.Hypot(cx-from.X, cz-from.Z) < spread/2 {
				continue
			}
			way, _, found := surface.astarCells(at, candidate)
			if found && len(way) > len(segment) {
				segment, end = way, candidate
			}
		}
		if len(segment) == 0 {
			t.Fatal("the chain found no corridor onward")
		}
		if len(path) == 0 {
			path = segment
		} else {
			path = append(path, segment[1:]...)
		}
		at = end
		terminal = surface.cellPose(tomb, 0, end)
	}
	points, probes := v.proveCaveRoute(surface, from, terminal, terminal, path)
	if probes != 256 {
		t.Fatalf("the proving pull spent %d probes over a %d-cell path, want the 256 budget spent exactly", probes, len(path))
	}
	if len(points) == 0 || points[len(points)-1] != terminal {
		t.Fatalf("the exhausted pull ends at %+v, want the terminal %+v", points, terminal)
	}
	for i, point := range points {
		if _, ok := surface.heightAt(point.X, point.Y, point.Z); !ok {
			t.Fatalf("point %d %+v stands on no floor", i, point)
		}
	}
}

/*
================
TestCaveRouteLegCapSplitsLongHalls

Inside the pull a leg is capped at room grain even where a longer
chord would arrive: across this tomb hall the straight chord walks
well past the cap, so without it the pull would hand the mover one
long leg; with it every leg stays at grain and the pull still ends
at the terminal.
================
*/
func TestCaveRouteLegCapSplitsLongHalls(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	const tomb = 0x8004
	surface := v.caveSurface(tomb)
	if surface == nil {
		t.Skip("this tomb floor is not in the shipped set")
	}
	big := caveBiggestBlock(surface)
	cells := caveSpreadCells(surface, big, 61)
	var from, terminal monster.Pose
	found := false
	for i := 0; i < len(cells) && !found; i++ {
		a := surface.cellPose(tomb, 0, cells[i])
		for j := i + 1; j < len(cells); j++ {
			b := surface.cellPose(tomb, 0, cells[j])
			if routeDistance(a, b) <= 2*maxCaveLegUnits {
				continue
			}
			if routeClear(v.PlanMonsterPath(a, b), b) {
				from, terminal, found = a, b, true
				break
			}
		}
	}
	if !found {
		t.Fatal("no hall past twice the leg cap in the biggest block; pick another floor")
	}
	startBlock, startMesh, startCell, startOK := surface.floorCellAt(from.X, from.Y, from.Z)
	goalBlock, goalMesh, goalCell, goalOK := surface.floorCellAt(terminal.X, terminal.Y, terminal.Z)
	if !startOK || !goalOK || startBlock != goalBlock {
		t.Fatalf("the hall endpoints locate to blocks %d and %d (ok %t/%t)", startBlock, goalBlock, startOK, goalOK)
	}
	path, _, routed := surface.astarCells(
		caveCell{block: startBlock, mesh: startMesh, cell: startCell},
		caveCell{block: goalBlock, mesh: goalMesh, cell: goalCell},
	)
	if !routed {
		t.Fatal("no corridor path across the hall's block")
	}
	points, probes, steps := v.pullCaveRoute(surface, from, terminal, terminal, path)
	if probes != 0 {
		t.Fatalf("the hall pull spent %d oracle probes; the geometric pull should need none here", probes)
	}
	if len(points) < 2 {
		t.Fatalf("the pull kept the %0.0f-unit hall in %d point(s); the leg cap is not binding", routeDistance(from, terminal), len(points))
	}
	// The pull's work is its strip walk (#1663): every leg it kept
	// was walked, reading its start and end heights and a profile
	// sample at least every maxDungeonFloorSampleSpacing, so the
	// kept legs alone set a floor under the steps however many
	// refused candidates the walk also tried.
	walked, prev := 0, from
	for _, point := range points {
		walked += 2 + int(math.Ceil(routeDistance(prev, point)/maxDungeonFloorSampleSpacing))
		prev = point
	}
	if steps < walked {
		t.Fatalf("the hall pull reports %d strip steps; its %d kept legs alone walk %d", steps, len(points), walked)
	}
	current := from
	for i, point := range points {
		if d := routeDistance(current, point); d > maxCaveLegUnits {
			t.Fatalf("leg %d runs %0.1f units, past the %0.1f cap", i, d, maxCaveLegUnits)
		}
		current = point
	}
	if current != terminal {
		t.Fatalf("the pull ends at %+v, want the terminal %+v", current, terminal)
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
