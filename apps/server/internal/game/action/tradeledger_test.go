/*
===========================================================================

tradeledger_test.go - a caravan sale's gold reaches the economy ledger

The ledger names a trade-goods sale's seller credit trade_sale_credit
(domain economyMemberReasons, #1348). The action tests stub the trade
door and the store tests label by hand, so no test ran a sale from the
wire through the real authority store into the ledger; this one does
(#2642), and reads the per-reason counters the committed rows feed.

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestCaravanSaleWritesTradeSaleCredit

A trader selling trade goods out of the transport's bag at a special
trade merchant commits through the real store: the seller's gold rises
by the sale's credit, and the ledger's counters gain exactly that much
trade_sale_credit (no npc_sale: the sale is a caravan sale, not an
ordinary shop sale).
================
*/
func TestCaravanSaleWritesTradeSaleCredit(t *testing.T) {
	seed := testCharacter()
	level := minTradeBuyerLevel
	seed.Level = &level
	seed.Job = domain.CharacterJob{Type: domain.JobTrader, Grade: 1, Alias: "Trader"}
	ref := &enterworld.ItemRef{RefObjID: 2151, Codename: "ITEM_ETC_TRADE_WC_01", TypeIDs: [4]int64{3, 3, 8, 1}, NativeFields: enterworld.NewNativeFields(map[string]float64{"price": 101, "sellPrice": 50, "maxStack": 40, "canSell": 1})}
	goods := enterworld.InventoryRow{Slot: 0, RefObjID: 2151, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 3, VarianceBits: "0", TradeOwner: seed.Job.Alias}
	seed.MissionInventory = []domain.InventoryRow{{Slot: 8, RefObjID: 200, Codename: "SUIT", TypeFlags: wire.PackTypeFlags(3, 1, 7, domain.JobTrader), StackCount: 1}}
	gid, _ := enterworld.CosObjectIDForCharacter(seed)
	seed.ActiveCOS = &domain.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 100, Summoned: true, Container: &domain.COSContainer{Capacity: 4, Rows: []domain.InventoryRow{goods}}}

	authority, err := store.Open(t.TempDir(), store.Options{DefaultSkills: doorSkillSeeder})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(authority.Close)
	if err := authority.CreateCharacter(testDivision, "test-account", seed); err != nil {
		t.Fatal(err)
	}
	c := authority.Characters().CharactersForDivision(testDivision)[0]

	refs := testCosSource(testItems())
	refs.staticItemSource[ref.Codename] = ref
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {c}},
		Items:      refs,
	}
	// The production wiring (wiring_authority.go): the trade settlement
	// commits through the authority store's own door.
	deps.UpdateTrade = authority.UpdateTrade
	deps.ReadCharacter = func(_ string, read func()) { authority.ReadState(read) }
	deps.ItemSaleNote = authority.NoteItemSale
	rt := NewRuntime(deps, nil)
	rt.Now = (&fakeClock{now: time.UnixMilli(1_000_000)}).Now
	rt.NpcSpawn.Enabled = true
	rt.NpcRoster = []simulation.NpcDef{{ObjectID: 17, RefObjID: 2010, Codename: "NPC_WC_SPECIAL", TalkFlags: simulation.NpcTalkFlagSpecialTrade, AuthoredSpawn: true, Spawn: simulation.SeedWorldState(c).Spawn, NpcTalkStoreGroups: []simulation.NpcTalkStoreGroup{{Tabs: []simulation.NpcTalkStoreTab{{TabID: 1}}}}}}
	rt.Selected.Set(testDivision, c.Name, 17)
	rt.Selected.OpenFunction(testDivision, c.Name, 17)
	rt.Commerce = &commerce.Catalog{Tabs: map[int32][]commerce.Offer{}, TradeQuotations: map[[2]uint32]commerce.TradeQuotation{{2010, 2151}: {Base: 1.1, Lower: 1.1, Upper: 1.1, BaseStock: 50000, Step: 250, Stock: 50000}}}
	rt.UpdateJobExperience = func(c *enterworld.Character, delta int64) ([]wire.Frame, bool) {
		c.Job.Exp += uint32(delta)
		return nil, true
	}
	if _, err := enterworld.BuildCOSRecord(c.ActiveCOS, refs.characters[c.ActiveCOS.Codename], refs); err != nil {
		t.Fatal(err)
	}
	before := authority.EconomyCounters()
	goldBefore := goldOf(c)
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeCosShopSell, CosGID: c.ActiveCOS.GID, NpcGID: 17, SourceSlot: 0, Quantity: 3})
	after := authority.EconomyCounters()
	credit := goldOf(c) - goldBefore
	if credit <= 0 {
		t.Fatalf("the sale credited no gold (before %d, after %d)", goldBefore, goldOf(c))
	}
	if len(c.ActiveCOS.Container.Rows) != 0 {
		t.Fatalf("the sold goods stayed in the transport: %+v", c.ActiveCOS.Container.Rows)
	}
	if got := after["gold.trade_sale_credit.gain"] - before["gold.trade_sale_credit.gain"]; got != uint64(credit) {
		t.Fatalf("trade_sale_credit gained %d, want the sale's credit %d", got, credit)
	}
	if got := after["gold.npc_sale.gain"] - before["gold.npc_sale.gain"]; got != 0 {
		t.Fatalf("the caravan sale also wrote npc_sale (%d): it is not an ordinary shop sale", got)
	}
	// Trade goods are not merchant items: nothing enters the
	// per-item buckets (#3307).
	if itemHours, err := authority.EconomyCharacterItemHours(c.ID, 0); err != nil || len(itemHours) != 0 {
		t.Fatalf("a caravan sale entered the item buckets: %+v (%v)", itemHours, err)
	}
}
