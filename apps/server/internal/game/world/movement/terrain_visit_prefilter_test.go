/*
===========================================================================

terrain_visit_prefilter_test.go - the union-box prefilter is exact

terrainVisitKey answers a chord whose box misses a placement's union box
without clipping each cell. Over real published placements and random
chords, the answer must equal the plain first-entry scan.

===========================================================================
*/
package movement

import (
	"math"
	"math/rand"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

const (
	// prefilterChordsPerPlacement is the random chords tried per placement.
	prefilterChordsPerPlacement = 200
	// prefilterReach spreads chord ends this far around the union box.
	prefilterReach = 400.0
)

/*
================
plainTerrainVisitKey

terrainVisitKey without the prefilter: the first entry over every cell.
================
*/
func plainTerrainVisitKey(cells *terrainCellSet, x0, z0, x1, z1 float64) float64 {
	if cells == nil {
		return 0
	}
	best := math.Inf(1)
	for _, r := range cells.cells {
		if t, ok := chordRectEntry(x0, z0, x1, z1, float64(r[0]), float64(r[1]), float64(r[2]), float64(r[3])); ok && t < best {
			best = t
		}
	}
	return best
}

/*
================
TestTerrainVisitPrefilterMatchesThePlainScan
================
*/
func TestTerrainVisitPrefilterMatchesThePlainScan(t *testing.T) {
	licensed.RequireGameData(t)
	v := NewAuthorityValidator(gamedatatest.WorldAuthorityDir(t))
	random := rand.New(rand.NewSource(1))
	// Jangan's potion building and the rebirth building: town placements
	// with registered terrain cells (terrain_object_cells_test.go).
	checked := 0
	for _, region := range []uint16{25000, 27471} {
		surface := v.surfaceForRegion(region)
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				for _, object := range v.objectNavSetForOffset(surface, dx, dz) {
					cells := object.placement.terrainCells
					if cells == nil || len(cells.cells) == 0 {
						continue
					}
					box := cells.box
					point := func() (float64, float64) {
						x := float64(box[0]) - prefilterReach + random.Float64()*(float64(box[2]-box[0])+2*prefilterReach)
						z := float64(box[1]) - prefilterReach + random.Float64()*(float64(box[3]-box[1])+2*prefilterReach)
						return x, z
					}
					for range prefilterChordsPerPlacement {
						x0, z0 := point()
						x1, z1 := point()
						// Edge cases: a chord ending on the union's edge, and a point chord.
						switch random.Intn(10) {
						case 0:
							x1 = float64(box[0])
						case 1:
							x1, z1 = x0, z0
						}
						want := plainTerrainVisitKey(cells, x0, z0, x1, z1)
						if got := terrainVisitKey(cells, x0, z0, x1, z1); got != want && !(math.IsInf(got, 1) && math.IsInf(want, 1)) {
							t.Fatalf("region %d placement %d chord (%v,%v)->(%v,%v): prefiltered %v, plain %v",
								region, object.placement.ordinal, x0, z0, x1, z1, got, want)
						}
						checked++
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no published placement with registered terrain cells was checked")
	}
}

/*
================
BenchmarkTerrainVisitKeyPublished

One chord past every placement of a town sector, prefiltered.
================
*/
func BenchmarkTerrainVisitKeyPublished(b *testing.B) {
	licensed.RequireGameData(b)
	v := NewAuthorityValidator(gamedatatest.WorldAuthorityDir(b))
	set := v.objectNavSetForOffset(v.surfaceForRegion(25000), 0, 0)
	from := simulation.Spawn{X: 100, Z: 100}
	b.ResetTimer()
	for range b.N {
		for i := range set {
			terrainVisitKey(set[i].placement.terrainCells, from.X, from.Z, from.X+40, from.Z+5)
		}
	}
}

/*
================
BenchmarkTerrainVisitKeyPublishedPlain

The same chords through the plain scan, the cost before the prefilter.
================
*/
func BenchmarkTerrainVisitKeyPublishedPlain(b *testing.B) {
	licensed.RequireGameData(b)
	v := NewAuthorityValidator(gamedatatest.WorldAuthorityDir(b))
	set := v.objectNavSetForOffset(v.surfaceForRegion(25000), 0, 0)
	from := simulation.Spawn{X: 100, Z: 100}
	b.ResetTimer()
	for range b.N {
		for i := range set {
			plainTerrainVisitKey(set[i].placement.terrainCells, from.X, from.Z, from.X+40, from.Z+5)
		}
	}
}
