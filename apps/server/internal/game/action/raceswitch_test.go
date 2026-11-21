/*
===========================================================================

raceswitch_test.go - the armor and accessory switch scrolls (port-only)

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
)

// The race switch fixture's bag slots.
const (
	raceArmorSlot  = 21
	raceShieldSlot = 22
	raceSwordSlot  = 23
	raceHeavySlot  = 24
	raceRingSlot   = 26
	raceScrollSlot = 25
)

/*
================
raceSwitchFixture

A Chinese degree-8 light armor piece (with its European protector twin),
a European shield (with its Chinese twin), a Chinese sword, a Chinese
heavy piece with no European twin and a Chinese ring (with its European
twin) in the bag, and two scrolls of the given kind in slot 25. Both race
switch flags are on.
================
*/
func raceSwitchFixture(t *testing.T, kind commerce.EquipSwitchKind) (*Runtime, *enterworld.Character, *enterworld.ItemRef) {
	t.Helper()
	c := testCharacter()
	items := testItems()
	gear := func(id uint32, code string, country int64, typeIDs [4]int64) *enterworld.ItemRef {
		return &enterworld.ItemRef{RefObjID: id, Codename: code, Country: country, TypeIDs: typeIDs,
			ReqQuadTypes: [4]int64{-1, -1, -1, -1}, NativeFields: enterworld.NewNativeFields(map[string]float64{"itemClass": 22})}
	}
	light := gear(9201, "ITEM_CH_W_LIGHT_08_BA_A", 0, [4]int64{3, 1, 2, 3})
	protector := gear(9202, "ITEM_EU_W_LIGHT_08_BA_A", 1, [4]int64{3, 1, 10, 3})
	shield := gear(9203, "ITEM_EU_SHIELD_08_A", 1, [4]int64{3, 1, 4, 2})
	chShield := gear(9204, "ITEM_CH_SHIELD_08_A", 0, [4]int64{3, 1, 4, 1})
	sword := gear(9205, "ITEM_CH_SWORD_08_A", 0, [4]int64{3, 1, 6, 2})
	heavy := gear(9206, "ITEM_CH_M_HEAVY_01_SA_A", 0, [4]int64{3, 1, 3, 1})
	ring := gear(9207, "ITEM_CH_RING_08_B", 0, [4]int64{3, 1, 5, 3})
	euRing := gear(9208, "ITEM_EU_RING_08_B", 1, [4]int64{3, 1, 12, 3})
	var scrollRow commerce.EquipSwitchScroll
	for _, row := range commerce.EquipSwitchScrolls {
		if row.Kind == kind {
			scrollRow = row
		}
	}
	scroll := &enterworld.ItemRef{RefObjID: 3791 + uint32(kind-commerce.SwitchArmor)*2, Codename: scrollRow.Codename,
		Country: 3, TypeIDs: [4]int64{3, 3, 13, 11}, ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "maxStack": 50})}
	for _, ref := range []*enterworld.ItemRef{light, protector, shield, chShield, sword, heavy, ring, euRing, scroll} {
		items[ref.Codename] = ref
	}
	row := func(slot int64, ref *enterworld.ItemRef, count int64) enterworld.InventoryRow {
		return enterworld.InventoryRow{Slot: slot, RefObjID: ref.RefObjID, Codename: ref.Codename,
			TypeFlags: ref.TypeFlags(), StackCount: count, Durability: 30}
	}
	c.MissionInventory = append(c.MissionInventory, row(raceArmorSlot, light, 1), row(raceShieldSlot, shield, 1),
		row(raceSwordSlot, sword, 1), row(raceHeavySlot, heavy, 1), row(raceRingSlot, ring, 1),
		row(raceScrollSlot, scroll, 2))
	rt, _ := newTestRuntime(c, items)
	rt.EquipSwitch = map[commerce.EquipSwitchKind]bool{commerce.SwitchArmor: true, commerce.SwitchAccessory: true}
	return rt, c, scroll
}

/*
================
raceSwitchUse

The use request: the scroll's slot and type, then the target slot.
================
*/
func raceSwitchUse(scroll *enterworld.ItemRef, target uint8) []byte {
	return wire.NewWriter(4).U8(raceScrollSlot).U16(scroll.TypeFlags()).U8(target).Payload()
}

/*
================
rowAtSlot

The bag row at a slot, by pointer, for a test to enhance it.
================
*/
func rowAtSlot(t *testing.T, c *enterworld.Character, slot int64) *enterworld.InventoryRow {
	t.Helper()
	for i := range c.MissionInventory {
		if c.MissionInventory[i].Slot == slot {
			return &c.MissionInventory[i]
		}
	}
	t.Fatalf("no row at slot %d", slot)
	return nil
}

