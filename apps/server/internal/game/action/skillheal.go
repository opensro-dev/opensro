/*
===========================================================================

skillheal.go - heal amounts and their application

Both native heal routines, CSkillManager_ApplySkillHeal (5A0850) for a cast
and CSkillManager_ApplyHealRecovery (5A09F0) for the eshp aura, run the
same three steps: a base amount from the heal block, the recipient's
0xAA / 0xAB scale, then the caster's weapon term. The result reaches the
recipient through CGObjChar_ApplyReducedRecovery (4A86A0).

===========================================================================
*/

package action

import (
	"strconv"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
healRoutine

Cast healing raises its flat amount; aura healing takes a share of the
maximum. Keep this choice explicit before applying common recovery rules.
================
*/
type healRoutine uint8

const (
	healCast healRoutine = iota // 5A0850: a percent raises the flat amount
	healAura                    // 5A09F0: a percent is a share of the maximum
)

/*
===============================================================================

AMOUNTS

===============================================================================
*/

/*
==================
skillHealAmounts

The amount half of 5A0850 / 5A09F0 for one recipient.
==================
*/
func (rt *Runtime) skillHealAmounts(division string, recipient, caster *enterworld.Character, skill enterworld.SkillRow, routine healRoutine) (hp, mp int64, ok bool) {
	stats, _, err := rt.playerCombatStats(division, recipient)
	if err != nil {
		return 0, 0, false
	}
	maxHP, maxMP, _, _ := rt.playerKeeperVitals(division, recipient)

	heal := skill.Heal
	// 59425E: a heal that reads getv HLRU raises both percent words by the
	// caster's value (Faith).
	if skill.Attack.Parameters.Has(enterworld.ParameterHealRecoveryUp) && caster != nil {
		casterStats, _, err := rt.playerCombatStats(division, caster)
		if err != nil {
			return 0, 0, false
		}
		up := casterStats.SkillParameters[enterworld.ParameterHealRecoveryUp]
		heal.HPPercent += up
		heal.MPPercent += up
	}
	if routine == healAura {
		hp, mp = auraHealBase(heal, maxHP, maxMP)
	} else {
		hp, mp = castHealBase(heal, maxHP)
	}

	scaleHP, _ := stats.Param(0xaa)
	scaleMP, _ := stats.Param(0xab)
	hp = healScale(hp, scaleHP)
	mp = healScale(mp, scaleMP)

	if heal.WeaponHP || heal.WeaponMP {
		bonusHP, bonusMP, ok := rt.weaponHealBonus(division, caster, heal)
		if !ok {
			return 0, 0, false
		}
		hp += bonusHP
		mp += bonusMP
	}
	return hp, mp, true
}

/*
==================
castHealBase

5A0850: a percent word raises its flat amount by that percent of itself
(5A08B0 / 5A08E2). nmh (+0x598) instead takes the HP percent of maximum
HP. The MP percent always works on the flat amount.
==================
*/
func castHealBase(heal enterworld.SkillHeal, maxHP int64) (hp, mp int64) {
	hp, mp = int64(int32(heal.HP)), int64(int32(heal.MP))

	if heal.HPPercent != 0 {
		if heal.OfMaxHP {
			hp = crtFtol(float64(int32(maxHP)) * float64(heal.HPPercent) / 100)
		} else {
			hp = raisedByPercent(hp, heal.HPPercent)
		}
	}
	if heal.MPPercent != 0 {
		mp = raisedByPercent(mp, heal.MPPercent)
	}
	return hp, mp
}

/*
==================
auraHealBase

5A09F0: a percent word replaces its flat amount with that share of the
recipient's maximum. The MP product is a 32-bit IMUL read unsigned.
==================
*/
func auraHealBase(heal enterworld.SkillHeal, maxHP, maxMP int64) (hp, mp int64) {
	hp, mp = int64(int32(heal.HP)), int64(int32(heal.MP))

	if heal.HPPercent != 0 {
		hp = crtFtol(float64(int32(maxHP)) * float64(heal.HPPercent) / 100)
	}
	if heal.MPPercent != 0 {
		product := uint32(int32(maxMP) * int32(heal.MPPercent))
		mp = crtFtol(float64(product) / 100)
	}
	return hp, mp
}

/*
================
raisedByPercent

The native IMUL wraps at 32 bits before its unsigned result reaches ftol.
================
*/
func raisedByPercent(flat int64, percent uint32) int64 {
	product := uint32(int32(flat) * int32(percent))
	return flat - crtFtol(float64(product)/-100)
}

/*
================
healScale

Applies the recipient's AA/AB amplification before recovery reductions.
================
*/
func healScale(amount int64, param float32) int64 {
	return crtFtol((float64(param)/100 + 1) * float64(int32(amount)))
}

/*
==================
weaponHealBonus

411080 for mwhh and mwmh: the caster's slot-6 item, its magical attack and
the caster's absorption ratio. An empty slot adds nothing.
==================
*/
func (rt *Runtime) weaponHealBonus(division string, caster *enterworld.Character, heal enterworld.SkillHeal) (hp, mp int64, ok bool) {
	var weapon *enterworld.InventoryRow
	for i := range caster.MissionInventory {
		if caster.MissionInventory[i].Slot == 6 {
			weapon = &caster.MissionInventory[i]
			break
		}
	}
	if weapon == nil {
		return 0, 0, true
	}

	ref, found := rt.statCatalogs().Items.ItemRefByCodename(weapon.Codename)
	if !found || ref.Combat == nil {
		return 0, 0, false
	}
	bits, err := strconv.ParseUint(weapon.VarianceBits, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	stats, _, err := rt.playerCombatStats(division, caster)
	if err != nil {
		return 0, 0, false
	}

	intellect, _ := stats.Param(2)
	ratio := combat.AbsorptionRatio(stats.Level, intellect)
	low, high := combat.WeaponMagicalAttack(ref, bits, uint8(max(0, min(weapon.Plus, 255))))
	if heal.WeaponHP {
		hp = int64(combat.WeaponHealBonus(low, high, ratio, heal.WeaponHPWord))
	}
	if heal.WeaponMP {
		mp = int64(combat.WeaponHealBonus(low, high, ratio, heal.WeaponMPWord))
	}
	return hp, mp, true
}

/*
===============================================================================

APPLICATION

===============================================================================
*/

/*
==================
applySkillRecovery

CGObjChar_ApplyReducedRecovery (4A86A0) into
CGObjChar_ApplyHealthAndManaOffset (4A87D0), source 0x40:

  - the recipient's 0x8F / 0x90 cut each amount (Panic / Combustion)
  - a negative result becomes 0
  - a dead recipient is untouched
  - each gauge clamps at its maximum

The caller holds the recipient's door. The frame is empty when nothing
changed.
==================
*/
func (rt *Runtime) applySkillRecovery(division string, who *enterworld.Character, hp, mp int64) (wire.Frame, bool) {
	stats, _, err := rt.playerCombatStats(division, who)
	if err != nil {
		return wire.Frame{}, false
	}

	if !enterworld.CharacterAlive(who) || hp == 0 && mp == 0 {
		return wire.Frame{}, true
	}

	maxHP, maxMP, currentHP, currentMP := rt.playerKeeperVitals(division, who)
	hpReduction, _ := stats.Param(combat.HPRecoveryReductionParameter)
	mpReduction, _ := stats.Param(combat.MPRecoveryReductionParameter)
	nextHP := combat.RecoverVital(currentHP, maxHP, hp, hpReduction)
	nextMP := combat.RecoverVital(currentMP, maxMP, mp, mpReduction)
	if nextHP == currentHP && nextMP == currentMP {
		return wire.Frame{}, true
	}
	who.CurrentHP, who.CurrentMP = &nextHP, &nextMP

	vitals := simulation.Vitals{CurrentHP: uint32(nextHP), CurrentMP: uint32(nextMP)}
	payload := simulation.VitalsRefreshWithSourcePayload(enterworld.ObjectIDForCharacter(who), simulation.VitalsSourceSkillRecovery, vitals)
	return wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: payload}, true
}
