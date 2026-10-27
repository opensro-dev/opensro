/*
===========================================================================

playerstrike_cos.go - a player's single-target hits on a COS

A thief can strike the transport a trader rides, a horse or a pet, as the
original lets a player select and attack a COS (#1002's spec). Admission is
the native attack-target validation: the attackable check
runs on the COS itself, then the target becomes the owner
(COS+0x1CD8) and every remaining PvP check runs on the owner exactly as for
a direct attack (playerAttackRefusal). The hit is the player's formula
against the COS's own defence, applied through the same companion owner a
monster's hit uses (cosHit, monsterstrike.go), so a death settles and a
transport's goods scatter exactly as they do today.

Aggression follows the native pair: the COS hit path
enters the owner into battle as attacked, and the
attacker's own battle entry swaps a COS target for its
owner before the hostile registration. Hitting a trader's
transport therefore marks the attacker against the trader as hitting the
trader would.

This file owns the COS target stage: a plain hit, a life-steal or charge
strike, an area with the COS as primary and a periodic install all land
on the COS's body, and a hostile debuff row installs its word on the
companion (hostiledebuff.go's debuffCOS, cosdebuff.go). A taunt stays
refused (monster-hostility only).

===========================================================================
*/
package action

import (
	"sync/atomic"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
cosTargetRefusal

The native attack-target validation for a COS target, in the attackable check's order:
 1. a dead, hidden (modes 6..7) or pickup-pet (+0x43C) COS refuses with 0x3006;
 2. an attacker in body mode 2 refuses with 0x3020;
 3. a target COS in body mode 2..4 refuses with 0x3020;
 4. an owner in body mode 2..4 or on the caster's
    free-battle team refuses with 0x3020, then the target becomes the owner
    and the whole PvP chain runs on it (playerAttackRefusal). Zero admits.

================
*/
func (rt *Runtime) cosTargetRefusal(division string, caster *enterworld.Character, target combatTarget, now int64) uint16 {
	return rt.cosTargetRefusalWith(division, caster, target, now, 0)
}

/*
================
cosTargetRefusalWith

cosTargetRefusal with the native validation's control flags: an area selection passes
playerAttackArea (mode 3), as the native hostile-target test asks for a player caster.
================
*/
func (rt *Runtime) cosTargetRefusalWith(division string, caster *enterworld.Character, target combatTarget, now int64, flags uint8) uint16 {
	if caster == nil || target.cos == nil || target.cosRef == nil || target.snapshot == nil || target.cos.CurrentHP == 0 ||
		target.cos.NativeBodyStatus >= mercenaryBodyHiddenFirst && target.cos.NativeBodyStatus <= mercenaryBodyHiddenLast ||
		target.cosRef.TidWord>>11 == domain.PickupPetBand {
		return 0x3006
	}
	if caster.NativeBodyStatus == untouchableBodyStatus {
		return 0x3020
	}
	if target.cos.NativeBodyStatus >= mercenaryBodyProtectedFirst && target.cos.NativeBodyStatus <= mercenaryBodyProtectedLast {
		return 0x3020
	}
	owner := target.snapshot
	if owner.NativeBodyStatus >= mercenaryBodyProtectedFirst && owner.NativeBodyStatus <= mercenaryBodyProtectedLast {
		return 0x3020
	}
	if code := rt.companionTeamRefusal(caster, owner); code != 0 {
		return code
	}
	return rt.playerAttackRefusal(division, caster, owner, now, flags)
}

/*
================
acceptCOSTargetStage

acceptSkillStagePhaseAt for a COS target: the shared admission, natively validated
through the owner, the phase cost, range and preparation, then one hit.
================
*/
func (rt *Runtime) acceptCOSTargetStage(st offensiveStage, target combatTarget) (OpResult, skillCastDecision) {
	division, character, snapshot, skill, now := st.division, st.character, st.snapshot, st.skill, st.now
	mask := admitExecution
	if st.rootID != 0 {
		mask &^= admitCooldown
	}
	if code := rt.skillAdmission(division, snapshot, skill, now, &admitTarget{at: target.at}, st.release, mask); code != 0 {
		return offensiveRefusal(code), skillCastRefused
	}
	if code := rt.cosTargetRefusal(division, snapshot, target, now); code != 0 {
		return offensiveRefusal(code), skillCastRefused
	}
	if skill.Threat.Only {
		// A taunt is monster-hostility only, as on a player.
		return offensiveRefusal(0x3006), skillCastRefused
	}
	if st.advanced {
		if _, refusal := rt.stagePhaseCost(division, snapshot, skill, now, st.release, st.rootID); refusal != 0 {
			return offensiveRefusal(refusal), skillCastRefused
		}
	}
	casterAt := rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)
	spacing, spacingOK := rt.combatTargetSpacing(snapshot, target, st.actionReach)
	if !spacingOK || !worldgeom.SamePlane(casterAt.RegionID, target.at.RegionID) {
		return OpResult{}, skillCastDeferred
	}
	if st.release == nil && !spacing.Contains(casterAt, target.at) {
		if st.rootID == 0 {
			return OpResult{}, skillCastDeferred
		}
		if code := linkedStageTargetRefusal(casterAt, target.at); code != 0 {
			return offensiveRefusal(code), skillCastRefused
		}
	}
	if skill.ActionCastingTimeMs != 0 && st.release == nil {
		return rt.prepareOffensiveCast(division, character, snapshot, st.cast, skill, st.advanced, st.rootID, now)
	}
	if skill.HostileDebuff.Pinned {
		return rt.debuffCOS(debuffCast{division: division, character: character, snapshot: snapshot,
			skill: skill, target: target.gid, now: now}, target)
	}
	// The player stage's dispatch order (pvpstrike.go), on the COS body:
	// a periodic installs its linked pair, an area resolves around the
	// COS as primary, anything else strikes it directly.
	if skill.TimedEffect.Periodic.Pinned {
		return rt.installPeriodicCast(periodicCast{division: division, character: character, snapshot: snapshot,
			skill: skill, cast: st.cast, target: target, attacker: st.attacker, now: now, release: st.release})
	}
	if skill.OffensiveArea.Radius != 0 {
		return rt.acceptSkillAreaAt(division, character, snapshot, skill, skill.OffensiveArea, false, true, target, st.attacker, st.loadout, st.consumeAmmo, now, st.rootID, st.release)
	}
	return rt.strikeCOSTarget(st, target, casterAt)
}

