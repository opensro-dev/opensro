/*
===========================================================================

geometry_test.go - outdoor sector seam and dungeon planar coordinate tests

Verifies frame-consistent delta, distance, interpolation, and sub-ULP
sector-seam normalization invariants.

===========================================================================
*/
package world

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

type worldSpaceParityFile struct {
	RegionSize       float64                `json:"regionSize"`
	DungeonRegionBit uint16                 `json:"dungeonRegionBit"`
	Cases            []worldSpaceParityCase `json:"cases"`
}

type worldSpaceParityPoint struct {
	RegionID uint16  `json:"regionId"`
	X        float64 `json:"x"`
	Z        float64 `json:"z"`
}

type worldSpaceParitySector struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type worldSpaceParityCase struct {
	Name              string                  `json:"name"`
	From              worldSpaceParityPoint   `json:"from"`
	To                worldSpaceParityPoint   `json:"to"`
	FromDungeon       bool                    `json:"fromDungeon"`
	ToDungeon         bool                    `json:"toDungeon"`
	FromSector        *worldSpaceParitySector `json:"fromSector"`
	ToSector          *worldSpaceParitySector `json:"toSector"`
	SamePlane         bool                    `json:"samePlane"`
	SamePlaneAdjacent bool                    `json:"samePlaneAdjacent"`
	Delta             *[2]float64             `json:"delta"`
	Distance          *float64                `json:"distance"`
	ServerDelta       *[2]float64             `json:"serverDelta"`
}

/*
================
TestWorldSpaceParityCases

Verifies that worldgeom matches the shared client/server world-space parity
cases in testdata/world-space-cases.json. On cross-region dungeon and
dungeon/outdoor pairs, client worldDelta follows SWorld_CalculateRegionOffsetVector
(client 0x00888140, zero region offset whenever either endpoint has 0x8000 set),
whereas worldgeom.Delta only short-circuits equal dungeon RegionIDs; those
cases pin delta for the client and serverDelta for Go.
================
*/
func TestWorldSpaceParityCases(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "world-space-cases.json"))
	if err != nil {
		t.Fatalf("read world-space-cases.json: %v", err)
	}
	var suite worldSpaceParityFile
	if err := json.Unmarshal(raw, &suite); err != nil {
		t.Fatalf("unmarshal world-space-cases.json: %v", err)
	}
	if suite.RegionSize != OutdoorRegionSize {
		t.Fatalf("regionSize = %v, want %v", suite.RegionSize, OutdoorRegionSize)
	}
	if suite.DungeonRegionBit != DungeonRegionBit {
		t.Fatalf("dungeonRegionBit = 0x%x, want 0x%x", suite.DungeonRegionBit, DungeonRegionBit)
	}
	if len(suite.Cases) == 0 {
		t.Fatal("world-space-cases.json has no cases")
	}
	for _, tc := range suite.Cases {
		if got := IsDungeonRegion(tc.From.RegionID); got != tc.FromDungeon {
			t.Errorf("%s: IsDungeonRegion(from=0x%x) = %v, want %v", tc.Name, tc.From.RegionID, got, tc.FromDungeon)
		}
		if got := IsDungeonRegion(tc.To.RegionID); got != tc.ToDungeon {
			t.Errorf("%s: IsDungeonRegion(to=0x%x) = %v, want %v", tc.Name, tc.To.RegionID, got, tc.ToDungeon)
		}
		if tc.FromSector != nil {
			if gotX, gotY := SectorX(tc.From.RegionID), SectorY(tc.From.RegionID); gotX != tc.FromSector.X || gotY != tc.FromSector.Y {
				t.Errorf("%s: from sector = (%d,%d), want (%d,%d)", tc.Name, gotX, gotY, tc.FromSector.X, tc.FromSector.Y)
			}
		}
		if tc.ToSector != nil {
			if gotX, gotY := SectorX(tc.To.RegionID), SectorY(tc.To.RegionID); gotX != tc.ToSector.X || gotY != tc.ToSector.Y {
				t.Errorf("%s: to sector = (%d,%d), want (%d,%d)", tc.Name, gotX, gotY, tc.ToSector.X, tc.ToSector.Y)
			}
		}
		if got := SamePlane(tc.From.RegionID, tc.To.RegionID); got != tc.SamePlane {
			t.Errorf("%s: SamePlane = %v, want %v", tc.Name, got, tc.SamePlane)
		}
		if got := SamePlaneAdjacent(tc.From.RegionID, tc.To.RegionID); got != tc.SamePlaneAdjacent {
			t.Errorf("%s: SamePlaneAdjacent = %v, want %v", tc.Name, got, tc.SamePlaneAdjacent)
		}
		wantDelta := tc.Delta
		if tc.ServerDelta != nil {
			wantDelta = tc.ServerDelta
		}
		if wantDelta != nil {
			from := RegionXZ{RegionID: tc.From.RegionID, X: tc.From.X, Z: tc.From.Z}
			to := RegionXZ{RegionID: tc.To.RegionID, X: tc.To.X, Z: tc.To.Z}
			dx, dz := Delta(from, to)
			if math.Abs(dx-wantDelta[0]) > 1e-9 || math.Abs(dz-wantDelta[1]) > 1e-9 {
				t.Errorf("%s: Delta = (%v,%v), want (%v,%v)", tc.Name, dx, dz, wantDelta[0], wantDelta[1])
			}
		}
		if tc.Distance != nil && tc.ServerDelta == nil {
			from := RegionXZ{RegionID: tc.From.RegionID, X: tc.From.X, Z: tc.From.Z}
			to := RegionXZ{RegionID: tc.To.RegionID, X: tc.To.X, Z: tc.To.Z}
			got := Distance(from, to)
			if math.Abs(got-*tc.Distance) > 1e-9 {
				t.Errorf("%s: Distance = %v, want %v", tc.Name, got, *tc.Distance)
			}
		}
	}
}

