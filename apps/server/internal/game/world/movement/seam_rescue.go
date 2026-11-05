/*
===========================================================================

seam_rescue.go - has a pose a legal walk out of where it stands?

InsideClosedObject (placementguard.go) sees a pose strictly inside one
closed object's cells. A pose wedged in the seam BETWEEN objects - the
Jangan south-gate lion's footprint corner against the gate wall, region
25256 (913, 258) - is inside no object's cells, stands on a walkable
mainland tile, and still has no legal walk out: every chord the native
walk clips from the pose dies on one of the two objects' blocking
outlines. RelocateStrandedSpawn's tile/deck/enclosure verdicts all call
such a pose legal, so the enter-world rescue never fires (trace:
post-cut/stuck-lion-trace.md).

poseHasLegalExit answers the walker's own question instead of a
geometric one: starting from the pose's retained owner, can the
production clip (ClipMovementPathFrom) commit movement that reaches
ordinary open ground? The search is a bounded breadth-first walk over
committed rest stands, so tiles, decks, links, outlines and heights
are inherited from the clip, never re-derived. Port-only, not native:
it runs only behind SRO_CLOSED_OBJECT_RESCUE, like the enclosure test
it widens.

===========================================================================
*/
package movement

import (
	"math"

	"opensro.online/server/internal/game/world/simulation"
)

// seamMaxStands bounds the exit search: pockets are a handful of
// stands, and a search that visits this many stands without exhausting
// is standing on real playfield, not in a seam (the same reasoning as
// minMainlandComponentTiles and the 65536 seen-cap in
// objectDeckStandSealed). Reaching the cap therefore means "has an
// exit"; only exhaustion below it means stranded.
const seamMaxStands = 256

// seamProgressEpsilon is how far a probe's committed rest must land
// from the probing stand to count as movement: below it the clip
// rested against its own start (contact noise, not a walk).
const seamProgressEpsilon = 2.0

// seamNearMargin is how far beyond a placement's reach box (anchor
// frame units) a pose still counts as near object geometry. It must
// exceed the probe fan's reach (a diagonal tile-centre probe is
// tileSize*sqrt(2), ~28 units) so no placement that could clip a
// probe chord is ever skipped by the prefilter.
const seamNearMargin = 40.0

// seamStand is one committed rest of the exit search: where the walker
// stands, and the owner it retained getting there.
type seamStand struct {
	pose  simulation.Spawn
	owner simulation.NavOwner
}

// seamStandKey dedupes stands the BFS has already expanded, in the
// visitKey style of objectDeckStandSealed.
type seamStandKey struct {
	region uint16
	tileX  int
	tileZ  int
	owner  simulation.NavOwner
}

/*
================
poseHasLegalExit

Whether the native walk, started at spawn's retained owner, can commit
movement onto ordinary open ground: a stand on a walkable mainland
tile whose point is not inside a closed object. Every hop is
arbitrated by ClipMovementPathFrom; the pose itself is never judged
an exit (its tile being walkable mainland is exactly the seam trap).
Fails open (true) on any missing data, and true again when the search
outgrows seamMaxStands - both mean "not proven stranded".
================
*/
func (v *WaterValidator) poseHasLegalExit(spawn simulation.Spawn, tilesPerAxis int, tileSize float64, verdicts map[uint64]bool) bool {
	if !v.seamNearObjectGeometry(spawn) {
		// Open field: no placement can clip a chord from here. This is
		// what keeps ordinary logins off the search entirely.
		return true
	}
	owner, _, ok := v.ResolveNavOwner(spawn, simulation.NavOwner{})
	if !ok || !owner.Resolved() {
		return true
	}
	start := seamStand{pose: v.ownedStart(spawn, owner), owner: owner}
	seen := map[seamStandKey]bool{seamKeyOf(start, tilesPerAxis, tileSize): true}
	queue := []seamStand{start}
	for head := 0; head < len(queue); head++ {
		cur := queue[head]
		for _, target := range v.seamProbeTargets(cur.pose, tilesPerAxis, tileSize) {
			report := v.ClipMovementPathFrom(cur.pose, cur.owner, target)
			if simulation.WorldDistance2D(cur.pose, report.Rest) < seamProgressEpsilon {
				continue
			}
			rest := report.Rest
			restOwner := report.RestOwner
			if !restOwner.Resolved() {
				resolved, _, resolvedOK := v.ResolveNavOwner(rest, simulation.NavOwner{})
				if !resolvedOK || !resolved.Resolved() {
					continue
				}
				restOwner = resolved
			}
			next := seamStand{pose: rest, owner: restOwner}
			key := seamKeyOf(next, tilesPerAxis, tileSize)
			if seen[key] {
				continue
			}
			seen[key] = true
			if len(seen) > seamMaxStands {
				return true
			}
			if v.seamExitStand(rest, tilesPerAxis, tileSize, verdicts) {
				return true
			}
			queue = append(queue, next)
		}
	}
	return false
}

