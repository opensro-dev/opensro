/*
===========================================================================

trades_test.go - the account's trade history read: its guards, its paging
and its privacy (trades.go)

The handler's contract with its source: a bearer guard and no-store answer,
a 404 for a character the account does not own, the newest-first page with a
cursor, a capped limit, a hidden counterparty reading as "a hunter", and a
row sequence that never reaches the wire.

===========================================================================
*/
package publicstats

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
tradesGet

One GET of the trade history with an optional bearer token; the recorder
keeps the status, the headers and the body.
================
*/
func tradesGet(s *Service, path, token string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	s.Handler().ServeHTTP(rec, request)
	return rec
}

/*
================
tradesAnswer

The page a trade-history answer decodes to.
================
*/
func tradesAnswer(t *testing.T, rec *httptest.ResponseRecorder) OwnTradesResponse {
	t.Helper()
	var page OwnTradesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body.String(), err)
	}
	return page
}

/*
================
TestOwnTradesNeedTheWriteTokenAndAWiredSource

The trade history is a bearer read like the characters list: no token, or a
wrong one, answers 401 with no-store; a service without the write token, or
without the source, leaves the route unserved.
================
*/
func TestOwnTradesNeedTheWriteTokenAndAWiredSource(t *testing.T) {
	served := privacyService(newFixture(), testWriteToken)
	served.src.Trades = func(string, string, *domain.TradeCursor, int) (domain.TradePage, error) {
		return domain.TradePage{}, nil
	}
	if rec := tradesGet(served, "/v1/accounts/alpha/trades", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token answered %d", rec.Code)
	}
	if rec := tradesGet(served, "/v1/accounts/alpha/trades", strings.Repeat("x", 32)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token answered %d", rec.Code)
	}
	rec := tradesGet(served, "/v1/accounts/alpha/trades", testWriteToken)
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("own token answered %d with Cache-Control %q", rec.Code, rec.Header().Get("Cache-Control"))
	}

	unwired := privacyService(newFixture(), testWriteToken)
	if rec := tradesGet(unwired, "/v1/accounts/alpha/trades", testWriteToken); rec.Code != http.StatusNotFound {
		t.Fatalf("an unwired source answered %d, want 404", rec.Code)
	}
	tokenless := privacyService(newFixture(), "")
	tokenless.src.Trades = served.src.Trades
	if rec := tradesGet(tokenless, "/v1/accounts/alpha/trades", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("a service without a write token answered %d, want 404", rec.Code)
	}
}

/*
================
TestOwnTradesRefusesAForeignCharacter

A character the account does not own answers 404, the same answer an unknown
name gets, and the source is asked for the path's account and the named
character only.
================
*/
func TestOwnTradesRefusesAForeignCharacter(t *testing.T) {
	s := privacyService(newFixture(), testWriteToken)
	s.src.Trades = func(account, character string, _ *domain.TradeCursor, _ int) (domain.TradePage, error) {
		if account != "alpha" || (character != "" && character != "Kekw") {
			return domain.TradePage{}, ErrNotOwned
		}
		return domain.TradePage{}, nil
	}
	if rec := tradesGet(s, "/v1/accounts/alpha/trades?character=Kekw", testWriteToken); rec.Code != http.StatusOK {
		t.Fatalf("an owned character answered %d", rec.Code)
	}
	if rec := tradesGet(s, "/v1/accounts/alpha/trades?character=Rival", testWriteToken); rec.Code != http.StatusNotFound {
		t.Fatalf("a foreign character answered %d, want 404", rec.Code)
	}
	if rec := tradesGet(s, "/v1/accounts/beta/trades", testWriteToken); rec.Code != http.StatusNotFound {
		t.Fatalf("another account answered %d, want 404", rec.Code)
	}
}

