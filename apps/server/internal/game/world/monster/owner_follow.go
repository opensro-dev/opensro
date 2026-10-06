/*
===========================================================================

owner_follow.go - COS owner formation and steering decisions

GameServer 549F80 follows an owner through CPositioner's eight reservations.
The caller owns navigation, state timers and movement publication.

===========================================================================
*/

package monster

import "math"

const (
	OwnerFollowDistance        = float32(60)
	ownerFollowShortDistance   = float32(30)
	ownerFollowTurnDegrees     = float32(5)
	ownerFollowChordDistance   = float32(100)
	ownerFollowCatchUpDistance = float32(80)
	ownerFollowSurfaceAttempts = 50
	ownerFollowRadiusScale     = 3
	ownerFollowOffsetScale     = 0.80000001192092896
	ownerFollowNudge           = 0.69999998807907104
	ownerFollowCatchUpFactor   = 1.25
	ownerFollowPercentIdentity = 100
	ownerFollowRoundTolerance  = float32(0.01)
)

/*
================
OwnerFollowInput
================
*/
type OwnerFollowInput struct {
	Live, Owner, SlotGoal, OldGoal, OwnerGoal Pose
	Moving, OwnerMoving                       bool
}

/*
================
NativeOwnerFollowMotion

549F80: the <=60 completion test precedes formation construction. After that,
<=30 and >=5 degrees steer immediately; a small turn may retain the old goal.
430AD0 explicitly drops height from these vectors.
================
*/
func NativeOwnerFollowMotion(in OwnerFollowInput) FollowMotion {
	if NativeOwnerFollowDistance(in.Live, in.Owner) <= OwnerFollowDistance {
		return FollowMotion{Satisfied: true}
	}
	dx, dz := nativeRelativeXZ(in.Live, in.SlotGoal)
	distance := nativePlanarLength(dx, dz)
	if in.OwnerMoving {
		ox, oz := nativeRelativeXZ(in.Owner, in.OwnerGoal)
		remaining := nativePlanarLength(ox, oz)
		if distance > remaining {
			distance = float32(float64(distance) + float64(remaining))
		}
	}
	x, z := normalizeWanderVector(dx, dz)
	fx, fz := nativeFacing(in.Live.Heading)
	angle := float32(float64(nativeVectorAngle(fx, fz, x, z)) * 57.295780181884766)
	if distance > ownerFollowShortDistance && angle < ownerFollowTurnDegrees {
		cx := float32(float64(float32(fx*distance)) - float64(float32(x*distance)))
		cz := float32(float64(float32(fz*distance)) - float64(float32(z*distance)))
		if nativePlanarLength(cx, cz) <= ownerFollowChordDistance {
			gx, gz := nativeRelativeXZ(in.Live, in.OldGoal)
			if in.Moving && FollowLocationCompatible(in.Live.RegionID, in.OldGoal.RegionID) && nativePlanarLength(gx, gz) > ownerFollowShortDistance {
				return FollowMotion{}
			}
			x, z = fx, fz
		}
	}
	return FollowMotion{Move: true, Motion: WanderMotion{X: float64(x), Z: float64(z), Distance: float64(distance)}}
}

/*
================
AssignOwnerFollow

555460 releases the previous reservation before assigning. Unlike squad
approach, a full CPositioner leaves the tactics' remembered index unchanged.
================
*/
func (slots *ApproachSlots) AssignOwnerFollow(gid uint32, remembered int, live, owner Pose) int {
	slots.Release(gid)
	for _, occupant := range slots {
		if occupant == 0 {
			return slots.Assign(gid, NativeApproachPreferredSlot(live, owner), nil)
		}
	}
	return remembered
}

