/*
===========================================================================

service.go - the website market's logic behind its routes
(publicstats/market.go)

Port-only, not native (SRO_CONSIGNMENT). Service turns a route's request
into a store door and the store's rows into what the site shows: item
names, categories and degrees from the item references, the account's
characters for the gold-buy picker, and the rules.

Two rules live here rather than in the store:

  - The website lists only from the account warehouse (the design doc):
    a bag belongs to the live character.
  - A website gold buy refuses a character that is online ("online"),
    since an online character's gold belongs to its action lane. It never
    waits for a logout.
  - The game's doors (Game*) are the mirror image: the in-game window
    lists from the online character's bag, pays from its gold, delivers
    to its bag when the buyer picks it, and makes and answers offers, on
    the action lane that owns that record (game/action market.go), which
    calls them under the division lock.
  - A website offer or accept that needs a character's record (the
    offering character's gold) runs under WhileOffline, as a gold buy
    does. The game's accept runs on the lane under the division lock,
    which already holds the buyer's record whether it is in the world or
    not.

A listing names its seller's character unless the seller listed
anonymously (owner, 2026-11-19); the seller still sees its own choice.

===========================================================================
*/
package market

import (
	"sort"
	"strings"

	"opensro.online/server/internal/agent/publicstats"
	"opensro.online/server/internal/domain"
)

// searchScan bounds how many open listings one search reads: 20 per
// account keeps a shard's market far below it.
const searchScan = 5000

// historyLimit bounds the sold/returned listings and purchases the
// account page shows.
const historyLimit = 50

// The categories, by item TypeID (TID1 3 = item; TID2 1 = equipment,
// 2 = pet, 3 = consumable and the rest). The equipment TID3 values are
// v1.150's itemdata: 1-3 Chinese garment/protector/armor, 9-11 European
// robe/light/heavy armor, 4 shield, 5 and 12 accessories, 6 weapons, 13
// and 14 avatars. "other" takes every item no row names.
var categories = []publicstats.MarketCategory{
	{Slug: "weapon", Name: "Weapons"},
	{Slug: "shield", Name: "Shields"},
	{Slug: "armor", Name: "Armor"},
	{Slug: "accessory", Name: "Accessories"},
	{Slug: "avatar", Name: "Avatars"},
	{Slug: "pet", Name: "Pets"},
	{Slug: "consumable", Name: "Consumables"},
	{Slug: "other", Name: "Other"},
}

/*
================
ItemInfo

What the market shows of an item reference.
================
*/
type ItemInfo struct {
	Name    string
	TypeIDs [4]int64
	Degree  int
	// Race is publicstats.MarketRaceChinese or MarketRaceEuropean, "" for
	// an item either race uses; Sealed is a sealed (rare) item.
	Race   string
	Sealed bool
}

/*
================
ServiceStore

The store doors the website market uses (data/store/market.go).
================
*/
type ServiceStore interface {
	MarketList(req domain.MarketListRequest) (domain.MarketListing, error)
	MarketBuy(req domain.MarketBuyRequest) (domain.MarketSale, error)
	MarketCancel(account string, id int64) (domain.MarketListing, error)
	MarketTakeBack(account string, id int64) (domain.MarketListing, error)
	MarketListingByID(id int64) (domain.MarketListing, error)
	MarketOpenListings(q domain.MarketQuery) ([]domain.MarketListing, error)
	MarketAccountListings(account string, limit int) ([]domain.MarketListing, error)
	MarketPurchases(account string, limit int) ([]domain.MarketSale, error)
	MarketWarehouse(account string) (domain.AccountStorage, error)
	MarketWallet(account string) (domain.MallBalance, error)
	MarketReclaim(req domain.MarketReclaimRequest) (domain.MarketListing, error)
	MarketMakeOffer(req domain.MarketOfferRequest) (domain.MarketOffer, error)
	MarketAcceptOffer(req domain.MarketAcceptRequest) (domain.MarketSale, domain.MarketOffer, error)
	MarketRejectOffer(sellerAccount string, id int64) (domain.MarketOffer, error)
	MarketWithdrawOffer(buyerAccount string, id int64) (domain.MarketOffer, error)
	MarketOffers(account string, limit int) (incoming, outgoing []domain.MarketOffer, err error)
	MarketOfferByID(id int64) (domain.MarketOffer, error)
	MarketOwedCharacters() (map[int64]bool, error)
	MarketClaimProceeds(c *domain.Character) ([]domain.MarketSale, error)
}

