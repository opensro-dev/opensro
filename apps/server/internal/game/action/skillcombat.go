/*
===========================================================================

skillcombat.go - offensive skill casts against monsters

Own single-target release, resource commit, and cast-token retirement. Shared
action admission precedes mutation; the token outlives the projectile flight.

===========================================================================
*/

package action

import (
	"strings"
	"sync/atomic"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/linkedpulse"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
pendingSkillFinalize

One delayed control packet retains its actor route independently of damage.
================
*/
type pendingSkillFinalize struct {
	sourceGID     uint32
	divisionID    string
	characterName string
	dueAtMs       int64
	frame         wire.Frame
	// chainBracket marks the mode-2 close of a chain ROOT. The v1.150 client
	// (85CB60) appends every linked stage's B245 to that root's deco, which
	// carries the one authored clip for the whole chain; closing it before
	// the last stage turns the remaining stages into result-only temporaries
	// (damage with the caster snapped to idle). Stages re-arm it instead of
	// opening brackets of their own; it never gates the chain's own stages.
	chainBracket bool
}

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
meet a seat or a transition later. playerCastBlocked adds the frozen, sleeping or stunned caster (58DAEF).
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
	return world.Sitting || nowMs < world.PostureTransitionUntilMs || rt.playerCastBlocked(divisionID, character.Name)
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
) (OpResult, skillCastDecision) {
	return rt.acceptSkillStageAt(divisionID, character, snapshot, cast, nowMs, 0)
}

/*
================
acceptSkillStageAt

Enter a fresh stage without a prepared execution-cost snapshot.
================
*/
func (rt *Runtime) acceptSkillStageAt(divisionID string, character, snapshot *enterworld.Character, cast wire.SkillAction, nowMs int64, rootID uint32) (OpResult, skillCastDecision) {
	return rt.acceptSkillStagePhaseAt(divisionID, character, snapshot, cast, nowMs, rootID, nil)
}

