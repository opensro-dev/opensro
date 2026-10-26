/*
===========================================================================

dungeon_wall_result_test.go - a dungeon wall reports clipped, a dead
doorway blocked (#1712)

The native edge walk, run with its stop flag set, stops at a wall edge
(flags & 0x12) with result bit 0 and keeps bit 28 for a doorway whose
neighbour cannot be resolved. Every outline edge of Donwhang's blocks is
walked into from its own cell: the 3 020 walls must report clipped, no
dead doorway may let the chord through, and one the chord reaches
reports blocked.

===========================================================================
*/
package movement

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// wallChordOvershoot is how far past an outline edge's midpoint the test
// chord runs, so it crosses the edge well inside its length.
const wallChordOvershoot = 3.0

/*
================
outlineEdgeChord

A chord from the centre of the outline edge's cell out through the edge's
midpoint, in region coordinates.
================
*/
func outlineEdgeChord(object *resolvedObjectNav, edge int) (simulation.Spawn, simulation.Spawn) {
	mesh := object.meshes[0]
	p := object.placement
	world := func(x, z float64) (float64, float64) {
		return object.cosYaw*x - object.sinYaw*z + p.x, object.sinYaw*x + object.cosYaw*z + p.z
	}
	a, b := int(mesh.outline.vertA[edge])*3, int(mesh.outline.vertB[edge])*3
	mx := (float64(mesh.vertices[a]) + float64(mesh.vertices[b])) / 2
	mz := (float64(mesh.vertices[a+2]) + float64(mesh.vertices[b+2])) / 2
	cx, cz := objectCellCentroid2D(mesh, int(mesh.outline.srcCell[edge]))
	dx, dz := mx-cx, mz-cz
	length := math.Hypot(dx, dz)
	fromX, fromZ := world(cx, cz)
	toX, toZ := world(mx+dx/length*wallChordOvershoot, mz+dz/length*wallChordOvershoot)
	from := simulation.Spawn{RegionID: planeCaveRegion, X: fromX, Y: p.y, Z: fromZ}
	to := simulation.Spawn{RegionID: planeCaveRegion, X: toX, Y: p.y, Z: toZ}
	return from, to
}

/*
================
TestDungeonWallReportsClippedAndDeadDoorwayBlocked
================
*/
func TestDungeonWallReportsClippedAndDeadDoorwayBlocked(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	surface := v.caveSurface(planeCaveRegion)
	if surface == nil {
		t.Fatal("the Donwhang cave surface is not loaded")
	}
	walls, wallsClipped, doorways, doorwaysBlocked := 0, 0, 0, 0
	for i := range surface.objects {
		object := &surface.objects[i]
		linked := map[int]bool{}
		for _, link := range object.placement.links {
			linked[link.edge] = true
		}
		mesh := object.meshes[0]
		for edge, flags := range mesh.outline.flags {
			if linked[edge] {
				continue
			}
			from, to := outlineEdgeChord(object, edge)
			if y, ok := surface.heightAt(from.X, from.Y, from.Z); ok {
				from.Y, to.Y = y, y
			}
			report := v.clipDungeonPath(from, to)
			switch {
			case flags&dungeonWallFlags != 0:
				walls++
				if report.NativeResult&monster.NavResultBlocked != 0 {
					t.Errorf("block %d wall edge %d (flags %#x): blocked, want clipped", object.placement.ordinal, edge, flags)
				}
				if report.NativeResult&monster.NavResultClipped != 0 {
					wallsClipped++
				}
			case flags&8 != 0:
				// Most dead doorways sit behind a wall or an obstacle
				// circle the chord meets first (bit 0); none may let it
				// through.
				doorways++
				if report.NativeResult == 0 {
					t.Errorf("block %d dead doorway %d: the chord passed", object.placement.ordinal, edge)
				}
				if report.NativeResult&monster.NavResultBlocked != 0 {
					doorwaysBlocked++
				}
			}
		}
	}
	t.Logf("walls %d (clipped %d), unlinked doorways %d (blocked %d)", walls, wallsClipped, doorways, doorwaysBlocked)
	// The premise: the chords really reach the edges, or the parity
	// above compares nothing.
	if walls == 0 || wallsClipped != walls || doorways == 0 || doorwaysBlocked == 0 {
		t.Fatalf("walls %d clipped %d, doorways %d blocked %d: the chords no longer reach the edges",
			walls, wallsClipped, doorways, doorwaysBlocked)
	}
}