/*
================
planPlayerCOSHitAt

planPlayerCOSHit at an area's running percent (0 reads as the whole
result, combat.FinishImpact) with an lfst row's life-steal base: an area
victim takes the life-steal record at its percent, as a monster does.
================
*/
func (rt *Runtime) planPlayerCOSHitAt(st offensiveStage, target combatTarget, casterAt simulation.Spawn, percent uint64, lifeStealBase int64) (cosHit, bool) {
	division, snapshot, skill, now := st.division, st.snapshot, st.skill, st.now
	owner, ref := target.player, target.cosRef
	// The authoritative companion, never the target's detached copy: its
	// death settles on the record the owner holds (petStrikeCOS reads it
	// the same way).
	var pet *enterworld.CharacterCOS
	rt.deps.Read(division, func() { pet = owner.CompanionByGID(target.gid) })
	if pet == nil || pet.RefObjID != target.cos.RefObjID || pet.SummonGeneration != target.cos.SummonGeneration {
		return cosHit{}, false
	}
	block := rt.newCosAbnormalOwnerForPet(division, owner, pet, now)
	defender, err := cosCombatStats(ref, pet, block.block, rt.cosDebuffWords(division, owner.Name, pet.GID, now))
	if err != nil {
		return cosHit{}, false
	}
	hit := cosHit{owner: owner, pet: pet, ref: ref, pose: target.at, block: block, displaceAt: -1,
		context: abnormal.HitContext{Attack: skill.ReplacementPinned && skill.Replacement.MatchesExecutionSelector}}
	remainingHP := pet.CurrentHP
	actor := criticalActor{division: division, character: snapshot.Name}
	for range skill.Attack.ImpactCount {
		var formula combat.Result
		if skill.LifeSteal.Present {
			// The percent rides into the life-steal formula, after its HP cap.
			formula = lifeStealResult(lifeStealBase, st.attacker, defender, remainingHP, uint32(percent))
			formula = combat.FinishImpact(formula, combat.ImpactTail{PercentApplied: true, Attack: skill.Attack.Present})
		} else {
			resolved, resolveErr := rt.resolvePlayerImpact(division, snapshot.Name, skill, st.attacker, defender, now, false)
			if resolveErr != nil {
				return cosHit{}, false
			}
			formula = combat.FinishImpact(resolved, combat.ImpactTail{Percent: percent, Attack: skill.Attack.Present})
		}
		hit.formulas = append(hit.formulas, formula)
		if formula.Blocked {
			continue
		}
		remainingHP -= min(remainingHP, formula.Damage)
		if remainingHP > 0 && hit.displacement == nil {
			hit.displacement, err = rt.planActorDisplacement(displacementRoll{division: division, actor: actor, from: casterAt, skill: skill, at: target.at, now: now},
				displacementTarget{flags: ref.Parameters.Knockdown, recovery: ref.Parameters.KORecoverMs, level: ref.Level, allowed: rt.cosDisplaceable(division, owner, pet, now)})
			if err != nil {
				return cosHit{}, false
			}
			if hit.displacement != nil {
				hit.displaceAt = len(hit.formulas) - 1
			}
		}
		rolled, rollErr := rt.rollPlayerOnCOS(division, snapshot, &skill.Abnormal, block)
		if rollErr != nil {
			return cosHit{}, false
		}
		hit.records = append(hit.records, rolled...)
	}
	if len(hit.formulas) == 0 {
		return cosHit{}, false
	}
	hit.block.sources = rt.captureAbnormalSources(division, hit.block.block, hit.records)
	return hit, true
}

