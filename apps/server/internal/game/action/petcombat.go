/*
===========================================================================

petcombat.go - an attack pet fights the monster its owner orders it to

The owner's COS attack order (C->S 0x70C5 tag 2, client 0x769E) reaches
CGObjCOS_DispatchOwnedCommand (4D2200). For a summoned attack pet it posts
AI event 0x19 to the pet's own AI::CTactics, whose state handler
(CAIState_OnOwnerAttackOrder 5595C0) sets the combat target and enters the
BATTLE state - the same state machine monsters fight in. The follow order
(event 0x1A, CAIState_OnOwnerFollowOrder 559600) leaves BATTLE again.

This module owns that per-pet combat intent. The pet tick (petai.go) pursues
the target with the pet's movement owner and strikes through the shared
combat formula and the one monster HP door, crediting the kill to the owner
so experience, party sharing and loot need no pet-specific path. A player
target is struck through the player-victim path with the owner as killer
(4E6590: a COS killer stands for its owner), and only when the owner may
attack it (5293A0).

Deviation, recorded deliberately: native monsters retaliate against the pet
that hit them. The port's monster acquisition targets players only, so the
hit is recorded against the owner until monsters can pursue a companion.

===========================================================================
*/

package action

import (
	"fmt"
	"strings"
	"sync/atomic"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// attackPetBand is the attack pet's COS band (TID word >> 11).
	attackPetBand = 3
	// actionRangeCutParameter is the parameter (0xB7) that shortens an
	// action's range, as monsterActionReach applies it to monsters.
	actionRangeCutParameter = 0xb7
	// crtRandRange is the CRT rand() span the native interval jitter uses.
	crtRandRange = 32768
)

/*
================
petCombatIntent

The BATTLE state of one attack pet: its target, the skill it retains while
it closes in (5472A0 keeps the selected skill through approach) and the
strategy interval of 5619E0.
================
*/
type petCombatIntent struct {
	target       uint32
	skillID      uint32
	intervalMs   uint32
	nextAttackMs int64
}

/*
================
orderPetAttack

AI event 0x19 for an attack pet: admit the order and set the combat
target. An order the pet cannot follow is dropped silently; the native
refusal bytes for this reply are not pinned for v1.150 (see the 0xB69E
note in item/wire/coscommand.go).
================
*/
func (rt *Runtime) orderPetAttack(division string, character, snapshot *enterworld.Character, cosGID, targetGID uint32, nowMs int64) OpResult {
	pet := snapshot.CompanionByGID(cosGID)
	if pet == nil || !pet.Summoned || pet.Mounted || pet.CurrentHP == 0 || targetGID == 0 {
		return OpResult{}
	}
	if ref, ok := rt.cosReference(pet); !ok || ref.TidWord>>11 != attackPetBand {
		return OpResult{}
	}
	target, ok := rt.resolveCombatTarget(division, snapshot, targetGID, nowMs)
	if !ok {
		return OpResult{}
	}
	if target.player != nil && rt.playerAttackTargetRefusal(division, snapshot, target.snapshot, nowMs) != 0 {
		return OpResult{}
	}
	state := rt.petSessionFor(division, character.Name, cosGID)
	if state == nil {
		return OpResult{}
	}
	if state.combat != nil && state.combat.target == targetGID {
		return OpResult{}
	}
	// CAIState_BATTLE_OnTargetChanged keeps the strategy; a new BATTLE
	// starts with a fresh one. Either way the next strike is due now.
	intent := &petCombatIntent{target: targetGID, nextAttackMs: nowMs}
	if state.combat != nil {
		intent.intervalMs = state.combat.intervalMs
	}
	state.combat = intent
	// Entering BATTLE leaves PICKITEM: a pending pickup ends unanswered.
	state.pickup = nil
	// 4D2200 retires the owner's effects with event bit 2 when it posts the
	// order, the same retirement a skill cast performs (59B745).
	rt.deps.Update(character, "pet-attack-order", func() bool {
		rt.retireEffectsOnEvent(division, character, effectEventSkillCast, nowMs)
		return true
	})
	return OpResult{}
}

