/*
===========================================================================

durability.go - an equipment item's maximum durability

The item's +0x194 in v1.188: the itemdata range picked by variance field 0
(the same interpolation a drop rolls its first durability from), then
CGItemEquip_ApplyMagicOptions (496A70) moves it by a percentage: 'nrep'
and 'duru' add theirs, 'dura' subtracts its own, and the result is
base + trunc(sum / 100 * base), at least 1. Repair restores this value.

===========================================================================
*/

package combat

import (
	"fmt"
	"math"
	"strconv"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
DurabilityFromVariance

The itemdata durability range interpolated by a variance field (0..31):
VarianceIntMin (at least 1) to Dur_U.
================
*/
func DurabilityFromVariance(ref *enterworld.ItemRef, field uint8) uint32 {
	minimum := int64(1)
	if ref != nil && ref.VarianceIntMin1c0 != nil {
		minimum = *ref.VarianceIntMin1c0
	}
	minimum = max(minimum, 1)
	maximum := minimum
	if ref != nil && ref.MaxDurability > maximum {
		maximum = ref.MaxDurability
	}
	durability := minimum + int64(float32(maximum-minimum)*(float32(field)/float32(31)))
	return uint32(min(durability, math.MaxUint32))
}

/*
================
EquipmentMaxDurability

The durability a repair restores: the row's variance base moved by its
magic options.
================
*/
func EquipmentMaxDurability(ref *enterworld.ItemRef, row enterworld.InventoryRow, source enterworld.MagicOptionSource) (uint32, error) {
	bits := uint64(0)
	if row.VarianceBits != "" {
		parsed, err := strconv.ParseUint(row.VarianceBits, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("combat: item %q variance %q: %w", row.Codename, row.VarianceBits, err)
		}
		bits = parsed
	}
	base := DurabilityFromVariance(ref, varianceAt(bits, 0))
	options, err := resolveMagicOptions(row.Codename, row.MagicOptions, source)
	if err != nil {
		return 0, err
	}
	sum := int64(0)
	for _, option := range options {
		switch option.tag {
		case optionTag("nrep"), optionTag("duru"):
			sum += int64(option.value)
		case optionTag("dura"):
			sum -= int64(option.value)
		}
	}
	if sum == 0 {
		return base, nil
	}
	// 496BD9: x87 product, truncated by fistp, added to the base word.
	maximum := uint32(int64(float64(sum)/100*float64(base))) + base
	return max(maximum, 1), nil
}

/*
================
RepairableByOptions

CGItemEquip_CanRepair's option half (497340): no 'nrep' option, and a
'rep' (MATTR_REPAIR) count, if present, above 1.
================
*/
func RepairableByOptions(row enterworld.InventoryRow, source enterworld.MagicOptionSource) (bool, error) {
	options, err := resolveMagicOptions(row.Codename, row.MagicOptions, source)
	if err != nil {
		return false, err
	}
	for _, option := range options {
		if option.tag == optionTag("nrep") || option.tag == optionTag("rep") && option.value <= 1 {
			return false, nil
		}
	}
	return true, nil
}

// RepairQuote limits: v1.188 496E60 caps the points one repair restores
// and the gold it charges.
const (
	maxRepairPoints = 100000
	maxRepairCost   = 100000000
)

/*
================
RepairQuote

The points a repair restores and the gold it costs. The price is the
v1.150 client's (CSOItem_CalculateRepairCost 789630, the cost its repair
confirmation shows): CostRepair / maximum per missing point as a float32,
at least 1, truncated over the points, plus CostRevive for a broken item,
which repairs from 1. v1.188 496E60 adds the gold rule: only the points the
player can pay for are restored. ok is false when nothing is missing or
the gold pays for no point.
================
*/
func RepairQuote(current, maximum uint32, costRepair, costRevive, gold int64) (restored uint32, cost int64, ok bool) {
	from, revive := current, int64(0)
	if from == 0 {
		from, revive = 1, max(costRevive, 0)
	}
	missing := int64(maximum) - int64(from)
	if missing <= 0 && current != 0 || maximum == 0 {
		return current, 0, false
	}
	perPoint := float32(float64(uint32(costRepair)) / float64(maximum))
	if !(perPoint >= 1) {
		perPoint = 1
	} else if perPoint > 1e38 {
		perPoint = 1e38
	}
	if gold <= revive {
		return current, 0, false
	}
	affordable := min(int64(float64(gold-revive)/float64(perPoint)), maxRepairPoints)
	points := min(max(missing, 0), affordable)
	if points <= 0 && current != 0 {
		return current, 0, false
	}
	cost = min(int64(float64(points)*float64(perPoint)), maxRepairCost) + revive
	return uint32(int64(from) + points), cost, true
}
