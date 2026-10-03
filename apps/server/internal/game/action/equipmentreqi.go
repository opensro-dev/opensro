/*
===========================================================================

equipmentreqi.go - effects that end when their equipment is gone

CSkillManager_ReevaluateEquipmentRequirements (59F0E0) runs after an item
move that touches an equipment slot (50F1F0) and when an item breaks
(CGObjPC_OffsetItemDurability; this port does not wear items yet). Every
self-applied instance (context mode 1) whose reqi list the new equipment
no longer meets is retired - except any SKILL_CH_FIRE_SHIELD_ row, which
the native exempts by name (59F397). Instances cast on someone else are
never re-checked.

A party aura's child is installed in mode 1 too, but its reqi (the Bard's
harp) belongs to the caster, who is re-checked on its own instance. Inferred:
the child is never re-checked against the member's equipment, otherwise any
equipment move of a harpless member ended its march or tambour.

===========================================================================
*/

package action

import (
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
)

// retireUnmetEquipmentEffects is 59F0E0 for c's committed equipment. The
// caller holds c's door and publishes the returned effects.
func (rt *Runtime) retireUnmetEquipmentEffects(division string, c *enterworld.Character) []statuseffect.Effect {
	skills := rt.deps.SkillData()
	if rt.effects == nil || skills == nil {
		return nil
	}
	items := rt.statCatalogs().Items
	var tokens []uint32
	for _, e := range rt.effects.Snapshot(division, c.Name) {
		if e.Phase != 1 || e.AuraParentToken != 0 {
			continue
		}
		row, ok := skills.SkillByID(e.SkillID)
		if !ok || !row.Reqi.Present || strings.Contains(row.Codename, "SKILL_CH_FIRE_SHIELD_") {
			continue
		}
		if reqiRefusal(c, items, row.Reqi) != 0 {
			tokens = append(tokens, e.InstanceToken)
		}
	}
	return rt.effects.RetireInstances(division, c.Name, tokens)
}
