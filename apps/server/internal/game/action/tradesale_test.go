/*
===========================================================================

tradesale_test.go - live trader admission, quotation and atomic settlement

===========================================================================
*/
package action

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/inventory"
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
	level := minTradeBuyerLevel
	c.Level = &level
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
TestTraderSellsUnquotedForeignGoodAtBaseSellPrice

The native trade quote sells a foreign good that has no
quotation entry at the item's base sell price instead of refusing it (the
"CANNOT FIND QUOT DATA" fall-through), with zero trade
profit. Unquoted goods (such as Asia Minor's TRADE_AM_01..03) have no
_ItemQuotation rows, so a trader sells them at that fallback price and earns
no route profit, job experience or pool contribution from the sale.
================
*/
func TestTraderSellsUnquotedForeignGoodAtBaseSellPrice(t *testing.T) {
	rt, c := traderFixture(t, false)
	pool := domain.TradeRewardPool{}
	rt.deps.(*enterworld.Deps).UpdateTrade = func(_ []*enterworld.Character, _ string, update func(*domain.TradeRewardPool) bool) bool {
		return update(&pool)
	}
	// Re-dress the fixture's cargo as an unquoted Asia Minor good and
	// drop every quotation row: the sale must price from sellPrice alone.
	source := rt.deps.ItemReferences().(cosTestItemSource)
	source.staticItemSource["ITEM_ETC_TRADE_AM_01"] = &enterworld.ItemRef{RefObjID: 7582, Codename: "ITEM_ETC_TRADE_AM_01", TypeIDs: [4]int64{3, 3, 8, 1}, NativeFields: enterworld.NewNativeFields(map[string]float64{"price": 101, "sellPrice": 50, "maxStack": 40, "canSell": 1})}
	c.MissionInventory[1].RefObjID = 7582
	c.MissionInventory[1].Codename = "ITEM_ETC_TRADE_AM_01"
	rt.Commerce.TradeQuotations = map[[2]uint32]commerce.TradeQuotation{}
	bindMerchantTax(t, rt, 2010, 20, 0)
	var quote shopProjection
	if err := json.Unmarshal(rt.shopCatalog(testDivision, c, 17).Payload, &quote); err != nil {
		t.Fatal(err)
	}
	if len(quote.SaleQuotes) != 1 || !reflect.DeepEqual(quote.SaleQuotes[0].Totals, []string{"40", "80", "120"}) {
		t.Fatalf("quote %+v", quote.SaleQuotes)
	}
	out := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopSell, NpcGID: 17, SourceSlot: 13, Quantity: 2})
	if goldOf(c) != 5080 || c.Job.Exp != 0 || c.Job.WeeklyReward != 0 || c.MissionInventory[1].StackCount != 1 || len(c.Buyback) != 0 {
		t.Fatalf("sale %+v character %+v", out, c)
	}
	if pool != (domain.TradeRewardPool{}) {
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
				// A missing row is no longer a refusal (native sells at
				// the base sell price; see
				// TestTraderSellsUnquotedForeignGoodAtBaseSellPrice). A
				// variable-rate row remains invalid authority.
				rt.Commerce.TradeQuotations[[2]uint32{2010, 2151}] = commerce.TradeQuotation{Base: 1.1, Lower: 1.0, Upper: 1.2, BaseStock: 50000, Step: 250, Stock: 50000}
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
		if len(members) != 3 || label != fmt.Sprintf("trade-sale %d", c.ID) {
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

The native purchase check's order and codes: a non-special merchant, then the trader test (a suited
trader with a transport, else 0x60 even for a suited thief), level 20
(0x4E), the required quest (0xB0) and the transport's distance.
================
*/
func TestTradePurchaseRequiresMerchantJobQuestAndTransport(t *testing.T) {
	cases := []struct {
		reason string
		thief  bool
		code   uint8
	}{
		{"merchant", false, tradeErrWrongMerchant},
		{"unsuited", false, tradeErrOnlyTrader},
		{"thief", true, tradeErrOnlyTrader},
		{"vehicle", false, tradeErrOnlyTrader},
		{"level", false, tradeErrBuyerLevel},
		{"quest", false, tradeErrQuest},
	}
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			rt, c := traderFixture(t, tc.thief)
			ref, _ := rt.deps.ItemReferences().ItemRefByCodename(c.MissionInventory[1].Codename)
			rt.Commerce.Tabs[1] = []commerce.Offer{{Slot: 2, Ref: ref, Price: 60, Stack: 40}}
			switch tc.reason {
			case "merchant":
				rt.NpcRoster[0].TalkFlags = simulation.NpcTalkFlagShop
			case "unsuited":
				c.MissionInventory = c.MissionInventory[1:]
			case "vehicle":
				c.ActiveCOS = nil
			case "level":
				level := minTradeBuyerLevel - 1
				c.Level = &level
			case "quest":
				rt.NpcRoster[0].Codename = "NPC_CH_SPECIAL2"
			}
			before := c.Snapshot()
			result := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopBuy, NpcGID: 17, ShopSlot: 2, Quantity: 3})
			if !reflect.DeepEqual(before, c.Snapshot()) || len(result.Frames) != 1 || result.Frames[0].Payload[0] != 2 ||
				result.Frames[0].Payload[1] != tc.code {
				t.Fatalf("purchase bypassed %s (want code 0x%X): %+v", tc.reason, tc.code, result)
			}
		})
	}
}

