/*
===========================================================================

trade_history_test.go - a trade's rows ride its commit and page newest first

Port-only (trade_history.go): the two rows of a committed trade are durable
with its commit, a failed commit keeps them pending and the next commit
writes each exactly once, and the account's read pages them newest first
with a cursor, scopes them to the account's own characters and reports a
hidden counterparty as hidden now.

===========================================================================
*/
package store

import (
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/testsupport/tier"
)

// tradeTestStack is the one stack the tests trade: a potion stack.
var tradeTestStack = domain.TradeItem{RefObjID: 3630, Quantity: 5}

/*
================
tradeRecordsBetween

The two rows of one exchange between first and second: first paid gold and
gave the stack, second received both. Each row names the other party.
================
*/
func tradeRecordsBetween(first, second *domain.Character, gold uint64) []domain.TradeRecord {
	paid := domain.TradeRecord{
		Division: testDivision, AccountID: first.AccountID, CharID: first.ID, CharName: first.Name,
		OtherAccountID: second.AccountID, OtherCharID: second.ID, OtherName: second.Name,
		Kind: domain.TradeKindExchange, GoldPaid: gold, ItemsGiven: []domain.TradeItem{tradeTestStack},
	}
	received := domain.TradeRecord{
		Division: testDivision, AccountID: second.AccountID, CharID: second.ID, CharName: second.Name,
		OtherAccountID: first.AccountID, OtherCharID: first.ID, OtherName: first.Name,
		Kind: domain.TradeKindExchange, GoldReceived: gold, ItemsReceived: []domain.TradeItem{tradeTestStack},
	}
	return []domain.TradeRecord{paid, received}
}

/*
================
tradeRowCount

The rows the table holds, read from an open handle.
================
*/
func tradeRowCount(t *testing.T, s *Store) int {
	t.Helper()
	var rows int
	if err := s.syncDBLocked().QueryRow("SELECT count(*) FROM trade_history").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

/*
================
noteTradeInDoor

Runs one trade's door the way the exchange does: the closure notes the rows
and reports success, so the rows join the door's own commit. The note's error
is returned, not fataled, because the closure runs under the store lock.
================
*/
func noteTradeInDoor(s *Store, first, second *domain.Character, records []domain.TradeRecord, label string) (bool, error) {
	var noteErr error
	committed := s.UpdateCharacters([]*domain.Character{first, second}, label, func() bool {
		noteErr = s.NoteTrade(records)
		return noteErr == nil
	})
	return committed, noteErr
}

/*
================
TestTradeHistoryRidesTheCommit

A trade's rows are durable exactly when its commit is. A failed commit keeps
both rows pending and writes nothing; the next commit writes them once, and
no later commit writes them again. The rows survive a restart.
================
*/
func TestTradeHistoryRidesTheCommit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	first, second := seededCharacter(), seededCharacter()
	first.Name, second.Name = "TradeFirst", "TradeSecond"
	if err := s.CreateCharacter(testDivision, "trade-account-a", first); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateCharacter(testDivision, "trade-account-b", second); err != nil {
		t.Fatal(err)
	}
	records := tradeRecordsBetween(first, second, 500)

	s.FailCommits(errors.New("disk failure"))
	if committed, err := noteTradeInDoor(s, first, second, records, "trade-failed"); !committed || err != nil {
		t.Fatalf("the trade's door = %v, %v; the failed commit is the store's, not the door's", committed, err)
	}
	s.FailCommits(nil)
	if got := tradeRowCount(t, s); got != 0 {
		t.Fatalf("a failed commit wrote %d trade rows", got)
	}
	if pending := s.snapshotPendingTrades(); len(pending) != 2 {
		t.Fatalf("pending rows after a failed commit = %d, want the trade's two", len(pending))
	}

	// Any door's next successful commit carries the pending rows, once.
	if !s.UpdateCharacter(first, "trade-next", func() bool { return true }) {
		t.Fatal("the next door refused")
	}
	if got := tradeRowCount(t, s); got != 2 {
		t.Fatalf("the retry wrote %d trade rows, want the trade's two", got)
	}
	if pending := s.snapshotPendingTrades(); len(pending) != 0 {
		t.Fatalf("written rows stayed pending: %d", len(pending))
	}
	s.UpdateCharacter(second, "trade-later", func() bool { return true })
	if got := tradeRowCount(t, s); got != 2 {
		t.Fatalf("a later commit doubled the trade: %d rows", got)
	}

	if committed, err := noteTradeInDoor(s, first, second, records, "trade-second"); !committed || err != nil {
		t.Fatalf("second trade = %v, %v", committed, err)
	}
	if got := tradeRowCount(t, s); got != 4 {
		t.Fatalf("two trades hold %d rows, want 4", got)
	}

	s.Close()
	reopened := openTest(t, dir, clock)
	if got := tradeRowCount(t, reopened); got != 4 {
		t.Fatalf("trade rows lost across a restart: %d rows", got)
	}
	page, err := reopened.TradeHistory(testDivision, first.AccountID, "", nil, 10)
	if err != nil || len(page.Entries) != 2 || page.More {
		t.Fatalf("first account's history after restart = %+v, %v", page, err)
	}
	row := page.Entries[0].Record
	if row.Kind != domain.TradeKindExchange || row.GoldPaid != 500 || row.GoldReceived != 0 ||
		len(row.ItemsGiven) != 1 || row.ItemsGiven[0] != tradeTestStack || len(row.ItemsReceived) != 0 {
		t.Fatalf("recorded row = %+v", row)
	}
}

