/*
===========================================================================

skillcombat.go - offensive skill casts against monsters

Own single-target release, resource commit, and cast-token retirement. Shared
action admission precedes mutation; the token outlives the projectile flight.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/world/monster"
	"sync/atomic"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/linkedpulse"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
skillCastDecision

skillCastDecision is the authority result behind an OpResult. An empty
frame slice is not a state transition: it can mean either a terminally
invalid command or a valid command that must wait for range/the preceding
action bracket. Basic attack continuations consume this distinction so a
moving target cannot turn a transient range miss into a cancelled intent.
==================
*/
type skillCastDecision uint8

const (
	skillCastRefused skillCastDecision = iota
	skillCastDeferred
	skillCastAccepted
)

/*
==================
skillCastPostureBlocked

skillCastPostureBlocked is the server-owned action-admission edge. The
client can compose 0x72CD while seated and has no presentation-side posture
gate, so the world plane must reject both the settled SIT state and the
invisible sit/stand transition before an action actor, movement handoff, HP
mutation, or B245 bracket exists.

A fresh press from a seat never gets here: standForSeatedCommand stands
the player first (seatedcommand.go has the native case). This refusal
covers the continuations - approach, release and resumed attacks - that
meet a seat or a transition later. playerCastBlocked adds the frozen, sleeping or stunned caster.
==================
*/
func (rt *Runtime) skillCastPostureBlocked(
	divisionID string,
	character *enterworld.Character,
	nowMs int64,
) bool {
	if character == nil || rt.Worlds == nil {
		return true
	}
	world := rt.Worlds.Snapshot(
		simulation.WorldKey(divisionID, character.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(character) },
	)
	// A knocked-down player's commands wait out its hold (motion 8).
	return world.Sitting || nowMs < world.PostureTransitionUntilMs || rt.playerCastBlocked(divisionID, character.Name) ||
		rt.PlayerKnockedDown(divisionID, character.Name, nowMs)
}

/*
==================
acceptSkillCast

acceptSkillCast owns learned racial base attacks and admitted direct-damage
offensive skills against one live monster. It snapshots
player stats, derives equipped-item ParamKeeper contributions, evaluates
the pinned v1.188 normal-result formula, commits HP through the monster
registry's sole mutation door, and serializes that committed outcome.

Unsupported shapes fail closed before a token is minted. In particular,
there is no visual-only success fallback: an unpinned status/enhancement
plane may not look like authoritative combat on the wire.
==================
*/
func (rt *Runtime) acceptSkillCast(
	divisionID string,
	character *enterworld.Character,
	snapshot *enterworld.Character,
	cast wire.SkillAction,
) OpResult {
	result, _ := rt.acceptSkillCastAt(
		divisionID,
		character,
		snapshot,
		cast,
		rt.Now().UnixMilli(),
		rt.handlerDoors(),
	)
	return result
}

/*
==================
acceptSkillCastAt

acceptSkillCastAt evaluates and commits one cast at one authoritative
world instant. Callers that already own a simulation-tick timestamp pass it
through; sampling rt.Now again inside this door would let a moving target
be admitted at one pose and refused at another pose in the same logical
transition.
==================
*/
func (rt *Runtime) acceptSkillCastAt(
	divisionID string,
	character *enterworld.Character,
	snapshot *enterworld.Character,
	cast wire.SkillAction,
	nowMs int64,
	doors characterDoors,
) (OpResult, skillCastDecision) {
	return rt.acceptSkillStageAt(divisionID, character, snapshot, cast, nowMs, 0, doors)
}

/*
================
linkedStageTargetRefusal

A linked stage (a combo's next row) beyond reach. The native cast handler returns it to
action state 2, which executes without measuring range (only state 1
pursues), and natively the target is asked only for the same plane and adjacent
sectors. Returns 0 to execute, else the refusal code.
================
*/
func linkedStageTargetRefusal(caster, target simulation.Spawn) uint16 {
	if !samePlaneAdjacent(caster, target) {
		return 0x3006
	}
	return 0
}

