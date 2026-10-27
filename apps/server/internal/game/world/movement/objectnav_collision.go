/*
===========================================================================

objectnav_collision.go - clips movement against native object navigation edges

===========================================================================
*/
package movement

import (
	"math"
	"slices"

	"opensro.online/server/internal/game/world/simulation"
)

// This file owns runtime object-deck arbitration and movement collision.
// objectnav.go owns resource decoding and placement resolution.
// objectDeckVerdict is the object-nav view of an enter-world spawn.
/*
================
objectDeckVerdict
================
*/
type objectDeckVerdict int

const (
	// objectDeckNone: terrain-owned spawn (or no object data reaches the
	// verdict) - the tile walkability plane decides alone, as before.
	objectDeckNone objectDeckVerdict = iota
	// objectDeckOpen: the spawn stands on an object surface whose lane
	// has a reachable open exit - a LEGAL elevated stand (bridge decks).
	// The tile plane below (often blocked seabed under a bridge) must
	// not strand it.
	objectDeckOpen
	// objectDeckSealed: the spawn stands on a data-sealed object lane -
	// trapped, no legal native walk can leave it. Stranded.
	objectDeckSealed
)

// objectDeckStandAt resolves the nearest object plane against the incoming Y.
// client 403D20 has no above-terrain clearance condition: stair decks can lie below
// the heightmap. Terrain wins ties and keeps walkers underneath overhead decks.
/*
================
objectDeckStandAt
================
*/
func (v *WaterValidator) objectDeckStandAt(surface *groundSurface, baseX, baseZ, y float64) *objectDeckStand {
	terrainY, ok := surface.terrainHeightAt(baseX, baseZ)
	if !ok {
		return nil
	}
	return v.spawnObjectDeckStand(surface, baseX, baseZ, y, terrainY)
}

// worldPointOnObjectDeck answers the movement planes' override question in
// their own frame (world = sector * NativeRegionSize + local): does the
// chord point at height y stand on an object deck? A blocked terrain tile
// under such a point is not a block for that move - the mover is on the
// deck plane, not the ground (the Constantinople harbor-bridge shape:
// deck ~105u above BLOCKED seabed tiles). Fail-open on any missing data.
/*
================
worldPointOnObjectDeck
================
*/
func (v *WaterValidator) worldPointOnObjectDeck(worldX, worldZ, y float64) bool {
	if worldX < 0 || worldZ < 0 {
		return false
	}
	sectorX := int(worldX / simulation.NativeRegionSize)
	sectorY := int(worldZ / simulation.NativeRegionSize)
	if sectorX > 0xff || sectorY > 0xff {
		return false
	}
	surface := v.surfaceForRegion(simulation.RegionIDForSectors(sectorX, sectorY))
	if surface == nil {
		return false
	}
	localX := worldX - float64(sectorX)*simulation.NativeRegionSize
	localZ := worldZ - float64(sectorY)*simulation.NativeRegionSize
	baseX := localX + float64(sectorX-simulation.SectorX(surface.seedRegionID))*surface.regionSize
	baseZ := localZ + float64(sectorY-simulation.SectorY(surface.seedRegionID))*surface.regionSize
	return v.objectDeckStandAt(surface, baseX, baseZ, y) != nil
}

