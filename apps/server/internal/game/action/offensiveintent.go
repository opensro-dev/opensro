/*
===========================================================================

offensiveintent.go - admitting and resolving an offensive skill command

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
resolveOffensiveSkill

Both immediate sequences and persistent attacks use the same learned-skill,
weapon and approach admission. Their release producers remain separate.
================
*/
func (rt *Runtime) resolveOffensiveSkill(c *enterworld.Character, id uint32) (enterworld.SkillRow, combat.Loadout, string) {
	if c == nil || !enterworld.CharacterAlive(c) || !enterworld.SkillLearned(c, id) || rt.deps.SkillData() == nil {
		return enterworld.SkillRow{}, combat.Loadout{}, "offensive-skill-unavailable"
	}
	skill, known := rt.deps.SkillData().SkillByID(id)
	sequence, immediate := enterworld.OffensiveSequence(rt.deps.SkillData(), id)
	if !known || skill.Group == 0 || (!immediate && !skill.TimedEffect.Periodic.Pinned && !skill.Threat.Only) {
		return enterworld.SkillRow{}, combat.Loadout{}, "offensive-shape-unsupported"
	}
	if immediate {
		skill = sequence[0]
	}
	_, loadout, err := combat.PlayerStats(c, rt.statCatalogs())
	if err != nil || !skillWeaponAdmitted(loadout, skill) {
		return skill, loadout, "offensive-weapon-incompatible"
	}
	return skill, loadout, ""
}

/*
================
resolveOffensiveStage

Only a server-owned continuation carries rootID. Network sub-row requests
still take resolveOffensiveSkill and fail the learned-root gate.
================
*/
func (rt *Runtime) resolveOffensiveStage(c *enterworld.Character, rootID, stageID uint32) (enterworld.SkillRow, combat.Loadout, string) {
	root, loadout, refusal := rt.resolveOffensiveSkill(c, rootID)
	if refusal != "" {
		return root, loadout, refusal
	}
	sequence, ok := enterworld.OffensiveSequence(rt.deps.SkillData(), rootID)
	if ok {
		for index, stage := range sequence {
			if index > 0 && stage.ID == stageID && skillWeaponAdmitted(loadout, stage) {
				return stage, loadout, ""
			}
		}
	}
	return enterworld.SkillRow{}, loadout, "offensive-continuation-invalid"
}

/*
================
beginOffensiveSkill

Retain the target through approach; release revalidates authority and costs.
================
*/
func (rt *Runtime) beginOffensiveSkill(division string, c, snapshot *enterworld.Character, cast wire.SkillAction) OpResult {
	if c == nil {
		return OpResult{}
	}
	skill, loadout, refusal := rt.resolveOffensiveSkill(snapshot, cast.ActionId)
	if refusal != "" {
		return rt.offensiveAdmissionRefusal(refusal)
	}
	if rt.Monsters == nil {
		return rt.offensiveAdmissionRefusal("offensive-shape-unsupported")
	}
	if !cast.HasTarget || cast.HasGroundTarget || cast.TargetGid == 0 {
		return offensiveRefusal(0x3011)
	}
	now := rt.Now().UnixMilli()
	if _, code := rt.offensiveCost(division, snapshot, skill, now); code != 0 {
		return offensiveRefusal(code)
	}
	// Command acceptance runs phase 0x37 (4ACED4), which carries the ammo
	// bit 0x20 (58E32D): an empty bow is refused at the press, before the
	// approach walks the archer into range.
	if skill.Ammunition.Count != 0 {
		if _, valid := rt.planEquippedAmmunition(snapshot, loadout.WeaponKind, ammunitionSpent(skill, true)); !valid {
			return offensiveRefusal(0x300e)
		}
	}
	reach := rt.playerActionReach(division, snapshot, skill, loadout)
	intent := basicAttackIntent{DivisionID: division, CharacterName: c.Name, TargetGid: cast.TargetGid, SkillID: skill.ID, ActionReach: reach, SingleCast: true}
	rt.setCombatIntent(intent)
	return rt.advanceBasicAttackIntent(c, intent, now)
}

/*
================
offensiveAdmissionRefusal

Keep unsupported-program diagnostics distinct from native gameplay refusals.
================
*/
func (rt *Runtime) offensiveAdmissionRefusal(reason string) OpResult {
	var result OpResult
	switch reason {
	case "offensive-shape-unsupported":
		notice := wire.NotificationFrame("This skill is not implemented yet.")
		result.Frames, result.ActorPrivate = []wire.Frame{notice}, []wire.Frame{notice}
	case "offensive-weapon-incompatible":
		result = offensiveRefusal(0x300d)
	case "offensive-skill-unavailable":
		result = offensiveRefusal(0x3003)
	}
	result.DiagnosticRefusal = reason
	return result
}
