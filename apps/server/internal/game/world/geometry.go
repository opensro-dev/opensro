/*
===========================================================================

geometry.go - outdoor sector grid and dungeon planar coordinate math

Owns the dependency-free coordinate contract shared by every outdoor and
dungeon world plane. Cross-region values are re-expressed here before
consumers perform same-frame arithmetic.

===========================================================================
*/
package world

import "math"

const (
	// OutdoorRegionSize is one native outdoor sector edge in world units.
	OutdoorRegionSize = 1920.0
	// DungeonRegionBit marks the non-sectorized dungeon coordinate plane.
	DungeonRegionBit uint16 = 0x8000
)

/*
================
RegionXZ

RegionXZ is one XZ point together with the region frame that gives its
local coordinates meaning.
================
*/
type RegionXZ struct {
	RegionID uint16
	X, Z     float64
}

/*
================
GridXZ

GridXZ is a point in the continuous outdoor sector grid.
================
*/
type GridXZ struct {
	X, Z float64
}

/*
================
SectorX

SectorX returns the low-byte outdoor sector coordinate.
================
*/
func SectorX(regionID uint16) int {
	return int(regionID) & 0xff
}

/*
================
SectorY

SectorY returns the complete high byte. Same-plane region differences
cancel the dungeon bit, preserving the native region-word convention.
================
*/
func SectorY(regionID uint16) int {
	return (int(regionID) >> 8) & 0xff
}

/*
================
RegionIDForSectors

RegionIDForSectors packs sector coordinates with native byte wrapping.
================
*/
func RegionIDForSectors(sectorX, sectorY int) uint16 {
	return uint16(((sectorY & 0xff) << 8) | (sectorX & 0xff))
}

/*
================
ExpandGrid

ExpandGrid re-expresses a region-local point in the continuous sector
grid used by navmesh and collision tiles.
================
*/
func ExpandGrid(position RegionXZ) GridXZ {
	return GridXZ{
		X: float64(SectorX(position.RegionID))*OutdoorRegionSize + position.X,
		Z: float64(SectorY(position.RegionID))*OutdoorRegionSize + position.Z,
	}
}

/*
================
LocalFromGrid

LocalFromGrid re-expresses a continuous grid point in regionID's frame.
================
*/
func LocalFromGrid(regionID uint16, point GridXZ) RegionXZ {
	return RegionXZ{
		RegionID: regionID,
		X:        point.X - float64(SectorX(regionID))*OutdoorRegionSize,
		Z:        point.Z - float64(SectorY(regionID))*OutdoorRegionSize,
	}
}

/*
================
IsDungeonRegion

IsDungeonRegion reports whether regionID belongs to the non-sectorized
dungeon plane.
================
*/
func IsDungeonRegion(regionID uint16) bool {
	return regionID&DungeonRegionBit != 0
}

/*
================
SamePlane

SamePlane reports whether both regions belong to the same outdoor/dungeon
coordinate plane. Sector arithmetic across that boundary is meaningless.
================
*/
func SamePlane(a, b uint16) bool {
	return IsDungeonRegion(a) == IsDungeonRegion(b)
}

/*
==================
SamePlaneAdjacent

The reference server's same-plane-and-adjacent-sectors rule, verified at
the instruction level: outdoors both sector bytes must be within one of
each other; inside the dungeon plane every pair of dungeon regions is
adjacent. This is the broadphase gate - the narrowing there belongs to
the callers' own checks (a floor's route, an actor distance), and sites
that need one specific region test for it themselves.
==================
*/
func SamePlaneAdjacent(a, b uint16) bool {
	if !SamePlane(a, b) {
		return false
	}
	if IsDungeonRegion(a) {
		return true
	}
	dx := int(a&0xff) - int(b&0xff)
	dz := int(a>>8) - int(b>>8)
	return dx >= -1 && dx <= 1 && dz >= -1 && dz <= 1
}

/*
================
Delta

Delta returns the frame-correct from->to planar vector. The high
sector byte excludes the dungeon marker; valid comparisons stay within one
plane, so this is identical to cancelling the shared marker bit.
================
*/
func Delta(from, to RegionXZ) (dx, dz float64) {
	dx = float64(SectorX(to.RegionID)-SectorX(from.RegionID))*OutdoorRegionSize + to.X - from.X
	fromSectorZ := (int(from.RegionID) >> 8) & 0x7f
	toSectorZ := (int(to.RegionID) >> 8) & 0x7f
	dz = float64(toSectorZ-fromSectorZ)*OutdoorRegionSize + to.Z - from.Z
	return dx, dz
}

/*
================
Distance

Distance returns the frame-correct planar distance.
================
*/
func Distance(from, to RegionXZ) float64 {
	dx, dz := Delta(from, to)
	return math.Hypot(dx, dz)
}

