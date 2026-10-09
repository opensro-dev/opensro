/*
===========================================================================

tradesale_test.go - live trader admission, quotation and atomic settlement

===========================================================================
*/
package action

import (
	"encoding/json"
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
traderFixture
================
*/
func traderFixture(t *testing.T, thief bool) (*Runtime, *enterworld.Character) {
	t.Helper()
	c := testCharacter()
	role := domain.JobTrader
	if thief {
		role = domain.JobThief
	}
	c.Job = domain.CharacterJob{Type: role, Grade: 1, Alias: "Trader"}
	ref := &enterworld.ItemRef{RefObjID: 2151, Codename: "ITEM_ETC_TRADE_WC_01", TypeIDs: [4]int64{3, 3, 8, 1}, NativeFields: enterworld.NewNativeFields(map[string]float64{"price": 101, "sellPrice": 50, "maxStack": 40, "canSell": 1})}
	refs := testCosSource(testItems())
	refs.staticItemSource[ref.Codename] = ref
	c.MissionInventory = []domain.InventoryRow{{Slot: 8, RefObjID: 200, Codename: "SUIT", TypeFlags: wire.PackTypeFlags(3, 1, 7, role), StackCount: 1}, {Slot: 13, RefObjID: 2151, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 3, VarianceBits: "0", TradeOwner: c.Job.Alias}}
	rt, _ := newTestRuntime(c, refs)
	rt.NpcSpawn.Enabled = true
	flags := simulation.NpcTalkFlagSpecialTrade
	if thief {
		flags |= simulation.NpcTalkFlagThiefBuy
	}
	rt.NpcRoster = []simulation.NpcDef{{ObjectID: 17, RefObjID: 2010, Codename: "NPC_WC_SPECIAL", TalkFlags: flags, AuthoredSpawn: true, Spawn: simulation.SeedWorldState(c).Spawn, NpcTalkStoreGroups: []simulation.NpcTalkStoreGroup{{Tabs: []simulation.NpcTalkStoreTab{{TabID: 1}}}}}}
	rt.Selected.Set(testDivision, c.Name, 17)
	rt.Selected.OpenFunction(testDivision, c.Name, 17)
	rt.Commerce = &commerce.Catalog{Tabs: map[int32][]commerce.Offer{}, TradeQuotations: map[[2]uint32]commerce.TradeQuotation{{2010, 2151}: {Base: 1.1, Lower: 1.1, Upper: 1.1, BaseStock: 50000, Step: 250, Stock: 50000}}}
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &domain.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 100, Summoned: true, Container: &domain.COSContainer{Capacity: 4}}
	rt.UpdateJobExperience = func(c *enterworld.Character, delta int64) ([]wire.Frame, bool) {
		c.Job.Exp += uint32(delta)
		return nil, true
	}
	return rt, c
}

/*
================
TestTraderQuoteAndPartialSaleUseNativeProfitAndTax
================
*/
func TestTraderQuoteAndPartialSaleUseNativeProfitAndTax(t *testing.T) {
	rt, c := traderFixture(t, false)
	pool := domain.TradeRewardPool{}
	rt.deps.(*enterworld.Deps).UpdateTrade = func(_ []*enterworld.Character, _ string, update func(*domain.TradeRewardPool) bool) bool {
		return update(&pool)
	}
	bindMerchantTax(t, rt, 2010, 20, 0)
	var quote shopProjection
	if err := json.Unmarshal(rt.shopCatalog(testDivision, c, 17).Payload, &quote); err != nil {
		t.Fatal(err)
	}
	if len(quote.SaleQuotes) != 1 || !reflect.DeepEqual(quote.SaleQuotes[0].Totals, []string{"89", "178", "267"}) {
		t.Fatalf("quote %+v", quote.SaleQuotes)
	}
	out := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopSell, NpcGID: 17, SourceSlot: 13, Quantity: 2})
	if goldOf(c) != 5178 || c.Job.Exp != 10 || c.Job.WeeklyReward != 17 || c.MissionInventory[1].StackCount != 1 || len(c.Buyback) != 0 {
		t.Fatalf("sale %+v character %+v", out, c)
	}
	if pool != (domain.TradeRewardPool{Hunters: 17}) {
		t.Fatalf("global pool %+v", pool)
	}
}

