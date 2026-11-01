package monster

import "math"

// NativeActorRelative is the reference server's relative-position
// computation. Dungeon region words
// are not outdoor sector coordinates. World/population eligibility belongs
// to the caller; this arithmetic does not authorize crossing instances.
func NativeActorRelative(from, to Pose) (x, y, z float32) {
	if (from.RegionID^to.RegionID)&0x8000 != 0 {
		far := math.Float32frombits(0x7e967699)
		return far, far, far
	}
	dx, dz := float64(0), float64(0)
	if from.RegionID&0x8000 == 0 {
		dx = float64(int(to.RegionID&255)-int(from.RegionID&255)) * 1920
		dz = float64(int(to.RegionID>>8)-int(from.RegionID>>8)) * 1920
	}
	// Native adds the sector displacement before subtracting the origin,
	// and spills each component before the distance calculation.
	return float32(dx + float64(float32(to.X)) - float64(float32(from.X))),
		float32(float64(float32(to.Y)) - float64(float32(from.Y))),
		float32(dz + float64(float32(to.Z)) - float64(float32(from.Z)))
}

// NativeActorDistance is the reference server's actor distance followed by
// the caller's float32 spill.
// Incompatible planes deliberately overflow the squared length to +Inf;
// replacing that with ordinary sector distance can admit a false target.
func NativeActorDistance(from, to Pose) float32 {
	x, y, z := NativeActorRelative(from, to)
	xd, yd, zd := float64(x), float64(y), float64(z)
	squared := float32(yd*yd + xd*xd + zd*zd)
	return float32(math.Sqrt(float64(squared)))
}