/*
================
acceptSkillStageAt

Enter a fresh stage without a prepared execution-cost snapshot.
================
*/
func (rt *Runtime) acceptSkillStageAt(divisionID string, character, snapshot *enterworld.Character, cast wire.SkillAction, nowMs int64, rootID uint32, doors characterDoors) (OpResult, skillCastDecision) {
	return rt.acceptSkillStagePhaseAt(divisionID, character, snapshot, cast, nowMs, rootID, nil, doors)
}

/*
================
acceptSkillStagePhaseAt

Share admission, range and preparation before choosing the release producer.
Persistent attacks install linked pairs; immediate attacks commit their hits.
================
*/
func (rt *Runtime) acceptSkillStagePhaseAt(divisionID string, character, snapshot *enterworld.Character, cast wire.SkillAction, nowMs int64, rootID uint32, release *pendingProjectileCast, doors characterDoors) (OpResult, skillCastDecision) {
	if character == nil || snapshot == nil || rt.Monsters == nil || !cast.HasTarget ||
		cast.HasGroundTarget || cast.TargetGid == 0 {
		return OpResult{}, skillCastRefused
	}
	if !enterworld.CharacterAlive(snapshot) {
		return OpResult{}, skillCastRefused
	}
	if rt.skillCastPostureBlocked(divisionID, snapshot, nowMs) {
		return OpResult{}, skillCastRefused
	}
	if release == nil && rt.actionAdmissionBlocked(divisionID, snapshot.Name, rootID != 0) {
		return OpResult{}, skillCastDeferred
	}

	skillSource := rt.deps.SkillData()
	// A transformed player's strike is the monster's skill, never learned.
	transformStrike := rootID == 0 && cast.ActionId != 0 && cast.ActionId == rt.transformAttackSkill(snapshot)
	if skillSource == nil || (rootID == 0 && !transformStrike && !enterworld.SkillLearned(snapshot, cast.ActionId)) {
		return OpResult{}, skillCastRefused
	}
	skill, known := skillSource.SkillByID(cast.ActionId)
	basic := transformStrike || isPinnedBaseAttack(snapshot, skill.Codename)
	advanced := false
	if !basic {
		var refusal string
		if rootID == 0 {
			_, _, refusal = rt.resolveOffensiveSkill(snapshot, cast.ActionId)
		} else {
			_, _, refusal = rt.resolveOffensiveStage(snapshot, rootID, cast.ActionId)
		}
		advanced = refusal == ""
		if refusal != "" {
			return rt.offensiveAdmissionRefusal(refusal), skillCastRefused
		}
	}
	actionLifecycleMs, actionLifecyclePinned := skill.ActionLifecycleMs()
	if !known || ((!skill.CombatPinned || !skill.Attack.Present) && !skill.TimedEffect.Periodic.Pinned && !skill.Threat.Only && !skill.StatusCast && !skill.FixedDamage.Present && !skill.LifeSteal.Present && !skill.HostileDebuff.Pinned) ||
		!actionLifecyclePinned || actionLifecycleMs == 0 && !skill.PositionEffect.Charge ||
		!skill.TargetRequired || (!basic && !advanced) {
		return OpResult{}, skillCastRefused
	}

	attacker, loadout, err := rt.playerAttackStats(divisionID, snapshot, skill)
	if err != nil {
		return OpResult{}, skillCastRefused
	}
	if release != nil {
		attacker.StealthStrike = release.stealthStrike
	}
	consumeAmmo := (basic && !transformStrike && weaponRequiresAmmunition(loadout.WeaponKind)) || (advanced && skill.Ammunition.Count != 0)
	if consumeAmmo {
		// The native prerequisite check refuses any skill whose cnsm requirement is unmet with
		// 0x300E, the basic attack included: SKILL_CH_BOW_BASE and
		// SKILL_EU_CROSSBOW_BASE carry cnsm 4/1 and 4/2. The client shows
		// the out-of-ammunition notice for it, as it does for skills.
		if _, valid := rt.planEquippedAmmunition(snapshot, loadout.WeaponKind, ammunitionSpent(skill, advanced)); !valid {
			return offensiveRefusal(0x300e), skillCastRefused
		}
	}
	actionReach := skillActionReach(skill, loadout, attacker)
	if actionReach <= 0 {
		return OpResult{}, skillCastRefused
	}

	target, ok := rt.characterMonster(divisionID, snapshot, cast.TargetGid)
	if !ok {
		// Not a monster of this world: a player, or nothing (pvpstrike.go).
		return rt.acceptPlayerTargetStage(offensiveStage{division: divisionID, character: character, snapshot: snapshot,
			cast: cast, skill: skill, basic: basic, advanced: advanced, rootID: rootID, release: release,
			attacker: attacker, loadout: loadout, consumeAmmo: consumeAmmo, actionReach: actionReach,
			lifecycleMs: actionLifecycleMs, now: nowMs, doors: doors})
	}
	if target.CurrentHP == 0 {
		return OpResult{}, skillCastRefused
	}
	if code := temptationTargetRefusal(skill, target); code != 0 {
		return offensiveRefusal(code), skillCastRefused
	}
	if code := rt.jobMonsterTargetRefusal(snapshot, target); code != 0 {
		return offensiveRefusal(code), skillCastRefused
	}
	if d := skill.TimedEffect.Periodic; d.Pinned {
		code := rt.periodicEffects.Refusal(linkedpulse.Effect{Division: divisionID,
			SourceGID: enterworld.ObjectIDForCharacter(snapshot), TargetGID: target.Gid,
			SkillID: skill.ID, LinkGroup: d.Link.Group, MaxPerTarget: d.Link.MaxOutgoing,
			DurationMs: d.DurationMs, PeriodMs: d.PeriodMs})
		if code != 0 {
			return offensiveRefusal(code), skillCastRefused
		}
	}
	defender, err := combat.MonsterInstanceStats(target)
	defender.MotionState = target.Motion.StateAt(nowMs)
	if err != nil {
		return OpResult{}, skillCastRefused
	}
	// Natively the linked row returns to admission state 2. Each stage
	// checks and pays its own resources; root ownership only skips the shared cooldown.
	mask := admitExecution
	if rootID != 0 {
		mask &^= admitCooldown
	}
	mover, ok := rt.Monsters.Mover(divisionID, target.Gid)
	if !ok {
		return OpResult{}, skillCastRefused
	}
	monsterPose := mover.LivePoseAt(nowMs, nil)
	struck := &admitTarget{
		motion: defender.MotionState,
		at:     simulation.Spawn{RegionID: monsterPose.RegionID, X: monsterPose.X, Y: monsterPose.Y, Z: monsterPose.Z},
	}
	if target.Ref.Structure {
		if code := rt.structureAttackRefusal(divisionID, snapshot, target, skill.ID, nowMs); code != 0 {
			return offensiveRefusal(code), skillCastRefused
		}
	}
	if code := rt.skillAdmission(divisionID, snapshot, skill, nowMs, struck, release, mask); code != 0 {
		return offensiveRefusal(code), skillCastRefused
	}
	if advanced {
		if _, refusal := rt.stagePhaseCost(divisionID, snapshot, skill, nowMs, release, rootID); refusal != 0 {
			return offensiveRefusal(refusal), skillCastRefused
		}
	}
	playerPose := rt.liveSpawn(
		simulation.WorldKey(divisionID, snapshot.Name),
		snapshot,
		nowMs,
	)
	spacing, spacingOK := rt.playerToMonsterCombatSpacing(snapshot, target, actionReach)
	// The approach radius admits a new cast. An already-owned release uses
	// the native target validation, not another range test.
	// Keep this symmetric with monsterAttackStage; fleeing is not cast cancel.
	if !spacingOK || !worldgeom.SamePlane(playerPose.RegionID, monsterPose.RegionID) {
		return OpResult{}, skillCastDeferred
	}
	targetAt := simulation.Spawn{RegionID: monsterPose.RegionID, X: monsterPose.X, Y: monsterPose.Y, Z: monsterPose.Z}
	if release == nil && !spacing.Contains(playerPose, targetAt) {
		if rootID == 0 {
			return OpResult{}, skillCastDeferred
		}
		if code := linkedStageTargetRefusal(playerPose, targetAt); code != 0 {
			return offensiveRefusal(code), skillCastRefused
		}
	}
	// Natively the attack starting here aims the caster's hawk.
	if release == nil && rootID == 0 {
		rt.noteHawkOwnerAttack(divisionID, snapshot, target.Gid, nowMs)
	}
	if skill.ActionCastingTimeMs != 0 && release == nil {
		return rt.prepareOffensiveCast(divisionID, character, snapshot, cast, skill, advanced, rootID, nowMs, doors)
	}

	primary := combatTarget{gid: target.Gid, monster: &target, at: targetAt}
	if skill.TimedEffect.Periodic.Pinned {
		return rt.installPeriodicCast(periodicCast{division: divisionID, character: character,
			snapshot: snapshot, skill: skill, cast: cast, target: primary, attacker: attacker, now: nowMs, release: release})
	}
	_, precision := rt.crowPlayerBonuses(divisionID, snapshot, target.Gid, skill, nowMs)
	attacker.HitRate *= 1 + precision/100
	formulas := make([]combat.Result, 0, skill.Attack.ImpactCount)
	if skill.Threat.Only {
		return rt.releaseTaunt(tauntCast{division: divisionID, character: character, snapshot: snapshot,
			skill: skill, primary: target, now: nowMs})
	}
	if skill.HostileDebuff.Pinned {
		return rt.debuffMonster(debuffCast{division: divisionID, character: character, snapshot: snapshot,
			skill: skill, target: target.Gid, now: nowMs})
	}
	if skill.OffensiveArea.Radius != 0 {
		return rt.acceptSkillAreaAt(divisionID, character, snapshot, skill, skill.OffensiveArea, false, true, primary, attacker, loadout, consumeAmmo, nowMs, rootID, release, doors)
	}
	// With no area of its own, an imbue-eligible attack
	// (att value 5) that passes the execution selector selects its victims with the active
	// imbue's efr (the Lightning Force); the chain's victims are flagged.
	if skill.Attack.Value5 != 0 && skill.ReplacementPinned && skill.Replacement.MatchesExecutionSelector {
		if imbue, _ := rt.activeWeaponImbue(divisionID, snapshot.Name, nowMs); imbue.Pinned && imbue.Area.Radius != 0 {
			return rt.acceptSkillAreaAt(divisionID, character, snapshot, skill, imbue.Area, true, advanced, primary, attacker, loadout, consumeAmmo, nowMs, rootID, release, doors)
		}
	}
	stealBase, stealOK := int64(0), true
	if skill.LifeSteal.Present {
		stealBase, stealOK = rt.lifeStealBase(divisionID, snapshot, skill.LifeSteal, attacker)
	}
	if !stealOK {
		return OpResult{}, skillCastRefused
	}
	for range skill.Attack.ImpactCount {
		if skill.LifeSteal.Present {
			steal := lifeStealResult(stealBase, attacker, defender, target.CurrentHP, fullAreaPercent)
			formulas = append(formulas, combat.FinishImpact(steal, combat.ImpactTail{PercentApplied: true, Attack: skill.Attack.Present}))
			continue
		}
		formula, resolveErr := rt.resolvePlayerImpact(divisionID, snapshot.Name, skill, attacker, defender, nowMs, false)
		if resolveErr != nil {
			return OpResult{}, skillCastRefused
		}
		formulas = append(formulas, combat.FinishImpact(formula, combat.ImpactTail{Attack: skill.Attack.Present}))
	}
	if len(formulas) == 0 {
		return OpResult{}, skillCastRefused
	}
	rt.penguinOwnerShatter(divisionID, snapshot, target, formulas, nowMs)
	rt.crowPlayerDamage(divisionID, snapshot, target, skill, formulas, nowMs)
	damagePlans, planned := rt.planMonsterImpacts(divisionID, snapshot, skill, target, formulas, nowMs)
	if !planned {
		return OpResult{}, skillCastRefused
	}
	var travel skillTravelPlan
	if skill.PositionEffect.Charge {
		from, owner := rt.liveNav(simulation.WorldKey(divisionID, snapshot.Name), snapshot, nowMs)
		radius, valid := rt.deps.CharacterBodyRadius(snapshot)
		goal, admitted := chargeSkillGoal(from, targetAt, skill.PositionEffect.Range, radius+target.BodyRadius())
		if !valid || !admitted {
			return OpResult{}, skillCastRefused
		}
		travel, planned = rt.planSkillTravel(snapshot.Name, from, owner, goal)
		if !planned {
			return OpResult{DiagnosticRefusal: "charge-navigation-refused"}, skillCastRefused
		}
	}
	var killDrops []grounditem.Item
	// The reward roster serves the settlement alone, and a strike
	// whose planned damage cannot kill reaches none (#2030): door the
	// caster alone and build no roster. A plan that can kill keeps
	// the roster-wide door exactly as before. The sequence refuses
	// outright when the target's HP moved off the planned value and
	// applied damage never exceeds planned damage, so a fatal impact
	// implies a lethal plan; settleSkillStrikeFatal guards the rest.
	var plannedDamage uint64
	for _, plan := range damagePlans {
		plannedDamage += uint64(plan.Damage)
	}
	canKill := plannedDamage >= uint64(target.CurrentHP)
	roster := rewardRoster{division: divisionID, characters: []*enterworld.Character{character}}
	if canKill {
		roster = rt.monsterRewardRoster(divisionID, character, nowMs)
	}
	var settlement monsterSettlement
	committed := make([]simulation.MonsterDamageResult, 0, len(formulas))
	var ammo ammunitionResult
	var killProgressionFrames []wire.Frame
	var battleFrames []wire.Frame
	var tuning, stolen wire.Frame
	var refusal uint16
	{
		// Character ammo and monster HP move under the same per-division lock
		// and the same character authority closure. Validation is read-only;
		// debit is the non-refusing tail after the HP authority accepts damage.
		if !doors.updateMany(roster.characters, "player-basic-attack", func() bool {
			var cost skillCharge
			if advanced {
				cost, refusal = rt.stagePhaseCost(divisionID, character, skill, nowMs, release, rootID)
				if refusal != 0 {
					return false
				}
			}
			debit, admitted := ammunitionDebit{index: -1}, true
			if consumeAmmo {
				debit, admitted = rt.planEquippedAmmunition(character, loadout.WeaponKind, ammunitionSpent(skill, advanced))
			}
			if !admitted {
				refusal = 0x300e
				return false
			}
			committed = rt.Monsters.ApplyDamageSequence(divisionID, target.Gid, target.CurrentHP, damagePlans)
			if len(committed) == 0 {
				return false
			}
			if release == nil {
				rt.startSkillCast(divisionID, character, skill, nowMs)
			}
			battleFrames = rt.enterBattleState(divisionID, character, nowMs)
			if consumeAmmo {
				ammo = applyAmmunitionDebit(character, debit)
			}
			if advanced {
				rt.commitOffensivePhaseCost(divisionID, character, skill, cost, nowMs, release != nil)
			} else if release == nil {
				rt.registerPlayerSkillCooldown(divisionID, character, skill, nowMs)
			}
			if skill.FixedDamage.Present {
				// After the cost: the drained MP refills the gauge the
				// cast just spent (skilltuning.go).
				tuning = rt.commitTuningMana(divisionID, character, skill.FixedDamage, committed)
			}
			if skill.LifeSteal.Present {
				// The caster recovers the life taken (skilllifesteal.go).
				stolen = rt.commitLifeSteal(divisionID, character, committed)
			}
			if skill.PositionEffect.Charge {
				rt.commitSkillTravel(simulation.WorldKey(divisionID, character.Name), character, travel)
			}
			if committed[len(committed)-1].Fatal {
				settlement = rt.settleSkillStrikeFatal(divisionID, character, roster, canKill, committed[len(committed)-1], monsterPose, nowMs)
				killProgressionFrames, killDrops = settlement.actorFrames, settlement.drops
			}
			return true
		}) {
			if refusal != 0 {
				return offensiveRefusal(refusal), skillCastRefused
			}
			return OpResult{}, skillCastRefused
		}
	}
	finalImpact := committed[len(committed)-1]
	if skill.PositionEffect.Charge {
		rt.bindResidentRegion(simulation.WorldKey(divisionID, character.Name), nowMs)
	}
	rt.commitSkillHostility(divisionID, enterworld.ObjectIDForCharacter(snapshot), target.Gid, skill, committed, nowMs)

	var instanceToken uint32
	if release != nil {
		instanceToken = release.token
	} else {
		instanceToken = atomic.AddUint32(&rt.castTokenCounter, 1)
	}
	impacts := make([]wire.SkillCastTargetImpact, 0, len(committed))
	for index, applied := range committed {
		impacts = append(impacts, committedSkillImpact(formulas[index], applied))
	}
	wireResult := wire.NewStationarySkillCastSingleTargetResult(
		wire.SkillCastSuccess{
			SkillId:       cast.ActionId,
			CasterGid:     enterworld.ObjectIDForCharacter(snapshot),
			InstanceToken: instanceToken,
		},
		target.Gid,
		impacts,
	)
	if skill.PositionEffect.Charge {
		wireResult = wire.NewSkillCastSingleTargetResult(wire.SkillCastSuccess{SkillId: skill.ID,
			CasterGid: enterworld.ObjectIDForCharacter(snapshot), InstanceToken: instanceToken}, target.Gid, impacts, travel.point)
	}
	success := wire.SkillCastSingleTargetResultFrame(wireResult)
	closeAt := nowMs + int64(actionLifecycleMs) - int64(skill.ActionDurationMs) + rt.wolfHowlActionMs(divisionID, snapshot, skill.ActionDurationMs)
	if release == nil {
		rt.queueSkillFinalize(divisionID, snapshot.Name, enterworld.ObjectIDForCharacter(snapshot), nowMs+int64(skill.ActionCastingTimeMs), wire.SkillCastReleaseFrame(instanceToken, target.Gid))
	} else {
		success = wire.SkillCastReleaseResultFrame(wireResult)
		closeAt = nowMs + rt.wolfHowlActionMs(divisionID, snapshot, skill.ActionDurationMs)
	}
	if skill.ActionHandler == enterworld.SkillActionProjectile {
		flight := projectileFlightMs(playerPose, targetAt, skill.ProjectileSpeed)
		// Natively zero-preparation shots are retained too. A fast shot or linked
		// stage must not lose its effect actor before the projectile arrives.
		closeAt = max(closeAt, nowMs+flight+1)
	}
	if !skill.PositionEffect.Charge {
		// Guided travel closes on arrival, just as the standalone tele owner.
		rt.queueSkillCastClose(divisionID, snapshot.Name, enterworld.ObjectIDForCharacter(snapshot), instanceToken, skill, rootID, closeAt)
	}
	if finalImpact.Fatal {
		// The fatal HP transition owns reward authority AND the resulting wire
		// burst. Native reward distributors synchronously invoke the player's
		// experience virtual, whose send completes before that call returns.
		// B505 only releases action/motion ownership. Keep the zero-HP source
		// alive independently for the authored death event that launches the
		// already-staged 0x30D2 particles.
		rt.queueMonsterDefeat(
			divisionID,
			target.Gid,
			nowMs+monsterDeathPresentationRetention.Milliseconds(),
		)
	}
	burnFrames := rt.monsterImpactAbnormalFrames(divisionID, target.Gid, committed)
	for _, impact := range committed {
		if impact.Knockdown != nil || impact.Knockback != nil {
			burnFrames = append(burnFrames, rt.interruptMonsterCast(divisionID, target.Gid)...)
			break
		}
	}
	actorFrames := append([]wire.Frame{success}, burnFrames...)
	broadcastFrames := append([]wire.Frame{success}, burnFrames...)
	if finalImpact.Fatal {
		life := monsterLifeDeadFrame(target.Gid)
		actorFrames = append(actorFrames, life)
		broadcastFrames = append(broadcastFrames, life)
	}
	if finalImpact.Fatal {
		// Port-only shard notice for a seal drop (announce.go), as the area,
		// periodic and credited-hit kills post it.
		rt.announceSealDrops(divisionID, target.Ref.Name, killDrops, nowMs)
		references := rt.groundReferences(killDrops)
		actorFrames = append(actorFrames, references...)
		broadcastFrames = append(broadcastFrames, references...)
		for _, killDrop := range killDrops {
			// Admit the same spawn to each session's scope so later ownership
			// release and expiry can reach every viewer of the earned loot.
			spawn := wire.DropBroadcastFrames(killDrop.SpawnRow(true))
			actorFrames = append(actorFrames, spawn...)
			broadcastFrames = append(broadcastFrames, spawn...)
		}
	}
	if consumeAmmo {
		actorFrames = append(actorFrames, ammunitionFrames(ammo)...)
	}
	// The attacker's unblocked attempts wear its weapon.
	var tally wearTally
	for _, formula := range formulas {
		tally.note(formula.Blocked, true)
	}
	wear := rt.applyEquipmentWear(divisionID, character, tally, doors)
	actorFrames = append(actorFrames, wear.actor...)
	broadcastFrames = append(broadcastFrames, wear.public...)
	// The actor sees the same fatal B245/drop prefix first, then the complete
	// native progression burst. Peers see only its gid-bearing level-up
	// presentation; the private complement is retained for the tick-owned
	// repeat-attack path, which has no synchronous request session to answer.
	privateFrames := wire.ProgressionPrivateFrames(killProgressionFrames)
	if consumeAmmo {
		privateFrames = append(ammunitionFrames(ammo), privateFrames...)
	}
	privateFrames = append(privateFrames, wear.actor...)
	if advanced {
		vitals := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshPayload(enterworld.ObjectIDForCharacter(character), rt.publishedVitals(divisionID, character))}
		actorFrames = append(actorFrames, vitals)
		privateFrames = append(privateFrames, vitals)
	}
	if tuning.Opcode != 0 {
		actorFrames = append(actorFrames, tuning)
		privateFrames = append(privateFrames, tuning)
	}
	if stolen.Opcode != 0 {
		actorFrames = append(actorFrames, stolen)
		broadcastFrames = append(broadcastFrames, stolen)
	}
	actorFrames = append(actorFrames, killProgressionFrames...)
	broadcastFrames = append(broadcastFrames, settlement.public...)
	// Other recipients' public level-up effects also belong in the actor's
	// projection; its own public subset is already in killProgressionFrames.
	actorFrames = append(actorFrames, settlement.otherPublic...)
	// Entering battle is a consequence of the hit, published after
	// its burst like the other damage-application state changes.
	actorFrames = append(actorFrames, battleFrames...)
	broadcastFrames = append(broadcastFrames, battleFrames...)
	travelFrames := rt.tailwindCommittedDamage(divisionID, character, target.Gid, committed, nowMs)
	broadcastFrames = append(broadcastFrames, travelFrames...)
	actorFrames = append(actorFrames, travelFrames...)
	if rootID == 0 {
		for _, impact := range committed {
			if impact.Damage > 0 {
				rt.penguinOwnerHit(divisionID, character, skill, []monster.Instance{target}, formulas, nowMs)
				rt.crowPlayerAttack(divisionID, character, skill, []uint32{target.Gid}, nowMs)
				rt.wolfExecuteAttack(divisionID, character, 0, 1, []uint32{target.Gid}, nowMs)
				break
			}
		}
	}
	return OpResult{
		Frames:       actorFrames,
		Broadcast:    broadcastFrames,
		ActorPrivate: privateFrames,
		Recipients:   settlement.others,
	}, skillCastAccepted
}

