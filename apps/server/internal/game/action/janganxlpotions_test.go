/*
===========================================================================

janganxlpotions_test.go - buying X-Large potions at Jangan's potion merchant

===========================================================================
*/
package action

import (
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
janganPotionFixture

A store-backed runtime whose commerce catalogue is the shipped one plus
the environment's goods, composed the way the GameWorld wiring composes
them, with a shop NPC standing on Jangan's potion tab. Returns the
runtime and the tab's X-Large slots (negative when the switch is off).
================
*/
func janganPotionFixture(t *testing.T, xl string) (*doorRuntime, int, int) {
	t.Helper()
	clientdata.RequireGameData(t)
	t.Setenv(commerce.EnvShopExtraGoods, "off")
	t.Setenv(commerce.EnvJanganXLPotions, xl)
	seed := testCharacter()
	setGold(seed, 100000)
	d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), seed)
	d.rt.deps.(*enterworld.Deps).Items = shippedItems(t)
	extra, err := commerce.ExtraGoodsFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	xlGoods, err := commerce.JanganXLPotionGoodsFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if err := d.rt.ConfigureCommerce(gamedatatest.TextdataDir(t), append(extra, xlGoods...)); err != nil {
		t.Fatal(err)
	}
	// The X-Large goods are addressed to Jangan's potion tab by name, so
	// while the switch is on that tab is the one holding both the smallest
	// potion and the X-Large pair (Hotan's tab stocks the pair natively
	// but no smallest potion). Off, it is a smallest-potion tab with no
	// larger size; Constantinople's tab is stocked identically.
	tabID, hpSlot, mpSlot := int32(0), -1, -1
	for id, offers := range d.rt.Commerce.Tabs {
		codes := map[string]int{}
		for _, offer := range offers {
			if offer.Ref != nil {
				codes[offer.Ref.Codename] = int(offer.Slot)
			}
		}
		_, small := codes["ITEM_ETC_HP_POTION_01"]
		_, large := codes["ITEM_ETC_HP_POTION_03"]
		if hp, xl := codes["ITEM_ETC_HP_POTION_05"]; small && xl {
			tabID, hpSlot = id, hp
			if mp, ok := codes["ITEM_ETC_MP_POTION_05"]; ok {
				mpSlot = mp
			}
			break
		}
		if small && !large && tabID == 0 {
			tabID = id
		}
	}
	if tabID == 0 {
		t.Fatal("the shipped catalogue has no Jangan potion tab")
	}
	spawn := simulation.SeedWorldState(d.character).Spawn
	d.rt.NpcSpawn.Enabled = true
	d.rt.NpcRoster = []simulation.NpcDef{{ObjectID: 17, RefObjID: 100, TalkFlags: simulation.NpcTalkFlagShop, AuthoredSpawn: true, Spawn: spawn, NpcTalkStoreGroups: []simulation.NpcTalkStoreGroup{{StoreGroupID: 100, Tabs: []simulation.NpcTalkStoreTab{{TabID: tabID}}}}}}
	d.rt.Selected.Set(testDivision, d.character.Name, 17)
	// An in-range shop request opened the merchant (npcrange.go).
	d.rt.Selected.OpenFunction(testDivision, d.character.Name, 17)
	return d, hpSlot, mpSlot
}

/*
================
TestBuyingXLPotionsAtJanganRoundTrips

With the switch on (owner 2026-11-07, #2771): the X-Large pair buys at
Jangan's potion merchant for Hotan's 900 each, the stacks grow in one
row, the gold stays debited across a restart, and the potions are still
in the bag when the character comes back.
================
*/
func TestBuyingXLPotionsAtJanganRoundTrips(t *testing.T) {
	d, hpSlot, mpSlot := janganPotionFixture(t, "on")
	if hpSlot < 0 || mpSlot < 0 {
		t.Fatalf("X-Large slots = %d, %d; want both shelved", hpSlot, mpSlot)
	}
	buy := func(slot, quantity int) OpResult {
		return trade(t, d.rt, d.character, wire.ItemMoveRequest{
			MovementType: 8, NpcGID: 17, ShopSlot: uint8(slot), Quantity: uint16(quantity),
		})
	}
	if r := buy(hpSlot, 2); len(r.Frames) == 0 || r.Frames[0].Opcode != 14 {
		t.Fatalf("HP purchase answered %+v", r.Frames)
	}
	if r := buy(mpSlot, 1); len(r.Frames) == 0 || r.Frames[0].Opcode != 14 {
		t.Fatalf("MP purchase answered %+v", r.Frames)
	}
	if r := buy(hpSlot, 1); len(r.Frames) == 0 || r.Frames[0].Opcode != 14 {
		t.Fatalf("second HP purchase answered %+v", r.Frames)
	}
	// 3 HP and 1 MP at 900: the second HP buy grows the first stack.
	const wantGold = 100000 - 4*900
	if goldOf(d.character) != wantGold {
		t.Fatalf("gold = %d, want %d after four potions at 900", goldOf(d.character), wantGold)
	}
	hp := bagRowByCodename(d.character, "ITEM_ETC_HP_POTION_05")
	mp := bagRowByCodename(d.character, "ITEM_ETC_MP_POTION_05")
	if hp == nil || hp.StackCount != 3 || mp == nil || mp.StackCount != 1 {
		t.Fatalf("bag rows: HP %+v, MP %+v; want stacks of 3 and 1", hp, mp)
	}

	back := d.reboot(t)
	if goldOf(back.character) != wantGold {
		t.Fatalf("gold after restart = %d, want %d", goldOf(back.character), wantGold)
	}
	hp = bagRowByCodename(back.character, "ITEM_ETC_HP_POTION_05")
	mp = bagRowByCodename(back.character, "ITEM_ETC_MP_POTION_05")
	if hp == nil || hp.StackCount != 3 || mp == nil || mp.StackCount != 1 {
		t.Fatalf("bag rows after restart: HP %+v, MP %+v; want stacks of 3 and 1", hp, mp)
	}
}

/*
================
TestJanganSellsNoXLPotionsWithTheSwitchOff

Off is the native Jangan shop: no X-Large offer on the tab, and a buy
aimed at the slot they would have taken refuses without touching gold
or the bag.
================
*/
func TestJanganSellsNoXLPotionsWithTheSwitchOff(t *testing.T) {
	d, hpSlot, mpSlot := janganPotionFixture(t, "off")
	if hpSlot >= 0 || mpSlot >= 0 {
		t.Fatalf("off shelves the X-Large pair at %d, %d", hpSlot, mpSlot)
	}
	before := goldOf(d.character)
	rows := len(d.character.MissionInventory)
	r := trade(t, d.rt, d.character, wire.ItemMoveRequest{
		MovementType: 8, NpcGID: 17, ShopSlot: 8, Quantity: 1,
	})
	if len(r.Frames) != 1 || r.Frames[0].Payload[0] != 2 {
		t.Fatalf("buying past the authored tab answered %+v", r.Frames)
	}
	if goldOf(d.character) != before || len(d.character.MissionInventory) != rows {
		t.Fatal("a refused purchase changed the character")
	}
}