/*
================
petSessionFor
================
*/
func (rt *Runtime) petSessionFor(division, name string, cosGID uint32) *petSession {
	rt.petMu.Lock()
	defer rt.petMu.Unlock()
	return rt.petSessions[petOwnerKey{division: division, name: strings.ToLower(name), gid: cosGID}]
}

/*
================
petCombatStep

One BATTLE tick for an attack pet: pursue the target until it is inside
the action's reach, then strike when the strategy interval allows. Returns
handled=false when the pet has no live target, so the caller follows the
owner instead.
================
*/
type petCombatStep struct {
	key        petOwnerKey
	state      *petSession
	snapshot   *enterworld.Character
	pet        *enterworld.CharacterCOS
	ref        *enterworld.CharacterRef
	run        float32
	constraint func(simulation.Spawn, simulation.Spawn) (simulation.Spawn, *simulation.MoveError)
	nowMs      int64
}

/*
================
advancePetCombat
================
*/
func (rt *Runtime) advancePetCombat(step petCombatStep) ([]simulation.Frame, bool) {
	intent := step.state.combat
	if intent == nil || step.ref.TidWord>>11 != attackPetBand {
		return nil, false
	}
	target, ok := rt.resolveCombatTarget(step.key.division, step.snapshot, intent.target, step.nowMs)
	if !ok || target.player != nil && rt.playerAttackTargetRefusal(step.key.division, step.snapshot, target.snapshot, step.nowMs) != 0 {
		step.state.combat = nil
		return nil, false
	}
	owner := rt.liveSpawn(simulation.WorldKey(step.key.division, step.snapshot.Name), step.snapshot, step.nowMs)
	targetAt := target.at
	targetRadius := float64(0)
	if target.monster != nil {
		targetRadius = target.monster.BodyRadius()
	} else if radius, valid := rt.deps.CharacterBodyRadius(target.snapshot); valid {
		targetRadius = radius
	}
	// INFERENCE: the pet's BATTLE leash is its owner's neighbourhood. A target
	// that leaves the owner's sector and its neighbours (the same
	// Pos_AreSamePlaneAndAdjacentSectors test 4D2200 applies to its position
	// order) ends the fight and the pet returns to follow.
	if !samePlaneAdjacent(owner, targetAt) {
		step.state.combat = nil
		return nil, false
	}
	skill, ok := rt.petAttackSkill(step, intent)
	if !ok {
		step.state.combat = nil
		return nil, false
	}
	block := rt.cosAbnormal(step.key.division, step.snapshot.Name, step.pet.GID)
	spacing := simulation.CombatSpacing{
		ActorBodyRadius:  simulation.BodyRadius(step.ref.Parameters.BodyRadius),
		TargetBodyRadius: simulation.BodyRadius(targetRadius),
		ActionReach:      reducedActionReach(float32(skill.ActionRange), cosParameter(step.ref, step.pet, block, actionRangeCutParameter)),
	}
	if !spacing.Valid() {
		step.state.combat = nil
		return nil, false
	}
	at := step.state.follower.Position(step.nowMs)
	if !spacing.Contains(at, targetAt) {
		return step.state.follower.Approach(targetAt, float64(step.run), step.nowMs, spacing.StandOffRadius(), step.constraint), true
	}
	frames := step.state.follower.Stop(step.nowMs)
	if step.nowMs < intent.nextAttackMs {
		return frames, true
	}
	var strike petStrikeResult
	if target.player != nil {
		strike, ok = rt.petStrikePlayer(step, target, skill)
	} else {
		strike, ok = rt.petStrike(step, *target.monster, skill, targetAt)
	}
	if !ok {
		return frames, true
	}
	intent.nextAttackMs = step.nowMs + int64(intent.intervalMs)
	// Select the next authored action on the next due strike.
	intent.skillID = 0
	if strike.fatal {
		step.state.combat = nil
	}
	return append(frames, strike.frames...), true
}

