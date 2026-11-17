/*
===========================================================================

portal_conditions.go - teleport link Restrict kinds 4, 5 and 6

The item and job conditions of a teleport link, as the reference server's
link check evaluates them (v1.188; the addresses are in #3765's
native-claims table). The Roc summit gate's link (GATE_RC_MOUNTAIN_GATE ->
GATE_RC_ROC_GATE, #3399) carries all three:

	kind 4  Data_1 names an item the traveller must hold in the bag, or
	        the link refuses 0x1C23;
	kind 5  the same test, and one of that item is spent once every
	        condition has passed (the player's item removal);
	kind 6  the traveller must be outside job mode (the player's job
	        state reads 4), or the link refuses 0x1C24.

portal.go loads the rows and runs portalAdmission; this file owns what
the three kinds read and spend.

===========================================================================
*/
package action

import "opensro.online/server/internal/game/enterworld"

const (
	// portalConditionHeldItem is Restrict kind 4: hold Data_1 in the bag.
	portalConditionHeldItem uint32 = 4
	// portalConditionSpentItem is Restrict kind 5: hold Data_1 and spend one.
	portalConditionSpentItem uint32 = 5
	// portalConditionNoJob is Restrict kind 6: outside job mode.
	portalConditionNoJob uint32 = 6

	// errCodeGateItem is 0x1C23: the condition's item is not in the bag
	// (or, for kind 5, could not be spent).
	errCodeGateItem byte = 0x23
	// errCodeGateJob is 0x1C24: the traveller is in job mode.
	errCodeGateJob byte = 0x24

	// portalBagFirstSlot is where the item search starts: the check asks
	// the inventory for slots 0xD up to its size (storage op +0x38 from
	// 0xD), so the thirteen equipment sockets never satisfy a condition.
	portalBagFirstSlot int64 = 0x0D
)

/*
================
portalItemConditionValid

Whether a kind 4 or 5 row's operands load. The native check skips the
condition when Data_1 is 0 and never reads Data_2; the port refuses such a
row at load instead, so an authoring slip cannot open a gate silently.
================
*/
func portalItemConditionValid(item, unused uint32) bool {
	return item != 0 && unused == 0
}

/*
================
portalNoJobConditionValid

Whether a kind 6 row's operands load. The native check writes a crash
dump and skips the test when either operand is set; the port refuses the
row at load.
================
*/
func portalNoJobConditionValid(a, b uint32) bool {
	return a == 0 && b == 0
}

/*
================
portalBagRow

The index in MissionInventory of the first bag row holding refObjID, in
slot order as the native search walks it, or -1.

Inference, recorded: the native search also skips an item whose 64-bit
serial (+0x20/+0x24) is zero. Every native item carries one, while the
port's RecordID is optional persistence, so the port does not test it.
================
*/
func portalBagRow(c *enterworld.Character, refObjID uint32) int {
	found := -1
	for i, row := range c.MissionInventory {
		if row.Slot < portalBagFirstSlot || row.RefObjID != refObjID || row.StackCount <= 0 {
			continue
		}
		if found < 0 || row.Slot < c.MissionInventory[found].Slot {
			found = i
		}
	}
	return found
}

/*
================
portalItemCondition

The refusal one kind 4, 5 or 6 condition gives the traveller, or 0.
portalAdmission calls it in the link's condition order.
================
*/
func portalItemCondition(c *enterworld.Character, condition portalCondition) byte {
	switch condition.kind {
	case portalConditionHeldItem, portalConditionSpentItem:
		if portalBagRow(c, condition.minimum) < 0 {
			return errCodeGateItem
		}
	case portalConditionNoJob:
		if enterworld.PartyJobClass(c) != enterworld.NoJobClass {
			return errCodeGateJob
		}
	}
	return 0
}

/*
================
portalSpendItems

Spends one of each kind 5 item, in the link's condition order, once the
whole check has passed (the native check collects the slots during its
walk and removes after it). Runs inside the travel transaction, so a
rolled-back move restores the bag with the gold (commitGateTravel). A
row that can no longer be found refuses 0x1C23, as a failed native
removal does.

The client hears of the loss through the travel itself: a gate move
resets the client and re-enters it (OpcodeResetClient), and the
re-entry's character data lists the bag as it now stands.
================
*/
func portalSpendItems(c *enterworld.Character, link portalLink) byte {
	// The spend works on a copy and lands whole: a later condition that
	// fails leaves the bag exactly as it was. The native pass removes one
	// slot at a time, so a failed second removal there keeps the first
	// one spent. That failure cannot follow the check's own find (the
	// slot was just found under the same lock), so the port refuses
	// whole instead of keeping a partial spend.
	spent := &enterworld.Character{MissionInventory: c.MissionInventory}
	for _, condition := range link.conditions {
		if condition.kind != portalConditionSpentItem {
			continue
		}
		index := portalBagRow(spent, condition.minimum)
		if index < 0 {
			return errCodeGateItem
		}
		rows := append([]enterworld.InventoryRow(nil), spent.MissionInventory...)
		if rows[index].StackCount > 1 {
			rows[index].StackCount--
		} else {
			rows = append(rows[:index], rows[index+1:]...)
		}
		spent.MissionInventory = rows
	}
	c.MissionInventory = spent.MissionInventory
	return 0
}
