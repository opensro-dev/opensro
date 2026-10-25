/*
===========================================================================

cave_route.go - routes across a cave floor's walkable meshes

The reference server steers cave movement in two stages: within the
sector it is standing in, an actor steps straight at its goal; toward
another sector it first walks a region route - the next portal cell
toward the destination - and only steps at the goal once the route
brings it into the goal's own sector. The port projects a whole cave
floor (the Donwhang cave, each tomb floor) onto a single region id,
so the sector grain inside a floor is the floor's authored blocks:
the walkable cell meshes each block carries, joined at the portal
edges the dungeon link pass resolved. This file is that route, at
that grain: each floor's blocks are linked once into a next-doorway
table (the floors are static), so a plan is a table lookup plus a
cell search inside the block the actor stands in, string-pulled
against the same path oracle the mover's own legs use. The legs
run to the goal when it shares the block, and to the next doorway's
landing otherwise; the mover replans there, the way the reference
steps portal to portal. Every leg handed out is one the mover can
actually walk. Where the floor has no surface, no cells under an
endpoint, or no route, the planner answers exactly what callers
get today or an honest unresolved route.

===========================================================================
*/
package movement

import (
	"container/heap"
	"math"

	"opensro.online/server/internal/game/world/monster"
)

// maxCaveRouteCells bounds one block's cell search; the largest
// shipped block carries about three thousand cells, so a search
// that spends the whole budget inside one block is not arriving.
const maxCaveRouteCells = 8192

// maxCaveRouteProbes bounds the path-oracle calls the proving
// pull (proveCaveRoute) may spend on one plan: the outdoor route's
// budget, because it proves one block's legs, the grain the outdoor
// search proves too. Past it the remaining corridor points go out
// unproven and the mover's own departure checks take over.
const maxCaveRouteProbes = monsterRouteProbeBudget

// maxCaveLegUnits bounds one leg's reach: the reference steps
// doorway to doorway, so legs stay at room grain even where a long
// straight chord happens to walk.
const maxCaveLegUnits = 600.0

/*
================
caveCell

One walkable cell of one block's mesh: the search's node identity.
================
*/
type caveCell struct {
	block, mesh, cell int
}

/*
================
caveCellRecord

One cell's A* record: the cost paid to reach it, its parent in the
search (a cell of the same mesh), and its queue order. One record
per cell lives in a flat slice indexed by the cell itself.
================
*/
type caveCellRecord struct {
	cost, priority float64
	parent         int
	hasParent      bool
	closed         bool
	order          int
}

/*
================
caveCellEntry

One frontier entry: a cell plus the priority and order it was
pushed with, snapshotted so a better re-push never mutates an
entry the heap is holding.
================
*/
type caveCellEntry struct {
	cell     int
	priority float64
	order    int
}

/*
================
caveCellQueue

The cell search's frontier, cheapest first, earliest pushed on
ties - the deterministic order the search has always used.
================
*/
type caveCellQueue struct {
	entries []caveCellEntry
}

/*
================
Len
================
*/
func (q *caveCellQueue) Len() int { return len(q.entries) }

/*
================
Less
================
*/
func (q *caveCellQueue) Less(i, j int) bool {
	if q.entries[i].priority != q.entries[j].priority {
		return q.entries[i].priority < q.entries[j].priority
	}
	return q.entries[i].order < q.entries[j].order
}

/*
================
Swap
================
*/
func (q *caveCellQueue) Swap(i, j int) { q.entries[i], q.entries[j] = q.entries[j], q.entries[i] }

/*
================
Push
================
*/
func (q *caveCellQueue) Push(v any) { q.entries = append(q.entries, v.(caveCellEntry)) }

/*
================
Pop
================
*/
func (q *caveCellQueue) Pop() any {
	n := len(q.entries) - 1
	v := q.entries[n]
	q.entries = q.entries[:n]
	return v
}

