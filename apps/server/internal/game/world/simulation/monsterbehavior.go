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
func (ops *MonsterMoverOps) advanceInstance(divisionID string, instance monster.Instance, divisionPlayers []playerPose, nowMs int64) ([]Frame, []MonsterPrivateFrames) {
	return ops.advanceInstanceSnapshot(divisionID, &instance, nil, divisionPlayers, nowMs)
}

/*
==================
advanceInstanceSnapshot

advanceInstance with the actor's mover already resolved by the leg
dispatch (behaviorActorMover, #2029): the step starts from that value
instead of re-reading it under a second lock. A nil snapshot marks
direct callers outside a leg, which keep the step's own read. The
snapshot is taken at dispatch, the same moment the instance snapshot
beside it is read, so it is never staler than the value the step used
to start from. It arrives by pointer and the step copies it exactly
once: at 632 bytes, the by-value hand-offs on the way in were a
measurable slice of the leg (#2150).
==================
*/
func (ops *MonsterMoverOps) advanceInstanceSnapshot(divisionID string, instance *monster.Instance, snapshot *monster.MoverState, divisionPlayers []playerPose, nowMs int64) ([]Frame, []MonsterPrivateFrames) {
	if instance.Ref.TrainingDummy {
		return nil, nil
	}
	frames, targeted := ops.advanceInstanceStep(divisionID, instance, snapshot, divisionPlayers, nowMs)
	frames = ops.resolveStuck(divisionID, instance.Gid, divisionPlayers, nowMs, frames)
	return frames, targeted
}

