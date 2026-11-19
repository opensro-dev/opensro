/*
===========================================================================

market_test.go - the consignment market's in-game lane: admission, the
bag rules, the frames a listing, buy, reclaim, offer or accept sends,
and the party notices and proceeds payments it routes

===========================================================================
*/
package action

import (
	"encoding/json"
	"testing"
	"time"

	"opensro.online/server/internal/agent/publicstats"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// marketSword is the test character's sword row (testCharacter).
const marketSwordRef = 11459

/*
================
fakeMarketDesk

Records what reached the desk and acts as the store would on the records
it is given: a listing takes the bag row, a bag delivery puts the sword
in the slot the lane chose, a gold buy or accept pays from the payer,
and a claim pays owed gold.
================
*/
type fakeMarketDesk struct {
	listed    []int64
	bought    []int64
	reclaimed []string
	accepted  []int64
	searched  []publicstats.MarketSearch
	bagSlots  []int64
	price     int64
	currency  string
	sellerID  int64
	offer     domain.MarketOffer
	owed      map[int64]int64
	failWith  error
}

func (f *fakeMarketDesk) Categories() []publicstats.MarketCategory {
	return []publicstats.MarketCategory{{Slug: "weapon", Name: "Weapons"}}
}

func (f *fakeMarketDesk) Search(q publicstats.MarketSearch) (publicstats.MarketListingsResponse, error) {
	f.searched = append(f.searched, q)
	row := testCharacter().MissionInventory[0]
	view := publicstats.MarketListingView{ID: 7, Version: 3, Currency: domain.MarketGold, Price: 900, Row: row, Seller: "Seller",
		Item: publicstats.MarketItem{Category: "weapon", Degree: 1}}
	return publicstats.MarketListingsResponse{Listings: []publicstats.MarketListingView{view}, Page: q.Page, Pages: 1, Total: 1}, nil
}

func (f *fakeMarketDesk) GameAccount(*domain.Character) (publicstats.OwnMarketResponse, error) {
	row := testCharacter().MissionInventory[0]
	return publicstats.OwnMarketResponse{Silk: 9,
		Listings:  []publicstats.MarketListingView{{ID: 1, Row: row, Seller: "asd2", Anonymous: true}},
		OffersIn:  []publicstats.MarketOfferView{{ID: 4, ListingID: 1, Row: row, Buyer: "Buyer", Price: 700}},
		OffersOut: []publicstats.MarketOfferView{{ID: 5, ListingID: 2, Row: row, Price: 300}},
	}, nil
}

func (f *fakeMarketDesk) GameList(c *domain.Character, slot int64, _ string, _ int64, anonymous bool) (publicstats.MarketListingView, error) {
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
	return publicstats.MarketListingView{ID: 11, Row: listed, Anonymous: anonymous}, nil
}

/*
================
fakeMarketDesk::deliver
================
*/
func (f *fakeMarketDesk) deliver(c *domain.Character, to string, bagSlot int64) publicstats.MarketSaleView {
	sale := publicstats.MarketSaleView{Currency: f.currency, Price: f.price, To: to, SellerChar: f.sellerID, Item: "Sword"}
	if to == domain.MarketToBag {
		row := testCharacter().MissionInventory[0]
		row.Slot = bagSlot
		c.MissionInventory = append(c.MissionInventory, row)
		sale.Slot = bagSlot
	}
	if f.currency == domain.MarketGold {
		gold := *c.Gold - f.price
		c.Gold = &gold
		f.owed[f.sellerID] += f.price
	}
	return sale
}

func (f *fakeMarketDesk) GameBuy(c *domain.Character, listing, _ int64, to string, bagSlot int64) (publicstats.MarketSaleView, error) {
	if f.failWith != nil {
		return publicstats.MarketSaleView{}, f.failWith
	}
	f.bought = append(f.bought, listing)
	f.bagSlots = append(f.bagSlots, bagSlot)
	sale := f.deliver(c, to, bagSlot)
	sale.ListingID = listing
	return sale, nil
}

func (f *fakeMarketDesk) GameReclaim(c *domain.Character, _ int64, to string, bagSlot int64) error {
	if f.failWith != nil {
		return f.failWith
	}
	f.reclaimed = append(f.reclaimed, to)
	if to == domain.MarketToBag {
		row := testCharacter().MissionInventory[0]
		row.Slot = bagSlot
		c.MissionInventory = append(c.MissionInventory, row)
	}
	return nil
}

func (f *fakeMarketDesk) GameOffer(c *domain.Character, listing, price int64, to string) (publicstats.MarketOfferView, error) {
	if f.failWith != nil {
		return publicstats.MarketOfferView{}, f.failWith
	}
	return publicstats.MarketOfferView{ID: 4, ListingID: listing, Price: price, To: to, Buyer: c.Name,
		Row: testCharacter().MissionInventory[0], SellerChar: f.sellerID, Item: publicstats.MarketItem{Name: "Sword"}}, nil
}

func (f *fakeMarketDesk) OfferBuyer(int64) (domain.MarketOffer, error) { return f.offer, f.failWith }

func (f *fakeMarketDesk) GameAccept(_ *domain.Character, offer int64, buyer *domain.Character, bagSlot int64) (publicstats.MarketSaleView, error) {
	f.accepted = append(f.accepted, offer)
	f.bagSlots = append(f.bagSlots, bagSlot)
	return f.deliver(buyer, f.offer.To, bagSlot), nil
}

func (f *fakeMarketDesk) GameReject(*domain.Character, int64) (publicstats.MarketOfferView, error) {
	return publicstats.MarketOfferView{BuyerChar: f.offer.BuyerChar, SellerChar: f.sellerID}, f.failWith
}

func (f *fakeMarketDesk) GameWithdraw(*domain.Character, int64) (publicstats.MarketOfferView, error) {
	return publicstats.MarketOfferView{BuyerChar: f.offer.BuyerChar, SellerChar: f.sellerID}, f.failWith
}

func (f *fakeMarketDesk) Owed() (map[int64]bool, error) {
	out := map[int64]bool{}
	for id, gold := range f.owed {
		out[id] = gold > 0
	}
	return out, nil
}

func (f *fakeMarketDesk) ClaimProceeds(c *domain.Character) ([]publicstats.MarketSaleView, []string, error) {
	owed := f.owed[c.ID]
	if owed == 0 {
		return nil, nil, nil
	}
	delete(f.owed, c.ID)
	gold := *c.Gold + owed
	c.Gold = &gold
	return []publicstats.MarketSaleView{{Currency: domain.MarketGold, Price: owed}}, []string{"Sword"}, nil
}

/*
================
marketFixture
================
*/
func marketFixture() (*Runtime, *enterworld.Character, *fakeMarketDesk) {
	c := testCharacter()
	c.AccountID = "alpha"
	rt, _ := newTestRuntime(c, testItems())
	desk := &fakeMarketDesk{owed: map[int64]int64{}}
	rt.Market = desk
	return rt, c, desk
}

/*
================
marketPair

Two characters of two accounts in one division, both admitted: the
actor (asd2, the test character) and the other party.
================
*/
func marketPair() (*Runtime, *enterworld.Character, *enterworld.Character, *fakeMarketDesk) {
	c := testCharacter()
	c.AccountID = "alpha"
	other := testCharacter()
	other.ID, other.Name, other.AccountID, other.MissionInventory = 8, "Other", "beta", nil
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {c, other}},
		Items:      testItems(),
		NpcSpawns:  enterworld.NpcSpawnConfig{Roster: simulation.DefaultNpcRoster()},
	}
	rt := NewRuntime(deps, nil)
	rt.Now = func() time.Time { return time.UnixMilli(1_000_000) }
	desk := &fakeMarketDesk{owed: map[int64]int64{}}
	rt.Market = desk
	for _, admitted := range []*enterworld.Character{c, other} {
		rt.characterAdmissions.Store(simulation.WorldKey(testDivision, admitted.Name),
			populationAdmission{division: testDivision, name: admitted.Name})
	}
	return rt, c, other, desk
}