/*
================
Service

The GameWorld fills every field (cmd/services/sro-gameworld). Characters
returns the account's live characters of the division; Tradeable is the
exchange's rule (action.Runtime.Tradeable).
================
*/
type Service struct {
	Store      ServiceStore
	Division   string
	Item       func(codename string) (ItemInfo, bool)
	Tradeable  func(row domain.InventoryRow) bool
	Characters func(account string) []*domain.Character
	Online     func(characterID int64) bool
	// WhileOffline runs a write against the character only while it holds
	// no world admission, under the gate entry admits under, and reports
	// online (write not run) otherwise. Online above is the published
	// session list, for display only: it can lag an entry.
	WhileOffline func(c *domain.Character, write func() error) (online bool, err error)
	// FreeBagSlot is the first free bag slot of a character, the bag's
	// size being the item lane's rule (inventory.BagEnd); -1 when full.
	FreeBagSlot func(c *domain.Character) int64
}

/*
================
Service::Categories
================
*/
func (s *Service) Categories() []publicstats.MarketCategory {
	return append([]publicstats.MarketCategory(nil), categories...)
}

/*
================
Service::Search

Reads the division's open listings, filters and orders them here (the
filters need the item references), and cuts one page.
================
*/
func (s *Service) Search(q publicstats.MarketSearch) (publicstats.MarketListingsResponse, error) {
	listings, err := s.Store.MarketOpenListings(domain.MarketQuery{Division: s.Division, Currency: q.Currency, Limit: searchScan})
	if err != nil {
		return publicstats.MarketListingsResponse{}, err
	}
	needle := strings.ToLower(strings.TrimSpace(q.Q))
	matched := []publicstats.MarketListingView{}
	for _, listing := range listings {
		view := s.listingView(listing)
		if q.Category != "" && view.Item.Category != q.Category {
			continue
		}
		if q.Degree != 0 && view.Item.Degree != q.Degree {
			continue
		}
		if q.Race != "" && view.Item.Race != "" && view.Item.Race != q.Race {
			continue
		}
		if q.Rarity != "" && view.Item.Sealed != (q.Rarity == publicstats.MarketRaritySealed) {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(view.Item.Name), needle) {
			continue
		}
		matched = append(matched, view)
	}
	sortListings(matched, q.Sort)
	pages := (len(matched) + publicstats.MarketPageSize - 1) / publicstats.MarketPageSize
	start := (q.Page - 1) * publicstats.MarketPageSize
	page := []publicstats.MarketListingView{}
	if start < len(matched) {
		page = matched[start:min(start+publicstats.MarketPageSize, len(matched))]
	}
	return publicstats.MarketListingsResponse{Listings: page, Page: q.Page, Pages: pages, Total: len(matched)}, nil
}

/*
================
sortListings

Newest first by default; ties fall back to the id, so a page never
reshuffles between two reads.
================
*/
func sortListings(views []publicstats.MarketListingView, order string) {
	sort.SliceStable(views, func(i, j int) bool {
		a, b := views[i], views[j]
		switch order {
		case publicstats.MarketSortEnding:
			if a.ExpiresMs != b.ExpiresMs {
				return a.ExpiresMs < b.ExpiresMs
			}
		case publicstats.MarketSortPriceAsc:
			if a.Price != b.Price {
				return a.Price < b.Price
			}
		case publicstats.MarketSortPriceDesc:
			if a.Price != b.Price {
				return a.Price > b.Price
			}
		default:
			if a.ListedMs != b.ListedMs {
				return a.ListedMs > b.ListedMs
			}
		}
		return a.ID > b.ID
	})
}

