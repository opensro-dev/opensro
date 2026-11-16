/*
===========================================================================

trades.go - the account's own trade history (port-only)

	GET /v1/accounts/{id}/trades?character=&before=&limit=  -> 200 trades

A bearer-guarded read beside /v1/accounts/{id}/characters (privacy.go): the
same listener, the same write token, the same no-store answer. The rows come
from the store's trade_history table, never from the roster snapshot, newest
first.

character names one of the account's characters; absent or empty means every
character of the account. A character the account does not own answers 404,
as an unknown one does. before is "<atMs>.<seq>", the next value of the page
before (absent is the first page). limit defaults to tradePageDefault and is
capped at tradePageMax. A counterparty whose character is hidden reads as
HiddenName, the privacy choice the community site applies everywhere
else.

Port-only, not native: the original has no public read API.

===========================================================================
*/
package publicstats

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"opensro.online/server/internal/domain"
)

const (
	// tradePageDefault is the page size when limit is absent.
	tradePageDefault = 20
	// tradePageMax caps a page: a request for more gets this many.
	tradePageMax = 50
)

/*
================
installTrades

Adds the trade history read to the token-guarded routes (installPrivacy
calls it only when the write token is configured and Trades is wired).
================
*/
func (s *Service) installTrades(mux *http.ServeMux, digest [sha256.Size]byte) {
	mux.HandleFunc("GET /v1/accounts/{id}/trades", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r, digest) {
			return
		}
		s.ownTrades(w, r)
	})
}

/*
================
ownTrades

One page of the account's trades. A malformed limit or cursor refuses with
400 before the store is asked; a character the account does not own answers
404 (ErrNotOwned); a store failure answers 503.
================
*/
func (s *Service) ownTrades(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, ok := parseTradeLimit(query.Get("limit"))
	if !ok {
		writeStatus(w, http.StatusBadRequest, "limit must be a positive whole number")
		return
	}
	before, ok := parseTradeCursor(query.Get("before"))
	if !ok {
		writeStatus(w, http.StatusBadRequest, "before must be <atMs>.<seq>")
		return
	}
	page, err := s.src.Trades(r.PathValue("id"), query.Get("character"), before, limit)
	if err != nil {
		if errors.Is(err, ErrNotOwned) {
			writeStatus(w, http.StatusNotFound, "not found")
			return
		}
		writeStatus(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	body, err := json.Marshal(ownTradesResponse(page))
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "encode")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

/*
================
parseTradeLimit

The page size: tradePageDefault when absent, at most tradePageMax. A value that
is not a positive whole number refuses.
================
*/
func parseTradeLimit(raw string) (int, bool) {
	if raw == "" {
		return tradePageDefault, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 {
		return 0, false
	}
	return min(limit, tradePageMax), true
}

/*
================
parseTradeCursor

The page boundary a previous page's next names: "<atMs>.<seq>", both whole
and non-negative. Absent is the first page (nil); anything else refuses.
================
*/
func parseTradeCursor(raw string) (*domain.TradeCursor, bool) {
	if raw == "" {
		return nil, true
	}
	atText, seqText, found := strings.Cut(raw, ".")
	if !found {
		return nil, false
	}
	atMs, errAt := strconv.ParseInt(atText, 10, 64)
	seq, errSeq := strconv.ParseInt(seqText, 10, 64)
	if errAt != nil || errSeq != nil || atMs < 0 || seq < 0 {
		return nil, false
	}
	return &domain.TradeCursor{AtMs: atMs, Seq: seq}, true
}

/*
================
ownTradesResponse

The page as the wire shows it. next names the last row when older rows remain
and is null on the last page. The store's row sequence never reaches the wire:
the cursor carries it.
================
*/
func ownTradesResponse(page domain.TradePage) OwnTradesResponse {
	out := OwnTradesResponse{Trades: make([]OwnTrade, 0, len(page.Entries))}
	for _, entry := range page.Entries {
		out.Trades = append(out.Trades, ownTrade(entry))
	}
	if page.More && len(page.Entries) > 0 {
		last := page.Entries[len(page.Entries)-1]
		next := strconv.FormatInt(last.Record.AtMs, 10) + "." + strconv.FormatInt(last.Seq, 10)
		out.Next = &next
	}
	return out
}

/*
================
ownTrade

One stored row as the account sees it. The counterparty's name is replaced
while its character is hidden.
================
*/
func ownTrade(entry domain.TradeEntry) OwnTrade {
	other := entry.Record.OtherName
	if entry.OtherHidden {
		other = HiddenName
	}
	return OwnTrade{
		AtMs:          entry.Record.AtMs,
		Character:     entry.Record.CharName,
		Kind:          entry.Record.Kind,
		OtherName:     other,
		GoldPaid:      entry.Record.GoldPaid,
		GoldReceived:  entry.Record.GoldReceived,
		Commission:    entry.Record.Commission,
		ItemsGiven:    ownTradeItems(entry.Record.ItemsGiven),
		ItemsReceived: ownTradeItems(entry.Record.ItemsReceived),
	}
}

/*
================
ownTradeItems

The item stacks of one side, as the wire names them.
================
*/
func ownTradeItems(items []domain.TradeItem) []OwnTradeItem {
	out := make([]OwnTradeItem, 0, len(items))
	for _, item := range items {
		out = append(out, OwnTradeItem{RefObjID: item.RefObjID, Quantity: item.Quantity})
	}
	return out
}
