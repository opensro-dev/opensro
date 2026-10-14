package movement

import (
	"math"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	spawnRescueMaxRadiusTiles = 48
	minMainlandComponentTiles = 128
	globalTileKeyShift        = 32
	maxWorldSector            = 0xff
)

// spawnRescueMaxRadiusTiles bounds the nearest-mainland search: 48 tiles at
// the native 20-unit tile size is a 960-unit rescue radius - enough to get
// off a mountain plateau into the valley without teleporting the player
// across the map.
// minMainlandComponentTiles separates "the walkable world" from an isolated
// pocket. A stranded character can stand on a WALKABLE island (a mountain
// plateau ringed by blocked cliff tiles - the live incident's second act:
// movement worked but every path out was clipped at the island edge), so a
// rescue target must belong to a connected walkable component of at least
// this many tiles (128 tiles = ~51k sq units). The component fill CROSSES
// region borders, so a border sliver continuing into a neighbor region is
// correctly counted as mainland.
// globalTile addresses one navmesh tile on the world grid:
// sector * tilesPerAxis + tile, uniform across regions.
type globalTile struct{ x, z int }

func (t globalTile) key() uint64 {
	return uint64(uint32(t.x))<<globalTileKeyShift | uint64(uint32(t.z))
}

// globalTileWalkable resolves one world-grid tile ACROSS bundle boundaries
// through the same cached region loader. ok=false when no walkability
// coverage exists there (missing bundle, or a bundle whose tile geometry
// disagrees with the query's tilesPerAxis).
func (v *WaterValidator) globalTileWalkable(tile globalTile, tilesPerAxis int) (walkable, ok bool) {
	if tile.x < 0 || tile.z < 0 {
		return false, false
	}
	sectorX := tile.x / tilesPerAxis
	sectorY := tile.z / tilesPerAxis
	if sectorX > maxWorldSector || sectorY > maxWorldSector {
		return false, false
	}
	regionID := simulation.RegionIDForSectors(sectorX, sectorY)
	surface := v.surfaceForRegion(regionID)
	if surface == nil {
		return false, false
	}
	grid := surface.blockedByOffset[offsetKey(
		sectorX-simulation.SectorX(surface.seedRegionID),
		sectorY-simulation.SectorY(surface.seedRegionID),
	)]
	if grid == nil || grid.tilesPerAxis != tilesPerAxis {
		return false, false
	}
	return grid.tileWalkable((tile.z%tilesPerAxis)*tilesPerAxis + tile.x%tilesPerAxis), true
}

// tileOnMainland runs a BOUNDED flood fill over the walkable world-grid
// tile graph, crossing region borders: the tile is mainland when its
// connected component reaches minMainlandComponentTiles before exhausting.
// The fill is capped at the threshold, so each query touches at most ~128
// tiles; cache carries verdicts across queries of one rescue (every tile a
// fill visits shares its component's verdict).
func (v *WaterValidator) tileOnMainland(start globalTile, tilesPerAxis int, cache map[uint64]bool) bool {
	if verdict, known := cache[start.key()]; known {
		return verdict
	}
	visited := map[uint64]bool{start.key(): true}
	queue := []globalTile{start}
	mainland := false
	for len(queue) > 0 {
		if len(visited) >= minMainlandComponentTiles {
			mainland = true
			break
		}
		tile := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, neighbor := range [4]globalTile{
			{tile.x - 1, tile.z}, {tile.x + 1, tile.z},
			{tile.x, tile.z - 1}, {tile.x, tile.z + 1},
		} {
			if visited[neighbor.key()] {
				continue
			}
			if walkable, ok := v.globalTileWalkable(neighbor, tilesPerAxis); !ok || !walkable {
				continue
			}
			visited[neighbor.key()] = true
			queue = append(queue, neighbor)
		}
	}
	// Every visited tile is in the start's component: on an early mainland
	// break they connect to a component that already cleared the threshold;
	// on exhaustion they are the entire island.
	for key := range visited {
		cache[key] = mainland
	}
	return mainland
}