/*
================
TestTradeHistoryDropsRowsPastRetention

A commit drops rows older than tradeHistoryRetention and keeps younger ones;
one commit drops at most tradeHistoryPruneBatch, and the rest drain over the
next commits.
================
*/
func TestTradeHistoryDropsRowsPastRetention(t *testing.T) {
	t.Parallel()

	clock := newTestClock()
	s := openTest(t, t.TempDir(), clock)
	first, second := seededCharacter(), seededCharacter()
	first.Name, second.Name = "KeepFirst", "KeepSecond"
	if err := s.CreateCharacter(testDivision, "keep-account-a", first); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateCharacter(testDivision, "keep-account-b", second); err != nil {
		t.Fatal(err)
	}
	records := tradeRecordsBetween(first, second, 100)
	if _, err := noteTradeInDoor(s, first, second, records, "old-trade"); err != nil {
		t.Fatal(err)
	}
	clock.Advance(tradeHistoryRetention - time.Hour)
	if _, err := noteTradeInDoor(s, first, second, records, "young-trade"); err != nil {
		t.Fatal(err)
	}
	if got := tradeRowCount(t, s); got != 4 {
		t.Fatalf("inside the window the table holds %d rows, want 4", got)
	}

	clock.Advance(2 * time.Hour)
	s.UpdateCharacter(first, "after-window", func() bool { return true })
	if got := tradeRowCount(t, s); got != 2 {
		t.Fatalf("past the old trade's window the table holds %d rows, want the young trade's 2", got)
	}

	// A backlog larger than one batch drains over several commits.
	backlog := tradeHistoryPruneBatch + 10
	for i := 0; i < backlog; i++ {
		if _, err := s.syncDBLocked().Exec(`INSERT INTO trade_history (at_ms, division, account_id, char_id, char_name, other_account_id, other_char_id, other_name, kind, gold_paid, gold_received, commission, items_given, items_received)
VALUES (0, ?, 'keep-account-a', 1, 'KeepFirst', 'keep-account-b', 2, 'KeepSecond', 'exchange', 0, 0, 0, '[]', '[]')`, testDivision); err != nil {
			t.Fatal(err)
		}
	}
	s.UpdateCharacter(first, "drain-one", func() bool { return true })
	if got := tradeRowCount(t, s); got != 2+10 {
		t.Fatalf("one commit left %d rows, want one batch dropped (12 left)", got)
	}
	s.UpdateCharacter(first, "drain-two", func() bool { return true })
	if got := tradeRowCount(t, s); got != 2 {
		t.Fatalf("the second commit left %d rows, want the young trade's 2", got)
	}
}

