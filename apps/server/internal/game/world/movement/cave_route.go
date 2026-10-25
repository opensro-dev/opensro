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
edges the dungeon link pass resolved. This file is that route: a
search over the floor's cell graph (cells inside a mesh, portal
jumps between blocks), string-pulled against the same path oracle
the mover's own legs use, so every leg the route hands out is one
the mover can actually walk. Where the floor has no surface, no
cells under an endpoint, or no route, the planner answers exactly
what callers get today or an honest unresolved route.

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

// maxCaveRouteWork bounds the summed search work (block route plus
// every block's cell search) one plan may spend; the route is
// cached by the mover, and a plan past this is not going to arrive
// either.
const maxCaveRouteWork = 32768

// maxCaveRouteProbes bounds the path-oracle calls the string-pull
// may spend proving legs (a floor-wide pursuit is the expensive
// tail; an ordinary chase is a room or two). Past it the remaining
// corridor points go out unproven and the mover's own departure
// checks take over.
const maxCaveRouteProbes = 2048

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
caveRouteNode

One A* frontier entry: the cell reached, the cost paid to reach it,
and the queue's deterministic order (cheapest, then earliest pushed).
================
*/
type caveRouteNode struct {
	at                caveCell
	cost, priority    float64
	parent            caveCell
	hasParent, closed bool
	order             int
}

/*
================
caveRouteQueue
================
*/
type caveRouteQueue []*caveRouteNode

/*
================
Len
================
*/
func (q caveRouteQueue) Len() int { return len(q) }

/*
================
Less
================
*/
func (q caveRouteQueue) Less(i, j int) bool {
	if q[i].priority != q[j].priority {
		return q[i].priority < q[j].priority
	}
	return q[i].order < q[j].order
}

/*
================
Swap
================
*/
func (q caveRouteQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }

/*
================
Push
================
*/
func (q *caveRouteQueue) Push(v any) { *q = append(*q, v.(*caveRouteNode)) }

