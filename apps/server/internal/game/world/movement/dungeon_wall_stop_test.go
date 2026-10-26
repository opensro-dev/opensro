/*
===========================================================================

dungeon_wall_stop_test.go - a dungeon wall stops the walk at the contact
(#1728, #1729)

On the server every ordinary move runs the dungeon walk with its stop
flag set (the flag's only writer sets it; #1729's closure), so a wall
contact is bit 0 with the rest at the contact - never the slide the
walk's stop-clear arm would make (bit 0x10 and a re-aimed leg along the
edge). The bit is pinned in dungeon_wall_result_test.go; what is pinned
here is the rest itself: on the chord, at the crossing with the wall
edge, owned by the cell that was walked out of. A slide displaces the
rest along the wall by the projected remainder - units on these chords -
so a rest that stays at the crossing is the stop behaviour, measured.

===========================================================================
*/
package movement

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// wallStopRestSlack bounds how far a wall rest may sit from the chord's
// crossing with the edge: the native contact nudges the crossing point
// toward the approached cell's centroid by at most 0.2 units
// (nativeCellEntry), on float32 stores.
const wallStopRestSlack = 0.25

// wallStopSampleStride spreads the wall-rest sample over Donwhang's
// wall edges (3 020 of them) instead of walking every one.
const wallStopSampleStride = 53

/*
================
wallEdgeFrame

A wall edge of one Donwhang block in region coordinates: its endpoints,
its midpoint, and the centroid of the cell the edge belongs to, the
point every test chord starts from.
================
*/
type wallEdgeFrame struct {
	ax, az, bx, bz float64
	mx, mz         float64
	cx, cz         float64
	cell           int
}

/*
================
wallEdgeFrameOf
================
*/
func wallEdgeFrameOf(object *resolvedObjectNav, edge int) wallEdgeFrame {
	mesh := object.meshes[0]
	p := object.placement
	world := func(x, z float64) (float64, float64) {
		return object.cosYaw*x - object.sinYaw*z + p.x, object.sinYaw*x + object.cosYaw*z + p.z
	}
	a, b := int(mesh.outline.vertA[edge])*3, int(mesh.outline.vertB[edge])*3
	ax, az := world(float64(mesh.vertices[a]), float64(mesh.vertices[a+2]))
	bx, bz := world(float64(mesh.vertices[b]), float64(mesh.vertices[b+2]))
	lx, lz := objectCellCentroid2D(mesh, int(mesh.outline.srcCell[edge]))
	cx, cz := world(lx, lz)
	return wallEdgeFrame{
		ax: ax, az: az, bx: bx, bz: bz,
		mx: (ax + bx) / 2, mz: (az + bz) / 2,
		cx: cx, cz: cz,
		cell: int(mesh.outline.srcCell[edge]),
	}
}

/*
================
wallChordCrossing

Where the chord (x0, z0) -> (x1, z1) crosses the edge (ax, az) ->
(bx, bz): the crossing point, and whether it lies on both segments.
================
*/
func wallChordCrossing(x0, z0, x1, z1, ax, az, bx, bz float64) (float64, float64, bool) {
	dx, dz := x1-x0, z1-z0
	ex, ez := bx-ax, bz-az
	den := dx*ez - dz*ex
	if math.Abs(den) < 1e-12 {
		return 0, 0, false
	}
	t := ((ax-x0)*ez - (az-z0)*ex) / den
	u := ((ax-x0)*dz - (az-z0)*dx) / den
	if t <= 0 || t >= 1 || u <= 0.02 || u >= 0.98 {
		return 0, 0, false
	}
	return x0 + dx*t, z0 + dz*t, true
}

