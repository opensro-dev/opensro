/*
===========================================================================

skillcooldown.go - effective reuse timing at the player admission boundary

Every skill family registers through this owner. Published reference rows
stay immutable; already admitted deadlines survive later status changes.

===========================================================================
*/

package action

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
)

const actionSpeedParameter = 0x8c

/*
================
playerSkillCooldown

Use the complete keeper projection, shared with attack and resource checks.
Those admission checks reject an invalid loadout before registration. Keep
its authored reuse delay if a teardown caller has already lost that data.
================
*/
func (rt *Runtime) playerSkillCooldown(division string, character *enterworld.Character, skill enterworld.SkillRow) uint32 {
	stats, _, err := rt.playerCombatStats(division, character)
	if err != nil {
		log.WithError(err).Error("skill cooldown keeper projection failed")
		return skill.CoolTimeMs
	}
	percent, _ := stats.Param(actionSpeedParameter)
	return skill.CooldownDurationMs(percent)
}

/*
================
registerPlayerSkillCooldown

Copy the row only for the existing cooldown-map writer. Other consumers
must continue to see authored casting, animation and effect lifetimes.
================
*/
func (rt *Runtime) registerPlayerSkillCooldown(division string, character *enterworld.Character, skill enterworld.SkillRow, now int64) {
	// Instant actions skip registration at 586C03 for an authored zero
	// cooldown. Persistent and projectile handlers still call 64C700.
	if skill.ActionHandler == enterworld.SkillActionInstant && skill.CoolTimeMs == 0 {
		return
	}
	if skill.ActionKind == 2 && skill.ActionDurationMs > 0 {
		stats, _, err := rt.playerCombatStats(division, character)
		if err == nil {
			percent, _ := stats.Param(actionSpeedParameter)
			character.SkillActionRecoveryUntilMs = now + int64(skill.ActionRecoveryDurationMs(percent))
		}
	}
	skill.CoolTimeMs = rt.playerSkillCooldown(division, character, skill)
	registerOffensiveCooldown(character, skill, now)
}
