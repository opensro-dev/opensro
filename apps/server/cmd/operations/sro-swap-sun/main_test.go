/*
===========================================================================

main_test.go - the Sun-to-Moon mapping on the shipped v1.150 data

A degree-9 Sun row becomes the Moon of the same item, honor variants too,
keeping its plus, variance word and options; durability above the Moon's
maximum is clamped to it, never raised; every other row is left alone.

===========================================================================
*/
package main

import (
	"opensro.online/server/internal/testsupport/tier"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

// fullVariance rolls every white field at 31 (100%).
const fullVariance = "1152921504606846975"

/*
================
TestSunRowsBecomeTheirMoon
================
*/
func TestSunRowsBecomeTheirMoon(t *testing.T) {
	tier.Long(t, "every sun row swaps to its moon row")
	dir := gamedatatest.TextdataDir(t)
	w := &swapper{items: enterworld.NewTextdataItems(dir), magic: enterworld.NewTextdataMagicOptions(dir)}
	for sun, moon := range map[string]string{
		"ITEM_CH_BLADE_09_C_RARE":       "ITEM_CH_BLADE_09_B_RARE",
		"ITEM_CH_SWORD_09_C_RARE_HONOR": "ITEM_CH_SWORD_09_B_RARE_HONOR",
		"ITEM_EU_TSWORD_09_C_RARE":      "ITEM_EU_TSWORD_09_B_RARE",
	} {
		ref, _ := w.items.ItemRefByCodename(sun)
		row := domain.InventoryRow{Slot: 6, RefObjID: ref.RefObjID, Codename: sun, TypeFlags: ref.TypeFlags(), Plus: 9,
			VarianceBits: fullVariance, Durability: 10_000, StackCount: 1}
		next, changed, err := w.swap(row)
		if err != nil || !changed || next.Codename != moon {
			t.Fatalf("%s: %v %v %s", sun, err, changed, next.Codename)
		}
		moonRef, _ := w.items.ItemRefByCodename(moon)
		maximum, err := combat.EquipmentMaxDurability(moonRef, next, w.magic)
		if err != nil || next.Durability != int64(maximum) || next.RefObjID != moonRef.RefObjID {
			t.Fatalf("%s: durability %d, Moon max %d (%v)", sun, next.Durability, maximum, err)
		}
		if next.Plus != 9 || next.VarianceBits != fullVariance || next.Slot != 6 {
			t.Fatalf("%s: kept fields changed %+v", sun, next)
		}
		low := row
		low.Durability = 1
		if next, _, _ := w.swap(low); next.Durability != 1 {
			t.Fatalf("%s: durability raised to %d", sun, next.Durability)
		}
	}
	for _, other := range []string{"ITEM_CH_BLADE_09_B_RARE", "ITEM_CH_BLADE_08_C_RARE", "ITEM_CH_BLADE_09_C", "ITEM_ETC_HP_POTION_01"} {
		if _, changed, err := w.swap(domain.InventoryRow{Codename: other, StackCount: 1}); changed || err != nil {
			t.Fatalf("%s: changed=%v err=%v", other, changed, err)
		}
	}
}

/*
================
TestCharacterNamesRefuseEmptyAndRepeated
================
*/
func TestCharacterNamesRefuseEmptyAndRepeated(t *testing.T) {
	if names, err := characterNames(" killmepls , OpenAI"); err != nil || len(names) != 2 || names[0] != "killmepls" || names[1] != "OpenAI" {
		t.Fatalf("%v %q", err, names)
	}
	for _, bad := range []string{"", "a,,b", "a,a", "a, a"} {
		if _, err := characterNames(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}