// ---- segment-vs-object-edge first contact (the clip's object class) ----
//
// NATIVE EVIDENCE for the walk semantics replicated here:
//
//   - client sub_428930 NavMesh_AdvanceWithinMesh: the per-cell edge walk clips
//     the move segment against each cell edge. On a crossing of an edge
//     carrying bit 0x4, the side-block bits gate the stop: 0x2 clips when
//     the walker leaves the edge's side-0 cell (srcCell), 0x1 when it
//     leaves side-1 (dstCell) - navEdgeSideTest(0/1) == cell. A clipped
//     crossing COMMITS the snapped hit point and STOPS the walk (bit0
//     NAV_CLIPPED; the recursion ends, the endpoint is the contact) - it
//     never continues across terrain past the contact. An un-clipped
//     bit-0x4 crossing recurses into the adjacent cell.
//   - client sub_428930's non-0x4 branch REFLECTS (NAV_REFLECTED, returns null
//     cell): the walk also ends at the edge. client sub_403fb0
//     ObjNavMesh_StepThroughObject's exit tail then decides open-vs-block:
//     a null-cell end after an OPEN outline edge retags to terrain
//     (exitObjectToTerrain) and the move continues; every failing leg
//     returns 0x10000000 - the client's hard-stop signal.
//   - client sub_428f40 CRTNavMeshObj_StepMove clamps an outside start into the
//     cell then dispatches the walk; client sub_403fb0 owns the world<->object
//     matrix bridge (the placement yaw+translate this file inverts).
//
// SHIPPED-DATA GROUND TRUTH (all 1191 offset-7 payloads, decoded
// 2026-07-29): every INTERNAL edge carries bit 0x4 (values 0x04/0x07/
// 0x14/0x86/0x87), so internal crossability is decided purely by the
// side-block bits. NO outline edge carries 0x4 (values 0x00/0x03/0x08/
// 0x10/0x80/0x83/0x88/0x90), so outline crossings all take the reflect
// branch. Flags 0x0 is the proven open exit-to-terrain. Edge-object links
// (0x8) require a resolved placement/outline-edge link. The caller admits
// that bounded passage before this conservative unlinked-edge predicate. Outline edges never carry a dstCell (0 of 29k+), so
// "outside" is always the dst side and one directional rule covers both
// groups.

// objectEdgeContactWindowUnits is the legacy UNOWNED contact approximation.
// Owned cells bypass this window: a stair's endpoint-Y chord is not its
// surface. This constant is not evidence of a native Y cutoff. Replacing the
// outside-object path requires the complete client 403FB0/428300 probe semantics.
const objectEdgeContactWindowUnits = 2.0

// objectNavEdgeBlocks decides whether crossing one edge, approached from
// the given side, stops the native walk (semantics + data ground truth in
// the section banner above).
/*
================
objectNavEdgeBlocks
================
*/
func objectNavEdgeBlocks(flags byte, outline, fromSrcSide bool) bool {
	if outline {
		if flags&0x08 != 0 {
			// A neighbor link is not an exit to terrain. Without the linked
			// placement topology, only blocking preserves authority.
			return true
		}
		if flags == 0 {
			// The open exit-to-terrain outline edge.
			return false
		}
		if flags&0x03 == 0 {
			// Reflect-class outline without side bits (0x10/0x80/0x90 in
			// the corpus): stops from either side.
			return true
		}
	}
	if fromSrcSide {
		return flags&0x2 != 0
	}
	return flags&0x1 != 0
}

// objectCellCentroid2D is the XZ centroid of a cell triangle - the
// side-classification anchor for the edge's srcCell (the native
// navEdgeSideTest resolves sides through precomputed cell linkage; the
// centroid reproduces it for non-degenerate triangles).
/*
================
objectCellCentroid2D
================
*/
func objectCellCentroid2D(mesh *objectNavMesh, cell int) (float64, float64) {
	a := mesh.cellA[cell]
	b := mesh.cellB[cell]
	c := mesh.cellC[cell]
	x := (float64(mesh.vertices[a*3]) + float64(mesh.vertices[b*3]) + float64(mesh.vertices[c*3])) / 3
	z := (float64(mesh.vertices[a*3+2]) + float64(mesh.vertices[b*3+2]) + float64(mesh.vertices[c*3+2])) / 3
	return x, z
}

// objectContactOptions narrows which edges of a mesh can block a chord.
/*
================
objectContactOptions
================
*/
type objectContactOptions struct {
	terrainEntry func(float64) bool
	exits        bool
	path         *objectOwnedPath
	ownedMesh    bool
	// walk is the whole chord's surface ownership (navowner.go). Where it
	// puts the walker on another surface, this mesh's edges are not in the
	// walker's way (it is on a different deck, or on this one's terrain).
	walk *navWalk
	// walkerAt is the chord fraction the terrain walker stood at when it
	// stepped this object (native visit or its last exit back onto terrain).
	// 428300's blocked branch nudges that position, not the chord start.
	walkerAt func(t float64) float64
}