/*
================
NativeOwnerFollowRunFactor

548A30 adjusts parameter 18's source-zero percentage against authored run
speed. The caller supplies the owner's current channel speed and run speed.
================
*/
func NativeOwnerFollowRunFactor(in OwnerFollowSpeed) float32 {
	speed := in.OwnerRun
	if in.Distance >= ownerFollowCatchUpDistance {
		speed = in.OwnerCurrent
		if in.OwnerRunning {
			speed = float32(float64(speed) * ownerFollowCatchUpFactor)
		}
	}
	return float32(float64(speed) * ownerFollowPercentIdentity / float64(in.AuthoredRun))
}

/*
================
OwnerFollowSpeed
================
*/
type OwnerFollowSpeed struct {
	Distance, OwnerCurrent, OwnerRun, AuthoredRun float32
	OwnerRunning                                  bool
}

/*
================
NativeOwnerFollowGoal

55E090 probes the formation point, nudging toward the owner by 0.7 up to
50 times. Surface resolution belongs to the existing navigation owner.
5400C0 supplies three times the integer body radius outside BATTLE.
================
*/
func NativeOwnerFollowGoal(in OwnerFormationGoal) Pose {
	owner, slot, bodyRadius := in.Owner, in.Slot, in.BodyRadius
	surface, normalize := in.Surface, in.Normalize
	if slot < 0 {
		return owner
	}
	reach := float32(float64(bodyRadius) * ownerFollowRadiusScale)
	radius := float32(float64(reach) * ownerFollowOffsetScale)
	x, z := NativeApproachOffset(slot, reach)
	goal := owner
	goal.X = float64(float32(float64(float32(owner.X)) + float64(x)))
	goal.Z = float64(float32(float64(float32(owner.Z)) + float64(z)))
	goal = normalize(goal)
	for attempt := 0; attempt < ownerFollowSurfaceAttempts; attempt++ {
		var valid bool
		goal, valid = surface(goal)
		rx, ry, rz := NativeActorRelative(goal, owner)
		squared := float32(float64(ry)*float64(ry) + float64(rx)*float64(rx) + float64(rz)*float64(rz))
		length := float64(float32(math.Sqrt(float64(squared))))
		if length <= float64(radius) && valid {
			goal.X = ownerFollowRound(float32(goal.X), rx)
			goal.Z = ownerFollowRound(float32(goal.Z), rz)
			return goal
		}
		inverse := float32(0)
		if float32(length) > 0 {
			inverse = float32(1 / float64(float32(length)))
		}
		rx = float32(float64(rx) * float64(inverse))
		ry = float32(float64(ry) * float64(inverse))
		rz = float32(float64(rz) * float64(inverse))
		goal.X = float64(float32(float64(float32(goal.X)) + float64(float32(float64(rx)*ownerFollowNudge))))
		goal.Y = float64(float32(float64(float32(goal.Y)) + float64(float32(float64(ry)*ownerFollowNudge))))
		goal.Z = float64(float32(float64(float32(goal.Z)) + float64(float32(float64(rz)*ownerFollowNudge))))
		goal = normalize(goal)
	}
	return goal
}

/*
================
ownerFollowRound

55E2CD..55E39E uses floor and the relative-to-owner sign, including -1.
================
*/
func ownerFollowRound(coordinate, relative float32) float64 {
	base := float32(math.Floor(float64(coordinate)))
	if float32(math.Abs(float64(float32(coordinate-base)))) <= ownerFollowRoundTolerance {
		return float64(base)
	}
	if relative > ownerFollowRoundTolerance {
		return float64(float32(base + 1))
	}
	return float64(float32(base - 1))
}

/*
================
NativeOwnerFollowDistance

549F80 spills the squared planar length before the square root.
================
*/
func NativeOwnerFollowDistance(live, owner Pose) float32 {
	x, z := nativeRelativeXZ(live, owner)
	return nativePlanarLength(x, z)
}

/*
================
OwnerFormationGoal
================
*/
type OwnerFormationGoal struct {
	Owner      Pose
	Slot       int
	BodyRadius float32
	Surface    func(Pose) (Pose, bool)
	Normalize  func(Pose) Pose
}
