/*
===========================================================================

damagereturn.go - dmgr: a player striking back at whoever damages it

CSkillManager_ProcessDamageEffects (5A0C2D) runs on the defender's skill
manager for every hit SkillCombat_CalculateHitOutcome resolves. Its rule
is the learned passive's (+0x204, the Warrior's two-hand return) while
that passive is enabled, else the live buff's (+0x208, the Warlock's Soul
Return). Each damaging hit of an attacker in range rolls the rule's chance
under the defender's key 0x46000000; the returns of one attack sum into
one area-effect context (+0x70) that 593D62 queues on the attacker
(CSkillManager_QueueAreaEffect 59EC50), so the attacker takes them after
the hit, as one credited hit of the defender.

The defender half (rule, roll, range, amount) is attacker-neutral; the
attacker half commits on a monster (returnDamageToMonster) or a player
(returnDamageToPlayer, pvpstrike.go's credited hit).

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
damageReturnRule

The defender's dmgr and the row that owns it (the hostility row of the
returned hit). The passive wins while enabled (5A0C2D 5A0C51..5A0C5E: +0x204
when its +0x10 is 1, else +0x208). +0x208 is the instance most recently
installed; inferred: the latest live start stands in for that slot.
================
*/
func (rt *Runtime) damageReturnRule(division string, c *enterworld.Character, defender combat.Stats, now int64) (enterworld.SkillDamageReturn, enterworld.SkillRow, bool) {
	skills := rt.deps.SkillData()
	if skills == nil {
		return enterworld.SkillDamageReturn{}, enterworld.SkillRow{}, false
	}
	if defender.DamageReturn.Present {
		row, ok := skills.SkillByID(defender.DamageReturnSkill)
		return defender.DamageReturn, row, ok
	}
	if rt.effects == nil {
		return enterworld.SkillDamageReturn{}, enterworld.SkillRow{}, false
	}
	var best enterworld.SkillRow
	var started int64
	found := false
	for _, effect := range rt.effects.Snapshot(division, c.Name) {
		if effect.StopRequested || effect.DurationPresent && now >= effect.ExpiresAtMs {
			continue
		}
		row, ok := skills.SkillByID(effect.SkillID)
		if !ok || !row.TimedEffect.Pinned || !row.TimedEffect.DamageReturn.Present {
			continue
		}
		if !found || effect.StartedAtMs >= started {
			best, started, found = row, effect.StartedAtMs, true
		}
	}
	return best.TimedEffect.DamageReturn, best, found
}

/*
================
rollDamageReturn

The sum the attacker takes back for hits, or zero. Each damaging hit of an
attacker within range rolls once (CSkillManager_RollProbability with the
rule's chance) and returns its two lane shares.
================
*/
func (rt *Runtime) rollDamageReturn(division string, c *enterworld.Character, rule enterworld.SkillDamageReturn, distance float64, hits []combat.Result) (uint32, error) {
	if !combat.DamageReturnInRange(rule, distance) {
		return 0, nil
	}
	var total uint32
	for _, hit := range hits {
		if !combat.DamageReturnApplies(rule, hit) {
			continue
		}
		proc, err := rt.effectOutcome(criticalActor{division: division, character: c.Name}, combat.DamageReturnRollKey, rule.Chance)
		if err != nil {
			return 0, err
		}
		if proc {
			total += combat.ReturnedDamage(rule, hit)
		}
	}
	return total, nil
}

