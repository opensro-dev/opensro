/*
===========================================================================

skillarea.go - area skills: victim selection around a centre

Plan all victims at release and commit their impacts together. The primary
target's pre-impact pose also owns the projectile's retained flight time.

===========================================================================
*/

package action

import (
	"sync/atomic"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
areaVictimPlan

Retain the release-time target and formulas until the whole area commits.
================
*/
type areaVictimPlan struct {
	target   monster.Instance
	formulas []combat.Result
	pose     monster.Pose
}

/*
==================
areaPlanInput

What one area strike's victim planning varies by. impacts is the records
each victim takes; chained marks every victim after the first as a chained
one (an imbue's spread); posePrimary keeps the first victim's pose (a
projectile's flight), poseAll every victim's (a trap's explosion).
==================
*/
type areaPlanInput struct {
	division             string
	snapshot             *enterworld.Character
	skill                enterworld.SkillRow
	attacker             combat.Stats
	victims              []monster.Instance
	reduction            uint8
	impacts              int
	chained              bool
	posePrimary, poseAll bool
	now                  int64
}

/*
==================
planAreaVictims

SkillCombat_CalculateHitOutcome (58E5F0) per victim in selection order:
every impact's formula at the area's running percent, which falls by the
reduction after each victim, cumulatively. A victim the planned damage
kills keeps its live pose for the reward settlement. ok is false when a
victim's stats, formula, pose or impact plan cannot be formed.
==================
*/
func (rt *Runtime) planAreaVictims(in areaPlanInput) (plans []areaVictimPlan, sequences [][]simulation.MonsterDamagePlan, ok bool) {
	plans = make([]areaVictimPlan, 0, len(in.victims))
	sequences = make([][]simulation.MonsterDamagePlan, 0, len(in.victims))
	percent := uint64(100)
	for index, target := range in.victims {
		defender, err := combat.MonsterInstanceStats(target)
		if err != nil {
			return nil, nil, false
		}
		defender.MotionState = target.Motion.StateAt(in.now)
		plan := areaVictimPlan{target: target}
		total := uint64(0)
		for range in.impacts {
			formula, err := rt.resolvePlayerImpact(in.division, in.snapshot.Name, in.skill, in.attacker, defender, in.now, in.chained && index > 0)
			if err != nil {
				return nil, nil, false
			}
			formula.Damage = uint32(uint64(formula.Damage) * percent / 100)
			total += uint64(formula.Damage)
			plan.formulas = append(plan.formulas, formula)
		}
		percent = percent * uint64(100-in.reduction) / 100
		if in.poseAll || total >= uint64(target.CurrentHP) || index == 0 && in.posePrimary {
			mover, found := rt.Monsters.Mover(in.division, target.Gid)
			if !found {
				return nil, nil, false
			}
			plan.pose = mover.LivePoseAt(in.now, nil)
		}
		impacts, planned := rt.planMonsterImpacts(in.division, in.snapshot, in.skill, target, plan.formulas, in.now)
		if !planned {
			return nil, nil, false
		}
		plans = append(plans, plan)
		sequences = append(sequences, impacts)
	}
	return plans, sequences, true
}

/*
==================
settleAreaFatalities

Inside the roster's door: every victim whose last committed impact was
fatal settles through the shared reward door at the pose its plan kept.
==================
*/
func (rt *Runtime) settleAreaFatalities(division string, c *enterworld.Character, roster rewardRoster, committed [][]simulation.MonsterDamageResult, plans []areaVictimPlan, now int64) (progression []wire.Frame, drops []grounditem.Item, settlements monsterSettlement) {
	for index, impacts := range committed {
		impact := impacts[len(impacts)-1]
		if !impact.Fatal {
			continue
		}
		s := rt.settleMonsterInsideDoor(division, c, roster, impact, plans[index].pose, now)
		progression = append(progression, s.actorFrames...)
		drops = append(drops, s.drops...)
		settlements.public = append(settlements.public, s.public...)
		settlements.otherPublic = append(settlements.otherPublic, s.otherPublic...)
		settlements.others = append(settlements.others, s.others...)
	}
	return progression, drops, settlements
}

/*
==================
areaVictims

areaVictims samples live positions without materializing additional nests.
Shape-2 uses the primary target's position, caster + candidate body radii,
and a three-dimensional distance (server 58a81c / 58ab6d..58ac23).
GID order is the port's deterministic registration-order policy; native
spatial-container traversal order is not reproduced by this registry.
Shapes 3 and 4 select along a line (skillarea_directional.go); reach is the
action's base range they measure with.
==================
*/
func (rt *Runtime) areaVictims(division string, c *enterworld.Character, primary monster.Instance, area enterworld.SkillOffensiveArea, reach float32, nowMs int64) []monster.Instance {
	validShape := area.Shape >= 1 && area.Shape <= 4 || area.Shape == 6
	if area.MaxTargets == 0 || !validShape {
		return nil
	}
	if _, ok := rt.characterMonster(division, c, primary.Gid); !ok {
		return nil
	}
	lease, exists := rt.Monsters.ObjectPopulation(division, primary.Gid)
	if !exists {
		return nil
	}
	radius, ok := rt.deps.CharacterBodyRadius(c)
	if !ok {
		return nil
	}
	mover, ok := rt.Monsters.Mover(division, primary.Gid)
	if !ok {
		return nil
	}
	center := mover.LivePoseAt(nowMs, nil)
	from := simulation.Spawn{RegionID: center.RegionID, X: center.X, Y: center.Y, Z: center.Z}
	if area.Shape == 3 || area.Shape == 4 {
		caster := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, nowMs)
		shape := areaShape{shape: area.Shape, width: area.Radius, maxTargets: area.MaxTargets}
		return rt.directionalVictims(division, lease, caster, radius, primary, from, shape, reach, nowMs)
	}
	if area.Shape == 1 {
		// Native 58A088..58A0D3 copies the caster position, whereas 58A831
		// copies the selected target. Sample the authority's current movement
		// leg, not the character's last persisted position.
		from = rt.liveSpawn(simulation.WorldKey(division, c.Name), c, nowMs)
	}
	out := []monster.Instance{primary}
	if area.MaxTargets == 1 {
		return out
	}
	var candidates []monster.Instance
	if area.Shape == 6 {
		candidates = rt.Monsters.CombatCandidatesInPopulation(division, lease, from, float64(area.Radius), nowMs, true)
	} else {
		candidates = rt.Monsters.CombatCandidatesInPopulation(division, lease, from, float64(area.Radius)+radius, nowMs, false)
	}
	for _, candidate := range candidates {
		if candidate.Gid == primary.Gid || candidate.CurrentHP == 0 {
			continue
		}
		out = append(out, candidate)
		if len(out) == int(area.MaxTargets) {
			break
		}
	}
	return out
}

