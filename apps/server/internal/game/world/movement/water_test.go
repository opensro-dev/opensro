package movement

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"opensro.online/server/internal/game/world/simulation"
)

// writeTestAsset drops one file under the synthetic public root.
func writeTestAsset(t *testing.T, root, publicPath, contents string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(publicPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

// syntheticWaterRoot builds a minimal catalog/index/bundle tree for region
// 0x6B4F covering every corner of the isNativeWaterCell gate: block (0,0)
// type-0 water at surface 50; block (1,0) a type -1 STALE record (dry
// despite waveType 3 - the 0x5FA8 live catch); block (2,0) type-0 water
// with waveType 0 (still water, the 0x5FA5 gorge shape); block (3,0)
// type-1 waveType-0 (dry); block (4,0) type-1 waveType-2 (water).
func syntheticWaterRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTestAsset(t, root, "assets/world/world-region-catalog.json", `{
		"regionsById": {
			"0x6b4f": [{
				"id": "0x6b4f",
				"seedRegionId": "0x6b4f",
				"worldRegionsPublicPath": "/assets/world/outdoor/world-regions.json",
				"bundlePublicPath": "/assets/world/outdoor/regions/region-6b4f.json"
			}]
		}
	}`)
	writeTestAsset(t, root, "assets/world/outdoor/world-regions.json", `{
		"regionSize": 1920,
		"seedRegionId": "0x0000",
		"regions": [
			{"id": "0x6b4f", "bundlePublicPath": "/assets/world/outdoor/regions/region-6b4f.json"}
		]
	}`)
	writeTestAsset(t, root, "assets/world/outdoor/regions/region-6b4f.json", `{
		"source": {"sectorId": "0x6b4f", "sectorX": 79, "sectorY": 107},
		"terrain": {"sectors": [{
			"sectorX": 79, "sectorY": 107,
			"blocks": [
				{"blockX": 0, "blockZ": 0, "water": {"type": 0, "waveType": 1, "height": 50}},
				{"blockX": 1, "blockZ": 0, "water": {"type": -1, "waveType": 3, "height": -20}},
				{"blockX": 2, "blockZ": 0, "water": {"type": 0, "waveType": 0, "height": 50}},
				{"blockX": 3, "blockZ": 0, "water": {"type": 1, "waveType": 0, "height": 50}},
				{"blockX": 4, "blockZ": 0, "water": {"type": 1, "waveType": 2, "height": 50}}
			]
		}]},
		"navmesh": {"regionSize": 1920, "regions": [{}]}
	}`)
	// A no-navmesh region: the reference builds NO surface for it, so every
	// destination there is accepted regardless of water.
	writeTestAsset(t, root, "assets/world/world-region-catalog-extra.json", `{}`)
	return root
}

func moveTo(regionID uint16, x, y, z float64) simulation.MovementRequest {
	return simulation.NormalizeMovementRequest(simulation.MovementRequest{Mode: 1, RegionID: regionID, X: x, Y: y, Z: z})
}

func TestWaterValidatorSyntheticSurface(t *testing.T) {
	validator := NewWaterValidator(syntheticWaterRoot(t))

	cases := []struct {
		name   string
		move   simulation.MovementRequest
		refuse bool
	}{
		// Block (0,0) spans x/z in [0,320): water surface 50.
		{"seabedDeepReject", moveTo(0x6B4F, 100, 10, 100), true},         // submersion 40
		{"boundaryJustOverReject", moveTo(0x6B4F, 100, 37.9, 100), true}, // submersion 12.1
		{"wadeAtMaxDepthAccept", moveTo(0x6B4F, 100, 38, 100), false},    // submersion 12 == max, not >
		{"shallowWadeAccept", moveTo(0x6B4F, 100, 45, 100), false},       // submersion 5
		{"bridgeDeckAboveAccept", moveTo(0x6B4F, 100, 80, 100), false},   // above the surface
		// Block (1,0) x in [320,640): type -1 stale record = DRY. The live
		// catch: a dry hollow 13.7u below the stale height must accept.
		{"staleTypeMinusOneDryAccept", moveTo(0x6B4F, 400, -33.7, 100), false},
		// Block (2,0) x in [640,960): type 0 + waveType 0 is STILL water.
		{"typeZeroWaveZeroWaterReject", moveTo(0x6B4F, 700, 10, 100), true}, // submersion 40
		// Block (3,0) x in [960,1280): type 1 + waveType 0 = dry.
		{"typeOneWaveZeroDryAccept", moveTo(0x6B4F, 1000, 10, 100), false},
		// Block (4,0) x in [1280,1600): type 1 + waveType != 0 = water.
		{"typeOneWaveNonzeroWaterReject", moveTo(0x6B4F, 1300, 10, 100), true}, // submersion 40
		{"dryBlockAccept", moveTo(0x6B4F, 100, 10, 400), false},                // block (0,1) has no record
		{"unknownRegionAccept", moveTo(0x1234, 100, -250, 100), false},         // no catalog entry
		{"nonDestinationModeAccept", simulation.MovementRequest{Mode: 2, RegionID: 0x6B4F, X: 100, Y: 10, Z: 100}, false},
	}
	for _, tc := range cases {
		refusal := validator.ValidateMovement(tc.move)
		if tc.refuse && refusal == nil {
			t.Errorf("%s: expected deep-water refusal", tc.name)
		}
		if !tc.refuse && refusal != nil {
			t.Errorf("%s: unexpected refusal %v", tc.name, refusal)
		}
		if refusal != nil && refusal.NativeErrorCode != 0x02 {
			t.Errorf("%s: nativeErrorCode = 0x%02X, want 0x02", tc.name, refusal.NativeErrorCode)
		}
	}
}

// encodeHeightMap packs row-major float32 heights as the bundle's base64
// little-endian height map field.
func encodeHeightMap(heights []float32) string {
	raw := make([]byte, len(heights)*4)
	for i, h := range heights {
		binary.LittleEndian.PutUint32(raw[i*4:], math.Float32bits(h))
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// syntheticHeightRoot builds a catalog/index/bundle tree at the REAL bundle
// geometry (96x96 tiles of 20 units, 97x97 height grid) for region 0x6B4F
// (sector 79,107) and its fully-open EAST neighbor 0x6B50 (sector 80,107):
//
//   - heights form the plane y = x/20 (vertex height = x vertex index), so
//     the bilinear sample anywhere is exactly x/20;
//   - 0x6B4F tiles x,z in [4,7] are BLOCKED (a 4x4 obstacle);
//   - 0x6B4F tiles x,z in [60,62] are an open ISLAND ringed by the blocked
//     moat x,z in [58,64] (9 walkable tiles, no way out in ANY region);
//   - 0x6B4F tiles x in [94,95], z in [0,23] are an open BORDER SLIVER
//     walled off in-region (x=93 and the z=24 cap blocked) but continuing
//     east into 0x6B50's open field - mainland only via the neighbor;
//   - 0x6B4F tile (10,10) is unblocked but its cell id misses the cell list.
func syntheticHeightRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTestAsset(t, root, "assets/world/world-region-catalog.json", `{
		"regionsById": {
			"0x6b4f": [{
				"id": "0x6b4f",
				"seedRegionId": "0x6b4f",
				"worldRegionsPublicPath": "/assets/world/outdoor/world-regions.json",
				"bundlePublicPath": "/assets/world/outdoor/regions/region-6b4f.json"
			}],
			"0x6b50": [{
				"id": "0x6b50",
				"seedRegionId": "0x6b50",
				"worldRegionsPublicPath": "/assets/world/outdoor/world-regions.json",
				"bundlePublicPath": "/assets/world/outdoor/regions/region-6b50.json"
			}]
		}
	}`)
	writeTestAsset(t, root, "assets/world/outdoor/world-regions.json", `{
		"regionSize": 1920,
		"seedRegionId": "0x0000",
		"regions": [
			{"id": "0x6b4f", "bundlePublicPath": "/assets/world/outdoor/regions/region-6b4f.json"},
			{"id": "0x6b50", "bundlePublicPath": "/assets/world/outdoor/regions/region-6b50.json"}
		]
	}`)

	const axis = 96
	heights := make([]float32, 97*97)
	for i := range heights {
		heights[i] = float32(i % 97)
	}
	heightMap := encodeHeightMap(heights)
	inRange := func(v, lo, hi int) bool { return v >= lo && v <= hi }

	blockedA := make([]byte, axis*axis)
	cellIDsA := make([]byte, axis*axis*4)
	for z := 0; z < axis; z++ {
		for x := 0; x < axis; x++ {
			obstacle := inRange(x, 4, 7) && inRange(z, 4, 7)
			moat := inRange(x, 58, 64) && inRange(z, 58, 64)
			island := inRange(x, 60, 62) && inRange(z, 60, 62)
			sliverWall := (x == 93 && inRange(z, 0, 24)) || (inRange(x, 94, 95) && z == 24)
			if obstacle || (moat && !island) || sliverWall {
				blockedA[z*axis+x] = 1
			}
		}
	}
	binary.LittleEndian.PutUint32(cellIDsA[(10*axis+10)*4:], 99) // cell-less tile (10,10)
	writeTestAsset(t, root, "assets/world/outdoor/regions/region-6b4f.json", fmt.Sprintf(`{
		"source": {"sectorId": "0x6b4f", "sectorX": 79, "sectorY": 107},
		"terrain": {"sectors": [{"sectorX": 79, "sectorY": 107, "blocks": []}]},
		"navmesh": {
			"regionSize": 1920,
			"tileSize": 20,
			"tilesPerAxis": 96,
			"heightMapAxisVertices": 97,
			"regions": [{"dx": 0, "dz": 0, "regionId": 27471, "heightMap": %q, "blockedTiles": %q, "tileCellIds": %q, "cells": {"count": 1}}]
		}
	}`,
		heightMap,
		base64.StdEncoding.EncodeToString(blockedA),
		base64.StdEncoding.EncodeToString(cellIDsA)))

	blockedB := make([]byte, axis*axis)
	cellIDsB := make([]byte, axis*axis*4)
	writeTestAsset(t, root, "assets/world/outdoor/regions/region-6b50.json", fmt.Sprintf(`{
		"source": {"sectorId": "0x6b50", "sectorX": 80, "sectorY": 107},
		"terrain": {"sectors": [{"sectorX": 80, "sectorY": 107, "blocks": []}]},
		"navmesh": {
			"regionSize": 1920,
			"tileSize": 20,
			"tilesPerAxis": 96,
			"heightMapAxisVertices": 97,
			"regions": [{"dx": 0, "dz": 0, "regionId": 27472, "heightMap": %q, "blockedTiles": %q, "tileCellIds": %q, "cells": {"count": 1}}]
		}
	}`,
		heightMap,
		base64.StdEncoding.EncodeToString(blockedB),
		base64.StdEncoding.EncodeToString(cellIDsB)))
	return root
}

func TestTerrainHeightAtSamplesNavmeshGrid(t *testing.T) {
	validator := NewWaterValidator(syntheticHeightRoot(t))

	// The fixture's height plane is y = x/20 everywhere.
	cases := []struct {
		name string
		x, z float64
		want float64
	}{
		{"originVertex", 0, 0, 0},
		{"midRegion", 960, 960, 48},
		{"bilinearMidCell", 250, 130, 12.5},
		{"nearEastEdge", 1910, 40, 95.5},
	}
	for _, tc := range cases {
		got, ok := validator.TerrainHeightAt(0x6B4F, tc.x, tc.z)
		if !ok {
			t.Errorf("%s: expected height coverage at (%v, %v)", tc.name, tc.x, tc.z)
			continue
		}
		if math.Abs(got-tc.want) > 1e-6 {
			t.Errorf("%s: height = %v, want %v", tc.name, got, tc.want)
		}
	}

	if _, ok := validator.TerrainHeightAt(0x6B4F, -10, 100); ok {
		t.Error("point in an uncovered neighbor sector must report no coverage")
	}
	if _, ok := validator.TerrainHeightAt(0x1234, 100, 100); ok {
		t.Error("unknown region must report no coverage")
	}
}

func TestWalkableTerrainHeightAtRejectsBlockedTiles(t *testing.T) {
	validator := NewWaterValidator(syntheticHeightRoot(t))

	height, ok := validator.WalkableTerrainHeightAt(0x6B4F, 250, 130)
	if !ok || math.Abs(height-12.5) > 1e-6 {
		t.Fatalf("open tile height = %v, ok=%v; want 12.5, true", height, ok)
	}
	if _, ok := validator.WalkableTerrainHeightAt(0x6B4F, 100, 100); ok {
		t.Fatal("blocked obstacle tile accepted as a monster spawn")
	}
	if _, ok := validator.WalkableTerrainHeightAt(0x6B4F, 200, 200); ok {
		t.Fatal("tile without a native nav cell accepted as a monster spawn")
	}
	if _, ok := validator.WalkableTerrainHeightAt(0x6B4F, -1, 100); ok {
		t.Fatal("out-of-frame point accepted as a monster spawn")
	}
}

func TestRelocateStrandedSpawnRescuesToMainland(t *testing.T) {
	validator := NewWaterValidator(syntheticHeightRoot(t))
	assertMainland := func(t *testing.T, rescued simulation.Spawn, label string) {
		t.Helper()
		if _, stranded, _ := validator.RelocateStrandedSpawn(rescued); stranded {
			t.Fatalf("%s: rescue point must itself be mainland: %+v", label, rescued)
		}
	}

	// (110, 110) sits inside the blocked 4x4 obstacle (tiles 4..7); the
	// rescue must land on adjacent mainland with the plane height applied.
	rescued, stranded, found := validator.RelocateStrandedSpawn(
		simulation.Spawn{RegionID: 0x6B4F, X: 110, Y: 999, Z: 110, Angle: 7},
	)
	if !stranded || !found {
		t.Fatalf("blocked spawn must rescue: stranded=%v found=%v", stranded, found)
	}
	if rescued.RegionID != 0x6B4F {
		t.Fatalf("rescue stayed in-region, got 0x%04X", rescued.RegionID)
	}
	if math.Abs(rescued.Y-rescued.X/20) > 1e-6 {
		t.Fatalf("rescue height = %v, want plane %v", rescued.Y, rescued.X/20)
	}
	if rescued.Angle != 7 {
		t.Fatalf("rescue must keep the facing, got %d", rescued.Angle)
	}
	assertMainland(t, rescued, "obstacle")

	// The WALKABLE island (9 tiles inside the moat) is the live incident's
	// second act: movement works locally but every path out clips at the
	// island edge. It must rescue BEYOND the moat onto the mainland.
	islandSpawn := simulation.Spawn{RegionID: 0x6B4F, X: 1230, Y: 61.5, Z: 1230}
	rescued, stranded, found = validator.RelocateStrandedSpawn(islandSpawn)
	if !stranded || !found {
		t.Fatalf("island spawn must rescue: stranded=%v found=%v", stranded, found)
	}
	if dist := simulation.WorldDistance2D(islandSpawn, rescued); dist < 60 {
		t.Fatalf("rescue must clear the moat (>=60u), moved %v", dist)
	}
	assertMainland(t, rescued, "island")

	// A mainland spawn passes through untouched.
	same, stranded, found := validator.RelocateStrandedSpawn(
		simulation.Spawn{RegionID: 0x6B4F, X: 710, Y: 35.5, Z: 250},
	)
	if stranded || found || same.X != 710 {
		t.Fatalf("mainland spawn must pass through: stranded=%v found=%v", stranded, found)
	}

	// No walkability data (unknown region) never counts as stranded.
	if _, stranded, _ := validator.RelocateStrandedSpawn(
		simulation.Spawn{RegionID: 0x1234, X: 100, Y: 0, Z: 100},
	); stranded {
		t.Fatal("missing walkability data must fail open, never stranded")
	}

	// Tile (10,10) is NOT blocked but its cell id misses the cell list -
	// the client's cells[tileCellIds[tile]] ?? null gate. Off the graph,
	// so it rescues too.
	_, stranded, found = validator.RelocateStrandedSpawn(
		simulation.Spawn{RegionID: 0x6B4F, X: 210, Y: 0, Z: 210},
	)
	if !stranded || !found {
		t.Fatalf("cell-less tile must rescue: stranded=%v found=%v", stranded, found)
	}
}

// TestRelocateStrandedSpawnCrossesRegionBorders is the "if it can break it
// will break" case: a walkable sliver at the region border, walled off
// in-region (48 tiles, far below the mainland threshold) but continuing
// east into the neighbor region's open field. The component fill must
// cross the border and recognize it as mainland - a region-local heuristic
// would wrongly relocate a legitimately-placed character.
func TestRelocateStrandedSpawnCrossesRegionBorders(t *testing.T) {
	validator := NewWaterValidator(syntheticHeightRoot(t))

	// Tile (94, 10) center in region 0x6B4F: inside the border sliver.
	sliver := simulation.Spawn{RegionID: 0x6B4F, X: 1890, Y: 94.5, Z: 210}
	if _, stranded, _ := validator.RelocateStrandedSpawn(sliver); stranded {
		t.Fatal("a border sliver connected to the neighbor region's field is mainland, not an island")
	}

	// Sanity: the neighbor region itself is mainland everywhere.
	if _, stranded, _ := validator.RelocateStrandedSpawn(
		simulation.Spawn{RegionID: 0x6B50, X: 960, Y: 48, Z: 960},
	); stranded {
		t.Fatal("the open neighbor region must be mainland")
	}
}

// TestRelocateStrandedSpawnRealMountain pins both acts of the live
// incident on the real bundles:
//
//  1. the stale-frame bug stranded character asd2 at region 0x60A0
//     (1842.79, 1279.23) - a BLOCKED mountain-face tile where the native
//     source-resolve gate rejected every move;
//  2. the first (walkability-only) rescue moved them to (1890, 1230) - a
//     walkable ISLAND plateau where movement worked but every path down
//     clipped at the island edge.
//
// Both must rescue onto the mainland.
func TestRelocateStrandedSpawnRealMountain(t *testing.T) {
	validator := realAuthorityValidator(t)

	for _, tc := range []struct {
		name  string
		spawn simulation.Spawn
	}{
		{"blockedMountainFace", simulation.Spawn{RegionID: 0x60A0, X: 1842.786, Y: 843.596, Z: 1279.231, Angle: 24576}},
		{"walkableIslandPlateau", simulation.Spawn{RegionID: 0x60A0, X: 1861, Y: 1017.941, Z: 1210, Angle: 24576}},
	} {
		rescued, stranded, found := validator.RelocateStrandedSpawn(tc.spawn)
		if !stranded {
			t.Fatalf("%s must report stranded", tc.name)
		}
		if !found {
			t.Fatalf("%s: a mainland rescue must exist within the search radius", tc.name)
		}
		if _, rescuedStranded, _ := validator.RelocateStrandedSpawn(rescued); rescuedStranded {
			t.Fatalf("%s: rescue point must itself be mainland: %+v", tc.name, rescued)
		}
		if dist := simulation.WorldDistance2D(tc.spawn, rescued); dist > 960 {
			t.Fatalf("%s: rescue moved %v units, want <= 960", tc.name, dist)
		}
	}
}

func TestTerrainHeightAtMissingAssetsDegradesToAbsent(t *testing.T) {
	validator := NewWaterValidator(t.TempDir())
	if _, ok := validator.TerrainHeightAt(0x6B4F, 100, 100); ok {
		t.Error("missing catalog must degrade to no-coverage, never a fabricated height")
	}
}

func TestWaterValidatorNoNavmeshAcceptsAll(t *testing.T) {
	root := t.TempDir()
	writeTestAsset(t, root, "assets/world/world-region-catalog.json", `{
		"regionsById": {"0x6b4f": [{
			"id": "0x6b4f", "seedRegionId": "0x6b4f",
			"worldRegionsPublicPath": "/assets/world/outdoor/world-regions.json",
			"bundlePublicPath": "/assets/world/outdoor/regions/region-6b4f.json"
		}]}
	}`)
	writeTestAsset(t, root, "assets/world/outdoor/world-regions.json",
		`{"regionSize": 1920, "regions": []}`)
	writeTestAsset(t, root, "assets/world/outdoor/regions/region-6b4f.json", `{
		"source": {"sectorId": "0x6b4f", "sectorX": 79, "sectorY": 107},
		"terrain": {"sectors": [{"sectorX": 79, "sectorY": 107, "blocks": [
			{"blockX": 0, "blockZ": 0, "water": {"waveType": 1, "height": 50}}
		]}]},
		"navmesh": {"regions": []}
	}`)
	validator := NewWaterValidator(root)
	if refusal := validator.ValidateMovement(moveTo(0x6B4F, 100, 10, 100)); refusal != nil {
		t.Errorf("no-navmesh bundle must accept (reference gate): %v", refusal)
	}
}

func TestWaterValidatorMissingAssetsAcceptAll(t *testing.T) {
	validator := NewWaterValidator(t.TempDir())
	if refusal := validator.ValidateMovement(moveTo(0x6850, 700, -250, 100)); refusal != nil {
		t.Errorf("missing catalog must degrade to accept: %v", refusal)
	}
}

// TestColdRegionLoadNeverReadsUnderLock proves the load-outside-lock
// discipline directly: every asset read the validator performs must find
// v.mu RELEASED. The injected reader probes with TryLock - single
// goroutine, so a failed TryLock can only mean the loading path itself
// still holds the mutex across disk I/O.
func TestColdRegionLoadNeverReadsUnderLock(t *testing.T) {
	validator := NewWaterValidator(syntheticHeightRoot(t))
	reads := 0
	violations := 0
	validator.readFile = func(name string) ([]byte, error) {
		reads++
		if validator.mu.TryLock() {
			validator.mu.Unlock()
		} else {
			violations++
		}
		return os.ReadFile(name)
	}

	if _, ok := validator.TerrainHeightAt(0x6B4F, 100, 100); !ok {
		t.Fatal("cold region load failed - the probe never exercised the load path")
	}
	if reads < 3 {
		t.Fatalf("only %d asset reads observed, want catalog+index+bundle", reads)
	}
	if violations != 0 {
		t.Fatalf("%d asset read(s) happened while v.mu was held", violations)
	}

	// A negative load (missing region) must also read outside the lock,
	// and must be cached: the second miss does no further I/O.
	if _, ok := validator.TerrainHeightAt(0x1234, 100, 100); ok {
		t.Fatal("unknown region must have no coverage")
	}
	if violations != 0 {
		t.Fatalf("%d asset read(s) under v.mu on the negative path", violations)
	}
	readsAfterMiss := reads
	validator.TerrainHeightAt(0x1234, 100, 100)
	if reads != readsAfterMiss {
		t.Fatal("a cached negative region re-read the disk")
	}
}

// TestConcurrentColdRegionLoadsDoNotSerialize drives two goroutines into
// two different cold regions and requires both bundle reads to be IN
// FLIGHT at once: the injected reader holds each bundle read at a
// rendezvous until the other arrives. If one load still serialized the
// other behind the validator mutex, the second could never arrive and the
// rendezvous would time out.
func TestConcurrentColdRegionLoadsDoNotSerialize(t *testing.T) {
	validator := NewWaterValidator(syntheticHeightRoot(t))
	entered := make(chan string, 2)
	release := make(chan struct{})
	validator.readFile = func(name string) ([]byte, error) {
		if strings.Contains(name, "region-6b4f.json") || strings.Contains(name, "region-6b50.json") {
			entered <- filepath.Base(name)
			select {
			case <-release:
			case <-time.After(30 * time.Second):
				// Fall through; the main goroutine already failed the test.
			}
		}
		return os.ReadFile(name)
	}

	var loaders sync.WaitGroup
	loaders.Add(2)
	go func() {
		defer loaders.Done()
		validator.TerrainHeightAt(0x6B4F, 100, 100)
	}()
	go func() {
		defer loaders.Done()
		validator.TerrainHeightAt(0x6B50, 100, 100)
	}()

	for arrived := 0; arrived < 2; arrived++ {
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Errorf("only %d bundle load(s) got in flight - cold loads serialize", arrived)
			arrived = 2
		}
	}
	close(release)
	loaders.Wait()

	if t.Failed() {
		return
	}
	if _, ok := validator.TerrainHeightAt(0x6B4F, 100, 100); !ok {
		t.Fatal("region 0x6B4F failed to load through the concurrent path")
	}
	if _, ok := validator.TerrainHeightAt(0x6B50, 100, 100); !ok {
		t.Fatal("region 0x6B50 failed to load through the concurrent path")
	}
}

// realAuthorityValidator resolves the exact immutable server projection used
// by GameWorld. The shipped development artifact is compressed, so reaching
// into the pre-compression directory would silently skip every real-asset
// regression after packaging moved to server.srogz.
func realAuthorityValidator(t *testing.T) *WaterValidator {
	t.Helper()
	paths := gamedatatest.Paths(t)
	return NewAuthorityValidator(paths.WorldAuthorityDir)
}

// TestWaterValidatorRealHarborAndGorge drives the validator over the REAL
// exported region bundles at the two reference-documented proof points: the
// Constantinople harbor (0x6850, seabed ~-250 under surface -170) and the
// 0x5FA5 gorge (water 50, bridge decks above it stay legal).
func TestWaterValidatorRealHarborAndGorge(t *testing.T) {
	validator := realAuthorityValidator(t)

	// Harbor block (2,0): x in [640,960), z in [0,320), surface -170.
	if refusal := validator.ValidateMovement(moveTo(0x6850, 700, -250, 100)); refusal == nil {
		t.Error("harbor seabed click must reject (submersion 80u)")
	}
	if refusal := validator.ValidateMovement(moveTo(0x6850, 700, -165, 100)); refusal != nil {
		t.Errorf("harbor surface swim at -165 (5u wade) must accept: %v", refusal)
	}

	// Gorge block (0,0): water surface 50.
	if refusal := validator.ValidateMovement(moveTo(0x5FA5, 100, 20, 100)); refusal == nil {
		t.Error("gorge drowned click must reject (submersion 30u)")
	}
	if refusal := validator.ValidateMovement(moveTo(0x5FA5, 100, 80, 100)); refusal != nil {
		t.Errorf("gorge bridge deck above the water must accept: %v", refusal)
	}

	// Europe start plateau: y=80 dry land clicks stay legal (the SCOUT
	// probe's move must never trip the gate).
	if refusal := validator.ValidateMovement(moveTo(0x6B4F, 1205, 80, 396)); refusal != nil {
		t.Errorf("Europe start click must accept: %v", refusal)
	}

	// Region 0x5FA8 block (0,0) carries a STALE type=-1 water record
	// (waveType 3, height -20). The live catch (MOVE-TRACE#28,
	// 2026-07-27): a dry hollow at y=-33.7 was silently refused as
	// 13.7u-deep phantom water while the client's own gate (which honours
	// the type field) accepted the click - the character never moved.
	if refusal := validator.ValidateMovement(moveTo(0x5FA8, 125.388, -33.728, 113.952)); refusal != nil {
		t.Errorf("0x5FA8 dry hollow under a stale type=-1 record must accept: %v", refusal)
	}
}

func TestWalkableSpawnHeightAtRealBanditNpcposAnchor(t *testing.T) {
	validator := realAuthorityValidator(t)
	height, ok := validator.WalkableSpawnHeightAt(
		0x5CA0,
		778.04999,
		1414.3,
		711.40002,
	)
	if !ok {
		t.Fatal("Bandit npcpos anchor has no walkable spawn surface")
	}
	if math.Abs(height-1390.334670643) > 0.001 {
		t.Fatalf("Bandit npcpos authoredY=1414.3 resolvedY=%.9f, want 1390.334670643", height)
	}
}
