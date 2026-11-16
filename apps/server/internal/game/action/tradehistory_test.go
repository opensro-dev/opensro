/*
===========================================================================

tradehistory_test.go - a committed exchange or stall sale writes both legs

The trade drives the real swap through a real authority store, so each
committed trade's two rows reach the trade_history table through the same
door the gold and the items move through. A cancelled exchange writes none.

===========================================================================
*/
package action

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/stall"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// tradeHistoryTime is the fake clock's start: every trade in these tests
// commits at it, so the rows carry it as their time.
const tradeHistoryTime = 1_000_000

/*
================
storeTradeFixture

Two players of two accounts created in a real authority store, wired the way
wiring_authority.go wires the doors: the character doors, the read door and
the trade note. The first player carries the tradable sword in slot 20, and
the store's clock is the fake clock, so a row's time is known.
================
*/
func storeTradeFixture(t *testing.T) (*Runtime, *store.Store, *enterworld.Character, *enterworld.Character) {
	t.Helper()
	clock := &fakeClock{now: time.UnixMilli(tradeHistoryTime)}
	authority, err := store.Open(t.TempDir(), store.Options{DefaultSkills: doorSkillSeeder, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(authority.Close)
	a, b := testCharacter(), testCharacter()
	b.ID, b.Name, b.MissionInventory = 4, "peer", nil
	if err := authority.CreateCharacter(testDivision, "acct-a", a); err != nil {
		t.Fatal(err)
	}
	if err := authority.CreateCharacter(testDivision, "acct-b", b); err != nil {
		t.Fatal(err)
	}
	items := testItems()
	sword := *items["ITEM_CH_SWORD_01_A_RARE"]
	sword.NativeFields = sword.NativeFields.With("canTrade", 1)
	items["ITEM_CH_SWORD_01_A_RARE"] = &sword
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {a, b}},
		Items:      items,
		NpcSpawns:  enterworld.NpcSpawnConfig{Roster: simulation.DefaultNpcRoster()},
	}
	deps.UpdateCharacters = authority.UpdateCharacters
	deps.ReadCharacter = func(_ string, read func()) { authority.ReadState(read) }
	deps.TradeNote = authority.NoteTrade
	rt := NewRuntime(deps, nil)
	rt.Now = clock.Now
	rt.PushCharacterFrames = func(string, string, []wire.Frame) {}
	rt.PushDivisionPeerFrames = func(string, string, []wire.Frame) {}
	return rt, authority, a, b
}