/*
================
withinOutlineNudgeMargin

withinOutlineNudgeMargin reports whether (x, z) lies within nudgeEscapeMargin
of any blocking outline segment of mesh (the 0.01u inward sliver an
inward-authored vertexDirection in outsideEdgeStart can place a terrain walker
into).
================
*/
func withinOutlineNudgeMargin(mesh *objectNavMesh, x, z float64) bool {
	const marginSq = (0.009999999776482582 * 2) * (0.009999999776482582 * 2)
	edges := &mesh.outline
	for i := range edges.flags {
		if edges.flags[i]&1 == 0 {
			continue
		}
		va, vb := int(edges.vertA[i])*3, int(edges.vertB[i])*3
		ax, az := float64(mesh.vertices[va]), float64(mesh.vertices[va+2])
		sx, sz := float64(mesh.vertices[vb])-ax, float64(mesh.vertices[vb+2])-az
		lenSq := sx*sx + sz*sz
		u := 0.0
		if lenSq > 0 {
			u = math.Max(0, math.Min(1, ((x-ax)*sx+(z-az)*sz)/lenSq))
		}
		dx, dz := x-(ax+sx*u), z-(az+sz*u)
		if dx*dx+dz*dz <= marginSq {
			return true
		}
	}
	return false
}

/*
==================
objectEdgeGroupChordContactDetail

Scans one edge group for the earliest blocking crossing of the object-local
chord, tightening bestT. Returns the (possibly improved) bestT and whether
any contact was found; response, when set, receives the contact point.
==================
*/
/*
================
objectEdgeGroupChordContactDetail
================
*/
func objectEdgeGroupChordContactDetail(
	mesh *objectNavMesh, edges *objectNavEdges, outline bool,
	x0, z0, y0, x1, z1, y1, bestT float64,
	linked func(int, bool, float64) bool, response *objectContactPoint, options ...objectContactOptions,
) (float64, bool) {
	var opts objectContactOptions
	if len(options) > 0 {
		opts = options[0]
	}
	const parallelEps = 1e-12
	rX, rZ := x1-x0, z1-z0
	found := false
	for i := range edges.flags {
		flags := edges.flags[i]
		if opts.terrainEntry != nil && !opts.ownedMesh && (!outline || flags&0x10 != 0) {
			continue
		}
		if outline {
			if flags == 0 && !opts.exits {
				continue
			}
		} else if flags&0x03 == 0 {
			continue
		}
		a := edges.vertA[i]
		b := edges.vertB[i]
		ax, ay, az := float64(mesh.vertices[a*3]), float64(mesh.vertices[a*3+1]), float64(mesh.vertices[a*3+2])
		bx, by, bz := float64(mesh.vertices[b*3]), float64(mesh.vertices[b*3+1]), float64(mesh.vertices[b*3+2])
		sX, sZ := bx-ax, bz-az
		den := rX*sZ - rZ*sX
		if math.Abs(den) < parallelEps {
			continue
		}
		t := ((ax-x0)*sZ - (az-z0)*sX) / den
		u := ((ax-x0)*rZ - (az-z0)*rX) / den
		if t <= 0 || t > 1 || t >= bestT || u < 0 || u > 1 {
			continue
		}
		edgeY := ay + (by-ay)*u
		chordY := y0 + (y1-y0)*t
		owner, owned := opts.path.cellAt(t)
		if owned && (!opts.ownedMesh || (int(edges.srcCell[i]) != owner && int(edges.dstCell[i]) != owner)) {
			continue
		}
		if !owned {
			if other, _, elsewhere := opts.walk.objectAt(t); elsewhere && other != opts.path {
				continue
			}
		}
		if !owned && opts.terrainEntry != nil && !opts.terrainEntry(t) {
			continue
		}
		if !owned && opts.terrainEntry == nil && math.Abs(chordY-edgeY) > objectEdgeContactWindowUnits {
			continue
		}
		ox, oz := x0, z0
		if !owned && opts.walkerAt != nil {
			at := opts.walkerAt(t)
			ox, oz = x0+(x1-x0)*at, z0+(z1-z0)*at
		}
		// Approach side: a straight chord crosses the edge LINE once, so
		// the walker's start side is the approach side (start exactly on
		// the line falls back to the far end's opposite side).
		originSide := sX*(oz-az) - sZ*(ox-ax)
		if originSide == 0 {
			originSide = -(sX*(z1-az) - sZ*(x1-ax))
		}
		cx, cz := objectCellCentroid2D(mesh, int(edges.srcCell[i]))
		srcSide := sX*(cz-az) - sZ*(cx-ax)
		if srcSide == 0 || originSide == 0 {
			// Degenerate cell or a chord along the edge line: cannot
			// orient the crossing. Fail open.
			continue
		}
		fromSrcSide := originSide*srcSide > 0
		inNudgeMargin := false
		if !owned && opts.walk != nil && outline && fromSrcSide {
			// Native CRTNavMeshObj_Move (client 428F40) dispatches an unowned
			// terrain walker to CRTNavMeshObj_EnterFromOutside (428300),
			// never to the owned-cell stepper. When a prior blocked outline hit
			// nudged the terrain walker 0.01u inward via an inward-authored
			// vertexDirection (outsideEdgeStart, client 4287A8..4288A6), its next step
			// starts on the srcCell side within vertexStep of that outline while
			// still owned by terrain: an outward chord escaping that nudge sliver
			// must not re-block, and an inward chord hitting a far outline edge
			// must block via outsideEdgeStart without capturing object ownership.
			const nudgeEscapeMargin = 0.009999999776482582 * 2
			if originSide*originSide <= nudgeEscapeMargin*nudgeEscapeMargin*(sX*sX+sZ*sZ) {
				continue
			}
			inNudgeMargin = withinOutlineNudgeMargin(mesh, ox, oz)
		}
		if outline && flags&8 != 0 && linked != nil && linked(i, fromSrcSide, t) {
			continue
		}
		reflection := (owned || opts.walk == nil) && opts.exits && outline && fromSrcSide && flags&0x1a == 0
		if !reflection && !objectNavEdgeBlocks(flags, outline, fromSrcSide) {
			continue
		}
		bestT = t
		found = true
		if response != nil {
			sourceSide := fromSrcSide && !inNudgeMargin
			*response = nativeObjectContact(mesh, edges, i, sourceSide, ox, oz, y0, x1, z1, reflection)
			response.outline = outline
			response.edgeFlags = flags
		}
	}
	return bestT, found
}

