/*
===========================================================================

dungeon_walk.go - the original's dungeon walk, cell by cell (#1728)

The original moves a mover through a dungeon by walking the current
block's mesh edge by edge from the mover's retained cell (dungeon_owner.go),
in XZ only:

	passable internal edge (4)  the next cell; no height is ever compared
	one-sided edge (2 / 1)      stops the leg from the blocked side, bit 0
	wall (flags & 0x12)         stops the leg at the contact, bit 0
	doorway (8), linked         the leg hands the mover to the neighbour
	                            block's landing cell (bits 1 and 4) and the
	                            query walks on from there
	doorway (8), no link        bit 28

then tests the block's obstacle circles against the leg (bit 0 at the
first contact; a start inside a circle heading into it is refused where
it stands instead, the blocked result). walkDungeon is one leg: a
doorway leg reports a
continuation from the landing cell, and the query loop every plane shares
(clipMovementPathFromWithWalk: six calls, a leg that started within five
units settles) walks on from there. The rest's height is its cell's
plane; the requested height never enters. Client twin 428930; the
server's walk is described in words (#1712, #1728).

===========================================================================
*/
package movement

import (
	"math"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// dungeonOwnerSlackUnits is how far outside its retained cell a leg
	// may start and still keep the cell (the native start handling moves
	// it onto the cell); a farther owner is stale and the start relocates.
	// A doorway hand-over already enters the landing cell (enterLanding);
	// this covers a mover's own retained cell after a sub-unit move such
	// as wire truncation of its coordinates.
	dungeonOwnerSlackUnits = 1.0
	// dungeonWalkEndEpsilon is how close to the chord's end a leg's last
	// span must reach to count as arrived.
	dungeonWalkEndEpsilon = 1e-9
	// dungeonDoorwayFlag marks an outline edge as a doorway.
	dungeonDoorwayFlag = 0x8
	// dungeonSideFlags are the one-sided bits of an internal edge: 2
	// blocks leaving its source cell, 1 leaving its destination cell.
	dungeonSideFromSource = 0x2
	dungeonSideFromDest   = 0x1
)

/*
================
dungeonWalkState

Where a walk stands between legs: the block (an index into the surface's
blocks and objects), the cell, and the world point the next leg starts
from.
================
*/
type dungeonWalkState struct {
	block, cell int
	x, z        float64
}

/*
================
WaterValidator.walkDungeon

One leg of the original's dungeon move from -> to (walkLeg), starting in
the retained cell (walkStart). A leg that crosses a linked doorway reports
a continuation at the crossing, owned by the landing cell; the shared
query loop decides whether it walks on.
================
*/
func (v *WaterValidator) walkDungeon(from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) ClipReport {
	report := ClipReport{Outcome: ClipBlocked, Class: ClipClassObject, Rest: from, NativeResult: monster.NavResultBlocked}
	if from.RegionID != to.RegionID {
		return report
	}
	v.dungeonSpawnOnce.Do(func() { v.dungeonSpawnSurfaces = v.loadDungeonSpawnSurfaces() })
	surface := v.dungeonSpawnSurfaces[from.RegionID]
	if surface == nil {
		return report
	}
	state, ok := surface.walkStart(from, fromOwner)
	if !ok {
		return report
	}
	end, bits, work := surface.walkLeg(state, to)
	switch {
	case bits&(monster.NavResultBlocked|monster.NavResultClipped) != 0:
		return surface.walkReport(from, end, bits, work)
	case bits&dungeonResultDoorway != 0:
		continued := surface.walkReport(from, end, bits, work)
		continued.Outcome, continued.Class, continued.continuation = ClipArrived, "", true
		return continued
	}
	return surface.arrivedReport(from, to, end, bits, work)
}

