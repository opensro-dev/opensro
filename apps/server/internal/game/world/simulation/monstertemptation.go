/*
===========================================================================

monstertemptation.go - a tempted monster fights the monsters around it

The Bard's Temptation lands Confusion (abnormal slot 16) on a regular
monster (monster.Instance.Tempted). While it lasts the monster's planner
sees the live monsters of its population where it would see players, so
the ordinary chase and attack lifecycle (startChaseLeg, tryMonsterAttack,
the B245 action bracket) runs against a monster target unchanged. A
monster struck by a tempted one sees that one monster as well, so its
retaliation can reach it. When the status ends neither view holds the
other monster any longer: the target is lost and both return to normal.

The action owner resolves the hit itself (action/temptation.go).

Owner's rule (the authority for this behaviour): a tempted monster attacks
other monsters nearby for the duration instead of players, and only
regular monsters and regular party monsters are affected.

===========================================================================
*/

package simulation

import "opensro.online/server/internal/game/world/monster"

const (
	// temptationFallbackSight is the foe search radius of a tempted monster
	// whose tactics author no sight range (a passive, unmatched nest).
	// Inferred: "attack other Monsters nearby" names no distance; 100 is
	// the radius of the Bard's own monster-selecting area (Discord Wave's
	// efr). A nest that authors a sight range uses its own.
	temptationFallbackSight = 100
)

/*
================
temptationView

The planner's candidate set for one monster. A tempted monster sees only
the other live monsters around it (and keeps seeing the one it already
fights, wherever it ran), and acquires them as an aggressive monster
would. A monster whose target is a tempted monster sees that monster in
addition to the players. Any other monster keeps the players unchanged.
================
*/
func (ops *MonsterMoverOps) temptationView(divisionID string, instance monster.Instance, mover monster.MoverState, players []playerPose, tactics monster.Tactics, nowMs int64) ([]playerPose, monster.Tactics) {
	if instance.Tempted() {
		// Inferred: the temptation makes every regular monster seek a
		// foe, passive ones included, as the description reads.
		tactics.Aggressive = true
		if tactics.SightRange <= 0 {
			tactics.SightRange = temptationFallbackSight
		}
		live := mover.LivePoseAt(nowMs, nil)
		return ops.Monsters.temptationFoes(divisionID, instance.Gid, live, mover.TargetGID(), tactics.SightRange, nowMs), tactics
	}
	target := mover.TargetGID()
	if target == 0 {
		return players, tactics
	}
	foe, ok := ops.Monsters.temptedFoe(divisionID, instance.Gid, target, nowMs)
	if !ok {
		return players, tactics
	}
	view := make([]playerPose, 0, len(players)+1)
	view = append(view, players...)
	return append(view, foe), tactics
}

/*
================
acquireTemptationFoe

A tempted monster that holds no target picks the nearest foe in sight on
every planner step, from the states the ordinary sight scan acquires in.
Inferred: the tempted monster does not wait for the acquisition timer;
the status lasts seconds and the ordinary scan cadence would only delay
the visible effect. A monster that already fights keeps its target; one
still holding a player was sent home by the confusion start event
(applyTacticsEvents) and acquires from HOMING here.
================
*/
func (ops *MonsterMoverOps) acquireTemptationFoe(divisionID string, instance monster.Instance, tactics monster.Tactics, mover monster.MoverState, foes []playerPose, nowMs int64) ([]Frame, []MonsterPrivateFrames, bool) {
	if !instance.Tempted() || mover.TargetGID() != 0 || len(foes) == 0 {
		return nil, nil, false
	}
	switch mover.Mode() {
	case monster.MoverIdle, monster.MoverWandering, monster.MoverReturning, monster.MoverFollowing:
	default:
		return nil, nil, false
	}
	live := mover.LivePoseAt(nowMs, ops.TerrainHeight)
	foe, found := nearestTemptationFoe(live, foes, tactics.SightRange)
	if !found {
		return nil, nil, false
	}
	mustMoverTransition(&mover, monster.MoverEventAggroAcquired, foe.Gid)
	if plan, planned := ops.selectMonsterAttack(divisionID, instance, 0); planned {
		ops.adoptMonsterAttack(&mover, plan)
		if frames, targeted, handled := ops.tryMonsterAttack(divisionID, instance, tactics, mover, foes, nowMs); handled {
			return frames, targeted, true
		}
	}
	return ops.startChaseLeg(divisionID, instance, tactics, mover, foe, nowMs), nil, true
}

/*
================
nearestTemptationFoe
================
*/
func nearestTemptationFoe(from monster.Pose, foes []playerPose, sight float64) (playerPose, bool) {
	var best playerPose
	bestDistance := sight
	found := false
	for _, foe := range foes {
		if d := planarDistanceSpawn(foe.Pose, poseToSpawn(from)); d <= bestDistance {
			best, bestDistance, found = foe, d, true
		}
	}
	return best, found
}

/*
================
temptationFoes

The live monsters of gid's population within sight of from, nearest
first, as planner candidates; the monster it currently fights joins them
wherever it is. A foe is its live pose and body radius; it has no player
movement intent, so the chase treats its live pose as settled.
================
*/
func (s *MonsterState) temptationFoes(division string, gid uint32, from monster.Pose, current uint32, sight float64, nowMs int64) []playerPose {
	lease, ok := s.ObjectPopulation(division, gid)
	if !ok {
		return nil
	}
	var foes []playerPose
	for _, candidate := range s.CombatCandidatesInPopulation(division, lease, poseToSpawn(from), sight, nowMs, true) {
		if candidate.Gid == gid || candidate.Gid == current {
			continue
		}
		if foe, live := s.monsterFoe(division, candidate.Gid, nowMs); live {
			foes = append(foes, foe)
		}
	}
	if current == 0 {
		return foes
	}
	if other, same := s.ObjectPopulation(division, current); !same || other != lease {
		return foes
	}
	if foe, live := s.monsterFoe(division, current, nowMs); live {
		foes = append(foes, foe)
	}
	return foes
}

/*
================
temptedFoe

The tempted monster target of gid as a planner candidate, when both live
in the same population.
================
*/
func (s *MonsterState) temptedFoe(division string, gid, target uint32, nowMs int64) (playerPose, bool) {
	instance, ok := s.Get(division, target)
	if !ok || !instance.Tempted() {
		return playerPose{}, false
	}
	lease, ok := s.ObjectPopulation(division, gid)
	if other, same := s.ObjectPopulation(division, target); !ok || !same || other != lease {
		return playerPose{}, false
	}
	return s.monsterFoe(division, target, nowMs)
}

/*
================
monsterFoe
================
*/
func (s *MonsterState) monsterFoe(division string, gid uint32, nowMs int64) (playerPose, bool) {
	instance, ok := s.Get(division, gid)
	if !ok || instance.CurrentHP == 0 {
		return playerPose{}, false
	}
	mover, ok := s.Mover(division, gid)
	if !ok {
		return playerPose{}, false
	}
	return playerPose{
		Gid:        gid,
		Pose:       poseToSpawn(mover.LivePoseAt(nowMs, nil)),
		BodyRadius: BodyRadius(instance.BodyRadius()),
	}, true
}
