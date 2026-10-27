/*
===========================================================================

water_assets.go - outdoor region catalog, bundle decoding, and surface cache

Resolves region IDs to decoded groundSurface bundles (water blocks, height
grids, blocked-tile grids, and object placement tables) with lock-free hot-path
region lookup and load-outside-lock cold resolution.

===========================================================================
*/
package movement

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math"
	"path/filepath"
	"strings"
	"sync/atomic"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	maxEncodedRegionSize = 0xffff
	encodedScalarBytes   = 4
	// maxNavGridAxis bounds decoded quadratic planes. Shipped grids use
	// 96 tiles / 97 vertices; 1024 leaves format headroom without allowing
	// an asset field to request multi-gigabyte allocations.
	maxNavGridAxis  = 1024
	maskedWaterType = 0
	flatWaterType   = 1
)

// emptyRegionSurface is the sentinel stored in WaterValidator.regionSurfaces
// for a region that resolved to nil (a cached negative miss).
var emptyRegionSurface groundSurface

/*
================
regionSurfaceTable
================
*/
func (v *WaterValidator) regionSurfaceTable() *[65536]atomic.Pointer[groundSurface] {
	if table := v.regionSurfaces.Load(); table != nil {
		return table
	}
	allocated := new([65536]atomic.Pointer[groundSurface])
	if v.regionSurfaces.CompareAndSwap(nil, allocated) {
		return allocated
	}
	return v.regionSurfaces.Load()
}

/*
================
storeRegionSurface
================
*/
func storeRegionSurface(table *[65536]atomic.Pointer[groundSurface], regionID uint16, surface *groundSurface) {
	if surface == nil {
		table[regionID].Store(&emptyRegionSurface)
		return
	}
	table[regionID].Store(surface)
}

/*
================
surfaceForRegion

surfaceForRegion resolves the ground surface for a destination region; nil
means "no surface" and the caller accepts (reference .catch). Warm hits read
the lock-free regionSurfaces table; cold resolution runs outside v.mu and
publishes double-checked.
================
*/
func (v *WaterValidator) surfaceForRegion(regionID uint16) *groundSurface {
	if table := v.regionSurfaces.Load(); table != nil {
		if cached := table[regionID].Load(); cached != nil {
			if cached == &emptyRegionSurface {
				return nil
			}
			return cached
		}
	}

	v.mu.Lock()
	surface, cached := v.surfaceByRegion[regionID]
	v.mu.Unlock()
	if cached {
		storeRegionSurface(v.regionSurfaceTable(), regionID, surface)
		return surface
	}

	v.cacheMisses.Add(1)
	resolved := v.resolveSurfaceForRegion(regionID)

	v.mu.Lock()
	if existing, ok := v.surfaceByRegion[regionID]; ok {
		// Another goroutine resolved this region meanwhile; its published
		// result wins (the underlying caches are load-once, so both
		// resolutions are equivalent - adopt theirs and drop ours).
		resolved = existing
	} else {
		v.surfaceByRegion[regionID] = resolved
	}
	v.mu.Unlock()

	storeRegionSurface(v.regionSurfaceTable(), regionID, resolved)
	return resolved
}

/*
================
NavCacheMisses

Cold surface/object-nav resolutions since boot (#1820 part C). The
monster leg diffs it around itself; expected delta zero, which is what
guards the boot preload.
================
*/
func (v *WaterValidator) NavCacheMisses() uint64 {
	if v == nil {
		return 0
	}
	return v.cacheMisses.Load()
}

/*
================
resolveSurfaceForRegion

resolveSurfaceForRegion is the uncached catalog + index resolution
(surfaceForRegion memoizes the result). Callers must NOT hold v.mu: each loader
below takes and releases it around its own cache, keeping the disk reads
outside.
================
*/
func (v *WaterValidator) resolveSurfaceForRegion(regionID uint16) *groundSurface {
	requestedHex := formatRegionID(regionID)

	catalog := v.loadCatalog()
	if catalog == nil {
		return nil
	}
	entries := catalog.RegionsByID[requestedHex]
	if len(entries) == 0 {
		entries = catalog.RegionsByID[strings.ToLower(requestedHex)]
	}
	if len(entries) == 0 {
		return nil
	}
	entry := selectCatalogEntry(entries, requestedHex)
	worldRegionsPath := entry.worldRegionsPath()
	if worldRegionsPath == "" {
		return nil
	}

	index := v.loadIndex(worldRegionsPath)
	if index == nil {
		return nil
	}
	bundlePublicPath := entry.bundlePath()
	for _, indexed := range index.Regions {
		if normalizeRegionID(indexed.ID) == requestedHex {
			if indexed.bundlePath() != "" {
				bundlePublicPath = indexed.bundlePath()
			}
			break
		}
	}
	if bundlePublicPath == "" {
		return nil
	}

	return v.surfaceForBundle(bundlePublicPath, index)
}

