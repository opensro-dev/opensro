/*
===========================================================================

creditedhit.go - damage a player's standing effect deals a monster

A linked pulse, the attacking hawk and a returned hit are not casts: no
admission, cost or range test, only damage committed through the monster
HP door with the player credited. A fatal hit settles rewards inside the
roster's character doors; a surviving one mutates only the monster (its
abnormal application reads the character door, so holding that door's
write lock would deadlock).

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
creditedMonsterHit

The committed impacts and, for a kill, the settlement. False when nothing
was committed.
================
*/
type creditedMonsterHit struct {
	impacts    []simulation.MonsterDamageResult
	settlement monsterSettlement
}

/*
================
commitCreditedMonsterHit

Plans formula on target for row and commits it. reason names the roster
door transaction a kill runs in.
================
*/
func (rt *Runtime) commitCreditedMonsterHit(division string, c, snapshot *enterworld.Character, row enterworld.SkillRow, target monster.Instance, formula combat.Result, reason string, now int64) (creditedMonsterHit, bool) {
	var hit creditedMonsterHit
	plans, ok := rt.planMonsterImpacts(division, snapshot, row, target, []combat.Result{formula}, now)
	if !ok {
		return hit, false
	}
	roster := rt.monsterRewardRoster(division, c, now)
	commit := func() bool {
		hit.impacts = rt.Monsters.ApplyDamageSequence(division, target.Gid, target.CurrentHP, plans)
		if len(hit.impacts) == 0 {
			return false
		}
		if hit.impacts[0].Fatal {
			pose := monster.Pose{}
			if mover, exists := rt.Monsters.Mover(division, target.Gid); exists {
				pose = mover.LivePoseAt(now, nil)
			}
			hit.settlement = rt.settleMonsterInsideDoor(division, c, roster, hit.impacts[0], pose, now)
		}
		return true
	}
	committed := false
	if plans[0].Damage >= target.CurrentHP {
		committed = rt.deps.UpdateMany(roster.characters, reason, commit)
	} else {
		committed = commit()
	}
	if !committed {
		return hit, false
	}
	rt.commitSkillHostility(division, enterworld.ObjectIDForCharacter(c), target.Gid, row, hit.impacts, now)
	return hit, true
}

/*
================
creditedHitResult

The hit's public frames after its own result frames: the monster's death,
its drops and the settlement's public frames, then the private progression
and the other recipients' frames.
================
*/
func (rt *Runtime) creditedHitResult(division string, target monster.Instance, hit creditedMonsterHit, public []wire.Frame, now int64) OpResult {
	if hit.impacts[0].Fatal {
		public = append(public, monsterLifeDeadFrame(target.Gid))
		public = append(public, rt.groundReferences(hit.settlement.drops)...)
		for _, drop := range hit.settlement.drops {
			public = append(public, wire.DropBroadcastFrames(drop.SpawnRow(true))...)
		}
		public = append(public, hit.settlement.public...)
		rt.queueMonsterDefeat(division, target.Gid, now+monsterDeathPresentationRetention.Milliseconds())
	}
	return OpResult{Broadcast: public, ActorPrivate: wire.ProgressionPrivateFrames(hit.settlement.actorFrames), Recipients: hit.settlement.others}
}

/*
================
queueMonsterLegRecipients

A kill committed on the monster leg (Temptation fight, returned hit) has
no actor session to answer; its party recipients wait for the action tick.
================
*/
func (rt *Runtime) queueMonsterLegRecipients(division string, recipients []RecipientFrames) {
	if len(recipients) == 0 {
		return
	}
	rt.monsterLegRecipientsMu.Lock()
	rt.monsterLegRecipients = append(rt.monsterLegRecipients, recipientDivisionFrames(division, recipients)...)
	rt.monsterLegRecipientsMu.Unlock()
}

/*
================
drainMonsterLegRecipients
================
*/
func (rt *Runtime) drainMonsterLegRecipients() []simulation.DivisionFrames {
	rt.monsterLegRecipientsMu.Lock()
	defer rt.monsterLegRecipientsMu.Unlock()
	out := rt.monsterLegRecipients
	rt.monsterLegRecipients = nil
	return out
}
