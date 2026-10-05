/*
===========================================================================

monsterattack.go - handoff from pursuit to the native attack lifecycle

The movement owner admits facing and action state before damage can commit.
This module selects attacks, classifies refusals, and preserves the recovery
deadline; action remains the sole owner of damage and reward mutations.

===========================================================================
*/
package simulation

import "opensro.online/server/internal/game/world/monster"

/*
================
MonsterAttackPlan
================
*/
type MonsterAttackPlan struct {
	SelfEffect        bool
	Summon            bool
	SkillID           uint32
	Reach             ActionReach
	CooldownMs        int64
	ActionLifecycleMs int64
}

/*
================
AttackPick

What a new choice among a monster's default skills reads: the uniform
sample its CRT draw is taken from, and the target it chooses against.
561B00 weighs a skill up by how far its reach exceeds the target's
distance, so a choice without a target weighs the authored weights alone.
================
*/
type AttackPick struct {
	Sample float64
	Target *AttackTarget
}

/*
================
AttackTarget

CAITactics_DistanceBetweenActors (53D7A0) from the monster to the target,
and the target's body radius (its vtable +0x560).
================
*/
type AttackTarget struct {
	Distance   float32
	BodyRadius float64
}

/*
================
attackPickFor

The pick against target, measured from the monster's live pose.
================
*/
func attackPickFor(sample float64, from monster.Pose, target playerPose) AttackPick {
	to := monster.Pose{RegionID: target.Pose.RegionID, X: target.Pose.X, Y: target.Pose.Y, Z: target.Pose.Z}
	return AttackPick{Sample: sample, Target: &AttackTarget{Distance: monster.NativeActorDistance(from, to), BodyRadius: float64(target.BodyRadius)}}
}

/*
================
MonsterAttackResult
================
*/
type MonsterAttackResult struct {
	Frames []Frame
	// Private holds each struck player's own consequences: a struck
	// companion's go to its owner, and an area attack carries one per victim.
	Private     []MonsterPrivateFrames
	Accepted    bool
	TargetAlive bool
	Refusal     MonsterAttackRefusal
}

/*
================
MonsterPrivateFrames

The frames one character's sessions alone receive from a monster action,
named by id (the tick's sessions) and by name (the action owner's push).
================
*/
type MonsterPrivateFrames struct {
	CharacterID   int64
	CharacterName string
	Frames        []Frame
}

// Refusal is meaningful only for an unaccepted action. Unclassified failures
// remain fail-closed; data/ownership errors must never become retry permission.
/*
================
MonsterAttackRefusal
================
*/
type MonsterAttackRefusal uint8

const (
	MonsterAttackUnavailable MonsterAttackRefusal = iota
	MonsterAttackApproachRequired
	MonsterAttackCommandRejected
)

/*
================
MonsterAttackOperation
================
*/
type MonsterAttackOperation func(divisionID string, instance monster.Instance, targetGid, skillID uint32, nowMs int64) MonsterAttackResult

/*
================
selectMonsterAttack
================
*/
func (ops *MonsterMoverOps) selectMonsterAttack(divisionID string, instance monster.Instance, requestedSkillID uint32, from monster.Pose, target playerPose) (MonsterAttackPlan, bool) {
	if ops.AttackPlan == nil {
		return MonsterAttackPlan{}, false
	}
	// Research 5472CB..5472FE reuses the active skill before invoking the
	// selector. Resolving a retained authored ID must not consume a choice
	// draw; its interval draw was already consumed on adoption.
	if requestedSkillID != 0 {
		return ops.AttackPlan(instance, requestedSkillID, AttackPick{})
	}
	if ops.Monsters != nil {
		if skill, selected := ops.Monsters.SelectConditionalSkill(divisionID, instance.Gid); selected {
			return ops.AttackPlan(instance, skill, AttackPick{})
		}
	}
	return ops.AttackPlan(instance, requestedSkillID, attackPickFor(ops.rand(), from, target))
}

