/*
===========================================================================

walkin_zone.go - the event zones a placed object's nav cells carry

A walk-in gate (the Donwhang cave's mouth, the red circle of a dungeon
door) is not an object a player clicks: it is a set of nav cells of a
placed object's collision mesh, named by the mesh's event-name list.
v1.188's region manager reports an object entering such a cell to the
GameServer's callback (CMap_Load 5304E0 registers 5301B0 at
RegionManagerBody+0xC8), which hands it to the PC's walk-in handler
(4EF480). This file answers which zone, if any, a point stands in; the
action lane owns what entering one does.

Two encodings exist in the v1.150 meshes:

  - nav flags & 2: each cell's extra byte is 0x80|0x40|index into the
    name list (cj_jin_entdoor01_01: 2 cells, 0xC0, "gate_jinsi_out");
  - nav flags & 1 without a cell byte: internal edges whose extra byte
    has 0x40 bound the zone (w_cd_dge01, the Donwhang mouth: a 30 x 30
    square of 2 cells inside four marked edges).

===========================================================================
*/
package movement

import (
	"math"
	"strings"

	"opensro.online/server/internal/game/world/simulation"
)

const (
	// noCellEvent marks a cell outside every event zone.
	noCellEvent int8 = -1
	// cellEventFlag is the event bit of a cell's extra byte; the low six
	// bits index the event-name list (0xC0 index 0 ... 0xC4 index 4 in
	// jinsi_floor2_s_center01).
	cellEventFlag      byte = 0x80
	cellEventIndexMask byte = 0x3f
	// edgeEventBoundary is the extra-byte bit of an internal edge on an
	// event zone's border (flags-5 meshes).
	edgeEventBoundary byte = 0x40
	// walkInZoneHeightTolerance bounds the gap between the point and the
	// zone cell's plane. Inference (the callback's call site inside the
	// region manager is not pinned): natively the mover's owner nav cell
	// is the zone cell, which only happens on that object's floor; every
	// zone floor in v1.150 lies flat on its object with nothing walkable
	// stacked within this band.
	walkInZoneHeightTolerance = 10.0
)

/*
================
cellEventIndex

The zone index a cell's extra byte names, if it is an event cell.
================
*/
func cellEventIndex(extra byte) (int8, bool) {
	if extra&cellEventFlag == 0 {
		return noCellEvent, false
	}
	return int8(extra & cellEventIndexMask), true
}

/*
================
newCellEvents
================
*/
func newCellEvents(cells int) []int8 {
	events := make([]int8, cells)
	for i := range events {
		events[i] = noCellEvent
	}
	return events
}

/*
================
resolveCellEvents

Settles a decoded mesh's zone cells. A cell byte naming no listed event
is dropped. A mesh with one event name and only boundary edges gets its
zone from them: cutting the marked internal edges splits the cells into
components, and a component bordering a marked edge, other than the
largest by area, is the zone. Inference: the marked square of w_cd_dge01
and of the Donwhang entry room is the smaller side of its own border (a
mesh's unconnected islands border no marked edge), and v1.150 has no
edge-encoded mesh with more than one name.
================
*/
func resolveCellEvents(mesh *objectNavMesh) {
	if mesh.cellEvents != nil {
		for i, event := range mesh.cellEvents {
			if event != noCellEvent && int(event) >= len(mesh.eventNames) {
				mesh.cellEvents[i] = noCellEvent
			}
		}
		return
	}
	if len(mesh.eventNames) != 1 || mesh.internal.events == nil {
		return
	}
	cells := mesh.cellCount()
	parent := make([]int, cells)
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	marked := false
	for i, dst := range mesh.internal.dstCell {
		if mesh.internal.events[i]&edgeEventBoundary != 0 {
			marked = true
			continue
		}
		if dst == 0xffff {
			continue
		}
		a, b := find(int(mesh.internal.srcCell[i])), find(int(dst))
		if a != b {
			parent[a] = b
		}
	}
	if !marked {
		return
	}
	bordered := map[int]bool{}
	for i, dst := range mesh.internal.dstCell {
		if mesh.internal.events[i]&edgeEventBoundary == 0 {
			continue
		}
		bordered[find(int(mesh.internal.srcCell[i]))] = true
		if dst != 0xffff {
			bordered[find(int(dst))] = true
		}
	}
	area := map[int]float64{}
	for cell := 0; cell < cells; cell++ {
		area[find(cell)] += objectCellArea(mesh, cell)
	}
	largest, largestArea := -1, -1.0
	for root, a := range area {
		if a > largestArea || a == largestArea && root < largest {
			largest, largestArea = root, a
		}
	}
	if len(area) < 2 {
		return
	}
	mesh.cellEvents = newCellEvents(cells)
	for cell := 0; cell < cells; cell++ {
		if root := find(cell); root != largest && bordered[root] {
			mesh.cellEvents[cell] = 0
		}
	}
}