/*
================
circleStopsFirst

Whether one of the block's obstacle circles meets the chord before the
fraction limit. Geometry only, from the shipped circle data, so the
sample selection never consults the walk under test: a circle contact
is a legitimate stop short of the wall and would masquerade as one.
================
*/
func circleStopsFirst(block *dungeonSpawnBlock, x0, z0, x1, z1, limit float64) bool {
	dx, dz := x1-x0, z1-z0
	a := dx*dx + dz*dz
	if a == 0 {
		return false
	}
	for _, o := range block.obstacles {
		x, z := x0-o.x, z0-o.z
		b, c := x*dx+z*dz, x*x+z*z-o.radiusSquared
		if c < 0 {
			return true
		}
		d := b*b - a*c
		if d < 0 {
			continue
		}
		if t := (-b - math.Sqrt(d)) / a; t >= 0 && t < limit {
			return true
		}
	}
	return false
}

/*
================
assertWallStop

One wall chord's stop behaviour: blocked with the clipped bit alone,
the rest at the chord's crossing with the wall edge (a slide would put
it units along the wall), owned by the cell walked out of.
================
*/
func assertWallStop(t *testing.T, v *WaterValidator, from, to simulation.Spawn, crossX, crossZ float64, ordinal, cell int) {
	t.Helper()
	report := v.ClipMovementPathFrom(from, simulation.NavOwner{}, to)
	if report.Outcome != ClipBlocked || report.NativeResult != monster.NavResultClipped {
		t.Errorf("block %d cell %d: %v %#x at %+v, want blocked with the clipped bit alone",
			ordinal, cell, report.Outcome, report.NativeResult, report.Rest)
		return
	}
	cross := simulation.Spawn{RegionID: from.RegionID, X: crossX, Z: crossZ}
	if d := simulation.WorldDistance2D(report.Rest, cross); d > wallStopRestSlack {
		t.Errorf("block %d cell %d: rest %+v is %.3f from the wall crossing (%.1f, %.1f), want the contact, not a slide along the wall",
			ordinal, cell, report.Rest, d, crossX, crossZ)
	}
	if d := simulation.WorldDistance2D(from, report.Rest); d <= 1 {
		t.Errorf("block %d cell %d: rest %+v never left the start %+v", ordinal, cell, report.Rest, from)
	}
	owner := report.RestOwner
	if owner.Kind != simulation.NavOwnerDungeon || int(owner.Object.Object) != ordinal || int(owner.Object.Cell) != cell {
		t.Errorf("block %d cell %d: rest owned by %+v, want that block and cell", ordinal, cell, owner)
	}
}