/*
================
acceptSkillStagePhaseAt

Share admission, range and preparation before choosing the release producer.
Persistent attacks install linked pairs; immediate attacks commit their hits.
================
*/
func (rt *Runtime) acceptSkillStagePhaseAt(divisionID string, character, snapshot *enterworld.Character, cast wire.SkillAction, nowMs int64, rootID uint32, release *pendingProjectileCast) (OpResult, skillCastDecision) {
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
	if !known || ((!skill.CombatPinned || !skill.Attack.Present) && !skill.TimedEffect.Periodic.Pinned && !skill.Threat.Only) ||
		!actionLifecyclePinned || actionLifecycleMs == 0 && !skill.PositionEffect.Charge ||
		!skill.TargetRequired || (!basic && !advanced) {
		return OpResult{}, skillCastRefused
	}

	attacker, loadout, err := rt.playerCombatStats(divisionID, snapshot)
	if err != nil {
		return OpResult{}, skillCastRefused
	}
	if release != nil {
		attacker.StealthStrike = release.stealthStrike
	}
	consumeAmmo := (basic && !transformStrike && weaponRequiresAmmunition(loadout.WeaponKind)) || (advanced && skill.Ammunition.Count != 0)
	if consumeAmmo {
		if _, valid := rt.planEquippedAmmunition(snapshot, loadout.WeaponKind, ammunitionSpent(skill, advanced)); !valid {
			if basic {
				return OpResult{}, skillCastRefused
			}
			return offensiveRefusal(0x300e), skillCastRefused
		}
	}
	actionReach := skillActionReach(skill, loadout, attacker)
	if actionReach <= 0 {
		return OpResult{}, skillCastRefused
	}

	target, ok := rt.characterMonster(divisionID, snapshot, cast.TargetGid)
	if !ok || target.CurrentHP == 0 {
		return OpResult{}, skillCastRefused
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
	// Execution runs the full mask. A linked stage is charged and cooled by
	// its root, so it drops those two checks.
	mask := admitExecution
	if rootID != 0 {
		mask &^= admitCooldown | admitResources
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
	if code := rt.skillAdmission(divisionID, snapshot, skill, nowMs, struck, release, mask); code != 0 {
		return offensiveRefusal(code), skillCastRefused
	}
	if advanced && rootID == 0 {
		if _, refusal := rt.offensivePhaseCost(divisionID, snapshot, skill, nowMs, release); refusal != 0 {
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
	// the native 585CD1 -> 58D8D0 target validation, not another range test.
	// Keep this symmetric with monsterAttackStage; fleeing is not cast cancel.
	if !spacingOK || simulation.IsDungeonRegion(playerPose.RegionID) != simulation.IsDungeonRegion(monsterPose.RegionID) {
		return OpResult{}, skillCastDeferred
	}
	targetAt := simulation.Spawn{RegionID: monsterPose.RegionID, X: monsterPose.X, Y: monsterPose.Y, Z: monsterPose.Z}
	if release == nil && !spacing.Contains(playerPose, targetAt) {
		return OpResult{}, skillCastDeferred
	}
	if skill.ActionCastingTimeMs != 0 && release == nil {
		var refusal uint16
		if !rt.deps.Update(character, "prepare-offensive-cooldown", func() bool {
			if !enterworld.CharacterAlive(character) {
				return false
			}
			if advanced && rootID == 0 {
				_, refusal = rt.offensiveCost(divisionID, character, skill, nowMs)
				if refusal != 0 {
					return false
				}
			}
			rt.startSkillCast(divisionID, character, nowMs)
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

	if skill.TimedEffect.Periodic.Pinned {
		return rt.installPeriodicCast(periodicCast{division: divisionID, character: character,
			snapshot: snapshot, skill: skill, cast: cast, target: target, attacker: attacker, now: nowMs, release: release})
	}
	formulas := make([]combat.Result, 0, skill.Attack.ImpactCount)
	if skill.Threat.Only {
		return rt.releaseTaunt(tauntCast{division: divisionID, character: character, snapshot: snapshot,
			skill: skill, primary: target, now: nowMs})
	}
	if skill.OffensiveArea.Radius != 0 {
		return rt.acceptSkillAreaAt(divisionID, character, snapshot, skill, skill.OffensiveArea, false, true, target, attacker, loadout, consumeAmmo, nowMs, rootID, release)
	}
	// 586E04..586E1B: with no area of its own, an imbue-eligible attack
	// (att value 5) that passes 589D20 selects its victims with the active
	// imbue's efr (the Lightning Force); the chain's victims are flagged.
	if skill.Attack.Value5 != 0 && skill.ReplacementPinned && skill.Replacement.MatchesExecutionSelector {
		if imbue, _ := rt.activeWeaponImbue(divisionID, snapshot.Name, nowMs); imbue.Pinned && imbue.Area.Radius != 0 {
			return rt.acceptSkillAreaAt(divisionID, character, snapshot, skill, imbue.Area, true, advanced, target, attacker, loadout, consumeAmmo, nowMs, rootID, release)
		}
	}
	for range skill.Attack.ImpactCount {
		formula, resolveErr := rt.resolvePlayerImpact(divisionID, snapshot.Name, skill, attacker, defender, nowMs, false)
		if resolveErr != nil {
			return OpResult{}, skillCastRefused
		}
		formulas = append(formulas, formula)
	}
	if len(formulas) == 0 {
		return OpResult{}, skillCastRefused
	}
	damagePlans, planned := rt.planMonsterImpacts(divisionID, snapshot, skill, target, formulas, nowMs)
	if !planned {
		return OpResult{}, skillCastRefused
	}
	var travel skillTravelPlan
	if skill.PositionEffect.Charge {
		from, owner := rt.liveNav(simulation.WorldKey(divisionID, snapshot.Name), snapshot, nowMs)
		radius, valid := rt.deps.CharacterBodyRadius(snapshot)
		goal, admitted := chargeSkillGoal(from, targetAt, skill.PositionEffect.Range, radius+target.Ref.BodyRadius)
		if !valid || !admitted {
			return OpResult{}, skillCastRefused
		}
		travel, planned = rt.planSkillTravel(snapshot.Name, from, owner, goal)
		if !planned {
			return OpResult{DiagnosticRefusal: "charge-navigation-refused"}, skillCastRefused
		}
	}
	var killDrops []grounditem.Item
	roster := rt.monsterRewardRoster(divisionID, character, nowMs)
	var settlement monsterSettlement
	committed := make([]simulation.MonsterDamageResult, 0, len(formulas))
	var ammoCount uint16
	var killProgressionFrames []wire.Frame
	var battleFrames []wire.Frame
	var refusal uint16
	{
		// Character ammo and monster HP move under the same per-division lock
		// and the same character authority closure. Validation is read-only;
		// debit is the non-refusing tail after the HP authority accepts damage.
		if !rt.deps.UpdateMany(roster.characters, "player-basic-attack", func() bool {
			var cost skillCharge
			if advanced && rootID == 0 {
				cost, refusal = rt.offensivePhaseCost(divisionID, character, skill, nowMs, release)
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
				rt.startSkillCast(divisionID, character, nowMs)
			}
			battleFrames = rt.enterBattleState(divisionID, character, nowMs)
			if consumeAmmo {
				ammoCount = applyAmmunitionDebit(character, debit)
			}
			if advanced && rootID == 0 {
				rt.commitOffensivePhaseCost(divisionID, character, skill, cost, nowMs, release != nil)
			} else if release == nil {
				rt.registerPlayerSkillCooldown(divisionID, character, skill, nowMs)
			}
			if skill.PositionEffect.Charge {
				rt.commitSkillTravel(simulation.WorldKey(divisionID, character.Name), character, travel)
			}
			if committed[len(committed)-1].Fatal {
				settlement = rt.settleMonsterInsideDoor(divisionID, character, roster, committed[len(committed)-1], monsterPose, nowMs)
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
	closeAt := nowMs + int64(actionLifecycleMs)
	if release == nil {
		rt.queueSkillFinalize(divisionID, snapshot.Name, enterworld.ObjectIDForCharacter(snapshot), nowMs+int64(skill.ActionCastingTimeMs), wire.SkillCastReleaseFrame(instanceToken, target.Gid))
	} else {
		success = wire.SkillCastReleaseResultFrame(wireResult)
		closeAt = nowMs + int64(skill.ActionDurationMs)
	}
	if skill.ProjectileSpeed != 0 {
		flight := projectileFlightMs(playerPose, targetAt, skill.ProjectileSpeed)
		// 5860D2 retains zero-preparation shots too. A fast shot or linked
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
		actorFrames = append(actorFrames, wire.AvatarInventorySlot7StackCountFrame(ammoCount))
	}
	// The actor sees the same fatal B245/drop prefix first, then the complete
	// native progression burst. Peers see only its gid-bearing level-up
	// presentation; the private complement is retained for the tick-owned
	// repeat-attack path, which has no synchronous request session to answer.
	privateFrames := wire.ProgressionPrivateFrames(killProgressionFrames)
	if consumeAmmo {
		privateFrames = append([]wire.Frame{wire.AvatarInventorySlot7StackCountFrame(ammoCount)}, privateFrames...)
	}
	if advanced && rootID == 0 {
		vitals := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshPayload(enterworld.ObjectIDForCharacter(character), rt.publishedVitals(divisionID, character))}
		actorFrames = append(actorFrames, vitals)
		privateFrames = append(privateFrames, vitals)
	}
	actorFrames = append(actorFrames, killProgressionFrames...)
	broadcastFrames = append(broadcastFrames, settlement.public...)
	// Other recipients' public level-up effects also belong in the actor's
	// projection; its own public subset is already in killProgressionFrames.
	actorFrames = append(actorFrames, settlement.otherPublic...)
	// Entering battle (4E27C0) is a consequence of the hit, published after
	// its burst like the other damage-application state changes.
	actorFrames = append(actorFrames, battleFrames...)
	broadcastFrames = append(broadcastFrames, battleFrames...)
	return OpResult{
		Frames:       actorFrames,
		Broadcast:    broadcastFrames,
		ActorPrivate: privateFrames,
		Recipients:   settlement.others,
	}, skillCastAccepted
}

/*
================
isPinnedBaseAttack

Resolve the racial seed catalog instead of inferring base attacks from IDs.
================
*/
func isPinnedBaseAttack(character *enterworld.Character, codename string) bool {
	for _, candidate := range enterworld.DefaultSkillCodenames(
		enterworld.ResolveCharacterRaceKey(character),
	) {
		if candidate == codename {
			return true
		}
	}
	return false
}

/*
==================
skillWeaponAdmitted

skillWeaponAdmitted is the weapon half of 58D480: a skill with reqi pairs
is judged by reqiRefusal alone and never reads its weapon kinds
(RefSkill+0xC7/+0xC8); only a skill without reqi compares them.
==================
*/
func skillWeaponAdmitted(loadout combat.Loadout, skill enterworld.SkillRow) bool {
	return skill.Reqi.Present || loadoutMatchesSkill(loadout, skill.RequiredWeaponKinds)
}

/*
================
loadoutMatchesSkill

The authored two-slot weapon requirement includes the bare-hand sentinel.
================
*/
func loadoutMatchesSkill(loadout combat.Loadout, kinds [2]uint8) bool {
	if kinds == [2]uint8{0xff, 0xff} {
		return true
	}
	for _, kind := range kinds {
		switch {
		case kind == 0xff:
			continue
		case kind == 1 && !loadout.HasWeapon:
			return true
		case loadout.HasWeapon && kind == loadout.WeaponKind:
			return true
		}
	}
	return false
}

/*
================
weaponRequiresAmmunition

Only bows and crossbows consume the shared secondary-equipment ammunition.
================
*/
func weaponRequiresAmmunition(kind uint8) bool {
	// RefItemData TID4 6 is the Chinese bow family; 12 is the European
	// crossbow family. Inventory's native socket map places TID 3.3.4
	// arrows/bolts in the shared secondary-equipment socket for those two.
	return kind == 6 || kind == 12
}

/*
================
queueSkillFinalize

Retain control order until the simulation clock reaches the due time.
================
*/
func (rt *Runtime) queueSkillFinalize(
	divisionID string,
	characterName string,
	sourceGID uint32,
	dueAtMs int64,
	frame wire.Frame,
) {
	if sourceGID == 0 {
		panic("cast control requires a source GID")
	}
	rt.pendingSkillFinalizesMu.Lock()
	rt.pendingSkillFinalizes = append(rt.pendingSkillFinalizes, pendingSkillFinalize{
		sourceGID:     sourceGID,
		divisionID:    divisionID,
		characterName: characterName,
		dueAtMs:       dueAtMs,
		frame:         frame,
	})
	rt.pendingSkillFinalizesMu.Unlock()
}

/*
==================
queueSkillCastClose

queueSkillCastClose schedules the mode-2 close for one accepted offensive
action. A chain root's close is its chain bracket; a server-owned stage
(rootID != 0) extends that bracket to its own action end and opens none:
the client never created a cast for the stage token, so a close on it is
a silent miss while the root would still be cut at its own duration.
==================
*/
func (rt *Runtime) queueSkillCastClose(divisionID, characterName string, sourceGID, token uint32, skill enterworld.SkillRow, rootID uint32, closeAtMs int64) {
	if rootID != 0 {
		rt.extendChainBracket(divisionID, characterName, closeAtMs)
		return
	}
	if sourceGID == 0 {
		panic("cast control requires a source GID")
	}
	rt.pendingSkillFinalizesMu.Lock()
	rt.pendingSkillFinalizes = append(rt.pendingSkillFinalizes, pendingSkillFinalize{
		sourceGID:     sourceGID,
		divisionID:    divisionID,
		characterName: characterName,
		dueAtMs:       closeAtMs,
		frame:         wire.SkillCastFinalizeFrame(token),
		chainBracket:  skill.ChainNext != 0,
	})
	rt.pendingSkillFinalizesMu.Unlock()
}

/*
==================
extendChainBracket

extendChainBracket keeps the root deco alive through the stage that was
just accepted. A bracket already drained stays closed; its stages then
resolve as native temporary results, exactly as a late linked B245 would.
==================
*/
func (rt *Runtime) extendChainBracket(divisionID, characterName string, closeAtMs int64) {
	rt.pendingSkillFinalizesMu.Lock()
	defer rt.pendingSkillFinalizesMu.Unlock()
	owner := simulation.WorldKey(divisionID, characterName)
	for i := range rt.pendingSkillFinalizes {
		pending := &rt.pendingSkillFinalizes[i]
		if pending.chainBracket && simulation.WorldKey(pending.divisionID, pending.characterName) == owner {
			pending.dueAtMs = max(pending.dueAtMs, closeAtMs)
		}
	}
}

/*
==================
chainStageBlocked

chainStageBlocked is the gate a server-owned chain stage waits on. Native
KEEP_UP (4AECB6..4AECC9) holds only while the casting instance at
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
		if simulation.WorldKey(pending.divisionID, pending.characterName) == ownerKey {
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
		if simulation.WorldKey(pending.divisionID, pending.characterName) == ownerKey {
			return true
		}
	}
	return false
}

/*
==================
clearSkillFinalizes

clearSkillFinalizes retires the server-owned B245 action brackets for one
disconnected character. A queued B505 belongs to that session lifecycle:
retaining it can block a quick reconnect and later broadcast a stale close.
==================
*/
func (rt *Runtime) clearSkillFinalizes(divisionID, characterName string) {
	rt.pendingSkillFinalizesMu.Lock()
	defer rt.pendingSkillFinalizesMu.Unlock()

	ownerKey := simulation.WorldKey(divisionID, characterName)
	delete(rt.currentSkillCommands, ownerKey)
	monsters := rt.pendingMonsterCasts[:0]
	for _, pending := range rt.pendingMonsterCasts {
		if !pending.selfEffect && simulation.WorldKey(pending.division, pending.characterName) == ownerKey {
			rt.pendingSkillFinalizes = append(rt.pendingSkillFinalizes, pendingSkillFinalize{sourceGID: pending.instance.Gid, divisionID: pending.division, characterName: monsterCastOwner(pending.instance.Gid), frame: wire.SkillCastFinalizeFrame(pending.token)})
		} else {
			monsters = append(monsters, pending)
		}
	}
	rt.pendingMonsterCasts = monsters
	projectiles := rt.pendingProjectileCasts[:0]
	for _, pending := range rt.pendingProjectileCasts {
		if simulation.WorldKey(pending.divisionID, pending.characterName) != ownerKey {
			projectiles = append(projectiles, pending)
		}
	}
	rt.pendingProjectileCasts = projectiles
	kept := rt.pendingSkillFinalizes[:0]
	for _, pending := range rt.pendingSkillFinalizes {
		if simulation.WorldKey(pending.divisionID, pending.characterName) != ownerKey {
			kept = append(kept, pending)
		}
	}
	rt.pendingSkillFinalizes = kept
}

/*
==================
openSkillCastOwnerSnapshot

openSkillCastOwnerSnapshot captures action ownership at the start of one
authoritative simulation-tick transaction. A due finalize may remove the live
queue entry later in that transaction, but repeat admission must not observe
that internal mutation until the next tick: clients need the published B505
release before another B245 can acquire motion-state ownership.

The value reports whether the owner's casting instance is open. Only that
gates a server-owned chain stage (see chainStageBlocked); every entry gates
a new command.
==================
*/
func (rt *Runtime) openSkillCastOwnerSnapshot() map[string]bool {
	rt.pendingSkillFinalizesMu.Lock()
	defer rt.pendingSkillFinalizesMu.Unlock()

	owners := make(map[string]bool, len(rt.pendingSkillFinalizes))
	for _, pending := range rt.pendingSkillFinalizes {
		owners[simulation.WorldKey(pending.divisionID, pending.characterName)] = false
	}
	for owner := range rt.currentSkillCommands {
		owners[owner] = true
	}
	for _, pending := range rt.pendingProjectileCasts {
		owners[simulation.WorldKey(pending.divisionID, pending.characterName)] = true
	}
	return owners
}

/*
==================
drainSkillFinalizes

drainSkillFinalizes emits due controls exactly once, retaining their source
and queue order. Only adjacent controls with the same route may coalesce.
A chain bracket is additionally held while its chain command is live: the
next step becomes due exactly when the previous step's action ends, so a
deadline alone would close the root deco between two stages.
==================
*/
func (rt *Runtime) drainSkillFinalizes(nowMs int64) []simulation.DivisionFrames {
	return rt.drainSelectedSkillFinalizes(nowMs, false)
}

/*
================
drainSelectedSkillFinalizes

Monster controls may settle before AI while player controls retain tick order.
================
*/
func (rt *Runtime) drainSelectedSkillFinalizes(nowMs int64, monstersOnly bool) []simulation.DivisionFrames {
	chains := rt.liveChainOwners()
	rt.pendingSkillFinalizesMu.Lock()
	if len(rt.pendingSkillFinalizes) == 0 {
		rt.pendingSkillFinalizesMu.Unlock()
		return nil
	}

	kept := rt.pendingSkillFinalizes[:0]
	var out []simulation.DivisionFrames
	for _, pending := range rt.pendingSkillFinalizes {
		if monstersOnly && !strings.HasPrefix(pending.characterName, "@monster:") {
			kept = append(kept, pending)
			continue
		}
		_, held := chains[simulation.WorldKey(pending.divisionID, pending.characterName)]
		if pending.dueAtMs > nowMs || (pending.chainBracket && held) {
			kept = append(kept, pending)
			continue
		}
		routeIndex := len(out)
		out = append(out, simulation.DivisionFrames{DivisionID: pending.divisionID, SourceGID: pending.sourceGID})
		out[routeIndex].Frames = append(out[routeIndex].Frames, simulation.Frame{
			Opcode:  pending.frame.Opcode,
			Payload: pending.frame.Payload,
			Current: pending.frame.Current,
			Scope:   pending.frame.Scope,
		})
	}
	rt.pendingSkillFinalizes = kept
	rt.pendingSkillFinalizesMu.Unlock()

	return coalesceDivisionFrames(out)
}

/*
==================
danceSelectorActive

danceSelectorActive is CSkillManager_CheckOwnerCondition 59DDF0: bit 0 of
skill-manager +0x1D0. 5842AC installs it from an active persistent skill's
scls word and 582C76 clears it when that skill retires, so it is set exactly
while such an effect is live.
==================
*/
func (rt *Runtime) danceSelectorActive(division string, c *enterworld.Character) bool {
	for _, effect := range rt.effects.Snapshot(division, c.Name) {
		row, ok := rt.deps.SkillData().SkillByID(effect.SkillID)
		if ok && row.SelectorMask&1 != 0 {
			return true
		}
	}
	return false
}
