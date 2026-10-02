// Package world owns the dependency-free coordinate contract shared by
// every outdoor world plane. Cross-region values are re-expressed here before
// consumers perform same-frame arithmetic.
package world

import "math"

const (
	// OutdoorRegionSize is one native outdoor sector edge in world units.
	OutdoorRegionSize = 1920.0
	// DungeonRegionBit marks the non-sectorized dungeon coordinate plane.
	DungeonRegionBit uint16 = 0x8000
)

// RegionXZ is one XZ point together with the region frame that gives its
// local coordinates meaning.
type RegionXZ struct {
	RegionID uint16
	X, Z     float64
}

// GridXZ is a point in the continuous outdoor sector grid.
type GridXZ struct {
	X, Z float64
}

// SectorX returns the low-byte outdoor sector coordinate.
func SectorX(regionID uint16) int {
	return int(regionID) & 0xff
}

// SectorY returns the complete high byte. Same-plane region differences
// cancel the dungeon bit, preserving the native region-word convention.
func SectorY(regionID uint16) int {
	return (int(regionID) >> 8) & 0xff
}

// RegionIDForSectors packs sector coordinates with native byte wrapping.
func RegionIDForSectors(sectorX, sectorY int) uint16 {
	return uint16(((sectorY & 0xff) << 8) | (sectorX & 0xff))
}

// ExpandGrid re-expresses a region-local point in the continuous sector
// grid used by navmesh and collision tiles.
func ExpandGrid(position RegionXZ) GridXZ {
	return GridXZ{
		X: float64(SectorX(position.RegionID))*OutdoorRegionSize + position.X,
		Z: float64(SectorY(position.RegionID))*OutdoorRegionSize + position.Z,
	}
}

// LocalFromGrid re-expresses a continuous grid point in regionID's frame.
func LocalFromGrid(regionID uint16, point GridXZ) RegionXZ {
	return RegionXZ{
		RegionID: regionID,
		X:        point.X - float64(SectorX(regionID))*OutdoorRegionSize,
		Z:        point.Z - float64(SectorY(regionID))*OutdoorRegionSize,
	}
}

// IsDungeonRegion reports whether regionID belongs to the non-sectorized
// dungeon plane.
func IsDungeonRegion(regionID uint16) bool {
	return regionID&DungeonRegionBit != 0
}

// SamePlane reports whether both regions belong to the same outdoor/dungeon
// coordinate plane. Sector arithmetic across that boundary is meaningless.
func SamePlane(a, b uint16) bool {
	return IsDungeonRegion(a) == IsDungeonRegion(b)
}

/*
==================
SamePlaneAdjacent

Pos_AreSamePlaneAndAdjacentSectors (430CE0). Two dungeon positions compare
only within one dungeon region; outdoors both sector bytes must be within
one of each other.
==================
*/
func SamePlaneAdjacent(a, b uint16) bool {
	if !SamePlane(a, b) {
		return false
	}
	if IsDungeonRegion(a) {
		return a == b
	}
	dx := int(a&0xff) - int(b&0xff)
	dz := int(a>>8) - int(b>>8)
	return dx >= -1 && dx <= 1 && dz >= -1 && dz <= 1
}

// Delta returns the frame-correct from->to planar vector. The high
// sector byte excludes the dungeon marker; valid comparisons stay within one
// plane, so this is identical to cancelling the shared marker bit.
func Delta(from, to RegionXZ) (dx, dz float64) {
	dx = float64(SectorX(to.RegionID)-SectorX(from.RegionID))*OutdoorRegionSize + to.X - from.X
	fromSectorZ := (int(from.RegionID) >> 8) & 0x7f
	toSectorZ := (int(to.RegionID) >> 8) & 0x7f
	dz = float64(toSectorZ-fromSectorZ)*OutdoorRegionSize + to.Z - from.Z
	return dx, dz
}

// Distance returns the frame-correct planar distance.
func Distance(from, to RegionXZ) float64 {
	dx, dz := Delta(from, to)
	return math.Hypot(dx, dz)
}

// NormalizeOutdoor folds an outdoor point into its canonical region-local
// frame. Dungeon locals are intentionally unbounded and pass through.
func NormalizeOutdoor(position RegionXZ) RegionXZ {
	if IsDungeonRegion(position.RegionID) {
		return position
	}
	return normalizeGrid(position)
}

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
	position.X -= float64(dx) * OutdoorRegionSize
	position.Z -= float64(dz) * OutdoorRegionSize
	return position
}

// Interpolate returns a live point along from->to. Outdoor interpolation
// folds across sector seams. Within a dungeon, signed locals are unbounded:
// SRO_Client 1.150 4125F3..4125F9 skips outdoor bounds for the dungeon bit.
// Do not fold a dungeon corpse into another region just because X/Z crosses
// zero or 1920. Cross-region admission remains the caller's responsibility;
// this same-dungeon fix does not establish dungeon portal traversal parity.
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
