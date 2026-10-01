package movement

// Path-walkability observer for the movement accept path (levelup wave,
// LANE-6; finding P-MOVE, board seq 98/192).
//
// THE FINDING: ValidateMovement refuses only deep-water destinations; the
// navmesh walkability plane is loaded but consulted only by the enter-world
// spawn rescue. A modified client can therefore move through walls, up
// cliffs, and into geometry - the stock client refuses blocked-tile clicks
// locally (cellForLocal gate) and clips its own advance to nav edges
// (sub_428930 NavMesh_AdvanceWithinMesh -> sub_43b980 NavEdge_ClipSegment),
// so retail players can never compose such a move.
//
// WHY AN ENDPOINT CHECK IS NOT A FIX: ApplyMove interpolates a STRAIGHT
// LINE from the live position to the goal (worldstate.go LiveSpawnAt), so a
// legal endpoint can be reached through illegal terrain. The unit of
// validation here is the whole interpolated chord.
//
// WHY THE DEFAULT IS OBSERVE, NOT ENFORCE - SEALED FINDING (triangulate-
// wave, 2026-07-27, COORD seq 235 close; supersedes the "client routes
// around obstacles" premise this guard originally shipped under, which is
// now DISPROVEN, and the seq-178 unproven-premise annotation that briefly
// replaced it):
//
// THE CLIENT HARD-STOPS AT WALLS. It does not route, slide, or detour.
// Two independent witnesses whose posts crossed on the board (binary,
// seq 208; raw dump, seq 220 - derived before reading the binary post):
// click-to-move commits exactly ONE destination point (no A*, no waypoint
// planner at any layer); layer 1 resolves WHERE via the click-ray ground
// pick 0x88d340; layer 2 governs HOW FAR via the per-frame ValidateMove
// 0x412230 (bounded <=6 navmesh crossings, called from
// CICharactor_Update 0x85d890 -> PathCtl_TickIntegrate 0x86d6d0); on a
// blocked result (bit 0x10000000) the client CLEARS its goal and stops -
// no tangential re-aim, no auto-reissue.
//
// CONSEQUENCE FOR THIS GUARD (the reason observe was right all along): a
// chord that crosses unwalkable terrain toward a walkable destination is
// a LEGITIMATE REQUEST BY CONSTRUCTION - the real client sends it in good
// faith and then truncates its own movement at the wall. Category B
// (segmentBlocked) is therefore not a suspicion signal at all; enforcing
// on it would refuse players for asking what the stock client asks
// constantly. The forward fix is not a gate but a SIMULATION: replicate
// the client's clip server-side so the planes agree about where the
// character stops (COORD seq 235 - a scoping assessment owned by
// FABLE-SRV/GROK-SRV precedes any such code; note the server today
// interpolates the FULL chord, so for B chords the planes already
// disagree about where the character ends up). CAUTION for whoever
// implements it (board seq 259 DIV-1/DIV-2, REVISED clipreplicate-wave
// seq202/seq212 binary+dump: the rest point is OBSTACLE-CLASS DEPENDENT):
// the client's clip is a <=6-crossing walk fed TINY per-frame
// micro-segments inside an unbounded frame accumulate. On a TERRAIN
// blocked edge the integrator COMMITS the clipped edge point (rest ON the
// edge, seam-nudged ~0.2u inward, frame-rate INDEPENDENT - deterministic,
// exactly reproducible by a one-shot geometric clip). On an OBJECT mesh it
// does NOT commit (rest = last committed micro-step, short of the object,
// frame-quantized) - and object meshes are invisible to this tile plane
// anyway (Q2 C1). The clipreplicate-wave landed that clip: clip.go,
// SRO_MOVE_CLIENT_CLIP. Frame accumulation remains the
// gold reference for MEASURING the residual (Q4 differential oracle).
// NOBODY flips this guard to enforce on the back of the Q1 close - that
// decision is separate, smaller, and COORD's.
//
// So the guard ships as detection: it classifies and logs every suspect
// move, refusing nothing by default (that switch is a COORD decision with
// the telemetry attached).
//
// OBJECT-NAV AWARENESS (this wave; Ruling 27's owed work item): the
// classification is no longer terrain-tile-only. A blocked tile probed
// anywhere on the chord - endpoint, departure, or interior - is
// OVERRIDDEN when the chord point there stands on an object-nav deck
// (worldPointOnObjectDeck, objectnav.go: the native nearest-Y owner
// arbitration of sub_403d20 over the shipped BMS object-nav payloads).
// This kills the known category-A FALSE POSITIVE that blocked the enforce
// posture: a legal click onto a bridge deck whose underlying seabed tile
// is blocked (the Constantinople harbor bridge, region 0x6850) used to
// classify endpointBlocked - the one verdict enforce refuses - and now
// classifies by the deck the mover actually stands on. Ground-level
// hostile clicks are untouched: the override requires the destination
// height to sit meaningfully ABOVE the terrain plane, which a stock
// ground pick onto a blocked tile never does. Missing object data fails
// open to the old tile verdict (never a refusal), the package standard.