/*
================
petAttackSkill

MonsterAttackPlan's selection over the pet's own default skills: a retained
skill stays chosen; otherwise sample uniformly among the complete, instant,
targeted attack rows. Choosing resamples the strategy interval (5619E0), as
adoptMonsterAttack does on a skill change.
================
*/
func (rt *Runtime) petAttackSkill(step petCombatStep, intent *petCombatIntent) (enterworld.SkillRow, bool) {
	skills := rt.deps.SkillData()
	if skills == nil {
		return enterworld.SkillRow{}, false
	}
	if intent.skillID != 0 {
		if row, ok := skills.SkillByID(intent.skillID); ok {
			return row, true
		}
	}
	valid := make([]enterworld.SkillRow, 0, len(step.ref.Parameters.DefaultSkillIDs))
	for _, id := range step.ref.Parameters.DefaultSkillIDs {
		row, ok := skills.SkillByID(id)
		lifecycle, pinned := row.ActionLifecycleMs()
		// Casting skills need the prepare/release lane; the authored pet
		// attacks are instant, so only those are admitted here.
		if id == 0 || !ok || !row.CombatPinned || !row.Attack.Present || !row.TargetRequired ||
			!pinned || lifecycle == 0 || !row.TimingPinned || row.CoolTimeMs == 0 ||
			!row.ActionRangePinned || row.ActionRange <= 0 || row.ActionCastingTimeMs != 0 {
			continue
		}
		valid = append(valid, row)
	}
	if len(valid) == 0 || rt.CombatRoll == nil {
		return enterworld.SkillRow{}, false
	}
	draw, err := rt.CombatRoll()
	if err != nil {
		return enterworld.SkillRow{}, false
	}
	choice := valid[int(draw)%len(valid)]
	block := rt.cosAbnormal(step.key.division, step.snapshot.Name, step.pet.GID)
	cooldown := choice.CooldownDurationMs(cosParameter(step.ref, step.pet, block, actionSpeedParameter))
	jitter, err := rt.CombatRoll()
	if err != nil {
		return enterworld.SkillRow{}, false
	}
	intent.skillID = choice.ID
	intent.intervalMs = monster.NextAttackInterval(intent.intervalMs, uint32(cooldown), jitter%crtRandRange)
	return choice, true
}

/*
================
petStrikeResult
================
*/
type petStrikeResult struct {
	frames []simulation.Frame
	fatal  bool
}

