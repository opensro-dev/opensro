/*
===========================================================================

modelswitcher_test.go - the Item Mall weapon model switchers (#3257)

Port-only, not native: with SRO_MALL_CATEGORIES on, a switcher dropped
on a degree-9 bag weapon of its race and type swaps it to its _HWSET
twin and back; everything else is refused unchanged, and off the
switcher is dead stock.

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

const (
	switcherTargetSlot = 21
	switcherScrollSlot = 25
)

/*
================
switcherFixture

A +5 Chinese degree-9 sword in bag slot 21 with its twin in the data,
an unrelated degree-8 sword in slot 22, and two sword switchers in
slot 25.
================
*/
func switcherFixture(t *testing.T, switcher string) (*Runtime, *enterworld.Character, *enterworld.ItemRef) {
	t.Helper()
	c := testCharacter()
	items := testItems()
	sword := func(id uint32, code string) *enterworld.ItemRef {
		return &enterworld.ItemRef{RefObjID: id, Codename: code, Country: 0, TypeIDs: [4]int64{3, 1, 6, 2},
			ReqQuadTypes: [4]int64{-1, -1, -1, -1}, NativeFields: enterworld.NewNativeFields(map[string]float64{"itemClass": 25})}
	}
	plain, twin := sword(9101, "ITEM_CH_SWORD_09_A"), sword(9102, "ITEM_CH_SWORD_09_A_HWSET")
	lower := sword(9103, "ITEM_CH_SWORD_08_A")
	scroll := &enterworld.ItemRef{RefObjID: 88018, Codename: switcher, Country: 3,
		TypeIDs: [4]int64{3, 3, 13, 8}, ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "maxStack": 10})}
	for _, ref := range []*enterworld.ItemRef{plain, twin, lower, scroll} {
		items[ref.Codename] = ref
	}
	c.MissionInventory = append(c.MissionInventory,
		enterworld.InventoryRow{Slot: switcherTargetSlot, RefObjID: plain.RefObjID, Codename: plain.Codename,
			TypeFlags: plain.TypeFlags(), StackCount: 1, Plus: 5, Durability: 77, MagicOptions: []uint64{0x0000000a00000123}},
		enterworld.InventoryRow{Slot: 22, RefObjID: lower.RefObjID, Codename: lower.Codename, TypeFlags: lower.TypeFlags(), StackCount: 1},
		enterworld.InventoryRow{Slot: switcherScrollSlot, RefObjID: scroll.RefObjID, Codename: scroll.Codename, TypeFlags: scroll.TypeFlags(), StackCount: 2})
	rt, _ := newTestRuntime(c, items)
	return rt, c, scroll
}

// switcherUse is the 0x75BD request dropping the scroll on target.
func switcherUse(scroll *enterworld.ItemRef, target uint8) []byte {
	return wire.NewWriter(4).U8(switcherScrollSlot).U16(scroll.TypeFlags()).U8(target).Payload()
}

// slotRow is the row standing in slot, or the zero row.
func slotRow(c *enterworld.Character, slot int64) enterworld.InventoryRow {
	for _, row := range c.MissionInventory {
		if row.Slot == slot {
			return row
		}
	}
	return enterworld.InventoryRow{}
}

/*
================
TestModelSwitcherTogglesTheWeaponToItsTwin

On: the first use swaps the sword to its twin, keeping plus, options and
durability, spends one scroll, and names the new reference only after
its reference row; the second use swaps it back.
================
*/
func TestModelSwitcherTogglesTheWeaponToItsTwin(t *testing.T) {
	t.Setenv(commerce.EnvMallCategories, "on")
	rt, c, scroll := switcherFixture(t, "ITEM_ORIGIN_CH_HW_SWORD_SCROLL")
	for _, want := range []struct {
		codename string
		ref      uint32
		left     int64
	}{{"ITEM_CH_SWORD_09_A_HWSET", 9102, 1}, {"ITEM_CH_SWORD_09_A", 9101, 0}} {
		out := rt.HandleItemUse(testDivision, c, switcherUse(scroll, switcherTargetSlot))
		if len(out.Frames) < 3 || out.Frames[0].Payload[0] != wire.ResultSuccess {
			t.Fatalf("use = %+v / %q", out.Frames, out.DiagnosticRefusal)
		}
		state, ok := findFrame(out.Frames, cosItemStateOpcode)
		if !ok || !bytes.Equal(state.Payload, binary.LittleEndian.AppendUint32([]byte{switcherTargetSlot, itemStateRefFlag}, want.ref)) {
			t.Fatalf("state frame %x in %+v", state.Payload, out.Frames)
		}
		assertReferencedBeforeNamed(t, "owner", out.Frames)
		row := slotRow(c, switcherTargetSlot)
		if row.Codename != want.codename || row.RefObjID != want.ref || row.Plus != 5 || row.Durability != 77 ||
			len(row.MagicOptions) != 1 {
			t.Fatalf("swapped row %+v, want %s", row, want.codename)
		}
		if got := slotRow(c, switcherScrollSlot).StackCount; got != want.left {
			t.Fatalf("scrolls left %d, want %d", got, want.left)
		}
	}
}