import (
	"math"
	"os"
	"sync/atomic"

	log "github.com/sirupsen/logrus"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

// EnvMovePathGuard selects the path-guard posture:
//
//	unset / ""             -> ENFORCE (secure production default)
//	"observe"              -> validate + log, refuse nothing (diagnostics)
//	"off"                  -> guard disabled entirely
//	"enforce"              -> refuse verdict A (endpointBlocked) ONLY; see PathGuard
//	anything else          -> OBSERVE with a loud warning (never silently
//	                          enforce, never silently off)
const EnvMovePathGuard = "SRO_MOVE_PATH_GUARD"

// PathGuardMode is the parsed posture.
type PathGuardMode string

const (
	PathGuardOff     PathGuardMode = "off"
	PathGuardObserve PathGuardMode = "observe"
	PathGuardEnforce PathGuardMode = "enforce"
)

// PathVerdict classifies one validated movement chord. The taxonomy is the
// telemetry split agreed with GROK-V6 (board seq 203): category A is
// hostile-shaped (the stock UI cannot compose a blocked-endpoint click),
// category B is EXPECTED on genuine play under Euclidean authority, and the
// remaining verdicts are the fail-open / exemption planes.
type PathVerdict string

const (
	// PathLegal: every covered tile of the chord is walkable.
	PathLegal PathVerdict = "legal"
	// PathEndpointBlocked (category A): the DESTINATION tile is unwalkable.
	// The stock client's ground-click gate refuses these locally, so an
	// accepted one implies a modified client. The only verdict "enforce"
	// refuses.
	PathEndpointBlocked PathVerdict = "endpointBlocked"
	// PathSegmentBlocked (category B): the endpoint is walkable but the
	// straight chord crosses at least one blocked tile. Genuine clicks
	// produce this constantly BY CONSTRUCTION (sealed, board seq 208/220):
	// the client sends the destination in good faith and hard-stops at the
	// first wall, so the chord legitimately crosses terrain the character
	// never traverses - NEVER an enforcement target (the fix, if any, is
	// replicating the client clip server-side, a COORD-scoped subsystem).
	PathSegmentBlocked PathVerdict = "segmentBlocked"
	// PathStartBlocked: the DEPARTURE tile is unwalkable (history from the
	// ungated past, spawn-rescue debris, or a prior accepted illegal move).
	// Never refused in any mode - the player must always be able to leave.
	// A same-tile hop on a blocked tile also classifies here, never as A.
	PathStartBlocked PathVerdict = "startBlocked"
	// PathNoCoverage: no walkability data covered any queried tile
	// (missing bundle / unknown region). The classifier records the gap;
	// the production guard refuses it because authority could not prove
	// the requested chord.
	PathNoCoverage PathVerdict = "noCoverage"
	// PathDungeonExempt: either end sits on the dungeon plane (region bit
	// 15). Dungeon locals are not bounded by the outdoor 1920-unit sector
	// grid (NormalizeSpawnFrame exempts them for the same reason), so
	// outdoor tile math must not be applied.
	PathDungeonExempt PathVerdict = "dungeonExempt"
)

// pathGuardMaxTiles caps the chord walk. A legitimate click is a handful of
// tiles (view-range clicks at 20u tiles); the wire clamp technically admits
// destinations hundreds of regions away, and the walk must stay O(bounded)
// inside the movement mutex. A truncated proof is refused by the production
// guard.
const pathGuardMaxTiles = 4096

// pathGuardSummaryEvery paces the periodic counter-summary log line (also
// emitted on the first inspection) so real-play rates are readable from the
// log without scraping every verdict line.
const pathGuardSummaryEvery = 1024

// PathReport is one validated chord.
type PathReport struct {
	Verdict PathVerdict
	// BlockedTileX/Z is the first offending WORLD-grid tile
	// (sector*tilesPerAxis + tile) for the three blocked verdicts.
	BlockedTileX, BlockedTileZ int
	// TilesChecked counts tiles the walk visited (including uncovered).
	TilesChecked int
	// TilesUncovered counts visited tiles with no walkability coverage.
	// Enforce mode refuses any nonzero count.
	TilesUncovered int
	// ObjectDeckOverrides counts blocked-tile probes overridden because
	// the chord point stands on an object-nav deck (objectnav.go): the
	// mover is on the deck plane, not the blocked ground under it. This
	// is the counter that killed the bridge-deck endpointBlocked false
	// positive (category A on a legal elevated click).
	ObjectDeckOverrides int
	// Truncated reports the walk hit pathGuardMaxTiles before the endpoint.
	Truncated bool
	// DistanceUnits is the 2D world distance of the chord.
	DistanceUnits float64
}

// ValidateMovementPath classifies the straight movement chord from -> to
// against the navmesh walkability plane. Both spawns must be in canonical
// region-local frame (LiveSpawnAt and NormalizeSpawnFrame both fold), and
// the walk addresses tiles through the same cross-region world grid the
// spawn rescue uses (globalTileWalkable), so the two consumers of the plane
// can never disagree about a tile.
//
// The classifier continues across missing tiles so it can report the whole
// proof gap. The production PathGuard refuses PathNoCoverage and any partial
// coverage.
func (v *WaterValidator) ValidateMovementPath(from, to simulation.Spawn) PathReport {
	return v.ValidateMovementPathFrom(from, simulation.NavOwner{}, to)
}

// pickableObjectSurface reports whether the destination point is on an object
// surface by the native FindNavCell rule applied to the point itself. This
// evaluates the CLICK (a ground pick carries the picked surface's own height,
// within wire truncation), never the walker - walker ownership comes only from
// the walk (navowner.go).
func (v *WaterValidator) pickableObjectSurface(to simulation.Spawn) bool {
	owner, _, ok := v.ResolveNavOwner(to, simulation.NavOwner{})
	return ok && owner.Kind == simulation.NavOwnerObject
}

// ValidateMovementPathFrom classifies the chord walked from the retained
// source owner (navowner.go); see ClipMovementPathFrom.
func (v *WaterValidator) ValidateMovementPathFrom(from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) PathReport {
	from = v.ownedStart(from, fromOwner)
	report := PathReport{Verdict: PathLegal, DistanceUnits: simulation.WorldDistance2D(from, to)}

	if simulation.IsDungeonRegion(from.RegionID) || simulation.IsDungeonRegion(to.RegionID) {
		report.Verdict = PathDungeonExempt
		return report
	}

	tilesPerAxis, tileSize, ok := v.gridParamsForRegion(from.RegionID)
	if !ok {
		if tilesPerAxis, tileSize, ok = v.gridParamsForRegion(to.RegionID); !ok {
			report.Verdict = PathNoCoverage
			return report
		}
	}

	// Continuous world-frame coordinates (regionSize == tilesPerAxis *
	// tileSize for every real bundle; the grid loader enforces per-grid
	// consistency through the tilesPerAxis match in globalTileWalkable).
	fromGrid := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: from.RegionID, X: from.X, Z: from.Z})
	toGrid := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: to.RegionID, X: to.X, Z: to.Z})
	fromWX, fromWZ := fromGrid.X, fromGrid.Z
	toWX, toWZ := toGrid.X, toGrid.Z

	startTile := globalTile{x: int(math.Floor(fromWX / tileSize)), z: int(math.Floor(fromWZ / tileSize))}
	endTile := globalTile{x: int(math.Floor(toWX / tileSize)), z: int(math.Floor(toWZ / tileSize))}

	walk := v.ownerWalk(from, fromOwner, to)
	covered := 0
	querier := &tileQuerier{v: v, tilesPerAxis: tilesPerAxis, grids: make(map[int64]*blockedGrid, 4)}
	// blockedAt takes the chord parameter of the probe so a blocked tile
	// is OVERRIDDEN where an object cell owns the walker (a bridge deck
	// over blocked seabed is a legal stand; without this a legal deck
	// click classified endpointBlocked). Ownership comes from the walk
	// from the retained source owner, never from chord height.
	blockedAt := func(tile globalTile, t float64) bool {
		walkable, known := querier.walkable(tile)
		report.TilesChecked++
		if !known {
			report.TilesUncovered++
			return false
		}
		covered++
		if walkable {
			return false
		}
		if _, _, owned := walk.objectAt(t); owned || walk.bridged(t) {
			report.ObjectDeckOverrides++
			return false
		}
		return true
	}

	// Endpoint and departure classify FIRST, so verdict precedence is
	// stable regardless of walk order: A (endpoint) > startBlocked > B.
	// A blocked start downgrades everything (escaping a bad tile inherently
	// crosses blocked terrain; counting it as A or B would poison the
	// telemetry both categories exist to keep clean).
	endBlocked := blockedAt(endTile, 1)
	if endBlocked && v.pickableObjectSurface(to) {
		// Category A means "a stock ground pick cannot produce this click".
		// A pick on a deck over blocked ground is producible even when the
		// walker cannot reach the deck; the walk then stops at the rail and
		// the chord reports segmentBlocked instead.
		endBlocked = false
		report.ObjectDeckOverrides++
	}
	startBlocked := false
	if startTile != endTile {
		startBlocked = blockedAt(startTile, 0)
	}

	switch {
	case startTile == endTile && endBlocked:
		report.Verdict = PathStartBlocked
		report.BlockedTileX, report.BlockedTileZ = endTile.x, endTile.z
	case startBlocked:
		report.Verdict = PathStartBlocked
		report.BlockedTileX, report.BlockedTileZ = startTile.x, startTile.z
	case endBlocked:
		report.Verdict = PathEndpointBlocked
		report.BlockedTileX, report.BlockedTileZ = endTile.x, endTile.z
	default:
		// Walk the interior of the chord (endpoints already classified).
		if tile, crossed, truncated := v.walkChord(fromWX, fromWZ, toWX, toWZ, tileSize, startTile, endTile, blockedAt); crossed {
			report.Verdict = PathSegmentBlocked
			report.BlockedTileX, report.BlockedTileZ = tile.x, tile.z
		} else if truncated {
			report.Truncated = true
		}
	}

	if covered == 0 {
		report.Verdict = PathNoCoverage
	}
	return report
}

