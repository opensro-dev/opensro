/*
===========================================================================

healbonus.go - the caster's weapon term on a heal

Formulae_CalculateWeaponMagicalHealBonus (411080), used by both heal
routines when a row carries mwhh or mwmh.

===========================================================================
*/

package combat

import (
	"math"

	"opensro.online/server/internal/game/enterworld"
)

/*
==================
WeaponMagicalAttack

The item's +0x1C0 / +0x1C4 exactly as CGItemEquip_CalculateBaseStats (495D60)
stores them at 496508 / 49657D:

	lo + float32((hi - lo) * variance + float32(perPlus * plus))

Bounds are the reference's magical attack (+0x240/+0x244, +0x24C/+0x250),
per-plus +0x254, variance index 5 (CGItemEquip_GetStatVariance 495D10:
bits / 31, stored to float32). The wire rounds these to integers; the
server keeps floats.
==================
*/
func WeaponMagicalAttack(ref *enterworld.ItemRef, varianceBits uint64, plus uint8) (minimum, maximum float32) {
	return weaponAttackRange(ref.Combat.MagicalAttack, varianceAt(varianceBits, 5), plus)
}

/*
================
WeaponPhysicalAttack

495D60 writes the physical pair at item +1B8/+1BC using variance index four.
Threat consumes these floats before the combat display rounds them.
================
*/
func WeaponPhysicalAttack(ref *enterworld.ItemRef, varianceBits uint64, plus uint8) (minimum, maximum float32) {
	return weaponAttackRange(ref.Combat.PhysicalAttack, varianceAt(varianceBits, 4), plus)
}

/*
================
weaponAttackRange

Both weapon lanes share the native plus, spread and final float stores.
================
*/
func weaponAttackRange(attack enterworld.ItemAttackRange, varianceWord, plus uint8) (minimum, maximum float32) {
	variance := float64(float32(float64(varianceWord) / 31))

	stat := func(r enterworld.ItemStatRange) float32 {
		lo, hi := float32(r.Min), float32(r.Max)
		plusTerm := float32(float64(float32(r.PerPlus)) * float64(plus))
		spread := float32((float64(hi)-float64(lo))*variance + float64(plusTerm))
		return float32(float64(lo) + float64(spread))
	}

	return stat(attack.Minimum), stat(attack.Maximum)
}

/*
==================
AbsorptionRatio

Formulae_CalculateAbsorptionRatio (410AB0) for a player:

	r = float32(INT / (((level - 1) * 5 + 40) * 0.8))

held to [0, 1.2]. NaN falls through both compares and yields 1.2.
==================
*/
func AbsorptionRatio(level uint8, intellect float32) float32 {
	denominator := ((float64(float32(level))-1)*5 + 40) * float64(float32(0.8))
	ratio := float32(float64(intellect) / denominator)

	switch {
	case ratio < 0:
		return 0
	case !(ratio < float32(1.2)):
		return float32(1.2)
	}
	return ratio
}

/*
==================
WeaponHealBonus

411080 once the item and ratio are known. The native name says "Physical",
but the fields it averages are the weapon's magical attack.

	ftol(float32((max + min) * 0.5) * ratio * word / 100)

==================
*/
func WeaponHealBonus(minimum, maximum, ratio float32, word uint32) int32 {
	average := float32((float64(maximum) + float64(minimum)) * 0.5)

	v := float64(average) * float64(ratio) * float64(word) / 100
	if math.IsNaN(v) || v >= 1<<31 || v < -(1<<31) {
		return math.MinInt32
	}
	return int32(v)
}
