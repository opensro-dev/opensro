/*
===========================================================================

dungeon_reach_test.go - the dungeon reach skip never skips a block that
mattered

clipDungeonPath, its passage resolve and the resident heightAt skip a block
whose reach box the chord or point misses (objectnav_reach.go). This runs
random chords over every real dungeon surface twice: on the surfaces as
loaded, and on copies with every reach cleared (an unknown reach is never
skipped, so the copy scans every block: the brute-force reference). The
clip reports and the heights must be identical.

===========================================================================
*/
package movement

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

// dungeonChordsPerRegion is the chord count per dungeon surface.
const dungeonChordsPerRegion = 4000

/*
================
everyBlockSurfaces

Copies of surfaces with every block and object reach and every cell box
cleared (meshes cloned without them), so no scan skips anything.
================
*/
func everyBlockSurfaces(surfaces map[uint16]*dungeonSpawnSurface) map[uint16]*dungeonSpawnSurface {
	out := make(map[uint16]*dungeonSpawnSurface, len(surfaces))
	bare := map[*objectNavMesh]*objectNavMesh{}
	strip := func(meshes []*objectNavMesh) []*objectNavMesh {
		copied := make([]*objectNavMesh, len(meshes))
		for i, m := range meshes {
			if bare[m] == nil {
				clone := *m
				clone.cellBoxes = nil
				bare[m] = &clone
			}
			copied[i] = bare[m]
		}
		return copied
	}
	for region, s := range surfaces {
		blocks := append([]dungeonSpawnBlock(nil), s.blocks...)
		objects := append([]resolvedObjectNav(nil), s.objects...)
		for i := range blocks {
			blocks[i].reach = objectReach{}
			blocks[i].meshes = strip(blocks[i].meshes)
		}
		for i := range objects {
			objects[i].reach = objectReach{}
			objects[i].meshes = strip(objects[i].meshes)
		}
		out[region] = &dungeonSpawnSurface{messageGrid: s.messageGrid, blocks: blocks, objects: objects}
	}
	return out
}

/*
================
dungeonChordStart

A point on a random cell of a random block, in the surface's frame: where
movers actually stand.
================
*/
func dungeonChordStart(random *rand.Rand, s *dungeonSpawnSurface) (float64, float64, float64) {
	b := s.blocks[random.Intn(len(s.blocks))]
	m := b.meshes[random.Intn(len(b.meshes))]
	if m.cellCount() == 0 {
		return b.x, b.y, b.z
	}
	cell := random.Intn(m.cellCount())
	var x, y, z float64
	for _, v := range [3]uint16{m.cellA[cell], m.cellB[cell], m.cellC[cell]} {
		x += float64(m.vertices[v*3]) / 3
		y += float64(m.vertices[v*3+1]) / 3
		z += float64(m.vertices[v*3+2]) / 3
	}
	return b.cosYaw*x - b.sinYaw*z + b.x, y + b.y, b.sinYaw*x + b.cosYaw*z + b.z
}

/*
================
dungeonValidators

The real dungeon surfaces, with the reach skip, and the every-block
reference.
================
*/
func dungeonValidators(t testing.TB) (*WaterValidator, *WaterValidator, []uint16) {
	t.Helper()
	v := realAuthorityValidator(t)
	v.dungeonSpawnOnce.Do(func() { v.dungeonSpawnSurfaces = v.loadDungeonSpawnSurfaces() })
	if len(v.dungeonSpawnSurfaces) == 0 {
		t.Fatal("no dungeon surfaces loaded")
	}
	every := &WaterValidator{dungeonSpawnSurfaces: everyBlockSurfaces(v.dungeonSpawnSurfaces)}
	every.dungeonSpawnOnce.Do(func() {})
	regions := make([]uint16, 0, len(v.dungeonSpawnSurfaces))
	for region := range v.dungeonSpawnSurfaces {
		regions = append(regions, region)
	}
	sort.Slice(regions, func(i, j int) bool { return regions[i] < regions[j] })
	return v, every, regions
}