/*
================
settleSkillStrikeFatal

The fatal impact of a skill strike, settled inside the strike door.
canKill says the plan was lethal and the roster is the full reward
roster; without it the settlement runs on the caster's narrow roster
and the kill was impossible by the planned-damage invariant, which
noteSurpriseFatal makes audible instead of silent (#2030).
================
*/
func (rt *Runtime) settleSkillStrikeFatal(division string, character *enterworld.Character, roster rewardRoster, canKill bool, impact simulation.MonsterDamageResult, pose monster.Pose, now int64) monsterSettlement {
	if !canKill {
		rt.noteSurpriseFatal(division, impact.Instance.Gid)
	}
	return rt.settleMonsterInsideDoor(division, character, roster, impact, pose, now)
}

/*
================
noteSurpriseFatal

A strike killed under a plan that could not kill: the invariant the
roster gate rests on broke. Count it and log it at Error with the
division and the monster, because the settlement just ran with the
narrow roster and underpaid - this is the sign that is not silence.
================
*/
func (rt *Runtime) noteSurpriseFatal(division string, gid uint32) {
	rt.surpriseFatalCount.Add(1)
	log.WithFields(log.Fields{"division": division, "gid": gid}).Error("strike killed under a non-lethal plan; reward settled with the narrow roster")
}