/*
================
TestDungeonWallStopRestsAtTheContact

Sampled wall edges of the shipped Donwhang cave, each walked at twice:
straight out of the cell, and diagonally, so the chord meets the wall
with units of travel left along it - the chord shape a slide needs.
================
*/
func TestDungeonWallStopRestsAtTheContact(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	surface := v.caveSurface(planeCaveRegion)
	if surface == nil {
		t.Fatal("the Donwhang cave surface is not loaded")
	}
	walls, straight, diagonal := 0, 0, 0
	for i := range surface.objects {
		object := &surface.objects[i]
		ordinal := object.placement.ordinal
		linked := map[int]bool{}
		for _, link := range object.placement.links {
			linked[link.edge] = true
		}
		blockIndex, ok := surface.ordinalIndex[ordinal]
		if !ok {
			continue
		}
		block := &surface.blocks[blockIndex]
		for edge, flags := range object.meshes[0].outline.flags {
			if linked[edge] || flags&dungeonWallFlags == 0 {
				continue
			}
			walls++
			if walls%wallStopSampleStride != 1 {
				continue
			}
			frame := wallEdgeFrameOf(object, edge)
			ox, oz := frame.mx-frame.cx, frame.mz-frame.cz
			out := math.Hypot(ox, oz)
			if out == 0 {
				continue
			}
			ox, oz = ox/out, oz/out
			ex, ez := -oz, ox
			if ex*(frame.bx-frame.ax)+ez*(frame.bz-frame.az) < 0 {
				ex, ez = -ex, -ez
			}
			heightFrom := simulation.Spawn{RegionID: planeCaveRegion, X: frame.cx, Z: frame.cz}
			y, ok := surface.heightAt(heightFrom.X, object.placement.y, heightFrom.Z)
			if !ok {
				continue
			}
			from := simulation.Spawn{RegionID: planeCaveRegion, X: frame.cx, Y: y, Z: frame.cz}
			// Straight out of the cell, the proven construction.
			toStraight := simulation.Spawn{RegionID: planeCaveRegion, X: frame.mx + ox*wallChordOvershoot, Y: y, Z: frame.mz + oz*wallChordOvershoot}
			if !circleStopsFirst(block, from.X, from.Z, toStraight.X, toStraight.Z, 1) {
				straight++
				assertWallStop(t, v, from, toStraight, frame.mx, frame.mz, ordinal, frame.cell)
			}
			// Diagonally: the same wall, met with travel left along it.
			edgeLen := math.Hypot(frame.bx-frame.ax, frame.bz-frame.az)
			along := math.Min(3, 0.3*edgeLen)
			toDiagonal := simulation.Spawn{RegionID: planeCaveRegion, X: frame.mx + ex*along + ox*wallChordOvershoot, Y: y, Z: frame.mz + ez*along + oz*wallChordOvershoot}
			crossX, crossZ, crosses := wallChordCrossing(from.X, from.Z, toDiagonal.X, toDiagonal.Z, frame.ax, frame.az, frame.bx, frame.bz)
			if !crosses {
				continue
			}
			chordLen := math.Hypot(toDiagonal.X-from.X, toDiagonal.Z-from.Z)
			crossT := math.Hypot(crossX-from.X, crossZ-from.Z) / chordLen
			if circleStopsFirst(block, from.X, from.Z, toDiagonal.X, toDiagonal.Z, crossT) {
				continue
			}
			diagonal++
			assertWallStop(t, v, from, toDiagonal, crossX, crossZ, ordinal, frame.cell)
		}
	}
	t.Logf("walls %d, straight chords %d, diagonal chords %d", walls, straight, diagonal)
	if straight < 20 || diagonal < 20 {
		t.Fatalf("the wall sample dwindled: %d straight, %d diagonal", straight, diagonal)
	}
}

/*
================
TestDungeonWallStopPlayerShapeAndRepeatClick

The player call shape - the same wall chord from the mover's retained
owner - stops at the same contact, and clicking the same goal again
from that rest goes nowhere: no creep along the wall toward the goal,
which is the slide a player would see.
================
*/
func TestDungeonWallStopPlayerShapeAndRepeatClick(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	surface := v.caveSurface(planeCaveRegion)
	if surface == nil {
		t.Fatal("the Donwhang cave surface is not loaded")
	}
	from, to, report := firstDonwhangWallChord(t, v)
	owner, ok := surface.dungeonOwnerAt(from.RegionID, from.X, from.Y, from.Z, simulation.NavOwner{})
	if !ok {
		t.Fatal("no dungeon owner at the wall chord's start")
	}
	owned := v.ClipMovementPathFrom(from, owner, to)
	if owned.NativeResult != report.NativeResult || simulation.WorldDistance2D(owned.Rest, report.Rest) > 0.01 {
		t.Fatalf("from the retained owner: %#x at %+v, want the ownerless stop %+v", owned.NativeResult, owned.Rest, report.Rest)
	}
	again := v.ClipMovementPathFrom(owned.Rest, owned.RestOwner, to)
	if again.Outcome != ClipBlocked || again.NativeResult != monster.NavResultClipped {
		t.Fatalf("the repeat click: %v %#x at %+v, want the same wall stop", again.Outcome, again.NativeResult, again.Rest)
	}
	if d := simulation.WorldDistance2D(again.Rest, owned.Rest); d > 0.3 {
		t.Fatalf("the repeat click crept %.3f along the wall to %+v, want the contact %+v", d, again.Rest, owned.Rest)
	}
}
