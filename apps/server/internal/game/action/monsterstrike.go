/*
===========================================================================

monsterstrike.go - one victim of a monster's attack, and the publication

A monster's attack commits each victim on its own (a player, or a
companion its owner keeps), then publishes them under one cast token: one
single-target result for an ordinary attack, one area result when the
skill authors an action area (efr kind 1). Admission (spacing, the action
door, casting time) stays with monsterAttackStage and monsterHitSummonedCOS.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/domain"
	"sync/atomic"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/internal/vitals"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
monsterStrike

One committed victim. public follows the cast result to every observer;
private goes to owner's sessions alone (a struck companion's to its owner).
================
*/
type monsterStrike struct {
	gid     uint32
	owner   *enterworld.Character
	pose    simulation.Spawn
	impacts []wire.SkillCastTargetImpact
	// absorb is a player's wall group, one record per impact.
	absorb  []wire.SkillCastTargetImpact
	public  []wire.Frame
	private []wire.Frame
	fatal   bool
}

/*
================
monsterStrikeOutcome

A strike that did not commit says whether the victim still lives and
whether the action door must refuse the command.
================
*/
type monsterStrikeOutcome struct {
	strike    monsterStrike
	committed bool
	alive     bool
	refusal   simulation.MonsterAttackRefusal
}

/*
================
monsterStrikeInput

The attack every victim of one action shares. percent is this victim's
share of the full damage (58E5F0 lowers it by the area's reduction after
each victim).
================
*/
type monsterStrikeInput struct {
	division string
	instance monster.Instance
	skill    enterworld.SkillRow
	attacker combat.Stats
	from     monster.Pose
	percent  uint64
	now      int64
}

/*
================
origin

The monster's pose as a spawn point.
================
*/
func (in monsterStrikeInput) origin() simulation.Spawn {
	return simulation.Spawn{RegionID: in.from.RegionID, X: in.from.X, Y: in.from.Y, Z: in.from.Z}
}

/*
================
finishMonsterImpact

The shared impact tail (combat.FinishImpact) for a monster's strike: this
victim's area share, then the monster attacker's damage scale (5874D0, by
its rarity byte), then the att floor. A defender its wall fully covers
(covered) keeps its zero record.
================
*/
func finishMonsterImpact(in monsterStrikeInput, formula combat.Result, covered bool) combat.Result {
	return combat.FinishImpact(formula, combat.ImpactTail{Percent: in.percent, MonsterAttacker: true,
		AttackerRarity: in.instance.Rarity(), Attack: in.skill.Attack.Present, Covered: covered})
}