/*
================
TestNoteTradeRefusesWholeOrNothing

A note the store cannot hold refuses both rows together: an unknown kind, a
party without an account, or gold above the column's range leaves nothing
pending, so the trade's history never holds one party's row alone.
================
*/
func TestNoteTradeRefusesWholeOrNothing(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	first, second := seededCharacter(), seededCharacter()
	first.AccountID, second.AccountID = "note-account-a", "note-account-b"
	good := tradeRecordsBetween(first, second, 10)
	bad := []struct {
		name   string
		mutate func([]domain.TradeRecord)
	}{
		{"unknown kind", func(rows []domain.TradeRecord) { rows[1].Kind = "barter" }},
		{"party without an account", func(rows []domain.TradeRecord) { rows[0].AccountID = "" }},
		{"gold above the column", func(rows []domain.TradeRecord) { rows[1].GoldReceived = math.MaxUint64 }},
	}
	for _, tc := range bad {
		records := append([]domain.TradeRecord(nil), good...)
		tc.mutate(records)
		if err := s.NoteTrade(records); err == nil {
			t.Fatalf("%s: the note was accepted", tc.name)
		}
		if pending := s.snapshotPendingTrades(); len(pending) != 0 {
			t.Fatalf("%s: a refused note left %d rows pending", tc.name, len(pending))
		}
	}
	if err := s.NoteTrade(good); err != nil {
		t.Fatal(err)
	}
	if pending := s.snapshotPendingTrades(); len(pending) != 2 {
		t.Fatalf("a valid note left %d rows pending, want 2", len(pending))
	}
}

