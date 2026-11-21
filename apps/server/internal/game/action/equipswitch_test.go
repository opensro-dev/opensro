/*
===========================================================================

equipswitch_test.go - the equipment switch scrolls' flags (port-only)

===========================================================================
*/

package action

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestEquipSwitchOffIsNative

The armor scroll with its flag off is an item no family serves, even
while the weapon scroll's flag is on.
================
*/
func TestEquipSwitchOffIsNative(t *testing.T) {
	c := testCharacter()
	items := testItems()
	scroll := &enterworld.ItemRef{RefObjID: 3791, Codename: "ITEM_MALL_ARMOR_SWITCH_SCROLL", Country: 3,
		TypeIDs: [4]int64{3, 3, 13, 11}, ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "maxStack": 50})}
	items[scroll.Codename] = scroll
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: switchScrollSlot,
		RefObjID: scroll.RefObjID, Codename: scroll.Codename, TypeFlags: scroll.TypeFlags(), StackCount: 1})
	rt, _ := newTestRuntime(c, items)
	rt.EquipSwitch = map[commerce.EquipSwitchKind]bool{commerce.SwitchWeapon: true}
	out := rt.HandleItemUse(testDivision, c,
		wire.NewWriter(4).U8(switchScrollSlot).U16(scroll.TypeFlags()).U8(switchWeaponSlot).Payload())
	if !strings.Contains(out.DiagnosticRefusal, "SRO_ARMOR_SWITCH_SCROLL") {
		t.Fatalf("refusal %q, want the armor flag named", out.DiagnosticRefusal)
	}
	if row, _ := inventoryRowAt(c, switchScrollSlot); row.StackCount != 1 {
		t.Fatalf("scroll row %+v, want it kept", row)
	}
}

/*
================
TestEquipSwitchDonorIsNoScroll

The shipped reinforce scroll the weapon switch first borrowed is its own
item again: with every flag on it is no switch scroll.
================
*/
func TestEquipSwitchDonorIsNoScroll(t *testing.T) {
	donor := &enterworld.ItemRef{RefObjID: 3790, Codename: "ITEM_MALL_REINFORCE_WEAPON_INC_2_SCROLL",
		TypeIDs: [4]int64{3, 3, 13, 11}}
	if family := admittedItemUseFamily(donor); family == itemUseEquipSwitch {
		t.Fatal("the shipped reinforce scroll is admitted as a switch scroll")
	}
}

// switchScrollDonors names the shipped row each switch scroll's item
// copies (scripts/switch_scroll_import.py), for the shipped-data tests.
var switchScrollDonors = map[commerce.EquipSwitchKind]string{
	commerce.SwitchWeapon:    "ITEM_MALL_REINFORCE_WEAPON_INC_2_SCROLL",
	commerce.SwitchArmor:     "ITEM_MALL_REINFORCE_ARMOR_INC_1_SCROLL",
	commerce.SwitchAccessory: "ITEM_MALL_REINFORCE_ACCESSARY_INC_1_SCROLL",
}

/*
================
switchScrollRefs

The shipped item tables plus one switch scroll's item, built as the
variant import builds it: its donor's row under the scroll's codename
and a fresh identity. The native test data carries no variant rows.
================
*/
type switchScrollRefs struct {
	enterworld.ItemRefSource
	scroll *enterworld.ItemRef
}

/*
================
switchScrollRefs.ItemRefByCodename
================
*/
func (s switchScrollRefs) ItemRefByCodename(codename string) (*enterworld.ItemRef, bool) {
	if codename == s.scroll.Codename {
		return s.scroll, true
	}
	return s.ItemRefSource.ItemRefByCodename(codename)
}

/*
================
switchScrollRefs.ItemRefByID
================
*/
func (s switchScrollRefs) ItemRefByID(id uint32) (*enterworld.ItemRef, bool) {
	if id == s.scroll.RefObjID {
		return s.scroll, true
	}
	return s.ItemRefSource.ItemRefByID(id)
}

/*
================
shippedSwitchScroll
================
*/
func shippedSwitchScroll(t *testing.T, kind commerce.EquipSwitchKind) (switchScrollRefs, *enterworld.ItemRef) {
	t.Helper()
	items := enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	donor, ok := items.ItemRefByCodename(switchScrollDonors[kind])
	if !ok {
		t.Fatalf("shipped donor %s missing", switchScrollDonors[kind])
	}
	scroll := *donor
	for _, row := range commerce.EquipSwitchScrolls {
		if row.Kind == kind {
			scroll.Codename = row.Codename
		}
	}
	scroll.RefObjID = 290001
	return switchScrollRefs{ItemRefSource: items, scroll: &scroll}, &scroll
}