/*
================
objectCellArea

A cell's XZ area in object-local units.
================
*/
func objectCellArea(mesh *objectNavMesh, cell int) float64 {
	a, b, c := mesh.cellA[cell], mesh.cellB[cell], mesh.cellC[cell]
	ax, az := float64(mesh.vertices[a*3]), float64(mesh.vertices[a*3+2])
	bx, bz := float64(mesh.vertices[b*3]), float64(mesh.vertices[b*3+2])
	cx, cz := float64(mesh.vertices[c*3]), float64(mesh.vertices[c*3+2])
	return math.Abs((bx-ax)*(cz-az)-(cx-ax)*(bz-az)) / 2
}

/*
================
WalkInZoneAt

The event zone the point stands in, by its codename in upper case as
the reference data spells it (GATE_DUNGEON_DH_IN), or "". Outdoors the
point's own region entry holds every object reaching into it (a large
object repeats in each neighbouring NVM); a dungeon point reads its
dungeon's resident objects.
================
*/
func (v *WaterValidator) WalkInZoneAt(at simulation.Spawn) string {
	if at.RegionID&0x8000 != 0 {
		v.dungeonSpawnOnce.Do(func() { v.dungeonSpawnSurfaces = v.loadDungeonSpawnSurfaces() })
		surface := v.dungeonSpawnSurfaces[at.RegionID]
		if surface == nil {
			return ""
		}
		return walkInZoneIn(surface.objects, nil, at)
	}
	surface := v.surfaceForRegion(at.RegionID)
	if surface == nil {
		return ""
	}
	dx := simulation.SectorX(at.RegionID) - simulation.SectorX(surface.seedRegionID)
	dz := simulation.SectorY(at.RegionID) - simulation.SectorY(surface.seedRegionID)
	set := v.objectNavSetForOffset(surface, dx, dz)
	return walkInZoneIn(set, v.walkInPlacements(objectNavSetKey{surface: surface, offset: offsetKey(dx, dz)}, set), at)
}

/*
================
walkInPlacements

The indices of a cached set's placements that carry event cells, so the
per-tick test skips every ordinary object.
================
*/
func (v *WaterValidator) walkInPlacements(key objectNavSetKey, set []resolvedObjectNav) []int {
	v.mu.Lock()
	indices, cached := v.walkInSets[key]
	v.mu.Unlock()
	if cached {
		return indices
	}
	indices = []int{}
	for i := range set {
		for _, mesh := range set[i].meshes {
			if mesh.cellEvents != nil {
				indices = append(indices, i)
				break
			}
		}
	}
	v.mu.Lock()
	v.walkInSets[key] = indices
	v.mu.Unlock()
	return indices
}

/*
================
walkInZoneIn

The zone among objects (only those at indices, when given) whose event
cell contains the point within walkInZoneHeightTolerance of its plane.
================
*/
func walkInZoneIn(objects []resolvedObjectNav, indices []int, at simulation.Spawn) string {
	test := func(object *resolvedObjectNav) string {
		p := object.placement
		c, s := math.Cos(p.yaw), math.Sin(p.yaw)
		lx := c*(at.X-p.x) + s*(at.Z-p.z)
		lz := -s*(at.X-p.x) + c*(at.Z-p.z)
		ly := at.Y - p.y
		for _, mesh := range object.meshes {
			if mesh.cellEvents == nil || lx < mesh.minX || lx > mesh.maxX || lz < mesh.minZ || lz > mesh.maxZ {
				continue
			}
			for cell, event := range mesh.cellEvents {
				if event == noCellEvent {
					continue
				}
				planeY, inside := objectCellPlaneYAt(mesh, cell, lx, lz)
				if inside && math.Abs(planeY-ly) <= walkInZoneHeightTolerance {
					return strings.ToUpper(mesh.eventNames[event])
				}
			}
		}
		return ""
	}
	if indices == nil {
		for i := range objects {
			if zone := test(&objects[i]); zone != "" {
				return zone
			}
		}
		return ""
	}
	for _, i := range indices {
		if zone := test(&objects[i]); zone != "" {
			return zone
		}
	}
	return ""
}
