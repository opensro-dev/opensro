/*
===========================================================================

navowner.go - retained surface identity and the cell ownership of a walked chord

===========================================================================
*/
package movement

import (
	"math"
	"slices"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

// Surface ownership for the movement authority. See simulation/navowner.go
// for the native contract (HLIL-verified, v1.188 SR_GameServer):
//
//   - a move walks from the RETAINED source cell (QueryMovement 0x98B300);
//   - terrain enters a placed object only by walking into it: the terrain
//     walker transforms the chord into the placement frame and runs the
//     object's own Move (CRTNavMeshTerrain_StepPlacedObject 0x9A0030 ->
//     vtable+8, 0x9B5A80); the reached cell becomes the owner;
//   - nearest-|deltaY| (CRTNavMeshTerrain_FindNavCell 0x99FD90, terrain wins
//     ties) is ONLY the teleport/entry rule (CheckPointValid 0x98B1D0).
//
// Do not reintroduce "is this chord point near a deck height?" tests: the
// chord Y of a move is the client's int16-truncated destination height, and
// guessing ownership from it is exactly what stranded players at the Hotan
// gate deck (terrain 243.04 beats deck 243.99 for a truncated y of 243).

// navOwner converts a resolved stand (at cell) into the value identity kept
// in world state. A stand built without its set address has no identity.
/*
================
navOwner
================
*/
func (s *objectDeckStand) navOwner(cell int) simulation.NavOwner {
	if s == nil || s.surface == nil {
		return simulation.NavOwner{}
	}
	return simulation.NavOwner{Kind: simulation.NavOwnerObject, Object: simulation.NavObjectCell{
		Surface: s.surface.seedRegionID,
		DX:      int8(s.setDX),
		DZ:      int8(s.setDZ),
		Object:  int32(s.objectIndex),
		Mesh:    int32(s.meshIndex),
		Cell:    int32(cell),
	}}
}

// withCell is the same placement/mesh standing on another cell.
/*
================
withCell
================
*/
func (s *objectDeckStand) withCell(cell int) *objectDeckStand {
	next := *s
	next.cellIndex = cell
	return &next
}

// local maps a world-grid point into the placement's object frame (inverse
// yaw+translate; Y is untouched by the placement transform).
/*
================
local
================
*/
func (s *objectDeckStand) local(gx, gz float64) (float64, float64) {
	p := s.placement
	c, sn := math.Cos(p.yaw), math.Sin(p.yaw)
	x := gx - (s.anchorX + p.x)
	z := gz - (s.anchorZ + p.z)
	return c*x + sn*z, -sn*x + c*z
}

// heightAt is the owned cell plane's world Y at a world-grid point.
/*
================
heightAt
================
*/
func (s *objectDeckStand) heightAt(gx, gz float64) (float64, bool) {
	lx, lz := s.local(gx, gz)
	y, ok := objectCellPlaneYAt(s.mesh, s.cellIndex, lx, lz)
	return y + s.placement.y, ok
}

// standForOwner rebuilds the stand an owner names. It returns nil when the
// identity no longer resolves (for example a different bundle now serves the
// region); callers then fall back to the native teleport rule.
/*
================
standForOwner
================
*/
func (v *WaterValidator) standForOwner(owner simulation.NavOwner) *objectDeckStand {
	if owner.Kind != simulation.NavOwnerObject {
		return nil
	}
	c := owner.Object
	surface := v.surfaceForRegion(c.Surface)
	if surface == nil || surface.seedRegionID != c.Surface {
		return nil
	}
	set := v.objectNavSetForOffset(surface, int(c.DX), int(c.DZ))
	if c.Object < 0 || int(c.Object) >= len(set) {
		return nil
	}
	meshes := set[c.Object].meshes
	if c.Mesh < 0 || int(c.Mesh) >= len(meshes) {
		return nil
	}
	mesh := meshes[c.Mesh]
	if c.Cell < 0 || int(c.Cell) >= mesh.cellCount() {
		return nil
	}
	anchorX := simulation.SectorX(surface.seedRegionID) + int(c.DX)
	anchorZ := simulation.SectorY(surface.seedRegionID) + int(c.DZ)
	return &objectDeckStand{
		set: set, objectIndex: int(c.Object), mesh: mesh, cellIndex: int(c.Cell),
		placement: set[c.Object].placement,
		anchorX:   float64(anchorX) * simulation.NativeRegionSize,
		anchorZ:   float64(anchorZ) * simulation.NativeRegionSize,
		surface:   surface, setDX: int(c.DX), setDZ: int(c.DZ), meshIndex: int(c.Mesh),
	}
}

// objectCellNeighbors lists the cells sharing an internal edge with cell.
/*
================
objectCellNeighbors
================
*/
func objectCellNeighbors(mesh *objectNavMesh, cell int) []int {
	var out []int
	for i := range mesh.internal.flags {
		a, b := int(mesh.internal.srcCell[i]), int(mesh.internal.dstCell[i])
		if a >= mesh.cellCount() || b >= mesh.cellCount() {
			continue
		}
		if a == cell {
			out = append(out, b)
		} else if b == cell {
			out = append(out, a)
		}
	}
	return out
}

// seedFrame converts a canonical spawn into the surface bundle's seed frame.
/*
================
seedFrame
================
*/
func seedFrame(surface *groundSurface, p simulation.Spawn) (float64, float64) {
	bx := p.X + float64(simulation.SectorX(p.RegionID)-simulation.SectorX(surface.seedRegionID))*surface.regionSize
	bz := p.Z + float64(simulation.SectorY(p.RegionID)-simulation.SectorY(surface.seedRegionID))*surface.regionSize
	return bx, bz
}

// ResolveNavOwner returns the owner of p and that surface's height at p.
// A still-valid hint is retained (the owned cell, or an edge neighbour after a
// sub-unit relocation such as wire truncation of X/Z). An unresolved or stale
// hint takes the native FindNavCell rule: nearest |deltaY|, terrain wins ties.
// ok is false only when no surface data covers p; dungeons keep their own
// navigation plane and return the input unchanged with ok=false.
/*
================
ResolveNavOwner
================
*/
func (v *WaterValidator) ResolveNavOwner(p simulation.Spawn, hint simulation.NavOwner) (simulation.NavOwner, float64, bool) {
	if simulation.IsDungeonRegion(p.RegionID) {
		return simulation.NavOwner{}, p.Y, false
	}
	surface := v.surfaceForRegion(p.RegionID)
	if surface == nil {
		return simulation.NavOwner{}, p.Y, false
	}
	bx, bz := seedFrame(surface, p)
	terrainY, terrainOK := surface.terrainHeightAt(bx, bz)
	grid := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: p.RegionID, X: p.X, Z: p.Z})

	switch hint.Kind {
	case simulation.NavOwnerObject:
		if stand := v.standForOwner(hint); stand != nil {
			if y, ok := stand.heightAt(grid.X, grid.Z); ok {
				return hint, y, true
			}
			// A retained native cell is repaired geometrically, not replaced
			// by a nearer-height surface after wire coordinate quantization.
			x, z := stand.local(grid.X, grid.Z)
			x, z = ownedCellStart(stand.mesh, stand.cellIndex, x, z)
			if y, ok := objectCellPlaneYAt(stand.mesh, stand.cellIndex, x, z); ok {
				return hint, y + stand.placement.y, true
			}
			for _, cell := range objectCellNeighbors(stand.mesh, stand.cellIndex) {
				next := stand.withCell(cell)
				if y, ok := next.heightAt(grid.X, grid.Z); ok {
					return next.navOwner(cell), y, true
				}
			}
		}
	case simulation.NavOwnerTerrain:
		if terrainOK {
			return simulation.TerrainOwner(), terrainY, true
		}
	}

	if !terrainOK {
		return simulation.NavOwner{}, p.Y, false
	}
	if stand := v.spawnObjectDeckStand(surface, bx, bz, p.Y, terrainY); stand != nil {
		return stand.navOwner(stand.cellIndex), stand.planeY, true
	}
	return simulation.TerrainOwner(), terrainY, true
}