/*
================
Service::Account

The signed-in player's market page: listings, purchases, the warehouse
to list from, the characters to pay with, the Silk balance and the rules.
================
*/
func (s *Service) Account(account string) (publicstats.OwnMarketResponse, error) {
	out := publicstats.OwnMarketResponse{
		Listings: []publicstats.MarketListingView{}, Purchases: []publicstats.MarketSaleView{},
		Warehouse: []publicstats.MarketWarehouseItem{}, Characters: []publicstats.MarketCharacter{},
		Rules:    rulesView(),
		OffersIn: []publicstats.MarketOfferView{}, OffersOut: []publicstats.MarketOfferView{},
	}
	listings, err := s.Store.MarketAccountListings(account, historyLimit)
	if err != nil {
		return out, err
	}
	for _, listing := range listings {
		out.Listings = append(out.Listings, s.ownListingView(listing))
	}
	purchases, err := s.Store.MarketPurchases(account, historyLimit)
	if err != nil {
		return out, err
	}
	for _, sale := range purchases {
		out.Purchases = append(out.Purchases, s.saleView(sale))
	}
	storage, err := s.Store.MarketWarehouse(account)
	if err != nil {
		return out, err
	}
	out.WarehouseGold = storage.Gold
	for _, row := range storage.Rows {
		out.Warehouse = append(out.Warehouse, publicstats.MarketWarehouseItem{Slot: row.Slot, Item: s.itemView(row),
			Tradeable: s.Tradeable(row)})
	}
	sort.Slice(out.Warehouse, func(i, j int) bool { return out.Warehouse[i].Slot < out.Warehouse[j].Slot })
	for _, c := range s.Characters(account) {
		gold := int64(0)
		if c.Gold != nil {
			gold = *c.Gold
		}
		out.Characters = append(out.Characters, publicstats.MarketCharacter{Name: c.Name, Gold: gold, Online: s.Online(c.ID)})
	}
	sort.Slice(out.Characters, func(i, j int) bool { return out.Characters[i].Name < out.Characters[j].Name })
	wallet, err := s.Store.MarketWallet(account)
	if err != nil {
		return out, err
	}
	out.Silk = wallet.Silk
	incoming, outgoing, err := s.Store.MarketOffers(account, historyLimit)
	if err != nil {
		return out, err
	}
	out.OffersIn, out.OffersOut = s.offerViews(incoming), s.offerViews(outgoing)
	return out, nil
}

/*
================
Service::GameAccount

The account page as one character sees it in the game: its own
listings, the offers on them and the offers and buys it made. A
character's bag listing, its gold and its offers are that character's
(the gold of a sale goes to the listing character), so a second
character of the account neither sees nor works them. What the account
shares stays shared: warehouse listings and website buys (character 0),
the Silk balance and the rules. The website page (Account) keeps the
whole account.
================
*/
func (s *Service) GameAccount(c *domain.Character) (publicstats.OwnMarketResponse, error) {
	out, err := s.Account(c.AccountID)
	if err != nil {
		return out, err
	}
	out.Listings = kept(out.Listings, func(v publicstats.MarketListingView) bool { return heldBy(v.SellerChar, c) })
	out.Purchases = kept(out.Purchases, func(v publicstats.MarketSaleView) bool { return heldBy(v.BuyerChar, c) })
	out.OffersIn = kept(out.OffersIn, func(v publicstats.MarketOfferView) bool { return heldBy(v.SellerChar, c) })
	out.OffersOut = kept(out.OffersOut, func(v publicstats.MarketOfferView) bool { return heldBy(v.BuyerChar, c) })
	return out, nil
}

/*
================
heldBy

Whether a record of character id is c's to see and work in the game:
its own, or the account's shared one (0, made on the website).
================
*/
func heldBy(id int64, c *domain.Character) bool {
	return id == 0 || id == c.ID
}

/*
================
kept

The rows keep accepts, in order.
================
*/
func kept[T any](rows []T, keep func(T) bool) []T {
	out := rows[:0]
	for _, row := range rows {
		if keep(row) {
			out = append(out, row)
		}
	}
	return out
}

/*
================
Service::characterListing

A game write on listing id by c: another character's listing is no
such listing, as the page never showed it.
================
*/
func (s *Service) characterListing(c *domain.Character, id int64) error {
	listing, err := s.Store.MarketListingByID(id)
	if err != nil {
		return err
	}
	if !heldBy(listing.SellerChar, c) {
		return domain.ErrMarketNotFound
	}
	return nil
}