/*
================
petStrike

One instant pet hit on a monster. The formula is the shared resolver with
the pet's combat projection; damage goes through the one monster HP door
with the owner as the reward credit, so experience, party sharing and loot
follow the ordinary kill settlement. The cast is published under the pet's
GID, so every viewer animates the pet.
================
*/
func (rt *Runtime) petStrike(step petCombatStep, target monster.Instance, skill enterworld.SkillRow, targetAt simulation.Spawn) (petStrikeResult, bool) {
	division, owner := step.key.division, step.state.character
	block := rt.cosAbnormal(division, step.snapshot.Name, step.pet.GID)
	attacker, err := cosCombatStats(step.ref, step.pet, block)
	if err != nil {
		return petStrikeResult{}, false
	}
	defender, err := monsterDefenderStats(target, step.nowMs)
	if err != nil {
		return petStrikeResult{}, false
	}
	actor := criticalActor{division: division, monster: step.pet.GID}
	formulas := make([]combat.Result, 0, skill.Attack.ImpactCount)
	for range skill.Attack.ImpactCount {
		formula, err := rt.resolveCombat(actor, skill, attacker, defender)
		if err != nil {
			return petStrikeResult{}, false
		}
		formulas = append(formulas, combat.FinishImpact(formula, combat.ImpactTail{Attack: skill.Attack.Present}))
	}
	if len(formulas) == 0 {
		return petStrikeResult{}, false
	}
	ownerGID := enterworld.ObjectIDForCharacter(step.snapshot)
	remaining := target.CurrentHP
	plans := make([]simulation.MonsterDamagePlan, 0, len(formulas))
	for _, formula := range formulas {
		plan := simulation.MonsterDamagePlan{GID: target.Gid, ExpectedHP: remaining, Damage: formula.Damage, CreditGID: ownerGID}
		if formula.Blocked {
			// 5905FB: a blocked impact deals no damage.
			plan.Damage = 0
		}
		plans = append(plans, plan)
		remaining -= min(remaining, plan.Damage)
		if remaining == 0 {
			break
		}
	}
	monsterPose := monster.Pose{RegionID: targetAt.RegionID, X: targetAt.X, Y: targetAt.Y, Z: targetAt.Z}
	roster := rt.monsterRewardRoster(division, owner, step.nowMs)
	var committed []simulation.MonsterDamageResult
	var settlement monsterSettlement
	if !rt.deps.UpdateMany(roster.characters, "pet-attack", func() bool {
		committed = rt.Monsters.ApplyDamageSequence(division, target.Gid, target.CurrentHP, plans)
		if len(committed) == 0 {
			return false
		}
		if committed[len(committed)-1].Fatal {
			settlement = rt.settleMonsterInsideDoor(division, owner, roster, committed[len(committed)-1], monsterPose, step.nowMs)
		}
		return true
	}) {
		return petStrikeResult{}, false
	}
	rt.commitSkillHostility(division, ownerGID, target.Gid, skill, committed, step.nowMs)

	token := atomic.AddUint32(&rt.castTokenCounter, 1)
	impacts := make([]wire.SkillCastTargetImpact, 0, len(committed))
	for index, applied := range committed {
		impacts = append(impacts, committedSkillImpact(formulas[index], applied))
	}
	success := wire.SkillCastSingleTargetResultFrame(wire.NewStationarySkillCastSingleTargetResult(
		wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: step.pet.GID, InstanceToken: token},
		target.Gid,
		impacts,
	))
	// A pet's cast brackets are its own, like a monster's ("@monster:<gid>"),
	// never the owner's.
	bracket := fmt.Sprintf("@pet:%d", step.pet.GID)
	lifecycle, _ := skill.ActionLifecycleMs()
	from := step.state.follower.Position(step.nowMs)
	closeAt := step.nowMs + max(int64(lifecycle), projectileFlightMs(from, targetAt, skill.ProjectileSpeed)+1)
	rt.queueSkillFinalize(division, bracket, step.pet.GID, step.nowMs, wire.SkillCastReleaseFrame(token, target.Gid))
	rt.queueSkillFinalize(division, bracket, step.pet.GID, closeAt, wire.SkillCastFinalizeFrame(token))

	fatal := committed[len(committed)-1].Fatal
	public := []wire.Frame{success}
	actorFrames := []wire.Frame{success}
	if fatal {
		rt.queueMonsterDefeat(division, target.Gid, step.nowMs+monsterDeathPresentationRetention.Milliseconds())
		burst := rt.monsterKillBurst(target.Gid, settlement.drops)
		public = append(public, burst...)
		actorFrames = append(append(actorFrames, burst...), settlement.actorFrames...)
		public = append(public, settlement.public...)
		actorFrames = append(actorFrames, settlement.otherPublic...)
		step.state.others = append(step.state.others, settlement.others...)
	}
	step.state.public = append(step.state.public, public...)
	return petStrikeResult{frames: simFrames(actorFrames), fatal: fatal}, true
}