/*
================
TestAdjacentRegionDeltaDistanceAndInterpolationShareOneFrame
================
*/
func TestAdjacentRegionDeltaDistanceAndInterpolationShareOneFrame(t *testing.T) {
	west := RegionIDForSectors(40, 50)
	east := RegionIDForSectors(41, 50)
	from := RegionXZ{RegionID: west, X: 1910, Z: 100}
	to := RegionXZ{RegionID: east, X: 10, Z: 100}

	dx, dz := Delta(from, to)
	if dx != 20 || dz != 0 || Distance(from, to) != 20 {
		t.Fatalf("adjacent seam delta=(%v,%v), distance=%v; want (20,0), 20", dx, dz, Distance(from, to))
	}
	mid := Interpolate(from, to, 0.5)
	if mid.RegionID != east || mid.X != 0 || mid.Z != 100 {
		t.Fatalf("adjacent seam midpoint=%+v, want east-region local (0,100)", mid)
	}
}

/*
================
TestNormalizeOutdoorCarriesBothAxesAndLeavesDungeonLocal
================
*/
func TestNormalizeOutdoorCarriesBothAxesAndLeavesDungeonLocal(t *testing.T) {
	region := RegionIDForSectors(40, 50)
	got := NormalizeOutdoor(RegionXZ{RegionID: region, X: -5, Z: OutdoorRegionSize + 7})
	wantRegion := RegionIDForSectors(39, 51)
	if got.RegionID != wantRegion || got.X != OutdoorRegionSize-5 || got.Z != 7 {
		t.Fatalf("normalized outdoor=%+v, want region=%#04x x=%v z=7", got, wantRegion, OutdoorRegionSize-5)
	}

	dungeon := RegionXZ{RegionID: DungeonRegionBit | 7, X: 4000, Z: -300}
	if got := NormalizeOutdoor(dungeon); got != dungeon {
		t.Fatalf("dungeon normalization changed %+v to %+v", dungeon, got)
	}
}