/*
================
returnDamageToMonster

The monster branch: after attacker struck c with hits, c's dmgr returns its
share to the attacker as c's credited hit. The frames are the attack's own
publication: a pulse result (B3C6 mode 2, the B0BC 59B220 sends for queued
area effects) with c as its source and the attack's skill (context +0xC),
then the kill. Inferred: the queued context is published as the linked
pulses' are, since both are tagAreaEffectContext records the attacker's
manager drains.
================
*/
func (rt *Runtime) returnDamageToMonster(division string, c *enterworld.Character, attacker monster.Instance, attackerPose monster.Pose, attackSkill uint32, defender combat.Stats, hits []combat.Result, now int64) OpResult {
	snapshot := rt.characterSnapshot(division, c)
	if snapshot == nil {
		return OpResult{}
	}
	rule, row, ok := rt.damageReturnRule(division, snapshot, defender, now)
	if !ok {
		return OpResult{}
	}
	at := rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)
	from := monster.Pose{RegionID: at.RegionID, X: at.X, Y: at.Y, Z: at.Z}
	damage, err := rt.rollDamageReturn(division, snapshot, rule, float64(monster.NativeActorDistance(from, attackerPose)), hits)
	if err != nil || damage == 0 {
		return OpResult{}
	}
	live, exists := rt.Monsters.Get(division, attacker.Gid)
	if !exists || live.CurrentHP == 0 {
		return OpResult{}
	}
	formula := combat.Result{Damage: min(damage, wire.MaxSkillActionDamage), ResultFlags: 1}
	hit, ok := rt.commitCreditedMonsterHit(division, c, snapshot, row, live, formula, "damage-return-kill", now)
	if !ok {
		return OpResult{}
	}
	source := enterworld.ObjectIDForCharacter(snapshot)
	public := []wire.Frame{wire.SkillPulseFrame(source, attackSkill, []wire.SkillAreaTarget{
		{GID: live.Gid, Impacts: []wire.SkillCastTargetImpact{committedSkillImpact(formula, hit.impacts[0])}},
	})}
	return rt.creditedHitResult(division, live, hit, public, now)
}

/*
================
returnDamageToPlayer

The player branch: c's dmgr returns its share to the attacking player as
c's credited hit, published as the monster branch's is. INFERENCE: the
queued context (+0x70) is not a hit outcome, so the attacker's own wall
does not split it.
================
*/
func (rt *Runtime) returnDamageToPlayer(division string, c, attacker *enterworld.Character, attackSkill uint32, defender combat.Stats, hits []combat.Result, now int64) OpResult {
	snapshot := rt.characterSnapshot(division, c)
	if snapshot == nil || attacker == nil {
		return OpResult{}
	}
	rule, row, ok := rt.damageReturnRule(division, snapshot, defender, now)
	if !ok {
		return OpResult{}
	}
	target, ok := rt.resolveCombatTarget(division, snapshot, enterworld.ObjectIDForCharacter(attacker), now)
	if !ok || target.player == nil {
		return OpResult{}
	}
	at := rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)
	from := monster.Pose{RegionID: at.RegionID, X: at.X, Y: at.Y, Z: at.Z}
	to := monster.Pose{RegionID: target.at.RegionID, X: target.at.X, Y: target.at.Y, Z: target.at.Z}
	damage, err := rt.rollDamageReturn(division, snapshot, rule, float64(monster.NativeActorDistance(from, to)), hits)
	if err != nil || damage == 0 {
		return OpResult{}
	}
	stats, _, err := rt.playerCombatStats(division, snapshot)
	if err != nil {
		return OpResult{}
	}
	formula := combat.Result{Damage: min(damage, wire.MaxSkillActionDamage), ResultFlags: 1}
	hit, result, landed := rt.creditPlayerHit(playerHitInput{division: division, caster: c, snapshot: snapshot,
		attacker: stats, skill: row, target: target, impacts: 1, fixed: &formula, now: now})
	if !landed {
		return OpResult{}
	}
	pulse := wire.SkillPulseFrame(enterworld.ObjectIDForCharacter(snapshot), attackSkill, []wire.SkillAreaTarget{
		{GID: target.gid, Impacts: hit.struck.impacts},
	})
	result.Broadcast = append([]wire.Frame{pulse}, result.Broadcast...)
	// The returned hit is c's: its private frames (a kill's rewards) are
	// c's, never the attacker's whose command this result answers.
	if len(result.ActorPrivate) > 0 {
		result.Recipients = append(result.Recipients, RecipientFrames{CharacterID: c.ID, Frames: result.ActorPrivate})
		result.ActorPrivate = nil
	}
	return result
}
