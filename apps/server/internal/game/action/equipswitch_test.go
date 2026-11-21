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
	scroll := &enterworld.ItemRef{RefObjID: 3791, Codename: "ITEM_MALL_REINFORCE_ARMOR_INC_1_SCROLL", Country: 3,
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
