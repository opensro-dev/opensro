/*
===========================================================================

plane_ownedstart_test.go - the owned start keeps the old answers on the
planes (#1674)

WaterValidator.ownedStart used to test the dungeon region bit itself.
#1674 moved each arm into its plane. These tests keep the old body here,
as the parity reference, and require the plane dispatch to answer every
real probe exactly as it did - and prove the arms really differ, so a
swapped arm cannot hide.

===========================================================================
*/
package movement

import (
	"math"
	"testing"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
legacyOwnedStart

The owned start as owned_start.go had it before #1674.
================
*/
func legacyOwnedStart(v *WaterValidator, from simulation.Spawn, owner simulation.NavOwner) simulation.Spawn {
	if simulation.IsDungeonRegion(from.RegionID) {
		return from
	}
	if !owner.Resolved() {
		owner, _, _ = v.ResolveNavOwner(from, simulation.NavOwner{})
	}
	stand := v.standForOwner(owner)
	if stand == nil {
		return from
	}
	grid := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: from.RegionID, X: from.X, Z: from.Z})
	x, z := stand.local(grid.X, grid.Z)
	nx, nz := ownedCellStart(stand.mesh, stand.cellIndex, x, z)
	if nx == x && nz == z {
		return from
	}
	c, s := math.Cos(stand.placement.yaw), math.Sin(stand.placement.yaw)
	from.X += c*(nx-x) - s*(nz-z)
	from.Z += s*(nx-x) + c*(nz-z)
	return from
}

/*
================
TestOwnedStartPlaneDispatchKeepsTheOldAnswers

Real probes on both planes - the Jangan veranda outdoors (a deck at
3.04 over terrain 0, so starts resolve to an object owner), the
Donwhang cave anchors - each anchor and every walked endpoint around
it, with no owner held and with the resolved owner held.
================
*/
func TestOwnedStartPlaneDispatchKeepsTheOldAnswers(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	outdoor := []simulation.Spawn{
		{RegionID: planeOutdoorRegion, X: 623, Y: 3.04, Z: 1271},
		{RegionID: planeOutdoorRegion, X: 680, Y: 3.04, Z: 1271}, // at the veranda rail
	}
	cave := []simulation.Spawn{
		{RegionID: planeCaveRegion, X: 993.197, Y: 0.00034, Z: -307.099},
		{RegionID: planeCaveRegion, X: 629, Y: -2.22, Z: 74},
	}
	var outdoorPoints, cavePoints []simulation.Spawn
	for _, pair := range planeProbes(outdoor) {
		outdoorPoints = append(outdoorPoints, pair[0], pair[1])
	}
	for _, pair := range planeProbes(cave) {
		cavePoints = append(cavePoints, pair[0], pair[1])
	}

	check := func(p simulation.Spawn, owner simulation.NavOwner) simulation.Spawn {
		got := v.ownedStart(p, owner)
		if want := legacyOwnedStart(v, p, owner); got != want {
			t.Fatalf("%+v: owned start %+v, legacy %+v", p, got, want)
		}
		return got
	}

	movedOutdoor, caveStarts := 0, 0
	for _, p := range outdoorPoints {
		if got := check(p, simulation.NavOwner{}); got != p {
			movedOutdoor++
		}
	}
	// The real call pattern holds the owner from the mover's last pose:
	// with the anchor's owner held, a start that has walked out of its
	// cell is replaced by the cell-edge entry.
	for _, anchor := range outdoor {
		owner, _, ok := v.ResolveNavOwner(anchor, simulation.NavOwner{})
		if !ok {
			t.Fatalf("the anchor %+v resolves no owner", anchor)
		}
		check(anchor, owner)
		for _, pair := range planeProbes([]simulation.Spawn{anchor}) {
			for _, p := range pair {
				if got := check(p, owner); got != p {
					movedOutdoor++
				}
			}
		}
	}
	for _, p := range cavePoints {
		if got := check(p, simulation.NavOwner{}); got != p {
			t.Fatalf("cave start moved: %+v -> %+v", p, got)
		}
		caveStarts++
	}
	// Both arms must be exercised for real: an outdoor start is replaced
	// somewhere, and cave starts stand, or the probes no longer reach
	// both planes.
	if movedOutdoor == 0 || caveStarts == 0 {
		t.Fatalf("moved outdoor starts %d, cave starts %d: the probes no longer reach both planes",
			movedOutdoor, caveStarts)
	}
}
