/*
===========================================================================

water.go - outdoor water-submersion gate and terrain/spawn height authority

WaterValidator owns the exported outdoor region catalog, water blocks, terrain
height grids, blocked-tile grids, and object-nav caches for movement and spawn
authority.

===========================================================================
*/
package movement

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/world/alexandria"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	waterCatalogPublicPath = "assets/world/world-region-catalog.json"
	// maxMovementAssetBytes is above the largest shipped region bundle
	// (about 86 MiB) while bounding accidental or hostile asset expansion.
	maxMovementAssetBytes int64 = 128 << 20
)

/*
================
WaterValidator

WaterValidator owns the outdoor movement assets: the optional deep-water
destination gate (validateMissionMovementForWorld, SRO_DEEP_WATER_REFUSAL),
terrain heights for spawns, and the nav caches the clips walk. The original
client sends seabed clicks and the original server's move path has no water
rule; the depth refusal came from a reference port, so the gate is
port-only and off by default.

The surface source is the MAPM 320-unit water block table (6x6 per region)
inside the exported region bundles - the same data the retail server derives
its water table from. The navmesh HEIGHT MAP is parsed alongside it
(TerrainHeightAt - the enter-world underground-spawn lift); blockers and tiles
stay unparsed (the reference deliberately does NOT re-run client nav checks
server-side).

Failure policy mirrors the reference exactly: any unreadable catalog, index or
bundle degrades to ACCEPT (.catch(() => null)), never to a refusal or a fault.
================
*/
type WaterValidator struct {
	// closedObjectRescue: RelocateStrandedSpawn also frees a spawn inside a
	// closed object (placementguard.go, SRO_CLOSED_OBJECT_RESCUE).
	closedObjectRescue bool
	// deepWaterRefusal: ValidateMovement refuses submerged destinations
	// (SRO_DEEP_WATER_REFUSAL). Off is native.
	deepWaterRefusal     bool
	root                 string
	catalogPath          string
	dungeonResourcesPath string
	// aiNavDataPath packs the dungeons' native route tables (ainavdata.go);
	// empty over a client public root, which ships none.
	aiNavDataPath string
	// caveSeamStep is the port-only cave seam step (cave_native_route.go).
	caveSeamStep bool
	// caveModernChase and caveObstacleEscape are the port-only cave chase
	// planner and obstacle escape (cave_chase.go); zero is native.
	caveModernChase    bool
	caveObstacleEscape bool
	// readFile is the asset reader (os.ReadFile in production; tests inject
	// it to prove the load-lock discipline below).
	readFile func(name string) ([]byte, error)

	// mu guards the cache maps ONLY. Disk reads and JSON parsing always run
	// with mu RELEASED (load-outside-lock, double-checked on re-acquire):
	// a cold region's bundle load must never serialize every movement
	// validation and terrain lookup server-wide behind a disk read. Losers
	// of a load race discard their copy and adopt the published one.
	mu sync.Mutex
	// scanEveryPlacement builds every placement with an unknown reach, so
	// no scan skips it (objectnav_reach.go): tests compare the skipping
	// scans against this brute-force reference. Set before first use;
	// production never sets it.
	scanEveryPlacement bool
	// hidden names the Alexandria regions the catalog drops while
	// SRO_ALEXANDRIA is off (HideAlexandria). Set before first use.
	hidden *alexandria.Overlay
	// catalogLoaded marks the one-shot catalog load, success OR failure, so
	// a missing catalog is a cached negative rather than a re-read per cold
	// region (all caches here treat nil as a known-unloadable negative).
	catalogLoaded bool
	catalog       *regionCatalog
	indexes       map[string]*regionIndexFile
	surfaces      map[string]*groundSurface // by bundle public path; nil = known-unloadable
	// surfaceByRegion memoizes the region -> surface RESOLUTION (catalog +
	// index scan), which profiling showed dominating every per-move lookup
	// (the index walk normalizes ~2k region-id strings per call). Pure
	// memoization: every underlying cache (catalog/indexes/surfaces) is
	// load-once and never invalidated, so the resolution is deterministic
	// for the validator's lifetime. nil values are cached misses.
	surfaceByRegion map[uint16]*groundSurface
	// regionSurfaces is the lock-free hot-path mirror of surfaceByRegion,
	// indexed directly by uint16 regionID (emptyRegionSurface marks a
	// cached nil miss). Allocated on first region resolution.
	regionSurfaces atomic.Pointer[[65536]atomic.Pointer[groundSurface]]
	// cacheMisses counts cold resolutions: the surfaceForRegion resolve
	// path and objectNavSetReach set builds. The boot preload
	// (PreloadOutdoorNavigation) warms every catalog region and every
	// placement offset, so a rising count after boot is a first-use
	// load inside the simulation (#1820 part C). Read the delta, never
	// the absolute value (the preload itself resolves cold).
	cacheMisses atomic.Uint64
	// objectIndexes caches the object-resource index files the sealed-deck
	// rescue resolves placements through (objectnav.go); nil = negative.
	objectIndexes map[string]*objectResourceIndex
	// objectNavMeshes caches decoded object-nav payloads by mesh JSON
	// public path; the empty slice is the negative (most meshes carry no
	// nav payload).
	objectNavMeshes map[string][]*objectNavMesh
	// objectNavSets caches, per surface + sector offset, the placements
	// that resolve to at least one decoded nav mesh - the unit the
	// per-move object queries iterate (objectnav.go); nil/empty =
	// negative.
	objectNavSets map[objectNavSetKey][]resolvedObjectNav
	// objectNavReaches holds each cached set's union reach
	// (objectnav_reach.go), filled with the set: a chord that misses it
	// can skip the whole set.
	objectNavReaches map[objectNavSetKey]objectReach
	// sectorNeighborhoods caches, per outdoor sector regionID, the
	// deduplicated non-empty 3x3 anchor probe list for terrainOutlineEntry
	// and objectChordFirstContact. Allocated on first neighborhood query.
	sectorNeighborhoods atomic.Pointer[[65536]atomic.Pointer[sectorObjectNavNeighborhood]]
	// walkInSets holds, per cached set, the indices of the placements
	// whose meshes carry event-zone cells (walkin_zone.go).
	walkInSets map[objectNavSetKey][]int
	// dungeonSpawnOnce owns the separately packaged DOF resident-nav
	// plane. Dungeon coordinates are not outdoor sector-local frames and
	// must never enter surfaceByRegion/grid tile math.
	dungeonSpawnOnce     sync.Once
	dungeonSpawnSurfaces map[uint16]*dungeonSpawnSurface
}

