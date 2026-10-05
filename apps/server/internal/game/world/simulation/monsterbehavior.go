/*
===========================================================================

monsterbehavior.go - one monster's planner step

===========================================================================
*/

package simulation

import "opensro.online/server/internal/game/world/monster"

/*
==================
advanceInstance

advanceInstance runs one monster's planner step and returns the frames
it emitted. All registry access is value-copy in / whole-value commit
out (the CloneWorldState discipline).
==================
*/
func (ops *MonsterMoverOps) advanceInstance(divisionID string, instance monster.Instance, divisionPlayers []playerPose, nowMs int64) ([]Frame, *monsterTargetFrames) {
	// A fatal B245 keeps the registry row alive briefly so the client can play
	// death, stage reward particles, and then receive the ordinary despawn
	// dissolve. That corpse is presentation-retained, not behavior-live: it
	// must never finish a wander/chase segment, emit a correction, retaliate,
	// or choose another idle leg while CurrentHP is zero.
	if instance.Motion.StateAt(nowMs) != 0 || (instance.Ref.MaxHP > 0 && instance.CurrentHP == 0) {
		return nil, nil
	}
	mover, ok := ops.Monsters.Mover(divisionID, instance.Gid)
	if !ok {
		return nil, nil
	}
	if ops.detachCrossPlaneNest(divisionID, instance, mover, nowMs) {
		return nil, nil
	}
	if frames, handled := ops.applyTacticsEvents(divisionID, instance, mover, nowMs); handled {
		return frames, nil
	}
	// Direct actor events (including control binding) may enter IDLE outside
	// this tick. Complete that entry before invoking its first OnTick.
	if mover.IdleEntryPending() {
		frames := ops.commitIdleEntry(divisionID, instance, mover, nil, nowMs)
		mover, ok = ops.Monsters.Mover(divisionID, instance.Gid)
		if !ok || mover.Mode() != monster.MoverIdle || mover.IdleEntryPending() {
			return frames, nil
		}
	}
	if mover.Mode() != monster.MoverPending {
		if frames, handled := ops.runActivityGate(divisionID, instance, mover, nowMs); handled {
			return frames, nil
		}
	}
	// The activity gate may have consumed its independent clock while leaving
	// the state active. Continue from that accepted owner snapshot.
	mover, ok = ops.Monsters.Mover(divisionID, instance.Gid)
	if !ok {
		return nil, nil
	}
	if frames, handled := ops.handleHelp(divisionID, instance, mover, divisionPlayers, nowMs); handled {
		return frames, nil
	}
	// 53FEA0 runs queued commands before the current state's OnTick.
	// PENDING's cadence belongs to 55AF60, not the pre-command 540D20 gate.
	if mover.Mode() == monster.MoverPending {
		frames, _ := ops.runActivityGate(divisionID, instance, mover, nowMs)
		return frames, nil
	}
	if mover.Mode() == monster.MoverBattleReentry {
		mustMoverTransition(&mover, monster.MoverEventTraceAbandoned, 0)
		mover.BehaviorDeadlineMs = nowMs + ops.idleDelayMs()
		return ops.commitIdleEntry(divisionID, instance, mover, nil, nowMs), nil
	}
	// The summon action retains movement ownership through recovery.
	if nowMs < instance.SummonActionUntilMs {
		return nil, nil
	}
	tactics := ops.resolveTactics(instance)
	// Temptation: a tempted monster, and a monster fighting one, plan
	// against monsters (monstertemptation.go).
	divisionPlayers, tactics = ops.temptationView(divisionID, instance, mover, divisionPlayers, tactics, nowMs)
	if frames, targeted, handled := ops.acquireTemptationFoe(divisionID, instance, tactics, mover, divisionPlayers, nowMs); handled {
		return frames, targeted
	}
	if mover.Mode() == monster.MoverWandering && mover.BehaviorDeadlineMs > 0 && nowMs > mover.BehaviorDeadlineMs {
		// 559EB0 -> event 37 -> 5599A0: enter IDLE before scanning.
		// WANDER::OnExit is a no-op; do not cancel the movement channel.
		mustMoverTransition(&mover, monster.MoverEventWanderExpired, 0)
		mover.BehaviorDeadlineMs = nowMs + ops.idleDelayMs()
		return ops.commitIdleEntry(divisionID, instance, mover, nil, nowMs), nil
	}
	if mover.Mode() == monster.MoverReturning && instance.Nest.HasControls {
		// Native HOMING has its own duration and scan-delay gates (55A690,
		// 55A720). It is not an unconditional aggro blackout until arrival.
		elapsed := uint32(nowMs) - mover.HomingStartedMs
		if elapsed > 50000 {
			mover.Pose = mover.LivePoseAt(nowMs, ops.TerrainHeight)
			mover.From, mover.To = monster.Pose{}, monster.Pose{}
			mover.DepartMs, mover.ArriveMs = 0, 0
			mustMoverTransition(&mover, monster.MoverEventHomingExpired, 0)
			mover.BehaviorDeadlineMs = nowMs + ops.idleDelayMs()
			return ops.commitIdleEntry(divisionID, instance, mover, []Frame{correctionFrame(instance.Gid, mover.Pose)}, nowMs), nil
		}
		if elapsed > mover.HomingAcquireAfterMs && ops.acquisitionReady(divisionID, instance, nowMs) && tactics.Aggressive {
			if target, found := nearestEligiblePlayer(instance, mover.LivePoseAt(nowMs, ops.TerrainHeight), divisionPlayers, tactics.SightRange); found {
				mustMoverTransition(&mover, monster.MoverEventAggroAcquired, target.Gid)
				if plan, planned := ops.selectMonsterAttack(divisionID, instance, 0); planned {
					ops.adoptMonsterAttack(&mover, plan)
					if frames, targeted, handled := ops.tryMonsterAttack(divisionID, instance, tactics, mover, divisionPlayers, nowMs); handled {
						return frames, targeted
					}
				}
				return ops.startChaseLeg(divisionID, instance, tactics, mover, target, nowMs), nil
			}
		}
	}
	if mover.Mode() == monster.MoverFollowing {
		if frames, handled := ops.stopOrAdvanceFollow(divisionID, instance, mover, nowMs); handled {
			return frames, nil
		}
	}
	atRest := mover.Mode() == monster.MoverWandering || mover.Mode() == monster.MoverIdle && nowMs <= mover.BehaviorDeadlineMs
	if mover.ControlMode() == monster.ControlSummoned && atRest {
		if target, found := ops.summonedTarget(divisionID, instance, mover, divisionPlayers, nowMs); found {
			mustMoverTransition(&mover, monster.MoverEventAggroAcquired, target.Gid)
		} else if frames, following := ops.followSummoner(divisionID, instance, mover, nowMs); following {
			return frames, nil
		}
	}
	if tactics == (monster.Tactics{}) {
		// Only an unmatched zero-speed row has no behavior contract. Keep it
		// stationary without touching mover state.
		return nil, nil
	}
	if mover.Mode() == monster.MoverRecovering {
		if nowMs < mover.BehaviorDeadlineMs {
			return nil, nil
		}
		return ops.startReturnLeg(
			divisionID, instance, tactics, mover, monster.MoverEventRecoveryElapsed, nowMs,
		), nil
	}
	if frames, targeted, handled := ops.tryMonsterAttack(
		divisionID, instance, tactics, mover, divisionPlayers, nowMs,
	); handled {
		return frames, targeted
	}
	if navigationNeedsContinuation(mover, nowMs) && mover.Mode() != monster.MoverFollowing {
		goal, _ := mover.NavigationGoal()
		speed, channel := mover.NavigationMotion()
		return ops.commitSegment(divisionID, instance, mover, goal, speed, channel, nowMs), nil
	}

	// In flight: the client path-follows the 0xB738 goal with its OWN
	// integrator - the server emits NOTHING while a segment matures
	// except chase corrections (BUG-7 fix, board seq593/594: the old
	// per-tick 0x30E3 LivePoseAt re-seed was a second position source
	// fighting the client's integrator - the rubber-band. REV seq116 #3:
	// glides are OPTIONAL once the goal is set; the arrival 0xB2F5
	// settles any residual drift).
	if mover.InFlight(nowMs) {
		if mover.Mode() == monster.MoverIdle {
			return ops.advanceIdle(divisionID, instance, tactics, mover, divisionPlayers, nowMs)
		}
		if mover.Mode() == monster.MoverWandering && tactics.Aggressive && ops.acquisitionReady(divisionID, instance, nowMs) {
			live := mover.LivePoseAt(nowMs, ops.TerrainHeight)
			if target, ok := nearestEligiblePlayer(instance, live, divisionPlayers, tactics.SightRange); ok {
				mustMoverTransition(&mover, monster.MoverEventAggroAcquired, target.Gid)
				if plan, planned := ops.selectMonsterAttack(divisionID, instance, 0); planned {
					ops.adoptMonsterAttack(&mover, plan)
					if frames, targeted, handled := ops.tryMonsterAttack(
						divisionID, instance, tactics, mover, divisionPlayers, nowMs,
					); handled {
						return frames, targeted
					}
				}
				return ops.startChaseLeg(divisionID, instance, tactics, mover, target, nowMs), nil
			}
		}
		if mover.Mode() == monster.MoverWandering && needsHoming(instance, mover.LivePoseAt(nowMs, ops.TerrainHeight)) {
			return ops.startReturnLeg(divisionID, instance, tactics, mover, monster.MoverEventHomingRequired, nowMs), nil
		}
		return ops.chaseRetarget(divisionID, instance, tactics, mover, divisionPlayers, nowMs), nil
	}

	// Segment matured: settle exactly once (0xB2F5 at the goal, facing
	// the travel heading - clearing the segment in the same commit is
	// what makes the settle single-shot), then the mover idles there.
	if mover.ArriveMs > mover.DepartMs {
		wasIdle := mover.Mode() == monster.MoverIdle
		mover.Pose = mover.To
		mover.From, mover.To = monster.Pose{}, monster.Pose{}
		mover.DepartMs, mover.ArriveMs = 0, 0
		mustMoverTransition(&mover, monster.MoverEventSegmentArrived, 0)
		if !wasIdle {
			mover.BehaviorDeadlineMs = nowMs + ops.idleDelayMs()
		}
		return ops.commitIdleEntry(divisionID, instance, mover,
			[]Frame{correctionFrame(instance.Gid, mover.Pose)}, nowMs), nil
	}

	// An owned retaliation/aggro target takes priority over passive sight
	// acquisition. Reacquiring here used to overwrite the target tuple after
	// every matured chase leg, which made retaliation ownership ambiguous.
	if mover.Mode() == monster.MoverChasing {
		// Passive monsters arrive here after an authoritative player hit arms
		// retaliation. Continue that explicit target independently of the
		// tactics.Aggressive sight-acquisition gate.
		if target, alive := eligiblePlayerByGid(instance, divisionPlayers, mover.TargetGID()); alive {
			return ops.startChaseLeg(divisionID, instance, tactics, mover, target, nowMs), nil
		}
		// Target gone (left division while we were idle): go home.
		return ops.startReturnLeg(divisionID, instance, tactics, mover, monster.MoverEventTargetLost, nowMs), nil
	}

	// SPAWN owns a real server-side hold. It is visible and visually standing,
	// but ordinary sight aggro and idle decisions are not eligible yet. A
	// damage transaction may still have interrupted it through the explicit
	// retaliation edge handled above.
	if mover.Mode() == monster.MoverSpawning {
		if nowMs < mover.BehaviorDeadlineMs {
			return nil, nil
		}
		mustMoverTransition(&mover, monster.MoverEventSpawnHoldElapsed, 0)
		mover.BehaviorDeadlineMs = nowMs + ops.idleDelayMs()
		return ops.commitIdleEntry(divisionID, instance, mover, nil, nowMs), nil
	}

	return ops.advanceIdle(divisionID, instance, tactics, mover, divisionPlayers, nowMs)
}