// objectMeshChordContact is the earliest blocking edge crossing of the
// object-local chord against one mesh, below bestT. Bounds-rejects the
// whole mesh first (XZ chord box vs vertex cloud, Y band widened by the
// contact window).
/*
================
objectMeshChordContactDetail
================
*/
func objectMeshChordContactDetail(mesh *objectNavMesh, x0, z0, y0, x1, z1, y1, bestT float64, linked func(int, bool, float64) bool, response *objectContactPoint, options ...objectContactOptions) (float64, bool) {
	var opts objectContactOptions
	if len(options) > 0 {
		opts = options[0]
	}
	if math.Max(x0, x1) < mesh.minX || math.Min(x0, x1) > mesh.maxX ||
		math.Max(z0, z1) < mesh.minZ || math.Min(z0, z1) > mesh.maxZ {
		return bestT, false
	}
	if !opts.ownedMesh && opts.terrainEntry == nil && (math.Max(y0, y1) < mesh.minY-objectEdgeContactWindowUnits ||
		math.Min(y0, y1) > mesh.maxY+objectEdgeContactWindowUnits) {
		return bestT, false
	}
	t, foundOutline := objectEdgeGroupChordContactDetail(mesh, &mesh.outline, true, x0, z0, y0, x1, z1, y1, bestT, linked, response, opts)
	t, foundInternal := objectEdgeGroupChordContactDetail(mesh, &mesh.internal, false, x0, z0, y0, x1, z1, y1, t, nil, response, opts)
	return t, foundOutline || foundInternal
}