/*
================
decodeMarketFrames

The lane's answer among frames, and the opcodes in order.
================
*/
func decodeMarketFrames(t *testing.T, frames []wire.Frame) (marketAnswer, []uint16) {
	t.Helper()
	var answer marketAnswer
	found := false
	opcodes := []uint16{}
	for _, frame := range frames {
		opcodes = append(opcodes, frame.Opcode)
		if frame.Opcode == opMarket && !found {
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
marketEvents

Every pushed event among frames.
================
*/
func marketEvents(t *testing.T, frames []wire.Frame) []marketEvent {
	t.Helper()
	var events []marketEvent
	for _, frame := range frames {
		var answer marketAnswer
		if frame.Opcode == opMarket && json.Unmarshal(frame.Payload, &answer) == nil && answer.Op == marketOpEvent {
			events = append(events, answer.Events...)
		}
	}
	return events
}

/*
================
TestMarketListEmptiesTheBagSlot

A listing from a bag slot answers ok with the anonymous choice, then
empties the slot on the client with the native drop result.
================
*/
func TestMarketListEmptiesTheBagSlot(t *testing.T) {
	rt, c, desk := marketFixture()
	result := rt.HandleMarket(testDivision, c, []byte(`{"op":"list","slot":20,"currency":"gold","price":900,"anonymous":true}`))
	answer, opcodes := decodeMarketFrames(t, result.Frames)
	if !answer.OK || answer.Version != marketAnswerVersion || answer.Listing == nil || answer.Listing.ID != 11 ||
		answer.Listing.Item.Ref != marketSwordRef || !answer.Listing.Anonymous {
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
			answer, opcodes := decodeMarketFrames(t, result.Frames)
			if answer.OK || answer.Error != tc.want || len(opcodes) != 1 || len(desk.listed) != 0 {
				t.Fatalf("answer %+v, frames %v, listed %v", answer, opcodes, desk.listed)
			}
		})
	}
}

/*
================
TestMarketBuyIntoTheBagOrTheWarehouse

A bag buy goes to the first free bag slot with the native pickup result,
then the balance; a warehouse buy sends no pickup. A full bag refuses
before the desk; a trader cannot buy.
================
*/
func TestMarketBuyIntoTheBagOrTheWarehouse(t *testing.T) {
	rt, c, desk := marketFixture()
	desk.currency, desk.price = domain.MarketGold, 1200
	result := rt.HandleMarket(testDivision, c, []byte(`{"op":"buy","listing":5,"version":2,"to":"bag"}`))
	answer, opcodes := decodeMarketFrames(t, result.Frames)
	if !answer.OK || answer.Sale == nil || answer.Sale.To != domain.MarketToBag {
		t.Fatalf("bag buy: %+v", answer)
	}
	// The sword holds slot 20; the first free bag slot is 13.
	if desk.bagSlots[0] != 13 || opcodes[1] != wire.OpItemMoveResponse || opcodes[len(opcodes)-1] != wire.OpPointsUpdate {
		t.Fatalf("bag slot %v, frames %v", desk.bagSlots, opcodes)
	}
	if *c.Gold != 3800 {
		t.Fatalf("gold = %d", *c.Gold)
	}

	desk.currency = domain.MarketSilk
	answer, opcodes = decodeMarketFrames(t, rt.HandleMarket(testDivision, c, []byte(`{"op":"buy","listing":6,"version":1,"to":"warehouse"}`)).Frames)
	if !answer.OK || len(opcodes) != 1 || desk.bagSlots[1] != -1 {
		t.Fatalf("warehouse buy: %+v, frames %v, slot %v", answer, opcodes, desk.bagSlots)
	}

	if answer, _ := decodeMarketFrames(t, rt.HandleMarket(testDivision, c, []byte(`{"op":"buy","listing":6,"version":1}`)).Frames); answer.OK ||
		answer.Error != marketRefusedRequest {
		t.Fatalf("a buy naming no destination: %+v", answer)
	}
	for slot := int64(0); slot < 200; slot++ {
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: slot, RefObjID: 62,
			Codename: "ITEM_ETC_GOLD_01", StackCount: 1})
	}
	if answer, _ := decodeMarketFrames(t, rt.HandleMarket(testDivision, c, []byte(`{"op":"buy","listing":7,"version":1,"to":"bag"}`)).Frames); answer.OK ||
		answer.Error != "bag-full" || len(desk.bought) != 2 {
		t.Fatalf("a full bag's buy: %+v", answer)
	}
	rt.Exchanges.Open(testDivision, c.Name, "Partner")
	if answer, _ := decodeMarketFrames(t, rt.HandleMarket(testDivision, c, []byte(`{"op":"buy","listing":7,"version":1,"to":"warehouse"}`)).Frames); answer.OK ||
		answer.Error != marketRefusedBusy {
		t.Fatalf("a trader's buy: %+v", answer)
	}
}

