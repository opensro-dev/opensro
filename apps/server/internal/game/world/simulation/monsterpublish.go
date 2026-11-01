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
	"time"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
advanceAndPublish
================
*/
func (ops *MonsterMoverOps) advanceAndPublish(divisionID string, actor behaviorActorSnapshot, players []playerPose, nowMs int64, sessions []SessionSnapshot, push Pusher) {
	// Part C times the advance itself, so the direct dispatch below and
	// the RunAction closure path accumulate into the same figure.
	if ops.leg != nil {
		started := time.Now()
		defer func() { ops.leg.noteAdvance(time.Since(started)) }()
	}
	instance := actor.instance
	// The step's whole-value commits guard on the generation this
	// actor was snapshot at (#2137 slice 1): a slower lane's set
	// since the snapshot drops them instead of being overwritten.
	ops.stepBaseGen, ops.stepGenArmed = actor.moverGen, true
	frames, targeted := ops.advanceInstanceSnapshot(divisionID, instance, actor.mover, true, players, nowMs)
	ops.stepGenArmed = false
	for _, session := range ops.divisionSessions(divisionID, sessions) {
		if len(frames) > 0 && ops.shownMonsters[session.SessionID][instance.Gid] {
			push.PushToSession(session.SessionID, frames)
		}
	}
	// Private consequences follow the public result in the same operation.
	deliverMonsterTargetFrames(divisionID, targeted, sessions, push)
	ops.vanishInSafeZone(divisionID, instance.Gid, nowMs)
}

/*
================
commitSteppedMover

The running step's whole-value commit of its own actor: guarded by
the step's base generation while advanceAndPublish has it armed, so
a slower lane's set since the snapshot (or since the step's last
accepted commit) drops the commit instead of being overwritten -
the actor replans at its next due. Outside a step the plain commit
of before runs unchanged. An accepted commit re-bases the step on
the row's new generation, so the step's own commits chain.
================
*/
func (ops *MonsterMoverOps) commitSteppedMover(divisionID string, gid uint32, mover monster.MoverState) bool {
	if !ops.stepGenArmed {
		return ops.Monsters.CommitMover(divisionID, gid, mover)
	}
	gen, ok := ops.Monsters.CommitMoverGen(divisionID, gid, mover, ops.stepBaseGen)
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
	if !ops.stepGenArmed {
		return ops.Monsters.CommitMoverFrames(divisionID, gid, mover, frames)
	}
	return ops.Monsters.CommitMoverFramesGen(divisionID, gid, mover, ops.stepBaseGen, frames)
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