/*
================
strikeCOSTarget

Plan, then one door for the caster and the owner: the cost and ammunition,
the owner's attacked registration (through the COS hit path), the hit on the
companion, the caster's battle state and its hostile registration against
the owner (natively the COS swaps for COS+0x1CD8). Then the B245 result and
the companion's publication.
================
*/
func (rt *Runtime) strikeCOSTarget(st offensiveStage, target combatTarget, casterAt simulation.Spawn) (OpResult, skillCastDecision) {
	division, character, snapshot, skill, now := st.division, st.character, st.snapshot, st.skill, st.now
	stealBase, stealOK := int64(0), true
	if skill.LifeSteal.Present {
		stealBase, stealOK = rt.lifeStealBase(division, snapshot, skill.LifeSteal, st.attacker)
	}
	if !stealOK {
		return OpResult{}, skillCastRefused
	}
	hit, planned := rt.planPlayerCOSHitAt(st, target, casterAt, 0, stealBase)
	if !planned {
		return OpResult{}, skillCastRefused
	}
	var travel skillTravelPlan
	if skill.PositionEffect.Charge {
		from, navOwner := rt.liveNav(simulation.WorldKey(division, snapshot.Name), snapshot, now)
		radius, valid := rt.deps.CharacterBodyRadius(snapshot)
		goal, admitted := chargeSkillGoal(from, target.at, skill.PositionEffect.Range, radius+float64(target.cosRef.Parameters.BodyRadius))
		if !valid || !admitted {
			return OpResult{}, skillCastRefused
		}
		if travel, planned = rt.planSkillTravel(snapshot.Name, from, navOwner, goal); !planned {
			return OpResult{DiagnosticRefusal: "charge-navigation-refused"}, skillCastRefused
		}
	}
	owner := target.player
	// The native PvP rewards classify the kill against the owner before the door, as a
	// player kill does; withVictim refreshes it inside.
	kill := rt.classifyPlayerKill(division, character, owner)
	var ammo ammunitionResult
	var battle, attacked, attack, killCredit, killPublic, killReward, killRewardPublic []wire.Frame
	var stolen wire.Frame
	var killAfter jobKillPayout
	var killWar domain.GuildWarCombat
	var refusal uint16
	if !rt.deps.UpdateMany([]*enterworld.Character{character, owner}, "player-strike-cos", func() bool {
		var cost skillCharge
		if st.advanced {
			if cost, refusal = rt.stagePhaseCost(division, character, skill, now, st.release, st.rootID); refusal != 0 {
				return false
			}
		}
		debit, admitted := ammunitionDebit{index: -1}, true
		if st.consumeAmmo {
			debit, admitted = rt.planEquippedAmmunition(character, st.loadout.WeaponKind, ammunitionSpent(skill, st.advanced))
		}
		if !admitted {
			refusal = 0x300e
			return false
		}
		if !rt.applyCOSHitInDoor(division, &hit, now) {
			return false
		}
		// After the last refusal: a refused door must leave the owner's
		// aggression map as it found it.
		attacked = rt.registerPlayerAttacked(division, owner, character, now)
		if hit.strike.fatal {
			// Natively the kill is booked against the owner (payCOSKillInDoor)
			// before the hostile target is registered below and before
			// the COS's own penalty relief.
			kill = kill.withVictim(character, owner)
			killReward, killRewardPublic, killAfter, killWar = rt.payCOSKillInDoor(division, character, owner, kill, hit.pet.Level, now)
			// Natively the killing blow eases the caster's murder
			// penalty by the gap to the companion's level (cosplayerkill.go).
			killCredit, killPublic = rt.creditPlayerCOSKill(division, character, hit.pet.Level)
		}
		if st.release == nil {
			rt.startSkillCast(division, character, skill, now)
		}
		battle = rt.enterBattleState(division, character, now)
		attack = rt.registerPlayerAttack(division, character, owner, now)
		if st.consumeAmmo {
			ammo = applyAmmunitionDebit(character, debit)
		}
		if st.advanced {
			rt.commitOffensivePhaseCost(division, character, skill, cost, now, st.release != nil)
		} else if st.release == nil {
			rt.registerPlayerSkillCooldown(division, character, skill, now)
		}
		if skill.LifeSteal.Present {
			stolen = rt.commitLifeSteal(division, character, cosDamageResults(&hit))
		}
		if skill.PositionEffect.Charge {
			rt.commitSkillTravel(simulation.WorldKey(division, character.Name), character, travel)
		}
		return true
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal), skillCastRefused
		}
		return OpResult{}, skillCastRefused
	}
	if skill.PositionEffect.Charge {
		rt.bindResidentRegion(simulation.WorldKey(division, character.Name), now)
	}
	strike := rt.publishCOSHit(&hit)
	rt.publishGuildWarCombat(division, killWar, now)
	caster := enterworld.ObjectIDForCharacter(snapshot)
	var token uint32
	if st.release != nil {
		token = st.release.token
	} else {
		token = atomic.AddUint32(&rt.castTokenCounter, 1)
	}
	success := wire.SkillCastSuccess{SkillId: st.cast.ActionId, CasterGid: caster, InstanceToken: token}
	result := wire.NewStationarySkillCastSingleTargetResult(success, target.gid, strike.impacts)
	frame := wire.SkillCastSingleTargetResultFrame(result)
	closeAt := now + int64(st.lifecycleMs)
	if st.release == nil {
		rt.queueSkillFinalize(division, snapshot.Name, caster, now+int64(skill.ActionCastingTimeMs), wire.SkillCastReleaseFrame(token, target.gid))
	} else {
		frame = wire.SkillCastReleaseResultFrame(result)
		closeAt = now + int64(skill.ActionDurationMs)
	}
	if skill.ActionHandler == enterworld.SkillActionProjectile {
		closeAt = max(closeAt, now+projectileFlightMs(casterAt, target.at, skill.ProjectileSpeed)+1)
	}
	rt.queueSkillCastClose(division, snapshot.Name, caster, token, skill, st.rootID, closeAt)
	actor := append([]wire.Frame{frame}, strike.public...)
	public := append([]wire.Frame{frame}, strike.public...)
	var private []wire.Frame
	if st.consumeAmmo {
		actor = append(actor, ammunitionFrames(ammo)...)
		private = append(private, ammunitionFrames(ammo)...)
	}
	// The attacker's unblocked attempts wear its weapon.
	var tally wearTally
	for _, formula := range hit.formulas {
		tally.note(formula.Blocked, true)
	}
	wear := rt.applyEquipmentWear(division, character, tally)
	actor = append(actor, wear.actor...)
	private = append(private, wear.actor...)
	public = append(public, wear.public...)
	if st.advanced {
		vitals := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshPayload(caster, rt.publishedVitals(division, character))}
		actor = append(actor, vitals)
		private = append(private, vitals)
	}
	public = append(public, attacked...)
	actor = append(actor, killReward...)
	private = append(private, killReward...)
	public = append(public, killRewardPublic...)
	actor = append(actor, killCredit...)
	private = append(private, killCredit...)
	public = append(public, killPublic...)
	actor = append(actor, attack...)
	public = append(public, attack...)
	actor = append(actor, battle...)
	public = append(public, battle...)
	if stolen.Opcode != 0 {
		actor = append(actor, stolen)
		public = append(public, stolen)
	}
	out := OpResult{Frames: actor, Broadcast: public, ActorPrivate: private}
	recipients := rt.payJobKillShares(killAfter)
	if len(strike.private) > 0 {
		recipients = append([]RecipientFrames{{CharacterID: owner.ID, Frames: strike.private}}, recipients...)
	}
	out.Recipients = recipients
	return out, skillCastAccepted
}