func (ops *MonsterMoverOps) advanceIdle(divisionID string, instance monster.Instance, tactics monster.Tactics, mover monster.MoverState, divisionPlayers []playerPose, nowMs int64) ([]Frame, *monsterTargetFrames) {
	// 55A8B0 checks state expiry before the acquisition callback.
	if nowMs > mover.BehaviorDeadlineMs {
		return ops.decideIdle(divisionID, instance, tactics, mover, nowMs), nil
	}
	// Timer 1 owns scan cadence inside the unexpired state.
	// In particular an empty scan consumes the gate; entering sight does not
	// re-arm it. Damage retaliation above is independent of this sight gate.
	if tactics.Aggressive && ops.acquisitionReady(divisionID, instance, nowMs) {
		if target, ok := nearestEligiblePlayer(instance, mover.LivePoseAt(nowMs, ops.TerrainHeight), divisionPlayers, tactics.SightRange); ok {
			mustMoverTransition(&mover, monster.MoverEventAggroAcquired, target.Gid)
			if plan, planned := ops.selectMonsterAttack(divisionID, instance, 0); planned {
				ops.adoptMonsterAttack(&mover, plan)
				if frames, targeted, handled := ops.tryMonsterAttack(
					divisionID, instance, tactics, mover, divisionPlayers, nowMs,
				); handled {
					return frames, targeted
				}
			}
			return ops.startChaseLeg(divisionID, instance, tactics, mover, target, nowMs), nil
		}
	}
	return nil, nil
}