// navWalk is the surface ownership along one straight chord: object-owned
// fraction spans (each traced cell by cell from the cell that entered it) and
// terrain everywhere else. A nil walk means ownership is unknown (dungeon or
// no data) and callers keep their legacy behaviour for that plane.
/*
================
navWalk
================
*/
type navWalk struct {
	paths []*objectOwnedPath // ascending, non-overlapping global-t spans
	// bridges are the [visit, crossing] fractions an outline entry skips
	// (CRTNavMeshObj_EnterFromOutside places the walker at the crossing).
	bridges [][2]float64
}

// lastExitBefore is the chord fraction at which the walker last left an
// object back onto terrain at or before t (0 when it never did).
/*
================
lastExitBefore
================
*/
func (w *navWalk) lastExitBefore(t float64) float64 {
	last := 0.0
	if w == nil {
		return last
	}
	for _, path := range w.paths {
		if n := len(path.spans); n > 0 && path.spans[n-1].to <= t+1e-9 && path.spans[n-1].to > last {
			last = path.spans[n-1].to
		}
	}
	return last
}

// bridged reports whether chord fraction t lies on terrain an entry skipped.
/*
================
bridged
================
*/
func (w *navWalk) bridged(t float64) bool {
	if w == nil {
		return false
	}
	for _, b := range w.bridges {
		if t >= b[0]-1e-9 && t <= b[1]+1e-9 {
			return true
		}
	}
	return false
}

