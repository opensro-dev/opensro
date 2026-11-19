/*
===========================================================================

market_offers.go - offers below a listing's price (opensro#3529)

Port-only, not native (SRO_CONSIGNMENT). The owner's rule (2026-11-19):
a buyer offers a price below the listing's,
nothing is held, and the seller accepts or rejects; the buyer may
withdraw. An accept is a sale at the offer's price (settleSaleLocked),
so it pays, delivers and closes the listing's other offers exactly as a
buy does.

The offer checks the buyer can pay when it is made; the accept checks
again, since the gold or Silk may have been spent. An offer whose buyer
can no longer pay, or whose character is gone, is closed by the accept
that finds it so; an offer the seller cannot deliver (the buyer's bag
and warehouse both full) stays open and the seller is told why.

===========================================================================
*/
package store

import (
	"database/sql"
	"errors"

	"opensro.online/server/internal/domain"
)

// marketOfferColumns is every column an offer read selects, in
// scanOffers' order.
const marketOfferColumns = `id, listing_id, division, buyer_account, buyer_char, buyer_name, price, dest, state, at_ms`

/*
================
MarketMakeOffer
================
*/
func (s *Store) MarketMakeOffer(req domain.MarketOfferRequest) (domain.MarketOffer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil || req.BuyerAccount == "" {
		return domain.MarketOffer{}, domain.ErrMarketUnavailable
	}
	if req.To != domain.MarketToBag && req.To != domain.MarketToWarehouse {
		return domain.MarketOffer{}, domain.ErrMarketUnavailable
	}
	db := s.syncDBLocked()
	listing, err := readListing(db, req.ListingID)
	if err != nil {
		return domain.MarketOffer{}, err
	}
	if listing.State != domain.MarketOpen || listing.ExpiresMs <= s.now().UnixMilli() {
		return domain.MarketOffer{}, domain.ErrMarketChanged
	}
	if listing.SellerAccount == req.BuyerAccount {
		return domain.MarketOffer{}, domain.ErrMarketOwnListing
	}
	if err := checkMarketPrice(listing.Currency, req.Price); err != nil {
		return domain.MarketOffer{}, err
	}
	if req.Price >= listing.Price {
		return domain.MarketOffer{}, domain.ErrMarketOfferPrice
	}
	division, owned := s.ownCharacterLocked(req.Buyer, req.BuyerAccount)
	if !owned || division != listing.Division {
		return domain.MarketOffer{}, domain.ErrMarketUnavailable
	}
	if err := s.canPayLocked(listing.Currency, req.BuyerAccount, req.Buyer, req.Price); err != nil {
		return domain.MarketOffer{}, err
	}
	var open, mine int
	if err := db.QueryRow("SELECT COUNT(*) FROM market_offers WHERE buyer_account = ? AND state = 'open'",
		req.BuyerAccount).Scan(&open); err != nil {
		return domain.MarketOffer{}, err
	}
	if open >= req.Rules.OfferCap {
		return domain.MarketOffer{}, domain.ErrMarketOfferCap
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM market_offers WHERE listing_id = ? AND buyer_account = ? AND state = 'open'",
		listing.ID, req.BuyerAccount).Scan(&mine); err != nil {
		return domain.MarketOffer{}, err
	}
	if mine != 0 {
		return domain.MarketOffer{}, domain.ErrMarketOfferOpen
	}
	offer := domain.MarketOffer{ListingID: listing.ID, Division: listing.Division, BuyerAccount: req.BuyerAccount,
		BuyerChar: req.Buyer.ID, BuyerName: req.Buyer.Name, Price: req.Price, To: req.To, State: domain.MarketOfferOpen,
		AtMs: s.now().UnixMilli()}
	tx, err := s.beginTxLocked()
	if err != nil {
		return domain.MarketOffer{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.Exec(`INSERT INTO market_offers (listing_id, division, buyer_account, buyer_char, buyer_name, price,
dest, state, at_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, offer.ListingID, offer.Division, offer.BuyerAccount,
		offer.BuyerChar, offer.BuyerName, offer.Price, offer.To, offer.State, offer.AtMs)
	if err != nil {
		return domain.MarketOffer{}, err
	}
	if offer.ID, err = result.LastInsertId(); err != nil {
		return domain.MarketOffer{}, err
	}
	if err := s.commitMarketTxLocked(tx, "market-offer"); err != nil {
		return domain.MarketOffer{}, err
	}
	return offer, nil
}

/*
================
MarketAcceptOffer

The seller takes the offer: a sale at its price to the offering
character. Its pick of bag or warehouse falls back to the other place
when full; both full refuses and leaves the offer open.
================
*/
func (s *Store) MarketAcceptOffer(req domain.MarketAcceptRequest) (domain.MarketSale, domain.MarketOffer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return domain.MarketSale{}, domain.MarketOffer{}, domain.ErrMarketUnavailable
	}
	db := s.syncDBLocked()
	offer, err := readOffer(db, req.OfferID)
	if err != nil {
		return domain.MarketSale{}, domain.MarketOffer{}, err
	}
	listing, err := readListing(db, offer.ListingID)
	if err != nil {
		return domain.MarketSale{}, offer, err
	}
	if listing.SellerAccount != req.SellerAccount {
		return domain.MarketSale{}, offer, domain.ErrMarketNotFound
	}
	if offer.State != domain.MarketOfferOpen {
		return domain.MarketSale{}, offer, domain.ErrMarketOfferGone
	}
	buyer := req.Buyer
	if _, owned := s.ownCharacterLocked(buyer, offer.BuyerAccount); !owned || buyer.ID != offer.BuyerChar {
		return domain.MarketSale{}, offer, s.closeOfferLocked(offer, domain.ErrMarketOfferGone)
	}
	if err := s.canPayLocked(listing.Currency, offer.BuyerAccount, buyer, offer.Price); err != nil {
		if errors.Is(err, domain.ErrMarketFunds) {
			return domain.MarketSale{}, offer, s.closeOfferLocked(offer, domain.ErrMarketBuyerFunds)
		}
		return domain.MarketSale{}, offer, err
	}
	storage, err := readAccountStorage(db, offer.BuyerAccount)
	if err != nil {
		return domain.MarketSale{}, offer, err
	}
	_, warehouseFree := freeStorageSlot(storage)
	bagFree := req.BagSlot >= 0 && !slotTaken(buyer.MissionInventory, req.BagSlot)
	to := offer.To
	switch {
	case to == domain.MarketToBag && !bagFree && warehouseFree:
		to = domain.MarketToWarehouse
	case to == domain.MarketToWarehouse && !warehouseFree && bagFree:
		to = domain.MarketToBag
	case !bagFree && !warehouseFree:
		return domain.MarketSale{}, offer, domain.ErrMarketBuyerFull
	}
	sale, err := s.settleSaleLocked(saleTerms{listing: listing, buyer: offer.BuyerAccount, payer: buyer,
		price: offer.Price, to: to, receiver: buyer, bagSlot: req.BagSlot, rules: req.Rules,
		within: func(tx *sql.Tx) error { return setOfferStateTx(tx, offer.ID, domain.MarketOfferAccepted) }})
	if err != nil {
		return domain.MarketSale{}, offer, err
	}
	offer.State = domain.MarketOfferAccepted
	return sale, offer, nil
}

/*
================
MarketRejectOffer
================
*/
func (s *Store) MarketRejectOffer(sellerAccount string, id int64) (domain.MarketOffer, error) {
	return s.answerOffer(id, domain.MarketOfferRejected, func(offer domain.MarketOffer, listing domain.MarketListing) bool {
		return listing.SellerAccount == sellerAccount
	})
}

/*
================
MarketWithdrawOffer
================
*/
func (s *Store) MarketWithdrawOffer(buyerAccount string, id int64) (domain.MarketOffer, error) {
	return s.answerOffer(id, domain.MarketOfferWithdrawn, func(offer domain.MarketOffer, _ domain.MarketListing) bool {
		return offer.BuyerAccount == buyerAccount
	})
}

/*
================
answerOffer

Moves an open offer to state for the party allowed says may; anyone else
finds no such offer.
================
*/
func (s *Store) answerOffer(id int64, state string, allowed func(domain.MarketOffer, domain.MarketListing) bool) (domain.MarketOffer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return domain.MarketOffer{}, domain.ErrMarketUnavailable
	}
	db := s.syncDBLocked()
	offer, err := readOffer(db, id)
	if err != nil {
		return domain.MarketOffer{}, err
	}
	listing, err := readListing(db, offer.ListingID)
	if err != nil {
		return domain.MarketOffer{}, err
	}
	if !allowed(offer, listing) {
		return domain.MarketOffer{}, domain.ErrMarketNotFound
	}
	if offer.State != domain.MarketOfferOpen {
		return offer, domain.ErrMarketOfferGone
	}
	tx, err := s.beginTxLocked()
	if err != nil {
		return domain.MarketOffer{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := setOfferStateTx(tx, offer.ID, state); err != nil {
		return domain.MarketOffer{}, err
	}
	if err := s.commitMarketTxLocked(tx, "market-offer"); err != nil {
		return domain.MarketOffer{}, err
	}
	offer.State = state
	return offer, nil
}

/*
================
MarketOffers

The account's offers: the newest limit open ones on its listings (to
answer), and its own, open first, then the last limit it made. The
incoming list is bounded because other accounts fill it: unbounded, a
flood of offers would grow the seller's page past what the window
reads. Answering one shows the next.
================
*/
func (s *Store) MarketOffers(account string, limit int) (incoming, outgoing []domain.MarketOffer, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return nil, nil, domain.ErrMarketUnavailable
	}
	db := s.syncDBLocked()
	rows, err := db.Query(`SELECT offer.id, offer.listing_id, offer.division, offer.buyer_account, offer.buyer_char,
offer.buyer_name, offer.price, offer.dest, offer.state, offer.at_ms FROM market_offers offer
JOIN market_listings listing ON listing.id = offer.listing_id
WHERE listing.seller_account = ? AND offer.state = 'open' ORDER BY offer.at_ms DESC, offer.id DESC LIMIT ?`,
		account, limit)
	if err != nil {
		return nil, nil, err
	}
	if incoming, err = scanOffers(rows); err != nil {
		return nil, nil, err
	}
	rows, err = db.Query(`SELECT `+marketOfferColumns+` FROM market_offers WHERE buyer_account = ? AND state = 'open'
UNION ALL
SELECT * FROM (SELECT `+marketOfferColumns+` FROM market_offers WHERE buyer_account = ? AND state != 'open'
ORDER BY at_ms DESC, id DESC LIMIT ?)`, account, account, limit)
	if err != nil {
		return nil, nil, err
	}
	outgoing, err = scanOffers(rows)
	return incoming, outgoing, err
}

/*
================
MarketOfferByID
================
*/
func (s *Store) MarketOfferByID(id int64) (domain.MarketOffer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return domain.MarketOffer{}, domain.ErrMarketUnavailable
	}
	return readOffer(s.syncDBLocked(), id)
}

//============================================================================

/*
================
canPayLocked

Whether the buyer holds price now: the character's gold for a gold
listing, the account's purchased Silk for a Silk one.
================
*/
func (s *Store) canPayLocked(currency, account string, buyer *domain.Character, price int64) error {
	if currency == domain.MarketGold {
		if ledgerGold(buyer) < price {
			return domain.ErrMarketFunds
		}
		return nil
	}
	wallet, err := readMallBalance(s.syncDBLocked(), account)
	if err != nil {
		return err
	}
	if int64(wallet.Silk) < price {
		return domain.ErrMarketFunds
	}
	return nil
}

/*
================
closeOfferLocked

Closes an offer that can no longer be accepted, and answers why.
================
*/
func (s *Store) closeOfferLocked(offer domain.MarketOffer, why error) error {
	tx, err := s.beginTxLocked()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := setOfferStateTx(tx, offer.ID, domain.MarketOfferClosed); err != nil {
		return err
	}
	if err := s.commitMarketTxLocked(tx, "market-offer"); err != nil {
		return err
	}
	return why
}

/*
================
setOfferStateTx

Moves an open offer to state; one that left the open state meanwhile is
ErrMarketOfferGone and the door rolls back.
================
*/
func setOfferStateTx(tx *sql.Tx, id int64, state string) error {
	result, err := tx.Exec("UPDATE market_offers SET state = ? WHERE id = ? AND state = 'open'", state, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return domain.ErrMarketOfferGone
	}
	return nil
}

/*
================
readOffer
================
*/
func readOffer(db *sql.DB, id int64) (domain.MarketOffer, error) {
	rows, err := db.Query(`SELECT `+marketOfferColumns+` FROM market_offers WHERE id = ?`, id)
	if err != nil {
		return domain.MarketOffer{}, err
	}
	offers, err := scanOffers(rows)
	if err != nil {
		return domain.MarketOffer{}, err
	}
	if len(offers) == 0 {
		return domain.MarketOffer{}, domain.ErrMarketOfferGone
	}
	return offers[0], nil
}

/*
================
scanOffers

Every row of an offer query; closes rows.
================
*/
func scanOffers(rows *sql.Rows) ([]domain.MarketOffer, error) {
	defer rows.Close()
	out := []domain.MarketOffer{}
	for rows.Next() {
		var offer domain.MarketOffer
		if err := rows.Scan(&offer.ID, &offer.ListingID, &offer.Division, &offer.BuyerAccount, &offer.BuyerChar,
			&offer.BuyerName, &offer.Price, &offer.To, &offer.State, &offer.AtMs); err != nil {
			return nil, err
		}
		out = append(out, offer)
	}
	return out, rows.Err()
}