/*
================
TestDungeonReachSkipMatchesEveryBlockScanned
================
*/
func TestDungeonReachSkipMatchesEveryBlockScanned(t *testing.T) {
	if testing.Short() {
		t.Skip("long: real-data dungeon chords")
	}
	v, every, regions := dungeonValidators(t)
	random := rand.New(rand.NewSource(1018))
	outcomes := map[string]int{}
	for _, region := range regions {
		surface := v.dungeonSpawnSurfaces[region]
		reference := every.dungeonSpawnSurfaces[region]
		for i := 0; i < dungeonChordsPerRegion; i++ {
			x, y, z := dungeonChordStart(random, surface)
			angle, length := random.Float64()*2*math.Pi, 1+random.Float64()*119
			from := simulation.Spawn{RegionID: region, X: x, Y: y, Z: z}
			to := simulation.Spawn{RegionID: region, X: x + math.Cos(angle)*length, Y: y + (random.Float64()-.5)*20,
				Z: z + math.Sin(angle)*length}
			skipped, scanned := v.clipDungeonPath(from, to), every.clipDungeonPath(from, to)
			if fmt.Sprintf("%+v", skipped) != fmt.Sprintf("%+v", scanned) {
				t.Fatalf("region %#x chord %+v -> %+v:\n skipped %+v\n every   %+v", region, from, to, skipped, scanned)
			}
			outcomes[fmt.Sprintf("%v/%x", skipped.Outcome, skipped.NativeResult)]++
			hx, hz := x+(random.Float64()-.5)*400, z+(random.Float64()-.5)*400
			h1, ok1 := surface.heightAt(hx, y, hz)
			h2, ok2 := reference.heightAt(hx, y, hz)
			if h1 != h2 || ok1 != ok2 {
				t.Fatalf("region %#x height at %v,%v,%v: %v %v vs every %v %v", region, hx, y, hz, h1, ok1, h2, ok2)
			}
		}
	}
	// Both arrivals and clips must occur, or the equality proves little.
	arrived, stopped := 0, 0
	for outcome, n := range outcomes {
		if outcome[:len(fmt.Sprint(ClipArrived))] == fmt.Sprint(ClipArrived) {
			arrived += n
		} else {
			stopped += n
		}
	}
	if arrived < 1000 || stopped < 1000 {
		t.Fatalf("only %d arrived and %d stopped chords: %v", arrived, stopped, outcomes)
	}
	t.Logf("%d regions, %d chords identical both ways: %v", len(regions), len(regions)*dungeonChordsPerRegion, outcomes)
}

/*
================
BenchmarkClipDungeonPath

One clipped dungeon step, with the reach skip and with every block
scanned, over the densest real dungeon surface.
================
*/
func BenchmarkClipDungeonPath(b *testing.B) {
	v, every, regions := dungeonValidators(b)
	densest := regions[0]
	for _, region := range regions {
		if len(v.dungeonSpawnSurfaces[region].objects) > len(v.dungeonSpawnSurfaces[densest].objects) {
			densest = region
		}
	}
	random := rand.New(rand.NewSource(7))
	chords := make([][2]simulation.Spawn, 1024)
	for i := range chords {
		x, y, z := dungeonChordStart(random, v.dungeonSpawnSurfaces[densest])
		angle := random.Float64() * 2 * math.Pi
		chords[i] = [2]simulation.Spawn{{RegionID: densest, X: x, Y: y, Z: z},
			{RegionID: densest, X: x + math.Cos(angle)*8, Y: y, Z: z + math.Sin(angle)*8}}
	}
	b.Logf("region %#x, %d blocks", densest, len(v.dungeonSpawnSurfaces[densest].objects))
	for _, run := range []struct {
		name string
		v    *WaterValidator
	}{{"reach", v}, {"every", every}} {
		b.Run(run.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				c := chords[i%len(chords)]
				run.v.clipDungeonPath(c[0], c[1])
			}
		})
	}
}