/*
================
dungeonSpawnSurface.walkStart

The cell a leg starts in: the retained owner's while the start lies on or
within dungeonOwnerSlackUnits of its cell (the original walks from the
retained cell, moving the start onto it), otherwise the cell under the
start nearest its height - the relocation rule.
================
*/
func (s *dungeonSpawnSurface) walkStart(from simulation.Spawn, owner simulation.NavOwner) (dungeonWalkState, bool) {
	if owner.Kind == simulation.NavOwnerDungeon && owner.Object.Surface == from.RegionID {
		if index, ok := s.ordinalIndex[int(owner.Object.Object)]; ok {
			block := &s.blocks[index]
			cell := int(owner.Object.Cell)
			if mesh := block.meshes[0]; cell >= 0 && cell < mesh.cellCount() {
				lx, lz := block.local(from.X, from.Z)
				if cellOutsideDistance(mesh, cell, lx, lz) <= dungeonOwnerSlackUnits {
					return dungeonWalkState{block: index, cell: cell, x: from.X, z: from.Z}, true
				}
			}
		}
	}
	located, ok := s.dungeonOwnerAt(from.RegionID, from.X, from.Y, from.Z, simulation.NavOwner{})
	if !ok {
		return dungeonWalkState{}, false
	}
	index, ok := s.ordinalIndex[int(located.Object.Object)]
	if !ok {
		return dungeonWalkState{}, false
	}
	return dungeonWalkState{block: index, cell: int(located.Object.Cell), x: from.X, z: from.Z}, true
}

// dungeonResultDoorway is the walk's bit 1: the leg crossed a doorway.
const dungeonResultDoorway = 0x2

/*
================
dungeonSpawnSurface.walkLeg

One leg from state toward to inside state's block: the cell trace, then
what stopped it - the destination, a doorway (handing over to the linked
block), a wall, a one-sided edge, a dead doorway - and the block's
obstacle circles. Returns where the leg ends, its bits and its work.
================
*/
func (s *dungeonSpawnSurface) walkLeg(state dungeonWalkState, to simulation.Spawn) (dungeonWalkState, uint32, int) {
	object := &s.objects[state.block]
	block := &s.blocks[state.block]
	mesh := object.meshes[0]
	x0, z0 := block.local(state.x, state.z)
	x1, z1 := block.local(to.X, to.Z)
	// The native start handling moves a start outside the retained cell
	// onto it (ownedCellStart); the chord's end is kept.
	x0, z0 = ownedCellStart(mesh, state.cell, x0, z0)
	side := dungeonSideCrossing(mesh)
	spans := traceMeshCells(mesh, state.cell, x0, z0, x1, z1, 0, side)
	work := len(spans) + mesh.cellCount()
	endT, lastCell := 0.0, state.cell
	if n := len(spans); n > 0 {
		endT, lastCell = spans[n-1].to, spans[n-1].cell
	}
	stop, inside := s.obstacleContact(state.block, state.x, state.z, to.X, to.Z, endT)
	if inside {
		// The leg starts inside a circle and heads into it: the
		// original refuses the step where it stands (the blocked
		// result), rather than clipping it at the start point.
		return state, monster.NavResultBlocked, work
	}
	if stop < endT {
		// A circle stops the leg before the mesh does: bit 0 at the rim,
		// in the cell the chord had reached there.
		cell := lastCell
		for _, span := range spans {
			if stop >= span.from && stop <= span.to {
				cell = span.cell
			}
		}
		return s.pulledBack(state, to, stop, cell), monster.NavResultClipped, work
	}
	if endT >= 1-dungeonWalkEndEpsilon {
		return dungeonWalkState{block: state.block, cell: lastCell, x: to.X, z: to.Z}, 0, work
	}
	edge, outline, found := crossedEdge(mesh, lastCell, x0, z0, x1, z1, endT)
	if !found {
		return s.pulledBack(state, to, endT, lastCell), monster.NavResultClipped, work
	}
	if !outline {
		// A one-sided internal edge stopped the trace.
		return s.contactAt(state, to, mesh, &mesh.internal, edge, lastCell, x0, z0, x1, z1), monster.NavResultClipped, work
	}
	flags := mesh.outline.flags[edge]
	if flags&dungeonDoorwayFlag != 0 {
		if next, ok := s.landing(state.block, edge); ok {
			wx, wz := state.x+(to.X-state.x)*endT, state.z+(to.Z-state.z)*endT
			next.x, next.z = s.enterLanding(next, wx, wz)
			return next, dungeonResultDoorway | dungeonResultSlid, work
		}
		return s.contactAt(state, to, mesh, &mesh.outline, edge, lastCell, x0, z0, x1, z1), monster.NavResultBlocked, work
	}
	return s.contactAt(state, to, mesh, &mesh.outline, edge, lastCell, x0, z0, x1, z1), dungeonOutlineResult(flags), work
}

