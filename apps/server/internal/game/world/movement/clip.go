/*
===========================================================================

clip.go - native collision queries and accepted rest poses

===========================================================================
*/
package movement

// Authoritative full-chord movement clipping over published terrain and BMS
// navigation. Terrain/object arbitration selects the earliest blocking contact;
// admitted deck height can override a blocked terrain tile beneath it.
//
// Known object-cell contacts retain the original client 43b980 intersection stores and
// client 45c1b0 cell-directed inset. They can move off the original chord. Unknown
// outside cell ownership, circular contacts and terrain retain conservative
// pullbacks. Explicit resolved links authorize passage; missing links do not.
//
// This does not reproduce the original client's frame-quantized movement
// integrator or the complete region-manager reflection/event protocol. Exact
// endpoint assertions cover the bounded native contact corpus; broader native
// movement equivalence remains a separate acceptance requirement.
// Production applies the clip; observe mode reports it without changing the goal.

import (
	"math"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
SpawnMoveTest

SpawnMoveTest is the move test the server's population creation runs for
a candidate generated around a nest centre: the result bits it acts on and
the walked resting point, whose height names the surface the walk reached.
Both outdoor and dungeon blocking contacts retain their result bits.
================
*/
func (v *WaterValidator) SpawnMoveTest(from, to simulation.Spawn) simulation.MonsterSpawnMove {
	report := v.ClipMovementPath(simulation.NormalizeSpawnFrame(from), simulation.NormalizeSpawnFrame(to))
	if report.Outcome != ClipBlocked {
		return simulation.MonsterSpawnMove{Rest: report.Rest}
	}
	return simulation.MonsterSpawnMove{Result: report.NativeResult, Rest: report.Rest}
}

// ClipOutcome classifies one clipped chord.
type ClipOutcome string

const (
	// ClipArrived: no blocking contact on any covered tile; the goal
	// stands as requested.
	ClipArrived ClipOutcome = "arrived"
	// ClipBlocked: the chord enters a blocked tile; Rest is the clip
	// point short of it.
	ClipBlocked ClipOutcome = "blocked"
	// ClipNoCoverage: no walkability data covered the chord. Fail-open,
	// package standard: unknown never clips.
	ClipNoCoverage ClipOutcome = "noCoverage"
	// ClipDungeonExempt: either end on the dungeon plane (bit 15);
	// outdoor tile math must not be applied. Never clips.
	ClipDungeonExempt ClipOutcome = "dungeonExempt"
	// ClipStartBlocked: the DEPARTURE tile is blocked. Never clips - the
	// player must always be able to leave (same doctrine as the pathguard
	// startBlocked verdict).
	ClipStartBlocked ClipOutcome = "startBlocked"
)

// clipRestPullback is how far short of the blocking contact the rest
// point lands: along each crossed axis for a terrain tile boundary, along
// the chord for an object edge. It exists so the rest is unambiguously on
// the near side of the contact (a rest exactly on a tile boundary would
// floor into the blocked tile when moving in +x/+z). 0.01u is the
// client's own portal-entry clamp scale (CRTNavMeshTerrain_Move clamps hop locals to
// (0.01, 1919.99)) - far below the frame-quantization residual Q3 measures.
const clipRestPullback = 0.01

// ClipClass names the obstacle class of a ClipBlocked contact - the two
// classes carry DIFFERENT client rest semantics (deterministic edge point
// vs frame-quantized bias-short; see the file banner) and must never mix
// in the telemetry.
type ClipClass string

const (
	ClipClassTerrain ClipClass = "terrain"
	ClipClassObject  ClipClass = "object"
)

/*
================
ClipReport

ClipReport is one clipped chord.
================
*/
type ClipReport struct {
	continuation bool
	Outcome      ClipOutcome
	// Class is the obstacle class of a ClipBlocked contact ("" otherwise).
	Class ClipClass
	// Rest is the goal the clip yields, canonical frame: the requested
	// goal for every outcome except ClipBlocked, where it is the chord
	// point pulled back just short of the first blocking contact.
	Rest simulation.Spawn
	// BlockedTileX/Z is the first blocking WORLD-grid tile (valid only
	// for ClipBlocked with Class terrain; an object contact is an edge,
	// not a tile, and leaves these zero).
	BlockedTileX, BlockedTileZ int
	// TilesChecked / TilesUncovered mirror PathReport semantics.
	TilesChecked   int
	TilesUncovered int
	// ObjectDeckOverrides counts blocked-tile probes overridden because
	// the chord point stands on an object-nav deck (same semantics as
	// PathReport).
	ObjectDeckOverrides int
	// Truncated: the walk hit pathGuardMaxTiles before the endpoint
	// without finding a block. Fail-open: never clips (a clip from an
	// unfinished scan would stop a player on evidence it never saw),
	// on either plane.
	Truncated bool
	// NativeResult is the JMX move-test result bit a ClipBlocked
	// contact corresponds to: monster.NavResultClipped for a blocked terrain
	// edge (404510 returns 1) or a side-blocked internal object edge (428930),
	// monster.NavResultBlocked for a failing object outline leg (client 403FB0) or
	// the six-call continuation limit in the server's movement query.
	// Dungeon mesh and circle contacts propagate the same bits through the
	// server's dungeon move. Zero for clear moves.
	NativeResult uint32
	// RestOwner is the surface the walk reached at Rest (navowner.go), and
	// Rest.Y is that surface's height there - never the request's int16 Y.
	// Unresolved on planes without ownership data (dungeon, no coverage).
	RestOwner simulation.NavOwner
	// Work is the dungeon clip's deterministic work (dungeon_clip.go):
	// cells tested, obstacle circles tested and floor samples read.
	// Zero outdoors, where TilesChecked counts the walk.
	Work int
}

/*
================
ClipMovementPath

ClipMovementPath walks the straight chord from -> to over the same world
tile grid ValidateMovementPath addresses (identical tile convention,
identical corner supercover rule) and stops at the FIRST blocked tile,
including the endpoint tile. Both spawns must be canonical
(NormalizeSpawnFrame / LiveSpawnAt frames).

Failure policy is the package standard: missing coverage degrades per
tile to "not blocked" and whole-chord to ClipNoCoverage - never a clip.
412230 continuation: each segment retains the original destination.

ClipMovementPath has no retained source owner, so it resolves one with the
native teleport rule (FindNavCell). Movers that retain a position owner -
players - must call ClipMovementPathFrom with it instead.
================
*/
func (v *WaterValidator) ClipMovementPath(from, to simulation.Spawn) ClipReport {
	return v.ClipMovementPathFrom(from, simulation.NavOwner{}, to)
}

/*
================
ClipMovementPathFrom

ClipMovementPathFrom walks from the retained source owner (the native
source cell the server's movement query starts from). Every continuation leg inherits
the owner the previous leg reached; ownership is never re-guessed from Y.
================
*/
func (v *WaterValidator) ClipMovementPathFrom(from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) ClipReport {
	report, _ := v.clipMovementPathFromWithWalk(from, fromOwner, to)
	return report
}

/*
================
clipMovementPathFromWithWalk

Returns the clipped report and, when the path resolved in a single unclipped
leg, the navWalk already computed for that chord so PlanMonsterPathFrom and
route probes do not walk the same chord a second time.
================
*/
func (v *WaterValidator) clipMovementPathFromWithWalk(from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) (ClipReport, *navWalk) {
	start := from
	startOwner := fromOwner
	checked, uncovered, overrides, work := 0, 0, 0, 0
	for calls := 1; calls <= 6; calls++ {
		report, walk := v.clipMovementSegmentWithWalk(start, startOwner, to)
		checked += report.TilesChecked
		uncovered += report.TilesUncovered
		overrides += report.ObjectDeckOverrides
		work += report.Work
		report.TilesChecked = checked
		report.TilesUncovered = uncovered
		report.ObjectDeckOverrides = overrides
		report.Work = work
		if !report.continuation {
			if calls == 1 {
				return report, walk
			}
			return report, nil
		}
		report.continuation = false
		f := contactF32
		dx := f(to.X + float64(simulation.SectorX(to.RegionID)-simulation.SectorX(start.RegionID))*1920 - start.X)
		dz := f(to.Z + float64(simulation.SectorY(to.RegionID)-simulation.SectorY(start.RegionID))*1920 - start.Z)
		dy := f(to.Y - start.Y)
		if f(dy*dy+dx*dx+dz*dz) < 25 {
			return continuedReport(report), nil
		}
		start = report.Rest
		startOwner = report.RestOwner
	}
	return ClipReport{Outcome: ClipBlocked, Class: ClipClassObject, Rest: from, RestOwner: fromOwner, TilesChecked: checked, TilesUncovered: uncovered, ObjectDeckOverrides: overrides, NativeResult: monster.NavResultBlocked}, nil
}

/*
================
ClipMovementSegmentFrom

ClipMovementSegmentFrom is one leg of ClipMovementPathFrom: a single
chord whose report says where the walk reached and whether it continues
(a linked transfer or an outline exit the next leg resumes from).
The server's line-of-sight test drives exactly this seam
leg by leg; callers that want the whole walk use ClipMovementPathFrom.
================
*/
func (v *WaterValidator) ClipMovementSegmentFrom(from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) ClipReport {
	return v.clipMovementSegment(from, fromOwner, to)
}

/*
================
continuedReport

The server's movement query ends a continuation run once the leg's start is
within 5 units of the destination: it breaks with the leg's own result,
which carries a continuation bit (0x14) and no failure bit (0x10000001 broke
earlier), and the position the leg reached. That is a completed step, not a
block. An object outline exit to terrain (client 403FB0, 4044CE..4044FE) is such a
leg, so a short step off an open deck edge stands at the exit point on the
terrain below.
================
*/
func continuedReport(report ClipReport) ClipReport {
	report.Outcome = ClipArrived
	report.Class = ""
	report.BlockedTileX, report.BlockedTileZ = 0, 0
	report.NativeResult = report.NativeResult&nativeLinkedResult | nativeContinueResult
	return report
}

/*
================
settleRest

settleRest puts the rest on the surface the walk reached at chord fraction
t: the walk owner there, retained through ResolveNavOwner so Rest.Y becomes
that surface's height (object cell plane or terrain heightfield).
================
*/
func (v *WaterValidator) settleRest(report *ClipReport, walk *navWalk, t float64) {
	if walk == nil {
		return
	}
	owner, y, ok := v.ResolveNavOwner(report.Rest, walk.ownerAt(t))
	if !ok {
		return
	}
	report.RestOwner = owner
	report.Rest.Y = y
}

/*
================
clipTileQuerierEntry
================
*/
type clipTileQuerierEntry struct {
	key  int64
	grid *blockedGrid
}

/*
================
clipTileQuerier
================
*/
type clipTileQuerier struct {
	v            *WaterValidator
	tilesPerAxis int
	count        int
	entries      [8]clipTileQuerierEntry
}

/*
================
walkable
================
*/
func (q *clipTileQuerier) walkable(tile globalTile) (walkable, ok bool) {
	if tile.x < 0 || tile.z < 0 {
		return false, false
	}
	sectorX := tile.x / q.tilesPerAxis
	sectorY := tile.z / q.tilesPerAxis
	if sectorX > 0xff || sectorY > 0xff {
		return false, false
	}
	key := offsetKey(sectorX, sectorY)
	var grid *blockedGrid
	found := false
	for i := 0; i < q.count; i++ {
		if q.entries[i].key == key {
			grid = q.entries[i].grid
			found = true
			break
		}
	}
	if !found {
		grid = q.v.gridForSectors(sectorX, sectorY, q.tilesPerAxis)
		if q.count < len(q.entries) {
			q.entries[q.count] = clipTileQuerierEntry{key: key, grid: grid}
			q.count++
		}
	}
	if grid == nil {
		return false, false
	}
	return grid.tileWalkable((tile.z%q.tilesPerAxis)*q.tilesPerAxis + tile.x%q.tilesPerAxis), true
}

/*
================
clipMovementSegment
================
*/
func (v *WaterValidator) clipMovementSegment(from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) ClipReport {
	report, _ := v.clipMovementSegmentWithWalk(from, fromOwner, to)
	return report
}

/*
================
clipMovementSegmentWithWalk

One segment: the start is owned first, then the plane the endpoints
are on clips it (clipSegmentOnPlane, plane.go).
================
*/
func (v *WaterValidator) clipMovementSegmentWithWalk(from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) (ClipReport, *navWalk) {
	from = v.ownedStart(from, fromOwner)
	return clipSegmentOnPlane(v, from, fromOwner, to)
}

/*
================
clipOutdoorSegment

The outdoor plane's segment clip (outdoorPlane.clipSegment): the world
tile grid walk, then the owned object surfaces and their contacts. The
start is already owned.
================
*/
func (v *WaterValidator) clipOutdoorSegment(from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) (ClipReport, *navWalk) {
	report := ClipReport{Outcome: ClipArrived, Rest: to}

	tilesPerAxis, tileSize, ok := v.gridParamsForRegion(from.RegionID)
	if !ok {
		if tilesPerAxis, tileSize, ok = v.gridParamsForRegion(to.RegionID); !ok {
			report.Outcome = ClipNoCoverage
			return report, nil
		}
	}

	fromGrid := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: from.RegionID, X: from.X, Z: from.Z})
	toGrid := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: to.RegionID, X: to.X, Z: to.Z})
	fromWX, fromWZ := fromGrid.X, fromGrid.Z
	toWX, toWZ := toGrid.X, toGrid.Z

	startTile := globalTile{x: int(math.Floor(fromWX / tileSize)), z: int(math.Floor(fromWZ / tileSize))}
	endTile := globalTile{x: int(math.Floor(toWX / tileSize)), z: int(math.Floor(toWZ / tileSize))}

	// Surface ownership along the chord, walked from the retained source
	// owner (navowner.go). A blocked terrain tile is not a block where an
	// object cell owns the walker: it stands on the deck, not the ground.
	walk := v.ownerWalk(from, fromOwner, to)
	transfer := walk.linkedTransfer(to)
	v.settleRest(&report, walk, 1)
	covered := 0
	querier := clipTileQuerier{v: v, tilesPerAxis: tilesPerAxis}
	blockedAt := func(tile globalTile, t float64) bool {
		walkable, known := querier.walkable(tile)
		report.TilesChecked++
		if !known {
			report.TilesUncovered++
			return false
		}
		covered++
		if transfer != nil && t >= transfer.at {
			return false
		}
		if walkable {
			return false
		}
		if _, _, owned := walk.objectAt(t); owned || walk.bridged(t) {
			report.ObjectDeckOverrides++
			return false
		}
		return true
	}

	// A blocked departure tile never clips: the player must always be able
	// to leave (spawn-rescue debris, ungated history).
	if blockedAt(startTile, 0) {
		report.Outcome = ClipStartBlocked
		if covered == 0 {
			report.Outcome = ClipNoCoverage
		}
		return report, nil
	}

	// The object plane's first contact runs on the FULL chord (a same-tile
	// move can still cross a deck rail). Composition below is
	// nearest-wins: the native walk stops at its first clip on either
	// plane and never continues past it (client CRTNavMeshObj_WalkOwnedCells bit0 semantics).
	objectT, objectKey, objectFound, objectPoint := v.objectChordFirstContact(fromWX, fromWZ, from.Y, toWX, toWZ, to.Y, walk)
	if transfer != nil && (!objectFound || objectT >= transfer.at-1e-9) {
		// The next QueryMovement leg starts at the linked cell's repaired
		// point. Contacts beyond this exit belong to that next leg.
		objectFound = false
		report.Rest, report.RestOwner = transfer.rest, transfer.owner
		report.NativeResult, report.continuation = nativeLinkedResult|nativeContinueResult, true
	}

	if startTile == endTile {
		if covered == 0 {
			report.Outcome = ClipNoCoverage
			return report, nil
		}
		if objectFound {
			return v.clipObjectRest(report, walk, from, to, fromWX, fromWZ, toWX, toWZ, objectT, objectPoint), nil
		}
		if report.continuation {
			return report, nil
		}
		return report, walk
	}

	contact, blocked, truncated := v.clipChord(fromWX, fromWZ, toWX, toWZ, tileSize, startTile, endTile, blockedAt)
	if covered == 0 {
		report.Outcome = ClipNoCoverage
		return report, nil
	}
	if truncated {
		// Fail-open on BOTH planes: the terrain scan is incomplete, so an
		// object contact past the scanned prefix cannot claim to be the
		// first block.
		report.Truncated = true
		return report, nil
	}
	// Native 404510 steps a cell's placed objects before it leaves the cell:
	// an object visited at or before the blocking terrain crossing wins.
	if objectFound && (!blocked || objectKey <= contact.t) {
		return v.clipObjectRest(report, walk, from, to, fromWX, fromWZ, toWX, toWZ, objectT, objectPoint), nil
	}
	if !blocked {
		if report.continuation {
			return report, nil
		}
		return report, walk
	}

	report.Outcome = ClipBlocked
	report.Class = ClipClassTerrain
	report.continuation = false
	report.NativeResult = monster.NavResultClipped
	report.BlockedTileX, report.BlockedTileZ = contact.tile.x, contact.tile.z

	// Rest = the chord point at the contact parameter, pulled back just
	// short of the crossed boundary on each crossed axis so the rest tile
	// is the last walkable tile.
	restWX := fromWX + (toWX-fromWX)*contact.t
	restWZ := fromWZ + (toWZ-fromWZ)*contact.t
	if contact.steppedX {
		if toWX > fromWX {
			restWX = float64(contact.tile.x)*tileSize - clipRestPullback
		} else {
			restWX = float64(contact.tile.x+1)*tileSize + clipRestPullback
		}
	}
	if contact.steppedZ {
		if toWZ > fromWZ {
			restWZ = float64(contact.tile.z)*tileSize - clipRestPullback
		} else {
			restWZ = float64(contact.tile.z+1)*tileSize + clipRestPullback
		}
	}

	restY := from.Y + (to.Y-from.Y)*contact.t
	restLocal := worldgeom.LocalFromGrid(from.RegionID, worldgeom.GridXZ{X: restWX, Z: restWZ})
	report.Rest = simulation.NormalizeSpawnFrame(simulation.Spawn{
		RegionID: from.RegionID,
		X:        restLocal.X,
		Y:        restY,
		Z:        restLocal.Z,
		Angle:    to.Angle,
	})
	v.settleRest(&report, walk, contact.t)
	return report, nil
}

