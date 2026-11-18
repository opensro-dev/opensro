/*
===========================================================================

market_test.go - the consignment market's in-game lane: admission, the
bag rules and the frames an online character's listing or buy sends

===========================================================================
*/
package action

import (
	"encoding/json"
	"testing"

	"opensro.online/server/internal/agent/publicstats"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
fakeMarketDesk

Records what reached the desk. GameList takes the row out of the bag as
the store does; GameBuy pays a gold listing from the character.
================
*/
type fakeMarketDesk struct {
	listed   []int64
	bought   []int64
	searched []publicstats.MarketSearch
	price    int64
	currency string
	failWith error
}

func (f *fakeMarketDesk) Categories() []publicstats.MarketCategory {
	return []publicstats.MarketCategory{{Slug: "weapon", Name: "Weapons"}}
}

func (f *fakeMarketDesk) Search(q publicstats.MarketSearch) (publicstats.MarketListingsResponse, error) {
	f.searched = append(f.searched, q)
	row := testCharacter().MissionInventory[0]
	view := publicstats.MarketListingView{ID: 7, Version: 3, Currency: domain.MarketGold, Price: 900, Row: row,
		Item: publicstats.MarketItem{Category: "weapon", Degree: 1}}
	return publicstats.MarketListingsResponse{Listings: []publicstats.MarketListingView{view}, Page: q.Page, Pages: 1, Total: 1}, nil
}

func (f *fakeMarketDesk) Account(string) (publicstats.OwnMarketResponse, error) {
	return publicstats.OwnMarketResponse{Silk: 9}, nil
}

func (f *fakeMarketDesk) GameList(c *domain.Character, slot int64, _ string, _ int64) (publicstats.MarketListingView, error) {
	if f.failWith != nil {
		return publicstats.MarketListingView{}, f.failWith
	}
	f.listed = append(f.listed, slot)
	rest := c.MissionInventory[:0]
	var listed domain.InventoryRow
	for _, row := range c.MissionInventory {
		if row.Slot != slot {
			rest = append(rest, row)
		} else {
			listed = row
		}
	}
	c.MissionInventory = rest
	return publicstats.MarketListingView{ID: 11, Row: listed}, nil
}

func (f *fakeMarketDesk) GameBuy(c *domain.Character, listing, _ int64) (publicstats.MarketSaleView, error) {
	if f.failWith != nil {
		return publicstats.MarketSaleView{}, f.failWith
	}
	f.bought = append(f.bought, listing)
	if f.currency == domain.MarketGold {
		gold := *c.Gold - f.price
		c.Gold = &gold
	}
	return publicstats.MarketSaleView{ListingID: listing, Currency: f.currency, Price: f.price}, nil
}

func (f *fakeMarketDesk) Cancel(string, int64) error   { return f.failWith }
func (f *fakeMarketDesk) TakeBack(string, int64) error { return f.failWith }

/*
================
marketFixture
================
*/
func marketFixture() (*Runtime, *enterworld.Character, *fakeMarketDesk) {
	c := testCharacter()
	c.AccountID = "alpha"
	rt, _ := newTestRuntime(c, testItems())
	desk := &fakeMarketDesk{}
	rt.Market = desk
	return rt, c, desk
}

/*
================
decodeMarketFrames

The lane's answer among a result's frames, and the opcodes in order.
================
*/
func decodeMarketFrames(t *testing.T, result OpResult) (marketAnswer, []uint16) {
	t.Helper()
	var answer marketAnswer
	found := false
	opcodes := []uint16{}
	for _, frame := range result.Frames {
		opcodes = append(opcodes, frame.Opcode)
		if frame.Opcode == opMarket {
			if err := json.Unmarshal(frame.Payload, &answer); err != nil {
				t.Fatal(err)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("no market answer in %v", opcodes)
	}
	return answer, opcodes
}

/*
================
TestMarketListEmptiesTheBagSlot

A listing from a bag slot answers ok, then empties the slot on the
client with the native drop result; the desk took the row.
================
*/
func TestMarketListEmptiesTheBagSlot(t *testing.T) {
	rt, c, desk := marketFixture()
	result := rt.HandleMarket(testDivision, c, []byte(`{"op":"list","slot":20,"currency":"gold","price":900}`))
	answer, opcodes := decodeMarketFrames(t, result)
	if !answer.OK || answer.Op != marketOpList || answer.Listing == nil || answer.Listing.ID != 11 ||
		answer.Listing.Item.Ref != 11459 {
		t.Fatalf("listing answer: %+v", answer)
	}
	if len(desk.listed) != 1 || desk.listed[0] != 20 || len(c.MissionInventory) != 0 {
		t.Fatalf("listed %v, bag %+v", desk.listed, c.MissionInventory)
	}
	if opcodes[0] != wire.OpItemMoveResponse || string(result.Frames[0].Payload) != string(wire.EncodeGroundDropResult(20)) {
		t.Fatalf("frames %v, first %x", opcodes, result.Frames[0].Payload)
	}
}

/*
================
TestMarketListRefusesSocketsAndBusyBags

An equipment socket or a slot past the bag is no listing; a stall's
keeper and an exchange's trader keep their bag. None reaches the desk.
================
*/
func TestMarketListRefusesSocketsAndBusyBags(t *testing.T) {
	for _, tc := range []struct {
		name  string
		slot  string
		enter func(rt *Runtime, c *enterworld.Character)
		want  string
	}{
		{name: "equipment socket", slot: "6", want: marketRefusedSlot},
		{name: "past the bag", slot: "250", want: marketRefusedSlot},
		{name: "stall keeper", slot: "20", want: marketRefusedBusy, enter: func(rt *Runtime, c *enterworld.Character) {
			if err := rt.Stalls.Open(testDivision, c.Name, "stall", 0); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "exchange", slot: "20", want: marketRefusedBusy, enter: func(rt *Runtime, c *enterworld.Character) {
			rt.Exchanges.Open(testDivision, c.Name, "Partner")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, c, desk := marketFixture()
			if tc.enter != nil {
				tc.enter(rt, c)
			}
			result := rt.HandleMarket(testDivision, c, []byte(`{"op":"list","slot":`+tc.slot+`,"currency":"gold","price":900}`))
			answer, opcodes := decodeMarketFrames(t, result)
			if answer.OK || answer.Error != tc.want || len(opcodes) != 1 || len(desk.listed) != 0 {
				t.Fatalf("answer %+v, frames %v, listed %v", answer, opcodes, desk.listed)
			}
		})
	}
}

/*
================
TestMarketGoldBuySendsTheBalance

A gold buy answers the sale and then the character's new balance; a
Silk buy changes no gold and sends none. A busy bag cannot pay.
================
*/
func TestMarketGoldBuySendsTheBalance(t *testing.T) {
	rt, c, desk := marketFixture()
	desk.currency, desk.price = domain.MarketGold, 1200
	answer, opcodes := decodeMarketFrames(t, rt.HandleMarket(testDivision, c, []byte(`{"op":"buy","listing":5,"version":2}`)))
	if !answer.OK || answer.Sale == nil || answer.Sale.ListingID != 5 || len(opcodes) != 2 || opcodes[1] != wire.OpPointsUpdate {
		t.Fatalf("gold buy: %+v, frames %v", answer, opcodes)
	}
	if *c.Gold != 3800 {
		t.Fatalf("gold = %d", *c.Gold)
	}

	desk.currency = domain.MarketSilk
	answer, opcodes = decodeMarketFrames(t, rt.HandleMarket(testDivision, c, []byte(`{"op":"buy","listing":6,"version":1}`)))
	if !answer.OK || len(opcodes) != 1 {
		t.Fatalf("silk buy: %+v, frames %v", answer, opcodes)
	}

	rt.Exchanges.Open(testDivision, c.Name, "Partner")
	answer, _ = decodeMarketFrames(t, rt.HandleMarket(testDivision, c, []byte(`{"op":"buy","listing":7,"version":1}`)))
	if answer.OK || answer.Error != marketRefusedBusy || len(desk.bought) != 2 {
		t.Fatalf("a trader's buy: %+v, bought %v", answer, desk.bought)
	}
}

/*
================
TestMarketRefusalsNameTheStoreCode

The store's refusal reaches the window by the site's code, so both
front ends share one vocabulary.
================
*/
func TestMarketRefusalsNameTheStoreCode(t *testing.T) {
	rt, c, desk := marketFixture()
	desk.failWith = domain.ErrMarketFunds
	answer, _ := decodeMarketFrames(t, rt.HandleMarket(testDivision, c, []byte(`{"op":"buy","listing":5,"version":2}`)))
	if answer.OK || answer.Error != "funds" {
		t.Fatalf("funds refusal: %+v", answer)
	}
	desk.failWith = domain.ErrMarketNotHeld
	answer, _ = decodeMarketFrames(t, rt.HandleMarket(testDivision, c, []byte(`{"op":"takeback","listing":5}`)))
	if answer.OK || answer.Error != "not-held" {
		t.Fatalf("take-back refusal: %+v", answer)
	}
}

/*
================
TestMarketSearchAndAdmission

A search answers its page and the categories; forged searches, unknown
fields and ops, and a market that is off refuse without a read.
================
*/
func TestMarketSearchAndAdmission(t *testing.T) {
	rt, c, desk := marketFixture()
	answer, _ := decodeMarketFrames(t, rt.HandleMarket(testDivision, c, []byte(`{"op":"search","category":"weapon","q":" sword "}`)))
	if !answer.OK || answer.Search == nil || len(answer.Search.Rows) != 1 || len(answer.Categories) != 1 {
		t.Fatalf("search answer: %+v", answer)
	}
	// The row carries the item as a preview the window can draw, and the
	// references frame comes first.
	if row := answer.Search.Rows[0]; row.ID != 7 || row.Category != "weapon" || row.Item.Ref != 11459 || row.Item.Slot != 0 ||
		len(row.Item.Body) == 0 {
		t.Fatalf("search row: %+v", row)
	}
	if _, opcodes := decodeMarketFrames(t, rt.HandleMarket(testDivision, c, []byte(`{"op":"search"}`))); opcodes[0] != opCommerceItemReferences {
		t.Fatalf("search frames: %v", opcodes)
	}
	if q := desk.searched[0]; q.Page != 1 || q.Q != "sword" || q.Category != "weapon" {
		t.Fatalf("search = %+v", q)
	}
	for name, payload := range map[string]string{
		"unknown category":  `{"op":"search","category":"wands"}`,
		"page past the end": `{"op":"search","page":101}`,
		"degree too high":   `{"op":"search","degree":12}`,
		"unknown currency":  `{"op":"search","currency":"points"}`,
		"unknown sort":      `{"op":"search","sort":"cheapest"}`,
		"unknown field":     `{"op":"search","account":"victim"}`,
		"unknown op":        `{"op":"steal"}`,
		"trailing data":     `{"op":"mine"}{"op":"mine"}`,
		"empty":             ``,
	} {
		answer, _ := decodeMarketFrames(t, rt.HandleMarket(testDivision, c, []byte(payload)))
		if answer.OK || answer.Error != marketRefusedRequest {
			t.Errorf("%s: %+v", name, answer)
		}
	}
	if len(desk.searched) != 2 {
		t.Fatalf("a forged search reached the desk: %d", len(desk.searched))
	}

	rt.Market = nil
	answer, _ = decodeMarketFrames(t, rt.HandleMarket(testDivision, c, []byte(`{"op":"mine"}`)))
	if answer.OK || answer.Error != marketRefusedUnavailable {
		t.Fatalf("a market that is off: %+v", answer)
	}
}