// dungeonResultSlid is the walk's bit 4, which a doorway crossing also
// carries: the query walks on while it is set.
const dungeonResultSlid = 0x10

/*
================
cellOutsideDistance

How far a local point lies outside a cell's triangle in XZ: 0 inside,
otherwise the distance to the triangle's nearest edge.
================
*/
func cellOutsideDistance(mesh *objectNavMesh, cell int, x, z float64) float64 {
	if _, inside := objectCellPlaneYAt(mesh, cell, x, z); inside {
		return 0
	}
	ids := [3]uint16{mesh.cellA[cell], mesh.cellB[cell], mesh.cellC[cell]}
	best := math.Inf(1)
	for k, a := range ids {
		b := ids[(k+1)%3]
		ax, az := float64(mesh.vertices[int(a)*3]), float64(mesh.vertices[int(a)*3+2])
		bx, bz := float64(mesh.vertices[int(b)*3]), float64(mesh.vertices[int(b)*3+2])
		sx, sz := bx-ax, bz-az
		u := 0.0
		if l := sx*sx + sz*sz; l > 0 {
			u = math.Max(0, math.Min(1, ((x-ax)*sx+(z-az)*sz)/l))
		}
		best = math.Min(best, math.Hypot(x-(ax+sx*u), z-(az+sz*u)))
	}
	return best
}

/*
================
dungeonSideCrossing

The crossing test traceMeshCells applies inside a dungeon mesh: a one-sided
internal edge refuses the side its bits block (2 from its source cell, 1
from its destination cell). nil for a mesh with no one-sided edge.
================
*/
func dungeonSideCrossing(mesh *objectNavMesh) func(from, to int) bool {
	sided := false
	for _, flags := range mesh.internal.flags {
		if flags&(dungeonSideFromSource|dungeonSideFromDest) != 0 {
			sided = true
			break
		}
	}
	if !sided {
		return nil
	}
	return func(from, to int) bool {
		for e, flags := range mesh.internal.flags {
			src, dst := int(mesh.internal.srcCell[e]), int(mesh.internal.dstCell[e])
			switch {
			case src == from && dst == to && flags&dungeonSideFromSource != 0:
				return false
			case dst == from && src == to && flags&dungeonSideFromDest != 0:
				return false
			}
		}
		return true
	}
}

/*
================
crossedEdge

The edge of cell the chord leaves through at fraction t: an outline edge,
or an internal one (a one-sided edge refused the crossing). found is
false when no edge of the cell is crossed there (float slack at a
vertex).
================
*/
func crossedEdge(mesh *objectNavMesh, cell int, x0, z0, x1, z1, t float64) (int, bool, bool) {
	best, bestOutline, bestGap := -1, false, math.Inf(1)
	test := func(edges *objectNavEdges, outline bool) {
		for e := range edges.vertA {
			if int(edges.srcCell[e]) != cell && int(edges.dstCell[e]) != cell {
				continue
			}
			a, b := int(edges.vertA[e])*3, int(edges.vertB[e])*3
			ax, az := float64(mesh.vertices[a]), float64(mesh.vertices[a+2])
			sx, sz := float64(mesh.vertices[b])-ax, float64(mesh.vertices[b+2])-az
			rx, rz := x1-x0, z1-z0
			den := rx*sz - rz*sx
			if math.Abs(den) < 1e-12 {
				continue
			}
			te, u := ((ax-x0)*sz-(az-z0)*sx)/den, ((ax-x0)*rz-(az-z0)*rx)/den
			if u < -1e-6 || u > 1+1e-6 {
				continue
			}
			if gap := math.Abs(te - t); gap < bestGap {
				best, bestOutline, bestGap = e, outline, gap
			}
		}
	}
	test(&mesh.outline, true)
	test(&mesh.internal, false)
	if best < 0 || bestGap > 1e-4 {
		return 0, false, false
	}
	return best, bestOutline, true
}

