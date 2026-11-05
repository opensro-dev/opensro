/*
===========================================================================

monsterhoming.go - the walk home: one stored goal per episode

The return leg a monster walks back toward its nest. The destination is
picked once when the walk begins, stored on the mover, and every leg
re-reads it; the walk ends within 5 units of it at a settled leg
boundary (#1703). Split from monstertick.go on the homing boundary.

===========================================================================
*/

package simulation

import (
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

// startReturnLeg uses the native movement-channel consumer (the
// run-state step), not the unrelated obstacle rotation sign in
// the tactics record.
/*
================
startReturnLeg
================
*/
func (ops *MonsterMoverOps) startReturnLeg(divisionID string, instance monster.Instance, tactics monster.Tactics, mover monster.MoverState, event monster.MoverEvent, nowMs int64) []Frame {
	before := ops.Monsters.prepareNavigation(divisionID, instance.Gid)
	mover, frames := ops.planReturnLeg(instance, mover, event, nowMs)
	return ops.commitSteppedNavigation(divisionID, before, mover, frames)
}

// homingArrivalDistance is the fixed 3-D arrival radius around the stored
// walk-home destination: inside it, the walk home is over.
const homingArrivalDistance = 5

/*
================
planReturnLeg
================
*/
func (ops *MonsterMoverOps) planReturnLeg(instance monster.Instance, mover monster.MoverState, event monster.MoverEvent, nowMs int64) (monster.MoverState, []Frame) {
	speed := instance.WalkSpeed()
	channel := wire.MoveStateWalk
	if instance.Nest.HasControls && monster.HomingRuns(instance.Ref.RunSpeed) {
		speed, channel = instance.RunSpeed(), wire.MoveStateRun
	}
	live := mover.LivePoseAt(nowMs, ops.GroundHeight)
	mover.Pose = live
	mustMoverTransition(&mover, event, 0)
	mover.BehaviorDeadlineMs = 0
	dest := anchorPose(instance)
	if instance.Nest.HasControls {
		// One destination per walk home: selected when the walk begins
		// (HOMING entry) and re-read by every leg after. The episode's end
		// discards it, so a stored goal here always belongs to the walk in
		// progress; the entry clock below re-arms on every entry, as it
		// always has.
		goal, stored := mover.HomingGoal()
		if !stored {
			goal = normalizeMonsterPose(monster.HomingCandidate(dest, live, float32(instance.Nest.Radius), instance.Ref, func() uint32 { return monster.SummonRandomWord(ops.rand()) }))
			mover.SetHomingGoal(goal)
		}
		mover.HomingStartedMs = uint32(nowMs)
		// (effective sight / run speed) * 1000, truncate.
		// A stationary actor cannot complete this travel-dependent gate.
		mover.HomingAcquireAfterMs = ^uint32(0)
		if monster.HomingRuns(instance.Ref.RunSpeed) {
			mover.HomingAcquireAfterMs = uint32(float64(float32(instance.Nest.SightRange+instance.BodyRadius())) / float64(float32(instance.RunSpeed())) * 1000)
		}
		dest = goal
		// Arrival ends the walk: within homingArrivalDistance (3-D) of the
		// stored goal. The test runs only when actor and goal share a
		// region; across regions the region route supplies the next
		// waypoint instead (that route is #1698's work, not this planner's).
		// Arrival is judged only at a leg boundary, from a settled pose: a
		// mover with unexpired client-visible motion first goes through the
		// planner below, which re-aims it or settles it with the correction
		// frame the wire contract requires. A leg planned from inside the
		// radius matures at the goal, and the next entry ends the walk.
		if !mover.InFlight(nowMs) && live.RegionID == goal.RegionID && tacticsDistance3D(live, goal) < homingArrivalDistance {
			mustMoverTransition(&mover, monster.MoverEventSegmentArrived, 0)
			mover.ClearHomingGoal()
			mover.BehaviorDeadlineMs = nowMs + ops.idleDelayMs()
			return mover, nil
		}
		if ops.hasPlanner() {
			// Clip from HOME, not the actor's current position. The
			// anchor has no walked cell: the teleport rule resolves it.
			path := ops.planPath(navReturn, anchorPose(instance), NavOwner{}, dest)
			if path == nil {
				if ops.PlanRoute != nil {
					mover.SetNavigationMotion(speed, channel)
					return ops.waitForNavigation(instance.Gid, mover, live, dest, speed, nil, nowMs)
				}
				if speed > 0 && planarDistance(live, dest) >= 0.01 {
					mover.NoteMoveIssued(live, mover.InFlight(nowMs))
					if mover.ArriveMs <= mover.DepartMs {
						mover.NoteMoveStopped(live)
					}
				}
				return ops.holdNavigation(instance.Gid, mover, live, nowMs)
			}
			dest = path.Rest()
		}
	}
	return ops.planSegment(instance, mover, dest, speed, channel, nowMs)
}