/*
================
NormalizeOutdoor

NormalizeOutdoor folds an outdoor point into its canonical region-local
frame. Dungeon locals are intentionally unbounded and pass through.
================
*/
func NormalizeOutdoor(position RegionXZ) RegionXZ {
	if IsDungeonRegion(position.RegionID) {
		return position
	}
	return normalizeGrid(position)
}

/*
================
clampOutdoorLocal

Sub-ULP negative coordinates (such as -4.44e-16 at an interpolated sector
seam) floor to sector offset -1, and adding OutdoorRegionSize (1920.0) in
IEEE-754 float64 rounds to 1920.0. Clamping folded coordinates into
[0, Nextafter(OutdoorRegionSize, 0)] preserves the half-open [0, 1920)
region-local invariant and keeps NormalizeOutdoor idempotent.
================
*/
func clampOutdoorLocal(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v >= OutdoorRegionSize {
		return math.Nextafter(OutdoorRegionSize, 0)
	}
	return v
}

/*
================
normalizeGrid
================
*/
func normalizeGrid(position RegionXZ) RegionXZ {
	dx := int(math.Floor(position.X / OutdoorRegionSize))
	dz := int(math.Floor(position.Z / OutdoorRegionSize))
	if dx == 0 && dz == 0 {
		return position
	}
	position.RegionID = RegionIDForSectors(
		SectorX(position.RegionID)+dx,
		SectorY(position.RegionID)+dz,
	)
	if dx != 0 {
		position.X = clampOutdoorLocal(position.X - float64(dx)*OutdoorRegionSize)
	}
	if dz != 0 {
		position.Z = clampOutdoorLocal(position.Z - float64(dz)*OutdoorRegionSize)
	}
	return position
}

/*
================
Interpolate

Interpolate returns a live point along from->to. Outdoor interpolation
folds across sector seams. Within a dungeon, signed locals are unbounded:
SRO_Client 1.150 4125F3..4125F9 skips outdoor bounds for the dungeon bit.
Do not fold a dungeon corpse into another region just because X/Z crosses
zero or 1920. Cross-region admission remains the caller's responsibility;
this same-dungeon fix does not establish dungeon portal traversal parity.
================
*/
func Interpolate(from, to RegionXZ, t float64) RegionXZ {
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	dx, dz := Delta(from, to)
	if IsDungeonRegion(from.RegionID) && from.RegionID == to.RegionID {
		return RegionXZ{RegionID: from.RegionID, X: from.X + dx*t, Z: from.Z + dz*t}
	}
	return normalizeGrid(RegionXZ{
		RegionID: from.RegionID,
		X:        from.X + dx*t,
		Z:        from.Z + dz*t,
	})
}

//============================================================================
// Plane-keyed arithmetic moved here from the simulation (#1646): the
// region bit decides a frame or a grid, which is this file's job.

// outdoorCellUnits and outdoorCellsPerAxis are the native message cell:
// six 320-unit cells per outdoor region axis.
const (
	outdoorCellUnits    = 320.0
	outdoorCellsPerAxis = 6
)

/*
================
ScopeRing

The 3x3 sector ring around an outdoor region, the simulation's monster
materialization and AI-activity candidate set. A dungeon region scopes to
exactly itself: ring math over the dungeon bit is meaningless.
================
*/
func ScopeRing(regionID uint16) []uint16 {
	if IsDungeonRegion(regionID) {
		return []uint16{regionID}
	}
	sx, sy := SectorX(regionID), SectorY(regionID)
	ring := make([]uint16, 0, 9)
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			ring = append(ring, RegionIDForSectors(sx+dx, sy+dy))
		}
	}
	return ring
}

/*
================
OutdoorLocalCell

The 320-unit message cell a region-local outdoor point falls in, as
(x, z) in 0..5. False for a dungeon region (its region lookup has a
different owner) and for a point outside the region's own [0, 1920)
frame or NaN: unlike the interest blocks it never folds into a
neighbour.
================
*/
func OutdoorLocalCell(position RegionXZ) (x, z int, ok bool) {
	if IsDungeonRegion(position.RegionID) || math.IsNaN(position.X) || math.IsNaN(position.Z) ||
		position.X < 0 || position.Z < 0 || position.X >= OutdoorRegionSize || position.Z >= OutdoorRegionSize {
		return 0, 0, false
	}
	x = int(float64(float32(position.X)) / outdoorCellUnits)
	z = int(float64(float32(position.Z)) / outdoorCellUnits)
	if x >= outdoorCellsPerAxis || z >= outdoorCellsPerAxis {
		return 0, 0, false
	}
	return x, z, true
}

/*
================
LocalCoordinateFloor

The lowest x/z a movement request may carry in this region: outdoor
packed-movement coordinates are unsigned, a dungeon's region-local
coordinates keep their sign (the int16 floor).
================
*/
func LocalCoordinateFloor(regionID uint16) float64 {
	if IsDungeonRegion(regionID) {
		return -0x8000
	}
	return 0
}
