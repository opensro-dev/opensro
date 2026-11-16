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

The seller is never named: a listing shows the item, price and times.

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
		Rules: rulesView(),
	}
	listings, err := s.Store.MarketAccountListings(account, historyLimit)
	if err != nil {
		return out, err
	}
	for _, listing := range listings {
		out.Listings = append(out.Listings, s.listingView(listing))
	}
	purchases, err := s.Store.MarketPurchases(account, historyLimit)
	if err != nil {
		return out, err
	}
	for _, sale := range purchases {
		out.Purchases = append(out.Purchases, publicstats.MarketSaleView{ListingID: sale.ListingID, Currency: sale.Currency,
			Price: sale.Price, Fee: sale.Fee, AtMs: sale.AtMs})
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
	return out, nil
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
		Admit: func(row domain.InventoryRow) error {
			if !s.Tradeable(row) {
				return domain.ErrMarketNotTradeable
			}
			return nil
		},
		Rules: Rules(),
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
	listing, err := s.Store.MarketListingByID(body.Listing)
	if err != nil {
		return publicstats.MarketSaleView{}, err
	}
	if listing.Division != s.Division {
		return publicstats.MarketSaleView{}, domain.ErrMarketNotFound
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
	return publicstats.MarketSaleView{ListingID: sale.ListingID, Currency: sale.Currency, Price: sale.Price, Fee: sale.Fee,
		AtMs: sale.AtMs}, nil
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
	}
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
	return publicstats.MarketRulesView{FeePercent: rules.FeePercent, DurationMs: rules.DurationMs, OpenCap: rules.OpenCap}
}