/*
================
Service::characterOffer

A game answer on offer id by c: the seller's side (seller true) is the
listing character's, the withdraw side the offering character's.
Another character's offer is gone, as the page never showed it.
================
*/
func (s *Service) characterOffer(c *domain.Character, id int64, seller bool) error {
	offer, err := s.Store.MarketOfferByID(id)
	if err != nil {
		return err
	}
	if !seller {
		if offer.BuyerChar != c.ID {
			return domain.ErrMarketOfferGone
		}
		return nil
	}
	if err := s.characterListing(c, offer.ListingID); err != nil {
		return domain.ErrMarketOfferGone
	}
	return nil
}

/*
================
Service::List

A website listing, from the account warehouse.
================
*/
func (s *Service) List(account string, body publicstats.MarketListBody) (publicstats.MarketListingView, error) {
	listing, err := s.Store.MarketList(domain.MarketListRequest{
		Division: s.Division, Account: account, Slot: *body.Slot, Currency: body.Currency, Price: body.Price,
		Admit: s.admit, Rules: Rules(),
	})
	if err != nil {
		return publicstats.MarketListingView{}, err
	}
	return s.listingView(listing), nil
}

/*
================
Service::Buy

A gold listing is paid by the named character, which must be the
account's and offline; a Silk listing from the account's wallet.
================
*/
func (s *Service) Buy(account string, body publicstats.MarketBuyBody) (publicstats.MarketSaleView, error) {
	listing, err := s.listingHere(body.Listing)
	if err != nil {
		return publicstats.MarketSaleView{}, err
	}
	request := domain.MarketBuyRequest{ListingID: body.Listing, Version: body.Version, BuyerAccount: account, Rules: Rules()}
	if listing.Currency == domain.MarketGold {
		payer := s.character(account, body.Character)
		if payer == nil {
			return publicstats.MarketSaleView{}, publicstats.ErrNotOwned
		}
		request.Payer = payer
	}
	var sale domain.MarketSale
	buy := func() error {
		var err error
		sale, err = s.Store.MarketBuy(request)
		return err
	}
	if request.Payer == nil {
		err = buy()
	} else {
		// The payer's record is written only while no entry admits it:
		// WhileOffline holds the gate entry admits under, so an entry
		// either comes first and refuses the buy, or follows and reads
		// the paid gold.
		var online bool
		online, err = s.WhileOffline(request.Payer, buy)
		if online {
			return publicstats.MarketSaleView{}, publicstats.ErrMarketOnline
		}
	}
	if err != nil {
		return publicstats.MarketSaleView{}, err
	}
	return s.saleView(sale), nil
}

/*
================
Service::Cancel
================
*/
func (s *Service) Cancel(account string, listing int64) error {
	_, err := s.Store.MarketCancel(account, listing)
	return err
}

/*
================
Service::TakeBack
================
*/
func (s *Service) TakeBack(account string, listing int64) error {
	_, err := s.Store.MarketTakeBack(account, listing)
	return err
}

/*
================
Service::GameList

An in-game listing, from the online character's bag. The caller is the
action lane under the division lock and has judged the slot a bag slot
and the bag free of a stall or an exchange.
================
*/
func (s *Service) GameList(c *domain.Character, slot int64, currency string, price int64, anonymous bool) (publicstats.MarketListingView, error) {
	listing, err := s.Store.MarketList(domain.MarketListRequest{
		Division: s.Division, Account: c.AccountID, Character: c, Slot: slot, Currency: currency, Price: price,
		Anonymous: anonymous, Admit: s.admit, Rules: Rules(),
	})
	if err != nil {
		return publicstats.MarketListingView{}, err
	}
	return s.ownListingView(listing), nil
}

/*
================
Service::GameBuy

An in-game buy: a gold listing is paid from the online character's own
gold, a Silk listing from its account's wallet. The item lands where the
buyer picked: bagSlot of its bag (the lane chose a free one), or the
warehouse. The caller is the action lane under the division lock, so the
record the store writes is the one the lane owns.
================
*/
func (s *Service) GameBuy(c *domain.Character, listing, version int64, to string, bagSlot int64) (publicstats.MarketSaleView, error) {
	found, err := s.listingHere(listing)
	if err != nil {
		return publicstats.MarketSaleView{}, err
	}
	request := domain.MarketBuyRequest{ListingID: listing, Version: version, BuyerAccount: c.AccountID, To: to,
		BagSlot: bagSlot, Rules: Rules()}
	if found.Currency == domain.MarketGold {
		request.Payer = c
	}
	if to == domain.MarketToBag {
		request.Receiver = c
	}
	sale, err := s.Store.MarketBuy(request)
	if err != nil {
		return publicstats.MarketSaleView{}, err
	}
	return s.saleView(sale), nil
}