/*
================
TestThiefQuoteRoundsTheWholeStolenQuantity
================
*/
func TestThiefQuoteRoundsTheWholeStolenQuantity(t *testing.T) {
	rt, c := traderFixture(t, true)
	pool := domain.TradeRewardPool{}
	rt.deps.(*enterworld.Deps).UpdateTrade = func(_ []*enterworld.Character, _ string, update func(*domain.TradeRewardPool) bool) bool {
		return update(&pool)
	}
	c.MissionInventory[1].TradeOwner = "Victim"
	row := c.MissionInventory[1]
	row.Slot = 0
	c.ActiveCOS.Container.Rows = []domain.InventoryRow{row}
	c.MissionInventory = c.MissionInventory[:1]
	source := rt.deps.ItemReferences().(cosTestItemSource)
	if _, err := enterworld.BuildCOSRecord(c.ActiveCOS, source.characters[c.ActiveCOS.Codename], source); err != nil {
		t.Fatal(err)
	}
	var quote shopProjection
	if err := json.Unmarshal(rt.shopCatalog(testDivision, c, 17).Payload, &quote); err != nil {
		t.Fatal(err)
	}
	if len(quote.SaleQuotes) != 1 || quote.SaleQuotes[0].CosGID != c.ActiveCOS.GID || !reflect.DeepEqual(quote.SaleQuotes[0].Totals, []string{"75", "151", "227"}) {
		t.Fatalf("quote %+v", quote.SaleQuotes)
	}
	result := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeCosShopSell, CosGID: c.ActiveCOS.GID, NpcGID: 17, SourceSlot: 0, Quantity: 3})
	if goldOf(c) != 5227 || c.Job.Exp != 121 || len(c.ActiveCOS.Container.Rows) != 0 || len(c.Buyback) != 0 {
		t.Fatalf("thief sale gold=%d exp=%d rows=%+v response=%+v", goldOf(c), c.Job.Exp, c.ActiveCOS.Container.Rows, result)
	}
	if pool != (domain.TradeRewardPool{Thieves: 22}) {
		t.Fatalf("global pool %+v", pool)
	}
}

/*
================
TestTraderRefusalsLeaveEveryAuthorityUnchanged
================
*/
func TestTraderRefusalsLeaveEveryAuthorityUnchanged(t *testing.T) {
	for _, reason := range []string{"owner", "job", "merchant", "quest", "vehicle", "pet class", "quantity", "quote"} {
		t.Run(reason, func(t *testing.T) {
			rt, c := traderFixture(t, false)
			q := wire.ItemMoveRequest{MovementType: wire.MoveTypeShopSell, NpcGID: 17, SourceSlot: 13, Quantity: 1}
			switch reason {
			case "owner":
				c.MissionInventory[1].TradeOwner = "Foreign"
			case "job":
				c.MissionInventory[0].TypeFlags = wire.PackTypeFlags(3, 1, 7, domain.JobHunter)
			case "merchant":
				rt.NpcRoster[0].TalkFlags = simulation.NpcTalkFlagShop
			case "quest":
				rt.NpcRoster[0].Codename = "NPC_CH_SPECIAL2"
			case "vehicle":
				c.ActiveCOS.Summoned = false
			case "pet class":
				c.ActiveCOS.RefObjID = 999999
			case "quantity":
				q.Quantity = 4
			case "quote":
				rt.Commerce.TradeQuotations = nil
			}
			before := c.Snapshot()
			result := trade(t, rt, c, q)
			if len(result.Frames) != 1 || result.Frames[0].Payload[0] != 2 || !reflect.DeepEqual(before, c.Snapshot()) {
				t.Fatalf("refusal mutated authority: %+v", result)
			}
		})
	}
}

