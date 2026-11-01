package monster

import "math"

// FollowMotion is the decision made by the reference server's ordinary
// tactics follow callback.
// State completion, retaining an existing movement, and issuing a movement
// are different results. The caller owns the timer and state transaction.
type FollowMotion struct {
	Satisfied bool
	Move      bool
	Motion    WanderMotion
}

func nativeFacing(heading uint16) (float32, float32) {
	a := float64(heading) / 65535 * 2 * math.Pi
	return float32(math.Cos(a)), float32(math.Sin(a))
}

func nativePlanarLength(x, z float32) float32 {
	return float32(math.Sqrt(float64(float32(float64(x)*float64(x) + float64(z)*float64(z)))))
}

// The reference server's vector angle spills the dot product, both lengths,
// their product and quotient before ACOS. In particular, this is an
// unsigned angle, not atan2.
func nativeVectorAngle(ax, az, bx, bz float32) float32 {
	dot := float32(float64(ax)*float64(bx) + float64(az)*float64(bz))
	denom := float32(float64(nativePlanarLength(ax, az)) * float64(nativePlanarLength(bx, bz)))
	q := float32(float64(dot) / float64(denom))
	return float32(math.Acos(math.Max(-1, math.Min(1, float64(q)))))
}

// The reference server's angle bucket negates the input and takes its
// unsigned angle from +X. Its direction table builds directions at the bin
// CENTERS (5, 15, ...355 degrees), clockwise.
func nativeFormationDirection(x, z float32) (float32, float32) {
	degrees := float32(float64(nativeVectorAngle(1, 0, -x, -z)) * 57.29577951308232)
	bin := int(float64(degrees) / 10)
	bin = max(0, min(35, bin))
	radians := float32(float64(5+10*bin) * 0.01745329238474369)
	return normalizeWanderVector(float32(math.Cos(float64(radians))), -float32(math.Sin(float64(radians))))
}

func NativeFollowMotion(live, leader Pose, bodyRadius, leaderRadius float64, moving bool, oldGoal Pose, random func() uint32) FollowMotion {
	dx, dz := nativeRelativeXZ(live, leader)
	if float64(nativePlanarLength(dx, dz)) < float64(float32(bodyRadius))+50 {
		return FollowMotion{Satisfied: true}
	}
	fx, fz := nativeFacing(live.Heading)
	lx, lz := nativeFacing(leader.Heading)
	sign := float32(1)
	if float32(float64(-dz)*float64(fx)-float64(fz)*float64(-dx)) < 0 {
		sign = -1
	}
	ox, oz := nativeFormationDirection(-lz*sign, lx*sign)
	radius := float32(float64(float32(bodyRadius)) + float64(int32(leaderRadius)) + float64(random()%10+1))
	goal := (WanderMotion{X: float64(ox), Z: float64(oz)}).Destination(leader, float64(radius))
	dx, dz = nativeRelativeXZ(live, goal)
	distance := nativePlanarLength(dx, dz)
	x, z := normalizeWanderVector(dx, dz)
	angle := float32(float64(nativeVectorAngle(fx, fz, x, z)) * 57.29577951308232)
	// A small turn with a short chord keeps the old direction. While moving,
	// retain the previous command until its remaining length is <=30.
	if angle < 5 {
		cx := float32(float64(float32(fx*distance)) - float64(float32(x*distance)))
		cz := float32(float64(float32(fz*distance)) - float64(float32(z*distance)))
		if nativePlanarLength(cx, cz) <= 100 {
			gx, gz := nativeRelativeXZ(live, oldGoal)
			if moving && nativePlanarLength(gx, gz) > 30 {
				return FollowMotion{}
			}
			x, z = fx, fz
		}
	}
	return FollowMotion{Move: true, Motion: WanderMotion{X: float64(x), Z: float64(z), Distance: float64(distance)}}
}
