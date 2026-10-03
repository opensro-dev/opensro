/*
===========================================================================

noise.go - the Bard's Noise: monsters do not attack its owner first

Noise (SKILL_EU_BARD_FORGETA_ATTACK_A, dura(900000) pola(1,lv) getv(BDMD))
is a timed self effect; enterworld admits it as SkillTimedEffect.Preemptive
and acceptTimedSelfEffect installs it with its buff icon. This module
answers the monster leg's question for one player: which first-attack
protection does it carry right now (simulation.MonsterMoverOps.
FirstAttackGuard), and the acquisition scan applies it (5299E0's last
branch, monster.FirstAttackProtected).

Owner's rule: Noise is the Bard's own, lasts its full 15 minutes even when
the Bard attacks (the row authors no skc, so no event retires it), and a
monster the Bard attacks, or one already fighting the Bard, fights back
normally: retaliation and an owned target never pass through the scan.

===========================================================================
*/

package action

import "opensro.online/server/internal/game/world/monster"

/*
================
FirstAttackGuard

The union of the live Noise instances on the player playerGID: the grade
bits of every one, and the highest level. An instance past its deadline,
or stopped, that the effect tick has not retired yet no longer protects.
================
*/
func (rt *Runtime) FirstAttackGuard(division string, playerGID uint32, nowMs int64) monster.FirstAttackGuard {
	var guard monster.FirstAttackGuard
	skills := rt.deps.SkillData()
	if skills == nil || rt.effects == nil {
		return guard
	}
	character := rt.findCharacterByGid(division, playerGID)
	if character == nil {
		return guard
	}
	for _, effect := range rt.effects.Snapshot(division, character.Name) {
		if effect.StopRequested || effect.DurationPresent && nowMs >= effect.ExpiresAtMs {
			continue
		}
		row, ok := skills.SkillByID(effect.SkillID)
		if !ok || !row.TimedEffect.Preemptive.Present {
			continue
		}
		guard.Mask |= row.TimedEffect.Preemptive.Mask
		guard.Level = max(guard.Level, row.TimedEffect.Preemptive.Level)
	}
	return guard
}