func (ops *MonsterMoverOps) advanceInstanceStep(divisionID string, instance *monster.Instance, snapshot *monster.MoverState, divisionPlayers []playerPose, nowMs int64) ([]Frame, []MonsterPrivateFrames) {
	ops.leg.noteStep()
	ops.stepDrainedEvents = 0
	// A fatal B245 keeps the registry row alive briefly so the client can play
	// death, stage reward particles, and then receive the ordinary despawn
	// dissolve. That corpse is presentation-retained, not behavior-live: it
	// must never finish a wander/chase segment, emit a correction, retaliate,
	// or choose another idle leg while CurrentHP is zero.
	if instance.Motion.StateAt(nowMs) != 0 || (instance.Ref.MaxHP > 0 && instance.CurrentHP == 0) {
		return nil, nil
	}
	var mover monster.MoverState
	if snapshot != nil {
		mover = *snapshot
	} else {
		stored, known := ops.Monsters.Mover(divisionID, instance.Gid)
		if !known {
			return nil, nil
		}
		mover = stored
	}
	if ops.detachCrossPlaneNest(divisionID, instance, &mover, nowMs) {
		return nil, nil
	}
	if frames, handled := ops.applyTacticsEvents(divisionID, instance, mover, nowMs); handled {
		return frames, nil
	}
	// Direct actor events (including control binding) may enter IDLE outside
	// this tick. Complete that entry before invoking its first OnTick.
	completedIdleEntry := false
	if mover.IdleEntryPending() {
		completedIdleEntry = true
		frames := ops.commitIdleEntry(divisionID, instance, mover, nil, nowMs)
		// The entry commit advanced the row; the fresh read below is
		// the step's new base, generation included (#2137 slice 1). A
		// planning step reads its plan's shadow instead (the commit
		// is recorded, not yet applied) - stepMoverGeneration.
		var freshGen uint64
		var ok bool
		mover, freshGen, ok = ops.stepMoverGeneration(divisionID, instance.Gid)
		if ok {
			ops.rebaseStepBase(freshGen)
		}
		if !ok || mover.Mode() != monster.MoverIdle || mover.IdleEntryPending() {
			return frames, nil
		}
	}
	tactics := ops.resolveTactics(instance)
	restFailure := restGuardFailure(ops, instance, mover, tactics, nowMs)
	ops.leg.noteRestGuard(restFailure)
	if restFailure == restGuardPass {
		return nil, nil
	}
	// The settled lane's candidate (#3027): every rest guard but the
	// cadence passes on the value this step started from, so the whole
	// step can only re-check the cadence and return. An entry completed
	// above disqualifies the step - the snapshot the verdict would
	// read still showed the entry pending, so the lane would step it.
	restCandidate := restFailure == restGuardCadence && !completedIdleEntry
	drainedEvents := ops.stepDrainedEvents
	if mover.Mode() != monster.MoverPending {
		// The gate hands back the mover it gated (its cadence may have
		// been consumed and committed); re-reading the row here would
		// repeat the locked lookup the gate just made.
		frames, gated, handled, gateAdvanced := ops.runActivityGate(divisionID, instance, mover, nowMs)
		if gated.Mode() == monster.MoverPending {
			ops.leg.noteGateSuspended()
		}
		if handled {
			return frames, nil
		}
		mover = gated
		// The gate's own commit may have advanced the row (a whole
		// write past the cadence stamp): re-base the step on the
		// generation the gated value sits at (#2137 slice 1). A pure
		// cadence stamp leaves the generation, so the chain base the
		// step already carries still names the row and the re-read
		// would only repeat the gate's answer (#2136).
		if gateAdvanced {
			if _, gen, fresh := ops.stepMoverGeneration(divisionID, instance.Gid); fresh {
				ops.rebaseStepBase(gen)
			}
		}
	}
	if frames, handled := ops.handleHelp(divisionID, instance, mover, divisionPlayers, nowMs); handled {
		return frames, nil
	}
	// The native command queue runs queued commands before the current
	// state's tick. PENDING's cadence belongs to its own state, not
	// the pre-command gate.
	if mover.Mode() == monster.MoverPending {
		frames, _, _, _ := ops.runActivityGate(divisionID, instance, mover, nowMs)
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
	// Temptation: a tempted monster, and a monster fighting one, plan
	// against monsters (monstertemptation.go).
	divisionPlayers, tactics = ops.temptationView(divisionID, instance, mover, divisionPlayers, tactics, nowMs)
	if frames, targeted, handled := ops.acquireTemptationFoe(divisionID, instance, tactics, mover, divisionPlayers, nowMs); handled {
		return frames, targeted
	}
	if mover.Mode() == monster.MoverWandering && mover.BehaviorDeadlineMs > 0 && nowMs > mover.BehaviorDeadlineMs {
		// WANDER expiry posts the state-entry event: enter IDLE before
		// scanning.
		// WANDER::OnExit is a no-op; do not cancel the movement channel.
		mustMoverTransition(&mover, monster.MoverEventWanderExpired, 0)
		mover.BehaviorDeadlineMs = nowMs + ops.idleDelayMs()
		return ops.commitIdleEntry(divisionID, instance, mover, nil, nowMs), nil
	}
	if mover.Mode() == monster.MoverReturning && instance.Nest.HasControls {
		// Native HOMING has its own duration and scan-delay gates. It
		// is not an unconditional aggro blackout until arrival.
		elapsed := uint32(nowMs) - mover.HomingStartedMs
		if elapsed > 50000 {
			mover.Pose = mover.LivePoseAt(nowMs, ops.GroundHeight)
			mover.From, mover.To = monster.Pose{}, monster.Pose{}
			mover.DepartMs, mover.ArriveMs = 0, 0
			mustMoverTransition(&mover, monster.MoverEventHomingExpired, 0)
			mover.BehaviorDeadlineMs = nowMs + ops.idleDelayMs()
			return ops.commitIdleEntry(divisionID, instance, mover, []Frame{correctionFrame(instance.Gid, mover.Pose)}, nowMs), nil
		}
		if elapsed > mover.HomingAcquireAfterMs && ops.acquisitionReady(divisionID, instance, nowMs) && tactics.Aggressive {
			from := mover.LivePoseAt(nowMs, ops.GroundHeight)
			if target, found := ops.acquireSightTarget(divisionID, instance, from, divisionPlayers, tactics); found {
				mustMoverTransition(&mover, monster.MoverEventAggroAcquired, target.Gid)
				if plan, planned := ops.selectMonsterAttack(divisionID, instance, 0, from, target); planned {
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
		if mover.Mode() == monster.MoverWandering {
			// One live pose serves the sight scan and the homing
			// test below: the mover is unchanged between them.
			live := mover.LivePoseAt(nowMs, ops.GroundHeight)
			if tactics.Aggressive && ops.acquisitionReady(divisionID, instance, nowMs) {
				if target, ok := ops.acquireSightTarget(divisionID, instance, live, divisionPlayers, tactics); ok {
					mustMoverTransition(&mover, monster.MoverEventAggroAcquired, target.Gid)
					if plan, planned := ops.selectMonsterAttack(divisionID, instance, 0, live, target); planned {
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
			if needsHoming(instance, live) {
				return ops.startReturnLeg(divisionID, instance, tactics, mover, monster.MoverEventHomingRequired, nowMs), nil
			}
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
		if mover.NoteMoveStopped(mover.Pose) {
			return ops.commitSteppedMoverFrames(divisionID, instance.Gid, mover,
				[]Frame{correctionFrame(instance.Gid, mover.Pose)}), nil
		}
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

	frames, targeted := ops.advanceIdle(divisionID, instance, tactics, mover, divisionPlayers, nowMs)
	if restCandidate && frames == nil && targeted == nil {
		// The candidate's whole observable outcome: no frame, no
		// commit door fired (the guards disarm each branch that
		// could reach one) and the leg-end remainder arm re-arms
		// the unchanged row. Counted so the settled lane's stage
		// gate prices this population before any code depends on
		// it (#3027 stage 1).
		ops.leg.noteRestSettled(drainedEvents > 0)
	}
	return frames, targeted
}

/*
================
restGuard

The idle-rest verdict's failing guard (restGuardFailure), named so
the leg record can count which guard refuses each resting step
(#3027 stage 1). The cadence slot names the settled lane's
candidate: an idle monster failing only there is due a cadence
re-check whose step can otherwise only return nil, so a verdict
frozen at snapshot time could settle it without stepping. Every
other slot names a fact the step itself must still read.
================
*/
type restGuard uint8

const (
	restGuardPass restGuard = iota
	restGuardMode
	restGuardCadence
	restGuardHelp
	restGuardSummoned
	restGuardTarget
	restGuardDeadline
	restGuardAggressive
	restGuardGoal
	restGuardSegment

	restGuardCount
)

// restGuardNames names each guard for the slow-leg line's rest census.
var restGuardNames = [restGuardCount]string{
	restGuardPass:       "pass",
	restGuardMode:       "mode",
	restGuardCadence:    "cadence",
	restGuardHelp:       "help",
	restGuardSummoned:   "summoned",
	restGuardTarget:     "target",
	restGuardDeadline:   "deadline",
	restGuardAggressive: "aggressive",
	restGuardGoal:       "goal",
	restGuardSegment:    "segment",
}

/*
================
restGuardFailure

The idle-rest verdict as its failing guard: restGuardPass when an
idle monster at rest can only return nil from the rest of
advanceInstanceStep, so the step may skip its pose, activity-gate
and AI-timer reads until the monster's next due time (#2136 item 3).
Each guard names the branch it disarms and reads only what the
caller already carries - the instance snapshot and the mover
value - so a monster failing any guard runs the full step exactly
as before.
================
*/
func restGuardFailure(ops *MonsterMoverOps, instance *monster.Instance, mover monster.MoverState, tactics monster.Tactics, nowMs int64) restGuard {
	// Mode branches below: pending, battle re-entry, wander expiry,
	// returning, following, recovering, chasing and spawning.
	if mover.Mode() != monster.MoverIdle || mover.IdleEntryPending() {
		return restGuardMode
	}
	// The activity gate's Due test, read without committing the cadence.
	if ops.activity != nil && uint32(nowMs)-mover.Activity.LastCheck > uint32(mover.Activity.Interval) {
		return restGuardCadence
	}
	// A pending help event or queued command is handled below.
	if instance.Help.HasPending() {
		return restGuardHelp
	}
	// A summoned monster at rest looks for its summoner's target.
	if mover.ControlMode() == monster.ControlSummoned {
		return restGuardSummoned
	}
	// Temptation rewrites the plan; a held target is fought below.
	if instance.Tempted() || mover.TargetGID() != 0 {
		return restGuardTarget
	}
	// advanceIdle decides only once the idle deadline passes.
	if nowMs > mover.BehaviorDeadlineMs {
		return restGuardDeadline
	}
	// An aggressive monster's sight scan reads the AI timer, and the
	// check consumes selection state: the scan cadence (a few hundred
	// milliseconds) cannot be deferred to the idle deadline (seconds).
	if tactics.Aggressive {
		return restGuardAggressive
	}
	// A held navigation goal commits its next segment below, even with
	// no segment open.
	if _, active := mover.NavigationGoal(); active {
		return restGuardGoal
	}
	// A live or matured segment still has to glide or settle.
	if mover.From != (monster.Pose{}) || mover.To != (monster.Pose{}) || mover.ArriveMs > mover.DepartMs || mover.InFlight(nowMs) {
		return restGuardSegment
	}
	return restGuardPass
}

/*
================
idleRestQuiescent

True when the idle-rest verdict passes every guard
(restGuardFailure); the step then returns at once.
================
*/
func idleRestQuiescent(ops *MonsterMoverOps, instance *monster.Instance, mover monster.MoverState, tactics monster.Tactics, nowMs int64) bool {
	return restGuardFailure(ops, instance, mover, tactics, nowMs) == restGuardPass
}

/*
================
restFacts

Every input the settled verdict reads, projected off the stored
rows by the rest-facts door (MonsterState.restFactsAt) instead of
the 2.7 KB snapshot expansion: the mover facts the guards read, the
resident facts behind them (motion, help, hit points, the nest the
detach test and the tactics projection read, the abnormal block's
confusion face behind Tempted), whether tactics events are queued,
and the row identity (slots, mover generation) a later commit
guards on. Plain values only; the door is the only writer.
================
*/
type restFacts struct {
	slot        uint32
	monsterSlot uint32
	moverGen    uint64

	mode               monster.MoverMode
	idleEntryPending   bool
	activity           monster.ActivityCadence
	controlMode        monster.ControlMode
	targetGID          uint32
	behaviorDeadlineMs int64
	navGoal            bool
	segFrom            monster.Pose
	segTo              monster.Pose
	segDepartMs        int64
	segArriveMs        int64
	liveRegion         uint16

	motion       monster.MotionHold
	help         monster.HelpInbox
	currentHP    uint32
	refMaxHP     uint32
	nestDetached bool
	nestRegionID uint16
	aggressive   bool
	tempted      bool

	eventsPending bool
}

/*
================
restGuardFailureFromFacts

restGuardFailure over the door's facts: the same guards in the same
order, each reading the projection of the fact the expanded read
reads - mode and entry from the mover row, cadence from its stamp,
help from the resident inbox, the target guard from the held target
or the abnormal block's confusion (Tempted, the one hotFill-joined
fact a guard touches), the aggressive guard from the nest
projection ResolveTactics derives it from. The equivalence test
(monsteridle_rest_door_test.go) pins this verdict against the
expanded read's over seeded states, per input.
================
*/
func restGuardFailureFromFacts(facts restFacts, activityWired bool, nowMs int64) restGuard {
	if facts.mode != monster.MoverIdle || facts.idleEntryPending {
		return restGuardMode
	}
	// The activity gate's Due test, read without committing the cadence.
	if activityWired && uint32(nowMs)-facts.activity.LastCheck > uint32(facts.activity.Interval) {
		return restGuardCadence
	}
	if facts.help.HasPending() {
		return restGuardHelp
	}
	if facts.controlMode == monster.ControlSummoned {
		return restGuardSummoned
	}
	if facts.tempted || facts.targetGID != 0 {
		return restGuardTarget
	}
	if nowMs > facts.behaviorDeadlineMs {
		return restGuardDeadline
	}
	if facts.aggressive {
		return restGuardAggressive
	}
	if facts.navGoal {
		return restGuardGoal
	}
	if facts.segFrom != (monster.Pose{}) || facts.segTo != (monster.Pose{}) || facts.segArriveMs > facts.segDepartMs ||
		facts.segArriveMs > facts.segDepartMs && nowMs < facts.segArriveMs {
		return restGuardSegment
	}
	return restGuardPass
}

/*
================
restSettledFromFacts

The settled verdict at snapshot-build time (#3027, design A): the
actor is a rest candidate (every guard but the cadence passes, the
cadence due) and the step stages before and beside the verdict
cannot act either - the corpse/motion guard stands down, the
cross-plane detach is a no-op on its face, and no tactics event is
queued for the step to drain. Such an actor's whole step returns
nil and commits nothing, so a lane may skip its snapshot fill and
its step and let the leg-end remainder arm re-arm the untouched
row. Any doubt answers false: the full step runs exactly as today.
================
*/
func restSettledFromFacts(facts restFacts, activityWired bool, nowMs int64) bool {
	if restGuardFailureFromFacts(facts, activityWired, nowMs) != restGuardCadence {
		return false
	}
	// The step's head guard: a corpse or a held motion returns there.
	if facts.motion.StateAt(nowMs) != 0 || (facts.refMaxHP > 0 && facts.currentHP == 0) {
		return false
	}
	// detachCrossPlaneNest on its face: already detached, or the
	// indoor bits agree, so its locked branch can never fire.
	if !facts.nestDetached && (facts.nestRegionID^facts.liveRegion)&0x8000 != 0 {
		return false
	}
	// The step drains the tactics queue before the verdict; a queued
	// event must reach that drain, never be skipped over.
	return !facts.eventsPending
}

func (ops *MonsterMoverOps) advanceIdle(divisionID string, instance *monster.Instance, tactics monster.Tactics, mover monster.MoverState, divisionPlayers []playerPose, nowMs int64) ([]Frame, []MonsterPrivateFrames) {
	// The native idle tick checks state expiry before the acquisition
	// callback.
	if nowMs > mover.BehaviorDeadlineMs {
		return ops.decideIdle(divisionID, instance, tactics, mover, nowMs), nil
	}
	// Timer 1 owns scan cadence inside the unexpired state.
	// In particular an empty scan consumes the gate; entering sight does not
	// re-arm it. Damage retaliation above is independent of this sight gate.
	if tactics.Aggressive && ops.acquisitionReady(divisionID, instance, nowMs) {
		from := mover.LivePoseAt(nowMs, ops.GroundHeight)
		if target, ok := ops.acquireSightTarget(divisionID, instance, from, divisionPlayers, tactics); ok {
			mustMoverTransition(&mover, monster.MoverEventAggroAcquired, target.Gid)
			if plan, planned := ops.selectMonsterAttack(divisionID, instance, 0, from, target); planned {
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

func (ops *MonsterMoverOps) decideIdle(divisionID string, instance *monster.Instance, tactics monster.Tactics, mover monster.MoverState, nowMs int64) []Frame {
	policy := monster.RetailBehaviorPolicy()
	if policy.RepeatIdle(ops.rand()) {
		mustMoverTransition(&mover, monster.MoverEventIdleRepeated, 0)
		mover.BehaviorDeadlineMs = nowMs + ops.idleDelayMs()
		return ops.commitIdleEntry(divisionID, instance, mover, nil, nowMs)
	}
	return ops.startWanderLeg(divisionID, instance, tactics, mover, nowMs)
}

// aiEventStart is the tactics event the abnormal callbacks post when a
// status starts (0x14).
const aiEventStart = 0x14

/*
==================
applyTacticsEvents

applyTacticsEvents runs the fear/confusion events queued by the abnormal
callbacks (the tactics current state's event handler). BATTLE
abandons a feared caster that is its current target, and any
player target under confusion; other states only record the event.
The end event needs no port action: the next scan resumes.
==================
*/
func (ops *MonsterMoverOps) applyTacticsEvents(divisionID string, instance *monster.Instance, mover monster.MoverState, nowMs int64) ([]Frame, bool) {
	events := ops.takeStepAIEvents(divisionID, instance.Gid)
	ops.stepDrainedEvents = len(events)
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
