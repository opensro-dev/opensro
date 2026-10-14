/*
===========================================================================

playerstrike_cos.go - a player's single-target hits on a COS

A thief can strike the transport a trader rides, a horse or a pet, as the
original lets a player select and attack a COS (#1002's spec). Admission is
CGObjPC_ValidateAttackTarget (5293A0): CGObjChar_CheckTargetAttackable
(5291D0) runs on the COS itself, then the target becomes the owner
(COS+0x1CD8) and every remaining PvP check runs on the owner exactly as for
a direct attack (playerAttackRefusal). The hit is the player's formula
against the COS's own defence, applied through the same companion owner a
monster's hit uses (cosHit, monsterstrike.go), so a death settles and a
transport's goods scatter exactly as they do today.

Aggression follows the native pair: CGObjCOS_ProcessNormalHit (52A1E0)
calls CGObjPC_EnterBattleOnAttacked (4E1DF0) for the owner, and the
attacker's CGObjPC_EnterBattleOnAttack (4E27C0) swaps a COS target for its
owner before CGObjPC_RegisterHostileTarget (4E25C0). Hitting a trader's
transport therefore marks the attacker against the trader as hitting the
trader would.

This file owns the single-target strike. An area, periodic, debuff, taunt,
drain or charge row aimed at a COS is refused here for now
(cosStrikeUnsupported): those paths only know monster and player victims.

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
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
cosTargetRefusal

5293A0 for a COS target. 5291D0 on the COS: a dead or untouchable COS and a
pickup pet refuse (0x3006); an owner whose body is protected (status 2..4)
refuses with 0x3020, as does the free-battle team through the owner. Then
the target becomes the owner and the whole PvP chain runs on it: self,
party, aggression weight, capes, the 1-star caravan protection, guild war,
jobs, the PK levels. Zero admits.
================
*/
func (rt *Runtime) cosTargetRefusal(division string, caster *enterworld.Character, target combatTarget, now int64) uint16 {
	if target.cos == nil || target.cosRef == nil || target.snapshot == nil || target.cos.CurrentHP == 0 ||
		target.cos.NativeBodyStatus == untouchableBodyStatus || target.cosRef.TidWord>>11 == domain.PickupPetBand {
		return 0x3006
	}
	owner := target.snapshot
	if owner.NativeBodyStatus >= mercenaryBodyProtectedFirst && owner.NativeBodyStatus <= mercenaryBodyProtectedLast {
		return 0x3020
	}
	if code := rt.companionTeamRefusal(caster, owner); code != 0 {
		return code
	}
	return rt.playerAttackRefusal(division, caster, owner, now, 0)
}

/*
================
cosStrikeUnsupported

The rows this file does not strike a COS with yet: anything but a plain
single-target hit. An imbue-eligible attack (att value 5) stays admitted: on
a COS it strikes the COS alone, without the imbue's chain victims.
================
*/
func cosStrikeUnsupported(skill enterworld.SkillRow) bool {
	return skill.Threat.Only || skill.HostileDebuff.Pinned || skill.TimedEffect.Periodic.Pinned ||
		skill.OffensiveArea.Radius != 0 || skill.ActionArea.Shape != 0 || skill.LifeSteal.Present ||
		skill.PositionEffect.Charge
}

/*
================
acceptCOSTargetStage

acceptSkillStagePhaseAt for a COS target: the shared admission, 5293A0
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
	if cosStrikeUnsupported(skill) {
		return offensiveRefusal(0x3006), skillCastRefused
	}
	if st.advanced {
		if _, refusal := rt.stagePhaseCost(division, snapshot, skill, now, st.release, st.rootID); refusal != 0 {
			return offensiveRefusal(refusal), skillCastRefused
		}
	}
	casterAt := rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)
	spacing, spacingOK := rt.combatTargetSpacing(snapshot, target, st.actionReach)
	if !spacingOK || simulation.IsDungeonRegion(casterAt.RegionID) != simulation.IsDungeonRegion(target.at.RegionID) {
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
	return rt.strikeCOSTarget(st, target, casterAt)
}

/*
================
planPlayerCOSHit

The player's formula against the COS's own defence for every impact, the
displacement on the first impact that leaves it standing, and the status
rolls of every unblocked impact (5905FB: a blocked one rolls nothing).
================
*/
func (rt *Runtime) planPlayerCOSHit(st offensiveStage, target combatTarget, casterAt simulation.Spawn) (cosHit, bool) {
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
	defender, err := cosCombatStats(ref, pet, block.block)
	if err != nil {
		return cosHit{}, false
	}
	hit := cosHit{owner: owner, pet: pet, ref: ref, pose: target.at, block: block, displaceAt: -1,
		context: abnormal.HitContext{Attack: skill.ReplacementPinned && skill.Replacement.MatchesExecutionSelector}}
	remainingHP := pet.CurrentHP
	actor := criticalActor{division: division, character: snapshot.Name}
	for range skill.Attack.ImpactCount {
		formula, resolveErr := rt.resolvePlayerImpact(division, snapshot.Name, skill, st.attacker, defender, now, false)
		if resolveErr != nil {
			return cosHit{}, false
		}
		formula = combat.FinishImpact(formula, combat.ImpactTail{Attack: skill.Attack.Present})
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
the owner's attacked registration (4E1DF0 via 52A1E0), the hit on the
companion, the caster's battle state and its hostile registration against
the owner (4E27C0 swaps the COS for COS+0x1CD8). Then the B245 result and
the companion's publication.
================
*/
func (rt *Runtime) strikeCOSTarget(st offensiveStage, target combatTarget, casterAt simulation.Spawn) (OpResult, skillCastDecision) {
	division, character, snapshot, skill, now := st.division, st.character, st.snapshot, st.skill, st.now
	hit, planned := rt.planPlayerCOSHit(st, target, casterAt)
	if !planned {
		return OpResult{}, skillCastRefused
	}
	owner := target.player
	var ammo ammunitionResult
	var battle, attacked, attack, killCredit, killPublic []wire.Frame
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
		attacked = rt.registerPlayerAttacked(division, owner, character, now)
		if !rt.applyCOSHitInDoor(division, &hit, now) {
			return false
		}
		if hit.strike.fatal {
			// 4C42F0 -> 4EB6B0: the killing blow eases the caster's murder
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
		return true
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal), skillCastRefused
		}
		return OpResult{}, skillCastRefused
	}
	strike := rt.publishCOSHit(&hit)
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
	// 593832: the attacker's unblocked attempts wear its weapon.
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
	actor = append(actor, killCredit...)
	private = append(private, killCredit...)
	public = append(public, killPublic...)
	actor = append(actor, attack...)
	public = append(public, attack...)
	actor = append(actor, battle...)
	public = append(public, battle...)
	out := OpResult{Frames: actor, Broadcast: public, ActorPrivate: private}
	if len(strike.private) > 0 {
		out.Recipients = []RecipientFrames{{CharacterID: owner.ID, Frames: strike.private}}
	}
	return out, skillCastAccepted
}

/*
================
rollPlayerOnCOS

590680 for a player's hit on a COS: the caster's level and learned
modifiers (rollPlayerOnMonster), the companion's level, resistances and
immunity (rollMonsterOnCOS: attack COS of band 1 are status-immune, 482840).
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