/*
================
caveSurface

The floor's loaded dungeon surface, loading the generated set once
like every other reader.
================
*/
func (v *WaterValidator) caveSurface(regionID uint16) *dungeonSpawnSurface {
	v.dungeonSpawnOnce.Do(func() { v.dungeonSpawnSurfaces = v.loadDungeonSpawnSurfaces() })
	return v.dungeonSpawnSurfaces[regionID]
}

/*
================
caveNextHop

One entry of a floor's doorway table: the cell to leave the current
block by (exit) and the cell that doorway lands on (land) in the
next block. The zero value is no route.
================
*/
type caveNextHop struct {
	exit, land caveCell
	ok         bool
}

/*
================
nextHop

The doorway table's answer for one pair of blocks.
================
*/
func (s *dungeonSpawnSurface) nextHop(fromBlock, toBlock int) (caveNextHop, bool) {
	hop := s.blockNextHop[fromBlock*len(s.blocks)+toBlock]
	return hop, hop.ok
}

/*
================
buildBlockNextHop

A floor's doorway table, one breadth-first pass per block over the
resolved portal links: first visit wins, so each destination keeps
the fewest-doorways way in, and the entry recorded for it is the
doorway the source block's own route leaves by (walk the visits
back to the source's child). The link and mesh guards are the ones
the route search has always used; a block they exclude is never a
doorway. Built once per floor - the data is static - so a plan
reads the next doorway instead of searching the floor.
================
*/
func (s *dungeonSpawnSurface) buildBlockNextHop() []caveNextHop {
	blocks := len(s.blocks)
	table := make([]caveNextHop, blocks*blocks)
	type entered struct {
		from int
		link objectNavLink
	}
	for source := 0; source < blocks; source++ {
		enteredBy := make([]entered, blocks)
		seen := make([]bool, blocks)
		seen[source] = true
		queue := []int{source}
		for len(queue) > 0 {
			at := queue[0]
			queue = queue[1:]
			if len(s.blocks[at].meshes) != 1 {
				continue
			}
			for _, link := range s.objects[at].placement.links {
				across, ok := s.ordinalIndex[link.target]
				if !ok || seen[across] || len(s.blocks[across].meshes) != 1 {
					continue
				}
				seen[across] = true
				enteredBy[across] = entered{from: at, link: link}
				queue = append(queue, across)
			}
		}
		for dest := 0; dest < blocks; dest++ {
			if dest == source || !seen[dest] {
				continue
			}
			step := dest
			for enteredBy[step].from != source {
				step = enteredBy[step].from
			}
			link := enteredBy[step].link
			exitMesh := s.blocks[source].meshes[0]
			landMesh := s.blocks[step].meshes[0]
			table[source*blocks+dest] = caveNextHop{
				exit: caveCell{block: source, mesh: 0, cell: int(exitMesh.outline.srcCell[link.edge])},
				land: caveCell{block: step, mesh: 0, cell: int(landMesh.outline.srcCell[link.targetEdge])},
				ok:   true,
			}
		}
	}
	return table
}