/*
================
TestModelSwitcherRefusesOtherTargets

A worn or empty slot is 0x82; another type's switcher, another degree
and a non-weapon are 0x84. Nothing changes and no scroll is spent.
================
*/
func TestModelSwitcherRefusesOtherTargets(t *testing.T) {
	t.Setenv(commerce.EnvMallCategories, "on")
	rt, c, scroll := switcherFixture(t, "ITEM_ORIGIN_CH_HW_SWORD_SCROLL")
	assertItemUseRefusedUnchanged(t, rt, c, switcherUse(scroll, 6), genderErrSlot)
	assertItemUseRefusedUnchanged(t, rt, c, switcherUse(scroll, 30), genderErrSlot)
	assertItemUseRefusedUnchanged(t, rt, c, switcherUse(scroll, 22), genderErrTarget)
	assertItemUseRefusedUnchanged(t, rt, c, switcherUse(scroll, switcherScrollSlot), genderErrTarget)

	rt, c, scroll = switcherFixture(t, "ITEM_ORIGIN_CH_HW_BLADE_SCROLL")
	assertItemUseRefusedUnchanged(t, rt, c, switcherUse(scroll, switcherTargetSlot), genderErrTarget)
	rt, c, scroll = switcherFixture(t, "ITEM_ORIGIN_EU_HW_SWORD_SCROLL")
	assertItemUseRefusedUnchanged(t, rt, c, switcherUse(scroll, switcherTargetSlot), genderErrTarget)
}

/*
================
TestModelSwitcherIsDeadStockWhileOff

Off (the native state) the switcher does nothing: the use answers the
generic refusal any unserved item gets, with no swap and no spend.
================
*/
func TestModelSwitcherIsDeadStockWhileOff(t *testing.T) {
	t.Setenv(commerce.EnvMallCategories, "off")
	rt, c, scroll := switcherFixture(t, "ITEM_ORIGIN_CH_HW_SWORD_SCROLL")
	out := rt.HandleItemUse(testDivision, c, switcherUse(scroll, switcherTargetSlot))
	if _, swapped := findFrame(out.Frames, cosItemStateOpcode); out.DiagnosticRefusal == "" || swapped ||
		len(out.Frames) == 0 || out.Frames[0].Payload[0] == wire.ResultSuccess {
		t.Fatalf("off use = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	if slotRow(c, switcherTargetSlot).Codename != "ITEM_CH_SWORD_09_A" || slotRow(c, switcherScrollSlot).StackCount != 2 {
		t.Fatalf("off use changed the bag: %+v", c.MissionInventory)
	}
}

/*
================
TestModelSwitcherTwin

The codename rule alone: race and type must match, only degree 9 has
twins, rare rows and shields are covered, and the twin maps back.
================
*/
func TestModelSwitcherTwin(t *testing.T) {
	for _, tc := range []struct{ switcher, target, want string }{
		{"ITEM_ORIGIN_CH_HW_SWORD_SCROLL", "ITEM_CH_SWORD_09_B_RARE", "ITEM_CH_SWORD_09_B_RARE_HWSET"},
		{"ITEM_ORIGIN_CH_HW_SWORD_SCROLL", "ITEM_CH_SWORD_09_B_RARE_HWSET", "ITEM_CH_SWORD_09_B_RARE"},
		{"ITEM_ORIGIN_EU_HW_SHIELD_SCROLL", "ITEM_EU_SHIELD_09_C", "ITEM_EU_SHIELD_09_C_HWSET"},
		{"ITEM_ORIGIN_EU_HW_TSTAFF_SCROLL", "ITEM_EU_STAFF_09_A", ""},
		{"ITEM_ORIGIN_CH_HW_SWORD_SCROLL", "ITEM_CH_SWORD_10_A", ""},
		{"ITEM_ORIGIN_CH_HW_SWORD_SCROLL", "ITEM_EU_SWORD_09_A", ""},
		{"ITEM_MALL_EQUIP_TRANSGENDER_1", "ITEM_CH_SWORD_09_A", ""},
	} {
		if got := modelSwitcherTwin(tc.switcher, tc.target); got != tc.want {
			t.Fatalf("%s on %s = %q, want %q", tc.switcher, tc.target, got, tc.want)
		}
	}
	for _, item := range commerce.ModelSwitcherItems {
		if !IsModelSwitcher(item) {
			t.Fatalf("%s is listed but not admitted", item)
		}
	}
	if IsModelSwitcher("ITEM_MALL_EQUIP_TRANSGENDER_1") {
		t.Fatal("the gender tool classified as a switcher")
	}
}
