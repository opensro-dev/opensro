/*
===========================================================================

uniquevariant.go - who may damage a build-locked unique copy

Port-only, not native (SRO_STR_INT_UNIQUES, world/monster/strint_uniques.go).
A STR copy admits only characters whose permanent allocation is dominated by
strength, an INT copy only the magical mirror, and the shipped unique admits
everyone. The rule is asked of the reference, so it costs one comparison and
disappears entirely when the switch is off (every shipped reference carries
UniqueBuildAny).

The one admission point for direct damage is planCreatureMonsterImpacts: a
single-target cast, a bow release, every area family, every credited effect
lane (hawk, linked pulse, damage return, pet shield) and every pet hit build
their plans through it. A refused impact never becomes a plan, so it also
never rolls a status: a lock cannot be bypassed by burning a copy from an
inadmissible caster and waiting.

Pet and COS lanes carry the owner's credit id and no character; the owner is
resolved here, outside the population lock, the same way the reward roster
resolves it.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
uniqueVariantRefused

Whether the target is a build-locked copy that refuses this character. The
permanent pools decide (domain.CharacterStrength/Intellect), never gear,
buffs or the damage type: a strictly physical total takes the STR copy, and
every other total, a balanced one included, takes the INT copy.
================
*/
func uniqueVariantRefused(c *enterworld.Character, target monster.Instance) bool {
	if c == nil {
		return false
	}
	if target.Ref.UniqueBuild == monster.UniqueBuildAny {
		return false
	}
	return target.Ref.AttackRefused(domain.CharacterStrength(c), domain.CharacterIntellect(c))
}

/*
================
monsterImpactRefused

The plan-time admission for one caster. A player is asked directly; a pet or
COS is asked through the character that owns it, because the copy refuses an
owner rather than the body that swings.
================
*/
func (rt *Runtime) monsterImpactRefused(caster monsterImpactCaster, target monster.Instance) bool {
	if target.Ref.UniqueBuild == monster.UniqueBuildAny {
		return false
	}
	if caster.character != nil {
		return uniqueVariantRefused(caster.character, target)
	}
	if caster.credit == 0 {
		return false
	}
	return uniqueVariantRefused(rt.findCharacterByGid(caster.division, caster.credit), target)
}
