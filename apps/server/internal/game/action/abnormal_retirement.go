/*
===========================================================================

abnormal_retirement.go - status cancellation through the shared effect owner

Selection uses authored native rules. Registry retirement removes modifiers,
casting-state contributions and linked ownership; the caller defers packets
until the character transaction commits.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
)

/*
================
retireAbnormalSkills

Run inside the character authority door. Current-command identity must be
read before the later cancellation publication clears that command.
================
*/
func (rt *Runtime) retireAbnormalSkills(division string, character *enterworld.Character, all bool) []statuseffect.Effect {
	if rt.effects == nil || rt.deps.SkillData() == nil {
		return nil
	}
	command, hasCurrent := rt.currentSkillCommandFor(division, character)
	var tokens []uint32
	for _, effect := range rt.effects.Snapshot(division, character.Name) {
		row, exists := rt.deps.SkillData().SkillByID(effect.SkillID)
		if exists && row.RetiresForAbnormal(all, hasCurrent && effect.InstanceToken == command.token) {
			tokens = append(tokens, effect.InstanceToken)
		}
	}
	return rt.effects.RetireInstances(division, character.Name, tokens)
}
