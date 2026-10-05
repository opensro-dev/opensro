/*
===========================================================================

monsterpursuit_controls.go - pursuit, homing and abandonment

===========================================================================
*/

package simulation

import (
	"math"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

/*
==================
advancePursuitControls

Stage the Timer 6 decision, geometry and movement packets, then admit them
together. A fresh hit must invalidate abandonment even when it retains the
same target (and therefore does not change RetaliationRevision).
==================
*/
func (ops *MonsterMoverOps) advancePursuitControls(division string, instance monster.Instance, mover monster.MoverState, target playerPose, live monster.Pose, now int64) ([]Frame, bool) {
	if !instance.Nest.HasControls || !instance.Nest.Controls.TraceEnabled() {
		return nil, false
	}
	// Sight acquisition has not committed its new mover yet. BATTLE's first
	// subsequent tick owns tracing, not the IDLE/WANDER acquisition callback.
	if mover.LastEvent() == monster.MoverEventAggroAcquired {
		return nil, false
	}
	// 55A090 -> 542670: the strategy's Timer 5 blocks battle decisions while
	// its accepted action owns the interval. Never cancel a released projectile.
	if mover.Mode() == monster.MoverAttacking && now < mover.NextAttackMs {
		return nil, false
	}
	s := ops.Monsters
	s.mu.Lock()
	state := s.populationForObject(division, instance.Gid)
	current, exists := state.instances.lookup(instance.Gid)
	alive := exists && current.CurrentHP != 0 && current.Motion.StateAt(now) == 0
	if !alive || !sameActorIdentity(current, instance) || state.movers.get(instance.Gid) != mover {
		s.mu.Unlock()
		return nil, true
	}
	timersBefore := *s.aiTimersLocked(state, current, now)
	opponents := current.Opponents
	s.mu.Unlock()
	timers := timersBefore
	// BATTLE calls 548120 -> 545E50 BEFORE Timer 6's speed/trace policy.
	// This independent home-distance predicate was present for HELP only.
	// FleeType selects other 548120 branches; do not apply ordinary pursuit to them.
	distance := tacticsDistance3D(live, monster.Pose{RegionID: target.Pose.RegionID, X: target.Pose.X, Y: target.Pose.Y, Z: target.Pose.Z})
	decision := monster.TraceContinue
	// The fixed-query uniques run 548340 in 548120's place (switchToSecondaryOpponent).
	if instance.Nest.Controls.FleeType == 0 && !instance.Nest.Controls.FixedQuery() {
		if !monsterWithinHomeTrace(instance, live, distance) {
			decision = monster.TraceAbandon
		}
	}
	if decision != monster.TraceAbandon && !timers.CheckTimer(6, uint32(now)) {
		return nil, false
	}
	last := mover.LastBattleActivityMs
	if uint32(now)-opponents[0].LastHitMs < uint32(now)-last {
		last = opponents[0].LastHitMs
	}
	if decision != monster.TraceAbandon {
		decision = instance.Nest.Controls.EvaluateTrace(distance, target.chaseGuidance().TargetMoving(), IsDungeonRegion(live.RegionID), uint32(now), last)
	}
	next := mover
	var frames []Frame
	switch decision {
	case monster.TraceAbandon:
		next.Pose = live
		next.From, next.To = monster.Pose{}, monster.Pose{}
		next.DepartMs, next.ArriveMs = 0, 0
		mustMoverTransition(&next, monster.MoverEventTraceAbandoned, 0)
		next.BehaviorDeadlineMs = now + ops.idleDelayMs()
		frames = []Frame{correctionFrame(instance.Gid, live)}
		var homeFrames []Frame
		next, homeFrames = ops.planIdleEntry(instance, next, now)
		frames = append(frames, homeFrames...)
	case monster.TraceRun, monster.TraceWalk:
		channel, speed := uint8(wire.MoveStateRun), instance.RunSpeed()
		if decision == monster.TraceWalk {
			channel, speed = wire.MoveStateWalk, instance.WalkSpeed()
		}
		if channel != currentChannel(mover.Channel) {
			next.PursuitChannel = channel
			if mover.InFlight(now) {
				goal := mover.MovementGoal()
				if intent, active := mover.NavigationGoal(); active {
					goal = intent
				}
				next, frames = ops.planSegment(instance, next, goal, speed, channel, now)
			} else {
				next.Channel = channel
				refresh := wire.ObjectStateRefresh{Gid: instance.Gid, StateType: wire.StateChannelMove, Value: channel}
				frames = []Frame{{Opcode: wire.OpObjectStateRefresh, Payload: refresh.Encode()}}
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state = s.populationForObject(division, instance.Gid)
	current, exists = state.instances.lookup(instance.Gid)
	alive = exists && current.CurrentHP != 0 && current.Motion.StateAt(now) == 0
	sameSpeeds := current.Ref.RunSpeed == instance.Ref.RunSpeed && current.Ref.WalkSpeed == instance.Ref.WalkSpeed
	// aiTimersLocked may create the timer entry, so it stays last and runs
	// only when everything before it still matches.
	if !alive || !sameActorIdentity(current, instance) || !sameSpeeds ||
		state.movers.get(instance.Gid) != mover || current.Opponents != opponents ||
		*s.aiTimersLocked(state, current, now) != timersBefore {
		return nil, true
	}
	if decision != monster.TraceContinue && !s.commitMoverLocked(state, instance.Gid, next) {
		return nil, true
	}
	*state.aiTimers[instance.Gid] = timers
	return frames, next != mover
}

/*
==================
monsterWithinHomeTrace

545E50: HELP and BATTLE use the retained CNest center, not the actor's
randomized initial spawn or HOMING's current destination. Summoned actors
and explicitly detached actors have no live nest; that branch is strict.
==================
*/
func monsterWithinHomeTrace(instance monster.Instance, live monster.Pose, distance float32) bool {
	controls := instance.Nest.Controls
	if controls.TraceBoundary == 0 || controls.TraceBoundary == 2 || controls.TraceData == 0 {
		return true
	}
	hasNest := !instance.NestDetached && instance.SummonerGID == 0
	home := anchorPose(instance)
	if hasNest && !monster.FollowLocationCompatible(live.RegionID, home.RegionID) {
		return false
	}
	return controls.HelpWithinTrace(distance, tacticsHomeDistance(live, home), float32(instance.Nest.Radius), hasNest)
}

func needsHoming(instance monster.Instance, pose monster.Pose) bool {
	if !instance.Nest.HasControls || instance.Ref.RunSpeed <= 0 {
		return false
	}
	// 545FDB: after CNest removal, the retained home is consulted only
	// when the tactics row enables HomingData. Do not erase that row.
	if instance.NestDetached && instance.Nest.Controls.HomingData <= 0 {
		return false
	}
	anchor := anchorPose(instance)
	// 545F70 calls 430AD0, whose +4 output is FLDZ (430B63/430B80).
	// Reusing combat's 3D delta makes actors above/below a nest return early.
	return !instance.Nest.Controls.InsideHome(tacticsHomeDistance(pose, anchor), float32(instance.Nest.Radius))
}

// Both 545E50 and 545F70 use 430AD0's planar, float32 home displacement.
func tacticsHomeDistance(pose, anchor monster.Pose) float32 {
	x, y, z := monster.NativeTacticsRelative(pose, anchor)
	return float32(math.Sqrt(float64(float32(float64(x)*float64(x) + float64(y)*float64(y) + float64(z)*float64(z)))))
}

// 53D7A0 stores the relative vector and squared length as float32 before
// sqrt, then its caller spills the result. Retain Y and those rounding seams.
func tacticsDistance3D(a, b monster.Pose) float32 {
	return monster.NativeActorDistance(a, b)
}

// sameActorIdentity reports that a re-read actor is still the one the
// decision was staged for: same nest row, attachment and summoner.
func sameActorIdentity(current, staged monster.Instance) bool {
	return current.Nest == staged.Nest && current.NestDetached == staged.NestDetached && current.SummonerGID == staged.SummonerGID
}