/*
================
TestGridFrameRoundTripAcrossOutdoorSeam
================
*/
func TestGridFrameRoundTripAcrossOutdoorSeam(t *testing.T) {
	west := RegionIDForSectors(50, 40)
	east := RegionIDForSectors(51, 40)
	grid := ExpandGrid(RegionXZ{RegionID: east, X: 10, Z: 25})
	inWest := LocalFromGrid(west, grid)
	if inWest.RegionID != west || inWest.X != OutdoorRegionSize+10 || inWest.Z != 25 {
		t.Fatalf("east point in west frame=%+v", inWest)
	}
	if roundTrip := ExpandGrid(inWest); roundTrip != grid {
		t.Fatalf("round trip=%+v, want %+v", roundTrip, grid)
	}
}

/*
================
TestSamePlaneRejectsOutdoorDungeonPair
================
*/
func TestSamePlaneRejectsOutdoorDungeonPair(t *testing.T) {
	if !SamePlane(0x3228, 0x3229) {
		t.Fatal("adjacent outdoor regions must share a plane")
	}
	if SamePlane(0x3228, 0x3228|DungeonRegionBit) {
		t.Fatal("outdoor and dungeon regions must not share a plane")
	}
}

/*
================
TestDungeonLiveInterpolationPreservesUnboundedLocalCoordinates
================
*/
func TestDungeonLiveInterpolationPreservesUnboundedLocalCoordinates(t *testing.T) {
	region := DungeonRegionBit | 7
	from := RegionXZ{RegionID: region, X: 3000, Z: -500}
	to := RegionXZ{RegionID: region, X: 5000, Z: 500}
	got := Interpolate(from, to, 0.5)
	if got.RegionID != region || got.X != 4000 || got.Z != 0 {
		t.Fatalf("dungeon midpoint=%+v, want region=%#04x local (4000,0)", got, region)
	}
}

/*
================
TestInterpolationClampsBeforeDepartureAndAfterArrival
================
*/
func TestInterpolationClampsBeforeDepartureAndAfterArrival(t *testing.T) {
	from := RegionXZ{RegionID: 0x3230, X: 100, Z: 200}
	to := RegionXZ{RegionID: 0x3230, X: 300, Z: 400}
	if got := Interpolate(from, to, -0.5); got != from {
		t.Fatalf("pre-departure interpolation=%+v, want from=%+v", got, from)
	}
	if got := Interpolate(from, to, 1.5); got != to {
		t.Fatalf("post-arrival interpolation=%+v, want to=%+v", got, to)
	}
}

/*
================
TestNormalizeOutdoorClampsSubULPSeamCrossingIntoHalfOpenInterval

Regression for #1210: when an interpolated outdoor coordinate lands at a
sub-ULP negative value (e.g., from.X = 3, dx = -374, t = 6/748 ->
x = -4.440892098500626e-16), math.Floor(x / 1920) is -1 and adding 1920.0
in float64 rounds to 1920.0, which breaks the [0, 1920) region-local
invariant and causes terrainHeightAt to index offsetKey(1, 0).
================
*/
func TestNormalizeOutdoorClampsSubULPSeamCrossingIntoHalfOpenInterval(t *testing.T) {
	east := RegionIDForSectors(0x70, 0x5c)
	west := RegionIDForSectors(0x6f, 0x5c)
	from := RegionXZ{RegionID: east, X: 3, Z: 3}
	to := RegionXZ{RegionID: west, X: 1549, Z: 1549}

	got := Interpolate(from, to, float64(6)/float64(748))
	if got.RegionID != west {
		t.Fatalf("interpolated region=%#04x, want west=%#04x", got.RegionID, west)
	}
	if got.X < 0 || got.X >= OutdoorRegionSize || got.Z < 0 || got.Z >= OutdoorRegionSize {
		t.Fatalf("interpolated local=(%v,%v) outside [0, %v)", got.X, got.Z, OutdoorRegionSize)
	}
	if math.Floor(got.X/OutdoorRegionSize) != 0 || math.Floor(got.Z/OutdoorRegionSize) != 0 {
		t.Fatalf("interpolated local=(%v,%v) floors to non-zero sector offset", got.X, got.Z)
	}
	if again := NormalizeOutdoor(got); again != got {
		t.Fatalf("NormalizeOutdoor not idempotent: %+v -> %+v", got, again)
	}
}

