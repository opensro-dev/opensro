/*
===========================================================================

plane_parity_test.go - parity pins for the action plane migration (#1763)

The legacy references below are the pre-migration bodies, kept verbatim:
a plane decision used to key on IsDungeonRegion at each call site and now
keys on worldgeom.SamePlane or simulation.PlaneKindOf at the same sites.
Every probe must agree with its legacy reference; the arm-swap mutation
(the PlaneCave comparison flipped to PlaneOutdoor) must break them.

===========================================================================
*/

package action

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

//============================================================================

/*
================
legacyPlaneEqual

The pre-migration plane-equality arm: IsDungeonRegion on both sides.
================
*/
func legacyPlaneEqual(a, b uint16) bool {
	return simulation.IsDungeonRegion(a) == simulation.IsDungeonRegion(b)
}

/*
================
legacyPositionSkillGoal

The pre-migration positionSkillGoal body, verbatim.
================
*/
func legacyPositionSkillGoal(from, requested simulation.Spawn, limit uint32) (simulation.Spawn, bool) {
	if !worldgeom.SamePlane(from.RegionID, requested.RegionID) || simulation.IsDungeonRegion(from.RegionID) && from.RegionID != requested.RegionID {
		return simulation.Spawn{}, false
	}
	a := worldgeom.RegionXZ{RegionID: from.RegionID, X: from.X, Z: from.Z}
	b := worldgeom.RegionXZ{RegionID: requested.RegionID, X: requested.X, Z: requested.Z}
	dx, dz := worldgeom.Delta(a, b)
	dy := requested.Y - from.Y
	distance := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if math.IsNaN(distance) || math.IsInf(distance, 0) {
		return simulation.Spawn{}, false
	}
	fraction := 1.0
	if distance > float64(limit) {
		fraction = float64(limit) / distance
	}
	p := worldgeom.Interpolate(a, b, fraction)
	return simulation.Spawn{RegionID: p.RegionID, X: p.X, Y: from.Y + dy*fraction, Z: p.Z, Angle: from.Angle}, true
}

/*
================
legacyWithinPartyRewardRange

The pre-migration withinPartyRewardRange body, verbatim.
================
*/
func legacyWithinPartyRewardRange(a, b simulation.Spawn) bool {
	if !worldgeom.SamePlane(a.RegionID, b.RegionID) {
		return false
	}
	dx, dz := float64(float32(b.X))-float64(float32(a.X)), float64(float32(b.Z))-float64(float32(a.Z))
	if !worldgeom.IsDungeonRegion(a.RegionID) {
		dx += float64(worldgeom.SectorX(b.RegionID)-worldgeom.SectorX(a.RegionID)) * 1920
		dz += float64(worldgeom.SectorY(b.RegionID)-worldgeom.SectorY(a.RegionID)) * 1920
	}
	x, z := float32(dx), float32(dz)
	squared := float32(float64(x)*float64(x) + float64(z)*float64(z))
	return math.Sqrt(float64(squared)) <= 1000
}

/*
================
legacyReverseMapPointValid

The pre-migration reverseMapPointValid body, verbatim.
================
*/
func legacyReverseMapPointValid(point enterworld.ReverseMapPoint) bool {
	if point.RegionID == 0 || simulation.IsDungeonRegion(point.RegionID) {
		return false
	}
	for _, v := range [3]float64{point.X, point.Y, point.Z} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return point.X >= 0 && point.X < reverseMapRegionSize && point.Z >= 0 && point.Z < reverseMapRegionSize
}

//============================================================================

/*
================
TestPlaneParityRegionMatrix

SamePlane and the PlaneCave comparison must agree with the legacy
IsDungeonRegion arms for every region pairing, region 0 included.
================
*/
func TestPlaneParityRegionMatrix(t *testing.T) {
	regions := []uint16{
		0,
		worldgeom.RegionIDForSectors(10, 10),
		worldgeom.RegionIDForSectors(11, 10),
		worldgeom.RegionIDForSectors(10, 11),
		worldgeom.RegionIDForSectors(250, 250),
		worldgeom.DungeonRegionBit | 1,
		worldgeom.DungeonRegionBit | 2,
		worldgeom.DungeonRegionBit,
	}
	for _, a := range regions {
		cave := simulation.PlaneKindOf(a) == simulation.PlaneCave
		if cave != simulation.IsDungeonRegion(a) {
			t.Fatalf("PlaneKindOf(%d) cave = %v, legacy IsDungeonRegion = %v", a, cave, simulation.IsDungeonRegion(a))
		}
		for _, b := range regions {
			if worldgeom.SamePlane(a, b) != legacyPlaneEqual(a, b) {
				t.Fatalf("SamePlane(%d, %d) = %v, legacy = %v", a, b, worldgeom.SamePlane(a, b), legacyPlaneEqual(a, b))
			}
		}
	}
}