/*
================
clipObjectRest

clipObjectRest carries the resolved cell-directed point into the authoritative
destination. Only contacts without a resolved native cell use chord pullback.
================
*/
func (v *WaterValidator) clipObjectRest(report ClipReport, walk *navWalk, from, to simulation.Spawn, fromWX, fromWZ, toWX, toWZ, contactT float64, point objectContactPoint) ClipReport {
	report.Outcome = ClipBlocked
	report.Class = ClipClassObject
	report.NativeResult = monster.NavResultClipped
	if point.outline && !point.outside {
		report.NativeResult = monster.NavResultBlocked
	}
	report.continuation = point.continuation

	restT := contactT
	if chordLen := math.Hypot(toWX-fromWX, toWZ-fromWZ); chordLen > 0 {
		restT = math.Max(contactT-clipRestPullback/chordLen, 0)
	}
	restWX := fromWX + (toWX-fromWX)*restT
	restWZ := fromWZ + (toWZ-fromWZ)*restT
	restY := from.Y + (to.Y-from.Y)*restT
	if point.valid {
		restWX, restWZ, restY = point.x, point.z, point.y
	}
	restLocal := worldgeom.LocalFromGrid(from.RegionID, worldgeom.GridXZ{X: restWX, Z: restWZ})
	report.Rest = simulation.NormalizeSpawnFrame(simulation.Spawn{
		RegionID: from.RegionID,
		X:        restLocal.X,
		Y:        restY,
		Z:        restLocal.Z,
		Angle:    to.Angle,
	})
	if !point.valid {
		v.settleRest(&report, walk, restT)
		return report
	}
	if point.outside || point.continuation && point.outline {
		tile, blocked := v.exitTileBlocked(from.RegionID, point.x, point.z)
		if !blocked {
			// Native outline reflection returns no object cell (428930 bit
			// 0x10); the terrain continuation must not repair itself back
			// INTO the cell it just exited. That loops forever at open
			// outlines.
			if owner, y, ok := v.ResolveNavOwner(report.Rest, simulation.TerrainOwner()); ok {
				report.RestOwner, report.Rest.Y = owner, y
			}
			return report
		}
		// client 403FB0 tests the terrain tile under the exit point before it hands
		// the walker to the terrain (client 4038D0, 4044D9 test al,1) and fails the
		// leg on a blocked one (404194: 0x10000000). Nothing continues; the
		// walker stays on the deck at the rim (the object owner below).
		report.continuation = false
		report.NativeResult = monster.NavResultBlocked
		report.BlockedTileX, report.BlockedTileZ = tile.x, tile.z
	}
	// A cell-directed contact point already lies on an object plane (its Y is
	// a surface height, not the request's). Keep the walk's object owner when
	// it has one there; otherwise this is the object the walker reached, and
	// the exact plane height lets the teleport rule identify it.
	hint := walk.ownerAt(restT)
	if hint.Kind != simulation.NavOwnerObject {
		hint = simulation.NavOwner{}
	}
	if owner, y, ok := v.ResolveNavOwner(report.Rest, hint); ok {
		report.RestOwner, report.Rest.Y = owner, y
	}
	return report
}

