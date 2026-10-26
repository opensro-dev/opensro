/*
===========================================================================

dungeon_walk_test.go - the original's dungeon walk, rule by rule (#1728)

No height test on a passable edge; a one-sided edge blocks only its side;
a doorway hands the mover to the landing cell, entering it as the original
enters a retained cell from outside, however far the two blocks' doorway
edges sit apart; and the upper-floor chord #1712 found pinned now moves.

===========================================================================
*/
package movement

import (
	"math"
	"testing"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
stepMesh

Two cells side by side over x 0..200, z 0..100, joined by one internal
edge at x 100 with the given flags; the east cell stands rise units
higher than the west.
================
*/
func stepMesh(rise float32, internalFlags byte) *objectNavMesh {
	m := &objectNavMesh{
		vertices: []float32{0, 0, 0, 100, 0, 0, 100, 0, 100, 0, 0, 100, 200, rise, 0, 200, rise, 100},
		// west: 0-1-2 and 0-2-3 would be two cells; keep one cell per side
		// by using the quad's triangle fans: west (0,1,2) is enough for the
		// probe line along z 30.
		cellA: []uint16{0, 1}, cellB: []uint16{1, 4}, cellC: []uint16{2, 2},
		minX: 0, maxX: 200, minY: 0, maxY: float64(rise), minZ: 0, maxZ: 100,
		internal: objectNavEdges{vertA: []uint16{1}, vertB: []uint16{2}, srcCell: []uint16{0}, dstCell: []uint16{1}, flags: []byte{internalFlags}},
		outline: objectNavEdges{
			vertA: []uint16{0, 2, 1, 4}, vertB: []uint16{1, 0, 4, 2},
			srcCell: []uint16{0, 0, 1, 1}, dstCell: []uint16{65535, 65535, 65535, 65535},
			flags: []byte{3, 3, 3, 3},
		},
	}
	m.cellNeighbors = buildObjectCellNeighbors(m)
	return m
}

/*
================
stepValidator
================
*/
func stepValidator(mesh *objectNavMesh) *WaterValidator {
	blocks := []dungeonSpawnBlock{{ordinal: 0, meshes: []*objectNavMesh{mesh}}}
	v := &WaterValidator{dungeonSpawnSurfaces: map[uint16]*dungeonSpawnSurface{
		donwhangCaveRegion: newDungeonSpawnSurface(worldgeom.DungeonMessageGrid{}, blocks),
	}}
	v.dungeonSpawnOnce.Do(func() {})
	return v
}

/*
================
TestDungeonWalkCrossesAHeightStep

A passable internal edge is crossed whatever the height on either side:
the walk moves cell to cell in XZ and stands on the reached cell's plane.
The height-sampled floor walk refused any step over two units.
================
*/
func TestDungeonWalkCrossesAHeightStep(t *testing.T) {
	v := stepValidator(stepMesh(10, 4))
	from, to := donwhangCaveSpawn(80, 0, 30), donwhangCaveSpawn(140, 0, 40)
	got := v.ClipMovementPath(from, to)
	if got.Outcome != ClipArrived || got.NativeResult != 0 {
		t.Fatalf("across a 10-unit step: %v %#x at %+v, want arrived", got.Outcome, got.NativeResult, got.Rest)
	}
	if got.Rest.Y <= 1 {
		t.Fatalf("rests at %+v, want the raised cell's plane", got.Rest)
	}
}

/*
================
TestDungeonWalkOneSidedEdge

An internal edge carrying side bit 2 blocks leaving its source cell, bit 0
at the edge; walking in from its destination cell passes.
================
*/
func TestDungeonWalkOneSidedEdge(t *testing.T) {
	v := stepValidator(stepMesh(0, 4|dungeonSideFromSource))
	west, east := donwhangCaveSpawn(80, 0, 30), donwhangCaveSpawn(140, 0, 40)
	if got := v.ClipMovementPath(west, east); got.Outcome != ClipBlocked || got.NativeResult != monster.NavResultClipped || got.Rest.X >= 100 {
		t.Fatalf("out of the source side: %v %#x at %+v, want clipped short of x 100", got.Outcome, got.NativeResult, got.Rest)
	}
	if got := v.ClipMovementPath(east, west); got.Outcome != ClipArrived {
		t.Fatalf("in from the destination side: %v %#x at %+v, want arrived", got.Outcome, got.NativeResult, got.Rest)
	}
}

/*
================
TestDungeonWalkDoorwayEntersTheLandingCell

Linked doorways are matched within five units, so on the shipped tomb
floor 0x8004 the doorway from block 17 (edge 9) lies up to 3.4 units off
block 21's landing cell. A step across it hands the mover to block 21 at
a point inside the landing cell - the original enters it as it enters a
retained cell from outside - and the next step walks on from there.
================
*/
func TestDungeonWalkDoorwayEntersTheLandingCell(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	const tomb = 0x8004
	surface := v.dungeonSpawnSurfaces[tomb]
	if surface == nil {
		t.Skip("tomb floor 0x8004 is not in the shipped set")
	}
	index, ok := surface.ordinalIndex[17]
	if !ok {
		t.Fatal("tomb block 17 is missing")
	}
	source := &surface.objects[index]
	var link objectNavLink
	found := false
	for _, l := range source.placement.links {
		if l.edge == 9 {
			link, found = l, true
		}
	}
	if !found || link.target != 21 {
		t.Fatalf("block 17 edge 9 links %+v, want block 21", source.placement.links)
	}
	mesh := source.meshes[0]
	a, b := int(mesh.outline.vertA[9])*3, int(mesh.outline.vertB[9])*3
	mx, mz := (float64(mesh.vertices[a])+float64(mesh.vertices[b]))/2, (float64(mesh.vertices[a+2])+float64(mesh.vertices[b+2]))/2
	cx, cz := objectCellCentroid2D(mesh, int(mesh.outline.srcCell[9]))
	dx, dz := mx-cx, mz-cz
	length := math.Hypot(dx, dz)
	fromX, fromZ := surface.blocks[index].world(cx, cz)
	toX, toZ := surface.blocks[index].world(mx+dx/length*3, mz+dz/length*3)
	from := simulation.Spawn{RegionID: tomb, X: fromX, Y: surface.blocks[index].y, Z: fromZ}
	if y, ok := surface.heightAt(from.X, from.Y, from.Z); ok {
		from.Y = y
	}
	to := simulation.Spawn{RegionID: tomb, X: toX, Y: from.Y, Z: toZ}
	leg := v.walkDungeon(from, simulation.NavOwner{}, to)
	if !leg.continuation || leg.NativeResult != dungeonResultDoorway|dungeonResultSlid {
		t.Fatalf("the leg toward the doorway: %+v, want a doorway hand-over", leg)
	}
	if leg.RestOwner.Kind != simulation.NavOwnerDungeon || leg.RestOwner.Object.Object != 21 {
		t.Fatalf("handed to %+v, want block 21", leg.RestOwner)
	}
	landing := surface.ordinalIndex[21]
	lx, lz := surface.blocks[landing].local(leg.Rest.X, leg.Rest.Z)
	if d := cellOutsideDistance(surface.objects[landing].meshes[0], int(leg.RestOwner.Object.Cell), lx, lz); d != 0 {
		t.Fatalf("handed over %.3f outside the landing cell", d)
	}
}

/*
================
TestDungeonWalkUpperFloorChordMoves

#1712's replay: a monster on Donwhang's upper floor (y 280) aiming at a
route point below. The height-sampled clip answered bit 28 with the rest
at the start, so the native route never moved it. The walk follows the
upper floor's cells: it either arrives or stops at a wall (bit 0) away
from the start.
================
*/
func TestDungeonWalkUpperFloorChordMoves(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	from := simulation.Spawn{RegionID: planeCaveRegion, X: 2187.683706065458, Y: 280.2384697811111, Z: 261.2369038462373}
	to := simulation.Spawn{RegionID: planeCaveRegion, X: 1922, Y: 280.0007019042969, Z: -13}
	got := v.ClipMovementPath(from, to)
	if got.NativeResult&monster.NavResultBlocked != 0 {
		t.Fatalf("upper-floor chord: %v %#x at %+v, want no bit 28", got.Outcome, got.NativeResult, got.Rest)
	}
	if simulation.WorldDistance2D(from, got.Rest) <= 1 {
		t.Fatalf("upper-floor chord rests at its start %+v", got.Rest)
	}
}
