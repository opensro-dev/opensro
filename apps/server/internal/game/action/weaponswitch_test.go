/*
===========================================================================

weaponswitch_test.go - the weapon switch scroll (port-only, not native)

===========================================================================
*/

package action

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

// The fixture's bag slots.
const (
	switchWeaponSlot = 21
	switchScrollSlot = 25
	switchShieldSlot = 22
)

/*
================
weaponSwitchFixture

A Chinese degree-8 sword in bag slot 21 with its spear twin in the data
(no blade twin), a shield in slot 22, and two scrolls in slot 25. The
flag is on.
================
*/
func weaponSwitchFixture(t *testing.T) (*Runtime, *enterworld.Character, *enterworld.ItemRef) {
	t.Helper()
	c := testCharacter()
	items := testItems()
	weapon := func(id uint32, code string, typeIDs [4]int64) *enterworld.ItemRef {
		return &enterworld.ItemRef{RefObjID: id, Codename: code, Country: 0, TypeIDs: typeIDs,
			ReqQuadTypes: [4]int64{-1, -1, -1, -1}, NativeFields: enterworld.NewNativeFields(map[string]float64{"itemClass": 22})}
	}
	sword := weapon(9101, "ITEM_CH_SWORD_08_A_RARE", [4]int64{3, 1, 6, 2})
	spear := weapon(9102, "ITEM_CH_SPEAR_08_A_RARE", [4]int64{3, 1, 6, 4})
	shield := weapon(9103, "ITEM_CH_SHIELD_08_A_RARE", [4]int64{3, 1, 4, 1})
	scroll := &enterworld.ItemRef{RefObjID: 3790, Codename: commerce.EquipSwitchScrolls[0].Codename, Country: 3,
		TypeIDs: [4]int64{3, 3, 13, 11}, ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "maxStack": 50})}
	for _, ref := range []*enterworld.ItemRef{sword, spear, shield, scroll} {
		items[ref.Codename] = ref
	}
	c.MissionInventory = append(c.MissionInventory,
		enterworld.InventoryRow{Slot: switchWeaponSlot, RefObjID: sword.RefObjID, Codename: sword.Codename,
			TypeFlags: sword.TypeFlags(), StackCount: 1, Durability: 40, VarianceBits: "123"},
		enterworld.InventoryRow{Slot: switchShieldSlot, RefObjID: shield.RefObjID, Codename: shield.Codename,
			TypeFlags: shield.TypeFlags(), StackCount: 1},
		enterworld.InventoryRow{Slot: switchScrollSlot, RefObjID: scroll.RefObjID, Codename: scroll.Codename,
			TypeFlags: scroll.TypeFlags(), StackCount: 2})
	rt, _ := newTestRuntime(c, items)
	rt.EquipSwitch = map[commerce.EquipSwitchKind]bool{commerce.SwitchWeapon: true}
	return rt, c, scroll
}

/*
================
weaponSwitchUse

The use request: the scroll's slot and type, then the target and kind.
================
*/
func weaponSwitchUse(scroll *enterworld.ItemRef, target uint8, kind uint8) []byte {
	return wire.NewWriter(5).U8(switchScrollSlot).U16(scroll.TypeFlags()).U8(target).U8(kind).Payload()
}

