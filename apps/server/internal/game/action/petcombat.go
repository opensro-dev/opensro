/*
===========================================================================

petcombat.go - an attack pet fights the monster its owner orders it to

The owner's COS attack order (C->S 0x70C5 tag 2, client 0x769E) reaches
the reference server's owned-COS command. For a summoned attack pet it posts
AI event 0x19 to the pet's own AI tactics, whose state handler
(the owner attack order) sets the combat target and enters the
BATTLE state - the same state machine monsters fight in. The follow order
(event 0x1A) leaves BATTLE again.

This module owns that per-pet combat intent. The pet tick (petai.go) pursues
the target with the pet's movement owner and strikes through the shared
combat formula and the one monster HP door, crediting the kill to the owner
so experience, party sharing and loot need no pet-specific path. A player
target is struck through the player-victim path with the owner as killer
(a COS killer stands for its owner), and only when the owner may
attack it.

Damage credit belongs to the owner; aggression belongs to the attacking
companion. Monster pursuit already resolves both kinds of opponent.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/companion"
	"strings"
	"sync/atomic"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
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
it closes in (the native battle action keeps the selected skill through approach) and the
native strategy interval.
================
*/
type petCombatIntent struct {
	target       uint32
	skillID      uint32
	intervalMs   uint32
	nextAttackMs int64
	castToken    uint32
	releaseAtMs  int64
	pursuing     bool
	// pursuingSinceMs is when the current approach began (petdiagnostics.go).
	pursuingSinceMs int64
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
	refuse := func(reason string) OpResult {
		rt.petDiagnostic(division, character.Name, cosGID, "order refused: "+reason, log.Fields{"target": targetGID})
		return OpResult{DiagnosticRefusal: "pet attack: " + reason}
	}
	pet := snapshot.CompanionByGID(cosGID)
	if pet == nil || !pet.Summoned || pet.Mounted || pet.CurrentHP == 0 || targetGID == 0 {
		return refuse("pet not summoned, mounted, dead or no target")
	}
	ref, ok := rt.cosReference(pet)
	if !ok || ref.TidWord>>11 != attackPetBand {
		return refuse("not an attack pet")
	}
	// The native owned-COS command validates a COS target through its owner and
	// retains the requested object for the AI event. Use the tick's resolver
	// so a companion GID reaches that same permission and damage path.
	target, ok := rt.resolvePetCombatTarget(petCombatStep{
		key: petOwnerKey{division: division}, snapshot: snapshot, pet: pet, ref: ref, nowMs: nowMs,
	}, targetGID)
	if !ok {
		// An order on a monster that just died is the normal end of a fight.
		if ended := rt.petTargetEnded(division, snapshot, targetGID); ended != "" {
			rt.countPetTargetEnded(true, ended)
			return OpResult{DiagnosticRefusal: "pet attack: " + ended}
		}
		return refuse("target not found")
	}
	if !petAttackBodyAllowed(pet, target.combatTarget) {
		return refuse("body status forbids the attack")
	}
	if target.player != nil && (rt.companionTeamRefusal(snapshot, target.snapshot) != 0 ||
		rt.playerAttackTargetRefusal(division, snapshot, target.snapshot, nowMs) != 0) {
		return refuse("player target not attackable")
	}
	state := rt.petSessionFor(division, character.Name, cosGID)
	if state == nil {
		return refuse("no pet session")
	}
	// The native command retires the owner's event-bit-2 effects after the
	// admitted order, including a repeated order for the existing target.
	defer func() {
		rt.deps.Update(character, "pet-attack-order", func() bool {
			rt.retireEffectsOnEvent(division, character, effectEventSkillCast, nowMs)
			return true
		})
	}()
	if state.combat != nil && state.combat.target == targetGID {
		return OpResult{}
	}
	// The native battle target change keeps the strategy; a new BATTLE
	// starts with a fresh one. Either way the next strike is due now.
	intent := &petCombatIntent{target: targetGID, nextAttackMs: nowMs}
	if state.combat != nil {
		intent.intervalMs = state.combat.intervalMs
	}
	rt.cancelPetCombat(petOwnerKey{division: division, name: strings.ToLower(character.Name), gid: cosGID}, state, nowMs)
	state.combat = intent
	// Entering BATTLE is a state change.
	resetPetStuck(state)
	// Entering BATTLE leaves PICKITEM, which answers a pending pickup.
	expireCosPickup(state)
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
	percent    uint64
	// trained is this strike's attack-pet plan (pettrained.go); nil native.
	trained *petTrainedStrike
	// announced: the strike's cast already went out at its windup.
	announced bool
}