/*
================
surfaceForBundle

surfaceForBundle loads (or answers from cache) one region bundle's surface.
The build - a disk read plus JSON decode of the largest asset in this type -
runs with v.mu released; nil (a failed build) is cached too, so a
known-unloadable bundle never re-reads the disk.
================
*/
func (v *WaterValidator) surfaceForBundle(bundlePublicPath string, index *regionIndexFile) *groundSurface {
	v.mu.Lock()
	surface, cached := v.surfaces[bundlePublicPath]
	v.mu.Unlock()
	if cached {
		return surface
	}

	built := v.buildSurface(bundlePublicPath, index)

	v.mu.Lock()
	defer v.mu.Unlock()
	if surface, cached := v.surfaces[bundlePublicPath]; cached {
		return surface
	}
	v.surfaces[bundlePublicPath] = built
	return built
}

/*
================
selectCatalogEntry

selectCatalogEntry ports selectMissionWorldCatalogEntry's preference order:
seedRegionId match, then id match, then the first entry.
================
*/
func selectCatalogEntry(entries []catalogEntry, requestedHex string) catalogEntry {
	for _, entry := range entries {
		if normalizeRegionID(entry.SeedRegionID) == requestedHex {
			return entry
		}
	}
	for _, entry := range entries {
		if normalizeRegionID(entry.ID) == requestedHex {
			return entry
		}
	}
	return entries[0]
}