/*
================
planCaveRoute

The cave plane's waypoint owner. A straight chord that already
walks (same room) returns the goal alone, exactly the native
same-sector branch. Anything else is planned at doorway grain:
inside the goal's own block the legs run to the goal; toward any
other block they run to the next doorway's landing and the mover
replans there, so one plan searches one block, never the floor.
The failure answers are today's route or an unresolved route with
the outdoor vocabulary's reasons.
================
*/
func (v *WaterValidator) planCaveRoute(from, goal monster.Pose) *monster.NavigationRoute {
	if from.RegionID != goal.RegionID {
		return monster.UnresolvedNavigationRoute(goal, monster.NavigationRouteBlocked)
	}
	surface := v.caveSurface(from.RegionID)
	if surface == nil {
		return monster.UnresolvedNavigationRoute(goal, monster.NavigationGeometryUnavailable)
	}
	if routeClear(v.PlanMonsterPath(from, goal), goal) {
		return monster.NewNavigationRoute(goal, []monster.Pose{goal}).WithProbes(1)
	}

	startBlock, startMesh, startCell, startOK := surface.floorCellAt(from.X, from.Y, from.Z)
	goalBlock, goalMesh, goalCell, goalOK := surface.floorCellAt(goal.X, goal.Y, goal.Z)
	if !startOK || !goalOK {
		// An endpoint stands off the authored floor (a knockback
		// onto a wall, a spawn between cells): today's answer,
		// never a worse one.
		return v.PlanMonsterRoute(from, goal)
	}
	start := caveCell{block: startBlock, mesh: startMesh, cell: startCell}
	target := caveCell{block: goalBlock, mesh: goalMesh, cell: goalCell}
	if start == target {
		return monster.NewNavigationRoute(goal, []monster.Pose{goal}).WithProbes(1)
	}
	if start.block == target.block {
		path, used, found := surface.astarCells(start, target)
		if !found {
			return monster.UnresolvedNavigationRoute(goal, monster.NavigationRouteBlocked).WithProbes(used)
		}
		points, probes := v.pullCaveRoute(surface, from, goal, goal, path)
		if len(points) == 0 {
			return monster.UnresolvedNavigationRoute(goal, monster.NavigationRouteBlocked).WithProbes(used + probes)
		}
		return monster.NewNavigationRoute(goal, points).WithProbes(used + probes)
	}
	hop, ok := surface.nextHop(start.block, target.block)
	if !ok {
		return monster.UnresolvedNavigationRoute(goal, monster.NavigationRouteBlocked)
	}
	path, used, found := surface.astarCells(start, hop.exit)
	if !found {
		return monster.UnresolvedNavigationRoute(goal, monster.NavigationRouteBlocked).WithProbes(used)
	}
	// The landing cell closes the path: the legs end standing in
	// the next block, where the mover's next plan takes over.
	path = append(path, hop.land)
	terminal := surface.cellPose(from.RegionID, goal.Heading, hop.land)
	points, probes := v.pullCaveRoute(surface, from, goal, terminal, path)
	if len(points) == 0 {
		return monster.UnresolvedNavigationRoute(goal, monster.NavigationRouteBlocked).WithProbes(used + probes)
	}
	return monster.NewNavigationRoute(goal, points).WithProbes(used + probes)
}

/*
================
floorCellAt

The floor cell holding a point: the block, mesh and cell whose
plane contains it, nearest in height to the point's own Y. The
scan mirrors heightAt so a point the height owner can stand on
always locates.
================
*/
func (s *dungeonSpawnSurface) floorCellAt(x, y, z float64) (int, int, int, bool) {
	bestBlock, bestMesh, bestCell := -1, -1, -1
	bestDelta := math.Inf(1)
	for i := range s.blocks {
		block := &s.blocks[i]
		if block.reach.missedBy(x, z, x, z) {
			continue
		}
		dx, dz := x-block.x, z-block.z
		localX := block.cosYaw*dx + block.sinYaw*dz
		localZ := -block.sinYaw*dx + block.cosYaw*dz
		localHintY := y - block.y
		for meshIndex, mesh := range block.meshes {
			if localX < mesh.minX || localX > mesh.maxX ||
				localZ < mesh.minZ || localZ > mesh.maxZ {
				continue
			}
			for cell := 0; cell < mesh.cellCount(); cell++ {
				if mesh.cellMissedBy(cell, localX, localX, localZ, localZ) {
					continue
				}
				localY, inside := objectCellPlaneYAt(mesh, cell, localX, localZ)
				if !inside {
					continue
				}
				if delta := math.Abs(float64(localY) - localHintY); delta < bestDelta {
					bestBlock, bestMesh, bestCell, bestDelta = i, meshIndex, cell, delta
				}
			}
		}
	}
	return bestBlock, bestMesh, bestCell, bestBlock >= 0
}