/*
================
dungeonSpawnSurface.landing

The block and cell a doorway hands a mover to: the linked block's landing
cell, the source cell of the linked outline edge there (resolveDungeonLinks
pairs the two edges).
================
*/
func (s *dungeonSpawnSurface) landing(block, edge int) (dungeonWalkState, bool) {
	for _, link := range s.objects[block].placement.links {
		if link.edge != edge {
			continue
		}
		target, ok := s.ordinalIndex[link.target]
		if !ok {
			return dungeonWalkState{}, false
		}
		mesh := s.objects[target].meshes[0]
		if link.targetEdge < 0 || link.targetEdge >= len(mesh.outline.srcCell) {
			return dungeonWalkState{}, false
		}
		return dungeonWalkState{block: target, cell: int(mesh.outline.srcCell[link.targetEdge])}, true
	}
	return dungeonWalkState{}, false
}

/*
================
dungeonSpawnSurface.enterLanding

Where a doorway crossing enters the landing cell. Linked doorways are
matched within five units (resolveDungeonLinks), so the crossing on the
source block's edge can lie off the landing cell - by up to 4.2 units on
the shipped tomb floors. The original enters the landing cell as it
enters any retained cell from outside: the line from the point to the
cell's centroid is cut at the cell's edge and the cut nudged inward
(ownedCellStart). A point already inside stays.
================
*/
func (s *dungeonSpawnSurface) enterLanding(landing dungeonWalkState, x, z float64) (float64, float64) {
	block := &s.blocks[landing.block]
	lx, lz := block.local(x, z)
	lx, lz = ownedCellStart(block.meshes[0], landing.cell, lx, lz)
	return block.world(lx, lz)
}

/*
================
dungeonSpawnSurface.contactAt

The native rest for an edge contact (nativeObjectContact: the crossing
point moved into the approached cell), in world coordinates, owned by
that cell.
================
*/
func (s *dungeonSpawnSurface) contactAt(state dungeonWalkState, to simulation.Spawn, mesh *objectNavMesh, edges *objectNavEdges, edge, cell int, x0, z0, x1, z1 float64) dungeonWalkState {
	source := int(edges.srcCell[edge]) == cell
	contact := nativeObjectContact(mesh, edges, edge, source, x0, z0, 0, x1, z1)
	if !contact.valid {
		return s.pulledBack(state, to, 0, cell)
	}
	// The native contact stores the world point as float32.
	wx, wz := s.blocks[state.block].world(contact.x, contact.z)
	return dungeonWalkState{block: state.block, cell: cell, x: contactF32(wx), z: contactF32(wz)}
}

/*
================
dungeonSpawnSurface.pulledBack

The point a hair short of chord fraction t on the leg (the clip's rest
pull-back), in cell.
================
*/
func (s *dungeonSpawnSurface) pulledBack(state dungeonWalkState, to simulation.Spawn, t float64, cell int) dungeonWalkState {
	dx, dz := to.X-state.x, to.Z-state.z
	t = math.Max(0, t-clipRestPullback/math.Max(math.Max(math.Abs(dx), math.Abs(dz)), clipRestPullback))
	return dungeonWalkState{block: state.block, cell: cell, x: state.x + dx*t, z: state.z + dz*t}
}