/*
================
TestMarketSalePaysTheSellerWhenHere

A gold sale pays a listing character in the world at once, on its own
session: its gold, then a sold notice. A Silk sale only tells it.
================
*/
func TestMarketSalePaysTheSellerWhenHere(t *testing.T) {
	rt, buyer, seller, desk := marketPair()
	desk.currency, desk.price, desk.sellerID = domain.MarketGold, 1000, seller.ID
	result := rt.HandleMarket(testDivision, buyer, []byte(`{"op":"buy","listing":5,"version":2,"to":"warehouse"}`))
	if len(result.Recipients) != 1 || result.Recipients[0].CharacterID != seller.ID {
		t.Fatalf("recipients = %+v", result.Recipients)
	}
	if *seller.Gold != 6000 || result.Recipients[0].Frames[0].Opcode != wire.OpPointsUpdate {
		t.Fatalf("seller gold %d, frames %+v", *seller.Gold, result.Recipients[0].Frames)
	}
	if events := marketEvents(t, result.Recipients[0].Frames); len(events) != 1 || events[0].Kind != marketEventSold || events[0].Paid != 1000 {
		t.Fatalf("seller events = %+v", events)
	}

	desk.currency = domain.MarketSilk
	result = rt.HandleMarket(testDivision, buyer, []byte(`{"op":"buy","listing":6,"version":2,"to":"warehouse"}`))
	if len(result.Recipients) != 1 || len(result.Recipients[0].Frames) != 1 || *seller.Gold != 6000 {
		t.Fatalf("a Silk sale's seller frames = %+v", result.Recipients)
	}

	rt.characterAdmissions.Delete(simulation.WorldKey(testDivision, seller.Name))
	desk.currency = domain.MarketGold
	if result := rt.HandleMarket(testDivision, buyer, []byte(`{"op":"buy","listing":7,"version":2,"to":"warehouse"}`)); len(result.Recipients) != 0 ||
		desk.owed[seller.ID] != 1000 {
		t.Fatalf("an absent seller was paid: %+v, owed %v", result.Recipients, desk.owed)
	}
}