/*
================
TestOwnTradesPagesNewestFirstWithACursor

The page is the source's order, newest first. next names the last row's
(atMs, seq) and the page after it starts strictly older than that; the last
page's next is null. The limit defaults to 20 and is capped at 50, and a
malformed limit or cursor refuses with 400 before the source is asked.
================
*/
func TestOwnTradesPagesNewestFirstWithACursor(t *testing.T) {
	s := privacyService(newFixture(), testWriteToken)
	rows := []domain.TradeEntry{
		{Seq: 9, Record: domain.TradeRecord{AtMs: 3000, CharName: "Kekw", Kind: domain.TradeKindExchange}},
		{Seq: 8, Record: domain.TradeRecord{AtMs: 2000, CharName: "Kekw", Kind: domain.TradeKindExchange}},
		{Seq: 7, Record: domain.TradeRecord{AtMs: 2000, CharName: "Kekw", Kind: domain.TradeKindExchange}},
		{Seq: 6, Record: domain.TradeRecord{AtMs: 1000, CharName: "Kekw", Kind: domain.TradeKindExchange}},
	}
	var lastLimit int
	s.src.Trades = func(_, _ string, before *domain.TradeCursor, limit int) (domain.TradePage, error) {
		lastLimit = limit
		var out []domain.TradeEntry
		for _, row := range rows {
			if before != nil && !(row.Record.AtMs < before.AtMs || (row.Record.AtMs == before.AtMs && row.Seq < before.Seq)) {
				continue
			}
			out = append(out, row)
		}
		more := len(out) > limit
		if more {
			out = out[:limit]
		}
		return domain.TradePage{Entries: out, More: more}, nil
	}

	first := tradesAnswer(t, tradesGet(s, "/v1/accounts/alpha/trades?limit=2", testWriteToken))
	if lastLimit != 2 || len(first.Trades) != 2 || first.Trades[0].AtMs != 3000 || first.Trades[1].AtMs != 2000 ||
		first.Next == nil || *first.Next != "2000.8" {
		t.Fatalf("first page = %+v (next %v, limit %d)", first.Trades, first.Next, lastLimit)
	}
	second := tradesAnswer(t, tradesGet(s, "/v1/accounts/alpha/trades?limit=2&before="+*first.Next, testWriteToken))
	if len(second.Trades) != 2 || second.Trades[0].AtMs != 2000 || second.Trades[1].AtMs != 1000 || second.Next != nil {
		t.Fatalf("second page = %+v (next %v)", second.Trades, second.Next)
	}

	tradesGet(s, "/v1/accounts/alpha/trades", testWriteToken)
	if lastLimit != 20 {
		t.Fatalf("an absent limit asked for %d rows, want 20", lastLimit)
	}
	tradesGet(s, "/v1/accounts/alpha/trades?limit=500", testWriteToken)
	if lastLimit != 50 {
		t.Fatalf("a limit of 500 asked for %d rows, want the cap of 50", lastLimit)
	}

	for _, query := range []string{"limit=0", "limit=-3", "limit=many", "before=2000", "before=x.8", "before=2000.", "before=-1.2"} {
		if rec := tradesGet(s, "/v1/accounts/alpha/trades?"+query, testWriteToken); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s answered %d, want 400", query, rec.Code)
		}
	}
}

/*
================
TestOwnTradesMasksAHiddenCounterparty

A counterparty whose character is hidden reads as "a hunter", and its name
never reaches the answer; a counterparty still listed keeps its name. The
row's sequence stays off the wire: the cursor carries it.
================
*/
func TestOwnTradesMasksAHiddenCounterparty(t *testing.T) {
	s := privacyService(newFixture(), testWriteToken)
	s.src.Trades = func(string, string, *domain.TradeCursor, int) (domain.TradePage, error) {
		hidden := domain.TradeEntry{Seq: 2, OtherHidden: true, Record: domain.TradeRecord{
			AtMs: 10, CharName: "Mine", OtherName: "Shadow", Kind: domain.TradeKindExchange, GoldPaid: 5}}
		open := domain.TradeEntry{Seq: 1, Record: domain.TradeRecord{
			AtMs: 9, CharName: "Mine", OtherName: "Daylight", Kind: domain.TradeKindExchange}}
		return domain.TradePage{Entries: []domain.TradeEntry{hidden, open}}, nil
	}
	rec := tradesGet(s, "/v1/accounts/alpha/trades", testWriteToken)
	if strings.Contains(rec.Body.String(), "Shadow") {
		t.Fatalf("a hidden counterparty's name reached the answer: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"seq"`) {
		t.Fatalf("the row sequence reached the answer: %s", rec.Body.String())
	}
	page := tradesAnswer(t, rec)
	if len(page.Trades) != 2 || page.Trades[0].OtherName != HiddenName || page.Trades[1].OtherName != "Daylight" {
		t.Fatalf("counterparties = %+v", page.Trades)
	}
	if page.Trades[0].GoldPaid != 5 || page.Trades[0].Character != "Mine" || page.Trades[0].Kind != domain.TradeKindExchange {
		t.Fatalf("the masked row lost its own fields: %+v", page.Trades[0])
	}
}