/*
================
objectAt
================
*/
func (w *navWalk) objectAt(t float64) (*objectOwnedPath, int, bool) {
	if w == nil {
		return nil, 0, false
	}
	for _, path := range w.paths {
		if cell, ok := path.cellAt(t); ok {
			return path, cell, true
		}
	}
	return nil, 0, false
}

// ownerAt is the owner at chord fraction t.
/*
================
ownerAt
================
*/
func (w *navWalk) ownerAt(t float64) simulation.NavOwner {
	if w == nil {
		return simulation.NavOwner{}
	}
	if path, cell, ok := w.objectAt(t); ok {
		return path.stand.navOwner(cell)
	}
	return simulation.TerrainOwner()
}

// heightAt is the owned object plane height at t (terrain callers sample the
// heightfield themselves).
/*
================
heightAt
================
*/
func (w *navWalk) heightAt(t float64) (float64, bool) {
	path, _, ok := w.objectAt(t)
	if !ok {
		return 0, false
	}
	return path.heightAt(t)
}

// pathForMesh returns the walk span owning this placement mesh, if any.
/*
================
pathForMesh
================
*/
func (w *navWalk) pathForMesh(mesh *objectNavMesh, anchorX, anchorZ float64, ordinal int) *objectOwnedPath {
	if w == nil {
		return nil
	}
	for _, path := range w.paths {
		s := path.stand
		if s.mesh == mesh && s.anchorX == anchorX && s.anchorZ == anchorZ && s.placement.ordinal == ordinal {
			return path
		}
	}
	return nil
}

// spans flattens the walk into full [0,1] coverage for world state.
/*
================
spans
================
*/
func (w *navWalk) spans() []simulation.NavOwnerSpan {
	if w == nil {
		return nil
	}
	var out []simulation.NavOwnerSpan
	add := func(from, to float64, owner simulation.NavOwner) {
		if to-from <= 1e-12 {
			return
		}
		if n := len(out); n > 0 && out[n-1].Owner == owner {
			out[n-1].To = to
			return
		}
		out = append(out, simulation.NavOwnerSpan{From: from, To: to, Owner: owner})
	}
	t := 0.0
	for _, path := range w.paths {
		for _, span := range path.spans {
			add(t, span.from, simulation.TerrainOwner())
			add(span.from, span.to, path.stand.navOwner(span.cell))
			t = span.to
		}
	}
	add(t, 1, simulation.TerrainOwner())
	return out
}

// navWalkLegLimit mirrors QueryMovement's leg cap (nLegs > 6 fails).
const navWalkLegLimit = 7