/*
================
TestWeaponSwitchScrollTurnsTheSwordIntoASpear
================
*/
func TestWeaponSwitchScrollTurnsTheSwordIntoASpear(t *testing.T) {
	rt, c, scroll := weaponSwitchFixture(t)
	out := rt.HandleItemUse(testDivision, c, weaponSwitchUse(scroll, switchWeaponSlot, 4))
	if len(out.Frames) < 3 || out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("use = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	want := binary.LittleEndian.AppendUint32([]byte{switchWeaponSlot, itemStateRefFlag}, 9102)
	state, ok := findFrame(out.Frames, cosItemStateOpcode)
	if !ok || !bytes.Equal(state.Payload, want) {
		t.Fatalf("state frame %x in %+v", state.Payload, out.Frames)
	}
	assertReferencedBeforeNamed(t, "actor", out.Frames)
	row, _ := inventoryRowAt(c, switchWeaponSlot)
	if row.Codename != "ITEM_CH_SPEAR_08_A_RARE" || row.RefObjID != 9102 || row.Durability != 40 || row.VarianceBits != "123" {
		t.Fatalf("switched row %+v", row)
	}
	if used, _ := inventoryRowAt(c, switchScrollSlot); used.StackCount != 1 {
		t.Fatalf("scroll row %+v, want one spent", used)
	}
}

/*
================
TestWeaponSwitchScrollOffIsNative

With the flag off the scroll is an item no family serves.
================
*/
func TestWeaponSwitchScrollOffIsNative(t *testing.T) {
	rt, c, scroll := weaponSwitchFixture(t)
	rt.EquipSwitch = nil
	assertItemUseRefusedUnchanged(t, rt, c, weaponSwitchUse(scroll, switchWeaponSlot, 4))
}

/*
================
TestWeaponSwitchKeepsPlusAndBlues

An enhanced weapon switches (owner, 2026-11-21): its plus and blue
options ride the new reference unchanged.
================
*/
func TestWeaponSwitchKeepsPlusAndBlues(t *testing.T) {
	rt, c, scroll := weaponSwitchFixture(t)
	blues := []uint64{enterworld.EncodeMagicOption(5, 3)}
	weapon := &c.MissionInventory[len(c.MissionInventory)-3]
	weapon.Plus, weapon.MagicOptions = 7, blues
	out := rt.HandleItemUse(testDivision, c, weaponSwitchUse(scroll, switchWeaponSlot, 4))
	if len(out.Frames) < 3 || out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("use = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	row, _ := inventoryRowAt(c, switchWeaponSlot)
	if row.Codename != "ITEM_CH_SPEAR_08_A_RARE" || row.Plus != 7 || len(row.MagicOptions) != 1 || row.MagicOptions[0] != blues[0] {
		t.Fatalf("switched row %+v, want the spear at +7 with its blue", row)
	}
}

/*
================
TestWeaponSwitchScrollRefusesTheWrongTarget

The shield, an equipped slot and an empty slot are no target.
================
*/
func TestWeaponSwitchScrollRefusesTheWrongTarget(t *testing.T) {
	rt, c, scroll := weaponSwitchFixture(t)
	assertItemUseRefusedUnchanged(t, rt, c, weaponSwitchUse(scroll, switchShieldSlot, 4), weaponSwitchErrTarget)
	assertItemUseRefusedUnchanged(t, rt, c, weaponSwitchUse(scroll, 6, 4), weaponSwitchErrTarget)
	assertItemUseRefusedUnchanged(t, rt, c, weaponSwitchUse(scroll, 30, 4), weaponSwitchErrTarget)
}

/*
================
TestWeaponSwitchScrollRefusesTheWrongKind

The weapon's own kind and the siege weapon are not offered; a kind with
no twin in the data, of either race, finds nothing.
================
*/
func TestWeaponSwitchScrollRefusesTheWrongKind(t *testing.T) {
	rt, c, scroll := weaponSwitchFixture(t)
	assertItemUseRefusedUnchanged(t, rt, c, weaponSwitchUse(scroll, switchWeaponSlot, 2), weaponSwitchErrKind)
	assertItemUseRefusedUnchanged(t, rt, c, weaponSwitchUse(scroll, switchWeaponSlot, 16), weaponSwitchErrKind)
	assertItemUseRefusedUnchanged(t, rt, c, weaponSwitchUse(scroll, switchWeaponSlot, 3), weaponSwitchErrNoTwin)
	assertItemUseRefusedUnchanged(t, rt, c, weaponSwitchUse(scroll, switchWeaponSlot, 8), weaponSwitchErrNoTwin)
}

/*
================
TestWeaponSwitchScrollOnTheShippedData

The shipped scroll and weapons: the donor passes the item use's own
requirements, a European sealed sword finds its dagger twin, and a
Chinese sword its European crossbow twin across the races (and back).
================
*/
func TestWeaponSwitchScrollOnTheShippedData(t *testing.T) {
	shippedWeaponSwitch(t, "ITEM_EU_SWORD_08_A_RARE", 13, "ITEM_EU_DAGGER_08_A_RARE")
	shippedWeaponSwitch(t, "ITEM_CH_SWORD_08_A_RARE", 12, "ITEM_EU_CROSSBOW_08_A_RARE")
	shippedWeaponSwitch(t, "ITEM_EU_STAFF_03_C", 6, "ITEM_CH_BOW_03_C")
}

/*
================
shippedWeaponSwitch

Switches one shipped weapon with the last shipped scroll.
================
*/
func shippedWeaponSwitch(t *testing.T, from string, kind uint8, want string) {
	t.Helper()
	items := enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	scroll, ok := items.ItemRefByCodename(commerce.EquipSwitchScrolls[0].Codename)
	sword, swordOK := items.ItemRefByCodename(from)
	if !ok || !swordOK {
		t.Fatalf("shipped scroll or %s missing", from)
	}
	c := testCharacter()
	c.MissionInventory = append(c.MissionInventory,
		enterworld.InventoryRow{Slot: switchWeaponSlot, RefObjID: sword.RefObjID, Codename: sword.Codename,
			TypeFlags: sword.TypeFlags(), StackCount: 1, Durability: 50},
		enterworld.InventoryRow{Slot: switchScrollSlot, RefObjID: scroll.RefObjID, Codename: scroll.Codename,
			TypeFlags: scroll.TypeFlags(), StackCount: 1})
	rt, _ := newTestRuntime(c, items)
	rt.EquipSwitch = map[commerce.EquipSwitchKind]bool{commerce.SwitchWeapon: true}
	out := rt.HandleItemUse(testDivision, c, weaponSwitchUse(scroll, switchWeaponSlot, kind))
	if len(out.Frames) < 3 || out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("use = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	if row, _ := inventoryRowAt(c, switchWeaponSlot); row.Codename != want {
		t.Fatalf("switched row %+v, want %s", row, want)
	}
	if _, left := inventoryRowAt(c, switchScrollSlot); left {
		t.Fatal("the last scroll was not spent")
	}
}
