/*
===========================================================================

skillareaburst.go - untargeted caster-centred attacks (Booming Chord/Wave)

The Bard's Booming Chord and Booming Wave strike without a target: the
hostile monsters around the caster are the victims. Selection follows the
untargeted taunt and status area (efr shape 1 around the caster, 58A088);
each victim takes every authored mc impact with the area's reduction per
victim in selection order, as the cast-owned area does (58E5F0). The rows
author zero casting time, so the cast resolves at command time like the
untargeted taunt: one commit, results on the opening record.

===========================================================================
*/

package action

import (
	"sync/atomic"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
acceptAreaBurst

Admission is shared with the prepared untargeted casts. The cost, cooldown
and cast start commit even when no monster stands in range, as the
untargeted status area and the untargeted taunt already do: the cast is
the caster's, not its victims'.
================
*/
func (rt *Runtime) acceptAreaBurst(division string, c, snapshot *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, now int64) OpResult {
	if !skill.AreaBurst || skill.TargetRequired || skill.Attack.ImpactCount == 0 || rt.Monsters == nil {
		return OpResult{DiagnosticRefusal: "area-burst-admission-refused"}
	}
	if refusal, refused := rt.admitUntargetedCast(division, snapshot, cast, skill, now, nil); refused {
		return refusal
	}
	victims := rt.tauntVictims(tauntCast{division: division, character: c, snapshot: snapshot,
		skill: enterworld.SkillRow{Threat: enterworld.SkillThreat{Area: skill.OffensiveArea}}, now: now})
	attacker, _, err := rt.playerCombatStats(division, snapshot)
	if err != nil {
		return OpResult{DiagnosticRefusal: "area-burst-stats-unavailable"}
	}
	plans := make([]areaVictimPlan, 0, len(victims))
	sequences := make([][]simulation.MonsterDamagePlan, 0, len(victims))
	percent := uint64(100)
	for _, target := range victims {
		defender, err := combat.MonsterInstanceStats(target)
		if err != nil {
			return OpResult{DiagnosticRefusal: "area-burst-defender"}
		}
		defender.MotionState = target.Motion.StateAt(now)
		plan := areaVictimPlan{target: target}
		total := uint64(0)
		for range skill.Attack.ImpactCount {
			formula, err := rt.resolvePlayerImpact(division, snapshot.Name, skill, attacker, defender, now, false)
			if err != nil {
				return OpResult{DiagnosticRefusal: "area-burst-formula"}
			}
			formula.Damage = uint32(uint64(formula.Damage) * percent / 100)
			total += uint64(formula.Damage)
			plan.formulas = append(plan.formulas, formula)
		}
		percent = percent * uint64(100-skill.OffensiveArea.ReductionPercent) / 100
		if total >= uint64(target.CurrentHP) {
			mover, ok := rt.Monsters.Mover(division, target.Gid)
			if !ok {
				return OpResult{DiagnosticRefusal: "area-burst-pose"}
			}
			plan.pose = mover.LivePoseAt(now, nil)
		}
		impacts, ok := rt.planMonsterImpacts(division, snapshot, skill, target, plan.formulas, now)
		if !ok {
			return OpResult{DiagnosticRefusal: "area-burst-plan"}
		}
		plans = append(plans, plan)
		sequences = append(sequences, impacts)
	}
	var committed [][]simulation.MonsterDamageResult
	var progression, battleFrames []wire.Frame
	var drops []grounditem.Item
	var settlements monsterSettlement
	var refusal uint16
	roster := rt.monsterRewardRoster(division, c, now)
	if !rt.deps.UpdateMany(roster.characters, "player-area-burst", func() bool {
		if !enterworld.CharacterAlive(c) || !enterworld.SkillLearned(c, skill.ID) {
			return false
		}
		cost, code := rt.offensivePhaseCost(division, c, skill, now, nil)
		refusal = code
		if code != 0 {
			return false
		}
		if len(sequences) > 0 {
			var ok bool
			if committed, ok = rt.Monsters.ApplyDamageSequences(division, sequences); !ok {
				return false
			}
			battleFrames = rt.enterBattleState(division, c, now)
		}
		rt.startSkillCast(division, c, now)
		rt.commitOffensivePhaseCost(division, c, skill, cost, now, false)
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
		return true
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal)
		}
		return OpResult{DiagnosticRefusal: "area-burst-commit-refused"}
	}
	gid := enterworld.ObjectIDForCharacter(snapshot)
	var after, deaths []wire.Frame
	var targets []wire.SkillAreaTarget
	for index, impacts := range committed {
		final := impacts[len(impacts)-1]
		target := final.Instance.Gid
		records := make([]wire.SkillCastTargetImpact, 0, skill.Attack.ImpactCount)
		for i, impact := range impacts {
			records = append(records, committedSkillImpact(plans[index].formulas[i], impact))
		}
		// Once an impact is fatal the remaining records are bare (58EE1B).
		for len(records) < int(skill.Attack.ImpactCount) {
			records = append(records, wire.SkillCastTargetImpact{Skipped: true})
		}
		targets = append(targets, wire.SkillAreaTarget{GID: target, Impacts: records})
		if final.Fatal {
			rt.queueMonsterDefeat(division, target, now+monsterDeathPresentationRetention.Milliseconds())
			deaths = append(deaths, monsterLifeDeadFrame(target))
			// No aggression for a dead victim; its damage still feeds a
			// Mana Switch link.
			rt.commitSkillHostility(division, gid, target, skill, impacts, now)
			continue
		}
		after = append(after, rt.monsterImpactAbnormalFrames(division, target, impacts)...)
		rt.commitSkillHostility(division, gid, target, skill, impacts, now)
	}
	token := atomic.AddUint32(&rt.castTokenCounter, 1)
	success := wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: gid, InstanceToken: token}
	open := wire.SkillCastUntargetedFrame(success)
	if len(targets) > 0 {
		open = wire.SkillCastUntargetedAreaFrame(success, targets)
	}
	lifetime, _ := skill.ActionLifecycleMs()
	rt.queueSkillFinalize(division, snapshot.Name, gid, now, wire.SkillCastReleaseFrame(token, 0))
	rt.queueSkillCastClose(division, snapshot.Name, gid, token, skill, 0, now+int64(lifetime))
	public := append([]wire.Frame{open}, after...)
	public = append(public, deaths...)
	public = append(public, rt.groundReferences(drops)...)
	for _, drop := range drops {
		public = append(public, wire.DropBroadcastFrames(drop.SpawnRow(true))...)
	}
	vitals := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshPayload(gid, rt.publishedVitals(division, c))}
	actor := append(append([]wire.Frame{}, public...), vitals)
	actor = append(actor, progression...)
	actor = append(actor, settlements.otherPublic...)
	private := append(wire.ProgressionPrivateFrames(progression), vitals)
	public = append(public, settlements.public...)
	// Entering battle (4E27C0) follows the hit's burst.
	actor = append(actor, battleFrames...)
	public = append(public, battleFrames...)
	return OpResult{Frames: actor, Broadcast: public, ActorPrivate: private, Recipients: settlements.others}
}
