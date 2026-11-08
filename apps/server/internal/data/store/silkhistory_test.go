/*
===========================================================================

silkhistory_test.go - silk movements recorded with the wallet, listed by period

===========================================================================
*/
package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/testsupport/tier"
)

/*
================
silkHistoryRow
================
*/
type silkHistoryRow struct {
	Kind     int
	Silk     uint32
	GiftSilk uint32
	Points   uint32
	AtMs     int64
}

/*
================
silkHistoryRows
================
*/
func silkHistoryRows(t *testing.T, s *Store, account string) []silkHistoryRow {
	t.Helper()
	found, err := s.syncDBLocked().Query("SELECT kind, silk, gift_silk, points, at_ms FROM silk_history WHERE account_id = ? ORDER BY seq", account)
	if err != nil {
		t.Fatal(err)
	}
	defer found.Close()
	rows := []silkHistoryRow{}
	for found.Next() {
		var row silkHistoryRow
		if err := found.Scan(&row.Kind, &row.Silk, &row.GiftSilk, &row.Points, &row.AtMs); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	if err := found.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

/*
================
TestSilkHistoryRecordsPurchaseInCommit

A purchase's use row is durable exactly when its debit is: a failed commit
records nothing, a points-only purchase charges no silk and records
nothing, and every character of the account reads the same history.
================
*/
func TestSilkHistoryRecordsPurchaseInCommit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	first := seededCharacter()
	second := seededCharacter()
	second.Name = "HistSibling"
	for _, character := range []*domain.Character{first, second} {
		if err := s.CreateCharacter(testDivision, "history-account", character); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.syncDBLocked().Exec("INSERT INTO mall_accounts VALUES (?, ?, ?, ?)", "history-account", 100, 20, 10); err != nil {
		t.Fatal(err)
	}
	grant := func(rows []domain.InventoryRow) ([]domain.InventoryRow, error) { return rows, nil }
	s.FailCommits(errors.New("disk failure"))
	if _, err := s.PurchaseMall(first, domain.MallBalance{Silk: 30, GiftSilk: 2, Points: 3}, grant); err == nil {
		t.Fatal("accepted a failed durable commit")
	}
	s.FailCommits(nil)
	if rows := silkHistoryRows(t, s, "history-account"); len(rows) != 0 {
		t.Fatalf("failed commit recorded history: %+v", rows)
	}
	if _, err := s.PurchaseMall(first, domain.MallBalance{Points: 3}, grant); err != nil {
		t.Fatal(err)
	}
	if rows := silkHistoryRows(t, s, "history-account"); len(rows) != 0 {
		t.Fatalf("points-only purchase recorded history: %+v", rows)
	}
	if _, err := s.PurchaseMall(first, domain.MallBalance{Silk: 30, GiftSilk: 2, Points: 3}, grant); err != nil {
		t.Fatal(err)
	}
	recorded := silkHistoryRows(t, s, "history-account")
	if len(recorded) != 1 || recorded[0].Kind != silkHistoryUse || recorded[0].Silk != 30 || recorded[0].GiftSilk != 2 ||
		recorded[0].Points != 3 || recorded[0].AtMs != clock.now.UnixMilli() {
		t.Fatalf("recorded use = %+v", recorded)
	}
	rows, total, err := s.SilkHistory(second, 1, 10)
	if err != nil || len(rows) != 1 || rows[0].Charged != 32 || total != 32 {
		t.Fatalf("sibling history = %+v, %d, %v", rows, total, err)
	}
	s.Close()
	reopened := openTest(t, dir, clock)
	rows, total, err = reopened.SilkHistory(reopened.Characters().CharactersForDivision(testDivision)[0], 1, 10)
	if err != nil || len(rows) != 1 || rows[0].Charged != 32 || total != 32 {
		t.Fatalf("history lost after restart: %+v, %d, %v", rows, total, err)
	}
}

/*
================
TestSilkHistoryPeriodsLimitAndTotal

Periods are whole days ending now; the total charges the whole period
even when the row list is capped, and charges never list as uses.
================
*/
func TestSilkHistoryPeriodsLimitAndTotal(t *testing.T) {
	t.Parallel()

	clock := newTestClock()
	s := openTest(t, t.TempDir(), clock)
	character := seededCharacter()
	if err := s.CreateCharacter(testDivision, "period-account", character); err != nil {
		t.Fatal(err)
	}
	if _, err := s.syncDBLocked().Exec("INSERT INTO mall_accounts VALUES (?, ?, ?, ?)", "period-account", 1000, 0, 0); err != nil {
		t.Fatal(err)
	}
	grant := func(rows []domain.InventoryRow) ([]domain.InventoryRow, error) { return rows, nil }
	buy := func(silk uint32) {
		t.Helper()
		if _, err := s.PurchaseMall(character, domain.MallBalance{Silk: silk}, grant); err != nil {
			t.Fatal(err)
		}
	}
	buy(10)
	clock.Advance(2 * 24 * time.Hour)
	buy(20)
	clock.Advance(6 * 24 * time.Hour)
	buy(40)
	if _, err := s.GrantSilk("period-account", 500); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		days  int
		limit int
		want  []uint32
		total uint32
	}{
		{1, 10, []uint32{40}, 40},
		{7, 10, []uint32{40, 20}, 60},
		{30, 10, []uint32{40, 20, 10}, 70},
		{90, 2, []uint32{40, 20}, 70},
	}
	for _, tc := range cases {
		rows, total, err := s.SilkHistory(character, tc.days, tc.limit)
		if err != nil {
			t.Fatal(err)
		}
		if total != tc.total || len(rows) != len(tc.want) {
			t.Fatalf("period %d: rows %+v total %d, want %v/%d", tc.days, rows, total, tc.want, tc.total)
		}
		for i, want := range tc.want {
			if rows[i].Charged != want {
				t.Fatalf("period %d row %d charged %d, want %d", tc.days, i, rows[i].Charged, want)
			}
		}
	}
	if _, _, err := s.SilkHistory(character, 0, 10); err == nil {
		t.Fatal("accepted a zero-day period")
	}
	impostor := *character
	if _, _, err := s.SilkHistory(&impostor, 30, 10); err == nil {
		t.Fatal("accepted a character outside store ownership")
	}
}

/*
================
TestSilkHistoryRecordsCharges

Credits record as charges with what actually landed: the operator grant,
the beta credit trimmed by its cap, and the starter exactly once.
================
*/
func TestSilkHistoryRecordsCharges(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	if _, err := s.GrantSilk("charge-account", 50); err != nil {
		t.Fatal(err)
	}
	if _, credited, err := s.CreditBetaSilk("charge-account", 100, 120); err != nil || !credited {
		t.Fatalf("beta credit: %v %v", credited, err)
	}
	if granted, err := s.GrantBetaSilkStarter("starter-account", 25); err != nil || !granted {
		t.Fatalf("starter: %v %v", granted, err)
	}
	if granted, err := s.GrantBetaSilkStarter("starter-account", 25); err != nil || granted {
		t.Fatalf("starter repeated: %v %v", granted, err)
	}
	charges := silkHistoryRows(t, s, "charge-account")
	if len(charges) != 2 || charges[0].Kind != silkHistoryCharge || charges[0].Silk != 50 ||
		charges[1].Kind != silkHistoryCharge || charges[1].Silk != 70 {
		t.Fatalf("charges = %+v", charges)
	}
	starter := silkHistoryRows(t, s, "starter-account")
	if len(starter) != 1 || starter[0].Kind != silkHistoryCharge || starter[0].Silk != 25 {
		t.Fatalf("starter charges = %+v", starter)
	}
}

/*
================
TestSilkHistoryUpgradeAddsTable

A schema 38 authority at layout 11 upgrades to 39/12 with its records
untouched; the upgrade adds the empty silk_history table. A second
upgrade has nothing to do.
================
*/
func TestSilkHistoryUpgradeAddsTable(t *testing.T) {
	tier.Long(t, "a schema-38 layout-11 authority upgrades to 39/12 with an empty silk_history table")

	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	c := seededCharacter()
	if err := s.CreateCharacter(testDivision, "account", c); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := s.syncDBLocked().QueryRow("SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, c.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	s.Close()
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, preSilkHistoryVersion)
	rewriteDatabaseMeta(t, dir, metaKeyLayoutVersion, preSilkHistoryLayoutVersion)
	db, err := openDB(filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP TABLE silk_history"); err != nil {
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
	if err := reopened.syncDBLocked().QueryRow("SELECT count(*) FROM sqlite_schema WHERE name = 'silk_history'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 1 {
		t.Fatal("the upgrade did not add silk_history")
	}
	reopenedCharacter := reopened.Characters().CharactersForDivision(testDivision)[0]
	if rows, total, err := reopened.SilkHistory(reopenedCharacter, 30, 10); err != nil || len(rows) != 0 || total != 0 {
		t.Fatalf("upgraded history = %+v, %d, %v", rows, total, err)
	}
	reopened.Close()
	if _, err := UpgradeAuthority(dir, true); !errors.Is(err, ErrAuthorityCurrent) {
		t.Fatalf("second upgrade = %v, want ErrAuthorityCurrent", err)
	}
}