/*
==================
tryMonsterAttack

tryMonsterAttack owns the chase->attack transition and the repeating
attack state. It interrupts an in-flight chase at the sampled live pose,
emits one settle/facing correction when either changed, and never lets the
movement planner and the action planner own the same monster simultaneously.
==================
*/
func (ops *MonsterMoverOps) tryMonsterAttack(
	divisionID string,
	instance monster.Instance,
	tactics monster.Tactics,
	mover monster.MoverState,
	players []playerPose,
	nowMs int64,
) ([]Frame, []MonsterPrivateFrames, bool) {
	if mover.TargetGID() == 0 ||
		(mover.Mode() != monster.MoverChasing && mover.Mode() != monster.MoverAttacking) {
		return nil, nil, false
	}
	target, alive := eligiblePlayerByGid(instance, players, mover.TargetGID())
	if !alive {
		return ops.startReturnLeg(divisionID, instance, tactics, mover, monster.MoverEventTargetLost, nowMs), nil, true
	}
	live := mover.LivePoseAt(nowMs, ops.TerrainHeight)
	// Every native timer query services the selected action timer first.
	// Selection must gate both pursuit and fresh skill selection, including
	// a cast whose duration exceeds the ordinary selector interval.
	if !ops.Monsters.selectedAITimerReady(divisionID, instance.Gid, nowMs) {
		return nil, nil, true
	}
	if frames, handled := ops.advancePursuitControls(divisionID, instance, mover, target, live, nowMs); handled {
		return frames, nil, true
	}
	if tactics.ChaseLeash > 0 && planarDistance(live, anchorPose(instance)) > tactics.ChaseLeash {
		return ops.startReturnLeg(divisionID, instance, tactics, mover, monster.MoverEventLeashBroken, nowMs), nil, true
	}
	// 5472A0 returns 2 while Timer 5 is closed; 55A1D5 chases only on 1.
	// Retain this port's existing accepted-action deadline before range-driven
	// replanning. This does not change skill timing or interval selection.
	// Target loss and eligibility are still checked above.
	if mover.Mode() == monster.MoverAttacking && nowMs < mover.NextAttackMs {
		return nil, nil, true
	}
	// Choose a damage-triggered wave at the next action boundary, from the
	// current HP band, not from a stale plan cached during another cast.
	if monster.SummonDue(instance) && nowMs < mover.NextAttackMs {
		return nil, nil, true
	}
	plan, planned := ops.selectMonsterAttack(divisionID, instance, mover.AttackSkillID, live, target)
	if !planned || (!plan.Summon && ((!plan.SelfEffect && plan.Reach <= 0) || plan.Reach < 0 || plan.ActionLifecycleMs <= 0)) || plan.CooldownMs <= 0 {
		if mover.Retaliating() || mover.Mode() == monster.MoverAttacking {
			// A malformed/missing authored action may never strand a
			// damage-owned target in zero-length chase replans. Fail closed on
			// combat, release aggro, and resume the wander lifecycle.
			return ops.startReturnLeg(divisionID, instance, tactics, mover, monster.MoverEventAttackUnavailable, nowMs), nil, true
		}
		return nil, nil, false
	}
	ops.adoptMonsterAttack(&mover, plan)
	spacing := CombatSpacing{
		ActorBodyRadius:  BodyRadius(instance.BodyRadius()),
		TargetBodyRadius: target.BodyRadius,
		ActionReach:      plan.Reach,
	}
	if !plan.Summon && !spacing.Valid() {
		return ops.startReturnLeg(divisionID, instance, tactics, mover, monster.MoverEventAttackUnavailable, nowMs), nil, true
	}
	if !plan.Summon && !spacing.Contains(poseToSpawn(live), target.Pose) {
		if mover.Mode() == monster.MoverAttacking || !mover.InFlight(nowMs) {
			return ops.startChaseLeg(divisionID, instance, tactics, mover, target, nowMs), nil, true
		}
		return nil, nil, false
	}

	hadSegment := mover.ArriveMs > mover.DepartMs
	facing := live
	// Retail's action steering resolves the live target position, derives a
	// horizontal direction, converts it through Math_DirVecToYaw and commits
	// CIObject_SetYaw before the action starts (client 8E0440, flags&8).
	// The server owns the equivalent authoritative transition: B245 describes
	// the action, but it does not carry a replacement heading.
	if heading, ok := HeadingFromMovement(poseToSpawn(live), target.Pose); ok {
		facing.Heading = heading
	}
	var frames []Frame
	if hadSegment || facing.Heading != live.Heading {
		frames = append(frames, correctionFrame(instance.Gid, facing))
	}
	mover.Pose = facing
	mover.From, mover.To = monster.Pose{}, monster.Pose{}
	mover.DepartMs, mover.ArriveMs = 0, 0
	mustMoverTransition(&mover, monster.MoverEventAttackStarted, target.Gid)
	if nowMs < mover.NextAttackMs || ops.BasicAttack == nil {
		return ops.Monsters.CommitMoverFrames(divisionID, instance.Gid, mover, frames), nil, true
	}
	// Admit facing/action ownership BEFORE the callback can commit damage.
	// Reserve the selected strategy interval, including its native jitter,
	// in that same value (547342 uses strategy +48, not raw skill cooldown).
	// Retaliation must retain it if post-cast bookkeeping is superseded.
	mover.NextAttackMs = nowMs + int64(mover.AttackIntervalMs)
	if !ops.Monsters.CommitMover(divisionID, instance.Gid, mover) {
		return nil, nil, true
	}
	result := ops.BasicAttack(divisionID, instance, target.Gid, plan.SkillID, nowMs)
	if result.Accepted {
		mover.LastBattleActivityMs = uint32(nowMs)
	}
	frames = append(frames, result.Frames...)
	targeted := result.Private
	// A refusal often returns the zero result, whose TargetAlive is false.
	// Only an accepted action can own fatal-result animation recovery.
	if !result.Accepted {
		switch result.Refusal {
		case MonsterAttackApproachRequired, MonsterAttackCommandRejected:
			// Research 5472A0 admission refusal retains the selected skill;
			// 59ADF1 -> event 4(-1) -> 558F70 completes/clears it instead.
			// Neither owns a HOMING transition. Recheck the live target and
			// range on the next AI tick, without recursively retrying damage.
			event := monster.MoverEventAttackApproachRequired
			if result.Refusal == MonsterAttackCommandRejected {
				event = monster.MoverEventSkillCommandRejected
			}
			mustMoverTransition(&mover, event, target.Gid)
			ops.Monsters.CommitMover(divisionID, instance.Gid, mover)
			return frames, targeted, true
		}
		return append(frames, ops.startReturnLeg(
			divisionID, instance, tactics, mover, monster.MoverEventAttackRefused, nowMs,
		)...), targeted, true
	}
	if !result.TargetAlive {
		// The fatal B245 owns the monster until its authored action finishes.
		// Returning in this same burst lets movement overwrite attack facing and
		// produces a hit while the monster is already walking away.
		mover.BehaviorDeadlineMs = nowMs + plan.ActionLifecycleMs
		mustMoverTransition(&mover, monster.MoverEventTargetDefeated, 0)
		ops.Monsters.CommitMover(divisionID, instance.Gid, mover)
		return frames, targeted, true
	}
	mover.NextAttackMs = nowMs + int64(mover.AttackIntervalMs)
	// Select the next authored default action on the next due tick.
	mover.AttackSkillID = 0
	ops.Monsters.CommitMover(divisionID, instance.Gid, mover)
	return frames, targeted, true
}