/*
================
TestTradeHistoryPagesNewestFirst

The account's history is newest first, ties broken by sequence, and paged
with the (atMs, seq) cursor of the last row. A character filter keeps that
character's rows; a character the account does not own answers
ErrTradeCharacterNotOwned exactly as an unknown name does. A counterparty
hidden now reads as hidden, and a limit of zero refuses.
================
*/
func TestTradeHistoryPagesNewestFirst(t *testing.T) {
	t.Parallel()

	clock := newTestClock()
	s := openTest(t, t.TempDir(), clock)
	one, two, rival := seededCharacter(), seededCharacter(), seededCharacter()
	one.Name, two.Name, rival.Name = "PageOne", "PageTwo", "PageRival"
	for _, pair := range []struct {
		account   string
		character *domain.Character
	}{{"pager", one}, {"pager", two}, {"rival", rival}} {
		if err := s.CreateCharacter(testDivision, pair.account, pair.character); err != nil {
			t.Fatal(err)
		}
	}
	trade := func(first, second *domain.Character) {
		t.Helper()
		if committed, err := noteTradeInDoor(s, first, second, tradeRecordsBetween(first, second, 1), "page-trade"); !committed || err != nil {
			t.Fatalf("trade = %v, %v", committed, err)
		}
	}
	trade(one, rival) // rows seq 1, 2 at t0
	clock.Advance(time.Second)
	trade(one, two) // rows seq 3, 4 at t1
	trade(two, one) // rows seq 5, 6 at t1 too
	clock.Advance(time.Second)
	trade(two, rival) // rows seq 7, 8 at t2

	seqs := func(page domain.TradePage) []int64 {
		var out []int64
		for _, entry := range page.Entries {
			out = append(out, entry.Seq)
		}
		return out
	}
	var cursor *domain.TradeCursor
	want := [][]int64{{7, 6}, {5, 4}, {3, 1}}
	more := []bool{true, true, false}
	for i := range want {
		page, err := s.TradeHistory(testDivision, "pager", "", cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		if got := seqs(page); len(got) != len(want[i]) || got[0] != want[i][0] || got[len(got)-1] != want[i][len(want[i])-1] || page.More != more[i] {
			t.Fatalf("page %d = %v (more %v), want %v (more %v)", i, got, page.More, want[i], more[i])
		}
		last := page.Entries[len(page.Entries)-1]
		cursor = &domain.TradeCursor{AtMs: last.Record.AtMs, Seq: last.Seq}
	}

	page, err := s.TradeHistory(testDivision, "pager", "PageTwo", nil, 10)
	if err != nil || len(seqs(page)) != 3 || seqs(page)[0] != 7 || seqs(page)[1] != 5 || seqs(page)[2] != 4 {
		t.Fatalf("PageTwo's history = %v, %v", seqs(page), err)
	}
	if _, err := s.TradeHistory(testDivision, "pager", "PageRival", nil, 10); !errors.Is(err, ErrTradeCharacterNotOwned) {
		t.Fatalf("a rival's character read = %v, want ErrTradeCharacterNotOwned", err)
	}
	if _, err := s.TradeHistory(testDivision, "pager", "Nobody", nil, 10); !errors.Is(err, ErrTradeCharacterNotOwned) {
		t.Fatalf("an unknown character read = %v, want ErrTradeCharacterNotOwned", err)
	}
	if _, err := s.TradeHistory(testDivision, "pager", "", nil, 0); err == nil {
		t.Fatal("a zero limit was accepted")
	}
	if page, err := s.TradeHistory(testDivision, "pager", "", &domain.TradeCursor{AtMs: 0, Seq: 0}, 10); err != nil || len(page.Entries) != 0 || page.More {
		t.Fatalf("a cursor before the first row = %+v, %v", page, err)
	}

	if !s.UpdateCharacter(rival, "page-hide", func() bool {
		rival.PublicHidden = true
		return true
	}) {
		t.Fatal("hiding the rival was refused")
	}
	page, err = s.TradeHistory(testDivision, "pager", "", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range page.Entries {
		hidden := entry.Record.OtherCharID == rival.ID
		if entry.OtherHidden != hidden {
			t.Fatalf("row %d: OtherHidden = %v, want %v (other %q)", entry.Seq, entry.OtherHidden, hidden, entry.Record.OtherName)
		}
	}

	// A reaped counterparty reads as hidden: its choice can no longer be
	// checked and its name is free for someone else.
	gone := seededCharacter()
	gone.Name = "PageGone"
	if err := s.CreateCharacter(testDivision, "gone", gone); err != nil {
		t.Fatal(err)
	}
	trade(one, gone)
	s.MutateCharacter(gone, "delete-reserve PageGone", func() {
		gone.DeletePending = true
		gone.DeleteReservedAt = clock.Now().Add(-8 * 24 * time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")
	})
	if reaped := s.ReapMaturedDeletions(); len(reaped) != 1 {
		t.Fatalf("reaped %v, want PageGone", reaped)
	}
	page, err = s.TradeHistory(testDivision, "pager", "PageOne", nil, 1)
	if err != nil || len(page.Entries) != 1 || page.Entries[0].Record.OtherName != "PageGone" || !page.Entries[0].OtherHidden {
		t.Fatalf("the reaped counterparty's row = %+v, %v; want it hidden", page.Entries, err)
	}
}

/*
================
TestTradeHistoryUpgradeAddsTable

A schema 44 authority at layout 12 upgrades to 45/13 with its records
untouched; the upgrade adds the empty trade_history table. A second upgrade
has nothing to do.
================
*/
func TestTradeHistoryUpgradeAddsTable(t *testing.T) {
	tier.Long(t, "a schema-44 layout-12 authority upgrades to 45/13 with an empty trade_history table")

	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	c := seededCharacter()
	if err := s.CreateCharacter(testDivision, "trade-upgrade-account", c); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := s.syncDBLocked().QueryRow("SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, c.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	s.Close()
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, preTradeHistoryVersion)
	rewriteDatabaseMeta(t, dir, metaKeyLayoutVersion, preTradeHistoryLayoutVersion)
	db, err := openDB(filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP TABLE trade_history"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if backup, err := UpgradeAuthority(dir, true); err != nil || backup == "" {
		t.Fatalf("upgrade %q: %v", backup, err)
	}
	reopened := openTest(t, dir, newTestClock())
	var after string
	if err := reopened.syncDBLocked().QueryRow("SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, c.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("the upgrade rewrote the character")
	}
	var tables int
	if err := reopened.syncDBLocked().QueryRow("SELECT count(*) FROM sqlite_schema WHERE name = 'trade_history'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 1 {
		t.Fatal("the upgrade did not add trade_history")
	}
	if page, err := reopened.TradeHistory(testDivision, "trade-upgrade-account", "", nil, 10); err != nil || len(page.Entries) != 0 || page.More {
		t.Fatalf("upgraded history = %+v, %v", page, err)
	}
	reopened.Close()
	if _, err := UpgradeAuthority(dir, true); !errors.Is(err, ErrAuthorityCurrent) {
		t.Fatalf("second upgrade = %v, want ErrAuthorityCurrent", err)
	}
}