/*
================
Pop
================
*/
func (q *caveRouteQueue) Pop() any {
	n := len(*q) - 1
	v := (*q)[n]
	*q = (*q)[:n]
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
planCaveRoute

The cave plane's waypoint owner. A straight chord that already
walks (same room) returns the goal alone, exactly the native
same-sector branch; anything else is routed across the cell graph
and smoothed; the failure answers are today's route or an
unresolved route with the outdoor vocabulary's reasons.
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

	path, probes, found := surface.searchCells(start, target, goal)
	if !found {
		return monster.UnresolvedNavigationRoute(goal, monster.NavigationRouteBlocked).WithProbes(probes)
	}
	points, clipProbes := v.smoothCaveRoute(surface, from, goal, path)
	if len(points) == 0 {
		return monster.UnresolvedNavigationRoute(goal, monster.NavigationRouteBlocked).WithProbes(probes + clipProbes)
	}
	return monster.NewNavigationRoute(goal, points).WithProbes(probes + clipProbes)
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
caveBlockHop

One block of a block route: the block itself, and the portal link
that enters it from the previous one (unset on the first block).
================
*/
type caveBlockHop struct {
	block int
	via   objectNavLink
}

/*
================
searchCells

The route across the floor, searched the way the reference server
reasons about it: a route over the floor's blocks first (portal
link to portal link), then the exact cell path inside each block
between the doorway it entered by and the doorway it leaves by.
That keeps every search inside one block's cells however large
the floor is. Returns the cell path from start to target
inclusive.
================
*/
func (s *dungeonSpawnSurface) searchCells(start, target caveCell, goal monster.Pose) ([]caveCell, int, bool) {
	if start.block == target.block {
		return s.astarCells(start, target)
	}
	hops, work, ok := s.blockRoute(start, target)
	if !ok {
		return nil, work, false
	}
	path := []caveCell{}
	entry := start
	for i, hop := range hops {
		last := i == len(hops)-1
		exit := target
		if !last {
			// The next hop's link is the one that leaves this block.
			via := hops[i+1].via
			mesh := s.blocks[hop.block].meshes[0]
			exit = caveCell{block: hop.block, mesh: 0, cell: int(mesh.outline.srcCell[via.edge])}
		}
		segment, used, ok := s.astarCells(entry, exit)
		work += used
		if !ok || work > maxCaveRouteWork {
			return nil, work, false
		}
		// The segment starts at its own entry cell: across a portal
		// that is the landing cell in this block, not a repeat of
		// the exit cell left behind, so nothing is dropped.
		path = append(path, segment...)
		if !last {
			via := hops[i+1].via
			mesh := s.blocks[hops[i+1].block].meshes[0]
			entry = caveCell{block: hops[i+1].block, mesh: 0, cell: int(mesh.outline.srcCell[via.targetEdge])}
		}
	}
	return path, work, true
}

/*
================
blockRoute

A* over the floor's blocks: leaving a block rides one of its
resolved portal links, priced by the doorway-to-doorway distance
through the block it leaves. Returns the block sequence from the
start's block to the target's, each with the link that leaves it.
================
*/
func (s *dungeonSpawnSurface) blockRoute(start, target caveCell) ([]caveBlockHop, int, bool) {
	type blockNode struct {
		block    int
		entry    caveCell
		cost     float64
		priority float64
		parent   int
		via      objectNavLink
		closed   bool
		order    int
	}
	goalX, goalY, goalZ := s.cellCentroid(target)
	nodes := map[int]*blockNode{}
	queue := &caveRouteQueue{}
	order := 0
	push := func(block int, entry caveCell, cost float64, parent int, via objectNavLink) {
		x, y, z := s.cellCentroid(entry)
		node := &blockNode{
			block: block, entry: entry, cost: cost, parent: parent, via: via,
			priority: cost + math.Sqrt((x-goalX)*(x-goalX)+(y-goalY)*(y-goalY)+(z-goalZ)*(z-goalZ)),
			order:    order,
		}
		order++
		nodes[block] = node
		heap.Push(queue, &caveRouteNode{at: caveCell{block: block}, priority: node.priority, order: node.order})
	}
	push(start.block, start, 0, -1, objectNavLink{})
	expansions := 0
	for queue.Len() > 0 {
		queued := heap.Pop(queue).(*caveRouteNode)
		node := nodes[queued.at.block]
		if node == nil || node.closed || node.order != queued.order {
			continue
		}
		node.closed = true
		expansions++
		if node.block == target.block {
			hops := []caveBlockHop{}
			for step := node; ; step = nodes[step.parent] {
				hops = append(hops, caveBlockHop{block: step.block, via: step.via})
				if step.parent < 0 {
					break
				}
			}
			for i, j := 0, len(hops)-1; i < j; i, j = i+1, j-1 {
				hops[i], hops[j] = hops[j], hops[i]
			}
			return hops, expansions, true
		}
		if len(s.blocks[node.block].meshes) != 1 {
			continue
		}
		entryX, entryY, entryZ := s.cellCentroid(node.entry)
		for _, link := range s.objects[node.block].placement.links {
			across, ok := s.ordinalIndex[link.target]
			if !ok || len(s.blocks[across].meshes) != 1 {
				continue
			}
			mesh := s.blocks[node.block].meshes[0]
			door := caveCell{block: node.block, mesh: 0, cell: int(mesh.outline.srcCell[link.edge])}
			doorX, doorY, doorZ := s.cellCentroid(door)
			landingMesh := s.blocks[across].meshes[0]
			landing := caveCell{block: across, mesh: 0, cell: int(landingMesh.outline.srcCell[link.targetEdge])}
			landX, landY, landZ := s.cellCentroid(landing)
			cost := node.cost +
				math.Sqrt((doorX-entryX)*(doorX-entryX)+(doorY-entryY)*(doorY-entryY)+(doorZ-entryZ)*(doorZ-entryZ)) +
				math.Sqrt((landX-doorX)*(landX-doorX)+(landY-doorY)*(landY-doorY)+(landZ-doorZ)*(landZ-doorZ))
			if seen := nodes[across]; seen == nil || (!seen.closed && cost < seen.cost) {
				push(across, landing, cost, node.block, link)
			}
		}
	}
	return nil, expansions, false
}

/*
================
astarCells

A* over one mesh's cells, following the internal cell adjacency.
Costs are centroid distances. Returns the cell path from start to
target inclusive.
================
*/
func (s *dungeonSpawnSurface) astarCells(start, target caveCell) ([]caveCell, int, bool) {
	goalX, goalY, goalZ := s.cellCentroid(target)
	nodes := map[caveCell]*caveRouteNode{}
	queue := &caveRouteQueue{}
	order := 0
	push := func(at caveCell, cost float64, parent caveCell, hasParent bool) {
		x, y, z := s.cellCentroid(at)
		node := &caveRouteNode{
			at: at, cost: cost, parent: parent, hasParent: hasParent,
			priority: cost + math.Sqrt((x-goalX)*(x-goalX)+(y-goalY)*(y-goalY)+(z-goalZ)*(z-goalZ)),
			order:    order,
		}
		order++
		nodes[at] = node
		heap.Push(queue, node)
	}
	push(start, 0, caveCell{}, false)
	expansions := 0
	for queue.Len() > 0 {
		node := heap.Pop(queue).(*caveRouteNode)
		if node.closed || nodes[node.at] != node {
			continue
		}
		node.closed = true
		expansions++
		if expansions > maxCaveRouteCells {
			return nil, expansions, false
		}
		if node.at == target {
			path := []caveCell{node.at}
			for step := node; step.hasParent; {
				step = nodes[step.parent]
				path = append(path, step.at)
			}
			for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
				path[i], path[j] = path[j], path[i]
			}
			return path, expansions, true
		}
		at := node.at
		mesh := s.blocks[at.block].meshes[at.mesh]
		fromX, fromY, fromZ := s.cellCentroid(at)
		for _, neighbour := range mesh.cellNeighbors[at.cell] {
			next := caveCell{block: at.block, mesh: at.mesh, cell: neighbour}
			x, y, z := s.cellCentroid(next)
			cost := node.cost + math.Sqrt((x-fromX)*(x-fromX)+(y-fromY)*(y-fromY)+(z-fromZ)*(z-fromZ))
			if seen := nodes[next]; seen == nil || (!seen.closed && cost < seen.cost) {
				push(next, cost, at, true)
			}
		}
	}
	return nil, expansions, false
}

