/*
===========================================================================

pursuit_direction.go - the outdoor pursuit step decision

The outdoor pursuit routine does not always step straight at its steering
point. This file owns the two decisions the chase leg author applies to an
authored goal before the segment is planned:

  - the direction gate: far from the steering point and roughly facing it,
    the actor steps along its current facing and realigns over the following
    ticks, unless the heading step would drift too far from the straight
    step, or the actor has already travelled too far on this move sequence;
  - the start-of-movement probe: a resting actor whose straight step is
    refused first tries alternate headings instead of pressing into the
    block.

The cave routine has neither decision and steps straight at the slot
point. The port's chase geometry is planar - distances and angles are
read in the ground plane, the frame the rest of the chase lane uses -
where the original reads three-dimensional vectors; the decision
boundaries coincide on flat ground.

===========================================================================
*/
package simulation

import (
	"math"

	"opensro.online/server/internal/game/world/monster"
)

// The native decision values. Strictness is part of the value: the gate
// arms only beyond the distance and inside the angle, the heading step is
// taken at or under the divergence, and the guard veto bites only beyond
// its distance.
const (
	pursuitGateDistance      = 30.0  // gate: steering distance must exceed this
	pursuitGateAlignDegrees  = 5.0   // gate: facing must be inside this angle
	pursuitGateDivergence    = 100.0 // heading step: divergence must not exceed this
	pursuitGuardDistance     = 30.0  // guard veto: sequence travel must not exceed this
	pursuitProbeReachMin     = 15.0  // probe: candidate reach clamps into [min, max]
	pursuitProbeReachMax     = 20.0
	pursuitProbeStepDistance = 20.0 // a probed step (adopted or not) travels this far
	pursuitProbeBucketDegree = 10.0 // probe: alternate headings are 10-degree buckets
	pursuitProbeBuckets      = 35   // probe: buckets tried after the straight one
)

/*
================
pursuitHeadingVector

The facing as a ground-plane unit vector. Heading words encode the wire
bearing (headingWordFromDelta), so the vector is (cos, sin) of the word's
bearing.
================
*/
func pursuitHeadingVector(heading uint16) (float64, float64) {
	radians := RadiansFromHeadingWord(heading)
	return math.Cos(radians), math.Sin(radians)
}

/*
================
pursuitAngleDegrees

The angle between two ground-plane unit vectors, in degrees.
================
*/
func pursuitAngleDegrees(ax, az, bx, bz float64) float64 {
	dot := ax*bx + az*bz
	if dot > 1 {
		dot = 1
	} else if dot < -1 {
		dot = -1
	}
	return math.Acos(dot) * 180 / math.Pi
}

/*
================
pursuitPointAlong

The point at a distance along a ground-plane unit direction from live,
folded back into region-local coordinates. The segment planner resolves
the height at the point it is given.
================
*/
func pursuitPointAlong(live monster.Pose, dirX, dirZ, distance float64) monster.Pose {
	point := monster.Pose{
		RegionID: live.RegionID,
		X:        live.X + dirX*distance,
		Y:        live.Y,
		Z:        live.Z + dirZ*distance,
		Heading:  live.Heading,
	}
	return normalizeMonsterPose(point)
}

/*
================
decidePursuitStep

The outdoor pursuit step decision for one authored chase goal. Returns
the goal to steer to and whether a step may be issued at all.

The probe runs first, from rest only: the straight step is offered to the
movement query (stepFree), and only when it comes back refused does the
probe try the 10-degree heading buckets after the straight one, at the
step distance clamped to [15, 20]. The first free bucket becomes the step
direction; a probed step travels 20 whether a bucket was free or not.

The gate then judges the (possibly probed) direction: beyond 30 from the
steering point and facing it within 5 degrees, the heading-projected
point over the step distance is compared with the straight point. A
divergence over 100 snaps the step straight; at or under, the step runs
along the current facing - unless the actor is already moving and has
travelled more than 30 from where its move sequence began, in which case
no step is issued this authoring and the current segment runs on.

stepFree is the movement query's verdict for a candidate endpoint: false
when the walk is refused (no coverage, or stopped at a wall). A nil
stepFree - no planner to ask - skips the probe; the gate still applies.
================
*/
func decidePursuitStep(live, goal monster.Pose, moving bool, guard monster.Pose, hasGuard bool, stepFree func(candidate monster.Pose) bool) (monster.Pose, bool) {
	dx, dz := monsterPoseDelta(live, goal)
	distance := math.Hypot(dx, dz)
	if distance < 0.01 {
		return goal, true
	}
	dirX, dirZ := dx/distance, dz/distance

	probed := false
	if !moving && stepFree != nil && !stepFree(goal) {
		reach := distance
		if reach < pursuitProbeReachMin {
			reach = pursuitProbeReachMin
		} else if reach > pursuitProbeReachMax {
			reach = pursuitProbeReachMax
		}
		bucket := int(math.Round(math.Atan2(dirZ, dirX) / (pursuitProbeBucketDegree * math.Pi / 180)))
		for k := 1; k <= pursuitProbeBuckets; k++ {
			angle := float64(bucket+k) * pursuitProbeBucketDegree * math.Pi / 180
			candidateX, candidateZ := math.Cos(angle), math.Sin(angle)
			if stepFree(pursuitPointAlong(live, candidateX, candidateZ, reach)) {
				dirX, dirZ = candidateX, candidateZ
				break
			}
		}
		distance = pursuitProbeStepDistance
		probed = true
	}

	headX, headZ := pursuitHeadingVector(live.Heading)
	if distance > pursuitGateDistance &&
		pursuitAngleDegrees(headX, headZ, dirX, dirZ) < pursuitGateAlignDegrees {
		divergence := math.Hypot(headX*distance-dirX*distance, headZ*distance-dirZ*distance)
		if divergence <= pursuitGateDivergence {
			if moving && hasGuard && planarDistance(live, guard) > pursuitGuardDistance {
				return goal, false
			}
			return pursuitPointAlong(live, headX, headZ, distance), true
		}
	}
	if probed {
		return pursuitPointAlong(live, dirX, dirZ, distance), true
	}
	return goal, true
}

/*
================
MonsterMoverOps.applyChaseDirectionGate

decidePursuitStep fed with the mover's live facts: whether the actor is
already moving, where its move sequence began (the guard reference the
progress tracker already owns), and - from rest only - the movement query
the probe offers candidate steps to. The query's blocked and no-coverage
results are the refusal; a slid or arrived walk is a free step.
================
*/
func (ops *MonsterMoverOps) applyChaseDirectionGate(live monster.Pose, mover monster.MoverState, goal monster.Pose, nowMs int64) (monster.Pose, bool) {
	moving := mover.InFlight(nowMs)
	guard := mover.Progress().SequenceOrigin()
	var stepFree func(candidate monster.Pose) bool
	if !moving && ops.hasPlanner() {
		stepFree = func(candidate monster.Pose) bool {
			path := ops.planPath(callerForMode(mover.Mode()), live, mover.LiveNavOwner(nowMs), candidate)
			return path != nil && path.Result()&(monster.NavResultClipped|monster.NavResultBlocked) == 0
		}
	}
	return decidePursuitStep(live, goal, moving, guard, guard.RegionID != 0, stepFree)
}