/*
================
Service::GameReclaim

The seller's cancel (an open listing) or take-back (a held one), into
its bag or the warehouse.
================
*/
func (s *Service) GameReclaim(c *domain.Character, listing int64, to string, bagSlot int64) error {
	if err := s.characterListing(c, listing); err != nil {
		return err
	}
	_, err := s.Store.MarketReclaim(domain.MarketReclaimRequest{Account: c.AccountID, ListingID: listing, To: to,
		Receiver: c, BagSlot: bagSlot})
	return err
}

/*
================
Service::GameOffer
================
*/
func (s *Service) GameOffer(c *domain.Character, listing, price int64, to string) (publicstats.MarketOfferView, error) {
	found, err := s.listingHere(listing)
	if err != nil {
		return publicstats.MarketOfferView{}, err
	}
	offer, err := s.Store.MarketMakeOffer(domain.MarketOfferRequest{ListingID: listing, BuyerAccount: c.AccountID,
		Buyer: c, Price: price, To: to, Rules: Rules()})
	if err != nil {
		return publicstats.MarketOfferView{}, err
	}
	return s.offerView(offer, found), nil
}

/*
================
Service::OfferBuyer

The offer, for the lane to find the character that made it: the record
an accept writes, which the lane holds under its division lock whether
that character is in the world or not.
================
*/
func (s *Service) OfferBuyer(offer int64) (domain.MarketOffer, error) {
	return s.Store.MarketOfferByID(offer)
}

/*
================
Service::GameAccept

The seller (c) accepts an offer in the game: buyer is the offering
character's record, which the lane holds under the division lock, and
bagSlot a free slot of its bag.
================
*/
func (s *Service) GameAccept(c *domain.Character, offer int64, buyer *domain.Character, bagSlot int64) (publicstats.MarketSaleView, error) {
	if err := s.characterOffer(c, offer, true); err != nil {
		return publicstats.MarketSaleView{}, err
	}
	sale, _, err := s.Store.MarketAcceptOffer(domain.MarketAcceptRequest{OfferID: offer, SellerAccount: c.AccountID,
		Buyer: buyer, BagSlot: bagSlot, Rules: Rules()})
	if err != nil {
		return publicstats.MarketSaleView{}, err
	}
	return s.saleView(sale), nil
}

/*
================
Service::AcceptOffer

A website accept: the store writes the buyer's record under the entry
gate (WhileOffline), so an entry either comes first and the accept
answers "online", or follows and reads the sale.
================
*/
func (s *Service) AcceptOffer(sellerAccount string, offer int64) (publicstats.MarketSaleView, error) {
	found, err := s.Store.MarketOfferByID(offer)
	if err != nil {
		return publicstats.MarketSaleView{}, err
	}
	buyer := s.characterByID(found.BuyerAccount, found.BuyerChar)
	var sale domain.MarketSale
	accept := func() error {
		bagSlot := int64(-1)
		if buyer != nil && s.FreeBagSlot != nil {
			bagSlot = s.FreeBagSlot(buyer)
		}
		var err error
		sale, _, err = s.Store.MarketAcceptOffer(domain.MarketAcceptRequest{OfferID: offer, SellerAccount: sellerAccount,
			Buyer: buyer, BagSlot: bagSlot, Rules: Rules()})
		return err
	}
	if buyer == nil {
		// The store closes an offer whose character is gone.
		err = accept()
	} else {
		var online bool
		online, err = s.WhileOffline(buyer, accept)
		if online {
			return publicstats.MarketSaleView{}, publicstats.ErrMarketOnline
		}
	}
	if err != nil {
		return publicstats.MarketSaleView{}, err
	}
	return s.saleView(sale), nil
}

/*
================
Service::GameReject
================
*/
func (s *Service) GameReject(c *domain.Character, offer int64) (publicstats.MarketOfferView, error) {
	if err := s.characterOffer(c, offer, true); err != nil {
		return publicstats.MarketOfferView{}, err
	}
	answered, err := s.Store.MarketRejectOffer(c.AccountID, offer)
	return s.answeredView(answered, err)
}