// walkChord runs a 2D supercover grid traversal (Amanatides-Woo DDA) over
// the world tile grid from (x0,z0) to (x1,z1) in world units, invoking
// blockedAt on every tile STRICTLY BETWEEN startTile and endTile (those two
// were classified by the caller) with the chord parameter of the tile's
// entry crossing. Returns the first blocked tile, whether one was found,
// and whether the walk hit the tile cap.
func (v *WaterValidator) walkChord(x0, z0, x1, z1, tileSize float64, startTile, endTile globalTile, blockedAt func(globalTile, float64) bool) (globalTile, bool, bool) {
	// Positions in tile units.
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
			return globalTile{}, false, false
		}
		if steps >= pathGuardMaxTiles {
			return globalTile{}, false, true
		}
		// Advance to the next tile the chord enters. t > 1 means the
		// remaining boundary lies past the endpoint - numerically possible
		// when the endpoint tile was reached exactly on a boundary; the
		// endTile check above is the primary exit.
		if tMaxX > 1 && tMaxZ > 1 {
			return globalTile{}, false, false
		}
		// The chord parameter of the crossing about to be stepped: the
		// object-deck override samples the chord point there.
		entryT := math.Min(math.Min(tMaxX, tMaxZ), 1)
		switch {
		case math.Abs(tMaxX-tMaxZ) < cornerEpsilon && stepX != 0 && stepZ != 0:
			// The chord passes EXACTLY through a tile corner. A true
			// supercover visits both orthogonal tiles that share the corner
			// (a diagonal squeeze between two blocked tiles is a real
			// wall the client would not walk through). Probe both before
			// advancing diagonally; checking only one leaves a direction-
			// dependent corner-cut exploit.
			corners := [...]globalTile{
				{tile.x + stepX, tile.z},
				{tile.x, tile.z + stepZ},
			}
			for _, corner := range corners {
				if corner != endTile && corner != startTile && blockedAt(corner, entryT) {
					return corner, true, false
				}
			}
			tile.x += stepX
			tile.z += stepZ
			tMaxX += tDeltaX
			tMaxZ += tDeltaZ
		case tMaxX < tMaxZ:
			tile.x += stepX
			tMaxX += tDeltaX
		default:
			tile.z += stepZ
			tMaxZ += tDeltaZ
		}
		if tile == endTile || tile == startTile {
			continue
		}
		if blockedAt(tile, entryT) {
			return tile, true, false
		}
	}
}