/*
================
petStrikePlayer

One instant pet hit on a player: the pet's formula behind the victim's
wall and its statuses (590680 with the pet's level), then the recipient
side in a door holding the owner and the victim, the owner standing for
the pet as attacker and killer. Published under the pet's GID.
================
*/
func (rt *Runtime) petStrikePlayer(step petCombatStep, target combatTarget, skill enterworld.SkillRow) (petStrikeResult, bool) {
	division, owner, victim := step.key.division, step.state.character, target.snapshot
	block := rt.cosAbnormal(division, step.snapshot.Name, step.pet.GID)
	attacker, err := cosCombatStats(step.ref, step.pet, block)
	if err != nil {
		return petStrikeResult{}, false
	}
	defender, _, err := rt.playerCombatStats(division, victim)
	if err != nil {
		return petStrikeResult{}, false
	}
	actor := criticalActor{division: division, monster: step.pet.GID}
	level := step.pet.Level
	if level == 0 {
		level = step.ref.Level
	}
	hit := playerHit{target: target, defender: defender, kill: rt.classifyPlayerKill(division, owner, victim)}
	hit.strike = playerStrike{division: division, victim: target.player, killer: deathKiller{player: owner, strikerLevel: int64(level)}, skill: skill, now: step.nowMs}
	if !rt.planPlayerStrike(&hit.strike,
		func(wall *enterworld.SkillWall) (combat.WallOutcome, error) {
			return rt.resolveCombatBehindWall(actor, skill, attacker, defender, wall)
		},
		func(wall *enterworld.SkillWall, _ combat.Result) ([]abnormal.Record, error) {
			return rt.rollCreatureOnPlayer(division, step.pet.GID, level, &skill.Abnormal, victim, defender, wall)
		}) {
		return petStrikeResult{}, false
	}
	from := step.state.follower.Position(step.nowMs)
	if err := rt.planStrikeDisplacement(&hit.strike, actor, from, target.at); err != nil {
		return petStrikeResult{}, false
	}
	var commit playerHitCommit
	if !rt.deps.UpdateMany([]*enterworld.Character{owner, target.player}, "pet-attack-player", func() bool {
		commit = rt.commitPlayerHitInDoor(division, owner, &hit, step.nowMs)
		return len(hit.struck.impacts) > 0
	}) {
		return petStrikeResult{}, false
	}
	token := atomic.AddUint32(&rt.castTokenCounter, 1)
	result := wire.NewStationarySkillCastSingleTargetResult(
		wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: step.pet.GID, InstanceToken: token}, target.gid, hit.struck.impacts)
	if hit.struck.absorb != nil {
		result = result.WithAbsorb(hit.struck.absorb)
	}
	success := wire.SkillCastSingleTargetResultFrame(result)
	bracket := fmt.Sprintf("@pet:%d", step.pet.GID)
	lifecycle, _ := skill.ActionLifecycleMs()
	closeAt := step.nowMs + max(int64(lifecycle), projectileFlightMs(from, target.at, skill.ProjectileSpeed)+1)
	rt.queueSkillFinalize(division, bracket, step.pet.GID, step.nowMs, wire.SkillCastReleaseFrame(token, target.gid))
	rt.queueSkillFinalize(division, bracket, step.pet.GID, closeAt, wire.SkillCastFinalizeFrame(token))
	public, recipient := rt.publishPlayerHit(division, hit, step.nowMs)
	public = append([]wire.Frame{success}, public...)
	public = append(public, commit.public...)
	actorFrames := append(append([]wire.Frame(nil), public...), commit.actor...)
	step.state.public = append(step.state.public, public...)
	step.state.others = append(step.state.others, recipient)
	step.state.others = append(step.state.others, rt.payJobKillShares(commit.shares)...)
	return petStrikeResult{frames: simFrames(actorFrames), fatal: hit.struck.fatal}, true
}

/*
================
monsterKillBurst

The fatal burst every killer publishes after its hit: the monster's dead
life state, then the references and spawns of the loot it dropped.
================
*/
func (rt *Runtime) monsterKillBurst(gid uint32, drops []grounditem.Item) []wire.Frame {
	frames := []wire.Frame{monsterLifeDeadFrame(gid)}
	frames = append(frames, rt.groundReferences(drops)...)
	for _, drop := range drops {
		// Admit the same spawn to each session's scope so later ownership
		// release and expiry can reach every viewer of the earned loot.
		frames = append(frames, wire.DropBroadcastFrames(drop.SpawnRow(true))...)
	}
	return frames
}

/*
================
monsterDefenderStats

A monster's defensive projection at the moment it is struck.
================
*/
func monsterDefenderStats(target monster.Instance, nowMs int64) (combat.Stats, error) {
	stats, err := combat.MonsterInstanceStats(target)
	if err != nil {
		return stats, err
	}
	stats.MotionState = target.Motion.StateAt(nowMs)
	return stats, nil
}