/*
================
Service::GameWithdraw
================
*/
func (s *Service) GameWithdraw(c *domain.Character, offer int64) (publicstats.MarketOfferView, error) {
	if err := s.characterOffer(c, offer, false); err != nil {
		return publicstats.MarketOfferView{}, err
	}
	answered, err := s.Store.MarketWithdrawOffer(c.AccountID, offer)
	return s.answeredView(answered, err)
}

/*
================
answeredView

The view of an offer a party just answered, for the other party's
notice.
================
*/
func (s *Service) answeredView(offer domain.MarketOffer, err error) (publicstats.MarketOfferView, error) {
	if err != nil {
		return publicstats.MarketOfferView{}, err
	}
	listing, err := s.Store.MarketListingByID(offer.ListingID)
	if err != nil {
		return publicstats.MarketOfferView{}, err
	}
	return s.offerView(offer, listing), nil
}

/*
================
Service::Owed

The characters some sale still owes gold (store market_proceeds.go).
================
*/
func (s *Service) Owed() (map[int64]bool, error) {
	return s.Store.MarketOwedCharacters()
}

/*
================
Service::ClaimProceeds

Pays c what its sales owe it; the caller is c's lane under the division
lock. The sales come back with the item names for the seller's notice.
================
*/
func (s *Service) ClaimProceeds(c *domain.Character) ([]publicstats.MarketSaleView, []string, error) {
	sales, err := s.Store.MarketClaimProceeds(c)
	if err != nil {
		return nil, nil, err
	}
	views := make([]publicstats.MarketSaleView, 0, len(sales))
	names := make([]string, 0, len(sales))
	for _, sale := range sales {
		views = append(views, s.saleView(sale))
		names = append(names, s.itemView(domain.InventoryRow{Codename: sale.Codename}).Name)
	}
	return views, names, nil
}

/*
================
Service::MakeOffer

A website offer: the named character, offline, is the payer it is
judged against. The item of an accepted website offer goes to the
warehouse.
================
*/
func (s *Service) MakeOffer(account string, body publicstats.MarketOfferBody) (publicstats.MarketOfferView, error) {
	listing, err := s.listingHere(body.Listing)
	if err != nil {
		return publicstats.MarketOfferView{}, err
	}
	buyer := s.character(account, body.Character)
	if buyer == nil {
		return publicstats.MarketOfferView{}, publicstats.ErrNotOwned
	}
	var offer domain.MarketOffer
	online, err := s.WhileOffline(buyer, func() error {
		var err error
		offer, err = s.Store.MarketMakeOffer(domain.MarketOfferRequest{ListingID: body.Listing, BuyerAccount: account,
			Buyer: buyer, Price: body.Price, To: domain.MarketToWarehouse, Rules: Rules()})
		return err
	})
	if online {
		return publicstats.MarketOfferView{}, publicstats.ErrMarketOnline
	}
	if err != nil {
		return publicstats.MarketOfferView{}, err
	}
	return s.offerView(offer, listing), nil
}

/*
================
Service::RejectOffer
================
*/
func (s *Service) RejectOffer(account string, offer int64) error {
	_, err := s.Store.MarketRejectOffer(account, offer)
	return err
}

/*
================
Service::WithdrawOffer
================
*/
func (s *Service) WithdrawOffer(account string, offer int64) error {
	_, err := s.Store.MarketWithdrawOffer(account, offer)
	return err
}

/*
================
characterByID
================
*/
func (s *Service) characterByID(account string, id int64) *domain.Character {
	for _, c := range s.Characters(account) {
		if c.ID == id {
			return c
		}
	}
	return nil
}

/*
================
offerViews
================
*/
func (s *Service) offerViews(offers []domain.MarketOffer) []publicstats.MarketOfferView {
	out := make([]publicstats.MarketOfferView, 0, len(offers))
	for _, offer := range offers {
		listing, err := s.Store.MarketListingByID(offer.ListingID)
		if err != nil {
			continue
		}
		out = append(out, s.offerView(offer, listing))
	}
	return out
}

/*
================
offerView
================
*/
func (s *Service) offerView(offer domain.MarketOffer, listing domain.MarketListing) publicstats.MarketOfferView {
	return publicstats.MarketOfferView{ID: offer.ID, ListingID: offer.ListingID, Item: s.itemView(listing.Item),
		Currency: listing.Currency, Asked: listing.Price, Price: offer.Price, Buyer: offer.BuyerName, To: offer.To,
		State: offer.State, AtMs: offer.AtMs, Row: listing.Item, SellerChar: listing.SellerChar, BuyerChar: offer.BuyerChar}
}