// cornerEpsilon treats DDA boundary crossings within this tile-fraction of
// each other as an exact corner crossing (the tie the supercover walk must
// visit both sides of). It is a hair above float noise on the ~96-tile
// grid, tight enough that only genuine corner geometry trips it.
const cornerEpsilon = 1e-9

// tileQuerier answers world-grid tile walkability with a per-walk cache of
// sector -> grid resolutions. It is semantically IDENTICAL to
// globalTileWalkable (same sector math, same offset lookup, same
// tilesPerAxis gate, same tile index) - the cache only removes the
// per-tile surface re-resolution (lock + catalog lookups), which dominated
// the warm chord cost. Grids are immutable once built and the surface
// cache is append-only, so holding the pointers across the walk is safe
// (globalTileWalkable itself reads them outside the lock).
type tileQuerier struct {
	v            *WaterValidator
	tilesPerAxis int
	grids        map[int64]*blockedGrid // by sector offsetKey; nil = no coverage
}

func (q *tileQuerier) walkable(tile globalTile) (walkable, ok bool) {
	if tile.x < 0 || tile.z < 0 {
		return false, false
	}
	sectorX := tile.x / q.tilesPerAxis
	sectorY := tile.z / q.tilesPerAxis
	if sectorX > 0xff || sectorY > 0xff {
		return false, false
	}
	key := offsetKey(sectorX, sectorY)
	grid, cached := q.grids[key]
	if !cached {
		grid = q.v.gridForSectors(sectorX, sectorY, q.tilesPerAxis)
		q.grids[key] = grid
	}
	if grid == nil {
		return false, false
	}
	return grid.tileWalkable((tile.z%q.tilesPerAxis)*q.tilesPerAxis + tile.x%q.tilesPerAxis), true
}

