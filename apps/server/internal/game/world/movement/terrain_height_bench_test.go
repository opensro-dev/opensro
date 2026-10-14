/*
===========================================================================

terrain_height_bench_test.go - what a terrain height costs across the world

Population spawns test terrain heights at nests all over the world, so the
working set of one AdvancePopulation pass is every region's height grid,
not the few around players. This measures one height lookup at scattered
real points. A 2 MiB page cache in front of the grids measured 10-20 us per
lookup here (a disk read on nearly every call) against 0.25 us in memory.

===========================================================================
*/
package movement

import (
	"math/rand"
	"testing"
)

/*
================
benchHeightPoints

Every terrain region of the shipped world (one probe per sector), then
scattered points inside them, visited in a fixed random order.
================
*/
func benchHeightPoints(b *testing.B, v *WaterValidator) [][3]float64 {
	var regions []uint16
	for y := 0; y < 128; y++ {
		for x := 0; x < 256; x++ {
			region := uint16(y<<8 | x)
			if _, ok := v.TerrainHeightAt(region, 960, 960); ok {
				regions = append(regions, region)
			}
		}
	}
	if len(regions) == 0 {
		b.Fatal("no terrain regions")
	}
	rng := rand.New(rand.NewSource(1))
	points := make([][3]float64, 1<<16)
	for i := range points {
		points[i] = [3]float64{float64(regions[rng.Intn(len(regions))]), rng.Float64() * 1919, rng.Float64() * 1919}
	}
	b.Logf("%d terrain regions", len(regions))
	return points
}

/*
================
BenchmarkTerrainHeightScattered
================
*/
func BenchmarkTerrainHeightScattered(b *testing.B) {
	v := realAuthorityValidator(b)
	points := benchHeightPoints(b, v)
	for _, p := range points {
		v.TerrainHeightAt(uint16(p[0]), p[1], p[2])
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := points[i%len(points)]
		v.TerrainHeightAt(uint16(p[0]), p[1], p[2])
	}
}