/*
================
TestDungeonOutlineResultByFlags
================
*/
func TestDungeonOutlineResultByFlags(t *testing.T) {
	cases := []struct {
		flags byte
		want  uint32
	}{
		{0x03, monster.NavResultClipped}, // Donwhang's walls
		{0x02, monster.NavResultClipped},
		{0x10, monster.NavResultClipped},
		{0x08, monster.NavResultBlocked}, // a doorway with no neighbour
		{0x20, monster.NavResultBlocked},
	}
	for _, c := range cases {
		if got := dungeonOutlineResult(c.flags); got != c.want {
			t.Errorf("flags %#x: %#x, want %#x", c.flags, got, c.want)
		}
	}
}

/*
================
firstDonwhangWallChord

A chord from a Donwhang cell into the first wall edge whose clip stops
short of it, for the behaviour tests below.
================
*/
func firstDonwhangWallChord(t *testing.T, v *WaterValidator) (simulation.Spawn, simulation.Spawn, ClipReport) {
	t.Helper()
	surface := v.caveSurface(planeCaveRegion)
	if surface == nil {
		t.Fatal("the Donwhang cave surface is not loaded")
	}
	for i := range surface.objects {
		object := &surface.objects[i]
		for edge, flags := range object.meshes[0].outline.flags {
			if flags&dungeonWallFlags == 0 {
				continue
			}
			from, to := outlineEdgeChord(object, edge)
			y, ok := surface.heightAt(from.X, from.Y, from.Z)
			if !ok {
				continue
			}
			from.Y, to.Y = y, y
			report := v.clipDungeonPath(from, to)
			// Chosen by geometry alone - stopped short of the edge, well
			// away from the start - so the result bit stays under test.
			if report.Outcome == ClipBlocked && simulation.WorldDistance2D(from, report.Rest) > 1 {
				return from, to, report
			}
		}
	}
	t.Fatal("no Donwhang wall chord stops on its wall")
	return simulation.Spawn{}, simulation.Spawn{}, ClipReport{}
}

/*
================
TestDungeonWallSpawnAndKnockbackSeeClipped

The population move test and the knockback both run SpawnMoveTest. Into a
cave wall it now reports the clipped bit with the wall contact as the rest:
creation then rejects the candidate as clipped (5F6EB0's bit-0 arm) instead
of the blocked arm's centre fallback, and a knockback stops at the wall
instead of being cancelled.
================
*/
func TestDungeonWallSpawnAndKnockbackSeeClipped(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	from, to, report := firstDonwhangWallChord(t, v)
	move := v.SpawnMoveTest(from, to)
	if move.Result != monster.NavResultClipped {
		t.Fatalf("spawn move test into a wall: result %#x, want clipped", move.Result)
	}
	if move.Rest != report.Rest || simulation.WorldDistance2D(from, move.Rest) <= 1 {
		t.Fatalf("spawn move test rest %+v, want the wall contact %+v", move.Rest, report.Rest)
	}
}

/*
================
TestDungeonWallCompanionStandsAtTheContact

A companion summoned toward a cave wall keeps the navigation endpoint
(a clipped move, 5F6EB0's creation policy) instead of returning to its
owner as a blocked move would.
================
*/
func TestDungeonWallCompanionStandsAtTheContact(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	from, to, report := firstDonwhangWallChord(t, v)
	placed := v.ConstrainCompanionSpawn(from, to)
	if simulation.WorldDistance2D(placed, from) <= 1 {
		t.Fatalf("companion returned to its owner at %+v; want the wall contact %+v", placed, report.Rest)
	}
	if simulation.WorldDistance2D(placed, report.Rest) > 0.01 {
		t.Fatalf("companion placed at %+v, want the wall contact %+v", placed, report.Rest)
	}
}