/*
================
prepareOffensiveCast

A cast with a preparation time charges and cools at the press, then waits
for its release (advanceProjectileCasts), whatever its target's kind.
================
*/
func (rt *Runtime) prepareOffensiveCast(divisionID string, character, snapshot *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, advanced bool, rootID uint32, nowMs int64, doors characterDoors) (OpResult, skillCastDecision) {
	var refusal uint16
	if !doors.update(character, "prepare-offensive-cooldown", func() bool {
		if !enterworld.CharacterAlive(character) {
			return false
		}
		if advanced {
			_, refusal = rt.offensiveCost(divisionID, character, skill, nowMs)
			if refusal != 0 {
				return false
			}
		}
		rt.startSkillCast(divisionID, character, skill, nowMs)
		rt.registerPlayerSkillCooldown(divisionID, character, skill, nowMs)
		return true
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal), skillCastRefused
		}
		return OpResult{DiagnosticRefusal: "offensive-prepare-commit-refused"}, skillCastRefused
	}
	return rt.prepareProjectileCast(divisionID, snapshot, cast, skill, nowMs, rootID), skillCastAccepted
}

/*
==================
chainStageBlocked

chainStageBlocked is the gate a server-owned chain stage waits on. Native
KEEP_UP holds only while the casting instance at
manager+1D8 (char+C08) is set, i.e. a positive-time step has not released.
Queued closes and zero-time releases are presentation, not this command;
the chain's own root bracket in particular must stay open meanwhile.
==================
*/
func (rt *Runtime) chainStageBlocked(divisionID, characterName string) bool {
	rt.pendingSkillFinalizesMu.Lock()
	defer rt.pendingSkillFinalizesMu.Unlock()
	return rt.castingInstanceOpenLocked(simulation.WorldKey(divisionID, characterName))
}