/*
================
TestPlaneParityPositionSkillGoal

positionSkillGoal must agree with its legacy body on admitted goals,
limit clamps and refusals, across planes and across regions.
================
*/
func TestPlaneParityPositionSkillGoal(t *testing.T) {
	outdoor := worldgeom.RegionIDForSectors(10, 10)
	outdoorEast := worldgeom.RegionIDForSectors(11, 10)
	caveA := worldgeom.DungeonRegionBit | 7
	caveB := worldgeom.DungeonRegionBit | 9
	probes := []struct {
		name      string
		from      simulation.Spawn
		requested simulation.Spawn
		limit     uint32
	}{
		{"outdoor same region near", simulation.Spawn{RegionID: outdoor, X: 100, Z: 100}, simulation.Spawn{RegionID: outdoor, X: 106, Z: 108}, 100},
		{"outdoor same region clamped", simulation.Spawn{RegionID: outdoor, X: 100, Z: 100}, simulation.Spawn{RegionID: outdoor, X: 400, Z: 500}, 30},
		{"outdoor adjacent region", simulation.Spawn{RegionID: outdoor, X: 1900, Z: 960}, simulation.Spawn{RegionID: outdoorEast, X: 40, Z: 960}, 100},
		{"outdoor adjacent clamped", simulation.Spawn{RegionID: outdoor, X: 10, Z: 960}, simulation.Spawn{RegionID: outdoorEast, X: 1900, Z: 960}, 25},
		{"cave same region", simulation.Spawn{RegionID: caveA, X: 100, Z: 100}, simulation.Spawn{RegionID: caveA, X: 500, Z: 600}, 100},
		{"cave other region refused", simulation.Spawn{RegionID: caveA, X: 100, Z: 100}, simulation.Spawn{RegionID: caveB, X: 100, Z: 100}, 100},
		{"outdoor to cave refused", simulation.Spawn{RegionID: outdoor, X: 100, Z: 100}, simulation.Spawn{RegionID: caveA, X: 100, Z: 100}, 100},
		{"cave to outdoor refused", simulation.Spawn{RegionID: caveA, X: 100, Z: 100}, simulation.Spawn{RegionID: outdoor, X: 100, Z: 100}, 100},
		{"coincident zero limit", simulation.Spawn{RegionID: outdoor, X: 100, Z: 100}, simulation.Spawn{RegionID: outdoor, X: 100, Z: 100}, 0},
		{"nan refused", simulation.Spawn{RegionID: outdoor, X: 100, Z: 100}, simulation.Spawn{RegionID: outdoor, X: math.NaN(), Z: 100}, 100},
	}
	for _, probe := range probes {
		got, gotOK := positionSkillGoal(probe.from, probe.requested, probe.limit)
		want, wantOK := legacyPositionSkillGoal(probe.from, probe.requested, probe.limit)
		if gotOK != wantOK || got != want {
			t.Fatalf("%s: positionSkillGoal = (%v, %v), legacy = (%v, %v)", probe.name, got, gotOK, want, wantOK)
		}
	}
}

/*
================
TestPlaneParityPartyRewardRange

withinPartyRewardRange must agree with its legacy body: the sector
offset applies outdoors only, the distance cap is unchanged.
================
*/
func TestPlaneParityPartyRewardRange(t *testing.T) {
	outdoor := worldgeom.RegionIDForSectors(10, 10)
	outdoorEast := worldgeom.RegionIDForSectors(11, 10)
	caveA := worldgeom.DungeonRegionBit | 7
	caveB := worldgeom.DungeonRegionBit | 9
	probes := []struct {
		name string
		a    simulation.Spawn
		b    simulation.Spawn
	}{
		{"outdoor same sector inside", simulation.Spawn{RegionID: outdoor}, simulation.Spawn{RegionID: outdoor, X: 700, Z: 700}},
		{"outdoor same sector outside", simulation.Spawn{RegionID: outdoor}, simulation.Spawn{RegionID: outdoor, X: 800, Z: 800}},
		{"outdoor cross sector offset", simulation.Spawn{RegionID: outdoor, X: 1900}, simulation.Spawn{RegionID: outdoorEast, X: 100}},
		{"outdoor cross sector far", simulation.Spawn{RegionID: outdoor, X: 10}, simulation.Spawn{RegionID: outdoorEast, X: 1900}},
		{"cave same region inside", simulation.Spawn{RegionID: caveA}, simulation.Spawn{RegionID: caveA, X: 900}},
		{"cave same region outside", simulation.Spawn{RegionID: caveA}, simulation.Spawn{RegionID: caveA, X: 1100}},
		{"cave same region boundary", simulation.Spawn{RegionID: caveA}, simulation.Spawn{RegionID: caveA, X: 1000}},
		{"cave other region local", simulation.Spawn{RegionID: caveA}, simulation.Spawn{RegionID: caveB, X: 100}},
		{"mixed planes", simulation.Spawn{RegionID: outdoor}, simulation.Spawn{RegionID: caveA, X: 100}},
	}
	for _, probe := range probes {
		got := withinPartyRewardRange(probe.a, probe.b)
		want := legacyWithinPartyRewardRange(probe.a, probe.b)
		if got != want {
			t.Fatalf("%s: withinPartyRewardRange = %v, legacy = %v", probe.name, got, want)
		}
	}
}

/*
================
TestPlaneParityReverseMapPointValid

reverseMapPointValid must agree with its legacy body on region 0,
cave regions, bounds and non-finite coordinates.
================
*/
func TestPlaneParityReverseMapPointValid(t *testing.T) {
	outdoor := worldgeom.RegionIDForSectors(10, 10)
	cave := worldgeom.DungeonRegionBit | 7
	probes := []enterworld.ReverseMapPoint{
		{RegionID: 0},
		{RegionID: cave, X: 100, Z: 100},
		{RegionID: outdoor},
		{RegionID: outdoor, X: 1919, Z: 1919},
		{RegionID: outdoor, X: 1920, Z: 0},
		{RegionID: outdoor, X: -1, Z: 0},
		{RegionID: outdoor, X: 0, Z: 1920},
		{RegionID: outdoor, X: math.NaN(), Z: 0},
		{RegionID: outdoor, X: 0, Z: math.Inf(1)},
		{RegionID: outdoor, X: 500, Y: 50, Z: 500},
	}
	for _, point := range probes {
		got := reverseMapPointValid(point)
		want := legacyReverseMapPointValid(point)
		if got != want {
			t.Fatalf("reverseMapPointValid(%v) = %v, legacy = %v", point, got, want)
		}
	}
}