// gridForSectors is globalTileWalkable's grid resolution step, factored so
// the querier can cache its result per sector.
func (v *WaterValidator) gridForSectors(sectorX, sectorY, tilesPerAxis int) *blockedGrid {
	regionID := simulation.RegionIDForSectors(sectorX, sectorY)
	surface := v.surfaceForRegion(regionID)
	if surface == nil {
		return nil
	}
	grid := surface.blockedByOffset[offsetKey(
		sectorX-simulation.SectorX(surface.seedRegionID),
		sectorY-simulation.SectorY(surface.seedRegionID),
	)]
	if grid == nil || grid.tilesPerAxis != tilesPerAxis {
		return nil
	}
	return grid
}

// gridParamsForRegion resolves the walkability grid geometry for a region's
// own sector (the same lookup RelocateStrandedSpawn opens with). ok=false
// when the region has no walkability coverage.
func (v *WaterValidator) gridParamsForRegion(regionID uint16) (tilesPerAxis int, tileSize float64, ok bool) {
	surface := v.surfaceForRegion(regionID)
	if surface == nil {
		return 0, 0, false
	}
	grid := surface.blockedByOffset[offsetKey(
		simulation.SectorX(regionID)-simulation.SectorX(surface.seedRegionID),
		simulation.SectorY(regionID)-simulation.SectorY(surface.seedRegionID),
	)]
	if grid == nil || grid.tileSize <= 0 || grid.tilesPerAxis < 1 {
		return 0, 0, false
	}
	return grid.tilesPerAxis, grid.tileSize, true
}