func (ops *MonsterMoverOps) decideIdle(divisionID string, instance monster.Instance, tactics monster.Tactics, mover monster.MoverState, nowMs int64) []Frame {
	policy := monster.RetailBehaviorPolicy()
	if policy.RepeatIdle(ops.rand()) {
		mustMoverTransition(&mover, monster.MoverEventIdleRepeated, 0)
		mover.BehaviorDeadlineMs = nowMs + ops.idleDelayMs()
		return ops.commitIdleEntry(divisionID, instance, mover, nil, nowMs)
	}
	return ops.startWanderLeg(divisionID, instance, tactics, mover, nowMs)
}

// aiEventStart is the tactics event the abnormal callbacks post when a
// status starts (4A4BD0 / 4A4F70: 0x14).
const aiEventStart = 0x14

/*
==================
applyTacticsEvents

applyTacticsEvents runs the fear/confusion events queued by the abnormal
callbacks (4A4BD0 / 4A4F70 -> CAITactics current state +80). BATTLE
(55A390) abandons a feared caster that is its current target, and any
player target under confusion; other states only record the event.
The end event (+84, 559760) needs no port action: the next scan resumes.
==================
*/
func (ops *MonsterMoverOps) applyTacticsEvents(divisionID string, instance monster.Instance, mover monster.MoverState, nowMs int64) ([]Frame, bool) {
	events := ops.Monsters.takeAIEvents(divisionID, instance.Gid)
	battle := mover.Mode() == monster.MoverChasing || mover.Mode() == monster.MoverAttacking
	for _, event := range events {
		if event.Event != aiEventStart || !battle || mover.TargetGID() == 0 {
			continue
		}
		if event.Kind == 9 && mover.TargetGID() != event.Source {
			continue
		}
		return ops.startReturnLeg(divisionID, instance, ops.resolveTactics(instance), mover, monster.MoverEventTargetLost, nowMs), true
	}
	return nil, false
}
