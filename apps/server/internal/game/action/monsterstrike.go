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
share of the full damage (the native hit outcome lowers it by the area's reduction after
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
victim's area share, then the monster attacker's damage scale (by
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
and the damage it returns to the monster (a fatal hit included).
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
	committed := rt.deps.UpdateDeferred(character, "monster-basic-attack", func() bool {
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
		alive, deletePending, nativeBodyStatus := rt.characterLifeState(divisionID, character)
		out.alive = alive
		if out.alive && !deletePending &&
			!monster.AllowsTargetStatus(instance.Ref.TidWord, instance.Nest.NativeTacticsFlags, nativeBodyStatus) {
			out.refusal = simulation.MonsterAttackCommandRejected
		}
		return out
	}
	if struck.fatal {
		rt.bindResidentRegion(simulation.WorldKey(divisionID, character.Name), nowMs)
	}
	public, private := rt.playerStruckFrames(divisionID, character, struck, nowMs)
	public = append(public, rt.petCounterStun(petCounterHit{division: divisionID, owner: character,
		victim: enterworld.ObjectIDForCharacter(snapshot), attacker: instance.Gid,
		formulas: strike.formulas[:len(struck.impacts)], now: nowMs})...)
	// The damage effects ran inside the hit outcome, before the hit landed, so a fatal
	// hit still returns its share; the attacker takes it afterwards.
	returned := rt.returnDamageToMonster(divisionID, character, instance, in.from, skill.ID, defender, strike.formulas[:len(struck.impacts)], nowMs)
	// The shares fence and Pain Quota links took off the hits land on their
	// takers as this monster's hits (linkeddamage.go).
	returned = mergeOpResults(returned, rt.strikeLinkedShares(divisionID, instance.Gid, strike.killer, skill, struck.linkMoves, nowMs, rt.tickDoors()))
	// A Scream Mask on the victim rolls its statuses on this monster (screammask.go).
	returned = mergeOpResults(returned, rt.screamMaskMonster(divisionID, character, instance, in.from, nowMs))
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
	// which the native hostility test never treats as hostile (a pick pet COS).
	if ref.TidWord>>11 == domain.PickupPetBand {
		return monsterStrikeOutcome{alive: true, refusal: simulation.MonsterAttackCommandRejected}
	}
	divisionID, instance, skill, nowMs := in.division, in.instance, in.skill, in.now
	out := monsterStrikeOutcome{alive: true}
	// A companion attacker (a player's pet in PvP) reuses this lane: only
	// a population monster is a monster source for the pet's trained
	// defenses, Resolve gains and hit-taken bonuses (checklist item 3).
	populationMonster := false
	if rt.Monsters != nil {
		_, populationMonster = rt.Monsters.Get(divisionID, instance.Gid)
	}
	if populationMonster && rt.spiritBraced(owner, pet) {
		skill.Knockback.Present = false
	}
	ownerBlock := rt.newCosAbnormalOwnerForPet(divisionID, owner, pet, nowMs)
	defender, err := cosCombatStats(ref, pet, ownerBlock.block, rt.cosDebuffWords(divisionID, owner.Name, pet.GID, nowMs))
	if err != nil {
		return out
	}
	// Companion attackers also reuse this function. Only a population
	// monster may encounter the pilot's defensive gear bonus.
	if populationMonster {
		defender = rt.petTrainingDefense(owner, pet, ref.MaxHP, defender)
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
			continue // natively no damage and no status roll
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
	hit := cosHit{monsterSource: populationMonster, resolveHits: populationMonster, owner: owner, pet: pet, ref: ref, pose: pose, block: ownerBlock, formulas: formulas, records: records,
		displacement: displacement, displaceAt: displaceAt,
		context: abnormal.HitContext{Attack: skill.ReplacementPinned && skill.Replacement.MatchesExecutionSelector}}
	hit.block.sources = rt.captureAbnormalSourcesLife(divisionID, hit.block.block, records)
	if !rt.deps.UpdateDeferred(owner, "monster-cos-hit", func() bool {
		if !rt.applyCOSHitInDoor(divisionID, &hit, nowMs) {
			return false
		}
		live := owner.CompanionByGID(pet.GID)
		if live == nil {
			return true // Fatal hits can retire a transport before training updates.
		}
		previousRows := live.Training.Rows
		if hit.monsterSource {
			for _, formula := range hit.strike.impacts {
				if !formula.Blocked && formula.Damage > 0 {
					rt.petTrainedHitTaken(owner, live)
					break
				}
			}
		}
		if live.Training.Rows != previousRows {
			hit.strike.private = append(hit.strike.private, rt.petTrainingStateFrames(live, nowMs)...)
		}
		return true
	}) {
		return out
	}
	out.strike, out.committed, out.alive = rt.publishCOSHit(&hit), true, !hit.strike.fatal
	if populationMonster && petStrikeDamaged(hit.strike) {
		rt.petGearProcs(divisionID, owner, pet.GID, instance.Gid, in.origin(), true, nowMs)
	}
	out.strike.public = append(out.strike.public, rt.petCounterStun(petCounterHit{division: divisionID, owner: owner,
		victim: pet.GID, attacker: instance.Gid, formulas: formulas[:len(hit.strike.impacts)], now: nowMs})...)
	return out
}

/*
================
cosHit

One planned hit on a companion: its formulas, status records and
displacement, judged on the COS's own reference and abnormal block, then
applied inside a door that holds the owner (applyCOSHitInDoor). A monster's
strike and a player's share it.
================
*/
type cosHit struct {
	resolveHits   bool
	monsterSource bool
	owner         *enterworld.Character
	pet           *enterworld.CharacterCOS
	ref           *enterworld.CharacterRef
	pose          simulation.Spawn
	block         *cosAbnormalOwner
	formulas      []combat.Result
	records       []abnormal.Record
	displacement  *playerDisplacement
	displaceAt    int
	context       abnormal.HitContext
	// winnerName names the player who dealt this hit ("" when the dealer
	// is a monster or a companion): a fatal hit's transport cargo is
	// reserved to the winner (cosdeath.go).
	winnerName string
	// Filled by applyCOSHitInDoor.
	strike    monsterStrike
	remaining uint32
	battle    []wire.Frame
}

/*
================
applyCOSHitInDoor

Inside a door holding the owner: the hit lands on the live companion, the
owner enters battle (the reference server's COS hit path enters the
owner into battle on COS+0x1CD8 before the pet takes the
hit, fatal or not), then the displacement and the status records. False
leaves everything unchanged: the companion left or died first.
================
*/
func (rt *Runtime) applyCOSHitInDoor(divisionID string, hit *cosHit, nowMs int64) bool {
	owner, pet := hit.owner, hit.pet
	live := owner.CompanionByGID(pet.GID)
	if live == nil || live.GID != pet.GID || live.CurrentHP == 0 {
		return false
	}
	strike := monsterStrike{gid: pet.GID, owner: owner, pose: hit.pose}
	if enterworld.CharacterAlive(owner) {
		hit.battle = rt.enterBattleState(divisionID, owner, nowMs)
	}
	// Resolve stack gains collect across the impacts and publish once,
	// after the loop: a strike that turns fatal mid-loop must not emit
	// 0x3FB5 stack-gain frames for a bear that died with stacks earned
	// by its earlier impacts (checklist item 1).
	var spiritFrames, spiritPrivate []wire.Frame
	for _, formula := range hit.formulas {
		hit.context.Magical = hit.context.Magical || formula.MagicalDamage != 0
		if hit.monsterSource && rt.petShieldRecipientAllowed(owner) {
			formula.Damage = rt.absorbPetShield(divisionID, pet.GID, formula.Damage, nowMs)
			formula.Damage = rt.petTrainedDamageTaken(divisionID, owner, live, formula.Damage, nowMs)
		}
		landed := !formula.Blocked && hit.formulas[len(strike.impacts)].Damage > 0
		live.CurrentHP -= vitals.HitDebit(live.CurrentHP, formula.Damage)
		if hit.resolveHits && landed && live.CurrentHP > 0 {
			blockFrames := rt.gainPetSpirit(divisionID, owner, live, nowMs)
			spiritFrames = append(spiritFrames, blockFrames...)
			if len(blockFrames) > 0 {
				spiritPrivate = append(spiritPrivate, rt.petTrainingStateFrames(live, nowMs)...)
			}
		}
		strike.fatal = live.CurrentHP == 0
		strike.impacts = append(strike.impacts, wire.SkillCastTargetImpact{Damage: formula.Damage,
			Fatal: strike.fatal, Blocked: formula.Blocked, ResultFlags: formula.ResultFlags})
		if strike.fatal {
			break
		}
	}
	if !strike.fatal && live.CurrentHP > 0 {
		hit.block.public = append(hit.block.public, spiritFrames...)
		hit.block.private = append(hit.block.private, spiritPrivate...)
	}
	block := hit.block
	if ref, ok := rt.cosReference(live); ok {
		block.ref = ref
	}
	if !strike.fatal && hit.displacement != nil && hit.displaceAt < len(strike.impacts) {
		if point, ok := rt.commitCOSDisplacement(block, hit.displacement); ok {
			if hit.displacement.down {
				strike.impacts[hit.displaceAt].Knockdown = point
			} else {
				strike.impacts[hit.displaceAt].Knockback = point
			}
		}
	}
	hit.remaining = live.CurrentHP
	if strike.fatal {
		block.changed = block.block.ClearAll(block)
		block.fatal = true
		block.winnerName = hit.winnerName
	} else {
		if block.block.Mask != 0 {
			block.changed = block.block.BreakOnHit(block, hit.context) || block.changed
		}
		for _, record := range hit.records {
			if hit.resolveHits && rt.spiritBraced(owner, live) && (record.Status == abnormal.Slow || record.Status == abnormal.Frostbite) {
				continue
			}
			if block.block.Apply(block, record, nowMs) {
				block.changed = true
			}
		}
	}
	block.commit()
	hit.strike = strike
	return true
}

/*
================
cosDamageResults

A committed COS hit's records in the drain owners' form (the life-steal
recovery sums damage, skilllifesteal.go).
================
*/
func cosDamageResults(hit *cosHit) []simulation.MonsterDamageResult {
	records := make([]simulation.MonsterDamageResult, 0, len(hit.strike.impacts))
	for _, impact := range hit.strike.impacts {
		records = append(records, simulation.MonsterDamageResult{Damage: impact.Damage, Fatal: impact.Fatal})
	}
	return records
}

/*
================
publishCOSHit

A committed companion hit's frames: the pet's HP, its abnormal changes and
the owner's battle entry for everyone, the owner's private frames for the
owner.
================
*/
func (rt *Runtime) publishCOSHit(hit *cosHit) monsterStrike {
	strike := hit.strike
	strike.public = append(strike.public, wire.Frame{
		Opcode:  simulation.OpVitalsUpdate,
		Payload: simulation.HPRefreshPayload(hit.pet.GID, 0, hit.remaining),
	})
	strike.public = append(strike.public, rt.cosAbnormalPublication(hit.pet.GID, hit.block)...)
	strike.public = append(strike.public, hit.battle...)
	strike.private = append(strike.private, hit.block.private...)
	return strike
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
(as the native hit outcome writes them), and a walled player's absorb group follows it under its gid
(the hit outcome appends it to the result's target list).
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
