package movement

import (
	"math"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

func TestPublishedJanganPotionBuildingEntryAndWestwardTrap(t *testing.T) {
	licensed.RequireGameData(t)
	v := NewAuthorityValidator(gamedatatest.WorldAuthorityDir(t))
	inside := simulation.Spawn{RegionID: 25000, X: 1640, Y: 0, Z: 1390}
	for _, z := range []float64{1250, 1300, 1550} {
		for _, y := range []float64{0, 8.8, 40} {
			from := inside
			from.Z, from.Y = z, y
			got := v.ClipMovementPath(from, inside)
			if got.Outcome != ClipBlocked || got.Class != ClipClassObject || got.TilesUncovered != 0 || math.Hypot(got.Rest.X-inside.X, got.Rest.Z-inside.Z) <= 40 {
				t.Fatalf("solid building entry from %+v: %+v", from, got)
			}
		}
	}
	west := inside
	west.X = 1590
	got := v.ClipMovementPath(inside, west)
	if got.Outcome != ClipBlocked || got.Class != ClipClassObject || got.Rest.X <= 1594.7 || got.Rest.X >= 1594.9 {
		t.Fatalf("reported westward boundary: %+v", got)
	}
	// A private validator permits a discriminating historical control without
	// changing published files or any live character: omit the terrain candidate
	// associations and the old edge-Y window admits the high wall but not the low one.
	set := v.objectNavSetForOffset(v.surfaceForRegion(inside.RegionID), 0, 0)
	if len(set) == 0 {
		t.Fatal("missing published object coverage")
	}
	for i := range set {
		if set[i].placement.terrainCells == nil {
			t.Fatal("published object is using legacy height admission")
		}
		set[i].placement.terrainCells = nil
	}
	from := inside
	from.Z = 1300
	if got := v.ClipMovementPath(from, inside); got.Outcome != ClipArrived {
		t.Fatalf("historical entry control: %+v", got)
	}
	if got := v.ClipMovementPath(inside, west); got.Outcome != ClipBlocked {
		t.Fatalf("historical westward trap control: %+v", got)
	}
}

func TestPublishedRebirthBuildingHeightIndependentEntry(t *testing.T) {
	licensed.RequireGameData(t)
	v := NewAuthorityValidator(gamedatatest.WorldAuthorityDir(t))
	for _, y := range []float64{80, 82, 120} {
		a := simulation.Spawn{RegionID: 27471, X: 1205, Y: y, Z: 396}
		b := simulation.Spawn{RegionID: 27471, X: 1394, Y: 82, Z: 398}
		got := v.ClipMovementPath(a, b)
		if got.Outcome != ClipBlocked || got.Class != ClipClassObject || got.Rest.X >= 1390 {
			t.Fatalf("height %v admitted building entry: %+v", y, got)
		}
	}
}