/*
================
actionAdmissionBlocked

A chain stage waits only on the casting instance; another command waits on
every open bracket so the client observes the previous action's close.
================
*/
func (rt *Runtime) actionAdmissionBlocked(divisionID, characterName string, chainStage bool) bool {
	if chainStage {
		return rt.chainStageBlocked(divisionID, characterName)
	}
	return rt.hasOpenSkillCast(divisionID, characterName)
}

/*
================
castingInstanceOpenLocked

The caller owns pendingSkillFinalizesMu while inspecting preparation state.
================
*/
func (rt *Runtime) castingInstanceOpenLocked(ownerKey string) bool {
	if _, current := rt.currentSkillCommands[ownerKey]; current {
		return true
	}
	for _, pending := range rt.pendingProjectileCasts {
		if pending.ownerKey == ownerKey {
			return true
		}
	}
	return false
}

/*
================
hasOpenSkillCast

Preparation and delayed controls independently retain action ownership.
================
*/
func (rt *Runtime) hasOpenSkillCast(divisionID, characterName string) bool {
	rt.pendingSkillFinalizesMu.Lock()
	defer rt.pendingSkillFinalizesMu.Unlock()

	ownerKey := simulation.WorldKey(divisionID, characterName)
	if rt.castingInstanceOpenLocked(ownerKey) {
		return true
	}
	for _, pending := range rt.pendingSkillFinalizes {
		if !pending.presentationOnly && pending.ownerKey == ownerKey {
			return true
		}
	}
	return false
}

/*
==================
danceSelectorActive

danceSelectorActive is the native owner-condition check: bit 0 of
skill-manager +0x1D0. The persistent cast installs it from an active
persistent skill's scls word and clears it when that skill retires, so it is
set exactly while such an effect is live.

Owner's rule: a Dancing needs the dancer inside ANOTHER Bard's music. Only
an instance handed out by someone else's aura counts (a child, with an
AuraParentToken); the dancer's own tambour or march does not. Inferred:
the requirement is the cast's; a running dance does not end when the
music does (it has its own MP, death, loading and hit rules).
==================
*/
func (rt *Runtime) danceSelectorActive(division string, c *enterworld.Character) bool {
	for _, effect := range rt.effects.Snapshot(division, c.Name) {
		if effect.AuraParentToken == 0 || effect.StopRequested {
			continue
		}
		row, ok := rt.deps.SkillData().SkillByID(effect.SkillID)
		if ok && row.SelectorMask&1 != 0 {
			return true
		}
	}
	return false
}