/*
================
rollPlayerOnCOS

The native status roll for a player's hit on a COS: the caster's level and learned
modifiers (rollPlayerOnMonster), the companion's level, resistances and
immunity (rollMonsterOnCOS: attack COS of band 1 are status-immune).
================
*/
func (rt *Runtime) rollPlayerOnCOS(division string, c *enterworld.Character, params *abnormal.SkillParams, target *cosAbnormalOwner) ([]abnormal.Record, error) {
	if params == nil || !params.Present() || target.ref == nil {
		return nil, nil
	}
	stats, _, err := rt.playerCombatStats(division, c)
	if err != nil {
		return nil, err
	}
	values := stats.SkillParameters
	pet := target.pet
	in := abnormal.RollInput{
		Params:      params,
		TargetLevel: pet.Level,
		CasterLevel: stats.Level,
		CasterModifier: func(key uint32) (uint32, bool) {
			slot, known := enterworld.SkillParameterFromKey(key)
			if !known {
				return 0, false
			}
			return values[slot], values[slot] != 0
		},
		SourceGID:    enterworld.ObjectIDForCharacter(c),
		TargetGID:    pet.GID,
		SourceName:   c.Name,
		TargetBonus:  target.Param(abnormalDiseaseBonusParam),
		TargetImmune: target.ref.TidWord&0x7fe == 0x1c6 && target.ref.TidWord>>11 == 1,
	}
	for index := range in.TargetResist {
		in.TargetResist[index] = target.Param(abnormalElementResistBase + uint16(index))
		in.TargetFlat[index] = target.Param(abnormalFlatResistanceBase + uint16(index))
	}
	random := &abnormalRandom{rt: rt, actor: criticalActor{division: division, character: c.Name}}
	return abnormal.Roll(in, random), random.err
}