/*
================
loadCatalog

loadCatalog is the one-shot world-region catalog load. A failed load is cached
as a nil negative (catalogLoaded still flips), so a deployment without the
asset degrades to accept-all once instead of re-reading the disk on every cold
region. The read itself runs with v.mu released.
================
*/
func (v *WaterValidator) loadCatalog() *regionCatalog {
	v.mu.Lock()
	if v.catalogLoaded {
		catalog := v.catalog
		v.mu.Unlock()
		return catalog
	}
	v.mu.Unlock()

	catalog := &regionCatalog{}
	if !v.readJSON(v.catalogPath, catalog) {
		catalog = nil
	}
	if catalog != nil {
		for name := range catalog.RegionsByID {
			if v.hidden.HidesRegion(parseRegionID(name)) {
				delete(catalog.RegionsByID, name)
			}
		}
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	if v.catalogLoaded {
		return v.catalog
	}
	v.catalog = catalog
	v.catalogLoaded = true
	return catalog
}

/*
================
loadIndex

loadIndex loads (or answers from cache) one region index file; a failed load
caches nil, the existing negative semantic. The read runs with v.mu released.
================
*/
func (v *WaterValidator) loadIndex(publicPath string) *regionIndexFile {
	v.mu.Lock()
	index, cached := v.indexes[publicPath]
	v.mu.Unlock()
	if cached {
		return index
	}

	loaded := &regionIndexFile{}
	if !v.readJSON(publicPath, loaded) {
		loaded = nil
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	if index, cached := v.indexes[publicPath]; cached {
		return index
	}
	v.indexes[publicPath] = loaded
	return loaded
}

/*
================
buildSurface

buildSurface ports the water-table half of createMissionGroundSurface. Callers
must NOT hold v.mu (surfaceForBundle owns caching the result).
================
*/
func (v *WaterValidator) buildSurface(bundlePublicPath string, index *regionIndexFile) *groundSurface {
	bundle := &regionBundle{}
	if !v.readJSON(bundlePublicPath, bundle) {
		return nil
	}
	// The reference returns null without a navmesh region list; preserve
	// the gate so the two servers accept/reject the same destinations.
	if len(bundle.Navmesh.Regions) == 0 {
		return nil
	}

	sectors := bundle.Terrain.Sectors
	if len(sectors) == 0 && len(bundle.Terrain.Blocks) > 0 {
		sectors = []bundleSector{{
			SectorX: bundle.Source.SectorX,
			SectorY: bundle.Source.SectorY,
			Blocks:  bundle.Terrain.Blocks,
		}}
	}

	waterByOffset := make(map[int64]*waterSector)
	for _, sector := range sectors {
		water := &waterSector{}
		any := false
		for _, block := range sector.Blocks {
			blockX, okX := jsonInt(block.BlockX)
			blockZ, okZ := jsonInt(block.BlockZ)
			if !okX || !okZ || blockX < 0 || blockX >= waterBlocksPerAxis ||
				blockZ < 0 || blockZ >= waterBlocksPerAxis {
				continue
			}
			if block.Water == nil {
				continue
			}
			// isNativeWaterCell port (TerrainHeightField.ts): a cell has water
			// iff type==0 (masked water - waveType is irrelevant, the 0x5FA5
			// gorge is type 0 / waveType 0) OR type==1 with waveType!=0
			// (flat pickable water). type -1/0xFF is NO water: dry regions
			// keep stale records with nonzero waveType and height, and
			// gating on waveType alone invents phantom water over every
			// depression below the stale height (runtime-caught 2026-07-27
			// at region 0x5FA8: dry hollow at y=-33.7 silently refused
			// against a stale type=-1 height=-20 record).
			waterType, typeErr := block.Water.Type.Float64()
			waveType, _ := block.Water.WaveType.Float64()
			height, heightErr := block.Water.Height.Float64()
			if typeErr != nil || heightErr != nil || math.IsNaN(height) || math.IsInf(height, 0) {
				continue
			}
			if !(waterType == maskedWaterType || (waterType == flatWaterType && waveType != 0)) {
				continue
			}
			index := blockZ*waterBlocksPerAxis + blockX
			water.hasWater[index] = true
			water.heights[index] = height
			any = true
		}
		if !any {
			continue
		}
		waterByOffset[offsetKey(sector.SectorX-bundle.Source.SectorX, sector.SectorY-bundle.Source.SectorY)] = water
	}

	seedRegionID := parseRegionID(bundle.Source.SectorID)
	if bundle.Source.SectorID == "" {
		seedRegionID = parseRegionID(index.SeedRegionID)
	}

	regionSize := simulation.NativeRegionSize
	if navSize, ok := jsonInt(bundle.Navmesh.RegionSize); ok && navSize >= 1 && navSize <= maxEncodedRegionSize {
		regionSize = float64(navSize)
	}
	if indexSize, ok := jsonInt(index.RegionSize); ok && indexSize >= 1 && indexSize <= maxEncodedRegionSize {
		regionSize = float64(indexSize)
	}

	placements, err := buildObjectPlacements(bundle)
	if err != nil {
		log.Warnf("movement: invalid terrain object associations: %v", err)
		return nil
	}
	heights := buildHeightGrids(bundle)
	return &groundSurface{
		seedRegionID:                  seedRegionID,
		regionSize:                    regionSize,
		waterByOffset:                 waterByOffset,
		heightsByOffset:               heights,
		blockedByOffset:               buildBlockedGrids(bundle),
		objectPlacementsByOffset:      placements,
		objectResourceIndexPublicPath: bundle.Objects.resourceIndexPath(),
	}
}

/*
================
buildObjectPlacements

buildObjectPlacements decodes each navmesh region entry's nav object placement
list for the sealed-deck rescue (objectnav.go). A malformed list degrades to no
placements for that entry only.
================
*/
func buildObjectPlacements(bundle *regionBundle) (map[int64][]objectNavPlacement, error) {
	placements := make(map[int64][]objectNavPlacement)
	for _, region := range bundle.Navmesh.Regions {
		rows, entries := decodeObjectPlacements(region.Objects)
		if len(rows) == 0 {
			continue
		}
		if err := admitTerrainObjectCells(region, rows, entries); err != nil {
			return nil, err
		}
		dx, _ := jsonInt(region.Dx)
		dz, _ := jsonInt(region.Dz)
		placements[offsetKey(dx, dz)] = rows
	}
	return placements, nil
}

/*
================
buildBlockedGrids

buildBlockedGrids decodes each navmesh region's byte-per-tile walkability
plane. A malformed or absent plane degrades to "no coverage" for that region
only.
================
*/
func buildBlockedGrids(bundle *regionBundle) map[int64]*blockedGrid {
	grids := make(map[int64]*blockedGrid)
	tilesPerAxis, okTiles := jsonInt(bundle.Navmesh.TilesPerAxis)
	tileSize, okSize := jsonInt(bundle.Navmesh.TileSize)
	if !okTiles || tilesPerAxis < 1 || tilesPerAxis > maxNavGridAxis ||
		!okSize || tileSize <= 0 {
		return grids
	}
	tileCount := tilesPerAxis * tilesPerAxis
	for _, region := range bundle.Navmesh.Regions {
		cellCount, okCells := jsonInt(region.Cells.Count)
		if region.BlockedTiles == "" || region.TileCellIds == "" || !okCells || cellCount < 1 {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(region.BlockedTiles)
		if err != nil || len(raw) != tileCount {
			continue
		}
		rawCellIDs, err := base64.StdEncoding.DecodeString(region.TileCellIds)
		if err != nil || len(rawCellIDs) != tileCount*encodedScalarBytes {
			continue
		}
		// Only the combined predicate is queried after admission. Retaining
		// the source cell IDs and blocked bytes costs 40 times as much.
		walkable := make([]uint64, (tileCount+63)/64)
		for i := 0; i < tileCount; i++ {
			if raw[i] == 0 && uint64(binary.LittleEndian.Uint32(rawCellIDs[i*encodedScalarBytes:])) < uint64(cellCount) {
				walkable[i/64] |= uint64(1) << uint(i%64)
			}
		}
		dx, _ := jsonInt(region.Dx)
		dz, _ := jsonInt(region.Dz)
		grids[offsetKey(dx, dz)] = &blockedGrid{
			tilesPerAxis: tilesPerAxis,
			tileSize:     float64(tileSize),
			walkable:     walkable,
		}
	}
	return grids
}

/*
================
buildHeightGrids

buildHeightGrids decodes each navmesh region's base64 little-endian float32
height map. A malformed or absent map degrades to "no coverage" for that region
only (the standard accept/absent failure policy).
================
*/
func buildHeightGrids(bundle *regionBundle) map[int64]*heightGrid {
	grids := make(map[int64]*heightGrid)
	axisVertices, okAxis := jsonInt(bundle.Navmesh.HeightMapAxisVertices)
	vertexStep, okStep := jsonInt(bundle.Navmesh.TileSize)
	if !okAxis || axisVertices < 2 || axisVertices > maxNavGridAxis ||
		!okStep || vertexStep <= 0 {
		return grids
	}
	for _, region := range bundle.Navmesh.Regions {
		if region.HeightMap == "" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(region.HeightMap)
		if err != nil || len(raw) != axisVertices*axisVertices*encodedScalarBytes {
			continue
		}
		heights := make([]float32, axisVertices*axisVertices)
		for i := range heights {
			heights[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*encodedScalarBytes:]))
		}
		dx, _ := jsonInt(region.Dx)
		dz, _ := jsonInt(region.Dz)
		grids[offsetKey(dx, dz)] = &heightGrid{
			axisVertices: axisVertices,
			vertexStep:   float64(vertexStep),
			heights:      heights,
		}
		if region.PlaneType != "" || region.PlaneHeight != "" {
			types, typeErr := base64.StdEncoding.DecodeString(region.PlaneType)
			planes, heightErr := base64.StdEncoding.DecodeString(region.PlaneHeight)
			if typeErr != nil || heightErr != nil || len(types) != 36 || len(planes) != 36*4 {
				delete(grids, offsetKey(dx, dz))
				continue
			}
			grid := grids[offsetKey(dx, dz)]
			grid.planeTypes, grid.planeHeights = types, make([]float32, 36)
			for i := range grid.planeHeights {
				value := math.Float32frombits(binary.LittleEndian.Uint32(planes[i*4:]))
				if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
					delete(grids, offsetKey(dx, dz))
					break
				}
				grid.planeHeights[i] = value
			}
		}
	}
	return grids
}

