/*
===========================================================================

creatureimpact.go - damage-free creature skills share the normal impact owner

The complete creature program is compiled separately from player admission.
A status-only action produces one successful record without a damage roll.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
creatureImpactCount
================
*/
func creatureImpactCount(skill enterworld.SkillRow) int {
	if skill.CreatureStatusCast || skill.StatusCast {
		return 1
	}
	return int(skill.Attack.ImpactCount)
}

/*
================
resolveCreatureImpact
================
*/
func (rt *Runtime) resolveCreatureImpact(actor criticalActor, skill enterworld.SkillRow, attacker, defender combat.Stats) (combat.Result, error) {
	out, err := rt.resolveCreatureImpactBehindWall(actor, skill, attacker, defender, nil)
	return out.Defender, err
}

/*
================
resolveCreatureImpactBehindWall

58E5F0 omits damage calculation when the skill has no att program.
================
*/
func (rt *Runtime) resolveCreatureImpactBehindWall(actor criticalActor, skill enterworld.SkillRow, attacker, defender combat.Stats, wall *enterworld.SkillWall) (combat.WallOutcome, error) {
	if skill.CreatureStatusCast {
		return combat.WallOutcome{Defender: combat.Result{ResultFlags: 1}}, nil
	}
	return rt.resolveCombatBehindWall(actor, skill, attacker, defender, wall)
}