/*
================
assertRaceSwitched

A successful use: the state frame names the twin's reference and the
row is the twin.
================
*/
func assertRaceSwitched(t *testing.T, out OpResult, c *enterworld.Character, slot uint8, refID uint32, code string) enterworld.InventoryRow {
	t.Helper()
	if len(out.Frames) < 3 || out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("use = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	want := binary.LittleEndian.AppendUint32([]byte{slot, itemStateRefFlag}, refID)
	if state, ok := findFrame(out.Frames, cosItemStateOpcode); !ok || !bytes.Equal(state.Payload, want) {
		t.Fatalf("state frame %x in %+v", state.Payload, out.Frames)
	}
	row, _ := inventoryRowAt(c, slot)
	if row.Codename != code || row.RefObjID != refID {
		t.Fatalf("switched row %+v, want %s", row, code)
	}
	return row
}

/*
================
TestRaceSwitchArmorKeepsPartAndSex

A Chinese light piece becomes the European protector of the same piece
and sex, its plus, blues and durability kept, one scroll spent.
================
*/
func TestRaceSwitchArmorKeepsPartAndSex(t *testing.T) {
	rt, c, scroll := raceSwitchFixture(t, commerce.SwitchArmor)
	blues := []uint64{enterworld.EncodeMagicOption(5, 3)}
	armor := rowAtSlot(t, c, raceArmorSlot)
	armor.Plus, armor.MagicOptions = 5, blues
	out := rt.HandleItemUse(testDivision, c, raceSwitchUse(scroll, raceArmorSlot))
	row := assertRaceSwitched(t, out, c, raceArmorSlot, 9202, "ITEM_EU_W_LIGHT_08_BA_A")
	if row.Plus != 5 || len(row.MagicOptions) != 1 || row.MagicOptions[0] != blues[0] || row.Durability != 30 {
		t.Fatalf("switched row %+v lost its plus, blues or durability", row)
	}
	assertReferencedBeforeNamed(t, "actor", out.Frames)
	if used, _ := inventoryRowAt(c, raceScrollSlot); used.StackCount != 1 {
		t.Fatalf("scroll row %+v, want one spent", used)
	}
}

/*
================
TestRaceSwitchShield

The armor scroll takes shields too (owner, 2026-11-21).
================
*/
func TestRaceSwitchShield(t *testing.T) {
	rt, c, scroll := raceSwitchFixture(t, commerce.SwitchArmor)
	out := rt.HandleItemUse(testDivision, c, raceSwitchUse(scroll, raceShieldSlot))
	assertRaceSwitched(t, out, c, raceShieldSlot, 9204, "ITEM_CH_SHIELD_08_A")
}

/*
================
TestRaceSwitchArmorRefusesWeapon
================
*/
func TestRaceSwitchArmorRefusesWeapon(t *testing.T) {
	rt, c, scroll := raceSwitchFixture(t, commerce.SwitchArmor)
	assertItemUseRefusedUnchanged(t, rt, c, raceSwitchUse(scroll, raceSwordSlot), weaponSwitchErrTarget)
}

/*
================
TestRaceSwitchArmorNoTwin

A piece the other race does not make keeps the item and the scroll.
================
*/
func TestRaceSwitchArmorNoTwin(t *testing.T) {
	rt, c, scroll := raceSwitchFixture(t, commerce.SwitchArmor)
	assertItemUseRefusedUnchanged(t, rt, c, raceSwitchUse(scroll, raceHeavySlot), weaponSwitchErrNoTwin)
}

/*
================
TestRaceSwitchEquippedRefused

An equipped slot or an empty bag slot is no target.
================
*/
func TestRaceSwitchEquippedRefused(t *testing.T) {
	rt, c, scroll := raceSwitchFixture(t, commerce.SwitchArmor)
	assertItemUseRefusedUnchanged(t, rt, c, raceSwitchUse(scroll, 1), weaponSwitchErrTarget)
	assertItemUseRefusedUnchanged(t, rt, c, raceSwitchUse(scroll, 40), weaponSwitchErrTarget)
}

/*
================
TestRaceSwitchLastScroll

Spending the last scroll rebuilds the bag: the target is changed while
its index still names it, and the scroll's row is gone.
================
*/
func TestRaceSwitchLastScroll(t *testing.T) {
	rt, c, scroll := raceSwitchFixture(t, commerce.SwitchArmor)
	rowAtSlot(t, c, raceScrollSlot).StackCount = 1
	out := rt.HandleItemUse(testDivision, c, raceSwitchUse(scroll, raceArmorSlot))
	assertRaceSwitched(t, out, c, raceArmorSlot, 9202, "ITEM_EU_W_LIGHT_08_BA_A")
	if _, left := inventoryRowAt(c, raceScrollSlot); left {
		t.Fatal("the last scroll was not spent")
	}
}

/*
================
TestRaceSwitchOnTheShippedData

The shipped donor and gear: a Chinese robe becomes the European
garment and a European shield the Chinese one.
================
*/
func TestRaceSwitchOnTheShippedData(t *testing.T) {
	shippedRaceSwitch(t, commerce.SwitchArmor, "ITEM_CH_M_CLOTHES_08_BA_A", "ITEM_EU_M_CLOTHES_08_BA_A")
	shippedRaceSwitch(t, commerce.SwitchArmor, "ITEM_EU_SHIELD_08_A", "ITEM_CH_SHIELD_08_A")
}

/*
================
shippedRaceSwitch

Switches one shipped item with the last shipped scroll of a kind.
================
*/
func shippedRaceSwitch(t *testing.T, kind commerce.EquipSwitchKind, from, want string) {
	t.Helper()
	items, scroll := shippedSwitchScroll(t, kind)
	item, itemOK := items.ItemRefByCodename(from)
	if !itemOK {
		t.Fatalf("shipped scroll or %s missing", from)
	}
	c := testCharacter()
	c.MissionInventory = append(c.MissionInventory,
		enterworld.InventoryRow{Slot: raceArmorSlot, RefObjID: item.RefObjID, Codename: item.Codename,
			TypeFlags: item.TypeFlags(), StackCount: 1, Durability: 50},
		enterworld.InventoryRow{Slot: raceScrollSlot, RefObjID: scroll.RefObjID, Codename: scroll.Codename,
			TypeFlags: scroll.TypeFlags(), StackCount: 1})
	rt, _ := newTestRuntime(c, items)
	rt.EquipSwitch = map[commerce.EquipSwitchKind]bool{kind: true}
	out := rt.HandleItemUse(testDivision, c, raceSwitchUse(scroll, raceArmorSlot))
	if len(out.Frames) < 3 || out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("use = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	if row, _ := inventoryRowAt(c, raceArmorSlot); row.Codename != want {
		t.Fatalf("switched row %+v, want %s", row, want)
	}
}

/*
================
TestRaceSwitchAccessory

A Chinese ring becomes the European ring of the same grade, its blues
kept.
================
*/
func TestRaceSwitchAccessory(t *testing.T) {
	rt, c, scroll := raceSwitchFixture(t, commerce.SwitchAccessory)
	blues := []uint64{enterworld.EncodeMagicOption(5, 3)}
	rowAtSlot(t, c, raceRingSlot).MagicOptions = blues
	out := rt.HandleItemUse(testDivision, c, raceSwitchUse(scroll, raceRingSlot))
	row := assertRaceSwitched(t, out, c, raceRingSlot, 9208, "ITEM_EU_RING_08_B")
	if len(row.MagicOptions) != 1 || row.MagicOptions[0] != blues[0] {
		t.Fatalf("switched ring %+v lost its blue", row)
	}
}

/*
================
TestRaceSwitchAccessoryRefusesArmor
================
*/
func TestRaceSwitchAccessoryRefusesArmor(t *testing.T) {
	rt, c, scroll := raceSwitchFixture(t, commerce.SwitchAccessory)
	assertItemUseRefusedUnchanged(t, rt, c, raceSwitchUse(scroll, raceArmorSlot), weaponSwitchErrTarget)
	assertItemUseRefusedUnchanged(t, rt, c, raceSwitchUse(scroll, raceShieldSlot), weaponSwitchErrTarget)
}

/*
================
TestRaceSwitchArmorRefusesAccessory
================
*/
func TestRaceSwitchArmorRefusesAccessory(t *testing.T) {
	rt, c, scroll := raceSwitchFixture(t, commerce.SwitchArmor)
	assertItemUseRefusedUnchanged(t, rt, c, raceSwitchUse(scroll, raceRingSlot), weaponSwitchErrTarget)
}

/*
================
TestRaceSwitchAccessoryOnTheShippedData
================
*/
func TestRaceSwitchAccessoryOnTheShippedData(t *testing.T) {
	shippedRaceSwitch(t, commerce.SwitchAccessory, "ITEM_CH_NECKLACE_08_A", "ITEM_EU_NECKLACE_08_A")
}

/*
================
TestRaceSwitchClampsDurability

The twin's maximum durability can be lower than the item's current
one: the switched item never holds more than its twin can, so a repair
still serves it.
================
*/
func TestRaceSwitchClampsDurability(t *testing.T) {
	rt, c, scroll := raceSwitchFixture(t, commerce.SwitchArmor)
	refs := rt.deps.ItemReferences()
	protector, _ := refs.ItemRefByCodename("ITEM_EU_W_LIGHT_08_BA_A")
	low := int64(20)
	protector.MaxDurability, protector.VarianceIntMin1c0 = low, &low
	out := rt.HandleItemUse(testDivision, c, raceSwitchUse(scroll, raceArmorSlot))
	row := assertRaceSwitched(t, out, c, raceArmorSlot, 9202, "ITEM_EU_W_LIGHT_08_BA_A")
	if row.Durability != 20 {
		t.Fatalf("switched durability %d, want the twin's maximum 20", row.Durability)
	}
}