// ---- the guard (mode + telemetry) ----

// PathValidator is the seam PathGuard runs on (WaterValidator implements
// it; tests fake it).
type PathValidator interface {
	ValidateMovementPath(from, to simulation.Spawn) PathReport
}

// PathGuardStats is a snapshot of the guard's verdict counters since boot.
type PathGuardStats struct {
	Inspected       uint64
	Legal           uint64
	EndpointBlocked uint64
	SegmentBlocked  uint64
	StartBlocked    uint64
	NoCoverage      uint64
	DungeonExempt   uint64
	Truncated       uint64
	Refused         uint64
	// ObjectDeckOverrides totals the blocked-tile probes overridden by an
	// object-deck stand across all inspected chords - the running proof
	// mass that the bridge-deck category-A false positive stays dead.
	ObjectDeckOverrides uint64
}

// PathGuard classifies every accepted-so-far ground move. Enforce mode is the
// production default and refuses blocked endpoints, missing/partial coverage,
// and scans too long to prove.
// Category B (segmentBlocked) is never refused by any mode: under Euclidean
// server authority it fires on genuine play (see the package comment), so a
// mode that refused it would ship a known bug factory.
type PathGuard struct {
	Mode      PathGuardMode
	Validator PathValidator

	inspected       atomic.Uint64
	legal           atomic.Uint64
	endpointBlocked atomic.Uint64
	segmentBlocked  atomic.Uint64
	startBlocked    atomic.Uint64
	noCoverage      atomic.Uint64
	dungeonExempt   atomic.Uint64
	truncated       atomic.Uint64
	refused         atomic.Uint64
	deckOverrides   atomic.Uint64
}

// PathGuardModeFromEnv defaults to enforcement. An unrecognized value also
// fails closed to enforcement; diagnostics must be selected explicitly.
func PathGuardModeFromEnv() PathGuardMode {
	switch value := os.Getenv(EnvMovePathGuard); value {
	case "", string(PathGuardEnforce):
		return PathGuardEnforce
	case string(PathGuardObserve):
		return PathGuardObserve
	case string(PathGuardOff):
		return PathGuardOff
	default:
		log.Warnf("movement: pathguard %s=%q unrecognized (want off|observe|enforce); defaulting to enforce", EnvMovePathGuard, value)
		return PathGuardEnforce
	}
}

// NewPathGuardFromEnv builds the guard over the shared walkability plane.
// Returns nil when the guard is configured off; logs the boot posture
// loudly so a deployment can be audited from its first log lines.
func NewPathGuardFromEnv(validator PathValidator) *PathGuard {
	mode := PathGuardModeFromEnv()
	if mode == PathGuardOff {
		log.Warnf("movement: pathguard OFF (%s=off) - movement path telemetry disabled", EnvMovePathGuard)
		return nil
	}
	if mode == PathGuardEnforce {
		log.Infof("movement: pathguard ENFORCE (default) - endpoint-blocked moves are refused; segment-blocked moves are constrained by clientclip")
	} else {
		log.Warnf("movement: pathguard OBSERVE - movement enforcement is diagnostic-only")
	}
	return &PathGuard{Mode: mode, Validator: validator}
}

// InspectMove classifies one mode-1 ground move (from = the live departure
// point, to = the normalized goal ApplyMove will commit). Returns a refusal
// ONLY in enforce mode and ONLY for verdict A; every other outcome is
// telemetry.
func (g *PathGuard) InspectMove(characterName string, from, to simulation.Spawn) *simulation.MoveError {
	return g.InspectMoveFrom(characterName, from, simulation.NavOwner{}, to)
}

