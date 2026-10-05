/*
===========================================================================

monstercombat.go - monster attacks on players

Admit authored attacks and source facts under the division owner, commit
target HP and status changes together, then publish the resulting wire frames.

===========================================================================
*/

package action

import (
	"math"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
monsterOwnsDefaultSkill

Reject retained requests for skills outside the monster's authored attack set.
================
*/
func monsterOwnsDefaultSkill(instance monster.Instance, skillID uint32) bool {
	for _, candidate := range instance.Ref.DefaultSkillIDs {
		if candidate == skillID {
			return true
		}
	}
	return false
}

/*
==================
MonsterAttackPlan

MonsterAttackPlan resolves the authored RefObjChar default-skill set. A
requested id must still belong to that set; zero makes 561B00's weighted
choice among only the complete attack rows, so malformed data can never
become a visual-only monster swing.
==================
*/
func (rt *Runtime) MonsterAttackPlan(instance monster.Instance, requestedSkillID uint32, pick simulation.AttackPick) (simulation.MonsterAttackPlan, bool) {
	var zero simulation.MonsterAttackPlan
	// Structures do not act as monsters do; a guard tower's fire is the
	// fortress war's (fortress structures never chase or retaliate).
	if instance.Ref.Structure || !instance.Ref.RewardActionPinned || rt.deps.SkillData() == nil {
		return zero, false
	}
	if requestedSkillID == 0 {
		if plan, ok := rt.monsterSummonPlan(instance, pick.Sample); ok {
			return plan, true
		}
	} else if monsterOwnsDefaultSkill(instance, requestedSkillID) {
		// 5472A0 reads the active skill before dispatching the selector.
		// A retained summon must not change bands (or replace a retained
		// ordinary attack) when damage/HP changes during approach.
		row, exists := rt.deps.SkillData().SkillByID(requestedSkillID)
		if exists && row.MonsterSelfEffect.Pinned {
			duration, valid := row.ActionLifecycleMs()
			if !valid || duration == 0 || row.CoolTimeMs == 0 {
				return zero, false
			}
			return simulation.MonsterAttackPlan{SkillID: row.ID, SelfEffect: true, CooldownMs: int64(row.CooldownDurationMs((monsterAbnormalContext{rt}).Param(instance, actionSpeedParameter))), ActionLifecycleMs: int64(duration)}, true
		}
		if exists && row.Summon.Present {
			duration, valid := row.ActionLifecycleMs()
			if !valid || !row.TimingPinned || row.CoolTimeMs == 0 {
				return zero, false
			}
			return simulation.MonsterAttackPlan{SkillID: row.ID, Summon: true, CooldownMs: int64(row.CooldownDurationMs((monsterAbnormalContext{rt}).Param(instance, actionSpeedParameter))), ActionLifecycleMs: int64(duration)}, true
		}
	}
	valid := make([]enterworld.SkillRow, 0, len(instance.Ref.DefaultSkillIDs))
	for _, skillID := range instance.Ref.DefaultSkillIDs {
		if skillID == 0 || (requestedSkillID != 0 && skillID != requestedSkillID) {
			continue
		}
		skill, ok := rt.deps.SkillData().SkillByID(skillID)
		actionLifecycleMs, actionLifecyclePinned := skill.ActionLifecycleMs()
		if !ok || !skill.CombatPinned || !skill.Attack.Present ||
			!skill.TargetRequired || !actionLifecyclePinned || actionLifecycleMs == 0 ||
			!skill.TimingPinned || skill.CoolTimeMs == 0 ||
			!skill.ActionRangePinned || skill.ActionRange <= 0 {
			continue
		}
		valid = append(valid, skill)
	}
	var skill enterworld.SkillRow
	if requestedSkillID != 0 {
		// A retained skill is reused as it is (5472A0); its weight was read
		// when it was chosen.
		if len(valid) == 0 {
			return zero, false
		}
		skill = valid[0]
	} else {
		chosen := false
		if skill, chosen = monsterWeightedSkill(instance, valid, pick); !chosen {
			return zero, false
		}
	}
	actionLifecycleMs, _ := skill.ActionLifecycleMs()
	return simulation.MonsterAttackPlan{
		SkillID: skill.ID, Reach: rt.monsterActionReach(instance, skill), CooldownMs: int64(skill.CooldownDurationMs((monsterAbnormalContext{rt}).Param(instance, actionSpeedParameter))),
		ActionLifecycleMs: int64(actionLifecycleMs),
	}, true
}

/*
==================
monsterWeightedSkill

CAISkill_Basic_SelectConditionalThenWeighted (561B00): each default skill
with a non-zero weight (RefSkill +0x164, column 66) enters with that
weight, raised by half of what its reach - the target's body radius, the
skill's range and the monster's own - exceeds the target's distance
(truncated). One CRT draw modulo the total plus one then takes the first
skill whose running total reaches it. A choice without a target weighs
the authored weights alone; with no weighted skill there is no choice.
==================
*/
func monsterWeightedSkill(instance monster.Instance, skills []enterworld.SkillRow, pick simulation.AttackPick) (enterworld.SkillRow, bool) {
	totals := make([]uint32, len(skills))
	total := uint32(0)
	for i, skill := range skills {
		if skill.AIWeight == 0 {
			continue
		}
		weight := uint32(skill.AIWeight)
		if pick.Target != nil {
			reach := float32(pick.Target.BodyRadius + float64(uint16(skill.ActionRange)) + instance.BodyRadius())
			slack := float32(float64(reach) - float64(pick.Target.Distance))
			if slack >= 0 {
				weight += uint32(int32(float64(slack) * 5.0 / 10.0))
			}
		}
		total += weight
		totals[i] = total
	}
	if total == 0 {
		return enterworld.SkillRow{}, false
	}
	draw := monster.SummonRandomWord(pick.Sample) % (total + 1)
	for i, skill := range skills {
		if totals[i] != 0 && totals[i] >= draw {
			return skill, true
		}
	}
	return enterworld.SkillRow{}, false
}

/*
==================
MonsterBasicAttack

MonsterBasicAttack commits one authoritative monster->player hit. The
result uses the same B245/B505 client conversation as player combat, which
is what activates the retail-derived animation, BSR sound events, hit VFX,
floating damage, hit reaction, and fatal/death presentation.
==================
*/
func (rt *Runtime) MonsterBasicAttack(
	divisionID string,
	instance monster.Instance,
	targetGid, skillID uint32,
	nowMs int64,
) simulation.MonsterAttackResult {
	unlock := rt.lockDivision(divisionID)
	defer unlock()
	return rt.monsterAttackStage(divisionID, instance, targetGid, skillID, nowMs, nil)
}

/*
================
monsterAttackStage

Resolve all cross-character facts before the target transaction. The division
lock spans admission, HP/status commit and publication ordering.
================
*/
func (rt *Runtime) monsterAttackStage(divisionID string, instance monster.Instance, targetGid, skillID uint32, nowMs int64, release *pendingMonsterCast) (result simulation.MonsterAttackResult) {
	if targetGid == 0 || skillID == 0 || rt.Monsters == nil ||
		!monsterOwnsDefaultSkill(instance, skillID) {
		return result
	}
	if live, exists := rt.Monsters.Get(divisionID, instance.Gid); !exists || live.CurrentHP == 0 || live.Motion.StateAt(nowMs) != 0 {
		return result
	} else {
		instance.SelfEffects = live.SelfEffects
		instance.Abnormal = live.Abnormal
	}
	if release != nil {
		if row, exists := rt.deps.SkillData().SkillByID(skillID); exists && row.MonsterSelfEffect.Pinned {
			return rt.releaseMonsterSelfEffect(divisionID, instance, row, nowMs, release)
		}
	}
	skill, ok := rt.deps.SkillData().SkillByID(skillID)
	defer func() {
		if release == nil && result.Refusal == simulation.MonsterAttackCommandRejected && skill.Summon.Present {
			rt.Monsters.RejectSummonCommand(divisionID, instance.Gid, nowMs)
		}
		if result.Accepted {
			// 4C1C30: a job monster's own attack rearms its idle timer.
			rt.Monsters.RefreshJobMonster(divisionID, instance.Gid, nowMs)
		}
		if release == nil && result.Accepted {
			// 5A1A40 reads the live action-speed keeper, including Frostbite
			// and Slow. The verified helper owns its float32 store boundaries.
			speed := (monsterAbnormalContext{rt}).Param(instance, 0x8c)
			rt.Monsters.CompleteMonsterSkillCommand(divisionID, instance.Gid, skill.AICommandDurationMs(speed), nowMs)
		}
	}()
	character := rt.findCharacterByGid(divisionID, targetGid)
	if character == nil {
		// A hit addressed to a COS gid (a mounted rider's ride, redirected
		// below, or a pet a test names) lands on the pet, including the 590680
		// status roll. CGObjMob_EvaluateHostility (529929) only credits the
		// hostility to the owner (COS+0x1CD8); it never moves the damage.
		if owner := rt.characterByCosGID(divisionID, targetGid); owner != nil {
			return rt.monsterHitSummonedCOS(divisionID, instance, monsterCastRecipient{owner, targetGid}, skillID, nowMs, release)
		}
		// A Temptation fight: a tempted monster, or the monster answering
		// it, strikes a monster (temptation.go).
		if target, fight := rt.monsterFightAdmitted(divisionID, instance, targetGid); fight && release == nil {
			return rt.monsterHitMonster(divisionID, instance, target, skillID, nowMs)
		}
		return result
	}
	snapshot := rt.characterSnapshot(divisionID, character)
	if snapshot == nil || snapshot.DeletePending || !enterworld.CharacterAlive(snapshot) {
		return result
	}
	// Only hostile-target skills swap (59B5F3 tests the target flag); summons
	// and self effects keep their own branches below.
	if ride := ridingCOS(snapshot); release == nil && ride != 0 && ok && skill.TargetRequired &&
		!skill.Summon.Present && !skill.MonsterSelfEffect.Pinned {
		return rt.monsterHitSummonedCOS(divisionID, instance, monsterCastRecipient{character, ride}, skillID, nowMs, nil)
	}
	if _, sameWorld := rt.characterMonster(divisionID, snapshot, instance.Gid); !sameWorld {
		return result
	}
	result.TargetAlive = true

	if !monster.AllowsTargetStatus(instance.Ref.TidWord, instance.Nest.NativeTacticsFlags, snapshot.NativeBodyStatus) {
		// 587630 binds ssou to RefSkill+354. The unique completion
		// side effect is applied by MonsterState even on command refusal.
		if ok {
			result.Refusal = simulation.MonsterAttackCommandRejected
		}
		return result
	}
	if ok && skill.Summon.Present {
		return rt.monsterSummon(divisionID, instance, skill, nowMs)
	}
	if ok && skill.MonsterSelfEffect.Pinned {
		mover, exists := rt.Monsters.Mover(divisionID, instance.Gid)
		if !exists {
			return result
		}
		spacing, valid := rt.monsterToPlayerCombatSpacing(instance, snapshot, 0)
		pose := mover.LivePoseAt(nowMs, nil)
		if !valid || !spacing.Contains(simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z}, rt.liveSpawn(simulation.WorldKey(divisionID, snapshot.Name), snapshot, nowMs)) {
			result.Refusal = simulation.MonsterAttackApproachRequired
			return result
		}
		if skill.ActionCastingTimeMs > 0 {
			return rt.prepareMonsterCast(divisionID, instance, monsterCastRecipient{snapshot, enterworld.ObjectIDForCharacter(snapshot)}, skill, nowMs)
		}
		return rt.releaseMonsterSelfEffect(divisionID, instance, skill, nowMs, nil)
	}
	actionLifecycleMs, actionLifecyclePinned := skill.ActionLifecycleMs()
	if !ok || !skill.CombatPinned || !skill.Attack.Present ||
		!skill.TargetRequired || !actionLifecyclePinned || actionLifecycleMs == 0 ||
		!skill.ActionRangePinned || skill.ActionRange <= 0 {
		return result
	}
	mover, ok := rt.Monsters.Mover(divisionID, instance.Gid)
	if !ok {
		return result
	}
	monsterPose := mover.LivePoseAt(nowMs, nil)
	playerPose := rt.liveSpawn(simulation.WorldKey(divisionID, snapshot.Name), snapshot, nowMs)
	spacing, spacingOK := rt.monsterToPlayerCombatSpacing(instance, snapshot, rt.monsterActionReach(instance, skill))
	// SR_GameServer 585C67/585CD1 revalidates an owned cast, not its
	// AutoCommand approach radius. Target movement after admission cannot
	// cancel release. Identity, life, status and coordinate plane still apply.
	if !spacingOK || simulation.IsDungeonRegion(monsterPose.RegionID) != simulation.IsDungeonRegion(playerPose.RegionID) {
		return result
	}
	if release == nil && !spacing.Contains(simulation.Spawn{
		RegionID: monsterPose.RegionID, X: monsterPose.X, Y: monsterPose.Y, Z: monsterPose.Z,
	}, playerPose) {
		// A target can move between the AI snapshot and this action door.
		// The approach owner must retain its skill and reacquire live range,
		// not turn this unissued command into damage failure or lost aggro.
		result.Refusal = simulation.MonsterAttackApproachRequired
		return result
	}
	attacker, err := combat.MonsterInstanceStats(instance)
	if err != nil {
		return result
	}
	defender, _, err := rt.playerCombatStats(divisionID, snapshot)
	if err != nil {
		return result
	}
	if release == nil && skill.ActionCastingTimeMs > 0 {
		return rt.prepareMonsterCast(divisionID, instance, monsterCastRecipient{snapshot, enterworld.ObjectIDForCharacter(snapshot)}, skill, nowMs)
	}
	in := monsterStrikeInput{division: divisionID, instance: instance, skill: skill, attacker: attacker, from: monsterPose, percent: fullAreaPercent, now: nowMs}
	outcome := rt.monsterStrikePlayer(in, character, snapshot, defender, playerPose)
	if !outcome.committed {
		result.TargetAlive, result.Refusal = outcome.alive, outcome.refusal
		return result
	}
	return rt.publishMonsterStrikes(monsterPublication{strike: in, release: release}, outcome.strike)
}

/*
================
findCharacterByGid

Invert ordinary player IDs through the indexed store, preserving the original
scan for clamped boundary fixtures. This lookup can acquire the store read lock.
================
*/
func (rt *Runtime) findCharacterByGid(divisionID string, gid uint32) *enterworld.Character {
	// Interior IDs invert exactly. Preserve the legacy clamped-ID lookup for
	// boundary fixtures rather than guessing which exceptional record owns it.
	if gid > domain.PlayerGIDBase && gid < domain.PlayerGIDBase+math.MaxInt32 {
		if source, ok := rt.deps.(domain.CharacterLookup); ok {
			return source.CharacterByID(divisionID, int64(gid-domain.PlayerGIDBase))
		}
	}
	for _, character := range rt.deps.CharactersForDivision(divisionID) {
		if character != nil && enterworld.ObjectIDForCharacter(character) == gid {
			return character
		}
	}
	return nil
}