/*
================
monsterStrikePlayer

One player victim through the shared player strike (playervictim.go):
every authored impact behind its wall at this victim's share, the status
rolls and the displacement roll, then the victim's door, its publication
and the damage it returns to the monster (5A0C2D, a fatal hit included).
================
*/
func (rt *Runtime) monsterStrikePlayer(in monsterStrikeInput, character, snapshot *enterworld.Character, defender combat.Stats, pose simulation.Spawn) monsterStrikeOutcome {
	divisionID, instance, skill, nowMs := in.division, in.instance, in.skill, in.now
	out := monsterStrikeOutcome{alive: true}
	strike := playerStrike{division: divisionID, victim: character, killer: deathKiller{monster: &instance}, skill: skill, now: nowMs}
	actor := criticalActor{division: divisionID, monster: instance.Gid}
	if !rt.planPlayerStrike(&strike,
		func(wall *enterworld.SkillWall) (combat.WallOutcome, error) {
			outcome, err := rt.resolveCombatBehindWall(actor, skill, in.attacker, defender, wall)
			outcome.Defender = finishMonsterImpact(in, outcome.Defender, outcome.Covered)
			outcome.Absorbed = uint32(uint64(outcome.Absorbed) * in.percent / 100)
			return outcome, err
		},
		func(wall *enterworld.SkillWall, _ combat.Result) ([]abnormal.Record, error) {
			return rt.rollMonsterOnPlayer(divisionID, instance, &skill.Abnormal, snapshot, defender, wall)
		}) {
		return out
	}
	if err := rt.planStrikeDisplacement(&strike, actor, in.origin(), pose); err != nil {
		return out
	}
	var struck playerStruck
	committed := rt.deps.Update(character, "monster-basic-attack", func() bool {
		// The detached admission snapshot can predate a status transition.
		// Revalidate under the same mutation door that commits HP. Do not
		// turn a living but ineligible target into a synthetic death result.
		if character.DeletePending || !enterworld.CharacterAlive(character) ||
			!monster.AllowsTargetStatus(instance.Ref.TidWord, instance.Nest.NativeTacticsFlags, character.NativeBodyStatus) {
			return false
		}
		struck = rt.strikePlayerInDoor(strike)
		return len(struck.impacts) > 0
	})
	if !committed || len(struck.impacts) == 0 {
		fresh := rt.characterSnapshot(divisionID, character)
		out.alive = enterworld.CharacterAlive(fresh)
		if out.alive && !fresh.DeletePending &&
			!monster.AllowsTargetStatus(instance.Ref.TidWord, instance.Nest.NativeTacticsFlags, fresh.NativeBodyStatus) {
			out.refusal = simulation.MonsterAttackCommandRejected
		}
		return out
	}
	if struck.fatal {
		rt.bindResidentRegion(simulation.WorldKey(divisionID, character.Name), nowMs)
	}
	public, private := rt.playerStruckFrames(divisionID, character, struck, nowMs)
	// 5A0C2D ran inside the hit outcome, before the hit landed, so a fatal
	// hit still returns its share; the attacker takes it afterwards.
	returned := rt.returnDamageToMonster(divisionID, character, instance, in.from, skill.ID, defender, strike.formulas[:len(struck.impacts)], nowMs)
	// The shares fence and Pain Quota links took off the hits land on their
	// takers as this monster's hits (linkeddamage.go).
	returned = mergeOpResults(returned, rt.strikeLinkedShares(divisionID, instance.Gid, strike.killer, skill, struck.linkMoves, nowMs))
	rt.queueMonsterLegRecipients(divisionID, returned.Recipients)
	out.strike = monsterStrike{gid: enterworld.ObjectIDForCharacter(snapshot), owner: character, pose: pose,
		impacts: struck.impacts, absorb: struck.absorb, fatal: struck.fatal,
		public: append(public, returned.Broadcast...), private: append(private, returned.ActorPrivate...)}
	out.committed, out.alive = true, !struck.fatal
	return out
}