// ownerWalk resolves surface ownership along from -> to starting from
// fromOwner (resolved with the teleport rule when unknown).
/*
================
ownerWalk
================
*/
func (v *WaterValidator) ownerWalk(from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) *navWalk {
	from = v.ownedStart(from, fromOwner)
	if simulation.IsDungeonRegion(from.RegionID) || simulation.IsDungeonRegion(to.RegionID) {
		return nil
	}
	owner := fromOwner
	if !owner.Resolved() {
		var ok bool
		if owner, _, ok = v.ResolveNavOwner(from, simulation.NavOwner{}); !ok {
			return nil
		}
	}
	a := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: from.RegionID, X: from.X, Z: from.Z})
	b := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: to.RegionID, X: to.X, Z: to.Z})
	walk := &navWalk{}
	t := 0.0
	var stand *objectDeckStand
	if owner.Kind == simulation.NavOwnerObject {
		if stand = v.standForOwner(owner); stand == nil {
			// The retained identity no longer resolves: re-enter by the
			// teleport rule rather than guessing terrain.
			var ok bool
			if owner, _, ok = v.ResolveNavOwner(from, simulation.NavOwner{}); !ok {
				return nil
			}
			stand = v.standForOwner(owner)
		}
	}
	for legs := 0; legs < navWalkLegLimit && t < 1; legs++ {
		if stand != nil {
			x0, z0 := stand.local(a.X, a.Z)
			x1, z1 := stand.local(b.X, b.Z)
			path := traceObjectCellsFrom(stand, x0, z0, x1, z1, t)
			if len(path.spans) == 0 {
				stand = nil
				continue
			}
			next, leave, enter := path.linkedContinuation(t)
			if next != nil {
				path.truncateAt(leave)
				walk.paths = append(walk.paths, path)
				walk.bridges = append(walk.bridges, [2]float64{leave, enter})
				stand, t = next, enter
				continue
			}
			walk.paths = append(walk.paths, path)
			t = path.spans[len(path.spans)-1].to
			stand = nil
			continue
		}
		entered, at, visit := v.terrainOutlineEntry(a.X, a.Z, b.X, b.Z, t)
		if entered == nil {
			break
		}
		// 428300 moves the walker straight to the outline crossing: the terrain
		// between the visit and the crossing is never walked.
		walk.bridges = append(walk.bridges, [2]float64{visit, at})
		stand, t = entered, at
	}
	return walk
}