/*
================
TestMarketProceedsTickPaysOnEntry

The hook pays an owed character once it is in the world, at most every
marketProceedsIntervalMs, and reads the sessions only when something is
owed.
================
*/
func TestMarketProceedsTickPaysOnEntry(t *testing.T) {
	rt, _, seller, desk := marketPair()
	reads := 0
	sessions := func() []simulation.SessionView {
		reads++
		return []simulation.SessionView{{DivisionID: testDivision, CharacterID: seller.ID}}
	}
	if out := rt.MarketProceedsTick(testDivision, sessions, 10_000); len(out) != 0 || reads != 0 {
		t.Fatalf("nothing owed: %d batches, %d reads", len(out), reads)
	}
	desk.owed[seller.ID] = 500
	if out := rt.MarketProceedsTick(testDivision, sessions, 10_500); len(out) != 0 {
		t.Fatalf("a pass inside the interval paid")
	}
	out := rt.MarketProceedsTick(testDivision, sessions, 12_000)
	if len(out) != 1 || out[0].OnlyCharacterID != seller.ID || *seller.Gold != 5500 {
		t.Fatalf("tick = %+v, gold %d", out, *seller.Gold)
	}
	desk.owed[seller.ID] = 500
	rt.characterAdmissions.Delete(simulation.WorldKey(testDivision, seller.Name))
	if out := rt.MarketProceedsTick(testDivision, sessions, 20_000); len(out) != 0 || *seller.Gold != 5500 {
		t.Fatalf("a character without an admission was paid")
	}
}