/*
================
cellCentroid

A cell's centre in world coordinates: the standable point every
route waypoint is built from.
================
*/
func (s *dungeonSpawnSurface) cellCentroid(at caveCell) (float64, float64, float64) {
	block := &s.blocks[at.block]
	mesh := block.meshes[at.mesh]
	a := int(mesh.cellA[at.cell]) * 3
	b := int(mesh.cellB[at.cell]) * 3
	c := int(mesh.cellC[at.cell]) * 3
	localX := (float64(mesh.vertices[a]) + float64(mesh.vertices[b]) + float64(mesh.vertices[c])) / 3
	localY := (float64(mesh.vertices[a+1]) + float64(mesh.vertices[b+1]) + float64(mesh.vertices[c+1])) / 3
	localZ := (float64(mesh.vertices[a+2]) + float64(mesh.vertices[b+2]) + float64(mesh.vertices[c+2])) / 3
	return block.cosYaw*localX - block.sinYaw*localZ + block.x,
		localY + block.y,
		block.sinYaw*localX + block.cosYaw*localZ + block.z
}

/*
================
astarCells

A* over one mesh's cells, following the internal cell adjacency.
Costs are centroid distances. Returns the cell path from start to
target inclusive. The adjacency never leaves a mesh, so a target
in another mesh (none of the shipped blocks has two) is
unreachable by construction and refused up front.
================
*/
func (s *dungeonSpawnSurface) astarCells(start, target caveCell) ([]caveCell, int, bool) {
	if start.block != target.block || start.mesh != target.mesh {
		return nil, 0, false
	}
	mesh := s.blocks[start.block].meshes[start.mesh]
	goalX, goalY, goalZ := s.cellCentroid(target)
	records := make([]caveCellRecord, mesh.cellCount())
	seen := make([]bool, mesh.cellCount())
	queue := &caveCellQueue{}
	order := 0
	push := func(cell int, cost float64, parent int, hasParent bool) {
		x, y, z := s.cellCentroid(caveCell{block: start.block, mesh: start.mesh, cell: cell})
		record := &records[cell]
		record.cost = cost
		record.priority = cost + math.Sqrt((x-goalX)*(x-goalX)+(y-goalY)*(y-goalY)+(z-goalZ)*(z-goalZ))
		record.parent = parent
		record.hasParent = hasParent
		record.order = order
		order++
		seen[cell] = true
		heap.Push(queue, caveCellEntry{cell: cell, priority: record.priority, order: record.order})
	}
	push(start.cell, 0, 0, false)
	expansions := 0
	for queue.Len() > 0 {
		entry := heap.Pop(queue).(caveCellEntry)
		record := &records[entry.cell]
		if record.closed || record.order != entry.order {
			continue
		}
		record.closed = true
		expansions++
		if expansions > maxCaveRouteCells {
			return nil, expansions, false
		}
		if entry.cell == target.cell {
			path := []caveCell{}
			for cell := entry.cell; ; {
				path = append(path, caveCell{block: start.block, mesh: start.mesh, cell: cell})
				if !records[cell].hasParent {
					break
				}
				cell = records[cell].parent
			}
			for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
				path[i], path[j] = path[j], path[i]
			}
			return path, expansions, true
		}
		at := caveCell{block: start.block, mesh: start.mesh, cell: entry.cell}
		fromX, fromY, fromZ := s.cellCentroid(at)
		for _, neighbour := range mesh.cellNeighbors[entry.cell] {
			x, y, z := s.cellCentroid(caveCell{block: start.block, mesh: start.mesh, cell: neighbour})
			cost := record.cost + math.Sqrt((x-fromX)*(x-fromX)+(y-fromY)*(y-fromY)+(z-fromZ)*(z-fromZ))
			if seen[neighbour] && (records[neighbour].closed || cost >= records[neighbour].cost) {
				continue
			}
			push(neighbour, cost, entry.cell, true)
		}
	}
	return nil, expansions, false
}

/*
================
cellPose

A cell's centre as a route waypoint. The centroid's height needs
no floor scan: a triangle's plane at its own centroid is the mean
of its corners, which is the centroid itself, so the point stands
exactly on its cell; the mover's own height sampling owns the
walk from there.
================
*/
func (s *dungeonSpawnSurface) cellPose(regionID uint16, heading uint16, at caveCell) monster.Pose {
	x, y, z := s.cellCentroid(at)
	return monster.Pose{RegionID: regionID, X: x, Y: y, Z: z, Heading: heading}
}