/*
================
NewWaterValidator

NewWaterValidator builds the validator over an explicit client public asset root.
================
*/
func NewWaterValidator(clientPublicRoot string) *WaterValidator {
	return newWaterValidator(
		clientPublicRoot,
		waterCatalogPublicPath,
		dungeonResourcesPublicPath,
	)
}

/*
================
NewAuthorityValidator

NewAuthorityValidator builds movement authority over the generated server
projection. This is the production constructor; it has no knowledge of the
browser client's publish layout.
================
*/
func NewAuthorityValidator(worldAuthorityDir string) *WaterValidator {
	v := newWaterValidator(
		filepath.Join(worldAuthorityDir, "movement"),
		"catalog.json",
		filepath.Join("dungeon", "dungeon-resources.json"),
	)
	v.aiNavDataPath = filepath.Join("dungeon", "ainav-data.json")
	return v
}

/*
================
HideAlexandria

Port-only, not native: drops the overlay's regions from the catalog, so
movement, spawn placement and preload treat them as a native build does,
as regions that do not exist. Called once at boot, before first use.
================
*/
func (v *WaterValidator) HideAlexandria(hidden *alexandria.Overlay) {
	v.hidden = hidden
}

/*
================
newWaterValidator
================
*/
func newWaterValidator(root, catalogPath, dungeonResourcesPath string) *WaterValidator {
	v := &WaterValidator{
		root:                 root,
		catalogPath:          catalogPath,
		dungeonResourcesPath: dungeonResourcesPath,
		readFile:             boundedMovementAssetReader(root),
		indexes:              make(map[string]*regionIndexFile),
		surfaces:             make(map[string]*groundSurface),
		surfaceByRegion:      make(map[uint16]*groundSurface),
		objectIndexes:        make(map[string]*objectResourceIndex),
		objectNavMeshes:      make(map[string][]*objectNavMesh),
		objectNavSets:        make(map[objectNavSetKey][]resolvedObjectNav),
		objectNavReaches:     make(map[objectNavSetKey]objectReach),
		walkInSets:           make(map[objectNavSetKey][]int),
	}
	if _, err := os.Stat(v.assetPath(catalogPath)); err != nil {
		log.Warnf("movement: world-region catalog unreadable under %s (%v); deep-water gate degrades to accept-all", root, err)
	}
	return v
}