/*
================
tradeHistoryPage

The account's trades, newest first, or the test fails.
================
*/
func tradeHistoryPage(t *testing.T, authority *store.Store, accountID string) []domain.TradeEntry {
	t.Helper()
	page, err := authority.TradeHistory(testDivision, accountID, "", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	return page.Entries
}

/*
================
TestExchangeRecordsBothLegs

A committed exchange writes one row for each party: the requester gave the
sword and paid nothing, received 1000 gold; the partner gave 1000 gold and
received the sword. A cancelled exchange, which never commits, writes nothing.
================
*/
func TestExchangeRecordsBothLegs(t *testing.T) {
	rt, authority, a, b := storeTradeFixture(t)
	rt.HandleExchangeRequest(testDivision, a, wire.NewWriter(4).U32(enterworld.ObjectIDForCharacter(b)).Payload())
	rt.ExchangeConsent().ApplyConsent(nil, testDivision, b, 1, 1)
	rt.HandleItemMove(testDivision, a, []byte{wire.MoveTypeExchangePut, 20})
	rt.HandleItemMove(testDivision, b, wire.NewWriter(5).U8(wire.MoveTypeExchangeGold).U32(1000).Payload())
	rt.HandleExchangeConfirm(testDivision, a, nil)
	rt.HandleExchangeConfirm(testDivision, b, nil)
	rt.HandleExchangeApprove(testDivision, a, nil)
	rt.HandleExchangeApprove(testDivision, b, nil)
	if goldOf(a) != 6000 || goldOf(b) != 4000 {
		t.Fatalf("the exchange did not commit: gold %d / %d", goldOf(a), goldOf(b))
	}

	mine := tradeHistoryPage(t, authority, "acct-a")
	theirs := tradeHistoryPage(t, authority, "acct-b")
	if len(mine) != 1 || len(theirs) != 1 {
		t.Fatalf("rows per account = %d / %d, want one each", len(mine), len(theirs))
	}
	first, second := mine[0].Record, theirs[0].Record
	if first.Kind != domain.TradeKindExchange || first.CharName != a.Name || first.OtherName != b.Name ||
		first.GoldPaid != 0 || first.GoldReceived != 1000 || first.Commission != 0 ||
		len(first.ItemsGiven) != 1 || first.ItemsGiven[0] != (domain.TradeItem{RefObjID: 11459, Quantity: 1}) ||
		len(first.ItemsReceived) != 0 {
		t.Fatalf("the requester's row = %+v", first)
	}
	if second.Kind != domain.TradeKindExchange || second.CharName != b.Name || second.OtherName != a.Name ||
		second.GoldPaid != 1000 || second.GoldReceived != 0 || second.Commission != 0 ||
		len(second.ItemsGiven) != 0 || len(second.ItemsReceived) != 1 ||
		second.ItemsReceived[0] != (domain.TradeItem{RefObjID: 11459, Quantity: 1}) {
		t.Fatalf("the partner's row = %+v", second)
	}
	if first.AtMs != tradeHistoryTime || second.AtMs != tradeHistoryTime {
		t.Fatalf("rows dated %d / %d, want the trade's time %d", first.AtMs, second.AtMs, tradeHistoryTime)
	}

	rt.HandleExchangeRequest(testDivision, a, wire.NewWriter(4).U32(enterworld.ObjectIDForCharacter(b)).Payload())
	rt.ExchangeConsent().ApplyConsent(nil, testDivision, b, 1, 1)
	rt.HandleExchangeCancel(testDivision, a, nil)
	if got := tradeHistoryPage(t, authority, "acct-a"); len(got) != 1 {
		t.Fatalf("a cancelled exchange wrote rows: %d for the requester", len(got))
	}
}

/*
================
TestStallBuyRecordsBothLegs

A visitor's purchase from a direct stall writes the owner's sale and the
buyer's purchase: the buyer paid the price and received the stack, the owner
gave the stack and received the price, with no commission on a direct sale.
================
*/
func TestStallBuyRecordsBothLegs(t *testing.T) {
	rt, authority, owner, buyer := storeStallFixture(t)
	title := wire.NewWriter(16).WStr("Potions").Payload()
	if out := rt.HandleStallCreate(testDivision, owner, title); !bytes.Equal(out.Frames[0].Payload, []byte{1}) {
		t.Fatalf("create %+v", out.Frames)
	}
	rt.HandleStallEdit(testDivision, owner, stallEdit(wire.StallEditAdd, 0, 13, 5, 0, 0x2c, 1, 0, 0, 0, 0, 0, 0, 0))
	rt.HandleStallEdit(testDivision, owner, stallEdit(wire.StallEditOpen, 1, 1))
	rt.HandleStallVisit(testDivision, buyer, wire.NewWriter(4).U32(enterworld.ObjectIDForCharacter(owner)).Payload())
	if out := rt.HandleStallBuy(testDivision, buyer, []byte{0}); !bytes.Equal(out.Frames[0].Payload, []byte{1, 0}) {
		t.Fatalf("buy %+v", out.Frames)
	}
	if goldOf(buyer) != 4700 || goldOf(owner) != 5300 {
		t.Fatalf("gold %d / %d", goldOf(buyer), goldOf(owner))
	}
	sold := tradeHistoryPage(t, authority, "acct-a")
	bought := tradeHistoryPage(t, authority, "acct-b")
	if len(sold) != 1 || len(bought) != 1 {
		t.Fatalf("rows per account = %d / %d, want one each", len(sold), len(bought))
	}
	seller, purchaser := sold[0].Record, bought[0].Record
	if seller.Kind != domain.TradeKindStallSell || seller.GoldReceived != 300 || seller.GoldPaid != 0 ||
		seller.Commission != 0 || len(seller.ItemsGiven) != 1 ||
		seller.ItemsGiven[0] != (domain.TradeItem{RefObjID: 3630, Quantity: 5}) || len(seller.ItemsReceived) != 0 {
		t.Fatalf("the owner's row = %+v", seller)
	}
	if purchaser.Kind != domain.TradeKindStallBuy || purchaser.GoldPaid != 300 || purchaser.GoldReceived != 0 ||
		purchaser.Commission != 0 || len(purchaser.ItemsReceived) != 1 ||
		purchaser.ItemsReceived[0] != (domain.TradeItem{RefObjID: 3630, Quantity: 5}) || len(purchaser.ItemsGiven) != 0 {
		t.Fatalf("the buyer's row = %+v", purchaser)
	}
	if purchaser.OtherName != owner.Name || seller.OtherName != buyer.Name {
		t.Fatalf("the rows name %q and %q, want each other", purchaser.OtherName, seller.OtherName)
	}
}

/*
================
TestStallTradeRecordsNameTheNetworkKinds

A network sale names the network kinds, and the seller's row carries the
commission it paid out of the price; the buyer pays the whole price.
================
*/
func TestStallTradeRecordsNameTheNetworkKinds(t *testing.T) {
	owner, buyer := testCharacter(), testCharacter()
	owner.AccountID, buyer.AccountID = "acct-owner", "acct-buyer"
	offer := stall.Slot{BagSlot: 13, RefObjID: 3630, Quantity: 5, Price: 1000}
	commission := stallCommission(offer.Price)
	records := stallTradeRecords(testDivision, owner, buyer, offer, commission, true)
	bought, sold := records[0], records[1]
	if bought.Kind != domain.TradeKindStallNetworkBuy || bought.GoldPaid != 1000 || bought.Commission != 0 {
		t.Fatalf("the network buyer's row = %+v", bought)
	}
	if sold.Kind != domain.TradeKindStallNetworkSell || sold.GoldReceived != 1000-commission || sold.Commission != commission {
		t.Fatalf("the network seller's row = %+v (commission %d)", sold, commission)
	}
	direct := stallTradeRecords(testDivision, owner, buyer, offer, 0, false)
	if direct[0].Kind != domain.TradeKindStallBuy || direct[1].Kind != domain.TradeKindStallSell || direct[1].GoldReceived != 1000 {
		t.Fatalf("a direct sale = %+v / %+v", direct[0], direct[1])
	}
}

/*
================
storeStallFixture

The owner and the buyer of a stall, created in a real authority store and
wired as storeTradeFixture wires them. The owner carries 20 potions in slot 13,
and both stand in the town a stall needs.
================
*/
func storeStallFixture(t *testing.T) (*Runtime, *store.Store, *enterworld.Character, *enterworld.Character) {
	t.Helper()
	rt, authority, owner, buyer := storeTradeFixture(t)
	owner.MissionInventory = append(owner.MissionInventory, enterworld.InventoryRow{
		Slot: 13, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 20,
	})
	items := rt.deps.(*enterworld.Deps).Items.(staticItemSource)
	potion := *items["ITEM_ETC_HP_POTION_01"]
	potion.NativeFields = potion.NativeFields.With("canTrade", 1)
	items["ITEM_ETC_HP_POTION_01"] = &potion
	rt.StallCategories = map[[4]int64]uint32{{3, 3, 1, 1}: 40}
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
	return rt, authority, owner, buyer
}