/*
================
areaBaseRange

58B29B..58B2A9 uses the row's range word or the truncated attack-range
keeper. Pursuit bonuses do not widen this directional selection geometry.
================
*/
func areaBaseRange(skill enterworld.SkillRow, attacker combat.Stats) float32 {
	if skill.ActionRange > 0 {
		return float32(uint16(skill.ActionRange))
	}
	param, _ := attacker.Param(0x21)
	return float32(uint16(int32(param)))
}

/*
==================
acceptSkillAreaAt

One area strike. A projectile area (the Chinese bow's pierce and special
shots) selects and resolves its victims at release like any area
(SkillAction_Projectile 585A1F -> TargetSelection_DispatchByShape, then
SkillCombat_CalculateHitOutcome), spends its arrow in the same commit, and
keeps the token open for the flight to the primary target (5860D2).

area is the row's own efr or, when chained is set, the active imbue's
(skillcombat.go): every victim after the primary is then a chained one.
charged is false for a basic attack spread by an imbue: like its single-
target hit, it pays no phase cost.
Each victim takes every authored impact; once one is fatal its remaining
records are bare type 8 (58EE1B, var_e5 set by 58FA53).
==================
*/
func (rt *Runtime) acceptSkillAreaAt(division string, character, snapshot *enterworld.Character, skill enterworld.SkillRow, area enterworld.SkillOffensiveArea, chained, charged bool, primary monster.Instance, attacker combat.Stats, loadout combat.Loadout, consumeAmmo bool, nowMs int64, rootID uint32, release *pendingProjectileCast) (OpResult, skillCastDecision) {
	if skill.Attack.ImpactCount == 0 {
		return OpResult{}, skillCastRefused
	}
	victims := rt.areaVictims(division, snapshot, primary, area, areaBaseRange(skill, attacker), nowMs)
	if len(victims) == 0 {
		return OpResult{}, skillCastRefused
	}
	plans, sequences, planned := rt.planAreaVictims(areaPlanInput{
		division: division, snapshot: snapshot, skill: skill, attacker: attacker, victims: victims,
		reduction: area.ReductionPercent, impacts: int(skill.Attack.ImpactCount), chained: chained,
		posePrimary: skill.ProjectileSpeed != 0, now: nowMs,
	})
	if !planned {
		return OpResult{}, skillCastRefused
	}
	var committed [][]simulation.MonsterDamageResult
	var progression []wire.Frame
	var drops []grounditem.Item
	var battleFrames []wire.Frame
	var refusal uint16
	roster := rt.monsterRewardRoster(division, character, nowMs)
	var settlements monsterSettlement
	var ammo ammunitionResult
	if !rt.deps.UpdateMany(roster.characters, "player-area-attack", func() bool {
		var cost skillCharge
		if charged && rootID == 0 {
			cost, refusal = rt.offensivePhaseCost(division, character, skill, nowMs, release)
			if refusal != 0 {
				return false
			}
		}
		debit, admitted := ammunitionDebit{index: -1}, true
		if consumeAmmo {
			debit, admitted = rt.planEquippedAmmunition(character, loadout.WeaponKind, ammunitionSpent(skill, charged))
		}
		if !admitted {
			refusal = 0x300e
			return false
		}
		var ok bool
		committed, ok = rt.Monsters.ApplyDamageSequences(division, sequences)
		if !ok {
			return false
		}
		if release == nil {
			rt.startSkillCast(division, character, nowMs)
		}
		battleFrames = rt.enterBattleState(division, character, nowMs)
		if consumeAmmo {
			ammo = applyAmmunitionDebit(character, debit)
		}
		if charged && rootID == 0 {
			rt.commitOffensivePhaseCost(division, character, skill, cost, nowMs, release != nil)
		} else if release == nil {
			rt.registerPlayerSkillCooldown(division, character, skill, nowMs)
		}
		progression, drops, settlements = rt.settleAreaFatalities(division, character, roster, committed, plans, nowMs)
		return true
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal), skillCastRefused
		}
		return OpResult{}, skillCastRefused
	}
	var burnFrames, deaths []wire.Frame
	var targets []wire.SkillAreaTarget
	for index, impacts := range committed {
		final := impacts[len(impacts)-1]
		gid := final.Instance.Gid
		for _, impact := range impacts {
			if impact.Knockdown != nil || impact.Knockback != nil {
				burnFrames = append(burnFrames, rt.interruptMonsterCast(division, gid)...)
				break
			}
		}
		if !final.Fatal {
			burnFrames = append(burnFrames, rt.monsterImpactAbnormalFrames(division, gid, impacts)...)
		}
		records := make([]wire.SkillCastTargetImpact, 0, skill.Attack.ImpactCount)
		for i, impact := range impacts {
			records = append(records, committedSkillImpact(plans[index].formulas[i], impact))
		}
		for len(records) < int(skill.Attack.ImpactCount) {
			records = append(records, wire.SkillCastTargetImpact{Skipped: true})
		}
		targets = append(targets, wire.SkillAreaTarget{GID: gid, Impacts: records})
		if final.Fatal {
			rt.queueMonsterDefeat(division, gid, nowMs+monsterDeathPresentationRetention.Milliseconds())
			// 4A9C80 publishes LIFE for every alive-to-dead transition, area
			// victims included; without it the client keeps moving the corpse.
			deaths = append(deaths, monsterLifeDeadFrame(gid))
		}
		// A fatal victim records no aggression; its damage still feeds a
		// Mana Switch link.
		rt.commitSkillHostility(division, enterworld.ObjectIDForCharacter(snapshot), gid, skill, impacts, nowMs)
	}
	var token uint32
	if release != nil {
		token = release.token
	} else {
		token = atomic.AddUint32(&rt.castTokenCounter, 1)
	}
	success := wire.SkillCastAreaFrame(wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: enterworld.ObjectIDForCharacter(snapshot), InstanceToken: token}, primary.Gid, targets)
	lifetime, _ := skill.ActionLifecycleMs()
	if release == nil {
		rt.queueSkillFinalize(division, snapshot.Name, enterworld.ObjectIDForCharacter(snapshot), nowMs+int64(skill.ActionCastingTimeMs), wire.SkillCastReleaseFrame(token, primary.Gid))
	} else {
		success = wire.SkillCastAreaReleaseFrame(wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: enterworld.ObjectIDForCharacter(snapshot), InstanceToken: token}, primary.Gid, targets)
		lifetime = uint64(skill.ActionDurationMs)
	}
	if skill.ProjectileSpeed != 0 {
		// The original target sample determines flight, even when impact
		// displaces or kills it. Zero-preparation area shots retain it too.
		pose := plans[0].pose
		flight := projectileFlightMs(rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, nowMs), simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z}, skill.ProjectileSpeed)
		lifetime = uint64(max(int64(lifetime), flight+1))
	}
	rt.queueSkillCastClose(division, snapshot.Name, enterworld.ObjectIDForCharacter(snapshot), token, skill, rootID, nowMs+int64(lifetime))
	public := append([]wire.Frame{success}, burnFrames...)
	public = append(public, deaths...)
	public = append(public, rt.groundReferences(drops)...)
	for _, drop := range drops {
		public = append(public, wire.DropBroadcastFrames(drop.SpawnRow(true))...)
	}
	actor := append([]wire.Frame{}, public...)
	private := wire.ProgressionPrivateFrames(progression)
	if rootID == 0 {
		vitals := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshPayload(enterworld.ObjectIDForCharacter(character), rt.publishedVitals(division, character))}
		actor = append(actor, vitals)
		private = append(private, vitals)
	}
	if consumeAmmo {
		frames := ammunitionFrames(ammo)
		actor = append(actor, frames...)
		private = append(frames, private...)
	}
	actor = append(actor, progression...)
	public = append(public, settlements.public...)
	actor = append(actor, settlements.otherPublic...)
	// Entering battle (4E27C0) follows the hit's burst.
	actor = append(actor, battleFrames...)
	public = append(public, battleFrames...)
	return OpResult{Frames: actor, Broadcast: public, ActorPrivate: private, Recipients: settlements.others}, skillCastAccepted
}