// RelocateStrandedSpawn is the enter-world unstuck: a spawn is STRANDED when
// it sits on a blocked navmesh tile, on a walkable island too small to be
// the real playfield (no legal native walk can leave it), or on a SEALED
// object-nav deck (an above-terrain object surface whose payload lane has
// no reachable open outline exit - the Jangan walkway incident; verdict in
// objectnav.go). The inverse also holds: a spawn standing on an OPEN
// object lane (bridge decks) is legal even when the tile plane below is
// blocked seabed - the object verdict overrides the tile verdict in both
// directions. The rescue is the nearest mainland tile center - walkable
// AND connected, across region borders, to at least
// minMainlandComponentTiles - with terrain height applied. For a sealed
// deck the origin tile itself is a candidate: the terrain under the deck
// is a legal stand when it is walkable mainland (the deck-trapped
// character drops to the ground plane).
//
// A spawn inside a closed object (a tree trunk or a rock whose outline
// blocks on every side, placementguard.go) is stranded too, and no rescue
// lands inside one. Port-only, not native: an extension of the login rescue
// above for BR-261014-1425-CE1E, where a player relogged into a trunk
// forever. The origin tile is a candidate, so the rescue is the nearest
// tile centre outside the object.
//
// Returns (result, stranded, rescueFound):
//   - on the mainland or no walkability data -> (input, false, false);
//     unknown never counts as stranded (fail-open, like every gate here).
//   - stranded, mainland tile within the radius -> (rescued, true, true).
//   - stranded, no mainland in radius -> (input, true, false); the caller
//     owns the fallback (race start profile).
func (v *WaterValidator) RelocateStrandedSpawn(spawn simulation.Spawn) (simulation.Spawn, bool, bool) {
	surface := v.surfaceForRegion(spawn.RegionID)
	if surface == nil {
		return spawn, false, false
	}
	grid := surface.blockedByOffset[offsetKey(
		simulation.SectorX(spawn.RegionID)-simulation.SectorX(surface.seedRegionID),
		simulation.SectorY(spawn.RegionID)-simulation.SectorY(surface.seedRegionID),
	)]
	if grid == nil || grid.tileSize <= 0 {
		return spawn, false, false
	}
	tilesPerAxis := grid.tilesPerAxis
	tileSize := grid.tileSize
	if spawn.X < 0 || spawn.Z < 0 || spawn.X >= surface.regionSize || spawn.Z >= surface.regionSize {
		// Out-of-frame coordinates never reach here post-normalization;
		// refuse rather than mis-address the world grid.
		return spawn, false, false
	}

	origin := globalTile{
		x: simulation.SectorX(spawn.RegionID)*tilesPerAxis + int(math.Floor(spawn.X/tileSize)),
		z: simulation.SectorY(spawn.RegionID)*tilesPerAxis + int(math.Floor(spawn.Z/tileSize)),
	}
	walkable, ok := v.globalTileWalkable(origin, tilesPerAxis)
	if !ok {
		return spawn, false, false
	}
	baseX := spawn.X + float64(simulation.SectorX(spawn.RegionID)-simulation.SectorX(surface.seedRegionID))*surface.regionSize
	baseZ := spawn.Z + float64(simulation.SectorY(spawn.RegionID)-simulation.SectorY(surface.seedRegionID))*surface.regionSize
	deckVerdict := v.spawnObjectDeckVerdict(surface, baseX, baseZ, spawn.Y)
	if deckVerdict == objectDeckOpen {
		// A LEGAL elevated stand (bridge deck with a reachable open
		// exit): the tile plane below - often blocked seabed under a
		// bridge span - must not strand it.
		return spawn, false, false
	}
	closedAsset, enclosed := v.InsideClosedObject(spawn)
	verdicts := map[uint64]bool{}
	if !enclosed && deckVerdict == objectDeckNone && walkable && v.tileOnMainland(origin, tilesPerAxis, verdicts) {
		return spawn, false, false
	}
	startRadius := 1
	if enclosed {
		log.Infof("movement: enter-world spawn 0x%04X (%.1f, %.1f, %.1f) is inside closed object asset %d; relocating",
			spawn.RegionID, spawn.X, spawn.Y, spawn.Z, closedAsset)
		startRadius = 0
	}
	if deckVerdict == objectDeckSealed {
		log.Infof("movement: enter-world spawn 0x%04X (%.1f, %.1f, %.1f) stands on a sealed object-nav deck; relocating",
			spawn.RegionID, spawn.X, spawn.Y, spawn.Z)
		// Radius 0 = the origin tile: dropping through the deck onto
		// walkable mainland terrain is the minimal legal rescue.
		startRadius = 0
	}
	bestDistSq := math.Inf(1)
	var best globalTile
	found := false
	for radius := startRadius; radius <= spawnRescueMaxRadiusTiles && !found; radius++ {
		for dz := -radius; dz <= radius; dz++ {
			for dx := -radius; dx <= radius; dx++ {
				// Ring only: interior tiles were covered by smaller radii.
				if dx > -radius && dx < radius && dz > -radius && dz < radius {
					continue
				}
				candidate := globalTile{origin.x + dx, origin.z + dz}
				if walkable, ok := v.globalTileWalkable(candidate, tilesPerAxis); !ok || !walkable {
					continue
				}
				if !v.tileOnMainland(candidate, tilesPerAxis, verdicts) {
					continue
				}
				if _, inside := v.InsideClosedObject(tileCenter(candidate, tilesPerAxis, tileSize)); inside {
					continue
				}
				distSq := float64(dx*dx + dz*dz)
				if distSq < bestDistSq {
					bestDistSq = distSq
					best = candidate
					found = true
				}
			}
		}
	}
	if !found {
		return spawn, true, false
	}

	rescued := tileCenter(best, tilesPerAxis, tileSize)
	rescued.Y, rescued.Angle = spawn.Y, spawn.Angle
	if terrainY, ok := v.TerrainHeightAt(rescued.RegionID, rescued.X, rescued.Z); ok {
		rescued.Y = terrainY
	}
	return rescued, true, true
}

/*
================
tileCenter

The region-local x/z of a world-grid tile's centre (Y left 0).
================
*/
func tileCenter(tile globalTile, tilesPerAxis int, tileSize float64) simulation.Spawn {
	return simulation.Spawn{
		RegionID: simulation.RegionIDForSectors(tile.x/tilesPerAxis, tile.z/tilesPerAxis),
		X:        (float64(tile.x%tilesPerAxis) + 0.5) * tileSize,
		Z:        (float64(tile.z%tilesPerAxis) + 0.5) * tileSize,
	}
}