/*
================
monsterStrikeCOS

One companion victim, judged on its own reference and abnormal block: the
owner's equipment can neither shield nor weaken a pet.
================
*/
func (rt *Runtime) monsterStrikeCOS(in monsterStrikeInput, owner *enterworld.Character, pet *enterworld.CharacterCOS, ref *enterworld.CharacterRef, pose simulation.Spawn) monsterStrikeOutcome {
	// Recheck at impact: a stale cast or area victim cannot harm a grab pet,
	// which 5299E0 never treats as hostile (CGObj_IsPickPetCOS, 483930).
	if ref.TidWord>>11 == domain.PickupPetBand {
		return monsterStrikeOutcome{alive: true, refusal: simulation.MonsterAttackCommandRejected}
	}
	divisionID, instance, skill, nowMs := in.division, in.instance, in.skill, in.now
	out := monsterStrikeOutcome{alive: true}
	ownerBlock := rt.newCosAbnormalOwnerForPet(divisionID, owner, pet, nowMs)
	defender, err := cosCombatStats(ref, pet, ownerBlock.block)
	if err != nil {
		return out
	}
	var displacement *playerDisplacement
	displaceAt := -1
	remainingHP := pet.CurrentHP
	var records []abnormal.Record
	var formulas []combat.Result
	for range creatureImpactCount(skill) {
		formula, resolveErr := rt.resolveCreatureImpact(criticalActor{division: divisionID, monster: instance.Gid}, skill, in.attacker, defender)
		formula = finishMonsterImpact(in, formula, false)
		if resolveErr != nil || formula.Damage == 0 && !formula.Blocked && !skill.CreatureStatusCast {
			return out
		}
		formulas = append(formulas, formula)
		if formula.Blocked {
			continue // 5905FB: no damage and no status roll
		}
		remainingHP -= min(remainingHP, formula.Damage)
		if remainingHP > 0 && displacement == nil {
			var err error
			displacement, err = rt.planActorDisplacement(displacementRoll{division: divisionID, actor: criticalActor{division: divisionID, monster: instance.Gid}, from: in.origin(), skill: skill, at: pose, now: nowMs},
				displacementTarget{flags: ref.Parameters.Knockdown, recovery: ref.Parameters.KORecoverMs, level: ref.Level, allowed: rt.cosDisplaceable(divisionID, owner, pet, nowMs)})
			if err != nil {
				return out
			}
			if displacement != nil {
				displaceAt = len(formulas) - 1
			}
		}
		rolled, rollErr := rt.rollMonsterOnCOS(cosAbnormalRoll{division: divisionID, caster: instance,
			params: &skill.Abnormal, target: ownerBlock})
		if rollErr != nil {
			return out
		}
		records = append(records, rolled...)
	}
	ownerBlock.sources = rt.captureAbnormalSources(divisionID, ownerBlock.block, records)
	strike := monsterStrike{gid: pet.GID, owner: owner, pose: pose}
	var battleFrames []wire.Frame
	var remaining uint32
	hitContext := abnormal.HitContext{Attack: skill.ReplacementPinned && skill.Replacement.MatchesExecutionSelector}
	committed := rt.deps.Update(owner, "monster-cos-hit", func() bool {
		live := owner.CompanionByGID(pet.GID)
		if live == nil || live.GID != pet.GID || live.CurrentHP == 0 {
			return false
		}
		// CGObjCOS_ProcessNormalHit (52A1E0): the owner (COS+0x1CD8) enters
		// battle (4E1DF0) before the pet takes the hit, fatal or not.
		if enterworld.CharacterAlive(owner) {
			battleFrames = rt.enterBattleState(divisionID, owner, nowMs)
		}
		for _, formula := range formulas {
			hitContext.Magical = hitContext.Magical || formula.MagicalDamage != 0
			live.CurrentHP -= vitals.HitDebit(live.CurrentHP, formula.Damage)
			strike.fatal = live.CurrentHP == 0
			strike.impacts = append(strike.impacts, wire.SkillCastTargetImpact{Damage: formula.Damage,
				Fatal: strike.fatal, Blocked: formula.Blocked, ResultFlags: formula.ResultFlags})
			if strike.fatal {
				break
			}
		}
		if !strike.fatal && displacement != nil && displaceAt < len(strike.impacts) {
			if point, ok := rt.commitCOSDisplacement(ownerBlock, displacement); ok {
				if displacement.down {
					strike.impacts[displaceAt].Knockdown = point
				} else {
					strike.impacts[displaceAt].Knockback = point
				}
			}
		}
		remaining = live.CurrentHP
		if strike.fatal {
			ownerBlock.changed = ownerBlock.block.ClearAll(ownerBlock)
			ownerBlock.fatal = true
		} else {
			if ownerBlock.block.Mask != 0 {
				ownerBlock.changed = ownerBlock.block.BreakOnHit(ownerBlock, hitContext) || ownerBlock.changed
			}
			for _, record := range records {
				if ownerBlock.block.Apply(ownerBlock, record, nowMs) {
					ownerBlock.changed = true
				}
			}
		}
		ownerBlock.commit()
		return true
	})
	if !committed {
		return out
	}
	strike.public = append(strike.public, wire.Frame{
		Opcode:  simulation.OpVitalsUpdate,
		Payload: simulation.HPRefreshPayload(pet.GID, 0, remaining),
	})
	strike.public = append(strike.public, rt.cosAbnormalPublication(pet.GID, ownerBlock)...)
	strike.public = append(strike.public, battleFrames...)
	strike.private = append(strike.private, ownerBlock.private...)
	out.strike, out.committed, out.alive = strike, true, !strike.fatal
	return out
}

/*
================
monsterPublication

The cast every victim's strike is published under.
================
*/
type monsterPublication struct {
	strike  monsterStrikeInput
	release *pendingMonsterCast
}