/*
================
TestMarketAcceptDeliversToTheBuyer

An accept writes the offering character's record under the lane and,
when it is in the world, sends it the delivery, the balance and a
notice; a buyer held by an exchange refuses.
================
*/
func TestMarketAcceptDeliversToTheBuyer(t *testing.T) {
	rt, seller, buyer, desk := marketPair()
	desk.currency, desk.price, desk.sellerID = domain.MarketGold, 700, seller.ID
	desk.offer = domain.MarketOffer{ID: 4, BuyerChar: buyer.ID, To: domain.MarketToBag}

	rt.Exchanges.Open(testDivision, buyer.Name, "Partner")
	if answer, _ := decodeMarketFrames(t, rt.HandleMarket(testDivision, seller, []byte(`{"op":"accept","offer":4}`)).Frames); answer.OK ||
		answer.Error != marketRefusedBuyerBusy || len(desk.accepted) != 0 {
		t.Fatalf("a trading buyer's accept: %+v", answer)
	}
	rt.Exchanges.Close(testDivision, buyer.Name)
	result := rt.HandleMarket(testDivision, seller, []byte(`{"op":"accept","offer":4}`))
	answer, _ := decodeMarketFrames(t, result.Frames)
	if !answer.OK || len(desk.accepted) != 1 || desk.bagSlots[0] != 13 {
		t.Fatalf("accept: %+v, slots %v", answer, desk.bagSlots)
	}
	var toBuyer []wire.Frame
	for _, recipient := range result.Recipients {
		if recipient.CharacterID == buyer.ID {
			toBuyer = recipient.Frames
		}
	}
	if len(toBuyer) < 3 || toBuyer[0].Opcode != wire.OpItemMoveResponse || *buyer.Gold != 5000-700 {
		t.Fatalf("buyer frames = %+v, gold %d", toBuyer, *buyer.Gold)
	}
	if events := marketEvents(t, toBuyer); len(events) != 1 || events[0].Kind != marketEventOfferAccepted {
		t.Fatalf("buyer events = %+v", events)
	}
	// The seller is the actor: its payment rides its own frames.
	if *seller.Gold != 5000+700 {
		t.Fatalf("seller gold = %d", *seller.Gold)
	}
}

