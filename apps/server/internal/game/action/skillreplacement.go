/*
===========================================================================

skillreplacement.go - buff replacement validation on one character

CSkillManager_ValidateBuffReplacement (59D870) as the untargeted, unlinked
casts call it (58E2F4): the new effect against the character's active list
and its current command's casting states. A refusal is 0x300C.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
)

/*
==================
requestSelfEffectReplacement

The validation phase, not the later effect insertion or timed-job restore.
The caller holds the character authority and division operation doors.
==================
*/
func (rt *Runtime) requestSelfEffectReplacement(division string, c *enterworld.Character, skill enterworld.SkillRow) bool {
	return rt.requestEffectReplacement(division, c, skill, effectReplacementContext{casterIsRecipient: true})
}

/*
==================
requestReleasedEffectReplacement

requestSelfEffectReplacement for the caster of skill at its own prepared
cast's release. A positive-time cast keeps its command current through its
release (currentcommand.go), so that command is the one being released,
not another command in progress: native validates at admission (58E2F4),
before the command is current, and its release installs without reading
its own states as a conflict. Live, a heal over time with a cast time
(Healing Orbit) was refused on its own caster by exactly that conflict.
==================
*/
func (rt *Runtime) requestReleasedEffectReplacement(division string, c *enterworld.Character, skill enterworld.SkillRow) bool {
	return rt.requestEffectReplacement(division, c, skill, effectReplacementContext{casterIsRecipient: true, released: true})
}

/*
================
effectReplacementContext

Incoming caster identity is independent of the existing effect's area source.
Only a self release may ignore its own current command's casting states.
================
*/
type effectReplacementContext struct {
	casterIsRecipient bool
	released          bool
}

/*
==================
requestEffectReplacement

The shared validation. A self release skips a current command of skill itself
(requestReleasedEffectReplacement).
==================
*/
func (rt *Runtime) requestEffectReplacement(division string, c *enterworld.Character, skill enterworld.SkillRow, context effectReplacementContext) bool {
	if !skill.ReplacementPinned || skill.Replacement.Lnks {
		return false
	}
	descriptors := map[uint32]statuseffect.ReplacementDescriptor{skill.ID: skill.Replacement}
	for _, old := range rt.effects.Snapshot(division, c.Name) {
		row, ok := rt.deps.SkillData().SkillByID(old.SkillID)
		if !ok || !row.ReplacementPinned {
			return false
		}
		descriptors[row.ID] = row.Replacement
	}
	application := statuseffect.ReplacementApplication{
		Effect:      statuseffect.Effect{DivisionID: division, CharacterName: c.Name, SkillID: skill.ID, SkillGroup: skill.Group},
		Descriptors: descriptors, CasterIsRecipient: context.casterIsRecipient,
	}
	if current, exists := rt.currentSkillCommandFor(division, c); exists && !(context.released && context.casterIsRecipient && current.skillID == skill.ID) {
		if !current.pinned {
			return false
		}
		application.CurrentPacked = current.descriptor.PackedStates
		if current.descriptor.Ovl2Present {
			application.CurrentOvl2 = current.descriptor.Ovl2
		}
	}
	return rt.effects.RequestReplacement(application)
}