/*
================
proveCaveRoute

The proving pull: the same string-pull as the geometric one, but
every candidate leg is proven against the path oracle the mover's
own legs use (galloping, then binary search, within the proof
budget). A cave oracle probe costs a floor scan per chord, which
is why plans do not pay it by default: this pull runs only when
the geometric pull's legs cross an obstacle circle, the one thing
the cell strip does not encode. A centre whose own chord will not
arrive (a hairpin edge) goes out unproven; the mover revalidates
every leg at departure either way, and a replan from any adopted
point is short.
================
*/
func (v *WaterValidator) proveCaveRoute(surface *dungeonSpawnSurface, from, goal, terminal monster.Pose, path []caveCell) ([]monster.Pose, int) {
	probes := 0
	points := make([]monster.Pose, 0, 16)
	current := from
	arrives := func(to monster.Pose) bool {
		if routeDistance(current, to) > maxCaveLegUnits {
			return false
		}
		probes++
		return routeClear(v.PlanMonsterPath(current, to), to)
	}
	emit := func(pose monster.Pose) {
		points = append(points, pose)
		current = pose
	}
	cellPose := func(i int) monster.Pose {
		return surface.cellPose(from.RegionID, goal.Heading, path[i])
	}

	next := 1 // the first path cell not yet adopted
	for next < len(path) {
		if probes < maxCaveRouteProbes && arrives(terminal) {
			break
		}
		if probes >= maxCaveRouteProbes {
			// Out of proof budget: the corridor itself, at a stride.
			for ; next < len(path); next += 16 {
				emit(cellPose(next))
			}
			break
		}
		// Gallop: offsets 0, 1, 3, 7, ... cells ahead until a chord
		// fails, then binary-search the last arriving centre.
		lastGood := -1
		firstBad := len(path)
		for offset := 0; next+offset < len(path); offset = offset*2 + 1 {
			if probes >= maxCaveRouteProbes {
				firstBad = next + offset
				break
			}
			if arrives(cellPose(next + offset)) {
				lastGood = next + offset
			} else {
				firstBad = next + offset
				break
			}
		}
		for firstBad-lastGood > 1 {
			mid := lastGood + (firstBad-lastGood)/2
			if probes >= maxCaveRouteProbes || !arrives(cellPose(mid)) {
				firstBad = mid
			} else {
				lastGood = mid
			}
		}
		if lastGood < next {
			// Even the next cell's centre will not arrive by chord
			// (the pair spans two giant triangles): cross at the
			// edge the cells share, whose halves stay inside one
			// triangle each, when the crossing itself arrives;
			// otherwise the centre goes out as authored and the
			// mover's replan works from wherever the leg stops.
			if joint, ok := surface.pathJoint(from.RegionID, goal.Heading, path[next-1], path[next]); ok && probes < maxCaveRouteProbes && arrives(joint) {
				emit(joint)
			}
			emit(cellPose(next))
			next++
			continue
		}
		emit(cellPose(lastGood))
		next = lastGood + 1
	}
	return append(points, terminal), probes
}

