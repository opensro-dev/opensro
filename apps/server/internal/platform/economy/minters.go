/*
===========================================================================

minters.go - the accounts that mint the most gold per hour (#3307)

GET /economy/v1/minters?from=&to= ranks the accounts by the gold they
minted in their busiest UTC hour inside the window, with the window's
total and the count of merchant sales (npc_sale), the denominator a
"sales per entry" reading divides by the census's entries.

Minted gold per character and hour already exists in the snapshot copy:
the two bulk reasons (monster_gold, npc_sale) in the store's hourly
character buckets (economy_ledger_char_hours), every other reason as its
exact economy_ledger row; a bulk row the store's fold lane has not eaten
yet is still a row, and an eaten one is no longer, so the two reads add
up to every change once. Which reasons mint is domain's
(EconomyReasonKindAt); a character's account is its record's, read from
the roster and the deletion archive, so a character deleted after
minting still counts for its account. The hour buckets carry a
character id and no division, so the answer is the whole shard's, as
store.EconomyCharacterHours reads them.

Read from the latest snapshot like every economy route: it can trail
the live store by one snapshot period, and snapshotAt says when.

===========================================================================
*/
package economy

import (
	"database/sql"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"time"

	"opensro.online/server/internal/domain"
)

// maxMinterRows bounds one answer; truncated says when more minted.
const maxMinterRows = 100

// npcSaleReason is the merchant sale's ledger reason.
const npcSaleReason = "npc_sale"

/*
================
AccountMint

One account's minted gold in the window: the total, its busiest UTC
hour (unix seconds) and that hour's mint, the characters that minted,
and how many merchant sales it made.
================
*/
type AccountMint struct {
	Account    string `json:"account"`
	Total      int64  `json:"total"`
	PeakHour   int64  `json:"peakHour"`
	PeakTotal  int64  `json:"peakTotal"`
	Characters int    `json:"characters"`
	NPCSales   int64  `json:"npcSales"`
}

/*
================
Service.serveMinters
================
*/
func (s *Service) serveMinters(w http.ResponseWriter, q url.Values, at time.Time) {
	win, err := parseWindow("1h", q.Get("from"), q.Get("to"), "", s.now())
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	minters, truncated, err := s.readMinters(win)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"snapshotAt": at.Unix(), "minters": minters, "truncated": truncated})
}

/*
================
mintTally

One account's running sums: mint per hour start (unix ms) and the
characters seen minting.
================
*/
type mintTally struct {
	row        AccountMint
	hours      map[int64]int64
	characters map[int64]bool
}

/*
================
Service.readMinters

The window's minting accounts, busiest hour first.
================
*/
func (s *Service) readMinters(win window) ([]AccountMint, bool, error) {
	db, err := openSnapshot(filepath.Join(s.dir, SnapshotFile))
	if err != nil {
		return nil, false, err
	}
	defer db.Close()
	accounts, err := characterAccounts(db)
	if err != nil {
		return nil, false, err
	}
	tallies := map[string]*mintTally{}
	note := func(hourMs, charID int64, reason string, gold, n int64) {
		account := accounts[charID]
		tally := tallies[account]
		if tally == nil {
			tally = &mintTally{row: AccountMint{Account: account}, hours: map[int64]int64{}, characters: map[int64]bool{}}
			tallies[account] = tally
		}
		if reason == npcSaleReason {
			tally.row.NPCSales += n
		}
		if gold <= 0 || domain.EconomyReasonKindAt(reason, true) != domain.EconomyKindMint {
			return
		}
		tally.hours[hourMs] += gold
		tally.characters[charID] = true
		tally.row.Total += gold
	}
	// win.to is the last second of the last bucket (parseWindow).
	fromMs, toMs := win.from*1000, (win.to+1)*1000
	if err := readCharHourMints(db, fromMs, toMs, note); err != nil {
		return nil, false, err
	}
	if err := readLedgerRowMints(db, fromMs, toMs, note); err != nil {
		return nil, false, err
	}
	rows := make([]AccountMint, 0, len(tallies))
	for _, tally := range tallies {
		if tally.row.Total == 0 {
			continue
		}
		for hourMs, gold := range tally.hours {
			if gold > tally.row.PeakTotal || (gold == tally.row.PeakTotal && hourMs/1000 < tally.row.PeakHour) {
				tally.row.PeakTotal, tally.row.PeakHour = gold, hourMs/1000
			}
		}
		tally.row.Characters = len(tally.characters)
		rows = append(rows, tally.row)
	}
	sort.Slice(rows, func(a, b int) bool {
		if rows[a].PeakTotal != rows[b].PeakTotal {
			return rows[a].PeakTotal > rows[b].PeakTotal
		}
		if rows[a].Total != rows[b].Total {
			return rows[a].Total > rows[b].Total
		}
		return rows[a].Account < rows[b].Account
	})
	if len(rows) > maxMinterRows {
		return rows[:maxMinterRows], true, nil
	}
	return rows, false, nil
}

