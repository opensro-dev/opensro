package movement

import "math"

const (
	waterBlocksPerAxis   = 6
	waterBlocksPerSector = waterBlocksPerAxis * waterBlocksPerAxis
	sectorOffsetStride   = 1 << 16
)

type waterSector struct {
	hasWater [waterBlocksPerSector]bool
	heights  [waterBlocksPerSector]float64
}

// heightGrid is one region's navmesh height map: axisVertices^2 vertices at
// vertexStep spacing (the exported JMXVNVM grid, 97x97 every 20 units).
// Every grid stays in memory (about 79 MB for the shipped world): population
// spawns sample heights at nests across the whole world in one tick, so a
// paged cache missed on nearly every lookup and stalled the tick on disk
// reads.
type heightGrid struct {
	axisVertices int
	vertexStep   float64
	heights      []float32
	planeTypes   []byte
	planeHeights []float32
}

// blockedGrid is one region's navmesh walkability plane, mirroring the
// client's cellForLocal gate: a tile is walkable when its blocked byte is
// zero AND its cell id indexes an existing cell.
type blockedGrid struct {
	tilesPerAxis int
	tileSize     float64
	walkable     []uint64
}

func (g *blockedGrid) tileWalkable(index int) bool {
	return g.walkable[index/64]&(uint64(1)<<uint(index%64)) != 0
}

type groundSurface struct {
	seedRegionID uint16
	regionSize   float64
	// waterByOffset keys dx*sectorOffsetStride+dz relative to the bundle source sector
	// (missionRegionOffsetKey).
	waterByOffset map[int64]*waterSector
	// heightsByOffset keys the same sector offsets to navmesh height grids.
	heightsByOffset map[int64]*heightGrid
	// blockedByOffset keys the same sector offsets to walkability grids.
	blockedByOffset map[int64]*blockedGrid
	// objectPlacementsByOffset keys the same sector offsets to the nav
	// object placements of that region entry (sealed-deck rescue,
	// objectnav.go). Placement coordinates are in the ENTRY's local frame
	// and may extend beyond it for neighbor-anchored objects.
	objectPlacementsByOffset map[int64][]objectNavPlacement
	// objectResourceIndexPublicPath names the assetId -> mesh resolution
	// table for those placements.
	objectResourceIndexPublicPath string
}

// terrainHeightAt bilinearly samples the navmesh height grid at a
// seed-frame point. Bilinear differs from the client's triangle-split
// sampler by at most the quad's diagonal curvature (sub-unit on the 20u
// grid); callers comparing against it must allow a small tolerance.
func (s *groundSurface) terrainHeightAt(baseX, baseZ float64) (float64, bool) {
	dxRegion := math.Floor(baseX / s.regionSize)
	dzRegion := math.Floor(baseZ / s.regionSize)
	grid := s.heightsByOffset[offsetKey(int(dxRegion), int(dzRegion))]
	if grid == nil {
		return 0, false
	}
	localX := baseX - dxRegion*s.regionSize
	localZ := baseZ - dzRegion*s.regionSize
	fx := localX / grid.vertexStep
	fz := localZ / grid.vertexStep
	maxCell := float64(grid.axisVertices - 2)
	ix := int(math.Min(math.Max(math.Floor(fx), 0), maxCell))
	iz := int(math.Min(math.Max(math.Floor(fz), 0), maxCell))
	tx := math.Min(math.Max(fx-float64(ix), 0), 1)
	tz := math.Min(math.Max(fz-float64(iz), 0), 1)
	h00, h10, h01, h11 := grid.quad(ix, iz)
	height := h00*(1-tx)*(1-tz) + h10*tx*(1-tz) + h01*(1-tx)*tz + h11*tx*tz
	// Native (client 404EE9, and the server's solid-plane height probe):
	// bit 2 makes this NVM plane solid. Raise the sampled ground to it,
	// never lower hills above it.
	block := int(localZ/(s.regionSize/6))*6 + int(localX/(s.regionSize/6))
	if block >= 0 && block < len(grid.planeTypes) && grid.planeTypes[block]&2 != 0 {
		height = math.Max(height, float64(grid.planeHeights[block]))
	}
	return height, true
}

// waterSurfaceHeightAt ports missionWaterSurfaceHeightAt: resolve the
// sector offset, then the 320-unit block (regionSize/6) within it.
func (s *groundSurface) waterSurfaceHeightAt(baseX, baseZ float64) (float64, bool) {
	dxRegion := math.Floor(baseX / s.regionSize)
	dzRegion := math.Floor(baseZ / s.regionSize)
	water := s.waterByOffset[offsetKey(int(dxRegion), int(dzRegion))]
	if water == nil {
		return 0, false
	}
	localX := baseX - dxRegion*s.regionSize
	localZ := baseZ - dzRegion*s.regionSize
	blockX := int(math.Floor(localX / (s.regionSize / waterBlocksPerAxis)))
	blockZ := int(math.Floor(localZ / (s.regionSize / waterBlocksPerAxis)))
	if blockX < 0 || blockZ < 0 ||
		blockX >= waterBlocksPerAxis || blockZ >= waterBlocksPerAxis {
		return 0, false
	}
	index := blockZ*waterBlocksPerAxis + blockX
	if !water.hasWater[index] {
		return 0, false
	}
	return water.heights[index], true
}

func offsetKey(dx, dz int) int64 {
	return int64(dx)*sectorOffsetStride + int64(dz)
}

/*
================
heightGrid.quad

The four heights of the grid cell whose low corner is vertex (x, z).
================
*/
func (g *heightGrid) quad(x, z int) (float64, float64, float64, float64) {
	i := z*g.axisVertices + x
	return float64(g.heights[i]), float64(g.heights[i+1]), float64(g.heights[i+g.axisVertices]), float64(g.heights[i+g.axisVertices+1])
}