/*
================
publishMonsterStrikes

The primary's strike, then (for an action area) every further victim the
area selects at the same release, each at the running reduced share. The
result names every victim under one token; finalize closes it after the
action, or after the primary's flight when that is longer.
================
*/
func (rt *Runtime) publishMonsterStrikes(p monsterPublication, primary monsterStrike) simulation.MonsterAttackResult {
	in, skill := p.strike, p.strike.skill
	strikes := []monsterStrike{primary}
	if area := skill.ActionArea; area.Shape != 0 && area.MaxTargets > 1 {
		percent := uint64(fullAreaPercent) * uint64(100-area.ReductionPercent) / 100
		for _, victim := range rt.monsterAreaVictims(in, in.origin(), primary) {
			in.percent = percent
			outcome := rt.strikeMonsterAreaVictim(in, victim)
			if !outcome.committed {
				continue // a secondary that cannot be struck leaves the others
			}
			strikes = append(strikes, outcome.strike)
			percent = percent * uint64(100-area.ReductionPercent) / 100
		}
	}
	var token uint32
	if p.release != nil {
		token = p.release.token
	} else {
		token = atomic.AddUint32(&rt.castTokenCounter, 1)
	}
	cast := wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: in.instance.Gid, InstanceToken: token}
	var frame wire.Frame
	if skill.ActionArea.Shape == 0 {
		wireResult := wire.NewStationarySkillCastSingleTargetResult(cast, primary.gid, primary.impacts)
		if primary.absorb != nil {
			wireResult = wireResult.WithAbsorb(primary.absorb)
		}
		frame = wire.SkillCastSingleTargetResultFrame(wireResult)
		if p.release != nil {
			frame = wire.SkillCastReleaseResultFrame(wireResult)
		}
	} else {
		targets := monsterAreaTargets(strikes, int(skill.Attack.ImpactCount))
		frame = wire.SkillCastAreaFrame(cast, primary.gid, targets)
		if p.release != nil {
			frame = wire.SkillCastAreaReleaseFrame(cast, primary.gid, targets)
		}
	}
	actionLifecycleMs, _ := skill.ActionLifecycleMs()
	flight := projectileFlightMs(in.origin(), primary.pose, skill.ProjectileSpeed)
	closeAt := in.now + max(int64(actionLifecycleMs), flight+1)
	owner := monsterCastOwner(in.instance.Gid)
	if p.release == nil {
		rt.queueSkillFinalize(in.division, owner, in.instance.Gid, in.now, wire.SkillCastReleaseFrame(token, primary.gid))
	} else {
		closeAt = in.now + max(int64(skill.ActionDurationMs), flight+1)
	}
	rt.queueSkillFinalize(in.division, owner, in.instance.Gid, closeAt, wire.SkillCastFinalizeFrame(token))
	result := simulation.MonsterAttackResult{Accepted: true, TargetAlive: !primary.fatal}
	result.Frames = append(result.Frames, simulationFrame(frame))
	for _, strike := range strikes {
		for _, f := range strike.public {
			result.Frames = append(result.Frames, simulationFrame(f))
		}
		if len(strike.private) == 0 {
			continue
		}
		private := simulation.MonsterPrivateFrames{CharacterID: strike.owner.ID, CharacterName: strike.owner.Name}
		for _, f := range strike.private {
			private.Frames = append(private.Frames, simulationFrame(f))
		}
		result.Private = append(result.Private, private)
	}
	return result
}

/*
================
monsterAreaTargets

Client 8E0190 reads target-major records with one impact count: a victim
whose hit was fatal before its last impact carries bare type-8 records
(58EE1B), and a walled player's absorb group follows it under its gid
(58EC9F appends it to the result's target list).
================
*/
func monsterAreaTargets(strikes []monsterStrike, impacts int) []wire.SkillAreaTarget {
	pad := func(records []wire.SkillCastTargetImpact) []wire.SkillCastTargetImpact {
		records = append([]wire.SkillCastTargetImpact(nil), records...)
		for len(records) < impacts {
			records = append(records, wire.SkillCastTargetImpact{Skipped: true})
		}
		return records
	}
	targets := make([]wire.SkillAreaTarget, 0, len(strikes))
	for _, strike := range strikes {
		targets = append(targets, wire.SkillAreaTarget{GID: strike.gid, Impacts: pad(strike.impacts)})
		if strike.absorb != nil {
			targets = append(targets, wire.SkillAreaTarget{GID: strike.gid, Impacts: pad(strike.absorb)})
		}
	}
	return targets
}

/*
================
simulationFrame
================
*/
func simulationFrame(f wire.Frame) simulation.Frame {
	return simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope}
}