/*
================
objectMeshChordContact
================
*/
func objectMeshChordContact(mesh *objectNavMesh, x0, z0, y0, x1, z1, y1, bestT float64, linked ...func(int, bool, float64) bool) (float64, bool) {
	var permit func(int, bool, float64) bool
	if len(linked) > 0 {
		permit = linked[0]
	}
	return objectMeshChordContactDetail(mesh, x0, z0, y0, x1, z1, y1, bestT, permit, nil)
}

// objectAnchorSearchRadiusSectors is the placement indexing contract: object
// nav meshes may overhang their anchor into an immediately adjacent sector.
// Every chord sector therefore searches its 3x3 anchor neighborhood.
const objectAnchorSearchRadiusSectors = 1

/*
================
chordSectors

The sectors a chord stands in, in the order the walker reaches them: the
start sector, every sector the supercover walk enters, then the end sector,
each once. Native CRTNavMeshTerrain_Move (404510) steps the placements of
each cell as it reaches it, and the scans keep the first of two equal
candidates, so the order is part of the result: a set (Go map) iteration
let a placement reachable through two surface identities (a multi-region
bundle and the neighbour's own bundle) win under either, run to run.
buf is the caller's stack storage.
================
*/
func (v *WaterValidator) chordSectors(buf []globalTile, ax, az, bx, bz float64) []globalTile {
	start := globalTile{x: int(math.Floor(ax / simulation.NativeRegionSize)), z: int(math.Floor(az / simulation.NativeRegionSize))}
	end := globalTile{x: int(math.Floor(bx / simulation.NativeRegionSize)), z: int(math.Floor(bz / simulation.NativeRegionSize))}
	sectors := append(buf[:0], start)
	v.walkChord(ax, az, bx, bz, simulation.NativeRegionSize, start, end, func(sector globalTile, _ float64) bool {
		if !slices.Contains(sectors, sector) {
			sectors = append(sectors, sector)
		}
		return false
	})
	if !slices.Contains(sectors, end) {
		sectors = append(sectors, end)
	}
	return sectors
}

