package simulation

import (
	"math"

	worldgeom "opensro.online/server/internal/game/world"
)

// Vec3 is a world-space triple in float64 (the reference server's number
// width; the wire narrows to float32 at encode time only).
type Vec3 struct {
	X, Y, Z float64
}

const twoPi = 2 * math.Pi

// SectorX is the region's x sector (low byte), server.mjs missionRegionSectorX.
func SectorX(regionID uint16) int {
	return worldgeom.SectorX(regionID)
}

// SectorY is the region's y sector (high byte), server.mjs missionRegionSectorY.
// NOTE: this keeps the dungeon bit 0x8000 inside the sector value (mask 0xff),
// while WorldDistance2D masks it out (0x7f). The asymmetry is the reference
// server's, preserved deliberately: same-world positions share the bit so the
// sector DIFFERENCES this feeds cancel it out.
func SectorY(regionID uint16) int {
	return worldgeom.SectorY(regionID)
}

// RegionIDForSectors packs sector coordinates back into a region id,
// server.mjs missionRegionId. Out-of-range sectors wrap through the byte
// masks exactly as the JS bit ops do.
func RegionIDForSectors(sectorX, sectorY int) uint16 {
	return worldgeom.RegionIDForSectors(sectorX, sectorY)
}

/*
================
RegionIDFromSeedLocal

Which region a seed-local position landed in, server.mjs
missionRegionIdFromSeedLocal. A cave floor has no sectors: its locals
are its own frame and run far outside the outdoor 1920 square (a Jangan
floor sits near x -13580), so a point on it stays on it. Folding it by
sector would name a cave region that does not exist and lose every
approach and Trace goal on that floor.
================
*/
func RegionIDFromSeedLocal(seedRegionID uint16, localX, localZ, regionSize float64) uint16 {
	if worldgeom.IsDungeonRegion(seedRegionID) {
		return seedRegionID
	}
	seedX := SectorX(seedRegionID)
	seedY := SectorY(seedRegionID)
	return RegionIDForSectors(
		seedX+int(math.Floor(localX/regionSize)),
		seedY+int(math.Floor(localZ/regionSize)),
	)
}

// RegionLocalToSeedLocal re-expresses a region-local position in the seed
// region's local frame, server.mjs missionRegionLocalPositionToSeedLocal.
func RegionLocalToSeedLocal(seedRegionID, sourceRegionID uint16, position Vec3, regionSize float64) Vec3 {
	seedX := SectorX(seedRegionID)
	seedY := SectorY(seedRegionID)
	sourceX := SectorX(sourceRegionID)
	sourceY := SectorY(sourceRegionID)

	return Vec3{
		X: float64(sourceX-seedX)*regionSize + position.X,
		Y: position.Y,
		Z: float64(sourceY-seedY)*regionSize + position.Z,
	}
}

// SeedLocalToRegionLocal re-expresses a seed-local position as local to
// regionID, server.mjs missionSeedLocalPositionToRegionLocal.
func SeedLocalToRegionLocal(seedRegionID, regionID uint16, position Vec3, regionSize float64) Vec3 {
	seedX := SectorX(seedRegionID)
	seedY := SectorY(seedRegionID)
	regionX := SectorX(regionID)
	regionY := SectorY(regionID)

	return Vec3{
		X: position.X - float64(regionX-seedX)*regionSize,
		Y: position.Y,
		Z: position.Z - float64(regionY-seedY)*regionSize,
	}
}

// WorldDistance2D is the region-aware 2D distance in native units,
// server.mjs missionWorldDistance2D. The y sector masks with 0x7f so the
// dungeon bit 0x8000 stays out of the sector math (both positions share it
// for a same-world compare).
func WorldDistance2D(a, b Spawn) float64 {
	return worldgeom.Distance(
		worldgeom.RegionXZ{RegionID: a.RegionID, X: a.X, Z: a.Z},
		worldgeom.RegionXZ{RegionID: b.RegionID, X: b.X, Z: b.Z},
	)
}

// headingWordFromDelta is THE single travel-direction -> wire-heading encoder
// for this package. Every plane that faces an entity along its own movement
// goes through here: the player plane (HeadingFromMovement below), the monster
// plane (headingWordToward, monstertick.go) and the NPC patrol
// (ComputeNpcPatrolState, npc.go).
//
// Native 8791A0 yields MODEL yaw (zero faces -Z). Before serialization,
// 853550 subtracts pi/2; spawn decoder 852F80 calls 8535A0 to add it back.
// Therefore the wire bearing is atan2(dz,dx), not atan2(dx,-dz).
// Keep the existing server rounding policy; client serialization truncates.
func headingWordFromDelta(dx, dz float64) uint16 {
	yaw := math.Mod(math.Mod(math.Atan2(dz, dx), twoPi)+twoPi, twoPi)
	return uint16(int(math.Round(yaw/twoPi*0xffff)) & 0xffff)
}

// HeadingFromMovement derives the full-circle u16 heading of the from->to
// travel direction, server.mjs missionHeadingFromMovement. ok is false for a
// (near) zero-length hop, where the reference returns undefined and callers
// keep the previous angle.
func HeadingFromMovement(from, to Spawn) (heading uint16, ok bool) {
	dx, dz := worldgeom.Delta(
		worldgeom.RegionXZ{RegionID: from.RegionID, X: from.X, Z: from.Z},
		worldgeom.RegionXZ{RegionID: to.RegionID, X: to.X, Z: to.Z},
	)

	if math.Hypot(dx, dz) < 0.000001 {
		return 0, false
	}

	return headingWordFromDelta(dx, dz), true
}

// clampFloat mirrors server.mjs clampFiniteNumber exactly: ANY non-finite
// input (NaN or either infinity) returns min, then the finite value clamps
// into [min, max].
func clampFloat(v, min, max float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return min
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

/*
================
roundI16Word

The signed word the movement wire carries for a region-local coordinate:
clamp into [-0x8000, 0x7fff], round, and return the int16 bit pattern for
the u16 slot. The client reads these words signed on both directions of
the movement contract - the 0x7738 request (DecodeClientMovementRequest)
and the 0xB738 destination block (the client parser's i16 reads; the
source block is f32 since protocol 11, #1744) -
so a cave-local coordinate north of the region origin (negative z, or a
height below zero) must keep its sign here. Clamping it to zero instead
walks every viewer to the origin edge while the mover's own book keeps
the signed pose: the two disagree until the next f32 settle corrects it
in one step (#1679).
================
*/
func roundI16Word(v float64) uint16 {
	return uint16(int16(math.Round(clampFloat(v, -0x8000, 0x7fff))))
}
