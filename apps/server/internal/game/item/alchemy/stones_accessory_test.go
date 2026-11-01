/*
===========================================================================

stones_accessory_test.go - Astral on accessories, on the shipped tables

The synthetic stone suite exercises a weapon; the player report behind
this file is a ring (thread 1560377337454002331: "nothing happens").
On the shipped catalogue an accessory is a legal Astral recipient
whenever its Immortal charge exceeds its Astral charge (magicoption's
own category rows admit accessories); without the charges the server's
answer is refusal 0x24 - the native 0x5424 family - which the client
renders as silence. These probes pin both halves so nobody "fixes" the
gate later.

===========================================================================
*/
package alchemy

import (
	"errors"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestAstralOnAccessoryFollowsTheImmortalCharge
================
*/
func TestAstralOnAccessoryFollowsTheImmortalCharge(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	c, err := LoadCatalog(dir, enterworld.NewTextdataItems(dir))
	if err != nil {
		t.Fatal(err)
	}
	ring, ok := c.Items["ITEM_CH_RING_09_C"]
	if !ok || category(ring.Flags) != "accessory" || ring.Degree() != 9 {
		t.Fatalf("degree-9 ring missing or miscategorised: %+v", ring)
	}
	material := func(tag uint32) Reference {
		t.Helper()
		for _, ref := range c.Items {
			opt, ok := c.Option(ref.Descriptions[0], int(ref.Params[0]))
			if ok && opt.Tag == tag && ref.Params[0] == 9 {
				if !opt.Allows(ring.Flags) {
					t.Fatalf("the degree-9 option for %q refuses accessories", ref.Name)
				}
				return ref
			}
		}
		t.Fatalf("no degree-9 material for tag %#x", tag)
		return Reference{}
	}
	athanasia := material(0x61746861)
	astral := material(MagicTagAstral)
	items := func(ringOptions []uint64, stone Reference) []inventory.Item {
		return []inventory.Item{
			{Slot: 13, RefObjID: ring.ID, Codename: ring.Name, TypeFlags: ring.Flags, Quantity: 1, MagicOptions: ringOptions},
			{Slot: 14, RefObjID: stone.ID, Codename: stone.Name, TypeFlags: stone.Flags, Quantity: 1},
		}
	}
	// Athanasia itself lands on the bare ring: the Immortal charge is
	// the Astral gate's key, and accessories hold it like any gear.
	out, err := c.Stone(items(nil, athanasia), []uint8{13, 14}, true, 0, sequence(t, 0, 0))
	if err != nil || !out.Success {
		t.Fatalf("athanasia on a bare ring: %v / %+v", err, out)
	}
	immortal := out.Items[0].MagicOptions
	// Astral without an Immortal charge is the native refusal, 0x24.
	if _, err := c.Stone(items(nil, astral), []uint8{13, 14}, true, 0, sequence(t, 0)); !errors.Is(err, Refusal(0x24)) {
		t.Fatalf("astral without immortal: %v, want refusal 0x24", err)
	}
	// One Immortal charge admits exactly one Astral charge.
	out, err = c.Stone(items(immortal, astral), []uint8{13, 14}, true, 0, sequence(t, 0, 0))
	if err != nil || !out.Success {
		t.Fatalf("astral with immortal 1: %v / %+v", err, out)
	}
	withAstral := append([]inventory.Item{}, out.Items...)
	withAstral = append(withAstral, inventory.Item{Slot: 14, RefObjID: astral.ID, Codename: astral.Name, TypeFlags: astral.Flags, Quantity: 1})
	if _, err := c.Stone(withAstral, []uint8{13, 14}, true, 0, sequence(t, 0)); !errors.Is(err, Refusal(0x24)) {
		t.Fatalf("astral at immortal == astral: %v, want refusal 0x24", err)
	}
}
