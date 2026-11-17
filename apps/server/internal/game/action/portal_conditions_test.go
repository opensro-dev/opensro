/*
===========================================================================

portal_conditions_test.go - teleport link Restrict kinds 4, 5 and 6

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

// The Roc summit link's quest item, as a test identity.
const testGateItem uint32 = 24164

/*
================
gateTraveller

A living, jobless level 90 character with the given bag.
================
*/
func gateTraveller(rows ...domain.InventoryRow) *enterworld.Character {
	hp, level := int64(100), int64(90)
	return &enterworld.Character{CurrentHP: &hp, Level: &level, MissionInventory: rows}
}

/*
================
TestPortalItemConditionsNeedTheBagItem

Kinds 4 and 5 admit only a traveller holding the item in the bag
(0x1C23 otherwise); the same item worn in an equipment socket does not
count, since the native search starts at slot 0xD.
================
*/
func TestPortalItemConditionsNeedTheBagItem(t *testing.T) {
	for _, kind := range []uint32{portalConditionHeldItem, portalConditionSpentItem} {
		link := portalLink{conditions: []portalCondition{{kind: kind, minimum: testGateItem}}}
		if got := portalAdmission(gateTraveller(), portalDestination{}, link, portalTraveller{}); got != errCodeGateItem {
			t.Fatalf("kind %d without the item: %#x", kind, got)
		}
		worn := gateTraveller(domain.InventoryRow{Slot: 4, RefObjID: testGateItem, StackCount: 1})
		if got := portalAdmission(worn, portalDestination{}, link, portalTraveller{}); got != errCodeGateItem {
			t.Fatalf("kind %d with the item in a socket: %#x", kind, got)
		}
		held := gateTraveller(domain.InventoryRow{Slot: 20, RefObjID: testGateItem, StackCount: 1})
		if got := portalAdmission(held, portalDestination{}, link, portalTraveller{}); got != 0 {
			t.Fatalf("kind %d with the item: %#x", kind, got)
		}
	}
}

/*
================
TestPortalNoJobCondition

Kind 6 refuses 0x1C24 while a job suit is worn, and admits a traveller
outside job mode.
================
*/
func TestPortalNoJobCondition(t *testing.T) {
	link := portalLink{conditions: []portalCondition{{kind: portalConditionNoJob}}}
	if got := portalAdmission(gateTraveller(), portalDestination{}, link, portalTraveller{}); got != 0 {
		t.Fatalf("jobless traveller refused: %#x", got)
	}
	// A hunter suit (3/1/7, type 4 = 3) in the job socket.
	suited := gateTraveller(domain.InventoryRow{Slot: enterworld.JobSuitSlot, TypeFlags: 0x3ac | 3<<11})
	if got := portalAdmission(suited, portalDestination{}, link, portalTraveller{}); got != errCodeGateJob {
		t.Fatalf("job mode admitted: %#x", got)
	}
}

/*
================
TestPortalSpendItems

Only kind 5 spends: one from a stack, the whole row from a single item,
and a kind 4 item stays. When a second kind 5 item is missing nothing is
spent at all.
================
*/
func TestPortalSpendItems(t *testing.T) {
	const other uint32 = 9000
	c := gateTraveller(
		domain.InventoryRow{Slot: 30, RefObjID: testGateItem, StackCount: 3},
		domain.InventoryRow{Slot: 14, RefObjID: other, StackCount: 1},
	)
	held := portalLink{conditions: []portalCondition{{kind: portalConditionHeldItem, minimum: testGateItem}}}
	if got := portalSpendItems(c, held); got != 0 || c.MissionInventory[0].StackCount != 3 {
		t.Fatalf("kind 4 spent: %#x %+v", got, c.MissionInventory)
	}
	spent := portalLink{conditions: []portalCondition{{kind: portalConditionSpentItem, minimum: testGateItem}, {kind: portalConditionSpentItem, minimum: other}}}
	if got := portalSpendItems(c, spent); got != 0 {
		t.Fatalf("spend refused: %#x", got)
	}
	if len(c.MissionInventory) != 1 || c.MissionInventory[0].RefObjID != testGateItem || c.MissionInventory[0].StackCount != 2 {
		t.Fatalf("after spend: %+v", c.MissionInventory)
	}
	before := c.MissionInventory
	if got := portalSpendItems(c, spent); got != errCodeGateItem {
		t.Fatalf("missing second item: %#x", got)
	}
	if len(c.MissionInventory) != 1 || c.MissionInventory[0].StackCount != 2 || &c.MissionInventory[0] != &before[0] {
		t.Fatalf("refused spend changed the bag: %+v", c.MissionInventory)
	}
}