/*
================
stripEdge

The edge two neighbouring path cells share, as world endpoints:
inside one block, the two vertices both triangles own; across a
portal, the link whose edge this cell owns and whose far edge the
next cell owns - several doorways can join the same pair of
blocks, and only this one is on the path. The geometric pull
accepts a leg only over the edges it properly crosses, so this is
its yardstick; the joint below is the same edge's middle.
================
*/
func (s *dungeonSpawnSurface) stripEdge(a, b caveCell) (head, tail [3]float64, ok bool) {
	if a.block == b.block && a.mesh == b.mesh {
		mesh := s.blocks[a.block].meshes[a.mesh]
		inA := map[uint16]bool{}
		for _, index := range []uint16{mesh.cellA[a.cell], mesh.cellB[a.cell], mesh.cellC[a.cell]} {
			inA[index] = true
		}
		var shared []uint16
		for _, index := range []uint16{mesh.cellA[b.cell], mesh.cellB[b.cell], mesh.cellC[b.cell]} {
			if inA[index] {
				shared = append(shared, index)
			}
		}
		if len(shared) != 2 {
			return head, tail, false
		}
		block := &s.blocks[a.block]
		endpoint := func(vertex uint16) [3]float64 {
			at := int(vertex) * 3
			localX := float64(mesh.vertices[at])
			localY := float64(mesh.vertices[at+1])
			localZ := float64(mesh.vertices[at+2])
			return [3]float64{
				block.cosYaw*localX - block.sinYaw*localZ + block.x,
				localY + block.y,
				block.sinYaw*localX + block.cosYaw*localZ + block.z,
			}
		}
		return endpoint(shared[0]), endpoint(shared[1]), true
	}
	meshA := s.blocks[a.block].meshes[a.mesh]
	for _, link := range s.objects[a.block].placement.links {
		across, found := s.ordinalIndex[link.target]
		if !found || across != b.block || int(meshA.outline.srcCell[link.edge]) != a.cell {
			continue
		}
		meshB := s.blocks[b.block].meshes[b.mesh]
		if int(meshB.outline.srcCell[link.targetEdge]) != b.cell {
			continue
		}
		placement := s.objects[a.block].placement
		return portalEndpoint(placement, meshA, link.edge, 0),
			portalEndpoint(placement, meshA, link.edge, 1), true
	}
	return head, tail, false
}

/*
================
pathJoint

The standpoint on the edge two neighbouring path cells share. A
chord through the joint stays within one triangle per half, which
the long centre-to-centre chord across a giant cell pair does not.
================
*/
func (s *dungeonSpawnSurface) pathJoint(regionID uint16, heading uint16, a, b caveCell) (monster.Pose, bool) {
	head, tail, ok := s.stripEdge(a, b)
	if !ok {
		return monster.Pose{}, false
	}
	x, y, z := (head[0]+tail[0])/2, (head[1]+tail[1])/2, (head[2]+tail[2])/2
	if floorY, snapped := s.heightAt(x, y, z); snapped {
		y = floorY
	}
	return monster.Pose{RegionID: regionID, X: x, Y: y, Z: z, Heading: heading}, true
}

