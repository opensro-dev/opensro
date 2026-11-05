/*
===========================================================================

seam_rescue.go - has this pose any legal walk out?

InsideClosedObject (placementguard.go) sees a player trapped inside one
closed object's cells. It cannot see the seam pocket: a pose wedged
between two objects (the Jangan south gate, where the west gate lion's
footprint meets the gate wall, region 25256 (913, 258)) stands inside
no object's cells, on a walkable mainland tile, yet no walk chord out
is ever accepted - wall-sliding along the seam parks a player in a
pocket that is inside nothing. The rescue at enter-world re-ran the
same per-object verdict and stayed silent (#2359).

poseHasLegalExit asks the walker instead of a second, parallel graph:
a bounded breadth-first search over stands (a rest point and the owner
the clip retained for it), where every hop is one production clip
(ClipMovementPathFrom) to a neighbouring tile centre. The production
walk already encodes every rule that traps the pose - tile grid,
object stands, outline contacts, deck lanes, link edges, heights - so
the search cannot drift from it. Reaching ordinary open ground (a
walkable mainland tile outside every closed object) is a legal exit;
exhausting the frontier under the visit cap without one is not.

Fail-open, the package standard: no surface, no owner, unknown reach
or a frontier too large to be a pocket all answer "has an exit". The
verdict feeds only RelocateStrandedSpawn, under the same port-only
SRO_CLOSED_OBJECT_RESCUE flag, and only adds stranded verdicts the
legacy tests already cleared (monotone widening).

===========================================================================
*/
package movement

import (
	"math"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

// seamProbeProgress is the least distance a probe's rest must gain on
// its stand before the rest counts as a reached stand. Below it the
// walk rested against the start (the refusal the seam is made of), it
// did not commit movement.
const seamProbeProgress = 2.0

// seamStandVisitCap bounds the stands one verdict may visit. Reaching
// it means the reachable ground is far too large to be a pocket, so
// the pose is legal: the fail-open reasoning the rescue's
// minMainlandComponentTiles flood cap already uses.
const seamStandVisitCap = 256

// seamStand is one reached rest point and the owner the clip retained
// for it, the node of the exit search.
type seamStand struct {
	at    simulation.Spawn
	owner simulation.NavOwner
}

// seamStandKey dedupes stands: the stand's world tile plus its owner
// identity (the retained cell for an object stand), in the style of
// objectDeckStandSealed's visitKey.
type seamStandKey struct {
	tile  uint64
	owner simulation.NavOwner
}

/*
================
poseHasLegalExit

Whether the production walk can carry this pose to ordinary open
ground. Only the pose's own surface is consulted.
================
*/
func (v *WaterValidator) poseHasLegalExit(spawn simulation.Spawn) bool {
	spawn = simulation.NormalizeSpawnFrame(spawn)
	surface := v.surfaceForRegion(spawn.RegionID)
	if surface == nil {
		return true
	}
	if !v.seamNearObjects(surface, spawn) {
		return true
	}
	owner, _, ok := v.ResolveNavOwner(spawn, simulation.NavOwner{})
	if !ok {
		return true
	}
	start := seamStand{at: v.ownedStart(spawn, owner), owner: owner}
	mainland := map[uint64]bool{}
	seen := map[seamStandKey]bool{v.seamKeyOf(start): true}
	queue := []seamStand{start}
	for len(queue) > 0 {
		stand := queue[0]
		queue = queue[1:]
		for _, next := range v.seamProbes(stand) {
			if v.seamIsOpenGround(next, mainland) {
				return true
			}
			key := v.seamKeyOf(next)
			if seen[key] {
				continue
			}
			seen[key] = true
			if len(seen) > seamStandVisitCap {
				return true
			}
			queue = append(queue, next)
		}
	}
	return false
}

/*
================
seamNearObjects

The cost prefilter: a seam pocket needs objects, so the search runs
only when the pose lies inside the one-tile-inflated union reach of
some local object set (objectnav_reach.go). Open-field poses - most
logins - pay a few box tests against sets built once and cached.
================
*/
func (v *WaterValidator) seamNearObjects(surface *groundSurface, spawn simulation.Spawn) bool {
	_, tileSize, ok := v.gridParamsForRegion(spawn.RegionID)
	if !ok || tileSize <= 0 {
		return false
	}
	gridPoint := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: spawn.RegionID, X: spawn.X, Z: spawn.Z})
	seedX, seedZ := simulation.SectorX(surface.seedRegionID), simulation.SectorY(surface.seedRegionID)
	for dz := -1; dz <= 1; dz++ {
		for dx := -1; dx <= 1; dx++ {
			set, reach := v.objectNavSetReach(surface, dx, dz)
			if len(set) == 0 || !reach.known {
				continue
			}
			anchor := worldgeom.RegionIDForSectors(seedX+dx, seedZ+dz)
			local := worldgeom.LocalFromGrid(anchor, gridPoint)
			if !reach.missedBy(local.X-tileSize, local.Z-tileSize, local.X+tileSize, local.Z+tileSize) {
				return true
			}
		}
	}
	return false
}

