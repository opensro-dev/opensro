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
	"sync/atomic"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/internal/vitals"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// fullAreaPercent is an unreduced hit: the primary victim, and every hit
// of a skill without an action area.
const fullAreaPercent = 100

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
	percent  uint64
	now      int64
}

/*
================
scaleAreaDamage

Every impact of an area victim deals its share of the formula's damage,
as the player area does (skillarea.go planAreaVictims).
================
*/
func scaleAreaDamage(formula combat.Result, percent uint64) combat.Result {
	formula.Damage = uint32(uint64(formula.Damage) * percent / 100)
	return formula
}

/*
================
monsterStrikePlayer

One player victim: every authored impact behind its wall, the status
rolls, then HP, wear, death or battle state under the character door.
defender is the player's combat stats, read before the door.
================
*/
func (rt *Runtime) monsterStrikePlayer(in monsterStrikeInput, character, snapshot *enterworld.Character, defender combat.Stats, pose simulation.Spawn) monsterStrikeOutcome {
	divisionID, instance, skill, nowMs := in.division, in.instance, in.skill, in.now
	out := monsterStrikeOutcome{alive: true}
	formulas := make([]combat.Result, 0, skill.Attack.ImpactCount)
	// 58EC6C: a defender behind a wall splits every impact. ck / lfst /
	// pdmg / pdm2 attacks get no absorb group (589EE0), yet the wall's
	// lanes still leave the defender's record.
	wall, walled := rt.standingWallOf(divisionID, snapshot.Name)
	var wallRule *enterworld.SkillWall
	if walled {
		wallRule = &wall.wall
	}
	var splits []wallSplit
	// 590680 rolls on every hit; 593F0C installs the records on a surviving
	// victim after 58F491/593BEF's damage breaks (root, sleep, stun).
	var abnormalRecords []abnormal.Record
	for range skill.Attack.ImpactCount {
		split, resolveErr := rt.resolveCombatBehindWall(criticalActor{division: divisionID, monster: instance.Gid}, skill, in.attacker, defender, wallRule)
		formula := scaleAreaDamage(split.Defender, in.percent)
		if resolveErr != nil || formula.Damage == 0 && !walled && !formula.Blocked && !formula.Slain {
			return out
		}
		splits = append(splits, wallSplit{absorbed: split.Absorbed, flags: formula.ResultFlags, covered: split.Covered})
		formulas = append(formulas, formula)
		if formula.Blocked || formula.Slain {
			continue // 5905FB: no status roll for a blocked or ck-killed impact
		}
		records, rollErr := rt.rollMonsterOnPlayer(divisionID, instance, &skill.Abnormal, snapshot, defender, wallRule)
		if rollErr != nil {
			return out
		}
		abnormalRecords = append(abnormalRecords, records...)
	}
	if len(formulas) == 0 {
		return out
	}
	abnormalOwner := rt.newPlayerAbnormalOwner(divisionID, character, nowMs)
	abnormalOwner.sources = rt.captureAbnormalSources(divisionID, abnormalOwner.block, abnormalRecords)

	strike := monsterStrike{gid: enterworld.ObjectIDForCharacter(snapshot), owner: character, pose: pose}
	var deathProgressionFrames, deathEffectFrames, battleFrames []wire.Frame
	// 593AE8: a record a standing wall absorbs (+0x10) skips the recipient
	// branch; any other landed record reaches it.
	struck := false
	var wear wearFrames
	hitContext := abnormal.HitContext{Attack: skill.ReplacementPinned && skill.Replacement.MatchesExecutionSelector}
	committed := rt.deps.Update(character, "monster-basic-attack", func() bool {
		// The detached admission snapshot can predate a status transition.
		// Revalidate under the same mutation door that commits HP. Do not
		// turn a living but ineligible target into a synthetic death result.
		if character.DeletePending || !enterworld.CharacterAlive(character) ||
			!monster.AllowsTargetStatus(instance.Ref.TidWord, instance.Nest.NativeTacticsFlags, character.NativeBodyStatus) {
			return false
		}
		_, _, remaining, _ := rt.playerKeeperVitals(divisionID, character)
		for _, formula := range formulas {
			hitContext.Magical = hitContext.Magical || formula.MagicalDamage != 0
			debit := int64(vitals.HitDebit(uint32(remaining), formula.Damage))
			if formula.Slain {
				debit = remaining // 58F778: the ck kill marks the target dead
			}
			remaining -= debit
			strike.fatal = remaining == 0
			strike.impacts = append(strike.impacts, wire.SkillCastTargetImpact{
				ResultFlags: formula.ResultFlags,
				// Native 585664 serializes the full hit independently of HP.
				Damage:  formula.Damage,
				Fatal:   strike.fatal,
				Blocked: formula.Blocked,
				Slain:   formula.Slain,
			})
			if strike.fatal {
				break
			}
		}
		character.CurrentHP = &remaining
		struck = len(strike.impacts) > 0
		if walled && !skill.WallBypass {
			var absorbed uint32
			strike.absorb, absorbed = wallRecords(wall, splits, len(strike.impacts))
			rt.drainWall(divisionID, character.Name, wall.token, absorbed)
			struck = !allWallAbsorbed(strike.absorb)
		}
		// 593C9F/593CB1: a struck player's landed hits wear its armour and
		// its blocks its shield; a wall that absorbs the whole hit skips
		// the recipient branch.
		if struck {
			var tally wearTally
			for _, impact := range strike.impacts {
				// 58F784 jumps past the landed count (58F79F): a ck kill
				// wears nothing.
				if !impact.Slain {
					tally.note(impact.Blocked, false)
				}
			}
			for _, roll := range [...]struct{ mode, count uint8 }{{wearArmour, tally.armour}, {wearShield, tally.shield}} {
				taken := rt.rollEquipmentWear(divisionID, character, roll.mode, roll.count)
				wear.actor = append(wear.actor, taken.actor...)
				wear.public = append(wear.public, taken.public...)
			}
		}
		if strike.fatal {
			deathEffectFrames, deathProgressionFrames = rt.settlePlayerDeathInDoor(divisionID, character, nowMs)
			abnormalOwner = rt.clearPlayerAbnormalInDoor(divisionID, character, nowMs)
		} else {
			battleFrames = rt.enterBattleState(divisionID, character, nowMs)
			abnormalOwner.applyHit(hitContext, abnormalRecords)
			// 58F72F: a landed hit tests the victim's skc damage masks.
			rt.cancelEffectsOnDamage(divisionID, character, skill.Attack.Flags, nowMs)
		}
		return len(strike.impacts) > 0
	})
	if !committed || len(strike.impacts) == 0 {
		fresh := rt.characterSnapshot(divisionID, character)
		out.alive = enterworld.CharacterAlive(fresh)
		if out.alive && !fresh.DeletePending &&
			!monster.AllowsTargetStatus(instance.Ref.TidWord, instance.Nest.NativeTacticsFlags, fresh.NativeBodyStatus) {
			out.refusal = simulation.MonsterAttackCommandRejected
		}
		return out
	}
	if strike.fatal {
		rt.bindResidentRegion(simulation.WorldKey(divisionID, character.Name), nowMs)
	}
	abnormalFrames := rt.playerAbnormalPublication(divisionID, character, abnormalOwner)
	strike.public = append(strike.public, abnormalFrames.public...)
	strike.private = append(strike.private, abnormalFrames.actor...)
	strike.public = append(strike.public, wear.public...)
	strike.private = append(strike.private, wear.actor...)
	if strike.fatal {
		if rt.PushCharacterFrames != nil && rt.PushDivisionPeerFrames != nil {
			// Native death retires effects before publishing the life change.
			// Enqueue under the action lock, before a rebirth/new application can
			// reuse these wire tokens. The simulation still owns combat delivery.
			rt.publishBodyStatus(divisionID, character.Name, deathEffectFrames)
		} else {
			strike.public = append(strike.public, deathEffectFrames...)
		}
		lifePublication := beginFatalLifePublication(strike.gid)
		// B245/B505 retains fatal damage until the client impact callback. The
		// death-sourced 0x33A6 then advances its native wire baseline to zero
		// without applying that damage twice. Present-point rebirth depends on
		// this baseline: its source-zero 0x33A6 restores effective HP by delta.
		baseline := lifePublication.publishDeathBaseline()
		strike.public = append(strike.public, wire.Frame{Opcode: baseline.opcode, Payload: baseline.payload})
		// 0x3122 owns the durable life-state transition, death timer, motion,
		// and rebirth UI. It intentionally does not own HP reconciliation.
		death := lifePublication.publishDead()
		strike.public = append(strike.public, wire.Frame{Opcode: death.opcode, Payload: death.payload})
	}
	// Entering battle (4E1DF0, from ProcessNormalHit) follows the hit.
	if struck && !strike.fatal {
		strike.public = append(strike.public, rt.offensiveResultRecipient(divisionID, character, nowMs)...)
	}
	strike.public = append(strike.public, battleFrames...)
	strike.private = append(strike.private, deathProgressionFrames...)
	out.strike, out.committed, out.alive = strike, true, !strike.fatal
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
	divisionID, instance, skill, nowMs := in.division, in.instance, in.skill, in.now
	out := monsterStrikeOutcome{alive: true}
	ownerBlock := rt.newCosAbnormalOwnerForPet(divisionID, owner, pet, nowMs)
	defender, err := cosCombatStats(ref, pet, ownerBlock.block)
	if err != nil {
		return out
	}
	var records []abnormal.Record
	var formulas []combat.Result
	for range skill.Attack.ImpactCount {
		formula, resolveErr := rt.resolveCombat(criticalActor{division: divisionID, monster: instance.Gid}, skill, in.attacker, defender)
		formula = scaleAreaDamage(formula, in.percent)
		if resolveErr != nil || formula.Damage == 0 && !formula.Blocked {
			return out
		}
		formulas = append(formulas, formula)
		if formula.Blocked {
			continue // 5905FB: no damage and no status roll
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

The cast every victim's strike is published under. from is the monster's
pose at the action.
================
*/
type monsterPublication struct {
	strike  monsterStrikeInput
	release *pendingMonsterCast
	from    simulation.Spawn
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
		for _, victim := range rt.monsterAreaVictims(in, p.from, primary) {
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
	flight := projectileFlightMs(p.from, primary.pose, skill.ProjectileSpeed)
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
