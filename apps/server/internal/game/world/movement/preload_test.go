/*
===========================================================================

preload_test.go - boot warming must remove reads, never change navigation

The same immutable assets serve cold and preloaded validators. Compare their
owner and rescue results, then forbid any further asset read on the warm one.

===========================================================================
*/
package movement

import (
	"math"
	"opensro.online/server/internal/testsupport/tier"
	"os"
	"runtime"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestPreloadOutdoorNavigationPreservesQueriesWithoutReads
================
*/
func TestPreloadOutdoorNavigationPreservesQueriesWithoutReads(t *testing.T) {
	root := syntheticObjectNavRoot(t)
	cold, warm := NewWaterValidator(root), NewWaterValidator(root)
	reads := 0
	warm.readFile = func(name string) ([]byte, error) {
		reads++
		return os.ReadFile(name)
	}
	count, err := warm.PreloadOutdoorNavigation()
	if err != nil || count != 1 || reads < 4 {
		t.Fatalf("preload: count=%d reads=%d error=%v", count, reads, err)
	}
	initialReads := reads
	for _, point := range []simulation.Spawn{
		{RegionID: 0x6b4f, X: 100, Y: 10, Z: 100},
		{RegionID: 0x6b4f, X: 500, Y: 10, Z: 500},
		{RegionID: 0x6b4f, X: 100, Y: 0, Z: 100},
		{RegionID: 0x6b4f, X: 1500, Y: 10, Z: 1500},
		{RegionID: 0x6b4f, X: 900, Y: 10, Z: 900},
	} {
		wantOwner, wantY, wantOK := cold.ResolveNavOwner(point, simulation.NavOwner{})
		owner, y, ok := warm.ResolveNavOwner(point, simulation.NavOwner{})
		if owner != wantOwner || y != wantY || ok != wantOK {
			t.Fatalf("owner changed at %+v: %+v/%v/%v versus %+v/%v/%v", point, owner, y, ok, wantOwner, wantY, wantOK)
		}
		want, wantStranded, wantFound := cold.RelocateStrandedSpawn(point)
		got, stranded, found := warm.RelocateStrandedSpawn(point)
		if got != want || stranded != wantStranded || found != wantFound {
			t.Fatalf("rescue changed at %+v", point)
		}
	}
	if _, err := warm.PreloadOutdoorNavigation(); err != nil {
		t.Fatal(err)
	}
	if reads != initialReads {
		t.Fatalf("queries after boot performed %d asset reads", reads-initialReads)
	}
}

/*
================
TestPreloadOutdoorNavigationRequiresCatalog
================
*/
func TestPreloadOutdoorNavigationRequiresCatalog(t *testing.T) {
	if _, err := NewWaterValidator(t.TempDir()).PreloadOutdoorNavigation(); err == nil {
		t.Fatal("missing boot catalog was accepted")
	}
}

/*
================
TestExactTerrainOwnerNeedsNoObjectAssets

Terrain wins a zero-distance tie, but a retained object cell still wins before
new ownership arbitration. This shortcut must never replace a valid deck hint.
================
*/
func TestExactTerrainOwnerNeedsNoObjectAssets(t *testing.T) {
	v := NewWaterValidator(syntheticObjectNavRoot(t))
	point := simulation.Spawn{RegionID: 0x6b4f, X: 100, Z: 100}
	if y, ok := v.TerrainHeightAt(point.RegionID, point.X, point.Z); !ok || y != 0 {
		t.Fatalf("invalid terrain fixture: %v/%v", y, ok)
	}
	readFile := v.readFile
	reads := 0
	v.readFile = func(name string) ([]byte, error) {
		reads++
		return readFile(name)
	}
	owner, y, ok := v.ResolveNavOwner(point, simulation.NavOwner{})
	if owner.Kind != simulation.NavOwnerTerrain || y != 0 || !ok || reads != 0 {
		t.Fatalf("exact terrain arbitration: owner=%+v y=%v ok=%v reads=%d", owner, y, ok, reads)
	}
	deckPoint := point
	deckPoint.Y = 10
	deck, _, _ := v.ResolveNavOwner(deckPoint, simulation.NavOwner{})
	if deck.Kind != simulation.NavOwnerObject {
		t.Fatal("fixture has no deck owner")
	}
	owner, y, ok = v.ResolveNavOwner(point, deck)
	if owner != deck || y != 10 || !ok {
		t.Fatalf("terrain shortcut discarded retained deck: %+v/%v/%v", owner, y, ok)
	}
}

/*
================
BenchmarkPreloadOutdoorNavigation

Run explicitly with -benchtime=1x to measure boot cost on the licensed map.
================
*/
func BenchmarkPreloadOutdoorNavigation(b *testing.B) {
	root := gamedatatest.WorldAuthorityDir(b)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	b.ResetTimer()
	for range b.N {
		v := NewAuthorityValidator(root)
		if _, err := v.PreloadOutdoorNavigation(); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		runtime.GC()
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		b.ReportMetric(float64(after.HeapAlloc-before.HeapAlloc), "retained-B")
		runtime.KeepAlive(v)
		b.StartTimer()
	}
}

/*
================
TestCatalogObjectRotationCacheIsBitExact

Check every resolved outdoor placement against the old per-query expressions.
No float32 store or SinCos substitution may change the collision transform.
================
*/
func TestCatalogObjectRotationCacheIsBitExact(t *testing.T) {
	tier.Long(t, "every outdoor placement rotation is bit-exact")
	v := NewAuthorityValidator(gamedatatest.WorldAuthorityDir(t))
	if _, err := v.PreloadOutdoorNavigation(); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, set := range v.objectNavSets {
		for _, object := range set {
			if math.Float64bits(object.cosYaw) != math.Float64bits(math.Cos(object.placement.yaw)) ||
				math.Float64bits(object.sinYaw) != math.Float64bits(math.Sin(object.placement.yaw)) {
				t.Fatalf("cached rotation changed at placement %+v", object.placement)
			}
			count++
		}
	}
	if count == 0 {
		t.Fatal("catalog test covered no object placements")
	}
	t.Logf("bit-exact rotations for %d resolved outdoor placements", count)
}
