/*
===========================================================================

statuscastarea.go - caster-centred damage-free status areas

Lightning Impact prepares without a target and, at release, rolls its
statuses on the hostile monsters around the caster through one zero-damage
record each. Selection follows the untargeted taunt; resolution and status
application follow the targeted status cast.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
acceptUntargetedStatusCast

Admission and preparation are shared with the planted trap. The release
debits the prepared cost even when no monster stands in range.
================
*/
func (rt *Runtime) acceptUntargetedStatusCast(division string, c, snapshot *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, now int64, pending *pendingProjectileCast) (OpResult, skillCastDecision) {
	if !skill.StatusCast || skill.TargetRequired || rt.Monsters == nil {
		return OpResult{DiagnosticRefusal: "status-area-admission-refused"}, skillCastRefused
	}
	if out, decision, done := rt.beginUntargetedCast(division, c, snapshot, cast, skill, now, pending, func(p *pendingProjectileCast) { p.statusArea = true }); done {
		return out, decision
	}
	rt.clearCurrentSkillCommand(division, c.Name)
	victims := rt.tauntVictims(tauntCast{division: division, character: c, snapshot: snapshot,
		skill: enterworld.SkillRow{Threat: enterworld.SkillThreat{Area: skill.OffensiveArea}}, now: now})
	attacker, _, err := rt.playerCombatStats(division, snapshot)
	if err != nil {
		return OpResult{DiagnosticRefusal: "status-area-stats-unavailable"}, skillCastRefused
	}
	formulas := make([][]combat.Result, 0, len(victims))
	sequences := make([][]simulation.MonsterDamagePlan, 0, len(victims))
	for _, target := range victims {
		defender, err := combat.MonsterInstanceStats(target)
		if err != nil {
			return OpResult{DiagnosticRefusal: "status-area-defender"}, skillCastRefused
		}
		formula, err := rt.resolvePlayerImpact(division, snapshot.Name, skill, attacker, defender, now, false)
		if err != nil {
			return OpResult{DiagnosticRefusal: "status-area-formula"}, skillCastRefused
		}
		plans, ok := rt.planMonsterImpacts(division, snapshot, skill, target, []combat.Result{formula}, now)
		if !ok {
			return OpResult{DiagnosticRefusal: "status-area-plan"}, skillCastRefused
		}
		formulas = append(formulas, []combat.Result{formula})
		sequences = append(sequences, plans)
	}
	var committed [][]simulation.MonsterDamageResult
	var battleFrames []wire.Frame
	var refusal uint16
	if !rt.deps.Update(c, "release-status-area", func() bool {
		if !enterworld.CharacterAlive(c) || !enterworld.SkillLearned(c, skill.ID) {
			return false
		}
		cost, code := rt.offensivePhaseCost(division, c, skill, now, pending)
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
		rt.commitOffensivePhaseCost(division, c, skill, cost, now, true)
		return true
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal), skillCastRefused
		}
		return OpResult{DiagnosticRefusal: "status-area-release-commit-refused"}, skillCastRefused
	}
	gid := enterworld.ObjectIDForCharacter(c)
	var statusFrames []wire.Frame
	var targets []wire.SkillAreaTarget
	for index, impacts := range committed {
		target := impacts[len(impacts)-1].Instance.Gid
		targets = append(targets, wire.SkillAreaTarget{GID: target, Impacts: []wire.SkillCastTargetImpact{committedSkillImpact(formulas[index][0], impacts[0])}})
		statusFrames = append(statusFrames, rt.monsterImpactAbnormalFrames(division, target, impacts)...)
		rt.commitSkillHostility(division, gid, target, skill, impacts, now)
	}
	success := wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: gid, InstanceToken: pending.token}
	released := wire.SkillCastReleaseFrame(pending.token, 0)
	if len(targets) > 0 {
		released = wire.SkillCastUntargetedAreaReleaseFrame(success, targets)
	}
	rt.queueSkillCastClose(division, c.Name, gid, pending.token, skill, 0, now+int64(skill.ActionDurationMs))
	vitals := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshWithSourcePayload(gid, simulation.VitalsSourceSkillRecovery, rt.publishedVitals(division, c))}
	public := append([]wire.Frame{released}, statusFrames...)
	public = append(public, battleFrames...)
	return OpResult{Frames: append(append([]wire.Frame{}, public...), vitals), Broadcast: public, ActorPrivate: []wire.Frame{vitals}}, skillCastAccepted
}