// terrainOutlineEntry finds the first object the terrain walker enters on
// the chord after tMin: an OUTLINE edge crossed from outside whose flags
// carry neither side-block bit 0x01 nor 0x10 (the same admission client-next
// ports as terrainOwnerPath, 404510 -> 403FB0 -> 428300). The entered cell is
// the edge's source cell; the object's own walk then owns the chord.
/*
================
terrainOutlineEntry
================
*/
func (v *WaterValidator) terrainOutlineEntry(ax, az, bx, bz, tMin float64) (*objectDeckStand, float64, float64) {
	const eps = 1e-9
	start := globalTile{x: int(math.Floor(ax / simulation.NativeRegionSize)), z: int(math.Floor(az / simulation.NativeRegionSize))}
	end := globalTile{x: int(math.Floor(bx / simulation.NativeRegionSize)), z: int(math.Floor(bz / simulation.NativeRegionSize))}
	sectors := map[globalTile]struct{}{start: {}, end: {}}
	v.walkChord(ax, az, bx, bz, simulation.NativeRegionSize, start, end, func(sector globalTile, _ float64) bool {
		sectors[sector] = struct{}{}
		return false
	})
	// A few sets per crossed sector: a scan of a stack array, not a map
	// that allocated its buckets on every chord.
	var checkedBuf [32]objectNavSetKey
	checked := checkedBuf[:0]
	var best *objectDeckStand
	bestT, bestKey := math.Inf(1), math.Inf(1)
	probe := func(surface *groundSurface, anchorX, anchorZ int) {
		if surface == nil {
			return
		}
		dx := anchorX - simulation.SectorX(surface.seedRegionID)
		dz := anchorZ - simulation.SectorY(surface.seedRegionID)
		key := objectNavSetKey{surface: surface, offset: offsetKey(dx, dz)}
		if slices.Contains(checked, key) {
			return
		}
		checked = append(checked, key)
		set := v.objectNavSetForOffset(surface, dx, dz)
		baseX, baseZ := float64(anchorX)*simulation.NativeRegionSize, float64(anchorZ)*simulation.NativeRegionSize
		for i := range set {
			// Native visit order (404510): the placement is stepped once the
			// walker stands in one of its registered terrain cells.
			visit := math.Max(tMin, terrainVisitKey(set[i].placement.terrainCells, ax-baseX, az-baseZ, bx-baseX, bz-baseZ))
			if math.IsInf(visit, 1) || visit > bestKey {
				continue
			}
			for meshIndex, mesh := range set[i].meshes {
				candidate := &objectDeckStand{set: set, objectIndex: i, mesh: mesh, placement: set[i].placement,
					anchorX: float64(anchorX) * simulation.NativeRegionSize, anchorZ: float64(anchorZ) * simulation.NativeRegionSize,
					surface: surface, setDX: dx, setDZ: dz, meshIndex: meshIndex}
				x0, z0 := candidate.local(ax, az)
				x1, z1 := candidate.local(bx, bz)
				if math.Max(x0, x1) < mesh.minX || math.Min(x0, x1) > mesh.maxX ||
					math.Max(z0, z1) < mesh.minZ || math.Min(z0, z1) > mesh.maxZ {
					continue
				}
				rx, rz := x1-x0, z1-z0
				edges := &mesh.outline
				for e := range edges.flags {
					if edges.flags[e]&0x11 != 0 {
						continue
					}
					va, vb := edges.vertA[e], edges.vertB[e]
					exX, exZ := float64(mesh.vertices[va*3]), float64(mesh.vertices[va*3+2])
					sx, sz := float64(mesh.vertices[vb*3])-exX, float64(mesh.vertices[vb*3+2])-exZ
					den := rx*sz - rz*sx
					if math.Abs(den) < 1e-12 {
						continue
					}
					t := ((exX-x0)*sz - (exZ-z0)*sx) / den
					u := ((exX-x0)*rz - (exZ-z0)*rx) / den
					if t <= tMin+eps || t < visit-eps || t >= 1 || u < 0 || u > 1 {
						continue
					}
					if visit == bestKey && t >= bestT {
						continue
					}
					src := int(edges.srcCell[e])
					if src >= mesh.cellCount() {
						continue
					}
					cx, cz := objectCellCentroid2D(mesh, src)
					inside := sx*(cz-exZ) - sz*(cx-exX)
					origin := sx*(z0-exZ) - sz*(x0-exX)
					if origin*inside > 0 {
						continue // chord starts on the cell side: an exit, not an entry
					}
					best, bestT, bestKey = candidate.withCell(src), t, visit
				}
			}
		}
	}
	for sector := range sectors {
		if sector.x < 0 || sector.z < 0 || sector.x > 0xff || sector.z > 0xff {
			continue
		}
		chordSurface := v.surfaceForRegion(simulation.RegionIDForSectors(sector.x, sector.z))
		for dz := -objectAnchorSearchRadiusSectors; dz <= objectAnchorSearchRadiusSectors; dz++ {
			for dx := -objectAnchorSearchRadiusSectors; dx <= objectAnchorSearchRadiusSectors; dx++ {
				anchorX, anchorZ := sector.x+dx, sector.z+dz
				if anchorX < 0 || anchorZ < 0 || anchorX > 0xff || anchorZ > 0xff {
					continue
				}
				probe(chordSurface, anchorX, anchorZ)
				probe(v.surfaceForRegion(simulation.RegionIDForSectors(anchorX, anchorZ)), anchorX, anchorZ)
			}
		}
	}
	if best == nil {
		return nil, 0, 0
	}
	return best, bestT, bestKey
}

// NavAuthority is the surface-ownership seam the movement runtime consumes.
/*
================
NavAuthority
================
*/
type NavAuthority interface {
	ResolveNavOwner(p simulation.Spawn, hint simulation.NavOwner) (simulation.NavOwner, float64, bool)
	WalkOwners(from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) simulation.NavWalk
}

// WalkOwners is the ownership record of one committed chord: owner spans for
// the live-position plane and the owner reached at its end.
/*
================
WalkOwners
================
*/
func (v *WaterValidator) WalkOwners(from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) simulation.NavWalk {
	walk := v.ownerWalk(from, fromOwner, to)
	if walk == nil {
		return simulation.NavWalk{}
	}
	return simulation.NavWalk{Spans: walk.spans(), Rest: walk.ownerAt(1)}
}

// ownerPathValidator / ownerClipValidator are the owner-aware variants of the
// PathGuard and ClientClip seams. Test fakes that only implement the
// owner-less methods keep the teleport-rule behaviour.
/*
================
ownerPathValidator
================
*/
type ownerPathValidator interface {
	ValidateMovementPathFrom(from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) PathReport
}

/*
================
ownerClipValidator
================
*/
type ownerClipValidator interface {
	ClipMovementPathFrom(from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) ClipReport
}