/*
================
boundedMovementAssetReader

boundedMovementAssetReader is the production file boundary for movement
authority. Paths are checked lexically against the root and opened through an
os.Root, which refuses any path - symlinked or not - that resolves outside it.
Regular files are required, and a size check happens before allocation and
again through a limited reader to cover replacement/growth races.
================
*/
func boundedMovementAssetReader(root string) func(string) ([]byte, error) {
	rootAbs, rootErr := filepath.Abs(root)

	return func(path string) ([]byte, error) {
		if rootErr != nil {
			return nil, fmt.Errorf("resolve asset root %s: %w", root, rootErr)
		}
		pathAbs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		relative, err := filepath.Rel(rootAbs, pathAbs)
		if err != nil || filepath.IsAbs(relative) ||
			relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("movement asset escapes public root: %s", path)
		}

		// The root is opened per read: a held directory handle would pin the
		// asset tree on Windows against replacement and removal.
		tree, err := os.OpenRoot(rootAbs)
		if err != nil {
			return nil, fmt.Errorf("open asset root %s: %w", rootAbs, err)
		}
		defer func() { _ = tree.Close() }()
		file, err := tree.Open(relative)
		if err != nil {
			return nil, fmt.Errorf("movement asset %s: %w", path, err)
		}
		defer func() { _ = file.Close() }()
		info, err := file.Stat()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("movement asset is not a regular file: %s", path)
		}
		if info.Size() < 0 || info.Size() > maxMovementAssetBytes {
			return nil, fmt.Errorf("movement asset is %d bytes; maximum is %d", info.Size(), maxMovementAssetBytes)
		}
		payload, err := io.ReadAll(io.LimitReader(file, maxMovementAssetBytes+1))
		if err != nil {
			return nil, err
		}
		if int64(len(payload)) > maxMovementAssetBytes {
			return nil, fmt.Errorf("movement asset grew past %d bytes while reading", maxMovementAssetBytes)
		}
		return payload, nil
	}
}

/*
================
ValidateSecurityAssets

ValidateSecurityAssets eagerly proves that the movement authority can resolve
both shipped starting worlds. Runtime path checks fail closed on uncovered
outdoor tiles; this boot check turns a wholly missing or broken asset tree into
a clear configuration error instead of a server where every first move is
rejected.
================
*/
func (v *WaterValidator) ValidateSecurityAssets() error {
	if v == nil || v.loadCatalog() == nil {
		return fmt.Errorf("movement: world-region catalog is required for movement authority")
	}
	for _, start := range []simulation.Spawn{simulation.EuropeStartProfile(), simulation.ChinaStartProfile()} {
		if v.surfaceForRegion(start.RegionID) == nil {
			return fmt.Errorf("movement: movement authority has no surface for start region 0x%04X", start.RegionID)
		}
	}
	return nil
}

var _ simulation.MovementValidator = (*WaterValidator)(nil)