/*
================
TestTraderPartySettlementFiltersRecipientsAndCommitsTogether
================
*/
func TestTraderPartySettlementFiltersRecipientsAndCommitsTogether(t *testing.T) {
	rt, c := traderFixture(t, false)
	hunter := c.Snapshot()
	hunter.ID, hunter.Name = c.ID+1, "Escort"
	hunter.CurrentHP = testInt64(100)
	setGold(hunter, 5000)
	hunter.Job = domain.CharacterJob{Type: domain.JobHunter, Grade: 1, Alias: "Escort"}
	hunter.MissionInventory = []domain.InventoryRow{{Slot: 8, TypeFlags: wire.PackTypeFlags(3, 1, 7, domain.JobHunter), StackCount: 1}}
	far := hunter.Snapshot()
	far.ID, far.Name = c.ID+2, "Far"
	setGold(far, 5000)
	moveCharacter(rt, far, c, 1001)
	fixtureCharacters(rt.deps.(*enterworld.Deps).Characters)[testDivision] = []*enterworld.Character{c, hunter, far}
	far.Job = hunter.Job
	far.MissionInventory = append([]domain.InventoryRow(nil), hunter.MissionInventory...)
	setParty(rt, c, hunter, hunter, far)
	deps := rt.deps.(*enterworld.Deps)
	calls := 0
	deps.UpdateCharacters = func(members []*enterworld.Character, label string, update func() bool) bool {
		calls++
		if len(members) != 3 || label != "shop-transaction" {
			t.Fatalf("transaction roster %+v %q", members, label)
		}
		return update()
	}
	hunterGold, farGold := goldOf(hunter), goldOf(far)
	var quote shopProjection
	if err := json.Unmarshal(rt.shopCatalog(testDivision, c, 17).Payload, &quote); err != nil {
		t.Fatal(err)
	}
	if quote.SaleQuotes[0].Totals[2] != "327" {
		t.Fatalf("party quote %+v", quote.SaleQuotes)
	}
	rt.RewardActorPresent = func(_ string, name string) bool { return name != hunter.Name }
	var offline shopProjection
	if err := json.Unmarshal(rt.shopCatalog(testDivision, c, 17).Payload, &offline); err != nil {
		t.Fatal(err)
	}
	if offline.SaleQuotes[0].Totals[2] != "333" {
		t.Fatalf("offline escort influenced quote %+v", offline.SaleQuotes)
	}
	rt.RewardActorPresent = nil
	result := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopSell, NpcGID: 17, SourceSlot: 13, Quantity: 3})
	if calls != 1 || goldOf(c) != 5327 || goldOf(hunter) != hunterGold+6 || hunter.Job.Exp != 6 || goldOf(far) != farGold || far.Job.Exp != 0 || len(result.Recipients) != 1 {
		t.Fatalf("settlement calls=%d actor=%d hunter=%d/%d far=%d/%d recipients=%+v", calls, goldOf(c), goldOf(hunter), hunter.Job.Exp, goldOf(far), far.Job.Exp, result.Recipients)
	}
}

/*
================
TestTraderOriginSaleAndCompletedQuest
================
*/
func TestTraderOriginSaleAndCompletedQuest(t *testing.T) {
	rt, c := traderFixture(t, false)
	ref, _ := rt.deps.ItemReferences().ItemRefByCodename(c.MissionInventory[1].Codename)
	rt.Commerce.Tabs[1] = []commerce.Offer{{Ref: ref, Stack: 40}}
	rt.NpcRoster[0].Codename = "NPC_CH_SPECIAL2"
	rt.Commerce.TradeQuests = map[string]uint32{"QNO_TRADE_CH_SPECIAL2_1": 91}
	c.CompletedQuestIds = []uint32{91}
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopSell, NpcGID: 17, SourceSlot: 13, Quantity: 3})
	if goldOf(c) != 5150 || c.Job.Exp != 0 || c.Job.WeeklyReward != 0 || len(c.Buyback) != 0 {
		t.Fatalf("origin settlement %+v", c)
	}
}

/*
================
TestTradeTransactionRefusalDoesNotPublishOrRemoveCargo
================
*/
func TestTradeTransactionRefusalDoesNotPublishOrRemoveCargo(t *testing.T) {
	rt, c := traderFixture(t, true)
	before := c.Snapshot()
	rt.deps.(*enterworld.Deps).UpdateCharacters = func([]*enterworld.Character, string, func() bool) bool { return false }
	result := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopSell, NpcGID: 17, SourceSlot: 13, Quantity: 3})
	if !reflect.DeepEqual(before, c.Snapshot()) || len(result.Recipients) != 0 || len(result.Frames) != 1 || result.Frames[0].Payload[0] != 2 {
		t.Fatalf("refusal %+v", result)
	}
}

/*
================
TestTradePurchaseRequiresMerchantJobQuestAndTransport
================
*/
func TestTradePurchaseRequiresMerchantJobQuestAndTransport(t *testing.T) {
	for _, reason := range []string{"merchant", "job", "quest", "vehicle"} {
		t.Run(reason, func(t *testing.T) {
			rt, c := traderFixture(t, false)
			ref, _ := rt.deps.ItemReferences().ItemRefByCodename(c.MissionInventory[1].Codename)
			rt.Commerce.Tabs[1] = []commerce.Offer{{Slot: 2, Ref: ref, Price: 60, Stack: 40}}
			switch reason {
			case "merchant":
				rt.NpcRoster[0].TalkFlags = simulation.NpcTalkFlagShop
			case "job":
				c.MissionInventory = c.MissionInventory[1:]
			case "quest":
				rt.NpcRoster[0].Codename = "NPC_CH_SPECIAL2"
			case "vehicle":
				c.ActiveCOS = nil
			}
			before := c.Snapshot()
			result := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopBuy, NpcGID: 17, ShopSlot: 2, Quantity: 3})
			if !reflect.DeepEqual(before, c.Snapshot()) || len(result.Frames) != 1 || result.Frames[0].Payload[0] != 2 {
				t.Fatalf("purchase bypassed %s: %+v", reason, result)
			}
		})
	}
}
