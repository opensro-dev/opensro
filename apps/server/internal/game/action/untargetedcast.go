/*
===========================================================================

untargetedcast.go - admission and preparation of untargeted hostile casts

Lightning Impact and the planted Fire Trap prepare without a target. Both
share the owner-state gates, the execution mask, the phase cost and the
prepared self-cast envelope; only their release owners differ. The Bard's
zero-casting-time area burst shares the admission alone.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
beginUntargetedCast

Shared admission for untargeted hostile casts: owner state, the execution
mask, the phase cost and, without a pending release, the preparation that
charges cooldown and opens the cast. done is false only for a release.
================
*/
func (rt *Runtime) beginUntargetedCast(division string, c, snapshot *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, now int64, pending *pendingProjectileCast, mark func(*pendingProjectileCast)) (OpResult, skillCastDecision, bool) {
	if refusal, refused := rt.admitUntargetedCast(division, snapshot, cast, skill, now, pending); refused {
		return refusal, skillCastRefused, true
	}
	if pending != nil {
		return OpResult{}, skillCastAccepted, false
	}
	var refusal uint16
	if !rt.deps.Update(c, "prepare-untargeted-cast", func() bool {
		if !enterworld.CharacterAlive(c) || !enterworld.SkillLearned(c, skill.ID) {
			return false
		}
		_, refusal = rt.offensiveCost(division, c, skill, now)
		if refusal != 0 {
			return false
		}
		rt.startSkillCast(division, c, skill, now)
		rt.registerPlayerSkillCooldown(division, c, skill, now)
		return true
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal), skillCastRefused, true
		}
		return OpResult{DiagnosticRefusal: "untargeted-cast-prepare-commit-refused"}, skillCastRefused, true
	}
	return rt.prepareUntargetedCast(division, snapshot, cast, skill, now, mark), skillCastAccepted, true
}

/*
================
admitUntargetedCast

The owner-state gates, the execution mask and the phase cost every
untargeted hostile cast passes, whether it prepares or, at zero casting
time, resolves at once (skillareaburst.go). refused reports a refusal.
================
*/
func (rt *Runtime) admitUntargetedCast(division string, snapshot *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, now int64, pending *pendingProjectileCast) (OpResult, bool) {
	if cast.HasTarget || cast.HasGroundTarget || !enterworld.CharacterAlive(snapshot) || !enterworld.SkillLearned(snapshot, skill.ID) {
		return OpResult{DiagnosticRefusal: "untargeted-cast-admission-refused"}, true
	}
	if snapshot.ActiveCOS != nil && snapshot.ActiveCOS.Mounted || snapshot.NativeTeleportMode != 0 {
		return OpResult{DiagnosticRefusal: "untargeted-cast-owner-state"}, true
	}
	if rt.skillCastPostureBlocked(division, snapshot, now) || pending == nil && rt.hasOpenSkillCast(division, snapshot.Name) {
		return OpResult{DiagnosticRefusal: "untargeted-cast-action-busy"}, true
	}
	if code := rt.skillAdmission(division, snapshot, skill, now, nil, pending, admitExecution); code != 0 {
		return offensiveRefusal(code), true
	}
	if _, code := rt.offensivePhaseCost(division, snapshot, skill, now, pending); code != 0 {
		return offensiveRefusal(code), true
	}
	return OpResult{}, false
}

/*
================
prepareUntargetedCast

The prepared self-cast envelope for an untargeted hostile release; mark
selects the release owner (a planted trap or a caster-centred status area).
================
*/
func (rt *Runtime) prepareUntargetedCast(division string, c *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, now int64, mark func(*pendingProjectileCast)) OpResult {
	out := rt.prepareSelfCast(division, c, cast, skill, now, false)
	rt.pendingSkillFinalizesMu.Lock()
	for i := range rt.pendingProjectileCasts {
		p := &rt.pendingProjectileCasts[i]
		if p.characterName == c.Name && p.divisionID == division && p.supportCast && p.cast.ActionId == skill.ID {
			p.supportCast = false
			mark(p)
		}
	}
	rt.pendingSkillFinalizesMu.Unlock()
	return out
}
