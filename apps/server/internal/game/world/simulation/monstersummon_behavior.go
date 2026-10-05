/*
===========================================================================

monstersummon_behavior.go - summoned monsters following their summoner

===========================================================================
*/

package simulation

import (
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

/*
==================
summonedTarget

547C70: local acquisition precedes the summoner's opponent. Personal
retaliation is already owned by the ordinary chase/attack state and never
enters this idle-only acquisition path.
==================
*/
func (ops *MonsterMoverOps) summonedTarget(divisionID string, instance monster.Instance, mover monster.MoverState, players []playerPose, nowMs int64) (playerPose, bool) {
	if !ops.acquisitionReady(divisionID, instance, nowMs) {
		return playerPose{}, false
	}
	if target, ok := nearestEligiblePlayer(instance, mover.LivePoseAt(nowMs, nil), players, instance.SummonSightRange); instance.SummonSightRange > 0 && ok {
		return target, true
	}
	parent, ok := ops.Monsters.Get(divisionID, mover.ControllerGID())
	if !ok || parent.CurrentHP == 0 {
		return playerPose{}, false
	}
	// 547DE5..547E59 reads both leader records, not its current movement
	// target. 53D7A0 includes height and rounds the distance to float32.
	live := mover.LivePoseAt(nowMs, nil)
	var candidates [2]playerPose
	var eligible [2]bool
	distances := [2]float32{1_000_000, 1_000_000}
	for index, gid := range parent.RememberedOpponents() {
		candidate, valid := eligiblePlayerByGid(instance, players, gid)
		if !valid || gid == 0 {
			continue
		}
		distances[index] = acquisitionDistance(live, candidate.Pose)
		candidates[index], eligible[index] = candidate, true
	}
	index := 0
	if distances[1] < distances[0] {
		index = 1
	}
	return candidates[index], eligible[index]
}

/*
==================
followSummoner

5487B0: Timer 7 gates admission, independently of FOLLOW's Timer 0.
The boolean means this decision handled the tick, not packet acceptance.
A rejected snapshot must not fall through into another stale-state plan.
==================
*/
func (ops *MonsterMoverOps) followSummoner(divisionID string, instance monster.Instance, mover monster.MoverState, nowMs int64) ([]Frame, bool) {
	if mover.ControllerGID() == 0 {
		return nil, false
	}
	plan, ok := ops.Monsters.prepareFollow(divisionID, instance, mover, nowMs)
	if !ok {
		return nil, true
	}
	if !plan.timers.CheckTimer(7, uint32(nowMs)) {
		frames, accepted := ops.Monsters.commitFollow(plan, mover, nil)
		return frames, !accepted
	}
	live, target := mover.LivePoseAt(nowMs, nil), plan.leader.LivePoseAt(nowMs, nil)
	if !followLeaderCompatible(plan, live, target) {
		frames, accepted := ops.Monsters.commitFollow(plan, mover, nil)
		return frames, !accepted
	}
	return ops.planFollowSegment(plan, live, target, nowMs)
}

func followLeaderCompatible(plan monsterFollowPlan, live, target monster.Pose) bool {
	return plan.mover.ControllerGID() != 0 && plan.leaderExists && plan.leaderHP != 0 &&
		monster.FollowLocationCompatible(live.RegionID, target.RegionID)
}

func (ops *MonsterMoverOps) planFollowSegment(plan monsterFollowPlan, live, target monster.Pose, nowMs int64) ([]Frame, bool) {
	distance := monster.NativeActorDistance(live, target)
	// 548877 admits equality. Admission only enters FOLLOW (55A930); its
	// movement callback runs on Timer 0, not in the Timer 7 selector.
	if !(distance >= float32(plan.instance.SummonerFollowRange)) {
		frames, accepted := ops.Monsters.commitFollow(plan, plan.mover, nil)
		return frames, !accepted
	}
	mover := plan.mover
	mustMoverTransition(&mover, monster.MoverEventFollowStarted, mover.ControllerGID())
	frames, _ := ops.Monsters.commitFollow(plan, mover, nil)
	return frames, true
}

func (ops *MonsterMoverOps) stopOrAdvanceFollow(divisionID string, instance monster.Instance, mover monster.MoverState, nowMs int64) ([]Frame, bool) {
	plan, ok := ops.Monsters.prepareFollow(divisionID, instance, mover, nowMs)
	if !ok {
		return nil, true
	}
	var frames []Frame
	// Arrival is a movement notification, not FOLLOW completion. 55A960
	// checks Timer 0 first and returns without cancellation for a dead/missing
	// controller. An explicit CSNM 12 owns unbinding.
	if mover.ArriveMs > mover.DepartMs && !mover.InFlight(nowMs) && !navigationNeedsContinuation(mover, nowMs) {
		mover.Pose = mover.To
		mover.From, mover.To = monster.Pose{}, monster.Pose{}
		mover.DepartMs, mover.ArriveMs = 0, 0
		mustMoverTransition(&mover, monster.MoverEventSegmentArrived, 0)
		frames = append(frames, correctionFrame(instance.Gid, mover.Pose))
	}
	if !plan.timers.CheckTimer(0, uint32(nowMs)) || !plan.leaderExists || plan.leaderHP == 0 {
		// The controller gate suspends the steering callback, not the accepted
		// navigation command. Its remaining corners still belong to movement.
		if navigationNeedsContinuation(mover, nowMs) {
			goal, _ := mover.NavigationGoal()
			speed, channel := mover.NavigationMotion()
			var movement []Frame
			mover, movement = ops.planSegment(instance, mover, goal, speed, channel, nowMs)
			frames = append(frames, movement...)
		}
		frames, _ = ops.Monsters.commitFollow(plan, mover, frames)
		return frames, true
	}
	live := mover.LivePoseAt(nowMs, ops.TerrainHeight)
	target := plan.leader.LivePoseAt(nowMs, ops.TerrainHeight)
	motion := monster.NativeFollowMotion(live, target, instance.BodyRadius(), plan.leaderRadius,
		mover.InFlight(nowMs), mover.MovementGoal(), func() uint32 { return monster.SummonRandomWord(ops.rand()) })
	if motion.Satisfied {
		// Event 36 -> vA0 ->5599A0 enters IDLE without stopping movement.
		mustMoverTransition(&mover, monster.MoverEventFollowSatisfied, 0)
		mover.BehaviorDeadlineMs = nowMs + ops.idleDelayMs()
		var home []Frame
		mover, home = ops.planIdleEntry(instance, mover, nowMs)
		frames = append(frames, home...)
	} else if motion.Move && instance.Ref.RunSpeed > 0 && motion.Motion.Distance > .01 {
		speed, channel := instance.WalkSpeed(), uint8(wire.MoveStateWalk)
		if monster.HomingRuns(instance.Ref.RunSpeed) {
			speed, channel = instance.RunSpeed(), wire.MoveStateRun
		}
		goal := normalizeMonsterPose(motion.Motion.Destination(live, motion.Motion.Distance))
		var movement []Frame
		mover, movement = ops.planSegment(instance, mover, goal, speed, channel, nowMs)
		frames = append(frames, movement...)
	} else if navigationNeedsContinuation(mover, nowMs) {
		goal, _ := mover.NavigationGoal()
		speed, channel := mover.NavigationMotion()
		var movement []Frame
		mover, movement = ops.planSegment(instance, mover, goal, speed, channel, nowMs)
		frames = append(frames, movement...)
	}
	frames, _ = ops.Monsters.commitFollow(plan, mover, frames)
	return frames, true
}