/*
================
seamProbes

The stands the production walk reaches from stand: one clip to each
of the eight neighbouring tile centres, each target at the height the
spawn walk would stand at there (WalkableSpawnHeightAt keeps the
deck/terrain arbitration the production one).
================
*/
func (v *WaterValidator) seamProbes(stand seamStand) []seamStand {
	_, tileSize, ok := v.gridParamsForRegion(stand.at.RegionID)
	if !ok || tileSize <= 0 {
		return nil
	}
	tileX := int(math.Floor(stand.at.X / tileSize))
	tileZ := int(math.Floor(stand.at.Z / tileSize))
	var out []seamStand
	for dz := -1; dz <= 1; dz++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dz == 0 {
				continue
			}
			target := simulation.NormalizeSpawnFrame(simulation.Spawn{
				RegionID: stand.at.RegionID,
				X:        (float64(tileX+dx) + 0.5) * tileSize,
				Z:        (float64(tileZ+dz) + 0.5) * tileSize,
			})
			height, ok := v.WalkableSpawnHeightAt(target.RegionID, target.X, stand.at.Y, target.Z)
			if !ok {
				continue
			}
			target.Y = height
			if next, ok := v.seamRestStand(stand, v.ClipMovementPathFrom(stand.at, stand.owner, target)); ok {
				out = append(out, next)
			}
		}
	}
	return out
}

/*
================
seamRestStand

The stand one clipped chord reached, or false when the chord never
committed movement: its rest gained less than seamProbeProgress on
the probing stand without arriving under a different owner (a
refusal rests where it started). An unresolved rest owner is
re-resolved once; a rest no surface owns is not a stand.
================
*/
func (v *WaterValidator) seamRestStand(stand seamStand, report ClipReport) (seamStand, bool) {
	rest := report.Rest
	gain := simulation.WorldDistance2D(stand.at, rest)
	if gain < seamProbeProgress && (gain <= clipRestPullback || report.RestOwner == stand.owner) {
		return seamStand{}, false
	}
	owner := report.RestOwner
	if !owner.Resolved() {
		resolved, _, ok := v.ResolveNavOwner(rest, simulation.NavOwner{})
		if !ok {
			return seamStand{}, false
		}
		owner = resolved
	}
	return seamStand{at: rest, owner: owner}, true
}

/*
================
seamIsOpenGround

Ordinary open ground: the stand's world tile is walkable and
mainland (the rescue's own verdicts), and the stand point is inside
no closed object.
================
*/
func (v *WaterValidator) seamIsOpenGround(stand seamStand, mainland map[uint64]bool) bool {
	tilesPerAxis, tileSize, ok := v.gridParamsForRegion(stand.at.RegionID)
	if !ok {
		return false
	}
	tile := globalTile{
		x: simulation.SectorX(stand.at.RegionID)*tilesPerAxis + int(math.Floor(stand.at.X/tileSize)),
		z: simulation.SectorY(stand.at.RegionID)*tilesPerAxis + int(math.Floor(stand.at.Z/tileSize)),
	}
	if walkable, covered := v.globalTileWalkable(tile, tilesPerAxis); !covered || !walkable {
		return false
	}
	if !v.tileOnMainland(tile, tilesPerAxis, mainland) {
		return false
	}
	_, enclosed := v.InsideClosedObject(stand.at)
	return !enclosed
}

/*
================
seamKeyOf

The dedupe key of one stand: its world-grid tile plus its owner.
================
*/
func (v *WaterValidator) seamKeyOf(stand seamStand) seamStandKey {
	tilesPerAxis, tileSize, ok := v.gridParamsForRegion(stand.at.RegionID)
	if !ok {
		return seamStandKey{owner: stand.owner}
	}
	tile := globalTile{
		x: simulation.SectorX(stand.at.RegionID)*tilesPerAxis + int(math.Floor(stand.at.X/tileSize)),
		z: simulation.SectorY(stand.at.RegionID)*tilesPerAxis + int(math.Floor(stand.at.Z/tileSize)),
	}
	return seamStandKey{tile: tile.key(), owner: stand.owner}
}