/*
================
advancePetCombat
================
*/
func (rt *Runtime) advancePetCombat(step petCombatStep) (frames []simulation.Frame, handled bool) {
	rt.acquireMercenaryTarget(step)
	intent := step.state.combat
	if intent == nil || step.ref.TidWord>>11 != attackPetBand && step.ref.TidWord>>11 != domain.MercenaryBand {
		return nil, false
	}
	if step.ref.TidWord>>11 == domain.MercenaryBand || step.ref.TidWord>>11 == attackPetBand {
		rt.advanceCompanionBattleFollowTimer(step)
	}
	if step.run < step.ref.RunSpeed {
		speedFrames := rt.setCompanionFollowSpeed(step, 100)
		step.run = cosParameter(step.ref, step.pet, rt.cosAbnormal(step.key.division, step.snapshot.Name, step.pet.GID), movementRunParameter)
		defer func() { frames = append(speedFrames, frames...) }()
	}
	target, ok := rt.resolvePetCombatTarget(step, intent.target)
	// The native owner check runs on every companion strike: the owner's free-battle team is
	// refused first, so a cape put on mid-fight ends an attack on a team-mate.
	if !ok || target.player != nil && (rt.companionTeamRefusal(step.snapshot, target.snapshot) != 0 ||
		step.ref.TidWord>>11 != domain.MercenaryBand && rt.playerAttackTargetRefusal(step.key.division, step.snapshot, target.snapshot, step.nowMs) != 0) {
		// A target that died or left ends the fight normally: counted, not logged.
		if ended := rt.petTargetEnded(step.key.division, step.snapshot, intent.target); !ok && ended != "" {
			rt.countPetTargetEnded(false, ended)
		} else {
			rt.petDiagnostic(step.key.division, step.snapshot.Name, step.pet.GID, "fight ended: target lost or not attackable",
				log.Fields{"target": intent.target, "petRef": step.pet.RefObjID, "petBand": step.ref.TidWord >> 11})
		}
		rt.cancelPetCombat(step.key, step.state, step.nowMs)
		return nil, false
	}
	owner := rt.liveSpawn(simulation.WorldKey(step.key.division, step.snapshot.Name), step.snapshot, step.nowMs)
	targetAt := target.at
	targetRadius := float64(0)
	if target.cosRef != nil {
		targetRadius = target.cosRef.Parameters.BodyRadius
	} else if target.monster != nil {
		targetRadius = target.monster.BodyRadius()
	} else if radius, valid := rt.deps.CharacterBodyRadius(target.snapshot); valid {
		targetRadius = radius
	}
	// INFERENCE: the pet's BATTLE leash is its owner's neighbourhood. A target
	// that leaves the owner's sector and its neighbours (the same
	// same-plane adjacent-sector test the native command applies to its position
	// order) ends the fight and the pet returns to follow.
	if !samePlaneAdjacent(owner, targetAt) {
		rt.petDiagnostic(step.key.division, step.snapshot.Name, step.pet.GID, "fight ended: target left the owner's sectors", log.Fields{"target": intent.target})
		rt.cancelPetCombat(step.key, step.state, step.nowMs)
		return nil, false
	}
	skill, ok := rt.petAttackSkill(step, intent, simulation.AttackTarget{Distance: float32(areaDistance(step.state.follower.Position(step.nowMs), targetAt)), BodyRadius: targetRadius})
	if !ok {
		rt.petDiagnostic(step.key.division, step.snapshot.Name, step.pet.GID, "fight ended: no usable attack skill", log.Fields{"target": intent.target, "ref": step.ref.Codename})
		rt.cancelPetCombat(step.key, step.state, step.nowMs)
		return nil, false
	}
	if rt.petExpansionAllowed(step.snapshot, step.pet) && companion.PetKind(step.pet.Codename) == "Penguin" && target.monster != nil {
		skill.ActionRange = 150
		skill.ActionCastingTimeMs = 600
		skill.ProjectileSpeed = 500
	}
	block := rt.cosAbnormal(step.key.division, step.snapshot.Name, step.pet.GID)
	spacing := simulation.CombatSpacing{
		ActorBodyRadius:  simulation.BodyRadius(step.ref.Parameters.BodyRadius),
		TargetBodyRadius: simulation.BodyRadius(targetRadius),
		ActionReach:      reducedActionReach(float32(skill.ActionRange), cosParameter(step.ref, step.pet, block, actionRangeCutParameter)),
	}
	if !spacing.Valid() {
		rt.petDiagnostic(step.key.division, step.snapshot.Name, step.pet.GID, "fight ended: invalid combat spacing", log.Fields{"target": intent.target, "petRadius": step.ref.Parameters.BodyRadius, "targetRadius": targetRadius, "reach": skill.ActionRange})
		rt.cancelPetCombat(step.key, step.state, step.nowMs)
		return nil, false
	}
	at := step.state.follower.Position(step.nowMs)
	if intent.castToken == 0 && !spacing.Contains(at, targetAt) {
		if !intent.pursuing {
			intent.pursuingSinceMs = step.nowMs
		}
		if step.nowMs-intent.pursuingSinceMs >= petApproachStallMs {
			rt.petDiagnostic(step.key.division, step.snapshot.Name, step.pet.GID, "approach not reaching strike range", log.Fields{
				"target": intent.target, "distance": simulation.WorldDistance2D(at, targetAt), "admission": spacing.AdmissionRadius(),
				"standOff": spacing.StandOffRadius(), "moving": step.state.follower.Moving(step.nowMs), "pursuingMs": step.nowMs - intent.pursuingSinceMs,
			})
		}
		intent.pursuing = true
		return step.state.follower.Approach(targetAt, float64(rt.cosPacedRun(step.ref, step.run)), step.nowMs, spacing.StandOffRadius(), step.constraint), true
	}
	intent.pursuing = false
	frames = step.state.follower.Stop(step.nowMs)
	if intent.castToken == 0 && step.nowMs < intent.nextAttackMs {
		return frames, true
	}
	if skill.ActionCastingTimeMs != 0 {
		if intent.castToken == 0 {
			intent.castToken = atomic.AddUint32(&rt.castTokenCounter, 1)
			intent.releaseAtMs = step.nowMs + int64(skill.ActionCastingTimeMs) + 1
			// The native battle action arms the strategy timer before submitting the cast.
			intent.nextAttackMs = step.nowMs + int64(intent.intervalMs)
			frame := wire.SkillCastUntargetedFrame(wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: step.pet.GID, InstanceToken: intent.castToken, OwnerOrTargetGid: target.gid})
			cast := []wire.Frame{frame}
			if id, ok := rt.wolfRendQueued(step); ok {
				// The rend clip plays from the windup; the release may still refuse it.
				cast = append(cast, wolfEmoteFrame(step.pet.GID, id))
			}
			step.state.public = append(step.state.public, cast...)
			return append(frames, simFrames(cast)...), true
		}
		if step.nowMs < intent.releaseAtMs {
			return frames, true
		}
	}
	step.announced = intent.castToken != 0
	skill, step.trained = rt.petTrainedDecide(step, skill, target)
	strike, ok := rt.strikePetTargets(step, target, skill)
	if !ok {
		rt.petDiagnostic(step.key.division, step.snapshot.Name, step.pet.GID, "fight ended: the strike was refused", log.Fields{"target": intent.target, "skill": skill.ID})
		rt.cancelPetCombat(step.key, step.state, step.nowMs)
		return frames, true
	}
	if intent.castToken == 0 {
		intent.nextAttackMs = step.nowMs + int64(intent.intervalMs)
	}
	// Select the next authored action on the next due strike.
	intent.skillID = 0
	intent.castToken, intent.releaseAtMs = 0, 0
	if strike.fatal {
		rt.cancelPetCombat(step.key, step.state, step.nowMs)
	}
	return append(frames, strike.frames...), true
}

