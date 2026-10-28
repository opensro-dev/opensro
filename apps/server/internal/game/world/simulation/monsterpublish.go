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
func (ops *MonsterMoverOps) advanceAndPublish(divisionID string, instance monster.Instance, players []playerPose, nowMs int64, sessions []SessionSnapshot, push Pusher) {
	// Part C times the advance itself, so the direct dispatch below and
	// the RunAction closure path accumulate into the same figure.
	if ops.leg != nil {
		started := time.Now()
		defer func() { ops.leg.noteAdvance(time.Since(started)) }()
	}
	frames, targeted := ops.advanceInstance(divisionID, instance, players, nowMs)
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
