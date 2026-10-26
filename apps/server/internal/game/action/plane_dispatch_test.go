/*
===========================================================================

plane_dispatch_test.go - the action package asks the plane owners
(#1763)

The area and strike filters, the operator towns and the transport bar
used to test the dungeon region bit themselves. #1763 moved each test
into the plane owner (worldgeom.SamePlane for a pair, simulation.
PlaneKindOf for one region), as screammask.go and cossummoning.go
already did. These tests keep the old expressions here, as the parity
reference, and require the answers to match on every probe.

===========================================================================
*/
package action

import (
	"testing"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
legacyCrossPlane

The pair test as the filters had it before #1763.
================
*/
func legacyCrossPlane(a, b uint16) bool {
	return simulation.IsDungeonRegion(a) != simulation.IsDungeonRegion(b)
}

/*
================
legacyOutdoorOnly

The single-region test as operator.go had it before #1763.
================
*/
func legacyOutdoorOnly(regionID uint16) bool {
	return !simulation.IsDungeonRegion(regionID)
}

// planeProbeRegions covers both planes and the region bit's edges.
var planeProbeRegions = []uint16{0x0000, 0x7FFF, 0x8000, 0x8001, 0xFFFF, 24999, 25000, 23687, 0x6046}

/*
================
TestPlanePredicatesKeepTheOldAnswers

Every pair over the region set: the owner's same-plane answer is the
old pair test's negation; every single region: the owner's kind is the
old bit, read both ways.
================
*/
func TestPlanePredicatesKeepTheOldAnswers(t *testing.T) {
	same, crossed := 0, 0
	for _, a := range planeProbeRegions {
		if got, want := simulation.PlaneKindOf(a) == simulation.PlaneCave, simulation.IsDungeonRegion(a); got != want {
			t.Fatalf("%#04x: cave %t, legacy %t", a, got, want)
		}
		if got, want := simulation.PlaneKindOf(a) == simulation.PlaneOutdoor, legacyOutdoorOnly(a); got != want {
			t.Fatalf("%#04x: outdoor %t, legacy %t", a, got, want)
		}
		for _, b := range planeProbeRegions {
			if got, want := worldgeom.SamePlane(a, b), !legacyCrossPlane(a, b); got != want {
				t.Fatalf("%#04x/%#04x: same plane %t, legacy %t", a, b, got, want)
			}
			if worldgeom.SamePlane(a, b) {
				same++
			} else {
				crossed++
			}
		}
	}
	// The matrix must reach both answers, or it no longer probes the bit.
	if same == 0 || crossed == 0 {
		t.Fatalf("same-plane pairs %d, crossed pairs %d: the matrix no longer probes both planes", same, crossed)
	}
}

/*
================
TestOperatorTownsPlaneAnswerKeepsTheOldAnswers

The operator town filter over a mixed table - outdoor, cave, non-recall
and unset destinations - answered as the legacy bit test answered it.
================
*/
func TestOperatorTownsPlaneAnswerKeepsTheOldAnswers(t *testing.T) {
	c := rebirthTestCharacter(20, 100)
	rt, _ := newTestRuntime(c, testItems())
	rt.portals = &portalCatalog{destinations: map[uint32]portalDestination{
		1: {id: 1, ref: 1, recall: true, spawn: simulation.Spawn{RegionID: 23687}},
		2: {id: 2, ref: 2, recall: true, spawn: simulation.Spawn{RegionID: 0x8001}},
		3: {id: 3, ref: 3, recall: false, spawn: simulation.Spawn{RegionID: 23687}},
		4: {id: 4, ref: 4, recall: true, spawn: simulation.Spawn{RegionID: 0x8004}},
		5: {id: 5, ref: 0, recall: true, spawn: simulation.Spawn{RegionID: 23687}},
	}}
	var want []uint32
	for _, destination := range rt.portals.destinations {
		if destination.recall && destination.ref != 0 && destination.spawn.RegionID != 0 && legacyOutdoorOnly(destination.spawn.RegionID) {
			want = append(want, destination.id)
		}
	}
	towns := rt.OperatorTowns()
	if len(towns) != len(want) {
		t.Fatalf("towns %+v, legacy %+v", towns, want)
	}
	for i, town := range towns {
		if town.ID != want[i] {
			t.Fatalf("towns %+v, legacy %+v", towns, want)
		}
	}
	if len(want) != 1 {
		t.Fatalf("legacy kept %d towns: the table no longer probes both planes", len(want))
	}
}

/*
================
TestTransportBarPlaneAnswerKeepsTheOldAnswers

The transport bar at every probed destination: barred exactly at the
legacy dungeon regions while the rule and the transport stand.
================
*/
func TestTransportBarPlaneAnswerKeepsTheOldAnswers(t *testing.T) {
	rt, c := guideFixture(t)
	summonTestTransport(rt, c)
	rt.JobGoodsTeleport = true
	barred, open := 0, 0
	for _, regionID := range planeProbeRegions {
		destination := simulation.Spawn{RegionID: regionID, X: 100, Y: 0, Z: 100}
		if got, want := rt.transportBarredFromDestination(c, destination), simulation.IsDungeonRegion(regionID); got != want {
			t.Fatalf("%#04x: barred %t, legacy %t", regionID, got, want)
		}
		if simulation.IsDungeonRegion(regionID) {
			barred++
		} else {
			open++
		}
	}
	if barred == 0 || open == 0 {
		t.Fatalf("barred %d, open %d: the destinations no longer probe both planes", barred, open)
	}
	rt.JobGoodsTeleport = false
	if rt.transportBarredFromDestination(c, simulation.Spawn{RegionID: 0x8001}) {
		t.Fatal("barred with the rule off")
	}
}