/*
================
TestTradeSaleJobAndQuantityRules

Natively a sale in a hunter's suit or in no suit is refused (job states 3 and
4, code 5). Its tail answers a quantity above the held count with 0x14 and one
above the stack cap with 0x15.
================
*/
func TestTradeSaleJobAndQuantityRules(t *testing.T) {
	cases := []struct {
		reason   string
		quantity uint16
		code     uint8
	}{
		{"unsuited", 3, tradeErrJob},
		{"hunter", 3, tradeErrJob},
		{"held", 4, wire.ErrCodeInputFewerThanRemain},
		{"cap", 41, tradeErrOverStack},
	}
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			rt, c := traderFixture(t, true)
			switch tc.reason {
			case "unsuited":
				c.MissionInventory = c.MissionInventory[1:]
			case "hunter":
				c.Job.Type = domain.JobHunter
				c.MissionInventory[0].TypeFlags = wire.PackTypeFlags(3, 1, 7, domain.JobHunter)
			}
			result := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopSell, NpcGID: 17, SourceSlot: 13, Quantity: tc.quantity})
			p := result.Frames[0].Payload
			if tc.code == 0 {
				if p[0] != 1 {
					t.Fatalf("an unsuited holder could not sell: % X", p)
				}
				return
			}
			if p[0] != 2 || p[1] != tc.code {
				t.Fatalf("%s: answer % X, want code 0x%X", tc.reason, p, tc.code)
			}
		})
	}
}

/*
================
TestTransportPurchaseSendsTradeLevel

A committed purchase into the transport tells the owner its trade level
(0x352D) after the move's answer; a refused one does not.
================
*/
func TestTransportPurchaseSendsTradeLevel(t *testing.T) {
	rt, c := traderFixture(t, false)
	ref, _ := rt.deps.ItemReferences().ItemRefByCodename(c.MissionInventory[1].Codename)
	rt.Commerce.Tabs[1] = []commerce.Offer{{Slot: 2, Ref: ref, Price: 60, Stack: 40}}
	result := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeCosShopBuy, NpcGID: 17, ShopSlot: 2, Quantity: 3,
		CosGID: c.ActiveCOS.GID})
	last, committed := result.Frames[len(result.Frames)-1], false
	for _, frame := range result.Frames {
		committed = committed || frame.Opcode == wire.OpItemMoveResponse && frame.Payload[0] == 1
	}
	if !committed || last.Opcode != wire.OpTradeStarCount || len(last.Payload) != 1 {
		t.Fatalf("purchase frames %+v, want the trade level last", result.Frames)
	}
	refused := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeCosShopBuy, NpcGID: 17, ShopSlot: 9, Quantity: 3,
		CosGID: c.ActiveCOS.GID})
	for _, frame := range refused.Frames {
		if frame.Opcode == wire.OpTradeStarCount {
			t.Fatal("a refused purchase sent a trade level")
		}
	}
}

/*
================
TestCommerceDoorLabelNamesTradeSales

A sale of goods (from the bag or from a transport) commits under the
trade door the economy ledger reads as a trade payout; a purchase and an
ordinary sale stay shop transactions.
================
*/
func TestCommerceDoorLabelNamesTradeSales(t *testing.T) {
	goods := wire.PackTypeFlags(3, 3, 8, 1)
	potion := wire.PackTypeFlags(3, 3, 1, 1)
	const transportGID, petGID = 0x00C00010, 0x00C00011
	c := &enterworld.Character{ID: 42,
		MissionInventory: []enterworld.InventoryRow{
			{Slot: 13, TypeFlags: goods, StackCount: 5}, {Slot: 14, TypeFlags: potion, StackCount: 5}},
		ActiveCOS: &enterworld.CharacterCOS{GID: transportGID, Summoned: true, Container: &domain.COSContainer{
			Capacity: 4, Rows: []enterworld.InventoryRow{{Slot: 0, TypeFlags: goods, StackCount: 5}}}},
	}
	if !inventory.IsTradeGoods(goods) || inventory.IsTradeGoods(potion) {
		t.Fatal("fixture type flags do not classify as intended")
	}
	sale := fmt.Sprintf("trade-sale %d", c.ID)
	// A pickup pet rides on its summoner's bag row (domain Companions).
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 15, StackCount: 1,
		Summon: &enterworld.CharacterCOS{GID: petGID, Summoned: true, Container: &domain.COSContainer{
			Capacity: 4, Rows: []enterworld.InventoryRow{{Slot: 0, TypeFlags: potion, StackCount: 5}}}}})
	cases := []struct {
		name    string
		move    uint8
		slot    uint8
		cos     uint32
		fromCOS bool
		want    string
	}{
		{"goods from the bag", wire.MoveTypeShopSell, 13, 0, false, sale},
		{"potion from the bag", wire.MoveTypeShopSell, 14, 0, false, "shop-transaction"},
		{"goods from the transport", wire.MoveTypeShopSell, 0, transportGID, true, sale},
		{"potion from a pickup pet", wire.MoveTypeShopSell, 0, petGID, true, "shop-transaction"},
		{"a purchase", wire.MoveTypeShopBuy, 13, 0, false, "shop-transaction"},
	}
	for _, tc := range cases {
		q := wire.ItemMoveRequest{MovementType: tc.move, SourceSlot: tc.slot, CosGID: tc.cos}
		if got := commerceDoorLabel(c, q, tc.fromCOS); got != tc.want {
			t.Fatalf("%s = %q, want %q", tc.name, got, tc.want)
		}
	}
	// A thief's goods sale names its own door; the seller is still named.
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: enterworld.JobSuitSlot,
		TypeFlags: wire.PackTypeFlags(3, 1, 7, domain.JobThief), StackCount: 1})
	q := wire.ItemMoveRequest{MovementType: wire.MoveTypeShopSell, SourceSlot: 13}
	if got, want := commerceDoorLabel(c, q, false), fmt.Sprintf("thief-sale %d", c.ID); got != want {
		t.Fatalf("thief goods sale = %q, want %q", got, want)
	}
}