// objectChordFirstContact walks the world-frame move chord against resolved
// object-nav placements anchored on the chord or in an adjacent sector. The
// candidate sectors come from a supercover walk rather than the chord's whole
// bounding rectangle, keeping diagonal requests linear and bounded.
/*
================
objectChordFirstContact
================
*/
func (v *WaterValidator) objectChordFirstContact(fromWX, fromWZ, fromY, toWX, toWZ, toY float64, walk *navWalk) (float64, float64, bool, objectContactPoint) {
	var sectorBuf [8]globalTile
	chordSectors := v.chordSectors(sectorBuf[:], fromWX, fromWZ, toWX, toWZ)
	multiSector := len(chordSectors) > 1

	// Nine anchors per crossed sector: a scan of a stack array, not a map
	// that allocated its buckets on every monster chord (57 MB a minute).
	var checkedBuf [64]objectNavSetKey
	checked := checkedBuf[:0]
	bestT, bestKey := math.Inf(1), math.Inf(1)
	var rest objectContactPoint
	found := false

	for _, sector := range chordSectors {
		if sector.x < 0 || sector.z < 0 || sector.x > 0xff || sector.z > 0xff {
			continue
		}
		probes := v.sectorObjectNavProbes(sector.x, sector.z)
		for pIdx := range probes {
			p := &probes[pIdx]
			if multiSector {
				if slices.Contains(checked, p.key) {
					continue
				}
				checked = append(checked, p.key)
			}
			surface, dx, dz := p.surface, p.dx, p.dz
			anchorWX, anchorWZ := p.baseX, p.baseZ
			set, setReach := p.set, p.reach
			lx0 := fromWX - anchorWX
			lz0 := fromWZ - anchorWZ
			lx1 := toWX - anchorWX
			lz1 := toWZ - anchorWZ
			// A chord that misses every placement of the set contacts none of them,
			// unless the walk stands on one (the placement test below, set-wide).
			if setReach.missedBy(lx0, lz0, lx1, lz1) && !walk.ownsInSet(surface, dx, dz) {
				continue
			}
			passages := resolveObjectPassages(set, lx0, fromY, lz0, lx1, toY, lz1)
			for i := range set {
				// A chord that misses everything this placement holds cannot
				// contact it (objectnav_reach.go), unless the walk owns one of
				// its meshes: that walk is scanned whatever the box says.
				if set[i].reach.missedBy(lx0, lz0, lx1, lz1) && !walk.ownsAnyMesh(&set[i], anchorWX, anchorWZ) {
					continue
				}
				placement := set[i].placement
				cosYaw, sinYaw := set[i].cosYaw, set[i].sinYaw
				ox0 := cosYaw*(lx0-placement.x) + sinYaw*(lz0-placement.z)
				oz0 := -sinYaw*(lx0-placement.x) + cosYaw*(lz0-placement.z)
				ox1 := cosYaw*(lx1-placement.x) + sinYaw*(lz1-placement.z)
				oz1 := -sinYaw*(lx1-placement.x) + cosYaw*(lz1-placement.z)
				oy0 := fromY - placement.y
				oy1 := toY - placement.y
				// Native visit order (404510): a terrain walker steps this placement
				// once it stands in a cell the placement is registered in, against the
				// rest of the chord; the earliest visit wins over later ones, whatever
				// their crossing parameters.
				visit := terrainVisitKey(placement.terrainCells, lx0, lz0, lx1, lz1)
				var terrainEntry func(float64) bool
				if placement.terrainCells != nil {
					terrainEntry = func(t float64) bool { return t >= visit-1e-9 }
				}
				for _, mesh := range set[i].meshes {
					var local objectContactPoint
					path := walk.pathForMesh(mesh, anchorWX, anchorWZ, placement.ordinal)
					if path == nil && math.IsInf(visit, 1) {
						continue
					}
					walkerAt := func(t float64) float64 { return math.Max(visit, walk.lastExitBefore(t)) }
					if t, ok := objectMeshChordContactDetail(mesh, ox0, oz0, oy0, ox1, oz1, oy1, math.Inf(1), func(edge int, _ bool, _ float64) bool { return passages.permits(i, edge) }, &local, objectContactOptions{exits: true, path: path, ownedMesh: path != nil, terrainEntry: terrainEntry, walk: walk, walkerAt: walkerAt}); ok {
						key := visit
						if path != nil {
							key = t // an owned mesh is walked cell by cell: its own contact order
						}
						if key > bestKey || key == bestKey && t >= bestT {
							continue
						}
						bestKey = key
						rest = local
						if rest.valid {
							rest.x = contactF32(cosYaw*local.x-sinYaw*local.z+placement.x) + anchorWX
							rest.z = contactF32(sinYaw*local.x+cosYaw*local.z+placement.z) + anchorWZ
							rest.y = contactF32(local.y + placement.y)
						}
						bestT = t
						found = true
					}
				}
			}
		}
	}
	return bestT, bestKey, found, rest
}

// spawnObjectDeckVerdict is the verdict RelocateStrandedSpawn consults:
// sealed only when the spawn stands on an object-nav deck (the
// objectDeckStandAt gates) and the cell's lane has no reachable proven
// terrain exit. An unresolved edge-object link is not treated as an exit.
/*
================
spawnObjectDeckVerdict
================
*/
func (v *WaterValidator) spawnObjectDeckVerdict(surface *groundSurface, baseX, baseZ, spawnY float64) objectDeckVerdict {
	stand := v.objectDeckStandAt(surface, baseX, baseZ, spawnY)
	if stand == nil {
		return objectDeckNone
	}
	if v.objectDeckStandSealed(stand) {
		return objectDeckSealed
	}
	return objectDeckOpen
}
