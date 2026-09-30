/*
===========================================================================

equipmentreinforcement.go - equipment-owned STR/INT scaling coefficients

The keeper graph owns derived attack and defense. Equipment replaces the
unarmed or unarmored base coefficients; it does not add a second independent
STR/INT contribution. Rebuilding a snapshot naturally restores absent or broken
items to the base graph.

===========================================================================
*/
package combat

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
)

const (
	physicalAttackMinimumReinforcement = uint16(34)
	magicalAttackMinimumReinforcement  = uint16(35)
	physicalAttackMaximumReinforcement = uint16(36)
	magicalAttackMaximumReinforcement  = uint16(37)
	physicalArmorReinforcementBase     = uint16(37)
	magicalArmorReinforcementBase      = uint16(43)
	physicalShieldReinforcement        = uint16(58)
	magicalShieldReinforcement         = uint16(59)
	reinforcementPhysicalVariance      = uint(1)
	reinforcementMagicalVariance       = uint(2)
	reinforcementVarianceMaximum       = float64(31)
	reinforcementPercent               = float64(100)
)

/*
================
reinforcementCoefficient

495D10 spills variance/31 to float32. Keep the interpolation's float32 spill
before converting the v1.150 fractional reference to a keeper percentage.
The v1.150 table has no reinforcement-per-plus term.
================
*/
func reinforcementCoefficient(source enterworld.ItemStatRange, variance uint8) float32 {
	fraction := float64(float32(float64(variance) / reinforcementVarianceMaximum))
	delta := float32((source.Max - source.Min) * fraction)
	value := float32(source.Min + float64(delta))
	return float32(float64(value) * reinforcementPercent)
}

/*
================
equipmentReinforcementWrites

497830 replaces weapon parameters 34..37, armor piece parameters 38..49,
and shield parameters 58..59 through source zero. Port inference: v1.150
references express both families as fractions, so normalize both to keeper
percentages, matching the initialized armor graph and weapon insertion units.
================
*/
func equipmentReinforcementWrites(ref *enterworld.ItemRef, bits uint64) []paramkeeper.Write {
	source := ref.Combat
	physical := varianceAt(bits, reinforcementPhysicalVariance)
	magical := varianceAt(bits, reinforcementMagicalVariance)
	tid := ref.TypeFlags()
	if isWeaponFamily(tid) {
		return []paramkeeper.Write{
			{Parameter: physicalAttackMinimumReinforcement, Value: reinforcementCoefficient(source.PhysicalReinforcement.Minimum, physical)},
			{Parameter: physicalAttackMaximumReinforcement, Value: reinforcementCoefficient(source.PhysicalReinforcement.Maximum, physical)},
			{Parameter: magicalAttackMinimumReinforcement, Value: reinforcementCoefficient(source.MagicalReinforcement.Minimum, magical)},
			{Parameter: magicalAttackMaximumReinforcement, Value: reinforcementCoefficient(source.MagicalReinforcement.Maximum, magical)},
		}
	}
	if isBodyProtectorFamily(tid) {
		return []paramkeeper.Write{
			{Parameter: physicalShieldReinforcement, Value: reinforcementCoefficient(source.PhysicalDefenseReinforcement, physical)},
			{Parameter: magicalShieldReinforcement, Value: reinforcementCoefficient(source.MagicalDefenseReinforcement, magical)},
		}
	}
	if isWearArmorFamily(tid) {
		part := uint16(ref.TypeIDs[3])
		return []paramkeeper.Write{
			{Parameter: physicalArmorReinforcementBase + part, Value: reinforcementCoefficient(source.PhysicalDefenseReinforcement, physical)},
			{Parameter: magicalArmorReinforcementBase + part, Value: reinforcementCoefficient(source.MagicalDefenseReinforcement, magical)},
		}
	}
	return nil
}