/*
================
TestOutdoorLocalCellIsTheRegionLocalGrid

Six 320-unit cells per outdoor region axis, region-local: a point at the
frame's edge or outside it, a NaN, and every dungeon point have no cell
(never folded into a neighbour, unlike the interest blocks).
================
*/
func TestOutdoorLocalCellIsTheRegionLocalGrid(t *testing.T) {
	for _, test := range []struct {
		name     string
		position RegionXZ
		x, z     int
		ok       bool
	}{
		{"origin", RegionXZ{RegionID: 0x62a8, X: 0, Z: 0}, 0, 0, true},
		{"last cell", RegionXZ{RegionID: 0x62a8, X: 1919.9, Z: 640}, 5, 2, true},
		{"frame edge", RegionXZ{RegionID: 0x62a8, X: 1920, Z: 10}, 0, 0, false},
		{"negative", RegionXZ{RegionID: 0x62a8, X: -1, Z: 10}, 0, 0, false},
		{"NaN", RegionXZ{RegionID: 0x62a8, X: math.NaN(), Z: 10}, 0, 0, false},
		{"dungeon", RegionXZ{RegionID: 0x8001, X: 100, Z: 100}, 0, 0, false},
	} {
		x, z, ok := OutdoorLocalCell(test.position)
		if x != test.x || z != test.z || ok != test.ok {
			t.Errorf("%s: (%d, %d, %t), want (%d, %d, %t)", test.name, x, z, ok, test.x, test.z, test.ok)
		}
	}
}

/*
================
TestScopeRingIsTheSectorRingOutdoorsAndSelfInACave
================
*/
func TestScopeRingIsTheSectorRingOutdoorsAndSelfInACave(t *testing.T) {
	if ring := ScopeRing(0x8001); len(ring) != 1 || ring[0] != 0x8001 {
		t.Fatalf("cave ring %v, want only the cave itself", ring)
	}
	ring := ScopeRing(0x62a8)
	if len(ring) != 9 || ring[4] != 0x62a8 {
		t.Fatalf("outdoor ring %v, want 9 sectors centred on 0x62a8", ring)
	}
}

/*
================
TestSameFloorAndSectorOffset

SameFloor joins every outdoor sector and only one cave floor to itself;
SectorOffset is the sector delta in world units outdoors and nothing on
the cave plane (#1764).
================
*/
func TestSameFloorAndSectorOffset(t *testing.T) {
	floors := []struct {
		a, b uint16
		want bool
	}{
		{0x62a8, 0x63a9, true}, {0x8001, 0x8001, true}, {0x8001, 0x8004, false}, {0x62a8, 0x8001, false},
	}
	for _, f := range floors {
		if got := SameFloor(f.a, f.b); got != f.want {
			t.Errorf("SameFloor(%#x, %#x) = %v, want %v", f.a, f.b, got, f.want)
		}
	}
	if dx, dz, ok := SectorOffset(0x62a8, 0x63a9); !ok || dx != OutdoorRegionSize || dz != OutdoorRegionSize {
		t.Errorf("outdoor offset (%v, %v, %v), want one region each way", dx, dz, ok)
	}
	if dx, dz, ok := SectorOffset(0x8001, 0x8004); ok || dx != 0 || dz != 0 {
		t.Errorf("cave offset (%v, %v, %v), want none", dx, dz, ok)
	}
}