/*
================
characterAccounts

Every character id the snapshot knows to its account: the roster, then
the deletion archive (a record keeps its id and account when deleted).
A character in neither reads as account "", shown rather than dropped.
================
*/
func characterAccounts(db *sql.DB) (map[int64]string, error) {
	accounts := map[int64]string{}
	for _, query := range []string{
		`SELECT json_extract(record, '$.id'), coalesce(json_extract(record, '$.accountId'), '') FROM deleted_characters`,
		`SELECT id, coalesce(json_extract(record, '$.accountId'), '') FROM characters`,
	} {
		if err := readAccounts(db, query, accounts); err != nil {
			return nil, err
		}
	}
	return accounts, nil
}

/*
================
readAccounts

One (id, account) query's rows into accounts; a later query overwrites
an earlier one's id.
================
*/
func readAccounts(db *sql.DB, query string, accounts map[int64]string) error {
	rows, err := db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id sql.NullInt64
		var account string
		if err := rows.Scan(&id, &account); err != nil {
			return err
		}
		if id.Valid {
			accounts[id.Int64] = account
		}
	}
	return rows.Err()
}

/*
================
readCharHourMints

The bulk reasons' character hours inside [fromMs, toMs). A snapshot of
an authority older than the buckets has no table, and so no rows.
================
*/
func readCharHourMints(db *sql.DB, fromMs, toMs int64, note func(int64, int64, string, int64, int64)) error {
	var tables int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema
WHERE type = 'table' AND name = 'economy_ledger_char_hours'`).Scan(&tables); err != nil {
		return err
	}
	if tables == 0 {
		return nil
	}
	return scanMints(db, `SELECT hour_ms, char_id, reason, total, n FROM economy_ledger_char_hours
WHERE hour_ms >= ? AND hour_ms < ?`, fromMs, toMs, note)
}

/*
================
readLedgerRowMints

The exact rows' gains inside [fromMs, toMs), summed per character,
reason and hour; a loss mints nothing and is left out in SQL.
================
*/
func readLedgerRowMints(db *sql.DB, fromMs, toMs int64, note func(int64, int64, string, int64, int64)) error {
	return scanMints(db, `SELECT at_ms - at_ms % 3600000, char_id, reason, sum(gold), count(*) FROM economy_ledger
WHERE at_ms >= ? AND at_ms < ? AND gold > 0 GROUP BY 1, 2, 3`, fromMs, toMs, note)
}

/*
================
scanMints
================
*/
func scanMints(db *sql.DB, query string, fromMs, toMs int64, note func(int64, int64, string, int64, int64)) error {
	rows, err := db.Query(query, fromMs, toMs)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var hourMs, charID, gold, n int64
		var reason string
		if err := rows.Scan(&hourMs, &charID, &reason, &gold, &n); err != nil {
			return err
		}
		note(hourMs, charID, reason, gold, n)
	}
	return rows.Err()
}
