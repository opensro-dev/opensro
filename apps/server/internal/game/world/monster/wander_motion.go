package monster

import (
	"math"
	worldgeom "opensro.online/server/internal/game/world"
)

// WanderMotion separates the short navigation probe from the actual movement
// request. The reference server's wander-direction selection returns a unit
// direction and 85 +/- rand()%40; its wander state entry passes both to the
// step destination and its movement submit. The 30-unit point is only a
// collision test.
type WanderMotion struct {
	X, Z, Distance float64
}

func NativeWanderMotion(heading uint16, random func() uint32) WanderMotion {
	sign := func(word uint32) float64 {
		if word%2 != 0 {
			return 1
		}
		return -1
	}
	distanceSign := sign(random())
	distance := 85 + distanceSign*float64(random()%40)
	turn := float64(random()%91 + 45)
	turn *= sign(random())
	// Actor+24/+2C are facing, not a position. Native adds the rotated
	// facing to the original and normalizes.
	yaw := float64(heading) / 65535 * 2 * math.Pi
	facingX, facingZ := float32(math.Cos(yaw)), float32(math.Sin(yaw))
	angle := float32(math.Atan2(float64(facingZ), float64(facingX)))
	angle = float32(float64(angle) + float64(float32(turn*0.01745329238474369)))
	x := float32(float64(facingX) + float64(float32(math.Cos(float64(angle)))))
	z := float32(float64(facingZ) + float64(float32(math.Sin(float64(angle)))))
	x, z = normalizeWanderVector(x, z)
	return WanderMotion{X: float64(x), Z: float64(z), Distance: distance}
}

func (m WanderMotion) Destination(from Pose, distance float64) Pose {
	from.X = float64(float32(float64(float32(from.X)) + float64(float32(m.X*distance))))
	from.Z = float64(float32(float64(float32(from.Z)) + float64(float32(m.Z*distance))))
	return from
}

func (m WanderMotion) AfterProbe(result uint32) WanderMotion {
	if result&(NavResultBlocked|NavResultClipped) != 0 {
		m.X, m.Z = -m.X, -m.Z
	}
	return m
}

// The native wander blend: add the normalized, region-relative controller
// direction to the already-normalized wander direction, then normalize
// again. A zero vector remains zero; no arbitrary fallback heading is
// introduced.
func (m WanderMotion) TowardController(from, controller Pose) WanderMotion {
	x, z := nativeRelativeXZ(from, controller)
	// The native relative helper explicitly zeroes Y, including indoor
	// relative vectors.
	x, z = normalizeWanderVector(x, z)
	x = float32(m.X + float64(x))
	z = float32(m.Z + float64(z))
	x, z = normalizeWanderVector(x, z)
	m.X, m.Z = float64(x), float64(z)
	return m
}

func normalizeWanderVector(x, z float32) (float32, float32) {
	// The native normalize spills squared length, sqrt and reciprocal.
	squared := float32(float64(x)*float64(x) + float64(z)*float64(z))
	length := float32(math.Sqrt(float64(squared)))
	var inverse float32
	if length > 0 {
		inverse = float32(1 / float64(length))
	}
	return float32(float64(x) * float64(inverse)), float32(float64(z) * float64(inverse))
}

func nativeRelativeXZ(from, to Pose) (float32, float32) {
	x, _, z := NativeTacticsRelative(from, to)
	return x, z
}

// NativeTacticsRelative is the reference server's tactics-relative
// position. Indoor coordinates are already local to
// their navigation plane, regardless of region-word differences. Crossing
// the indoor bit returns the native sentinel in ALL three components.
// Keep this tactics contract separate from outdoor world-grid arithmetic.
func NativeTacticsRelative(from, to Pose) (x, y, z float32) {
	if !worldgeom.SamePlane(from.RegionID, to.RegionID) {
		far := math.Float32frombits(0x7e967699)
		return far, far, far
	}
	dx, dz := float64(float32(to.X))-float64(float32(from.X)), float64(float32(to.Z))-float64(float32(from.Z))
	if ox, oz, sectorized := worldgeom.SectorOffset(from.RegionID, to.RegionID); sectorized {
		dx += ox
		dz += oz
	}
	return float32(dx), 0, float32(dz)
}