// InspectMoveFrom classifies the chord walked from the mover's retained
// source owner (navowner.go).
func (g *PathGuard) InspectMoveFrom(characterName string, from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) *simulation.MoveError {
	if g == nil || g.Validator == nil || g.Mode == PathGuardOff {
		return nil
	}

	var report PathReport
	if owned, ok := g.Validator.(ownerPathValidator); ok {
		report = owned.ValidateMovementPathFrom(from, fromOwner, to)
	} else {
		report = g.Validator.ValidateMovementPath(from, to)
	}
	total := g.inspected.Add(1)

	switch report.Verdict {
	case PathLegal:
		g.legal.Add(1)
	case PathEndpointBlocked:
		g.endpointBlocked.Add(1)
	case PathSegmentBlocked:
		g.segmentBlocked.Add(1)
	case PathStartBlocked:
		g.startBlocked.Add(1)
	case PathNoCoverage:
		g.noCoverage.Add(1)
	case PathDungeonExempt:
		g.dungeonExempt.Add(1)
	}
	if report.Truncated {
		g.truncated.Add(1)
	}
	if report.ObjectDeckOverrides > 0 {
		g.deckOverrides.Add(uint64(report.ObjectDeckOverrides))
	}

	wouldRefuse := report.Verdict == PathEndpointBlocked ||
		report.Verdict == PathNoCoverage ||
		report.TilesUncovered > 0 ||
		report.Truncated
	refusing := wouldRefuse && g.Mode == PathGuardEnforce

	if report.Verdict != PathLegal && report.Verdict != PathDungeonExempt {
		log.WithFields(log.Fields{
			"guard":         "pathwalk",
			"verdict":       string(report.Verdict),
			"char":          characterName,
			"mode":          string(g.Mode),
			"wouldRefuse":   wouldRefuse,
			"refusing":      refusing,
			"fromRegion":    from.RegionID,
			"fromX":         from.X,
			"fromZ":         from.Z,
			"toRegion":      to.RegionID,
			"toX":           to.X,
			"toZ":           to.Z,
			"blockedTile":   [2]int{report.BlockedTileX, report.BlockedTileZ},
			"tiles":         report.TilesChecked,
			"uncovered":     report.TilesUncovered,
			"deckOverrides": report.ObjectDeckOverrides,
			"truncated":     report.Truncated,
			"distance":      report.DistanceUnits,
		}).Debug("movement: pathguard verdict")
	}

	if total == 1 || total%pathGuardSummaryEvery == 0 {
		g.logSummary(total)
	}

	if refusing {
		g.refused.Add(1)
		reason := "pathguard endpointBlocked: destination tile is unwalkable " +
			"(stock client cannot compose this click)"
		if report.Verdict == PathNoCoverage || report.TilesUncovered > 0 {
			reason = "pathguard noCoverage: movement authority data is incomplete"
		} else if report.Truncated {
			reason = "pathguard truncated: movement chord exceeds the bounded authority scan"
		}
		return &simulation.MoveError{
			NativeErrorCode: simulation.NativeErrorInvalidRequest,
			Reason:          reason,
		}
	}
	return nil
}

// Stats snapshots the counters (test + telemetry surface).
func (g *PathGuard) Stats() PathGuardStats {
	return PathGuardStats{
		Inspected:           g.inspected.Load(),
		Legal:               g.legal.Load(),
		EndpointBlocked:     g.endpointBlocked.Load(),
		SegmentBlocked:      g.segmentBlocked.Load(),
		StartBlocked:        g.startBlocked.Load(),
		NoCoverage:          g.noCoverage.Load(),
		DungeonExempt:       g.dungeonExempt.Load(),
		Truncated:           g.truncated.Load(),
		Refused:             g.refused.Load(),
		ObjectDeckOverrides: g.deckOverrides.Load(),
	}
}

func (g *PathGuard) logSummary(total uint64) {
	stats := g.Stats()
	log.WithFields(log.Fields{
		"guard":           "pathwalk",
		"inspected":       total,
		"legal":           stats.Legal,
		"endpointBlocked": stats.EndpointBlocked,
		"segmentBlocked":  stats.SegmentBlocked,
		"startBlocked":    stats.StartBlocked,
		"noCoverage":      stats.NoCoverage,
		"dungeonExempt":   stats.DungeonExempt,
		"truncated":       stats.Truncated,
		"refused":         stats.Refused,
		"deckOverrides":   stats.ObjectDeckOverrides,
		"mode":            string(g.Mode),
	}).Info("movement: pathguard summary")
}