/*
================
pullCaveRoute

Pulls one block's cell path tight into legs geometrically, ending
at the terminal. The candidates are the path cells' own centres,
and a leg is accepted only when it walks the strip the way the
mover's departure clip will (legWalksStrip): its chord crosses
every shared edge between its endpoints' cells in plan view - so
each piece of it lies inside one (convex) cell - and its height
profile, sampled on the strip's own planes, never steps further
than the clip's floor step. No path-oracle probes at plan time:
an oracle probe is a floor scan per chord, while this validation
reads only the strip's cells, and the mover's clip still walks
every leg at departure, as it does for every route in the game.
Legs stay within the room grain (maxCaveLegUnits). Where even the
next centre will not validate (two giant triangles), the leg goes
through the shared edge's midpoint, whose halves lie inside one
triangle each.

The one thing the strip does not encode is a block's obstacle
circles: should a finished leg cross one, the whole plan is
handed to the proving pull, whose chords can end short of a
circle and resume past it.
================
*/
func (v *WaterValidator) pullCaveRoute(surface *dungeonSpawnSurface, from, goal, terminal monster.Pose, path []caveCell) ([]monster.Pose, int) {
	edges := make([][4]float64, len(path)-1)
	for k := 0; k+1 < len(path); k++ {
		head, tail, ok := surface.stripEdge(path[k], path[k+1])
		if !ok {
			// No shared edge to reason over: prove it the slow way.
			return v.proveCaveRoute(surface, from, goal, terminal, path)
		}
		edges[k] = [4]float64{head[0], head[2], tail[0], tail[2]}
	}
	points := make([]monster.Pose, 0, 16)
	cells := make([]caveCell, 0, 16)
	current := from
	currentCell := 0 // the path cell current stands in
	emit := func(pose monster.Pose, cell caveCell) {
		points = append(points, pose)
		cells = append(cells, cell)
		current = pose
	}
	for currentCell < len(path)-1 {
		// The terminal closes the block when its chord validates
		// at grain; a longer same-room step is the plan head's
		// direct chord, already tried.
		if routeDistance(current, terminal) <= maxCaveLegUnits &&
			surface.legWalksStrip(path, edges, current, currentCell, terminal, len(path)-1) {
			emit(terminal, path[len(path)-1])
			break
		}
		// The farthest centre whose chord still walks the strip,
		// far first: a near centre can fail the height profile
		// where a farther one, at a kinder angle, passes.
		best := currentCell
		for j := len(path) - 2; j > currentCell; j-- {
			cand := surface.cellPose(from.RegionID, goal.Heading, path[j])
			if routeDistance(current, cand) > maxCaveLegUnits {
				continue
			}
			if surface.legWalksStrip(path, edges, current, currentCell, cand, j) {
				best = j
				break
			}
		}
		if best == currentCell {
			// Even the next centre is out of grain or off-strip:
			// cross at the shared edge, then take the centre; each
			// half lies inside one triangle by construction.
			if joint, ok := surface.pathJoint(from.RegionID, goal.Heading, path[currentCell], path[currentCell+1]); ok {
				emit(joint, path[currentCell+1])
			}
			emit(surface.cellPose(from.RegionID, goal.Heading, path[currentCell+1]), path[currentCell+1])
			currentCell++
			continue
		}
		emit(surface.cellPose(from.RegionID, goal.Heading, path[best]), path[best])
		currentCell = best
	}
	if current != terminal {
		emit(terminal, path[len(path)-1])
	}
	if legsCrossObstacle(surface, from, path[0], points, cells) {
		return v.proveCaveRoute(surface, from, goal, terminal, path)
	}
	return points, 0
}

/*
================
cellPlaneHeight

The height of one cell's own plane over a world point, by the
same local-frame math the floor scan uses.
================
*/
func (s *dungeonSpawnSurface) cellPlaneHeight(at caveCell, x, z float64) (float64, bool) {
	block := &s.blocks[at.block]
	dx, dz := x-block.x, z-block.z
	localX := block.cosYaw*dx + block.sinYaw*dz
	localZ := -block.sinYaw*dx + block.cosYaw*dz
	localY, inside := objectCellPlaneYAt(block.meshes[at.mesh], at.cell, localX, localZ)
	if !inside {
		return 0, false
	}
	return float64(localY) + block.y, true
}

/*
================
chordCrossing

Where along the from-to chord it meets the plan-view edge e, as a
parameter in (0, 1) with the edge's own parameter in [0, 1];
parallel or merely touching at the chord's own endpoints is no
crossing (a chord through a shared vertex then simply does not
validate, which costs a shorter leg, never a wrong one).
================
*/
func chordCrossing(from, to monster.Pose, e [4]float64) (float64, bool) {
	dx, dz := to.X-from.X, to.Z-from.Z
	ex, ez := e[2]-e[0], e[3]-e[1]
	den := dx*ez - dz*ex
	if den == 0 {
		return 0, false
	}
	ox, oz := e[0]-from.X, e[1]-from.Z
	t := (ox*ez - oz*ex) / den
	u := (ox*dz - oz*dx) / den
	if t <= 0 || t >= 1 || u < 0 || u > 1 {
		return 0, false
	}
	return t, true
}

