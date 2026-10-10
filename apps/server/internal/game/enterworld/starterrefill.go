/*
===========================================================================

starterrefill.go - the beta's potion refill on world entry

Not a native rule. Alongside the never-spent starter kit (starterkit.go),
every character on the beta keeps a full stack of HP and MP potions: they
are used up normally, and each world entry tops the stack back up. Natively
potions carry no level requirement; the grade follows the character's level
along the shop towns' bands (inference: herb from level 1, small from 10,
medium from 20, large from 40, extra large from 60), so a refill neither
outclasses nor lags the character.

It rides the same switch as the kit (SRO_BETA_STARTER_KIT).

===========================================================================
*/

package enterworld

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/item/inventory"
)

/*
================
betaRefillFamilies

Each family's grades, weakest first.
================
*/
var betaRefillFamilies = [][]string{
	{"ITEM_ETC_HP_POTION_01", "ITEM_ETC_HP_POTION_02", "ITEM_ETC_HP_POTION_03", "ITEM_ETC_HP_POTION_04", "ITEM_ETC_HP_POTION_05"},
	{"ITEM_ETC_MP_POTION_01", "ITEM_ETC_MP_POTION_02", "ITEM_ETC_MP_POTION_03", "ITEM_ETC_MP_POTION_04", "ITEM_ETC_MP_POTION_05"},
}

// betaRefillGradeLevels are the levels at which each grade takes over.
var betaRefillGradeLevels = []int64{1, 10, 20, 40, 60}

/*
================
StarterRefill

One potion family: its grades as item rows and the stack each refills to.
================
*/
type StarterRefill struct {
	Grades []WireItem
	Stack  []int64
}

/*
================
ResolveStarterRefills

A family with a grade missing from itemdata is a data defect: it is logged
and left out whole rather than handing out a wrong grade.
================
*/
func ResolveStarterRefills(items ItemRefSource) []StarterRefill {
	var out []StarterRefill
	for _, family := range betaRefillFamilies {
		refill := StarterRefill{}
		complete := true
		for _, codename := range family {
			row, ok := items.ItemRefByCodename(codename)
			if !ok || row == nil {
				log.Warnf("bootstrap: starter refill item %s missing from itemdata", codename)
				complete = false
				break
			}
			// The native full stack, even when SRO_STACK_SIZES raised the
			// cap: a refill to the raised cap would hand out that many
			// free potions on every world entry.
			stack := row.NativeStackCap()
			if stack < 1 {
				stack = 1
			}
			refill.Grades = append(refill.Grades, WireItem{
				RefObjID: row.RefObjID, TypeFlags: row.TypeFlags(), Codename: row.Codename,
				Name: row.Name, NativeFields: row.NativeFields, Icon: row.Icon, Kind: "item",
			})
			refill.Stack = append(refill.Stack, stack)
		}
		if complete {
			out = append(out, refill)
		}
	}
	return out
}

/*
================
refillGrade
================
*/
func refillGrade(level int64) int {
	grade := 0
	for i, from := range betaRefillGradeLevels {
		if level >= from {
			grade = i
		}
	}
	return grade
}

/*
================
characterLevelOrOne
================
*/
func characterLevelOrOne(character *Character) int64 {
	if character.Level == nil || *character.Level < 1 {
		return 1
	}
	return *character.Level
}

/*
================
StarterRefillShort

True when some family's level grade is below a full stack.
================
*/
func StarterRefillShort(character *Character, refills []StarterRefill) bool {
	grade := refillGrade(characterLevelOrOne(character))
	for _, refill := range refills {
		if grade >= len(refill.Grades) {
			continue
		}
		if heldCount(character, refill.Grades[grade].RefObjID) < refill.Stack[grade] {
			return true
		}
	}
	return false
}

/*
================
RefillStarterPotions

Tops the level's grade of each family up to one full stack: the first
partial stack is raised, or a new stack goes into the first free bag slot.
Returns how many families were refilled. The caller holds the character's
mutation door.
================
*/
func RefillStarterPotions(character *Character, refills []StarterRefill) int {
	grade := refillGrade(characterLevelOrOne(character))
	refilled := 0
	for _, refill := range refills {
		if grade >= len(refill.Grades) {
			continue
		}
		item, full := refill.Grades[grade], refill.Stack[grade]
		if heldCount(character, item.RefObjID) >= full {
			continue
		}
		rows := append([]InventoryRow(nil), character.MissionInventory...)
		raised := false
		for i, row := range rows {
			if row.RefObjID == item.RefObjID && row.StackCount > 0 && row.StackCount < full && isBagSlot(character, row.Slot) {
				rows[i].StackCount = full
				raised = true
				break
			}
		}
		if !raised {
			slot, ok := firstFreeBagSlot(character)
			if !ok {
				log.Warnf("bootstrap: %s has no free bag slot for starter refill %s", character.Name, item.Codename)
				continue
			}
			rows = append(rows, InventoryRow{
				Slot: slot, RefObjID: item.RefObjID, Codename: item.Codename,
				TypeFlags: item.TypeFlags, VarianceBits: "0", StackCount: full,
			})
		}
		character.MissionInventory = rows
		refilled++
	}
	return refilled
}

/*
================
heldCount

The total of one item across the bag.
================
*/
func heldCount(character *Character, refObjID uint32) int64 {
	var total int64
	for _, row := range character.MissionInventory {
		if row.RefObjID == refObjID && row.StackCount > 0 && isBagSlot(character, row.Slot) {
			total += row.StackCount
		}
	}
	return total
}

/*
================
isBagSlot
================
*/
func isBagSlot(character *Character, slot int64) bool {
	return slot >= int64(inventory.EquipmentSlotEnd) && slot < int64(inventory.BagEnd(character))
}
