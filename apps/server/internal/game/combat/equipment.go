package combat

import (
	"fmt"
	"math"

	"opensro.online/server/internal/game/enterworld"
)

func deriveItemStats(
	ref *enterworld.ItemRef,
	varianceBits uint64,
	plus uint8,
) (Stats, bool, error) {
	var out Stats
	tid := ref.TypeFlags()
	source := ref.Combat
	if source == nil {
		return out, false, fmt.Errorf(
			"combat: item %q has no typed combat reference",
			ref.Codename,
		)
	}

	switch {
	case isWeaponFamily(tid):
		out.HitRate = integerRange(source.HitRate, varianceAt(varianceBits, 3), plus)
		out.PhysicalAttackMin = integerRange(
			source.PhysicalAttack.Minimum,
			varianceAt(varianceBits, 4),
			plus,
		)
		out.PhysicalAttackMax = integerRange(
			source.PhysicalAttack.Maximum,
			varianceAt(varianceBits, 4),
			plus,
		)
		out.MagicalAttackMin = integerRange(
			source.MagicalAttack.Minimum,
			varianceAt(varianceBits, 5),
			plus,
		)
		out.MagicalAttackMax = integerRange(
			source.MagicalAttack.Maximum,
			varianceAt(varianceBits, 5),
			plus,
		)
		out.CriticalRate = integerRange(
			source.CriticalRate,
			varianceAt(varianceBits, 6),
			0,
		)
		return out, true, nil

	case isBodyProtectorFamily(tid):
		out.BlockRate = integerRange(source.BlockRate, varianceAt(varianceBits, 3), 0)
		out.PhysicalDefense = floatRange(
			source.PhysicalDefense,
			varianceAt(varianceBits, 4),
			plus,
		)
		out.MagicalDefense = floatRange(
			source.MagicalDefense,
			varianceAt(varianceBits, 5),
			plus,
		)

	case isAccessoryFamily(tid):
		out.ParryRate = floatRange(source.ParryRate, varianceAt(varianceBits, 0), plus)
		out.MagicalParry = floatRange(
			source.MagicalParry,
			varianceAt(varianceBits, 1),
			plus,
		)

	case isWearArmorFamily(tid):
		out.PhysicalDefense = floatRange(
			source.PhysicalDefense,
			varianceAt(varianceBits, 3),
			plus,
		)
		out.MagicalDefense = floatRange(
			source.MagicalDefense,
			varianceAt(varianceBits, 4),
			plus,
		)
		out.EvasionRate = integerRange(source.EvasionRate, varianceAt(varianceBits, 5), plus)
	}
	return out, false, nil
}

func varianceAt(bits uint64, statIndex uint) uint8 {
	return uint8((bits >> (statIndex * 5)) & 0x1f)
}

func floatRange(source enterworld.ItemStatRange, variance, plus uint8) float64 {
	value := (source.Max-source.Min)*float64(variance)/31 +
		source.Min +
		source.PerPlus*float64(plus)
	return float64(float32(value))
}

func integerRange(source enterworld.ItemStatRange, variance, plus uint8) float64 {
	minimum := math.Trunc(source.Min)
	maximum := math.Trunc(source.Max)
	plusTerm := math.Trunc(source.PerPlus * float64(plus))
	value := (maximum-minimum)*float64(variance)/31 +
		minimum +
		plusTerm +
		0.5
	return math.Trunc(value)
}

func commonEquipmentGate(tid uint16) bool {
	return tid&0x02 == 0 && tid&0x1c == 0x0c && tid&0x60 == 0x20
}

func isWeaponFamily(tid uint16) bool {
	return commonEquipmentGate(tid) && tid&0x0780 == 0x0300
}

func isBodyProtectorFamily(tid uint16) bool {
	if !commonEquipmentGate(tid) || tid&0x0780 != 0x0200 {
		return false
	}
	subclass := tid >> 11
	return subclass == 1 || subclass == 2
}

func isAccessoryFamily(tid uint16) bool {
	if !commonEquipmentGate(tid) {
		return false
	}
	tid3 := (tid >> 7) & 0x0f
	return tid3 == 5 || tid3 == 12
}

func isWearArmorFamily(tid uint16) bool {
	if !commonEquipmentGate(tid) {
		return false
	}
	switch (tid >> 7) & 0x0f {
	case 1, 2, 3, 9, 10, 11:
		return true
	default:
		return false
	}
}

/*
================
ItemWhiteStats

An equipment row's white stats: its reference's ranges at the row's
variance word and plus, as combat reads them. The operator's Sun-to-Moon
swap prints them before and after.
================
*/
func ItemWhiteStats(ref *enterworld.ItemRef, varianceBits uint64, plus uint8) (Stats, error) {
	stats, _, err := deriveItemStats(ref, varianceBits, plus)
	return stats, err
}
