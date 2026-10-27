package monster

import (
	"math"
	worldgeom "opensro.online/server/internal/game/world"
)

// The reference server mob's slot +3CC. This is a packed actor-class
// predicate, not the monster rarity byte. Ordinary uniques do not take this
// shortcut.
func HomingUsesExactAnchor(ref MonsterRef) bool {
	tid := NativeTypeWord(ref)
	return tid&2 != 0 && tid&0x1c == 4 && tid&0x60 == 0x40 && tid&0x780 == 0x200 && tid&0xf800 == 0x2000
}

// HomingCandidate lifts the reference server's homing-destination
// selection: one CRT draw, a normalized home-to-actor XZ vector, radius/3 +
// rand()%(1+trunc(radius/3)). Navigation then clips HOME -> candidate; this
// function must not do a terrain snap. Float32 stores mirror the native
// relative-position and normalization helpers and the destination component
// stores.
func HomingCandidate(home, current Pose, radius float32, ref MonsterRef, next func() uint32) Pose {
	if radius < 0 || math.IsNaN(float64(radius)) || math.IsInf(float64(radius), 0) {
		panic("invalid homing radius")
	}
	dx, dz := worldgeom.Delta(worldgeom.RegionXZ{RegionID: home.RegionID, X: float64(float32(home.X)), Z: float64(float32(home.Z))}, worldgeom.RegionXZ{RegionID: current.RegionID, X: float64(float32(current.X)), Z: float64(float32(current.Z))})
	// The native helper's FLDZ arm: this helper explicitly discards
	// vertical delta.
	x, z := float32(dx), float32(dz)
	length := float32(math.Sqrt(float64(float32(float64(x)*float64(x) + float64(z)*float64(z)))))
	inverse := float32(0)
	if length > 0 {
		inverse = float32(1 / float64(length))
	}
	x, z = x*inverse, z*inverse
	draw := nextAITimerRandom(next) // Even the exact-anchor class consumes it.
	divisor := int64(1) - int64(float64(radius)/-3)
	distance := float32(float32(float64(radius)/3) + float32(int64(draw)%divisor))
	if HomingUsesExactAnchor(ref) {
		return home
	}
	home.X = float64(float32(home.X) + x*distance)
	home.Y = float64(float32(home.Y))
	home.Z = float64(float32(home.Z) + z*distance)
	return home // caller normalizes the region frame before world navigation
}
