/*
===========================================================================

monstercombat.go - monster attacks on players

Admit authored attacks and source facts under the division owner, commit
target HP and status changes together, then publish the resulting wire frames.

===========================================================================
*/

package action

import (
	"fmt"
	"math"
	"sync/atomic"

	"opensro.online/server/internal/domain"
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
requested id must still belong to that set; zero samples uniformly from
only the complete attack rows, so malformed data can never become a
visual-only monster swing.
==================
*/
func (rt *Runtime) MonsterAttackPlan(instance monster.Instance, requestedSkillID uint32, sample float64) (simulation.MonsterAttackPlan, bool) {
	var zero simulation.MonsterAttackPlan
	if !instance.Ref.RewardActionPinned || rt.deps.SkillData() == nil {
		return zero, false
	}
	if requestedSkillID == 0 {
		if plan, ok := rt.monsterSummonPlan(instance, sample); ok {
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
	if len(valid) == 0 {
		return zero, false
	}
	if sample < 0 || math.IsNaN(sample) {
		sample = 0
	}
	if sample >= 1 {
		sample = math.Nextafter(1, 0)
	}
	skill := valid[int(sample*float64(len(valid)))]
	actionLifecycleMs, _ := skill.ActionLifecycleMs()
	return simulation.MonsterAttackPlan{
		SkillID: skill.ID, Reach: rt.monsterActionReach(instance, skill), CooldownMs: int64(skill.CooldownDurationMs((monsterAbnormalContext{rt}).Param(instance, actionSpeedParameter))),
		ActionLifecycleMs: int64(actionLifecycleMs),
	}, true
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
		if release == nil && result.Accepted {
			// 5A1A40 reads the live action-speed keeper, including Frostbite
			// and Slow. The verified helper owns its float32 store boundaries.
			speed := (monsterAbnormalContext{rt}).Param(instance, 0x8c)
			rt.Monsters.CompleteMonsterSkillCommand(divisionID, instance.Gid, skill.AICommandDurationMs(speed), nowMs)
		}
	}()
	character := rt.findCharacterByGid(divisionID, targetGid)
	if character == nil {
		// 529929: when the mob's hostility byte is set and the player has a
		// COS, the target object becomes PC+0x1CD8. A hit addressed to that
		// gid lands on the pet, including the 590680 status roll.
		if owner := rt.characterByCosGID(divisionID, targetGid); owner != nil {
			return rt.monsterHitSummonedCOS(divisionID, instance, owner, skillID, nowMs, release)
		}
		return result
	}
	snapshot := rt.characterSnapshot(divisionID, character)
	if snapshot == nil || snapshot.DeletePending || !enterworld.CharacterAlive(snapshot) {
		return result
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
		split, resolveErr := rt.resolveCombatBehindWall(criticalActor{division: divisionID, monster: instance.Gid}, skill, attacker, defender, wallRule)
		formula := split.Defender
		if resolveErr != nil || formula.Damage == 0 && !walled && !formula.Blocked {
			return result
		}
		splits = append(splits, wallSplit{absorbed: split.Absorbed, flags: formula.ResultFlags, covered: split.Covered})
		formulas = append(formulas, formula)
		if formula.Blocked {
			continue // 5905FB: no status roll for a blocked impact
		}
		records, rollErr := rt.rollMonsterOnPlayer(divisionID, instance, &skill.Abnormal, snapshot, defender, wallRule)
		if rollErr != nil {
			return result
		}
		abnormalRecords = append(abnormalRecords, records...)
	}
	if len(formulas) == 0 {
		return result
	}
	abnormalOwner := rt.newPlayerAbnormalOwner(divisionID, character, nowMs)
	abnormalOwner.sources = rt.captureAbnormalSources(divisionID, abnormalOwner.block, abnormalRecords)

	impacts := make([]wire.SkillCastTargetImpact, 0, len(formulas))
	var absorbRecords []wire.SkillCastTargetImpact
	var fatal bool
	var deathProgressionFrames []wire.Frame
	var deathEffectFrames []wire.Frame
	var battleFrames []wire.Frame
	// 593AE8: a record a standing wall absorbs (+0x10) skips the recipient
	// branch; any other landed record reaches it.
	struck := false
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
			remaining -= debit
			fatal = remaining == 0
			impacts = append(impacts, wire.SkillCastTargetImpact{
				ResultFlags: formula.ResultFlags,
				// Native 585664 serializes the full hit independently of HP.
				Damage:  formula.Damage,
				Fatal:   fatal,
				Blocked: formula.Blocked,
			})
			if fatal {
				break
			}
		}
		character.CurrentHP = &remaining
		struck = len(impacts) > 0
		if walled && !skill.WallBypass {
			var absorbed uint32
			absorbRecords, absorbed = wallRecords(wall, splits, len(impacts))
			rt.drainWall(divisionID, character.Name, wall.token, absorbed)
			struck = !allWallAbsorbed(absorbRecords)
		}
		if fatal {
			deathEffectFrames, deathProgressionFrames = rt.settlePlayerDeathInDoor(divisionID, character, nowMs)
			abnormalOwner = rt.clearPlayerAbnormalInDoor(divisionID, character, nowMs)
		} else {
			battleFrames = rt.enterBattleState(divisionID, character, nowMs)
			abnormalOwner.applyHit(hitContext, abnormalRecords)
			// 58F72F: a landed hit tests the victim's skc damage masks.
			rt.cancelEffectsOnDamage(divisionID, character, skill.Attack.Flags, nowMs)
		}
		return len(impacts) > 0
	})
	if !committed || len(impacts) == 0 {
		fresh := rt.characterSnapshot(divisionID, character)
		result.TargetAlive = enterworld.CharacterAlive(fresh)
		if result.TargetAlive && !fresh.DeletePending &&
			!monster.AllowsTargetStatus(instance.Ref.TidWord, instance.Nest.NativeTacticsFlags, fresh.NativeBodyStatus) {
			result.Refusal = simulation.MonsterAttackCommandRejected
		}
		return result
	}
	var token uint32
	if fatal {
		rt.bindResidentRegion(simulation.WorldKey(divisionID, character.Name), nowMs)
	}
	abnormalFrames := rt.playerAbnormalPublication(divisionID, character, abnormalOwner)
	if release != nil {
		token = release.token
	} else {
		token = atomic.AddUint32(&rt.castTokenCounter, 1)
	}
	wireResult := wire.NewStationarySkillCastSingleTargetResult(
		wire.SkillCastSuccess{
			SkillId: skillID, CasterGid: instance.Gid, InstanceToken: token,
		},
		targetGid,
		impacts,
	)
	if absorbRecords != nil {
		wireResult = wireResult.WithAbsorb(absorbRecords)
	}
	frame := wire.SkillCastSingleTargetResultFrame(wireResult)
	flight := projectileFlightMs(simulation.Spawn{RegionID: monsterPose.RegionID, X: monsterPose.X, Y: monsterPose.Y, Z: monsterPose.Z}, playerPose, skill.ProjectileSpeed)
	closeAt := nowMs + max(int64(actionLifecycleMs), flight+1)
	if release == nil {
		rt.queueSkillFinalize(divisionID, fmt.Sprintf("@monster:%d", instance.Gid), instance.Gid, nowMs, wire.SkillCastReleaseFrame(token, targetGid))
	} else {
		frame = wire.SkillCastReleaseResultFrame(wireResult)
		closeAt = nowMs + max(int64(skill.ActionDurationMs), flight+1)
	}
	rt.queueSkillFinalize(
		divisionID,
		fmt.Sprintf("@monster:%d", instance.Gid),
		instance.Gid,
		closeAt,
		wire.SkillCastFinalizeFrame(token),
	)
	result.Frames = []simulation.Frame{{Opcode: frame.Opcode, Payload: frame.Payload, Current: frame.Current, Scope: frame.Scope}}
	for _, f := range abnormalFrames.public {
		result.Frames = append(result.Frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
	}
	for _, f := range abnormalFrames.actor {
		result.TargetFrames = append(result.TargetFrames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
	}
	if fatal {
		if rt.PushCharacterFrames != nil && rt.PushDivisionPeerFrames != nil {
			// Native death retires effects before publishing the life change.
			// Enqueue under the action lock, before a rebirth/new application can
			// reuse these wire tokens. The simulation still owns combat delivery.
			rt.publishBodyStatus(divisionID, character.Name, deathEffectFrames)
		} else {
			for _, ended := range deathEffectFrames {
				result.Frames = append(result.Frames, simulation.Frame{Opcode: ended.Opcode, Payload: ended.Payload})
			}
		}
		lifePublication := beginFatalLifePublication(targetGid)
		// B245/B505 retains fatal damage until the client impact callback. The
		// death-sourced 0x33A6 then advances its native wire baseline to zero
		// without applying that damage twice. Present-point rebirth depends on
		// this baseline: its source-zero 0x33A6 restores effective HP by delta.
		baseline := lifePublication.publishDeathBaseline()
		result.Frames = append(result.Frames, simulation.Frame{
			Opcode: baseline.opcode, Payload: baseline.payload,
		})

		// 0x3122 owns the durable life-state transition, death timer, motion,
		// and rebirth UI. It intentionally does not own HP reconciliation.
		death := lifePublication.publishDead()
		result.Frames = append(result.Frames, simulation.Frame{
			Opcode: death.opcode, Payload: death.payload,
		})
	}
	// Entering battle (4E1DF0, from ProcessNormalHit) follows the hit.
	if struck && !fatal {
		for _, f := range rt.offensiveResultRecipient(divisionID, character, nowMs) {
			result.Frames = append(result.Frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload})
		}
	}
	for _, f := range battleFrames {
		result.Frames = append(result.Frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload})
	}
	result.Accepted = true
	result.TargetAlive = !fatal
	for _, progression := range deathProgressionFrames {
		result.TargetFrames = append(result.TargetFrames, simulation.Frame{
			Opcode: progression.Opcode, Payload: progression.Payload,
		})
	}
	return result
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
