/*
===========================================================================

spawnregion.go - native generated-position admission to available regions

Monster nests and companions share the 530A00 region clamp before their
independent collision test. Region availability is not cell walkability.

===========================================================================
*/
package simulation

import (
	"math"

	worldgeom "opensro.online/server/internal/game/world"
)

const (
	nativeSpawnRegionAttempts = 8
	nativeSpawnRegionAngle    = 0.78539818525314331
)

/*
================
ClampGeneratedSpawnRegion

530A00's mode-zero arm, called by 5F6F57. The eight candidates are around
the generated position, not the origin; each uses the original separation.
Preserve x87 float stores and first-match order. Exhaustion falls back to
the admitted origin; the native diagnostic dump is not a gameplay effect.
================
*/
func ClampGeneratedSpawnRegion(origin, generated Spawn, available func(uint16) bool) Spawn {
	if IsDungeonRegion(origin.RegionID) != IsDungeonRegion(generated.RegionID) {
		return origin
	}
	if available == nil || available(generated.RegionID) {
		return generated
	}
	dx, dz := worldgeom.Delta(
		worldgeom.RegionXZ{RegionID: origin.RegionID, X: origin.X, Z: origin.Z},
		worldgeom.RegionXZ{RegionID: generated.RegionID, X: generated.X, Z: generated.Z},
	)
	dx, dz = float64(float32(dx)), float64(float32(dz))
	distance := float32(math.Sqrt(dx*dx + dz*dz))
	for i := 0; i < nativeSpawnRegionAttempts; i++ {
		angle := float32(float64(i) * nativeSpawnRegionAngle)
		cos := float32(math.Cos(float64(angle)))
		sin := float32(math.Sin(float64(angle)))
		candidate := generated
		candidate.X = float64(float32(float64(float32(generated.X)) + float64(cos)*float64(distance)))
		candidate.Z = float64(float32(float64(float32(generated.Z)) + float64(sin)*float64(distance)))
		candidate = NormalizeSpawnFrame(candidate)
		if available(candidate.RegionID) {
			return candidate
		}
	}
	return origin
}
