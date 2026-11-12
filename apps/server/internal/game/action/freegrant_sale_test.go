/*
===========================================================================

freegrant_sale_test.go - a merchant pays nothing for free-grant items

Port-only, not native (#3308): while SRO_BETA_STARTER_KIT is on, the
rows the kit hands out free (the kit scrolls and every refill grade)
sell for 0 - the sale still succeeds and clears the bag, the quote
shows the same 0, and no npc_sale ledger row is written. With the
switch off the same rows pay their native price. Both directions run
from the wire through the real authority store into the ledger
counters.

===========================================================================
*/
package action

import (
	"encoding/json"
	"testing"
	"time"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
freeGrantShopFixture

A character holding a refilled potion stack and a kit scroll in front
of an open merchant, settled through the real authority store.
================
*/
func freeGrantShopFixture(t *testing.T) (*store.Store, *Runtime, *enterworld.Character) {
	t.Helper()
	seed := testCharacter()
	seed.MissionInventory = append(seed.MissionInventory,
		enterworld.InventoryRow{Slot: 13, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01",
			TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 5, VarianceBits: "0"},
		enterworld.InventoryRow{Slot: 14, RefObjID: 61, Codename: "ITEM_ETC_SCROLL_RETURN_01",
			TypeFlags: wire.PackTypeFlags(3, 3, 3, 1), StackCount: 1, VarianceBits: "0"})

	authority, err := store.Open(t.TempDir(), store.Options{DefaultSkills: doorSkillSeeder})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(authority.Close)
	if err := authority.CreateCharacter(testDivision, "test-account", seed); err != nil {
		t.Fatal(err)
	}
	c := authority.Characters().CharactersForDivision(testDivision)[0]

	items := testItems()
	potion := items["ITEM_ETC_HP_POTION_01"]
	potion.NativeFields = potion.NativeFields.With("sellPrice", 15).With("canSell", 1)
	items["ITEM_ETC_SCROLL_RETURN_01"] = &enterworld.ItemRef{
		RefObjID: 61, Codename: "ITEM_ETC_SCROLL_RETURN_01", TypeIDs: [4]int64{3, 3, 3, 1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{
			"price": 100, "sellPrice": 40, "maxStack": 20, "canSell": 1}),
	}
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {c}},
		Items:      items,
	}
	// The production wiring (wiring_authority.go): the trade settlement
	// commits through the authority store's own door.
	deps.UpdateTrade = authority.UpdateTrade
	deps.ReadCharacter = func(_ string, read func()) { authority.ReadState(read) }
	rt := NewRuntime(deps, nil)
	rt.Now = (&fakeClock{now: time.UnixMilli(1_000_000)}).Now
	rt.NpcSpawn.Enabled = true
	rt.NpcRoster = []simulation.NpcDef{{ObjectID: 17, RefObjID: 100, Codename: "NPC_CH_SMITH",
		TalkFlags: simulation.NpcTalkFlagShop, AuthoredSpawn: true, Spawn: simulation.SeedWorldState(c).Spawn,
		NpcTalkStoreGroups: []simulation.NpcTalkStoreGroup{{StoreGroupID: 100, Tabs: []simulation.NpcTalkStoreTab{{TabID: 1}}}}}}
	rt.Selected.Set(testDivision, c.Name, 17)
	rt.Selected.OpenFunction(testDivision, c.Name, 17)
	rt.Commerce = &commerce.Catalog{Tabs: map[int32][]commerce.Offer{1: {{Slot: 2, Ref: potion, Price: 60, Stack: 50}}}}
	return authority, rt, c
}

/*
================
TestFreeGrantSalePriceFollowsTheKitSwitch

Kit on: the potion stack and the kit scroll sell, credit nothing,
write no npc_sale row, and quote 0. Kit off: the same rows pay their
native prices and the ledger gains exactly the credit.
================
*/
func TestFreeGrantSalePriceFollowsTheKitSwitch(t *testing.T) {
	for _, kitOn := range []bool{true, false} {
		name := "kit off pays the native price"
		if kitOn {
			name = "kit on pays nothing"
		}
		t.Run(name, func(t *testing.T) {
			if kitOn {
				t.Setenv(enterworld.EnvBetaStarterKit, "on")
			} else {
				t.Setenv(enterworld.EnvBetaStarterKit, "off")
			}
			authority, rt, c := freeGrantShopFixture(t)

			var quote shopProjection
			if err := json.Unmarshal(rt.shopCatalog(testDivision, c, 17).Payload, &quote); err != nil {
				t.Fatal(err)
			}
			quoted := map[uint8]string{}
			for _, row := range quote.SaleQuotes {
				quoted[row.Slot] = row.Price
			}
			wantPotionQuote, wantScrollQuote := "15", "40"
			if kitOn {
				wantPotionQuote, wantScrollQuote = "0", "0"
			}
			if quoted[13] != wantPotionQuote || quoted[14] != wantScrollQuote {
				t.Fatalf("quotes %v, want potion %s and scroll %s", quoted, wantPotionQuote, wantScrollQuote)
			}

			before := authority.EconomyCounters()
			goldBefore := goldOf(c)
			for _, sale := range []wire.ItemMoveRequest{
				{MovementType: wire.MoveTypeShopSell, NpcGID: 17, SourceSlot: 13, Quantity: 5},
				{MovementType: wire.MoveTypeShopSell, NpcGID: 17, SourceSlot: 14, Quantity: 1},
			} {
				result := trade(t, rt, c, sale)
				if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 1 {
					t.Fatalf("the sale of slot %d did not succeed: %+v", sale.SourceSlot, result.Frames)
				}
			}
			if bagCount(c, "ITEM_ETC_HP_POTION_01") != 0 || bagCount(c, "ITEM_ETC_SCROLL_RETURN_01") != 0 {
				t.Fatalf("the sold rows stayed in the bag: %+v", c.MissionInventory)
			}
			after := authority.EconomyCounters()
			credited := goldOf(c) - goldBefore
			ledgerGain := after["gold.npc_sale.gain"] - before["gold.npc_sale.gain"]
			want := uint64(0)
			if !kitOn {
				want = 5*15 + 40
			}
			if credited != want || ledgerGain != want {
				t.Fatalf("credited %d with npc_sale gain %d, want both %d", credited, ledgerGain, want)
			}
		})
	}
}