/*
================
legWalksStrip

Whether the chord from a standpoint in path[fromCell] to one in
path[toCell] walks the strip the way the mover's clip will. In
plan view it must cross every shared edge between the two cells,
in order (each piece then lies inside one convex cell). Its
height profile - the strip cells' planes sampled as the clip
samples, at most maxDungeonFloorSampleSpacing apart, following
the walked height - must never step more than
maxDungeonFloorStep at a sample, except at the doorway seam
between blocks, where the clip's passage covers the gap instead.
Endpoints standing outside their own cell refuse.
================
*/
func (s *dungeonSpawnSurface) legWalksStrip(path []caveCell, edges [][4]float64, from monster.Pose, fromCell int, to monster.Pose, toCell int) bool {
	crossings := make([]float64, 0, toCell-fromCell)
	for k := fromCell; k < toCell; k++ {
		t, ok := chordCrossing(from, to, edges[k])
		if !ok {
			return false
		}
		if len(crossings) > 0 && t <= crossings[len(crossings)-1] {
			return false
		}
		crossings = append(crossings, t)
	}
	curY, ok := s.cellPlaneHeight(path[fromCell], from.X, from.Z)
	if !ok {
		return false
	}
	chordLength := routeDistance(from, to)
	bounds := make([]float64, 0, len(crossings)+2)
	bounds = append(bounds, 0)
	bounds = append(bounds, crossings...)
	bounds = append(bounds, 1)
	for i := 0; i+1 < len(bounds); i++ {
		cellIndex := fromCell + i
		t0, t1 := bounds[i], bounds[i+1]
		samples := max(1, int(math.Ceil((t1-t0)*chordLength/maxDungeonFloorSampleSpacing)))
		for j := 0; j < samples; j++ {
			tm := t0 + (t1-t0)*(float64(j)+0.5)/float64(samples)
			h, inside := s.cellPlaneHeight(path[cellIndex], from.X+(to.X-from.X)*tm, from.Z+(to.Z-from.Z)*tm)
			if !inside {
				return false
			}
			if math.Abs(h-curY) <= maxDungeonFloorStep {
				curY = h
				continue
			}
			// Off the walked floor away from the doorway seam:
			// the clip blocks here.
			if !seamInterval(path, fromCell, i) {
				return false
			}
		}
	}
	endY, ok := s.cellPlaneHeight(path[toCell], to.X, to.Z)
	if !ok {
		return false
	}
	if math.Abs(endY-curY) > maxDungeonFloorStep && path[toCell].block == path[toCell-1].block {
		return false
	}
	return true
}

/*
================
seamInterval

Whether sample interval i of a leg (counted from fromCell) touches
the doorway seam: the block changes at one of its bounding edges.
A leg's strip holds at most one seam, its last edge.
================
*/
func seamInterval(path []caveCell, fromCell, interval int) bool {
	for _, k := range [2]int{fromCell + interval - 1, fromCell + interval} {
		if k >= 0 && k+1 < len(path) && path[k].block != path[k+1].block {
			return true
		}
	}
	return false
}

/*
================
legsCrossObstacle

Whether any leg (from, then each point in turn) passes over one
of the obstacle circles of the blocks its endpoints stand in.
Segment distance is plan-view, the gate the mover's own clip
applies before its floor checks.
================
*/
func legsCrossObstacle(surface *dungeonSpawnSurface, from monster.Pose, fromCell caveCell, points []monster.Pose, cells []caveCell) bool {
	prev, prevCell := from, fromCell
	for i, point := range points {
		for _, block := range [2]int{prevCell.block, cells[i].block} {
			for _, obstacle := range surface.blocks[block].obstacles {
				if segmentNearCircle2(prev.X, prev.Z, point.X, point.Z, obstacle.x, obstacle.z, obstacle.radiusSquared) {
					return true
				}
			}
		}
		prev, prevCell = point, cells[i]
	}
	return false
}

/*
================
segmentNearCircle2

Whether a plan-view segment comes within the circle's radius of
its centre.
================
*/
func segmentNearCircle2(ax, az, bx, bz, cx, cz, radiusSquared float64) bool {
	dx, dz := bx-ax, bz-az
	along := 0.0
	if lengthSquared := dx*dx + dz*dz; lengthSquared > 0 {
		along = ((cx-ax)*dx + (cz-az)*dz) / lengthSquared
		if along < 0 {
			along = 0
		}
		if along > 1 {
			along = 1
		}
	}
	nearX, nearZ := ax+dx*along-cx, az+dz*along-cz
	return nearX*nearX+nearZ*nearZ < radiusSquared
}
