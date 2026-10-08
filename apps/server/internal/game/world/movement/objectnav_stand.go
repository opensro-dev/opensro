/*
===========================================================================

objectnav_stand.go - selects the nearest native navigation plane at a point

===========================================================================
*/
package movement

import (
	"math"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
	"slices"
)

// spawnObjectDeckStand resolves the object cell a seed-frame spawn point
// stands on, mirroring the native nearest-Y arbitration. It searches the
// point sector and adjacent placement anchors so an overhanging deck is
// owned by its mesh rather than the terrain below.
/*
================
spawnObjectDeckStand
================
*/
func (v *WaterValidator) spawnObjectDeckStand(surface *groundSurface, baseX, baseZ, spawnY, terrainY float64) *objectDeckStand {
	terrainDelta := math.Abs(terrainY - spawnY)
	// Native nearest-Y arbitration replaces terrain only for a strictly
	// smaller distance. Zero is already the best possible result.
	if terrainDelta == 0 {
		return nil
	}
	var best *objectDeckStand
	bestDelta := terrainDelta
	point := worldgeom.NormalizeOutdoor(worldgeom.RegionXZ{RegionID: surface.seedRegionID, X: baseX, Z: baseZ})
	pointSectorX := worldgeom.SectorX(point.RegionID)
	pointSectorZ := worldgeom.SectorY(point.RegionID)
	gridPoint := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: surface.seedRegionID, X: baseX, Z: baseZ})
	// A handful of sets (nine anchors on at most two surfaces): a scan of a
	// stack array, not a map that allocated its buckets every monster step.
	var checkedBuf [18]objectNavSetKey
	checked := checkedBuf[:0]

	probe := func(candidate *groundSurface, anchorX, anchorZ int) {
		if candidate == nil {
			return
		}
		dx := anchorX - simulation.SectorX(candidate.seedRegionID)
		dz := anchorZ - simulation.SectorY(candidate.seedRegionID)
		key := objectNavSetKey{surface: candidate, offset: offsetKey(dx, dz)}
		if slices.Contains(checked, key) {
			return
		}
		checked = append(checked, key)
		set := v.objectNavSetForOffset(candidate, dx, dz)
		anchorRegion := worldgeom.RegionIDForSectors(anchorX, anchorZ)
		anchorLocal := worldgeom.LocalFromGrid(anchorRegion, gridPoint)
		localX, localZ := anchorLocal.X, anchorLocal.Z

		for i := range set {
			placement := set[i].placement
			// Inverse of the forward yaw+translate transform: object-local
			// point = R(-yaw) * (region point - origin); Y is untouched.
			cosYaw, sinYaw := set[i].cosYaw, set[i].sinYaw
			objectLocalX := cosYaw*(localX-placement.x) + sinYaw*(localZ-placement.z)
			objectLocalZ := -sinYaw*(localX-placement.x) + cosYaw*(localZ-placement.z)
			objectLocalY := spawnY - placement.y
			for meshIndex, mesh := range set[i].meshes {
				if objectLocalX < mesh.minX || objectLocalX > mesh.maxX ||
					objectLocalZ < mesh.minZ || objectLocalZ > mesh.maxZ {
					continue
				}
				if objectLocalY-mesh.maxY > bestDelta || mesh.minY-objectLocalY > bestDelta {
					continue
				}
				for cell := 0; cell < mesh.cellCount(); cell++ {
					cellPlaneY, inside := objectCellPlaneYAt(mesh, cell, objectLocalX, objectLocalZ)
					if !inside {
						continue
					}
					planeY := cellPlaneY + placement.y
					delta := math.Abs(planeY - spawnY)
					if delta < bestDelta {
						bestDelta = delta
						best = &objectDeckStand{set: set, objectIndex: i, mesh: mesh, cellIndex: cell, planeY: planeY, placement: placement, anchorX: float64(anchorX) * simulation.NativeRegionSize, anchorZ: float64(anchorZ) * simulation.NativeRegionSize,
							surface: candidate, setDX: dx, setDZ: dz, meshIndex: meshIndex}
					}
				}
			}
		}
	}

	// FindNavCell queries the point's region first. Neighboring NVMs can
	// repeat this same mesh with unresolved link placeholders; letting a
	// rounded copy win loses the current region's authored portal table.
	probe(surface, pointSectorX, pointSectorZ)
	probe(v.surfaceForRegion(point.RegionID), pointSectorX, pointSectorZ)
	if best != nil {
		return best
	}
	for dz := -objectAnchorSearchRadiusSectors; dz <= objectAnchorSearchRadiusSectors; dz++ {
		for dx := -objectAnchorSearchRadiusSectors; dx <= objectAnchorSearchRadiusSectors; dx++ {
			anchorX, anchorZ := pointSectorX+dx, pointSectorZ+dz
			if anchorX < 0 || anchorZ < 0 || anchorX > 0xff || anchorZ > 0xff {
				continue
			}
			probe(surface, anchorX, anchorZ)
			probe(v.surfaceForRegion(simulation.RegionIDForSectors(anchorX, anchorZ)), anchorX, anchorZ)
		}
	}
	return best
}