/*
================
TestMarketOffersTellTheOtherParty

An offer tells the listing character, a reject the offering one, a
withdraw the listing one, each only when it is in the world.
================
*/
func TestMarketOffersTellTheOtherParty(t *testing.T) {
	rt, buyer, seller, desk := marketPair()
	desk.sellerID = seller.ID
	desk.offer = domain.MarketOffer{BuyerChar: buyer.ID}
	result := rt.HandleMarket(testDivision, buyer, []byte(`{"op":"offer","listing":3,"price":600,"to":"warehouse"}`))
	answer, _ := decodeMarketFrames(t, result.Frames)
	if !answer.OK || answer.Offer == nil || answer.Offer.Item.Ref != marketSwordRef {
		t.Fatalf("offer answer: %+v", answer)
	}
	if len(result.Recipients) != 1 || result.Recipients[0].CharacterID != seller.ID {
		t.Fatalf("offer recipients = %+v", result.Recipients)
	}
	if events := marketEvents(t, result.Recipients[0].Frames); len(events) != 1 || events[0].Kind != marketEventOffer ||
		events[0].Who != buyer.Name {
		t.Fatalf("offer events = %+v", events)
	}
	if result := rt.HandleMarket(testDivision, seller, []byte(`{"op":"reject","offer":4}`)); len(result.Recipients) != 1 ||
		result.Recipients[0].CharacterID != buyer.ID {
		t.Fatalf("reject recipients = %+v", result.Recipients)
	}
	if result := rt.HandleMarket(testDivision, buyer, []byte(`{"op":"withdraw","offer":4}`)); len(result.Recipients) != 1 ||
		result.Recipients[0].CharacterID != seller.ID {
		t.Fatalf("withdraw recipients = %+v", result.Recipients)
	}
	rt.characterAdmissions.Delete(simulation.WorldKey(testDivision, seller.Name))
	if result := rt.HandleMarket(testDivision, buyer, []byte(`{"op":"offer","listing":3,"price":600,"to":"bag"}`)); len(result.Recipients) != 0 {
		t.Fatalf("an absent seller was sent frames: %+v", result.Recipients)
	}
}

/*
================
TestMarketReclaimIntoTheBag
================
*/
func TestMarketReclaimIntoTheBag(t *testing.T) {
	rt, c, desk := marketFixture()
	result := rt.HandleMarket(testDivision, c, []byte(`{"op":"takeback","listing":5,"to":"bag"}`))
	answer, opcodes := decodeMarketFrames(t, result.Frames)
	if !answer.OK || len(desk.reclaimed) != 1 || desk.reclaimed[0] != domain.MarketToBag || opcodes[1] != wire.OpItemMoveResponse {
		t.Fatalf("bag take-back: %+v, frames %v", answer, opcodes)
	}
	answer, opcodes = decodeMarketFrames(t, rt.HandleMarket(testDivision, c, []byte(`{"op":"cancel","listing":5,"to":"warehouse"}`)).Frames)
	if !answer.OK || len(opcodes) != 1 {
		t.Fatalf("warehouse cancel: %+v, frames %v", answer, opcodes)
	}
}

/*
================
TestMarketRefusalsNameTheStoreCode
================
*/
func TestMarketRefusalsNameTheStoreCode(t *testing.T) {
	rt, c, desk := marketFixture()
	desk.failWith = domain.ErrMarketFunds
	answer, _ := decodeMarketFrames(t, rt.HandleMarket(testDivision, c, []byte(`{"op":"buy","listing":5,"version":2,"to":"warehouse"}`)).Frames)
	if answer.OK || answer.Error != "funds" {
		t.Fatalf("funds refusal: %+v", answer)
	}
	desk.failWith = domain.ErrMarketOfferPrice
	answer, _ = decodeMarketFrames(t, rt.HandleMarket(testDivision, c, []byte(`{"op":"offer","listing":5,"price":9,"to":"bag"}`)).Frames)
	if answer.OK || answer.Error != "offer-price" {
		t.Fatalf("offer refusal: %+v", answer)
	}
}

