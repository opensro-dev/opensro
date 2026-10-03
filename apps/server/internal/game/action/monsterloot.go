/*
===========================================================================

monsterloot.go - monster reward selection and publication order

===========================================================================
*/

package action

import (
	"math"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// v1.188 _RefDropGold GoldMax, levels 1..140. The table is keyed by level,
// so unlike RefObj numeric identities it is safe mechanism/content evidence
// across the server-dump boundary. GoldMin follows the table's exact
// ceil(3.5*level+24.5) progression and is calculated below.
var monsterGoldMaxByLevel = [...]uint16{
	59, 66, 74, 81, 88, 96, 103, 110, 118, 125,
	132, 140, 147, 154, 162, 169, 176, 184, 191, 198,
	206, 213, 221, 228, 235, 243, 250, 257, 265, 272,
	279, 287, 294, 301, 309, 316, 323, 331, 338, 345,
	353, 360, 368, 375, 382, 390, 397, 404, 412, 419,
	426, 434, 441, 448, 456, 463, 470, 478, 485, 492,
	500, 507, 515, 522, 529, 537, 544, 551, 559, 566,
	573, 581, 588, 595, 603, 610, 617, 625, 632, 639,
	647, 654, 662, 669, 676, 684, 691, 698, 706, 713,
	720, 728, 735, 742, 750, 757, 764, 772, 779, 786,
	794, 801, 809, 816, 823, 831, 838, 845, 853, 860,
	867, 875, 882, 889, 897, 904, 911, 919, 926, 933,
	941, 948, 956, 963, 970, 978, 985, 992, 1000, 1007,
	1014, 1022, 1029, 1036, 1044, 1051, 1058, 1066, 1073, 1080,
}

/*
================
monsterGoldRange
================
*/
func monsterGoldRange(level uint8) (uint32, uint32, bool) {
	if level == 0 || int(level) > len(monsterGoldMaxByLevel) {
		return 0, 0, false
	}
	minimum := uint32(7*uint32(level)+50) / 2
	return minimum, uint32(monsterGoldMaxByLevel[level-1]), true
}

// rollMonsterGoldAmount ports the two reference-table draws that construct
// the gold heap. Player/monster-level admission belongs to the later common
// prepared-drop publication boundary, just as it does in the native path.
/*
================
rollMonsterGoldAmount
================
*/
func (rt *Runtime) rollMonsterGoldAmount(monster monster.Instance) (uint32, bool) {
	minimum, maximum, ok := monsterGoldRange(monster.Ref.Level)
	if !ok || rt.DropRoll == nil {
		return 0, false
	}

	// _RefDropGold probability is 0.0001 in the backup and is scaled by the
	// loader to the runtime threshold 100. The draw is still consumed: it is
	// part of the native RNG order even though every 0..100 value succeeds.
	referenceChance, err := rt.DropRoll()
	if err != nil || referenceChance%101 > 100 {
		return 0, false
	}
	amountRoll, err := rt.DropRoll()
	if err != nil {
		return 0, false
	}
	span := maximum - minimum
	amount := minimum + uint32(float32(amountRoll)/float32(32767)*float32(span))

	// CServerData_BuildMonsterGoldHeap @0x726900 rarity multipliers.
	multiplier := uint64(1)
	if monster.Rarity()>>4 == 1 {
		multiplier *= 9
	}
	switch monster.Rarity() & 0x0f {
	case 3, 8:
		multiplier *= 30
	case 4, 7:
		multiplier *= 4
	}
	scaled := uint64(amount) * multiplier
	if scaled == 0 || scaled > math.MaxInt32 {
		return 0, false
	}
	// The beta gold rate is port-only and applies after the native heap is
	// built, so it never changes the RNG order; it clamps instead of refusing.
	if rt.GoldRate > 1 {
		scaled = min(scaled*uint64(rt.GoldRate), math.MaxInt32)
	}
	return uint32(scaled), true
}

// admitMonsterDrop is the publish-side CGObjMob gate. Retail runs it only
// after the complete prepared-drop list has been generated; keeping it out of
// the gold builder is what preserves RNG order and permits more than one row.
/*
================
admitMonsterDrop
================
*/
func (rt *Runtime) admitMonsterDrop(playerLevel uint8, monster monster.Instance) bool {
	if rt.DropRoll == nil {
		return false
	}
	threshold := int32(100)
	gap := int32(playerLevel) - int32(monster.Ref.Level)
	if gap > 6 {
		threshold = int32((1 - float64(gap)*0.04) * 100)
	}
	if threshold < 0 {
		threshold = 0
	}
	if threshold > 100 {
		threshold = 100
	}
	roll, err := rt.DropRoll()
	return err == nil && int32(roll%101) <= threshold
}

/*
================
rollCombinedMillion
================
*/
func (rt *Runtime) rollCombinedMillion() (uint32, bool) {
	if rt.DropRoll == nil {
		return 0, false
	}
	first, err := rt.DropRoll()
	if err != nil {
		return 0, false
	}
	second, err := rt.DropRoll()
	if err != nil {
		return 0, false
	}
	return ((second << 15) | first) % 1_000_000, true
}

// Class probabilities are compiled once from the version-joined native tables.
/*
================
selectEquipmentGroup
================
*/
func (rt *Runtime) selectEquipmentGroup(level uint8, rare bool) (int, bool) {
	roll, ok := rt.rollCombinedMillion()
	if !ok {
		return 0, false
	}
	return loot.EquipmentGroup(level, rare, roll)
}

/*
================
resolveMonsterDropCountry
================
*/
func (rt *Runtime) resolveMonsterDropCountry(country uint8) (uint8, bool) {
	if country != 3 {
		return country, loot.HasEquipmentCountry(country)
	}
	if rt.DropRoll == nil {
		return 0, false
	}
	roll, err := rt.DropRoll()
	if err != nil {
		return 0, false
	}
	return uint8(roll & 1), true
}

var droppedEquipmentPlusCumulative = []float32{
	0.88333303, 0.96666598, 0.99047601, 0.99727899,
	0.99922299, 0.99977797, 0.999937, 0.999982,
	0.99999499, 0.99999899, 1.0,
}

/*
================
rollDroppedEquipmentPlus
================
*/
func (rt *Runtime) rollDroppedEquipmentPlus() (uint8, bool) {
	roll, ok := rt.rollCombinedMillion()
	if !ok {
		return 0, false
	}
	for optLevel, probability := range droppedEquipmentPlusCumulative {
		threshold := uint32(float64(probability) * 1_000_000)
		if roll <= threshold {
			if optLevel >= 7 {
				return 7, true
			}
			return uint8(optLevel), true
		}
	}
	return 0, true
}

/*
================
equipmentVarianceFieldCount
================
*/
func equipmentVarianceFieldCount(ref *enterworld.ItemRef) int {
	if ref == nil {
		return 0
	}
	switch ref.TypeIDs[2] {
	case 6: // weapons
		return 7
	case 5, 12: // CH/EU accessories
		return 2
	case 1, 2, 3, 4, 9, 10, 11: // armor and shields
		return 6
	default:
		return 0
	}
}

/*
================
rollLowBiasedVarianceField
================
*/
func (rt *Runtime) rollLowBiasedVarianceField() (uint8, bool) {
	if rt.DropRoll == nil {
		return 0, false
	}
	minimum := float32(1)
	for i := 0; i < 3; i++ {
		roll, err := rt.DropRoll()
		if err != nil {
			return 0, false
		}
		candidate := float32(roll) / float32(32767)
		if candidate < minimum {
			minimum = candidate
		}
	}
	return uint8(float64(minimum) * 31), true
}

/*
================
rollDroppedEquipmentVariance
================
*/
func (rt *Runtime) rollDroppedEquipmentVariance(ref *enterworld.ItemRef) (uint64, uint32, bool) {
	fieldCount := equipmentVarianceFieldCount(ref)
	var bits uint64
	var first uint8
	for field := 0; field < fieldCount; field++ {
		value, ok := rt.rollLowBiasedVarianceField()
		if !ok {
			return 0, 0, false
		}
		if field == 0 {
			first = value
		}
		bits |= uint64(value&0x1f) << (field * 5)
	}

	minimum := int64(1)
	if ref != nil && ref.VarianceIntMin1c0 != nil {
		minimum = *ref.VarianceIntMin1c0
	}
	if minimum < 1 {
		minimum = 1
	}
	maximum := minimum
	if ref != nil && ref.MaxDurability > maximum {
		maximum = ref.MaxDurability
	}
	delta := maximum - minimum
	interpolated := int64(float32(delta) * (float32(first) / float32(31)))
	durability := minimum + interpolated
	if durability > math.MaxUint32 {
		durability = math.MaxUint32
	}
	return bits, uint32(durability), true
}

/*
================
prepareEquipmentDrop
================
*/
func (rt *Runtime) prepareEquipmentDrop(
	monster monster.Instance,
	at simulation.Spawn,
	droppedBy string,
	now time.Time,
) (grounditem.Item, bool) {
	if rt.DropRoll == nil {
		return grounditem.Item{}, false
	}
	rarityRoll, err := rt.DropRoll()
	if err != nil {
		return grounditem.Item{}, false
	}
	rare := rarityRoll%1000 == 1
	_, _, attempts := loot.MonsterDropBudget(monster.Rarity(), monster.Ref.Codename)
	for attempt := 0; attempt < attempts; attempt++ {
		if item, ok := rt.prepareEquipmentDropKind(monster, at, droppedBy, now, rare); ok {
			return item, true
		}
	}
	return grounditem.Item{}, false
}

/*
================
prepareEquipmentDropKind
================
*/
func (rt *Runtime) prepareEquipmentDropKind(monster monster.Instance, at simulation.Spawn, droppedBy string, now time.Time, rare bool) (grounditem.Item, bool) {
	group, selected := rt.selectEquipmentGroup(monster.Ref.Level, rare)
	if !selected {
		return grounditem.Item{}, false
	}
	dropCountry, ok := rt.resolveMonsterDropCountry(monster.Ref.Country)
	if !ok {
		return grounditem.Item{}, false
	}
	chosen, ok := loot.SelectEquipment(dropCountry, group, rare, monster.Ref.Level, rt.DropRoll)
	if !ok {
		return grounditem.Item{}, false
	}
	plus, ok := rt.rollDroppedEquipmentPlus()
	if !ok {
		return grounditem.Item{}, false
	}
	return rt.prepareSelectedDrop(loot.DropItem{Codename: chosen.Codename, Count: 1, Plus: plus}, at, droppedBy, now)
}

// planMonsterKillLoot resolves and rolls the drop before entering the commit
// door. The caller commits this value together with progression, so a crash
// cannot persist only one half of a kill reward.
/*
================
planMonsterKillLoot
================
*/
func (rt *Runtime) planMonsterKillLoot(
	snapshot *enterworld.Character,
	monster monster.Instance,
	pose monster.Pose,
	nowMs int64,
) []grounditem.Item {
	if snapshot == nil || snapshot.Level == nil {
		return nil
	}
	at := simulation.Spawn{
		RegionID: pose.RegionID,
		X:        pose.X,
		Y:        pose.Y,
		Z:        pose.Z,
		Angle:    pose.Heading,
	}
	now := time.UnixMilli(nowMs)
	capacity, passes, _ := loot.MonsterDropBudget(monster.Rarity(), monster.Ref.Codename)
	if rt.DropPassRate > 1 {
		passes *= rt.DropPassRate
	}
	prepared := rt.prepareUniqueDrops(uniqueDropContext{mob: monster, at: at, owner: snapshot.Name, now: now})
	for _, chosen := range loot.AssignedDrops(monster.Ref.Codename, capacity-len(prepared), rt.DropRoll) {
		if item, ok := rt.prepareSelectedDrop(chosen, at, snapshot.Name, now); ok {
			prepared = append(prepared, item)
		}
	}
	items := rt.deps.ItemReferences()
	for pass := 0; pass < passes && len(prepared) < capacity; pass++ {
		if amount, ok := rt.rollMonsterGoldAmount(monster); ok && items != nil {
			tier := inventory.GoldHeapTier(amount)
			if ref, found := items.ItemRefByCodename(tier); found && ref != nil {
				prepared = append(prepared, PlanGoldDrop(GoldHeapRef{
					RefObjID: ref.RefObjID,
					Codename: ref.Codename,
					Tid1:     uint8(ref.TypeIDs[0]),
					Tid2:     uint8(ref.TypeIDs[1]),
					Tid3:     uint8(ref.TypeIDs[2]),
					Tid4:     uint8(ref.TypeIDs[3]),
				}, amount, at, snapshot.Name, now))
			}
		}
		if len(prepared) >= capacity {
			break
		}
		if equipment, ok := rt.prepareEquipmentDrop(monster, at, snapshot.Name, now); ok {
			prepared = append(prepared, equipment)
		}
		for category := 0; category < 7 && len(prepared) < capacity; category++ {
			family := [...]int{2, 3, 6, 10, 4, 8, 7}[category]
			// Reconstruction: family 7 follows the native categories so its
			// authored speed-tablet table is reachable in v1.150.
			if category == 4 || category == 5 {
				if rt.DropRoll == nil {
					return nil
				}
				choice, err := rt.DropRoll()
				if err != nil {
					return nil
				}
				family += int(choice & 1)
			}
			if item, ok := rt.prepareConsumableDrop(monster, family, at, snapshot.Name, now); ok {
				prepared = append(prepared, item)
			}
		}
	}

	ownerJID := enterworld.ObjectIDForCharacter(snapshot)
	admitted := make([]grounditem.Item, 0, len(prepared))
	for _, planned := range prepared {
		if !rt.admitMonsterDrop(uint8(*snapshot.Level), monster) {
			continue
		}
		planned.OwnerJID = ownerJID
		planned = rt.scatterMonsterDrop(planned, at, monster.Rarity())
		admitted = append(admitted, planned)
	}
	return admitted
}
