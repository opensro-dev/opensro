/*
===========================================================================

statuscastarea.go - caster-centred damage-free status areas

Lightning Impact prepares without a target and, at release, rolls its
statuses on the hostile monsters and attackable players around the caster
through one zero-damage record each. Selection is the caster-centred area
(casterAreaVictims); resolution and status application follow the
targeted status cast.

===========================================================================
*/

package action

import (
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
	victims := rt.casterAreaVictims(division, snapshot, skill, skill.OffensiveArea, now)
	attacker, _, err := rt.playerCombatStats(division, snapshot)
	if err != nil {
		return OpResult{DiagnosticRefusal: "status-area-stats-unavailable"}, skillCastRefused
	}
	plan, planned := rt.planAreaVictims(areaPlanInput{division: division, caster: c, snapshot: snapshot, skill: skill,
		attacker: attacker, victims: victims, impacts: 1, now: now})
	if !planned {
		return OpResult{DiagnosticRefusal: "status-area-plan"}, skillCastRefused
	}
	var commit areaCommit
	var battleFrames []wire.Frame
	var refusal uint16
	roster := rt.monsterRewardRoster(division, c, now)
	if !rt.deps.UpdateMany(roster.characters, "release-status-area", func() bool {
		if !enterworld.CharacterAlive(c) || !enterworld.SkillLearned(c, skill.ID) {
			return false
		}
		cost, code := rt.offensivePhaseCost(division, c, skill, now, pending)
		refusal = code
		if code != 0 {
			return false
		}
		if len(plan.victims) > 0 {
			var ok bool
			if commit, ok = rt.commitAreaInDoor(division, c, roster, &plan, now); !ok {
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
	published := rt.publishArea(division, snapshot, skill, 1, plan, commit, now)
	success := wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: gid, InstanceToken: pending.token}
	released := wire.SkillCastReleaseFrame(pending.token, 0)
	if len(published.targets) > 0 {
		released = wire.SkillCastUntargetedAreaReleaseFrame(success, published.targets)
	}
	rt.queueSkillCastClose(division, c.Name, gid, pending.token, skill, 0, now+int64(skill.ActionDurationMs))
	vitals := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshWithSourcePayload(gid, simulation.VitalsSourceSkillRecovery, rt.publishedVitals(division, c))}
	public := append([]wire.Frame{released}, published.after...)
	public = append(public, battleFrames...)
	actor := append(append([]wire.Frame{}, public...), vitals)
	actor = append(actor, commit.playerActor...)
	private := append([]wire.Frame{vitals}, wire.ProgressionPrivateFrames(commit.playerActor)...)
	out := OpResult{Frames: actor, Broadcast: public, ActorPrivate: private, Recipients: published.recipients}
	return mergeOpResults(out, published.returned), skillCastAccepted
}