/*
================
ValidateMovement

While SRO_DEEP_WATER_REFUSAL is on, ValidateMovement refuses a mode-1
destination whose submersion (water surface minus destination y) exceeds
simulation.MaxWadeDepth. The submersion is of the DESTINATION POINT, not the
terrain water column: bridge decks above the river carry a destination y above
the surface and stay legal, while seabed clicks reject. Port-only, not native:
off (the default) accepts every destination, as the original server does. Only
the destination is judged, so with the gate on a walk across deep water to a
shallow goal still passes, and a character stopped on that seabed could not
click its way out (BR-261023-2025-B5DA).
================
*/
func (v *WaterValidator) ValidateMovement(m simulation.MovementRequest) *simulation.MoveError {
	if !v.deepWaterRefusal || m.Mode != simulation.MovementAckDestinationMode {
		return nil
	}
	surface := v.surfaceForRegion(m.RegionID)
	if surface == nil {
		return nil
	}
	baseX := m.X + float64(simulation.SectorX(m.RegionID)-simulation.SectorX(surface.seedRegionID))*surface.regionSize
	baseZ := m.Z + float64(simulation.SectorY(m.RegionID)-simulation.SectorY(surface.seedRegionID))*surface.regionSize
	waterSurface, ok := surface.waterSurfaceHeightAt(baseX, baseZ)
	if !ok {
		return nil
	}
	submersion := waterSurface - m.Y
	if submersion > simulation.MaxWadeDepth {
		return &simulation.MoveError{
			NativeErrorCode: simulation.NativeErrorInvalidRequest,
			Reason: fmt.Sprintf("deepWaterDestination region=0x%04X (%.1f, %.1f) submersion=%.1f (surface=%.1f destY=%.1f)",
				m.RegionID, m.X, m.Z, submersion, waterSurface, m.Y),
		}
	}
	return nil
}

/*
================
TerrainHeightAt

TerrainHeightAt samples the exported navmesh height map at a region-local point
(native units). ok=false means "no height authority for that point" (missing
catalog/bundle/height map) and callers must treat it as absent, never as height
zero. Same load/cache path as the water gate; same degrade-to-absent failure
policy.
================
*/
func (v *WaterValidator) TerrainHeightAt(regionID uint16, x, z float64) (float64, bool) {
	surface := v.surfaceForRegion(regionID)
	if surface == nil {
		return 0, false
	}
	baseX := x + float64(simulation.SectorX(regionID)-simulation.SectorX(surface.seedRegionID))*surface.regionSize
	baseZ := z + float64(simulation.SectorY(regionID)-simulation.SectorY(surface.seedRegionID))*surface.regionSize
	return surface.terrainHeightAt(baseX, baseZ)
}

/*
================
WalkableTerrainHeightAt

WalkableTerrainHeightAt is the monster-population spawn boundary. Unlike
TerrainHeightAt, a height sample alone is insufficient: generated monsters may
only materialize on a tile the exported navmesh marks walkable.
================
*/
func (v *WaterValidator) WalkableTerrainHeightAt(regionID uint16, x, z float64) (float64, bool) {
	tilesPerAxis, tileSize, ok := v.gridParamsForRegion(regionID)
	if !ok || x < 0 || z < 0 ||
		x >= float64(tilesPerAxis)*tileSize ||
		z >= float64(tilesPerAxis)*tileSize {
		return 0, false
	}
	tile := globalTile{
		x: simulation.SectorX(regionID)*tilesPerAxis + int(math.Floor(x/tileSize)),
		z: simulation.SectorY(regionID)*tilesPerAxis + int(math.Floor(z/tileSize)),
	}
	if walkable, covered := v.globalTileWalkable(tile, tilesPerAxis); !covered || !walkable {
		return 0, false
	}
	return v.TerrainHeightAt(regionID, x, z)
}

/*
================
WalkableSpawnHeightAt

WalkableSpawnHeightAt resolves a radius-generated monster position against the
native surface nearest the nest's authored Y. Terrain is only eligible when its
nav tile is walkable; a containing object-nav cell may win the same nearest-Y
arbitration used by client CRTNavMeshTerrain_ResolvePosition even when the terrain below it is blocked
(bridges, platforms, and elevated quest arenas).

The plane that owns the region answers (plane_lookups.go): the outdoor
arm is this surface owner, the cave arm is the resident dungeon-nav host,
not outdoor sector math.
================
*/
func (v *WaterValidator) WalkableSpawnHeightAt(
	regionID uint16,
	x, authoredY, z float64,
) (float64, bool) {
	return v.validatorPlanes().walkableSpawnHeightAt(regionID, x, authoredY, z)
}