/*
================
smoothCaveRoute

Pulls the cell path tight into waypoints the mover can walk. The
candidates are the path cells' own centres - every one stands on
the floor it belongs to - and from each adopted point the pull
gallops forward (exponential probes, then a binary search between
the last centre that arrives and the first that does not) against
the same path oracle the mover's legs use, so straight rooms
collapse to a few legs while bending ones keep their corners. A
centre whose own chord will not arrive (a hairpin edge) goes out
unproven; the mover revalidates every leg at departure either
way, and a replan from any adopted point is short.
================
*/
func (v *WaterValidator) smoothCaveRoute(surface *dungeonSpawnSurface, from, goal monster.Pose, path []caveCell) ([]monster.Pose, int) {
	probes := 0
	points := make([]monster.Pose, 0, 16)
	current := from
	// cellPose is the standable centre of path cell i, snapped to
	// the floor with its own height as the hint: the cave's floors
	// stack, so a distant hint would pick the wrong level.
	cellPose := func(i int) monster.Pose {
		x, y, z := surface.cellCentroid(path[i])
		if floorY, ok := surface.heightAt(x, y, z); ok {
			y = floorY
		}
		return monster.Pose{RegionID: from.RegionID, X: x, Y: y, Z: z, Heading: goal.Heading}
	}
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

	next := 1 // the first path cell not yet adopted
	for next < len(path) {
		if probes < maxCaveRouteProbes && arrives(goal) {
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
			if joint, ok := surface.pathJoint(from.RegionID, goal.Heading, path[next-1], path[next]); ok && arrives(joint) {
				emit(joint)
			}
			emit(cellPose(next))
			next++
			continue
		}
		emit(cellPose(lastGood))
		next = lastGood + 1
	}
	return append(points, goal), probes
}

/*
================
pathJoint

The standpoint on the edge two neighbouring path cells share:
inside one block, the midpoint of the two vertices both triangles
own; across a portal, the middle of the portal edge. A chord
through the joint stays within one triangle per half, which the
long centre-to-centre chord across a giant cell pair does not.
================
*/
func (s *dungeonSpawnSurface) pathJoint(regionID uint16, heading uint16, a, b caveCell) (monster.Pose, bool) {
	pose := func(x, y, z float64) monster.Pose {
		if floorY, ok := s.heightAt(x, y, z); ok {
			y = floorY
		}
		return monster.Pose{RegionID: regionID, X: x, Y: y, Z: z, Heading: heading}
	}
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
			return monster.Pose{}, false
		}
		block := &s.blocks[a.block]
		head, tail := int(shared[0])*3, int(shared[1])*3
		localX := (float64(mesh.vertices[head]) + float64(mesh.vertices[tail])) / 2
		localY := (float64(mesh.vertices[head+1]) + float64(mesh.vertices[tail+1])) / 2
		localZ := (float64(mesh.vertices[head+2]) + float64(mesh.vertices[tail+2])) / 2
		return pose(
			block.cosYaw*localX-block.sinYaw*localZ+block.x,
			localY+block.y,
			block.sinYaw*localX+block.cosYaw*localZ+block.z), true
	}
	// Across a portal: the link whose edge this cell owns and whose
	// far edge the next cell owns - several doorways can join the
	// same pair of blocks, and only this one is on the path.
	meshA := s.blocks[a.block].meshes[a.mesh]
	for _, link := range s.objects[a.block].placement.links {
		across, ok := s.ordinalIndex[link.target]
		if !ok || across != b.block || int(meshA.outline.srcCell[link.edge]) != a.cell {
			continue
		}
		meshB := s.blocks[b.block].meshes[b.mesh]
		if int(meshB.outline.srcCell[link.targetEdge]) != b.cell {
			continue
		}
		placement := s.objects[a.block].placement
		head := portalEndpoint(placement, meshA, link.edge, 0)
		tail := portalEndpoint(placement, meshA, link.edge, 1)
		return pose((head[0]+tail[0])/2, (head[1]+tail[1])/2, (head[2]+tail[2])/2), true
	}
	return monster.Pose{}, false
}