/*
================
TestMarketSearchMineAndAdmission

A search answers its page with the seller's name; "mine" previews the
listings and both offer lists in one reference frame. Forged searches,
unknown fields, ops and destinations, and a market that is off refuse.
================
*/
func TestMarketSearchMineAndAdmission(t *testing.T) {
	rt, c, desk := marketFixture()
	answer, _ := decodeMarketFrames(t, rt.HandleMarket(testDivision, c, []byte(`{"op":"search","category":"weapon","q":" sword "}`)).Frames)
	if !answer.OK || answer.Search == nil || len(answer.Search.Rows) != 1 || len(answer.Categories) != 1 {
		t.Fatalf("search answer: %+v", answer)
	}
	if row := answer.Search.Rows[0]; row.ID != 7 || row.Seller != "Seller" || row.Item.Ref != marketSwordRef || len(row.Item.Body) == 0 {
		t.Fatalf("search row: %+v", row)
	}
	if q := desk.searched[0]; q.Page != 1 || q.Q != "sword" || q.Category != "weapon" {
		t.Fatalf("search = %+v", q)
	}
	result := rt.HandleMarket(testDivision, c, []byte(`{"op":"mine"}`))
	answer, opcodes := decodeMarketFrames(t, result.Frames)
	if !answer.OK || answer.Mine == nil || len(answer.Mine.Listings) != 1 || len(answer.Mine.OffersIn) != 1 ||
		len(answer.Mine.OffersOut) != 1 || !answer.Mine.Listings[0].Anonymous || answer.Mine.OffersIn[0].Buyer != "Buyer" ||
		opcodes[0] != opCommerceItemReferences || answer.Mine.OffersOut[0].Item.Slot != 2 {
		t.Fatalf("mine: %+v, frames %v", answer.Mine, opcodes)
	}
	for name, payload := range map[string]string{
		"unknown category":  `{"op":"search","category":"wands"}`,
		"page past the end": `{"op":"search","page":101}`,
		"degree too high":   `{"op":"search","degree":12}`,
		"unknown currency":  `{"op":"search","currency":"points"}`,
		"unknown sort":      `{"op":"search","sort":"cheapest"}`,
		"unknown field":     `{"op":"search","account":"victim"}`,
		"unknown op":        `{"op":"steal"}`,
		"unknown place":     `{"op":"buy","listing":1,"version":1,"to":"mailbox"}`,
		"trailing data":     `{"op":"mine"}{"op":"mine"}`,
		"empty":             ``,
	} {
		answer, _ := decodeMarketFrames(t, rt.HandleMarket(testDivision, c, []byte(payload)).Frames)
		if answer.OK || answer.Error != marketRefusedRequest {
			t.Errorf("%s: %+v", name, answer)
		}
	}
	if len(desk.searched) != 1 {
		t.Fatalf("a forged search reached the desk: %d", len(desk.searched))
	}
	rt.Market = nil
	answer, _ = decodeMarketFrames(t, rt.HandleMarket(testDivision, c, []byte(`{"op":"mine"}`)).Frames)
	if answer.OK || answer.Error != marketRefusedUnavailable {
		t.Fatalf("a market that is off: %+v", answer)
	}
}

/*
================
TestMarketWritesNeedNoKeeper

The market opens from the HUD anywhere in the world: a listing goes
through with no NPC selected or near.
================
*/
func TestMarketWritesNeedNoKeeper(t *testing.T) {
	rt, c, desk := marketFixture()
	rt.Selected.Clear(testDivision, c.Name)
	list := []byte(`{"op":"list","slot":13,"currency":"gold","price":10}`)
	if got := marketAnswerFirst(t, rt.HandleMarket(testDivision, c, list)); !got.OK {
		t.Fatalf("a listing with no NPC = %+v", got)
	}
	if len(desk.listed) != 1 {
		t.Fatalf("the listing did not reach the market: %+v", desk.listed)
	}
}

/*
================
marketAnswerFirst

The answer of a lane result.
================
*/
func marketAnswerFirst(t *testing.T, result OpResult) marketAnswer {
	t.Helper()
	answer, _ := decodeMarketFrames(t, result.Frames)
	return answer
}
