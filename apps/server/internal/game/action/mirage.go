/*
===========================================================================

mirage.go - the Warlock's Mirage and Phantasma lower monster hostility
toward the caster

Mirage (SKILL_EU_WARLOCK_CONFUSIONA_AGGROLOW_A) and Phantasma (_B) are
efr(1,1,r,n,0,16) dtnt(flat,0) mwdt(567): a prepared cast that names no
target (enterworld/skillthreatdecrease.go). "Monsters stuck in a mirage
reduce their hostility": at the release up to efr's most-targets monsters
within efr's radius of the caster receive one hate event whose amounts
are negative, as Discord Wave's (discordwave.go).

SkillCombat_ApplyResultRecipients (593D62..593D90) sources a dtnt event
at the cast's target, or at the caster (arg4+8) when the cast has none;
an untargeted cast has none, so the hate cut is the caster's. The ledger
update (5473C0) clamps at zero, so a monster that holds no hate for the
caster is unchanged.

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
acceptMirage

Admission and preparation are the untargeted cast's. The release debits
the prepared cost, then cuts the caster's hostility on the monsters
around the caster (the taunt's caster-centred selection); each is a
successful zero-damage record of the release.
================
*/
func (rt *Runtime) acceptMirage(division string, c, snapshot *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, now int64, pending *pendingProjectileCast) (OpResult, skillCastDecision) {
	if !skill.Threat.Decrease || skill.TargetRequired || rt.Monsters == nil {
		return OpResult{DiagnosticRefusal: "mirage-admission-refused"}, skillCastRefused
	}
	if out, decision, done := rt.beginUntargetedCast(division, c, snapshot, cast, skill, now, pending, func(p *pendingProjectileCast) { p.threatDecrease = true }); done {
		return out, decision
	}
	rt.clearCurrentSkillCommand(division, c.Name)
	cut, ok := rt.threatDecreaseCut(division, snapshot, skill)
	if !ok {
		return OpResult{DiagnosticRefusal: "mirage-weapon-unavailable"}, skillCastRefused
	}
	victims := rt.tauntVictims(tauntCast{division: division, character: c, snapshot: snapshot, skill: skill, now: now})
	var refusal uint16
	if !rt.deps.Update(c, "release-mirage", func() bool {
		if !enterworld.CharacterAlive(c) || !enterworld.SkillLearned(c, skill.ID) {
			return false
		}
		cost, code := rt.offensivePhaseCost(division, c, skill, now, pending)
		refusal = code
		if code != 0 {
			return false
		}
		rt.commitOffensivePhaseCost(division, c, skill, cost, now, true)
		return true
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal), skillCastRefused
		}
		return OpResult{DiagnosticRefusal: "mirage-release-commit-refused"}, skillCastRefused
	}
	gid := enterworld.ObjectIDForCharacter(c)
	decrease := simulation.HostilityEvent{Attacker: gid, Aggression: -int32(cut), Percent: -int32(skill.Threat.DecreasePercent)}
	var targets []wire.SkillAreaTarget
	for _, victim := range victims {
		rt.recordSkillHostility(division, victim.Gid, []simulation.HostilityEvent{decrease}, now)
		targets = append(targets, wire.SkillAreaTarget{GID: victim.Gid, Impacts: []wire.SkillCastTargetImpact{{ResultFlags: 1}}})
	}
	released := wire.SkillCastReleaseFrame(pending.token, 0)
	if len(targets) > 0 {
		released = wire.SkillCastUntargetedAreaReleaseFrame(wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: gid, InstanceToken: pending.token}, targets)
	}
	rt.queueSkillCastClose(division, c.Name, gid, pending.token, skill, 0, now+int64(skill.ActionDurationMs))
	vitals := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshWithSourcePayload(gid, simulation.VitalsSourceSkillRecovery, rt.publishedVitals(division, c))}
	return OpResult{Frames: []wire.Frame{released, vitals}, Broadcast: []wire.Frame{released}, ActorPrivate: []wire.Frame{vitals}}, skillCastAccepted
}
