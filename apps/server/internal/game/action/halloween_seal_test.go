/*
===========================================================================

halloween_seal_test.go - the Halloween helmet seal at every door

The seal (its own switch, on after the event) refuses the helmet at
the trade table, the warehouse and the player-handing exits while on,
and every door behaves natively while off; every refusal is checked
against an untouched bag. The fixtures (halloweenTestItems,
halloweenRuntime, halloweenTestWindow and the inventory helpers)
stay with the exchange tests in halloween_test.go.

===========================================================================
*/
package action

import (
	"bytes"
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestHalloweenHelmetSealBothSettings

The seal (its own switch, on after the event) refuses the helmet at
the trade table and the warehouse while on, and every door behaves
natively while off.
================
*/
func TestHalloweenHelmetSealBothSettings(t *testing.T) {
	c := testCharacter()
	helmetRow := enterworld.InventoryRow{
		Slot: 20, RefObjID: 23455, Codename: "ITEM_ETC_E071030_M_EGHEAD",
		TypeFlags: wire.PackTypeFlags(3, 1, 13, 0), StackCount: 1,
	}
	c.MissionInventory = []enterworld.InventoryRow{helmetRow}
	rt, _ := halloweenRuntime(t, c, halloweenTestWindow)
	t.Setenv(progression.EnvHalloweenHelmetSeal, "on")
	rt.HalloweenHelmetSeal = progression.HalloweenHelmetSealFromEnv()

	item, _ := bagOf(c).At(20)
	if rt.storableItem(item) {
		t.Fatal("sealed helmet entered the warehouse")
	}
	if _, code := rt.exchangeOfferable(c, 20); code != wire.ExchangeErrCannotTrade {
		t.Fatal("sealed helmet reached the trade table", code)
	}
	if _, code := rt.stallOfferable(testDivision, c, 20); code != wire.StallErrInvalidOp {
		t.Fatal("sealed helmet reached the stall", code)
	}

	rt.HalloweenHelmetSeal = progression.HalloweenHelmetSeal{}
	if !rt.storableItem(item) {
		t.Fatal("unsealed helmet refused by the warehouse")
	}
	if _, code := rt.exchangeOfferable(c, 20); code != 0 {
		t.Fatal("unsealed helmet refused by the trade table", code)
	}
	if _, code := rt.stallOfferable(testDivision, c, 20); code != 0 {
		t.Fatal("unsealed helmet refused by the stall", code)
	}
}

/*
================
TestHalloweenHelmetSealDoorRefusals

A sealed event helmet is refused at every exit that hands it to
another player with the door's own failure code on the wire, and
moves through each door untouched while the seal is off (each half
proves the other).

The merchant is the one exit the seal leaves open: the sale
destroys the helmet, and the sale test proves no buyback row pays
for one. The ground drop's is the indisposable refusal, and the
companion transfer's is the never-enter refusal; the exchange,
the stall, and the warehouse are pinned at their helper layer in
TestHalloweenHelmetSealBothSettings and in the warehouse's own wire
tests (its refusal is announced by the same guard it already uses
for an unborrowable row).
================
*/
func TestHalloweenHelmetSealDoorRefusals(t *testing.T) {
	newCharacter := func() *enterworld.Character {
		c := testCharacter()
		c.MissionInventory = []enterworld.InventoryRow{{
			Slot: 20, RefObjID: 23455, Codename: "ITEM_ETC_E071030_M_EGHEAD",
			TypeFlags: wire.PackTypeFlags(3, 1, 13, 0), StackCount: 1,
		}}
		return c
	}

	t.Run("shop sale", func(t *testing.T) {
		c := newCharacter()
		items := halloweenTestItems()
		items["ITEM_ETC_E071030_M_EGHEAD"].NativeFields = items["ITEM_ETC_E071030_M_EGHEAD"].NativeFields.With("sellPrice", 100)
		rt, _ := newTestRuntime(c, items)
		t.Setenv(progression.EnvHalloweenHelmetSeal, "on")
		rt.HalloweenHelmetSeal = progression.HalloweenHelmetSealFromEnv()
		spawn := simulation.SeedWorldState(c).Spawn
		rt.NpcSpawn.Enabled = true
		rt.NpcRoster = []simulation.NpcDef{{ObjectID: 17, RefObjID: 100, TalkFlags: simulation.NpcTalkFlagShop, AuthoredSpawn: true, Spawn: spawn, NpcTalkStoreGroups: []simulation.NpcTalkStoreGroup{{StoreGroupID: 100, Tabs: []simulation.NpcTalkStoreTab{{TabID: 1}}}}}}
		rt.Selected.Set(testDivision, c.Name, 17)
		// An in-range shop request opened the merchant (npcrange.go).
		rt.Selected.OpenFunction(testDivision, c.Name, 17)
		rt.Commerce = &commerce.Catalog{Tabs: map[int32][]commerce.Offer{1: {{Slot: 2, Ref: items["ITEM_ETC_HP_POTION_01"], Price: 60, Stack: 50}}}}

		before := c.Snapshot()
		r := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopSell, NpcGID: 17, SourceSlot: 20, Quantity: 1})
		// The seal leaves the merchant open: the sale destroys the
		// helmet (the bag row is gone next snapshot) and pays no
		// buyback row, so no player can repurchase it.
		if len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 ||
			bagCount(c, "ITEM_ETC_E071030_M_EGHEAD") != 0 || goldOf(c) != goldOf(before)+100 ||
			len(c.Buyback) != 0 {
			t.Fatalf("sealed sale: %+v gold %d buyback %v", r, goldOf(c), c.Buyback)
		}
		c.MissionInventory = []enterworld.InventoryRow{{
			Slot: 20, RefObjID: 23455, Codename: "ITEM_ETC_E071030_M_EGHEAD",
			TypeFlags: wire.PackTypeFlags(3, 1, 13, 0), StackCount: 1,
		}}
		rt.HalloweenHelmetSeal = progression.HalloweenHelmetSeal{}
		r = trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopSell, NpcGID: 17, SourceSlot: 20, Quantity: 1})
		if len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 ||
			bagCount(c, "ITEM_ETC_E071030_M_EGHEAD") != 0 || goldOf(c) != goldOf(before)+200 ||
			len(c.Buyback) != 0 {
			t.Fatalf("unsealed sale: %+v gold %d buyback %v", r, goldOf(c), c.Buyback)
		}
	})

	t.Run("shop sale of an unsellable row", func(t *testing.T) {
		c := newCharacter()
		// The HP potion's fixture reference carries no canSell byte,
		// so the merchant refuses the sale before any seal logic.
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
			Slot: 21, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01",
			TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 1,
		})
		items := halloweenTestItems()
		rt, _ := newTestRuntime(c, items)
		spawn := simulation.SeedWorldState(c).Spawn
		rt.NpcSpawn.Enabled = true
		rt.NpcRoster = []simulation.NpcDef{{ObjectID: 17, RefObjID: 100, TalkFlags: simulation.NpcTalkFlagShop, AuthoredSpawn: true, Spawn: spawn, NpcTalkStoreGroups: []simulation.NpcTalkStoreGroup{{StoreGroupID: 100, Tabs: []simulation.NpcTalkStoreTab{{TabID: 1}}}}}}
		rt.Selected.Set(testDivision, c.Name, 17)
		rt.Selected.OpenFunction(testDivision, c.Name, 17)
		rt.Commerce = &commerce.Catalog{Tabs: map[int32][]commerce.Offer{1: {{Slot: 2, Ref: items["ITEM_ETC_HP_POTION_01"], Price: 60, Stack: 50}}}}

		before := c.Snapshot()
		r := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopSell, NpcGID: 17, SourceSlot: 21, Quantity: 1})
		if len(r.Frames) != 1 || !bytes.Equal(r.Frames[0].Payload, []byte{2, wire.ErrCodeCannotDealAtShop}) ||
			!reflect.DeepEqual(before.MissionInventory, c.MissionInventory) {
			t.Fatalf("unsellable sale: %+v", r)
		}
	})

	t.Run("ground drop", func(t *testing.T) {
		c := newCharacter()
		rt, _ := halloweenRuntime(t, c, halloweenTestWindow)
		t.Setenv(progression.EnvHalloweenHelmetSeal, "on")
		rt.HalloweenHelmetSeal = progression.HalloweenHelmetSealFromEnv()

		before := c.Snapshot()
		r := rt.HandleItemMove(testDivision, c, encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeGroundDrop, SourceSlot: 20}))
		if len(r.Frames) != 1 || !bytes.Equal(r.Frames[0].Payload, []byte{2, wire.ErrCodeCannotBeDropped}) ||
			!reflect.DeepEqual(before.MissionInventory, c.MissionInventory) {
			t.Fatalf("sealed drop: %+v", r)
		}

		rt.HalloweenHelmetSeal = progression.HalloweenHelmetSeal{}
		r = rt.HandleItemMove(testDivision, c, encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeGroundDrop, SourceSlot: 20}))
		echo := false
		for _, frame := range r.Frames {
			if len(frame.Payload) > 0 && frame.Payload[0] == 1 {
				echo = true
			}
		}
		if !echo || bagCount(c, "ITEM_ETC_E071030_M_EGHEAD") != 0 {
			t.Fatalf("unsealed drop: %+v", r)
		}
	})

	t.Run("cos transfer", func(t *testing.T) {
		c := newCharacter()
		rt, _ := newTestRuntime(c, testCosSource(halloweenTestItems()))
		t.Setenv(progression.EnvHalloweenHelmetSeal, "on")
		rt.HalloweenHelmetSeal = progression.HalloweenHelmetSealFromEnv()
		gid, _ := enterworld.CosObjectIDForCharacter(c)
		c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 100, Summoned: true, Container: &domain.COSContainer{Capacity: 4}}

		payload, err := (wire.ItemMoveRequest{MovementType: wire.MoveTypePlayerToCos, CosGID: gid, SourceSlot: 20, DestSlot: 0}).Encode()
		if err != nil {
			t.Fatal(err)
		}
		before := c.Snapshot()
		r := rt.HandleItemMove(testDivision, c, payload)
		if len(r.Frames) != 1 || !bytes.Equal(r.Frames[0].Payload, []byte{2, wire.ErrCodeCannotEnterCosBag}) ||
			!reflect.DeepEqual(before.MissionInventory, c.MissionInventory) {
			t.Fatalf("sealed transfer: %+v", r)
		}

		rt.HalloweenHelmetSeal = progression.HalloweenHelmetSeal{}
		r = rt.HandleItemMove(testDivision, c, payload)
		if len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 ||
			bagCount(c, "ITEM_ETC_E071030_M_EGHEAD") != 0 || len(c.ActiveCOS.Container.Rows) != 1 {
			t.Fatalf("unsealed transfer: %+v", r)
		}
	})
}
