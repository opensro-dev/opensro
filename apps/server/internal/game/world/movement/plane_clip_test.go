/*
===========================================================================

plane_clip_test.go - the clip family's plane dispatch keeps the old answers
(#1671)

The segment clip and the applied-clip arrival rule used to test the
dungeon region bit in clip.go and client_clip.go. #1671 moved each arm
into its plane. These tests keep the old dispatcher here, as the parity
reference, and require the plane dispatch to answer every real probe
exactly as it did.

===========================================================================
*/
package movement

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
legacyClipSegment

The segment dispatch as clip.go had it before #1671: an endpoint in a
dungeon region takes the dungeon clip, every other pair the outdoor walk.
================
*/
func legacyClipSegment(v *WaterValidator, from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) (ClipReport, *navWalk) {
	from = v.ownedStart(from, fromOwner)
	if simulation.IsDungeonRegion(from.RegionID) || simulation.IsDungeonRegion(to.RegionID) {
		// #1728: the cave arm is one leg of the dungeon walk from the
		// retained cell (it reports the rest's owner itself).
		return v.walkDungeon(from, fromOwner, to), nil
	}
	return v.clipOutdoorSegment(from, fromOwner, to)
}

/*
================
clipSegmentParity

The plane dispatch against the legacy one on each probe: the same
report and the same owner walk.
================
*/
func clipSegmentParity(t *testing.T, v *WaterValidator, probes [][2]simulation.Spawn) {
	t.Helper()
	for _, probe := range probes {
		from, to := probe[0], probe[1]
		gotReport, gotWalk := v.clipMovementSegmentWithWalk(from, simulation.NavOwner{}, to)
		wantReport, wantWalk := legacyClipSegment(v, from, simulation.NavOwner{}, to)
		if !reflect.DeepEqual(gotReport, wantReport) || !reflect.DeepEqual(gotWalk, wantWalk) {
			t.Fatalf("%+v -> %+v: plane clip %+v, legacy %+v", from, to, gotReport, wantReport)
		}
	}
}

/*
================
TestClipSegmentPlaneDispatchKeepsTheOldAnswers

Real probes on both planes - the Jangan veranda outdoors, the Donwhang
cave anchors - and chords across the boundary both ways, which both
dispatches must refuse the same way.
================
*/
func TestClipSegmentPlaneDispatchKeepsTheOldAnswers(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	outdoor := []simulation.Spawn{
		{RegionID: planeOutdoorRegion, X: 623, Y: 3.04, Z: 1271},
		{RegionID: planeOutdoorRegion, X: 680, Y: 3.04, Z: 1271}, // at the veranda rail
	}
	cave := []simulation.Spawn{
		{RegionID: planeCaveRegion, X: 993.197, Y: 0.00034, Z: -307.099},
		{RegionID: planeCaveRegion, X: 629, Y: -2.22, Z: 74},
	}
	outdoorProbes, caveProbes := planeProbes(outdoor), planeProbes(cave)
	clipSegmentParity(t, v, outdoorProbes)
	clipSegmentParity(t, v, caveProbes)

	// The probes must exercise both arms for real: some outdoor walks
	// arrive on the tile grid, and some cave walks arrive on a floor.
	arrived := func(probes [][2]simulation.Spawn) int {
		n := 0
		for _, probe := range probes {
			if report, _ := v.clipMovementSegmentWithWalk(probe[0], simulation.NavOwner{}, probe[1]); report.Outcome == ClipArrived {
				n++
			}
		}
		return n
	}
	if arrived(outdoorProbes) == 0 || arrived(caveProbes) == 0 {
		t.Fatalf("arrivals: outdoor %d, cave %d; the probes no longer reach both clips", arrived(outdoorProbes), arrived(caveProbes))
	}

	crossings := [][2]simulation.Spawn{{outdoor[0], cave[0]}, {cave[0], outdoor[0]}}
	clipSegmentParity(t, v, crossings)
	for _, crossing := range crossings {
		if report, _ := v.clipMovementSegmentWithWalk(crossing[0], simulation.NavOwner{}, crossing[1]); report.Outcome != ClipBlocked {
			t.Fatalf("%+v -> %+v crossed the cave boundary: %+v", crossing[0], crossing[1], report)
		}
	}
}

/*
================
TestArrivalKeepsRestOnPlaneIsTheOldCaveRule

The applied clip's arrival rule: the cave plane keeps the walked rest,
the outdoor plane the goal - exactly the region-bit test it replaces,
across outdoor regions, both cave pair regions and the bit's edges.
================
*/
func TestArrivalKeepsRestOnPlaneIsTheOldCaveRule(t *testing.T) {
	for _, region := range []uint16{planeOutdoorRegion, 0x6B4F, 0x0000, 0x7FFF, planeCaveRegion, planeOtherCaveRegion, 0x8000, 0xFFFF} {
		if got, want := arrivalKeepsRestOnPlane(region), simulation.IsDungeonRegion(region); got != want {
			t.Errorf("region %#04x: keeps rest %t, the old rule %t", region, got, want)
		}
	}
}
