/*
===========================================================================

dungeon_owner_test.go - movers carry their dungeon cell (#1727)

A cave clip hands back its rest's dungeon owner; a doorway advances it to
the next block, a stacked floor keeps its floor, a stale or foreign hint
falls back to the original's relocation rule, and a monster path carries
the owner through LiveNavOwner. Owning the cell changes no position.

===========================================================================
*/
package movement

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// The stacked column the height parity test uses: floors at 0 and 280.
const (
	stackedColumnX     = -200
	stackedColumnZ     = -460
	stackedUpperFloorY = 280.001
)

/*
================
caveOwnerBlock

The DOF ordinal a dungeon owner names; -1 for any other owner.
================
*/
func caveOwnerBlock(owner simulation.NavOwner) int {
	if owner.Kind != simulation.NavOwnerDungeon {
		return -1
	}
	return int(owner.Object.Object)
}

/*
================
TestCaveClipCarriesItsOwnerThroughADoorway

The Donwhang doorway between blocks 68 and 67 (the seam at z 40): a chord
from 68 across it rests in 67, and the owner says so.
================
*/
func TestCaveClipCarriesItsOwnerThroughADoorway(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	from := simulation.Spawn{RegionID: planeCaveRegion, X: 1761, Y: 420, Z: 30}
	to := simulation.Spawn{RegionID: planeCaveRegion, X: 1761, Y: 420, Z: 50}
	start, _, ok := v.ResolveNavOwner(from, simulation.NavOwner{})
	if !ok || caveOwnerBlock(start) != 68 {
		t.Fatalf("start owner %+v (ok %t), want block 68", start, ok)
	}
	report := v.ClipMovementPathFrom(from, start, to)
	if report.Outcome != ClipArrived {
		t.Fatalf("doorway chord %v", report.Outcome)
	}
	if caveOwnerBlock(report.RestOwner) != 67 {
		t.Fatalf("rest owner %+v, want block 67", report.RestOwner)
	}
}

/*
================
TestCaveOwnerKeepsItsFloorOnAStackedColumn

At the stacked column each floor resolves to its own owner, and a short
chord on the upper floor keeps the upper owner: the start's owner is kept
while its cell still holds the rest.
================
*/
func TestCaveOwnerKeepsItsFloorOnAStackedColumn(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	upper := simulation.Spawn{RegionID: planeCaveRegion, X: stackedColumnX, Y: stackedUpperFloorY, Z: stackedColumnZ}
	ground := upper
	ground.Y = 0
	upperOwner, _, okUpper := v.ResolveNavOwner(upper, simulation.NavOwner{})
	groundOwner, _, okGround := v.ResolveNavOwner(ground, simulation.NavOwner{})
	if !okUpper || !okGround || upperOwner == groundOwner {
		t.Fatalf("stacked owners: upper %+v (%t), ground %+v (%t); want two distinct cells", upperOwner, okUpper, groundOwner, okGround)
	}
	// The ground point resolved with the upper owner as hint is still
	// the upper cell's XZ, so the hint is kept: the owner, not the
	// height, names the floor.
	if kept, _, _ := v.ResolveNavOwner(ground, upperOwner); kept != upperOwner {
		t.Fatalf("the upper owner was re-guessed from height: %+v", kept)
	}
	step := upper
	step.X += 0.5
	report := v.ClipMovementPathFrom(upper, upperOwner, step)
	if report.RestOwner != upperOwner {
		t.Fatalf("a short upper-floor step rests on %+v, want the upper owner %+v", report.RestOwner, upperOwner)
	}
}

/*
================
TestCaveOwnerRelocatesWithoutAUsableHint

A hint from another region, or whose cell no longer holds the point,
falls back to the original's relocation rule (the nearest floor under the
point); resolution never moves the point.
================
*/
func TestCaveOwnerRelocatesWithoutAUsableHint(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	p := simulation.Spawn{RegionID: planeCaveRegion, X: 1761, Y: 420, Z: 30}
	want, _, ok := v.ResolveNavOwner(p, simulation.NavOwner{})
	if !ok {
		t.Fatal("no owner under the probe point")
	}
	far := simulation.Spawn{RegionID: planeCaveRegion, X: 1761, Y: 420, Z: 50}
	farOwner, _, _ := v.ResolveNavOwner(far, simulation.NavOwner{})
	foreign := want
	foreign.Object.Surface = planeCaveRegion + 1
	for name, hint := range map[string]simulation.NavOwner{
		"another region":   foreign,
		"a cell elsewhere": farOwner,
		"an outdoor owner": simulation.TerrainOwner(),
		"no owner":         {},
	} {
		got, y, ok := v.ResolveNavOwner(p, hint)
		if !ok || got != want || y != p.Y {
			t.Errorf("%s: owner %+v y %v (ok %t), want %+v at y %v", name, got, y, ok, want, p.Y)
		}
	}
}

/*
================
TestCaveMonsterPathCarriesItsOwner

A cave monster path carries the rest's owner, so the mover's live owner
names its cell once it arrives.
================
*/
func TestCaveMonsterPathCarriesItsOwner(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	from := monster.Pose{RegionID: planeCaveRegion, X: 1761, Y: 420, Z: 30}
	to := monster.Pose{RegionID: planeCaveRegion, X: 1761, Y: 420, Z: 50}
	path := v.PlanMonsterPath(from, to)
	if path == nil {
		t.Fatal("no path across the doorway")
	}
	mover := monster.MoverState{From: from, To: path.Rest(), DepartMs: 1000, ArriveMs: 2000}
	mover.AdoptNavigation(path)
	if got := mover.LiveNavOwner(5000); caveOwnerBlock(got) != 67 {
		t.Fatalf("live owner after arrival %+v, want block 67", got)
	}
}
