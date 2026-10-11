package simulation

import (
	"math"
	"opensro.online/server/internal/game/world/monster"
)

const monsterNavigationRetryMs int64 = 1000

func routedBehavior(m monster.MoverState) bool {
	return m.Mode() == monster.MoverChasing || m.Mode() == monster.MoverFollowing || m.Mode() == monster.MoverReturning
}

// The shared owner for chase/follow/home intent. Geometry is cached only on
// the admitted mover. Every corner is a normal B738 leg with the same surface
// sampler used by combat, scope and death. No player path semantics change.
func (ops *MonsterMoverOps) planSegment(instance monster.Instance, mover monster.MoverState, goal monster.Pose, speed float64, channel uint8, now int64) (monster.MoverState, []Frame) {
	if ops.PlanRoute == nil || !routedBehavior(mover) {
		return ops.planDirectSegment(instance, mover, goal, speed, channel, now)
	}
	goal.X, goal.Z = math.Round(goal.X), math.Round(goal.Z)
	goal = normalizeMonsterPose(goal)
	live := mover.LivePoseAt(now, ops.TerrainHeight)
	if planarDistance(live, goal) < .01 {
		hadSegment := mover.ArriveMs > mover.DepartMs
		mover.CancelNavigation()
		next, frames := ops.planDirectSegment(instance, mover, goal, speed, channel, now)
		if hadSegment && len(frames) == 0 {
			frames = []Frame{correctionFrame(instance.Gid, live)}
		}
		return next, frames
	}
	previous, active := mover.NavigationGoal()
	previousSpeed, _ := mover.NavigationMotion()
	mover.SetNavigationMotion(speed, channel)
	// A moving target must not defeat the failed-search work budget. Target
	// replacement/retaliation cancels intent through the state machine first.
	if active && mover.NavigationWaiting(now) {
		return mover, nil
	}
	changed := previous.RegionID != goal.RegionID || previous.X != goal.X || previous.Z != goal.Z || previous.Y != goal.Y
	// Small live-target updates may reuse a detour corridor. Direct pursuit
	// keeps its existing exact refresh behavior. Larger moves replace the
	// route; attack eligibility always reads the live target independently.
	reuseDetour := mover.NavigationHasDetour() && previous.RegionID == goal.RegionID && planarDistance(previous, goal) <= 32 && math.Abs(previous.Y-goal.Y) <= .01
	if active && changed && !reuseDetour {
		mover.CancelNavigation()
		mover.SetNavigationMotion(speed, channel)
	}
	for point, ok := mover.NavigationWaypoint(); ok && planarDistance(live, point) < .01; point, ok = mover.NavigationWaypoint() {
		mover.AdvanceNavigation()
	}
	point, ready := mover.NavigationWaypoint()
	if ready && mover.InFlight(now) && mover.Channel == channel && previousSpeed == speed && planarDistance(mover.MovementGoal(), point) < .01 {
		return mover, nil // same validated leg; no packet or geometry refresh
	}
	if !ready {
		route := ops.PlanRoute(live, goal)
		ops.Navigation.plan(callerForMode(mover.Mode()), route)
		if route == nil || route.Status() != monster.NavigationRouteReady || route.Len() == 0 {
			return ops.waitForNavigation(instance.Gid, mover, live, goal, now)
		}
		mover.BeginNavigation(route)
		point, _ = mover.NavigationWaypoint()
	}
	next, frames := ops.planDirectSegment(instance, mover, point, speed, channel, now)
	if !next.InFlight(now) || planarDistance(next.To, point) > .01 {
		// Geometry changed, a corridor failed, or movement made no progress.
		// Retain the behavior goal and invalidate the route. A clipped corner
		// must never masquerade as arrival at the target/leader/home.
		return ops.waitForNavigation(instance.Gid, mover, live, goal, now)
	}
	return next, frames
}

func (ops *MonsterMoverOps) waitForNavigation(gid uint32, mover monster.MoverState, live, goal monster.Pose, now int64) (monster.MoverState, []Frame) {
	// Correct an old segment once; subsequent retry ticks are silent.
	hadSegment := mover.ArriveMs > mover.DepartMs
	mover.Pose = live
	mover.From, mover.To = monster.Pose{}, monster.Pose{}
	mover.DepartMs, mover.ArriveMs = 0, 0
	mover.AdoptNavigation(nil)
	mover.WaitNavigation(goal, now+monsterNavigationRetryMs)
	if hadSegment {
		return mover, []Frame{correctionFrame(gid, live)}
	}
	return mover, nil
}

func navigationNeedsContinuation(mover monster.MoverState, now int64) bool {
	goal, active := mover.NavigationGoal()
	return active && !mover.InFlight(now) && planarDistance(mover.LivePoseAt(now, nil), goal) > .01
}
