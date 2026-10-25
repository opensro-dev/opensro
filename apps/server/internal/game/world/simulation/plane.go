/*
===========================================================================

plane.go - the navigation plane a position lives on, as one owner (#1646)

The open world and the caves (the dungeon region bit) run the same jobs
with different native providers: the sectorized region map outdoors and
the cave's own dungeon nav data indoors. Plane is that provider
seen from the simulation. Callers ask Planes.For(region) and never branch
on the region bit; the outdoor/cave difference lives only inside the two
implementations (movement/plane.go), which is also where native differs.

The interfaces live here, beside the code that consumes them, because the
movement package that implements them imports this one (the
MovementValidator precedent). Two things are deliberately not a plane
service: the tactics choice (which pursuit routine an actor runs is an AI
decision the server takes once, at spawn, from the plane's kind) and the interest
buckets (pure position arithmetic, mover_interest_index.go). The ground
height joins with its first migrated caller: outdoors it must be the
walkable surface nearest the caller (a deck or a bridge), not bare terrain,
and that needs its own deck parity probe (#1649 review).

===========================================================================
*/
package simulation

import (
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
PlaneKind
================
*/
type PlaneKind uint8

const (
	// PlaneOutdoor is the sectorized open world.
	PlaneOutdoor PlaneKind = iota
	// PlaneCave is a non-sectorized dungeon region (worldgeom.DungeonRegionBit).
	PlaneCave
)

/*
================
PlaneKindOf

The plane a region belongs to: the one place the region bit is read for
behaviour (worldgeom owns the arithmetic).
================
*/
func PlaneKindOf(regionID uint16) PlaneKind {
	if worldgeom.IsDungeonRegion(regionID) {
		return PlaneCave
	}
	return PlaneOutdoor
}

/*
================
Plane

One plane's navigation. Every method takes positions on this plane;
Planes.Between refuses a pair that spans two.
================
*/
type Plane interface {
	// Kind names the plane, for the AI choices keyed by it.
	Kind() PlaneKind
	// Clip walks from (with its retained surface owner) toward to and stops
	// where the plane blocks it; a refusal carries the native error.
	Clip(characterName string, from Spawn, fromOwner NavOwner, to Spawn) (Spawn, NavWalk, *MoveError)
	// LineOfSight reports whether the walk from -> to is clear.
	LineOfSight(from Spawn, fromOwner NavOwner, to Spawn) bool
	// PlanPath is the walked path from the retained owner toward goal.
	PlanPath(from monster.Pose, fromOwner NavOwner, goal monster.Pose) *monster.NavigationPath
	// NextWaypoint is the AI route toward goal: outdoors the direction-gate
	// detour, in a cave the native region route (#1645).
	NextWaypoint(from, goal monster.Pose) *monster.NavigationRoute
	// Relative is the planar vector from -> to.
	Relative(from, to worldgeom.RegionXZ) (dx, dz float64)
	// SameNeighbourhood reports whether two regions are the same or adjacent
	// on this plane.
	SameNeighbourhood(a, b uint16) bool
}

/*
================
Planes

The plane owner: For picks the plane of a region; Between does the same
for a pair and refuses one that crosses the outdoor/cave boundary, which
no navigation call may do.
================
*/
type Planes interface {
	For(regionID uint16) Plane
	Between(a, b uint16) (Plane, bool)
}
