/*
===========================================================================

monsterchase.go - monster chase pacing

===========================================================================
*/

package simulation

import (
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

// referenceChaseRefreshMinMs is the machine-confirmed pacing gate in the
// research server's AutoCommand target-range approach routine
// (0x004b04bf..0x004b04e6). The production simulation owner runs at the same
// 100 ms cadence; this deadline never creates work outside that owner.
const referenceChaseRefreshMinMs int64 = 100

/*
==================
chaseRetarget

chaseRetarget keeps an in-flight chase honest. Leash breach or target loss
flips to the return leg. A target command/state change refreshes immediately;
an in-flight target also receives a bounded live-position refresh before the
client's current pursuit segment expires. Non-chase segments emit nothing.
==================
*/
func (ops *MonsterMoverOps) chaseRetarget(
	divisionID string,
	instance monster.Instance,
	tactics monster.Tactics,
	mover monster.MoverState,
	divisionPlayers []playerPose,
	nowMs int64,
) []Frame {
	if mover.Mode() != monster.MoverChasing {
		return nil
	}
	// Y is irrelevant here: live feeds planarDistance (XZ) only, and either
	// successor leg resolves terrain again. Avoid a per-tick terrain sample.
	live := mover.LivePoseAt(nowMs, nil)
	target, targetAlive := eligiblePlayerByGid(instance, divisionPlayers, mover.TargetGID())
	if !targetAlive {
		return ops.startReturnLeg(divisionID, instance, tactics, mover, monster.MoverEventTargetLost, nowMs)
	}
	if tactics.ChaseLeash > 0 && planarDistance(live, anchorPose(instance)) > tactics.ChaseLeash {
		return ops.startReturnLeg(divisionID, instance, tactics, mover, monster.MoverEventLeashBroken, nowMs)
	}

	// The live pose owns geometry; guidance owns command identity and whether
	// the target is still moving. The reference AutoCommand routine evaluates this
	// lane at a 100 ms minimum cadence and refreshes a live-target pursuit goal
	// while out of range. Suppressing those refreshes lets the client arrive at
	// each short goal and visibly stop until the next server tick—the whole
	// entity, including its nameplate, pulses rather than merely its animation.
	targetGuidance := target.chaseGuidance()
	lastGuidance, hasGuidance := mover.ChaseGuidance()
	guidanceChanged := !hasGuidance ||
		targetGuidance.TargetMoving() != lastGuidance.TargetMoving() ||
		planarDistanceSpawn(
			poseToSpawn(targetGuidance.Destination()),
			poseToSpawn(lastGuidance.Destination()),
		) > 10
	refreshDue := targetGuidance.TargetMoving() &&
		nowMs-mover.DepartMs >= referenceChaseRefreshMinMs
	shouldReaim := mover.RetaliationPending() || guidanceChanged || refreshDue
	if !shouldReaim {
		return nil
	}
	return ops.startChaseLeg(divisionID, instance, tactics, mover, target, nowMs)
}

/*
==================
startChaseLeg

startChaseLeg authors ordinary monster pursuit from the live target pose.
The moving-target branch uses the native center distance, not AutoCommand's inner ring.
Target guidance and the derived destination remain separate state by design.
==================
*/
func (ops *MonsterMoverOps) startChaseLeg(
	divisionID string,
	instance monster.Instance,
	tactics monster.Tactics,
	mover monster.MoverState,
	target playerPose,
	nowMs int64,
) []Frame {
	// Legs travel at the effective parameters 17/18 (4AA410), which
	// frostbite and slow scale; capability gates keep the authored speeds.
	speed := instance.RunSpeed()
	channel := wire.MoveStateRun
	if mover.PursuitChannel == wire.MoveStateWalk {
		speed, channel = instance.WalkSpeed(), wire.MoveStateWalk
	}
	if speed <= 0 {
		speed = instance.WalkSpeed()
		channel = wire.MoveStateWalk
	}
	if speed <= 0 {
		return ops.startReturnLeg(divisionID, instance, tactics, mover, monster.MoverEventAttackUnavailable, nowMs)
	}

	live := mover.LivePoseAt(nowMs, ops.TerrainHeight)
	mover.Pose = live
	mustMoverTransition(&mover, monster.MoverEventChaseStarted, target.Gid)
	// ChaseGuidance is target movement state, not spatial approach geometry. A
	// command destination may be far beyond—or already opposite—the live body.
	// The target's sampled pose remains the only legal geometry input.
	targetGuidance := target.chaseGuidance()
	spacing := CombatSpacing{
		ActorBodyRadius:  BodyRadius(instance.BodyRadius()),
		TargetBodyRadius: target.BodyRadius,
		ActionReach:      ActionReach(mover.AttackReach),
	}
	goal, disposition := spacing.ApproachGoal(poseToSpawn(live), target.Pose)
	switch disposition {
	case CombatApproachInvalid:
		return ops.startReturnLeg(divisionID, instance, tactics, mover, monster.MoverEventAttackUnavailable, nowMs)
	case CombatApproachHold:
		return nil
	}

	// 548CE2..548D75: travel actor-to-target center distance along the
	// approach direction while the target moves. AutoCommand's two-unit inset
	// is insufficient when a running target advances five units per tick.
	// Outdoor only: dungeon actors run 549460, which has no such branch.
	if targetGuidance.TargetMoving() && ordinarySquadApproach(instance) && !IsDungeonRegion(live.RegionID) {
		goal = NormalizeSpawnFrame(target.Pose)
	}
	before := ops.Monsters.prepareApproachNavigation(divisionID, instance, mover, target, nowMs)
	if before.approach != nil {
		goal = squadApproachGoal(live, target, spacing, before.approach.slot)
	}

	if err := mover.SetChaseGuidance(targetGuidance); err != nil {
		panic(err)
	}
	destination := monster.Pose{
		RegionID: goal.RegionID,
		X:        goal.X,
		Y:        goal.Y,
		Z:        goal.Z,
		Heading:  goal.Angle,
	}

	// POLICY: chase uses the authored run channel so the 0x3122 state and
	// 0xB738 segment make the client and server integrate at the same speed.
	return ops.commitPreparedSegment(divisionID, before, instance, mover, destination, speed, channel, nowMs)
}
