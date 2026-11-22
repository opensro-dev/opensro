/*
===========================================================================

monsterpublish.go - one advanced monster's publication to its viewers

advanceAndPublish is the single exit of the mover step: the public
frames go to the division's sessions that have been shown the monster,
the private consequences follow in the same operation, and the
safe-zone vanish is weighed last.

===========================================================================
*/

package simulation

import (
	"slices"
	"time"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
advanceAndPublish
================
*/
func (ops *MonsterMoverOps) advanceAndPublish(divisionID string, actor *behaviorActorSnapshot, players []playerPose, nowMs int64, sessions []SessionSnapshot, push Pusher) {
	// Part C times the advance itself, so the direct dispatch below and
	// the RunAction closure path accumulate into the same figure.
	if ops.leg != nil {
		started := time.Now()
		defer func() { ops.leg.noteAdvance(time.Since(started)) }()
	}
	// The actor is the leg view's own entry (#2982), read in place:
	// the step never writes through it.
	instance := &actor.instance
	// The step's whole-value commits guard on the generation this
	// actor was snapshot at (#2137 slice 1): a slower lane's set
	// since the snapshot drops them instead of being overwritten.
	ops.stepBaseGen, ops.stepGenArmed = actor.moverGen, true
	ops.stepSlot, ops.stepMonsterSlot, ops.stepQueueSlot = actor.slot, actor.monsterSlot, actor.queueSlot
	frames, targeted := ops.advanceInstanceSnapshot(divisionID, instance, &actor.mover, players, nowMs)
	ops.stepGenArmed = false
	// A step that emitted nothing has no viewer to find. Most steps emit
	// nothing, and the walk below is one pass over the division's
	// sessions per stepped actor: with it unguarded the leg paid actors
	// times sessions every tick (2.8 ms of a 61 ms tick at 105 players,
	// live profile 2026-11-10). By index, because a SessionSnapshot is a
	// large value and the range copy was most of that cost.
	if len(frames) > 0 {
		viewers := ops.divisionSessions(divisionID, sessions)
		for i := range viewers {
			sessionID := viewers[i].SessionID
			if ops.scopeShown(sessionID)[instance.Gid] {
				push.PushToSession(sessionID, monsterFramesForViewer(frames, &viewers[i]))
			}
		}
	}
	// Private consequences follow the public result in the same operation.
	deliverMonsterTargetFrames(divisionID, targeted, sessions, push)
	ops.vanishInSafeZone(divisionID, actor, nowMs)
}

/*
================
monsterFramesForViewer

The frames of one monster's publication that one of its viewers may
take. Every frame reaches every viewer of the monster except a 0xB4B5
ride state: a ride state names its rider, and the client applies it to
that rider with no absent-actor arm (client 0x777F60: the registry
find at 0x777FB2 flows untested into CICharactor_SetMountedActionState
at 0x777FC3), so it may reach only a viewer that holds the rider - a
session whose published set names it, or the rider's own session,
which owns the mount. A slain mounted pet's dismount therefore leaves
with the pet's death frames for the monster's viewers who hold the
owner, and never reaches the rest (#4112). A ride state whose payload
does not decode stays with the shared frames, as the action lane's
frameSourceGID fallback keeps it under the caller's source. With no
decodable ride state in the batch - the common step - the frames pass
through untouched.
================
*/
func monsterFramesForViewer(frames []Frame, viewer *SessionSnapshot) []Frame {
	hasRide := false
	for i := range frames {
		if frames[i].Opcode != wire.OpCosRideState {
			continue
		}
		if _, ok := wire.CosRideStateRider(frames[i].Payload); ok {
			hasRide = true
			break
		}
	}
	if !hasRide {
		return frames
	}
	out := make([]Frame, 0, len(frames))
	for i := range frames {
		if frames[i].Opcode == wire.OpCosRideState && !viewerHoldsRide(viewer, frames[i]) {
			continue
		}
		out = append(out, frames[i])
	}
	return out
}

/*
================
viewerHoldsRide

Whether this session may take one ride state: it holds the rider (its
published set names the rider gid), or it is the rider (it owns the
mount the frame names). An undecodable payload stays shared, as in
monsterFramesForViewer above.
================
*/
func viewerHoldsRide(viewer *SessionSnapshot, frame Frame) bool {
	rider, ok := wire.CosRideStateRider(frame.Payload)
	if !ok {
		return true
	}
	if _, found := slices.BinarySearch(viewer.PublishedObjects, rider); found {
		return true
	}
	mount, ok := wire.CosRideStateMount(frame.Payload)
	return ok && viewer.ownsCompanion(mount)
}

/*
================
commitSteppedMover

The running step's whole-value commit of its own actor: guarded by
the step's base generation while advanceAndPublish has it armed, so
a slower lane's set since the snapshot (or since the step's last
accepted commit) drops the commit instead of being overwritten -
the actor replans at its next due. The commit reads and answers
through the slots the snapshot carried, as commitSteppedActivity's
does (#3027). Outside a step the plain commit of before runs
unchanged. An accepted commit re-bases the step on the row's new
generation, so the step's own commits chain.
================
*/
func (ops *MonsterMoverOps) commitSteppedMover(divisionID string, gid uint32, mover monster.MoverState) bool {
	if ops.plan != nil {
		// Planning (#2205): record the commit for the ordered commit
		// and answer as if it landed; a replay that drops drops the
		// whole plan, so the optimism is never observable.
		ops.plan.recordMover(mover)
		return true
	}
	if !ops.stepGenArmed {
		return ops.Monsters.CommitMover(divisionID, gid, mover)
	}
	gen, ok := ops.Monsters.CommitMoverGenAt(divisionID, gid, ops.stepSlot, ops.stepMonsterSlot, mover, ops.stepBaseGen)
	if ok {
		ops.stepBaseGen = gen
	}
	return ok
}

/*
================
commitSteppedMoverFrames

commitSteppedMover for frame-carrying commits: the frames release
only when the commit landed on its base generation.
================
*/
func (ops *MonsterMoverOps) commitSteppedMoverFrames(divisionID string, gid uint32, mover monster.MoverState, frames []Frame) []Frame {
	if ops.plan != nil {
		// Planning (#2205): as commitSteppedMover - the frames ride
		// the plan's output and publish only if the replay lands.
		ops.plan.recordMover(mover)
		return frames
	}
	if !ops.stepGenArmed {
		return ops.Monsters.CommitMoverFrames(divisionID, gid, mover, frames)
	}
	return ops.Monsters.CommitMoverFramesGenAt(divisionID, gid, ops.stepSlot, ops.stepMonsterSlot, mover, ops.stepBaseGen, frames)
}

/*
================
commitSteppedActivity

The activity gate's commit routed through the step's funnel
(monsteractivity.go's runActivityGate): applied at once outside a
plan, recorded for the ordered commit inside one (#2205). The
second answer is the write's cadence verdict (commitActivityAt's,
or the pair's own comparison on the plan path, which records
without writing): the gate reads its generation rule from it
instead of comparing the rows again (#2029).
================
*/
func (ops *MonsterMoverOps) commitSteppedActivity(division string, gid uint32, before, after monster.MoverState, now int64) (bool, bool) {
	if ops.plan != nil {
		ops.plan.recordActivity(before, after)
		// A plan records the pair's own verdict; the ordered commit
		// replays through commitActivityAt directly, so counting
		// here - and only here - counts each arm once (#3027).
		cadenceOnly := cadenceOnlyWrite(before, after)
		ops.leg.noteCadenceArm(cadenceOnly)
		return true, cadenceOnly
	}
	// The step's own actor is armed: commit through its carried
	// slots (#2133) and the base generation its snapshot read
	// (#3027). The doors verify each hint against the gid, so
	// this is the gid commit with the map resolves left out.
	if ops.stepGenArmed {
		committed, cadenceOnly := ops.Monsters.commitActivityAt(division, gid, ops.stepSlot, ops.stepMonsterSlot, ops.stepQueueSlot, ops.stepBaseGen, before, after, now)
		ops.leg.noteCadenceArm(committed && cadenceOnly)
		return committed, cadenceOnly
	}
	committed, cadenceOnly := ops.Monsters.commitActivityAt(division, gid, behaviorNoMoverSlot, behaviorNoMonsterSlot, behaviorNoQueueSlot, behaviorNoBaseGen, before, after, now)
	ops.leg.noteCadenceArm(committed && cadenceOnly)
	return committed, cadenceOnly
}

/*
================
commitSteppedFollow

The follow transaction's commit routed through the step's funnel
(monstersummon_behavior.go): applied at once outside a plan,
recorded for the ordered commit inside one (#2205). The
transaction's own validation - instance, mover, timers, leader
facts - is the replay's revalidation, and a rejection there drops
the whole plan like any stale commit.
================
*/
func (ops *MonsterMoverOps) commitSteppedFollow(follow monsterFollowPlan, mover monster.MoverState, frames []Frame) ([]Frame, bool) {
	if ops.plan != nil {
		// Planning (#2205): record the transaction for the ordered
		// commit and answer as if it landed; a replay that drops
		// drops the whole plan, so the optimism is never observable.
		ops.plan.recordFollow(follow, mover, frames)
		return frames, true
	}
	return ops.Monsters.commitFollow(follow, mover, frames)
}

/*
================
takeStepAIEvents

The step's tactics-event drain (monsterbehavior.go's
applyTacticsEvents): a real drain outside a plan. Inside one the
drain is a consumption, so the step only reads the queue and
records it - the events leave the queue at commit, and a dropped
plan leaves them queued for the actor's next due. A second take
in the same step sees the drain, exactly as outside a plan.
================
*/
func (ops *MonsterMoverOps) takeStepAIEvents(divisionID string, gid uint32) []monsterAIEvent {
	// The step's own actor is armed: resolve the population through
	// its carried monster slot (#2133). The doors verify the hint
	// against the gid, so another actor still resolves by gid.
	monsterSlot := behaviorNoMonsterSlot
	if ops.stepGenArmed {
		monsterSlot = ops.stepMonsterSlot
	}
	if ops.plan == nil {
		return ops.Monsters.takeAIEventsAt(divisionID, gid, monsterSlot)
	}
	if ops.plan.aiEventsTaken {
		return nil
	}
	ops.plan.aiEventsTaken = true
	ops.plan.aiEvents = ops.Monsters.peekAIEventsAt(divisionID, gid, monsterSlot)
	return ops.plan.aiEvents
}

/*
================
stepMoverGeneration

The running step's re-read of its own actor's row (monsterbehavior.go
re-reads after the idle-entry and gate commits): the live row outside
a plan; inside one, the plan's shadow - the value the recorded
commits will leave - because the row itself still holds the
leg-start value until the ordered commit. The generation answered
with the shadow is the snapshot's: only the value is load-bearing,
the commit chains off the row's real generation at replay.
================
*/
func (ops *MonsterMoverOps) stepMoverGeneration(divisionID string, gid uint32) (monster.MoverState, uint64, bool) {
	if ops.plan != nil && ops.plan.shadowed {
		return ops.plan.shadow, ops.plan.actor.moverGen, true
	}
	if ops.stepGenArmed {
		return ops.Monsters.MoverGenerationAt(divisionID, gid, ops.stepSlot, ops.stepMonsterSlot)
	}
	return ops.Monsters.MoverGeneration(divisionID, gid)
}

/*
================
rebaseStepBase

Chain the running step's later generation-guarded commits off a
generation it just re-read: the armed assignment outside a plan, a
recorded marker inside one (#2205) that the commit honours by
adopting the row's generation at the same point in the replay.
================
*/
func (ops *MonsterMoverOps) rebaseStepBase(gen uint64) {
	if ops.plan != nil {
		ops.plan.recordRebase()
		return
	}
	if ops.stepGenArmed {
		ops.stepBaseGen = gen
	}
}

/*
================
divisionSessions

divisionSessions is the session list one division's publications
iterate: the running leg's pre-grouped index while a leg runs (the
publish path runs once per actor and must not rescan every session),
or the caller's slice filtered on the fly for direct calls outside a
leg.
================
*/
func (ops *MonsterMoverOps) divisionSessions(divisionID string, sessions []SessionSnapshot) []SessionSnapshot {
	if ops.legSessions != nil {
		return ops.legSessions[divisionID]
	}
	filtered := make([]SessionSnapshot, 0, len(sessions))
	for _, session := range sessions {
		if session.DivisionID == divisionID {
			filtered = append(filtered, session)
		}
	}
	return filtered
}
