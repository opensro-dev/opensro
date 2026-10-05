/*
===========================================================================

stall_test.go - opening a stall, visiting it, buying from it and from afar

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
stallFixture

The exchange pair in a town; the owner also carries 20 potions in slot
13.
================
*/
func stallFixture(t *testing.T) (*Runtime, *enterworld.Character, *enterworld.Character, map[string][]wire.Frame) {
	t.Helper()
	rt, owner, buyer, pushed := exchangeFixture(t)
	town := int64(0x62a8)
	for _, c := range []*enterworld.Character{owner, buyer} {
		if c.World == nil {
			c.World = &enterworld.CharacterWorld{}
		}
		if c.World.Spawn == nil {
			c.World.Spawn = &enterworld.WorldSpawn{}
		}
		c.World.Spawn.RegionID = &town
	}
	owner.MissionInventory = append(owner.MissionInventory, enterworld.InventoryRow{
		Slot: 13, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 20,
	})
	items := rt.deps.(*enterworld.Deps).Items.(staticItemSource)
	potion := *items["ITEM_ETC_HP_POTION_01"]
	potion.NativeFields = potion.NativeFields.With("canTrade", 1)
	items["ITEM_ETC_HP_POTION_01"] = &potion
	rt.StallCategories = map[[4]int64]uint32{{3, 3, 1, 1}: 40}
	return rt, owner, buyer, pushed
}

/*
================
stallEdit
================
*/
func stallEdit(kind uint8, body ...byte) []byte {
	return append([]byte{kind}, body...)
}

/*
================
TestStallSellsToAVisitor

The owner opens, puts 5 of 20 potions up for 300 gold and opens for
business; a visitor sees the offer and buys it: 5 potions land in the
visitor's first empty bag slot, 15 stay with the owner and 300 gold
changes hands.
================
*/
func TestStallSellsToAVisitor(t *testing.T) {
	rt, owner, buyer, pushed := stallFixture(t)
	title := wire.NewWriter(16).WStr("Potions").Payload()
	if out := rt.HandleStallCreate(testDivision, owner, title); !bytes.Equal(out.Frames[0].Payload, []byte{1}) {
		t.Fatalf("create %+v", out.Frames)
	}
	add := stallEdit(wire.StallEditAdd, 0, 13, 5, 0, 0x2c, 1, 0, 0, 0, 0, 0, 0, 0)
	if out := rt.HandleStallEdit(testDivision, owner, add); len(out.Frames) != 1 || out.Frames[0].Payload[0] != 1 {
		t.Fatalf("add %+v", out.Frames)
	}
	if out := rt.HandleStallEdit(testDivision, owner, stallEdit(wire.StallEditOpen, 1, 1)); !bytes.Equal(out.Frames[0].Payload, []byte{1, 5, 1, 1}) {
		t.Fatalf("open %+v", out.Frames)
	}
	if out := rt.HandleStallEdit(testDivision, owner, add); out.Frames[0].Payload[0] != 2 {
		t.Fatal("an open stall took a new offer")
	}
	visit := rt.HandleStallVisit(testDivision, buyer, wire.NewWriter(4).U32(enterworld.ObjectIDForCharacter(owner)).Payload())
	if len(visit.Frames) != 1 || visit.Frames[0].Payload[0] != 1 {
		t.Fatalf("visit %+v", visit.Frames)
	}
	lastFrame(t, pushed[owner.Name], wire.OpStallEvent)

	listed := rt.HandleStallNetworkSearch(testDivision, buyer, []byte{0, 0, 40, 0, 0, 0, 0})
	if p := listed.Frames[0].Payload; p[0] != 1 || p[1] != 1 || p[2] != 1 {
		t.Fatalf("network search % X", p)
	}

	if out := rt.HandleStallBuy(testDivision, buyer, []byte{0}); !bytes.Equal(out.Frames[0].Payload, []byte{1, 0}) {
		t.Fatalf("buy %+v", out.Frames)
	}
	if goldOf(buyer) != 4700 || goldOf(owner) != 5300 {
		t.Fatalf("gold %d / %d", goldOf(buyer), goldOf(owner))
	}
	if len(buyer.MissionInventory) != 1 || buyer.MissionInventory[0].Slot != 13 || buyer.MissionInventory[0].StackCount != 5 {
		t.Fatalf("buyer bag %+v", buyer.MissionInventory)
	}
	for _, row := range owner.MissionInventory {
		if row.Slot == 13 && row.StackCount != 15 {
			t.Fatalf("owner kept %d potions", row.StackCount)
		}
	}
	sold := lastFrame(t, pushed[owner.Name], wire.OpStallEvent)
	if sold.Payload[0] != wire.StallEventSold || sold.Payload[1] != 0 {
		t.Fatalf("sale event % X", sold.Payload)
	}
}

/*
================
TestStallNetworkSaleTakesCommission

A networked offer bought from afar pays the owner its price less 1%.
================
*/
func TestStallNetworkSaleTakesCommission(t *testing.T) {
	rt, owner, buyer, _ := stallFixture(t)
	rt.HandleStallCreate(testDivision, owner, wire.NewWriter(16).WStr("Potions").Payload())
	rt.HandleStallEdit(testDivision, owner, stallEdit(wire.StallEditAdd, 0, 13, 20, 0, 0xe8, 3, 0, 0, 0, 0, 0, 0, 0))
	rt.HandleStallEdit(testDivision, owner, stallEdit(wire.StallEditOpen, 1, 1))
	s, _ := rt.Stalls.Get(testDivision, owner.Name)
	gid := enterworld.ObjectIDForCharacter(owner)
	stale := wire.NewWriter(24).U32(gid).U8(0).U64(1000).U16(20).U8(0).U64(s.Slots[0].Serial + 1).Payload()
	if out := rt.HandleStallNetworkBuy(testDivision, buyer, stale); !bytes.Equal(out.Frames[0].Payload, []byte{2, wire.StallErrNetworkStale}) {
		t.Fatalf("a stale row answered %+v", out.Frames)
	}
	buy := wire.NewWriter(24).U32(gid).U8(0).U64(1000).U16(20).U8(0).U64(s.Slots[0].Serial).Payload()
	if out := rt.HandleStallNetworkBuy(testDivision, buyer, buy); !bytes.Equal(out.Frames[0].Payload, []byte{1}) {
		t.Fatalf("network buy %+v", out.Frames)
	}
	if goldOf(buyer) != 4000 || goldOf(owner) != 5990 {
		t.Fatalf("gold %d / %d", goldOf(buyer), goldOf(owner))
	}
}
