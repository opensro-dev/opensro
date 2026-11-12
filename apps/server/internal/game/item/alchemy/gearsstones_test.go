/*
===========================================================================

gearsstones_test.go - the gears magic stones on the native stone path

Port-only, not native (issue #3257): the later client's leveled magic
stones (commerce.GearsStoneKinds, degrees 1-7) name shipped
magicoption rows, so the native Stone application admits them with
no catalog addition. The first half proves the option resolution
for all 112 against the shipped tables; the second applies one
synthetic gears stone end to end.

===========================================================================
*/
package alchemy

import (
	"errors"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestGearsStoneOptionsResolveOnShippedTables

Every gears kind at every degree names an option the shipped
magicoption table carries (the import's Descriptions[0] values,
measured from the later client), so the imported rows pass the
native option and degree gates unchanged.
================
*/
func TestGearsStoneOptionsResolveOnShippedTables(t *testing.T) {
	clientdata.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	c, err := LoadCatalog(dir, enterworld.NewTextdataItems(dir))
	if err != nil {
		t.Fatal(err)
	}
	// The kinds' Descriptions[0] values, as the later client authors
	// them: each kind's option name at degrees 1-7.
	options := map[string]string{
		"STR": "MATTR_STR", "INT": "MATTR_INT", "HP": "MATTR_HP", "MP": "MATTR_MP",
		"HR": "MATTR_HR", "DUR": "MATTR_DUR", "ER": "MATTR_ER", "SOLID": "MATTR_SOLID",
		"BURN": "MATTR_RESIST_BURN", "POISON": "MATTR_RESIST_POISON",
		"FROSTBITE": "MATTR_RESIST_FROSTBITE", "ZOMBIE": "MATTR_RESIST_ZOMBIE",
		"ESHOCK": "MATTR_RESIST_ESHOCK", "EVADE_BLOCK": "MATTR_EVADE_BLOCK",
		"EVADE_CRITICAL": "MATTR_EVADE_CRITICAL", "LUCK": "MATTR_LUCK",
	}
	if len(options) != 16 {
		t.Fatalf("gears kinds = %d, want 16", len(options))
	}
	for kind, name := range options {
		for degree := 1; degree <= 7; degree++ {
			if _, ok := c.Option(name, degree); !ok {
				t.Fatalf("gears kind %s degree %d: no shipped option %s", kind, degree, name)
			}
		}
	}
}

/*
================
TestGearsStoneAppliesLikeANativeStone

A gears stone (magic-stone type 3/3/11/1, degree 5, MATTR_STR)
applies to a degree-5 weapon through the untouched Stone path: the
option lands, one stone is spent, and a degree mismatch answers the
native magic-degree code.
================
*/
func TestGearsStoneAppliesLikeANativeStone(t *testing.T) {
	c, items := fixture()
	weapon := c.Items["weapon"]
	weapon.Class = 13
	weapon.MaxMagic = 9
	c.Items["weapon"] = weapon
	stone := Reference{ID: 4, Name: "ITEM_ETC_ARCHEMY_LEVEL_MAGICSTONE_STR_05",
		Flags: wire.PackTypeFlags(3, 3, 11, 1), Params: [5]uint32{5, 0, 0, 50},
		Descriptions: [5]string{"MATTR_STR", "", "", "weapon"}}
	c.Items[stone.Name] = stone
	c.Magic[11] = Magic{ID: 11, Name: "MATTR_STR", Degree: 5, Tag: 0x737472,
		Params: [3]uint32{1<<16 | 2, 3 << 16}, Categories: []string{"weapon"}}
	items = items[:1]
	items[0].Codename = "weapon"
	items = append(items, inventory.Item{Slot: 14, RefObjID: stone.ID, Codename: stone.Name,
		TypeFlags: stone.Flags, Quantity: 2})

	out, err := c.Stone(items, []uint8{13, 14}, true, 0, sequence(t, 50, 1, 100))
	if err != nil {
		t.Fatal(err)
	}
	if !out.Success {
		t.Fatalf("gears stone outcome = %+v, want success", out)
	}
	if len(out.Items) != 2 || out.Items[1].Quantity != 1 {
		t.Fatalf("gears stone spend = %+v, want one of two left", out.Items)
	}
	if len(out.Items[0].MagicOptions) != 1 || out.Items[0].MagicOptions[0] != 2<<32|11 {
		t.Fatalf("weapon options = %v, want the MATTR_STR value", out.Items[0].MagicOptions)
	}

	// A degree-9 weapon refuses the degree-5 stone after the roll,
	// with the native magic-degree code, keeping the stone.
	weapon.Class = 25
	c.Items["weapon"] = weapon
	_, err = c.Stone(items, []uint8{13, 14}, true, 0, sequence(t, 50))
	var refusal Refusal
	if !errors.As(err, &refusal) || refusal != Refusal(stoneErrMagicDegree) {
		t.Fatalf("degree mismatch = %v, want refusal %#x", err, stoneErrMagicDegree)
	}
}