/*
================
exitTileBlocked

Whether the terrain tile under an object exit point (world grid x, z) is
blocked: the client 403FB0 reads the bit-0 tile flag through 4038D0 before an outline
exit continues on the terrain. Missing coverage is never a block (the
package's fail-open policy).
================
*/
func (v *WaterValidator) exitTileBlocked(regionID uint16, wx, wz float64) (globalTile, bool) {
	tilesPerAxis, tileSize, ok := v.gridParamsForRegion(regionID)
	if !ok {
		return globalTile{}, false
	}
	tile := globalTile{x: int(math.Floor(wx / tileSize)), z: int(math.Floor(wz / tileSize))}
	walkable, known := v.globalTileWalkable(tile, tilesPerAxis)
	return tile, known && !walkable
}

/*
================
clipContact

clipContact is the first blocking contact of a chord walk.
================
*/
type clipContact struct {
	tile     globalTile
	t        float64
	steppedX bool
	steppedZ bool
}

/*
================
clipChord

clipChord is the supercover DDA of walkChord with two deliberate
differences: it TESTS THE ENDPOINT TILE (the clip stops inside a blocked
destination too - the client does), and it reports the crossing
parameter and axis of the blocking entry so the caller can place the
rest point. blockedAt receives the (clamped) entry parameter for the
object-deck override. Corner rule is the same dual-probe walkChord
uses: a chord through an exact tile corner must not slip between two
diagonally blocked tiles.
================
*/
func (v *WaterValidator) clipChord(x0, z0, x1, z1, tileSize float64, startTile, endTile globalTile, blockedAt func(globalTile, float64) bool) (clipContact, bool, bool) {
	fx0, fz0 := x0/tileSize, z0/tileSize
	fx1, fz1 := x1/tileSize, z1/tileSize
	dx, dz := fx1-fx0, fz1-fz0

	tile := startTile
	stepX, stepZ := 0, 0
	tMaxX, tMaxZ := math.Inf(1), math.Inf(1)
	tDeltaX, tDeltaZ := math.Inf(1), math.Inf(1)

	if dx > 0 {
		stepX = 1
		tMaxX = (float64(tile.x+1) - fx0) / dx
		tDeltaX = 1 / dx
	} else if dx < 0 {
		stepX = -1
		tMaxX = (fx0 - float64(tile.x)) / -dx
		tDeltaX = 1 / -dx
	}
	if dz > 0 {
		stepZ = 1
		tMaxZ = (float64(tile.z+1) - fz0) / dz
		tDeltaZ = 1 / dz
	} else if dz < 0 {
		stepZ = -1
		tMaxZ = (fz0 - float64(tile.z)) / -dz
		tDeltaZ = 1 / -dz
	}

	for steps := 0; ; steps++ {
		if tile == endTile {
			return clipContact{}, false, false
		}
		if steps >= pathGuardMaxTiles {
			return clipContact{}, false, true
		}
		if tMaxX > 1 && tMaxZ > 1 {
			return clipContact{}, false, false
		}
		switch {
		case math.Abs(tMaxX-tMaxZ) < cornerEpsilon && stepX != 0 && stepZ != 0:
			// Exact corner crossing: probe the orthogonal neighbor first
			// (a diagonal squeeze between two blocked tiles is a wall the
			// client would not walk through), then advance the diagonal.
			t := tMaxX
			probeT := math.Min(t, 1)
			corner := globalTile{tile.x + stepX, tile.z}
			if corner != startTile && blockedAt(corner, probeT) {
				return clipContact{tile: corner, t: t, steppedX: true}, true, false
			}
			tile.x += stepX
			tile.z += stepZ
			tMaxX += tDeltaX
			tMaxZ += tDeltaZ
			if tile == startTile {
				continue
			}
			if blockedAt(tile, probeT) {
				return clipContact{tile: tile, t: t, steppedX: true, steppedZ: true}, true, false
			}
		case tMaxX < tMaxZ:
			t := tMaxX
			tile.x += stepX
			tMaxX += tDeltaX
			if tile == startTile {
				continue
			}
			if blockedAt(tile, math.Min(t, 1)) {
				return clipContact{tile: tile, t: t, steppedX: true}, true, false
			}
		default:
			t := tMaxZ
			tile.z += stepZ
			tMaxZ += tDeltaZ
			if tile == startTile {
				continue
			}
			if blockedAt(tile, math.Min(t, 1)) {
				return clipContact{tile: tile, t: t, steppedZ: true}, true, false
			}
		}
	}
}