/*
================
petAttackSkill

MonsterAttackPlan's selection over the pet's own default skills: a retained
skill stays chosen; otherwise the native weighted selector chooses among complete,
targeted attack rows. Choosing resamples the strategy interval, as
adoptMonsterAttack does on a skill change.
================
*/
func (rt *Runtime) petAttackSkill(step petCombatStep, intent *petCombatIntent, target simulation.AttackTarget) (enterworld.SkillRow, bool) {
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
		// Positive casting time stays in this companion's BATTLE intent until release.
		if id == 0 || !ok || (!row.CombatPinned || !row.Attack.Present) && !row.CreatureStatusCast || !row.TargetRequired ||
			!pinned || lifecycle == 0 || !row.TimingPinned || row.CoolTimeMs == 0 ||
			!row.ActionRangePinned || row.ActionRange <= 0 {
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
	choice, chosen := monsterWeightedSkill(monster.Instance{Ref: step.ref.Parameters}, valid, simulation.AttackPick{Sample: float64(draw%crtRandRange) / crtRandRange, Target: &target})
	if !chosen {
		return enterworld.SkillRow{}, false
	}
	block := rt.cosAbnormal(step.key.division, step.snapshot.Name, step.pet.GID)
	cooldown := choice.CooldownDurationMs(cosParameter(step.ref, step.pet, block, actionSpeedParameter))
	jitter, err := rt.CombatRoll()
	if err != nil {
		return enterworld.SkillRow{}, false
	}
	intent.skillID = choice.ID
	intent.intervalMs = monster.NextAttackInterval(intent.intervalMs, uint32(cooldown), jitter%crtRandRange)
	// Port-only attack speed (pettrained.go); 1 for every native companion.
	intent.intervalMs = uint32(float64(intent.intervalMs) / rt.petAttackSpeed(step))
	return choice, true
}

/*
================
petStrikeResult
================
*/
type petStrikeResult struct {
	strike   monsterStrike
	hpDamage uint64 // HP actually removed; native impact damage may include overkill.
	actor    []wire.Frame
	frames   []simulation.Frame
	fatal    bool
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
	attacker, err := cosCombatStats(step.ref, step.pet, block, rt.cosDebuffWords(division, step.snapshot.Name, step.pet.GID, step.nowMs))
	if err != nil {
		return petStrikeResult{}, false
	}
	defender, err := monsterDefenderStats(target, step.nowMs)
	if err != nil {
		return petStrikeResult{}, false
	}
	if step.trained != nil {
		attacker, defender = petTrainedStats(step.trained, target, attacker, defender)
	}
	actor := criticalActor{division: division, monster: step.pet.GID}
	formulas := make([]combat.Result, 0, skill.Attack.ImpactCount)
	for range creatureImpactCount(skill) {
		formula, err := rt.resolveCreatureImpact(actor, skill, attacker, defender)
		if err != nil {
			return petStrikeResult{}, false
		}
		formulas = append(formulas, combat.FinishImpact(formula, combat.ImpactTail{Percent: step.percent, Attack: skill.Attack.Present}))
	}
	if len(formulas) == 0 {
		return petStrikeResult{}, false
	}
	if step.trained != nil {
		rt.petTrainedDamage(step.trained, target, formulas)
	}
	ownerGID := enterworld.ObjectIDForCharacter(step.snapshot)
	level := step.pet.Level
	if level == 0 {
		level = step.ref.Level
	}
	plans, planned := rt.planCreatureMonsterImpacts(monsterImpactCaster{division: division,
		actor: actor, credit: ownerGID, level: level, from: step.state.follower.Position(step.nowMs)}, skill, target, formulas, step.nowMs)
	if !planned {
		return petStrikeResult{}, false
	}
	if step.trained != nil && step.trained.coldExtension {
		for i := range plans {
			for j := range plans[i].Abnormal {
				if plans[i].Abnormal[j].Status == abnormal.Frostbite {
					plans[i].Abnormal[j].DurationMs += 2000
				}
			}
		}
	}
	monsterPose := monster.Pose{RegionID: targetAt.RegionID, X: targetAt.X, Y: targetAt.Y, Z: targetAt.Z}
	roster := rt.monsterRewardRoster(division, owner, step.nowMs)
	var committed []simulation.MonsterDamageResult
	var settlement monsterSettlement
	var trainedFrames []wire.Frame
	var hpDamage uint64
	if !rt.deps.UpdateManyDeferred(roster.characters, "pet-attack", func() bool {
		// The live summoner owns the trained rows and counters (pettrained.go):
		// a row spent meanwhile refuses the hit, a refused commit spends nothing.
		livePet := owner.CompanionByGID(step.pet.GID)
		if _, ok := rt.petTrainedCommit(step, owner, livePet, step.trained, 0, false, true); !ok {
			return false
		}
		committed = rt.Monsters.ApplyDamageSequence(division, target.Gid, target.CurrentHP, plans)
		if len(committed) == 0 {
			return false
		}
		dealt := uint32(0)
		for _, applied := range committed {
			dealt += applied.Damage
			hpDamage += uint64(applied.Damage)
		}
		hpDamage = min(hpDamage, uint64(target.CurrentHP))
		trainedFrames, _ = rt.petTrainedCommit(step, owner, livePet, step.trained, dealt, committed[len(committed)-1].Fatal, false)
		if committed[len(committed)-1].Fatal {
			roster.companionKill = true
			settlement = rt.settleMonsterInsideDoor(division, owner, roster, committed[len(committed)-1], monsterPose, step.nowMs)
		}
		return true
	}) {
		return petStrikeResult{}, false
	}
	rt.commitSkillHostility(division, step.pet.GID, target.Gid, skill, committed, step.nowMs)

	impacts := make([]wire.SkillCastTargetImpact, 0, len(committed))
	for index, applied := range committed {
		impacts = append(impacts, committedSkillImpact(formulas[index], applied))
	}
	fatal := committed[len(committed)-1].Fatal
	public := rt.monsterImpactAbnormalFrames(division, target.Gid, committed)
	if step.trained != nil && step.trained.rend && !step.announced {
		// Port-only: the Savage Rend emote (its skill ID) plays the client's
		// pet-rend clip; like the trained frames below it reaches viewers and owner.
		for _, u := range step.trained.uses {
			trainedFrames = append(trainedFrames, wolfEmoteFrame(step.pet.GID, u.skill.ID))
		}
	}
	if step.trained != nil && step.trained.executed && committed[len(committed)-1].Fatal {
		trainedFrames = append(trainedFrames, wolfEmoteFrame(target.Gid, wolfEmoteExecuted))
	}
	// Trained heals: every viewer sees the pet's HP, the owner its own.
	public = append(public, trainedFrames...)
	actorFrames := append([]wire.Frame(nil), trainedFrames...)
	if step.trained != nil {
		actorFrames = append(actorFrames, step.trained.ownerState...)
	}
	if fatal {
		rt.queueMonsterDefeat(division, target.Gid, step.nowMs+monsterDeathPresentationRetention.Milliseconds())
		// Port-only shard notice for a seal drop (announce.go).
		rt.announceSealDrops(division, target.Ref.Name, settlement.drops, step.nowMs)
		burst := rt.monsterKillBurst(target.Gid, settlement.drops)
		public = append(public, burst...)
		actorFrames = append(actorFrames, settlement.actorFrames...)
		public = append(public, settlement.public...)
		actorFrames = append(actorFrames, settlement.otherPublic...)
		step.state.others = append(step.state.others, settlement.others...)
	}
	return petStrikeResult{strike: monsterStrike{gid: target.Gid, pose: targetAt, impacts: impacts, public: public, fatal: fatal}, actor: actorFrames, hpDamage: hpDamage, fatal: fatal}, true
}

/*
================
petStrikePlayer

One instant pet hit on a player: the pet's formula behind the victim's
wall and its statuses (the abnormal roll with the pet's level), then the recipient
side in a door holding the owner and the victim, the owner standing for
the pet as attacker and killer. Published under the pet's GID.
================
*/
func (rt *Runtime) petStrikePlayer(step petCombatStep, target combatTarget, skill enterworld.SkillRow) (petStrikeResult, bool) {
	division, owner, victim := step.key.division, step.state.character, target.snapshot
	block := rt.cosAbnormal(division, step.snapshot.Name, step.pet.GID)
	attacker, err := cosCombatStats(step.ref, step.pet, block, rt.cosDebuffWords(division, step.snapshot.Name, step.pet.GID, step.nowMs))
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
			out, err := rt.resolveCreatureImpactBehindWall(actor, skill, attacker, defender, wall)
			out.Defender = combat.FinishImpact(out.Defender, combat.ImpactTail{Percent: step.percent, Attack: skill.Attack.Present,
				Covered: out.Covered})
			out.Absorbed = uint32(uint64(out.Absorbed) * step.percent / fullAreaPercent)
			return out, err
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
	if !rt.deps.UpdateManyDeferred([]*enterworld.Character{owner, target.player}, "pet-attack-player", func() bool {
		commit = rt.commitPlayerHitInDoor(division, owner, &hit, step.nowMs)
		return len(hit.struck.impacts) > 0
	}) {
		return petStrikeResult{}, false
	}
	public, recipient := rt.publishPlayerHit(division, hit, step.nowMs)
	public = append(public, commit.public...)
	step.state.others = append(step.state.others, recipient)
	step.state.others = append(step.state.others, rt.payJobKillShares(commit.after)...)
	return petStrikeResult{strike: monsterStrike{gid: target.gid, pose: target.at, impacts: hit.struck.impacts, absorb: hit.struck.absorb, public: public, fatal: hit.struck.fatal}, actor: commit.actor, fatal: hit.struck.fatal}, true
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