/*
================
dungeonSpawnSurface.obstacleContact

The first obstacle circle of the leg's block the chord from (x0, z0) to
(x1, z1) meets before fraction limit, as a fraction; limit when none.
The original tests each block's circles as 2D XZ circles after its mesh
walk (the record's height is never read). A start inside a circle
heading into it - the start-to-centre vector against the chord below
zero - refuses the whole leg where it stands: the second return
reports that refusal and no rim fraction applies. A start inside
heading out is no contact at all. A chord with no x-extent in the
block's own frame is never tested either: the original's line form has
no such line, so its circle test finds nothing along it.
================
*/
func (s *dungeonSpawnSurface) obstacleContact(block int, x0, z0, x1, z1, limit float64) (float64, bool) {
	dx, dz := x1-x0, z1-z0
	b := &s.blocks[block]
	if b.cosYaw*dx+b.sinYaw*dz == 0 {
		return limit, false
	}
	best := limit
	a := dx*dx + dz*dz
	for _, o := range b.obstacles {
		x, z := x0-o.x, z0-o.z
		dot, c := x*dx+z*dz, x*x+z*z-o.radiusSquared
		if c < 0 {
			if dot >= 0 {
				continue
			}
			return 0, true
		}
		disc := dot*dot - a*c
		if disc < 0 {
			continue
		}
		if t := (-dot - math.Sqrt(disc)) / a; t >= 0 && t <= 1 && t < best {
			best = t
		}
	}
	return best, false
}

/*
================
dungeonSpawnSurface.walkReport

A walk that stopped short: blocked at end, its height the cell's plane.
================
*/
func (s *dungeonSpawnSurface) walkReport(from simulation.Spawn, end dungeonWalkState, result uint32, work int) ClipReport {
	rest := from
	rest.X, rest.Z = end.x, end.z
	rest.Y = s.cellHeight(end, from.Y)
	return ClipReport{Outcome: ClipBlocked, Class: ClipClassObject, Rest: rest, NativeResult: result,
		RestOwner: s.walkOwner(from.RegionID, end), Work: work}
}

/*
================
dungeonSpawnSurface.arrivedReport

A walk that reached to: arrived on its cell's plane.
================
*/
func (s *dungeonSpawnSurface) arrivedReport(from, to simulation.Spawn, end dungeonWalkState, result uint32, work int) ClipReport {
	rest := to
	rest.Y = s.cellHeight(end, from.Y)
	return ClipReport{Outcome: ClipArrived, Rest: rest, NativeResult: result &^ (dungeonResultDoorway | dungeonResultSlid),
		RestOwner: s.walkOwner(from.RegionID, end), Work: work}
}

/*
================
dungeonSpawnSurface.walkOwner
================
*/
func (s *dungeonSpawnSurface) walkOwner(region uint16, end dungeonWalkState) simulation.NavOwner {
	return dungeonOwner(region, s.blocks[end.block].ordinal, 0, end.cell)
}

/*
================
dungeonSpawnSurface.cellHeight

The height of a walk's point on its cell's plane; fallback where the
plane does not reach it (a point on the cell's rim).
================
*/
func (s *dungeonSpawnSurface) cellHeight(end dungeonWalkState, fallback float64) float64 {
	block := &s.blocks[end.block]
	lx, lz := block.local(end.x, end.z)
	if y, ok := objectCellPlaneYAt(block.meshes[0], end.cell, lx, lz); ok {
		return float64(y) + block.y
	}
	return fallback
}

/*
================
dungeonSpawnBlock.local
================
*/
func (b *dungeonSpawnBlock) local(x, z float64) (float64, float64) {
	dx, dz := x-b.x, z-b.z
	return b.cosYaw*dx + b.sinYaw*dz, -b.sinYaw*dx + b.cosYaw*dz
}

/*
================
dungeonSpawnBlock.world
================
*/
func (b *dungeonSpawnBlock) world(x, z float64) (float64, float64) {
	return b.cosYaw*x - b.sinYaw*z + b.x, b.sinYaw*x + b.cosYaw*z + b.z
}