/*
================
saleView
================
*/
func (s *Service) saleView(sale domain.MarketSale) publicstats.MarketSaleView {
	return publicstats.MarketSaleView{ListingID: sale.ListingID, Currency: sale.Currency, Price: sale.Price, Fee: sale.Fee,
		AtMs: sale.AtMs, To: sale.To, Slot: sale.Slot, SellerChar: sale.SellerChar, BuyerChar: sale.BuyerChar,
		Item: s.itemView(domain.InventoryRow{Codename: sale.Codename}).Name}
}

/*
================
ownListingView

A listing as its own seller sees it: the name even when anonymous, with
the anonymous choice beside it.
================
*/
func (s *Service) ownListingView(listing domain.MarketListing) publicstats.MarketListingView {
	view := s.listingView(listing)
	view.Seller, view.Anonymous, view.SellerChar = listing.SellerName, listing.Anonymous, listing.SellerChar
	return view
}

/*
================
admit

A listing's admission: the exchange's rule (Tradeable).
================
*/
func (s *Service) admit(row domain.InventoryRow) error {
	if !s.Tradeable(row) {
		return domain.ErrMarketNotTradeable
	}
	return nil
}

/*
================
listingHere

The listing id when it is this division's market's; another division's
is no such listing.
================
*/
func (s *Service) listingHere(id int64) (domain.MarketListing, error) {
	listing, err := s.Store.MarketListingByID(id)
	if err != nil {
		return domain.MarketListing{}, err
	}
	if listing.Division != s.Division {
		return domain.MarketListing{}, domain.ErrMarketNotFound
	}
	return listing, nil
}

/*
================
character

The account's character of that name, or nil.
================
*/
func (s *Service) character(account, name string) *domain.Character {
	if name == "" {
		return nil
	}
	for _, c := range s.Characters(account) {
		if strings.EqualFold(c.Name, name) {
			return c
		}
	}
	return nil
}

/*
================
listingView
================
*/
func (s *Service) listingView(listing domain.MarketListing) publicstats.MarketListingView {
	return publicstats.MarketListingView{
		ID: listing.ID, Version: listing.Version, Item: s.itemView(listing.Item), Currency: listing.Currency,
		Price: listing.Price, ListedMs: listing.ListedMs, ExpiresMs: listing.ExpiresMs, State: listing.State,
		Seller: publicSeller(listing), Row: listing.Item,
	}
}

/*
================
publicSeller

The name every viewer sees: none when the seller listed anonymously.
================
*/
func publicSeller(listing domain.MarketListing) string {
	if listing.Anonymous {
		return ""
	}
	return listing.SellerName
}

/*
================
itemView
================
*/
func (s *Service) itemView(row domain.InventoryRow) publicstats.MarketItem {
	view := publicstats.MarketItem{RefObjID: row.RefObjID, Codename: row.Codename, Category: "other", Plus: row.Plus,
		Quantity: row.StackCount}
	if info, ok := s.Item(row.Codename); ok {
		view.Name = info.Name
		view.Degree = info.Degree
		view.Race = info.Race
		view.Sealed = info.Sealed
		view.Category = Category(info.TypeIDs)
	}
	return view
}

/*
================
Category

The picker slug for an item's TypeIDs (the categories table above).
================
*/
func Category(types [4]int64) string {
	if types[0] != 3 {
		return "other"
	}
	switch types[1] {
	case 2:
		return "pet"
	case 3:
		return "consumable"
	case 1:
		switch types[2] {
		case 6:
			return "weapon"
		case 4:
			return "shield"
		case 1, 2, 3, 9, 10, 11:
			return "armor"
		case 5, 12:
			return "accessory"
		case 13, 14:
			return "avatar"
		}
	}
	return "other"
}

/*
================
rulesView
================
*/
func rulesView() publicstats.MarketRulesView {
	rules := Rules()
	return publicstats.MarketRulesView{FeePercent: rules.FeePercent, DurationMs: rules.DurationMs, OpenCap: rules.OpenCap,
		OfferCap: rules.OfferCap}
}
