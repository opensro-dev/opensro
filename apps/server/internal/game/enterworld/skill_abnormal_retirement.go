/*
===========================================================================

skill_abnormal_retirement.go - native status-driven skill retirement policy

The same authored selection applies to player and monster skill instances.
Effect owners perform teardown; this predicate never mutates an actor.

===========================================================================
*/

package enterworld

import "opensro.online/server/internal/game/item/statuseffect"

const abnormalPreservedSkillCategory = 3

/*
================
RetiresForAbnormal

59FF80 selects the current command and category-3 ao/pw, preserving cbuf.
5A0100 instead removes active ordinary buffs, preserving category 3 and
the cbuf/nbuf/bbuf markers at descriptor +358/+35C/+360.
================
*/
func (s SkillRow) RetiresForAbnormal(all, current bool) bool {
	if all {
		return s.ActionKind != 0 && s.Replacement.Category != abnormalPreservedSkillCategory &&
			!s.BuffCancelConfirm && !s.VoluntaryCancelBlocked && !s.BuffSecondary
	}
	return statuseffect.RetirementSelected(false, s.ActionKind, current, s.Replacement.Category,
		s.CastGate.Ao, s.CastGate.Pw, s.BuffCancelConfirm)
}