/*
================
seamExitStand

Whether one reached stand is ordinary open ground: its tile is
walkable and mainland (the rescue's own tile verdicts, sharing the
caller's cache), and its point is not inside a closed object.
================
*/
func (v *WaterValidator) seamExitStand(stand simulation.Spawn, tilesPerAxis int, tileSize float64, verdicts map[uint64]bool) bool {
	tile := seamTileOf(stand, tilesPerAxis, tileSize)
	walkable, ok := v.globalTileWalkable(tile, tilesPerAxis)
	if !ok || !walkable {
		return false
	}
	if !v.tileOnMainland(tile, tilesPerAxis, verdicts) {
		return false
	}
	_, enclosed := v.InsideClosedObject(stand)
	return !enclosed
}

/*
================
seamNearObjectGeometry

Whether any placement resolved around the pose could clip one of its
probe chords: the pose, in each surrounding set's anchor frame, within
a placement's reach box widened by seamNearMargin. A placement whose
reach is unknown is never skipped (objectReach's own rule).
================
*/
func (v *WaterValidator) seamNearObjectGeometry(spawn simulation.Spawn) bool {
	surface := v.surfaceForRegion(spawn.RegionID)
	if surface == nil {
		return true
	}
	regionSize := surface.regionSize
	for dz := -1; dz <= 1; dz++ {
		for dx := -1; dx <= 1; dx++ {
			set := v.objectNavSetForOffset(surface, dx, dz)
			anchorX := simulation.SectorX(surface.seedRegionID) + dx
			anchorZ := simulation.SectorY(surface.seedRegionID) + dz
			localX := spawn.X + float64(simulation.SectorX(spawn.RegionID)-anchorX)*regionSize
			localZ := spawn.Z + float64(simulation.SectorY(spawn.RegionID)-anchorZ)*regionSize
			for i := range set {
				reach := set[i].reach
				if !reach.known {
					return true
				}
				if localX >= reach.minX-seamNearMargin && localX <= reach.maxX+seamNearMargin &&
					localZ >= reach.minZ-seamNearMargin && localZ <= reach.maxZ+seamNearMargin {
					return true
				}
			}
		}
	}
	return false
}

/*
================
seamProbeTargets

The eight neighbouring tile centres around a stand's tile, at the
height a real move goal there would carry (WalkableSpawnHeightAt, the
same surface arbitration entry spawns use). Targets whose height does
not resolve are not probed.
================
*/
func (v *WaterValidator) seamProbeTargets(stand simulation.Spawn, tilesPerAxis int, tileSize float64) []simulation.Spawn {
	origin := seamTileOf(stand, tilesPerAxis, tileSize)
	targets := make([]simulation.Spawn, 0, 8)
	for dz := -1; dz <= 1; dz++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dz == 0 {
				continue
			}
			candidate := globalTile{x: origin.x + dx, z: origin.z + dz}
			if candidate.x < 0 || candidate.z < 0 ||
				candidate.x/tilesPerAxis > maxWorldSector || candidate.z/tilesPerAxis > maxWorldSector {
				continue
			}
			target := tileCenter(candidate, tilesPerAxis, tileSize)
			height, ok := v.WalkableSpawnHeightAt(target.RegionID, target.X, stand.Y, target.Z)
			if !ok {
				continue
			}
			target.Y = height
			target.Angle = stand.Angle
			targets = append(targets, target)
		}
	}
	return targets
}

/*
================
seamTileOf

The world-grid tile a stand stands on, addressed exactly as
RelocateStrandedSpawn addresses its origin tile.
================
*/
func seamTileOf(stand simulation.Spawn, tilesPerAxis int, tileSize float64) globalTile {
	return globalTile{
		x: simulation.SectorX(stand.RegionID)*tilesPerAxis + int(math.Floor(stand.X/tileSize)),
		z: simulation.SectorY(stand.RegionID)*tilesPerAxis + int(math.Floor(stand.Z/tileSize)),
	}
}

/*
================
seamKeyOf
================
*/
func seamKeyOf(stand seamStand, tilesPerAxis int, tileSize float64) seamStandKey {
	tile := seamTileOf(stand.pose, tilesPerAxis, tileSize)
	return seamStandKey{region: stand.pose.RegionID, tileX: tile.x, tileZ: tile.z, owner: stand.owner}
}

/*
================
rescueCandidateLegal

Whether a rescue candidate tile centre may receive the player: not
inside a closed object at the height the rescue would stand at, and
with a legal walk out of its own stand. The origin tile of a seam
trap fails here even though its centre is outside every object, so
the rescue search passes over the pocket instead of landing in it.
================
*/
func (v *WaterValidator) rescueCandidateLegal(center simulation.Spawn, spawnY float64, tilesPerAxis int, tileSize float64, verdicts map[uint64]bool) bool {
	stand := v.rescueStand(center, spawnY)
	if _, enclosed := v.InsideClosedObject(stand); enclosed {
		return false
	}
	return v.poseHasLegalExit(stand, tilesPerAxis, tileSize, verdicts)
}
