/*
===========================================================================

skilllifesteal.go - Life Drain: the hit takes HP and the caster gets it

The Warlock's lfst hits (enterworld/skilllifesteal.go) run the ordinary
offensive release, single target or area: admission, cost, approach, the
hit record, the status rolls, aggression, rewards and the cast bracket
are skillcombat.go's and skillarea.go's. This file owns what lfst
changes: the hit's value (Formulae_CalculateLifeSteal 40F750, called from
SkillCombat_CalculateHitOutcome 58F4B5 in place of the att lanes) and the
caster's recovery of it (CGObjChar_ApplyReducedRecovery, source 0x40).

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// fullAreaPercent is a primary victim's byte +5: the whole result.
const fullAreaPercent = 100

/*
================
lifeStealBase

40F750's base: lfst, the caster's BSHP when the row reads it (getv
+0x534), and the mwhs share of the magical weapon (411080). Reads the
caster's stats, so it runs before the commit door.
================
*/
func (rt *Runtime) lifeStealBase(division string, caster *enterworld.Character, steal enterworld.SkillLifeSteal, attacker combat.Stats) (int64, bool) {
	base := int64(steal.Amount)
	if steal.Power {
		base += int64(attacker.SkillParameters[enterworld.ParameterLifeStealPower])
	}
	if steal.WeaponPercent == 0 {
		return base, true
	}
	weapon, ok := rt.casterMagicalWeapon(division, caster)
	if !ok {
		return 0, false
	}
	if weapon.armed {
		base += int64(combat.WeaponHealBonus(weapon.low, weapon.high, weapon.ratio, steal.WeaponPercent))
	}
	return base, true
}

/*
================
lifeStealResult

One victim's record: a normal result carrying the life taken at the
victim's area percent (byte +5 of its target entry; 100 for the primary).
================
*/
func lifeStealResult(base int64, attacker, defender combat.Stats, targetHP, percent uint32) combat.Result {
	damage := combat.LifeSteal(base, attacker.Level, defender.Level, targetHP, percent)
	return combat.Result{Damage: min(damage, wire.MaxSkillActionDamage), ResultFlags: 1}
}

/*
================
commitLifeSteal

Inside the caster's door, after the hits commit: the caster recovers the
life its committed records took, through the recipient's recovery cuts
(Panic). The frame is the caster's 0x33A6, empty when nothing changed.
================
*/
func (rt *Runtime) commitLifeSteal(division string, caster *enterworld.Character, impacts ...[]simulation.MonsterDamageResult) wire.Frame {
	var taken int64
	for _, sequence := range impacts {
		for _, impact := range sequence {
			taken += int64(impact.Damage)
		}
	}
	if taken == 0 {
		return wire.Frame{}
	}
	frame, _ := rt.applySkillRecovery(division, caster, taken, 0)
	return frame
}