/*
================
readJSON

readJSON loads one public asset; false degrades to accept, loudly at debug
level only (missing dungeon bundles are normal).
================
*/
func (v *WaterValidator) readJSON(publicPath string, out any) bool {
	assetPath := v.assetPath(publicPath)
	if assetPath == "" {
		return false
	}
	text, err := v.readFile(assetPath)
	if err != nil {
		log.Debugf("movement: water asset %s unreadable: %v", publicPath, err)
		return false
	}
	if err := json.Unmarshal(text, out); err != nil {
		log.Debugf("movement: water asset %s undecodable: %v", publicPath, err)
		return false
	}
	return true
}

/*
================
assetPath

assetPath ports publicAssetPath's containment rule: the resolved path must stay
under the public root.
================
*/
func (v *WaterValidator) assetPath(publicPath string) string {
	if publicPath == "" {
		return ""
	}
	rootAbs, err := filepath.Abs(v.root)
	if err != nil {
		return ""
	}
	resolved := filepath.Join(rootAbs, filepath.FromSlash(strings.TrimLeft(publicPath, "/")))
	resolved = filepath.Clean(resolved)
	if resolved != rootAbs && !strings.HasPrefix(resolved, rootAbs+string(filepath.Separator)) {
		return ""
	}
	return resolved
}
