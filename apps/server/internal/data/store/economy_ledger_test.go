/*
===========================================================================

economy_ledger_test.go - every committed gold change is one ledger row

Port-only (economy_ledger.go): the row names the door that made the change,
joins its commit, waits with it through a failed commit, and keeps its own
door's label inside a tick batch.

===========================================================================
*/
package store

import (
	"errors"
	"fmt"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
storedLedgerRow
================
*/
type storedLedgerRow struct {
	charID        int64
	reason, door  string
	gold, balance int64
}

/*
================
ledgerRows

The table's rows, oldest first.
================
*/
func ledgerRows(t *testing.T, s *Store) []storedLedgerRow {
	t.Helper()
	rows, err := s.db.Query("SELECT char_id, reason, door, gold, balance FROM economy_ledger ORDER BY seq")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []storedLedgerRow
	for rows.Next() {
		var row storedLedgerRow
		if err := rows.Scan(&row.charID, &row.reason, &row.door, &row.gold, &row.balance); err != nil {
			t.Fatal(err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

/*
================
doorRows

The rows after every fixture's creation and seed rows.
================
*/
func doorRows(t *testing.T, s *Store) []storedLedgerRow {
	t.Helper()
	var out []storedLedgerRow
	for _, row := range ledgerRows(t, s) {
		if row.door != "create-character" && row.door != "test-seed-gold" {
			out = append(out, row)
		}
	}
	return out
}

/*
================
newLedgerCharacter

A stored character holding gold, given through a door the ledger does not
know (so its own row reads "unattributed").
================
*/
func newLedgerCharacter(t *testing.T, s *Store, name string, gold int64) *domain.Character {
	t.Helper()
	c := seededCharacter()
	c.Name = name
	if err := s.CreateCharacter(testDivision, "ledger-account-"+name, c); err != nil {
		t.Fatal(err)
	}
	s.UpdateCharacter(c, "test-seed-gold", func() bool {
		c.Gold = &gold
		return true
	})
	return c
}

/*
================
setGoldIn

A door that leaves c with balance.
================
*/
func setGoldIn(s *Store, c *domain.Character, label string, balance int64) {
	s.UpdateCharacter(c, label, func() bool {
		c.Gold = &balance
		return true
	})
}

/*
================
TestDoorGoldChangesBecomeLedgerRows

Each door's change is one row: its reason by door and direction, the door
without any name or id after it, the signed change and the new balance. A
door that moves no gold writes none.
================
*/
func TestDoorGoldChangesBecomeLedgerRows(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	c := newLedgerCharacter(t, s, "LedgerOne", 1000)
	setGoldIn(s, c, "shop-transaction", 700)
	setGoldIn(s, c, "shop-transaction", 750)
	setGoldIn(s, c, "pickup-gold 1/2", 800)
	s.UpdateCharacter(c, "move", func() bool {
		c.Level = new(int64)
		return true
	})
	want := []storedLedgerRow{
		{c.ID, "shop_buy", "shop-transaction", -300, 700},
		{c.ID, "npc_sale", "shop-transaction", 50, 750},
		{c.ID, "monster_gold", "pickup-gold", 50, 800},
	}
	got := ledgerRows(t, s)
	starting := *seededCharacter().Gold
	if len(got) < 2 || got[0].reason != "starting_gold" || got[0].door != "create-character" || got[0].gold != starting ||
		got[1].reason != domain.EconomyReasonUnattributed || got[1].door != "test-seed-gold" || got[1].balance != 1000 {
		t.Fatalf("creation and seed rows %+v", got)
	}
	got = got[2:]
	if len(got) != len(want) {
		t.Fatalf("rows %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	counters := s.EconomyCounters()
	if counters["gold.shop_buy.loss"] != 300 || counters["gold.npc_sale.gain"] != 50 || counters["gold.monster_gold.gain"] != 50 {
		t.Fatalf("counters %v", counters)
	}
}

/*
================
TestTransferWritesBothSides

A two-character door (an exchange) writes each side's change.
================
*/
func TestTransferWritesBothSides(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	a := newLedgerCharacter(t, s, "LedgerA", 500)
	b := newLedgerCharacter(t, s, "LedgerB", 100)
	s.UpdateCharacters([]*domain.Character{a, b}, "exchange", func() bool {
		ag, bg := int64(300), int64(300)
		a.Gold, b.Gold = &ag, &bg
		return true
	})
	got := doorRows(t, s)
	if len(got) != 2 {
		t.Fatalf("exchange rows %+v", got)
	}
	sides := map[int64]int64{}
	for _, row := range got {
		if row.reason != "exchange" || row.door != "exchange" {
			t.Fatalf("exchange row %+v", row)
		}
		sides[row.charID] = row.gold
	}
	if sides[a.ID] != -200 || sides[b.ID] != 200 {
		t.Fatalf("exchange sides %v", sides)
	}
}

/*
================
TestTradeSettlementNamesSellerAndShares

A trade-goods sale pays the seller and the party in one settlement; the
door names the seller, so the seller's row is its credit and the others'
are party shares. A thief's sale at the den is its own reason.
================
*/
func TestTradeSettlementNamesSellerAndShares(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	seller := newLedgerCharacter(t, s, "LedgerSeller", 100)
	share := newLedgerCharacter(t, s, "LedgerShare", 100)
	pay := func(door string) map[int64]string {
		before := len(ledgerRows(t, s))
		s.UpdateTrade([]*domain.Character{seller, share}, fmt.Sprintf("%s %d", door, seller.ID), func(*domain.TradeRewardPool) bool {
			sg, hg := *seller.Gold+500, *share.Gold+50
			seller.Gold, share.Gold = &sg, &hg
			return true
		})
		reasons := map[int64]string{}
		for _, row := range ledgerRows(t, s)[before:] {
			if row.door != door {
				t.Fatalf("%s row %+v names door %q", door, row, row.door)
			}
			reasons[row.charID] = row.reason
		}
		return reasons
	}
	if got := pay("trade-sale"); got[seller.ID] != "trade_sale_credit" || got[share.ID] != "trade_party_share" || len(got) != 2 {
		t.Fatalf("trade sale reasons %v", got)
	}
	if got := pay("thief-sale"); got[seller.ID] != "thief_den_sale" || got[share.ID] != "trade_party_share" || len(got) != 2 {
		t.Fatalf("thief sale reasons %v", got)
	}
}

/*
================
TestTickBatchKeepsEachDoorsLabel

Deferred doors share one commit under the batch's label, but each row
names the door that made it, and none is written before the flush.
================
*/
func TestTickBatchKeepsEachDoorsLabel(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	c := newLedgerCharacter(t, s, "LedgerTick", 100)
	s.BeginTickBatch()
	s.UpdateCharacterDeferred(c, "pickup-gold", func() bool {
		gold := int64(160)
		c.Gold = &gold
		return true
	})
	s.UpdateCharacterDeferred(c, "npc-repair", func() bool {
		gold := int64(150)
		c.Gold = &gold
		return true
	})
	if rows := doorRows(t, s); len(rows) != 0 {
		t.Fatalf("rows before the flush: %+v", rows)
	}
	if stats := s.FlushTickBatch(); !stats.Flushed || stats.Err != nil {
		t.Fatalf("flush %+v", stats)
	}
	rows := doorRows(t, s)
	if len(rows) != 2 || rows[0].reason != "monster_gold" || rows[0].gold != 60 ||
		rows[1].reason != "repair" || rows[1].gold != -10 || rows[1].balance != 150 {
		t.Fatalf("batched rows %+v", rows)
	}
}

/*
================
TestFailedCommitKeepsTheRowPending

A failed commit writes neither the record nor its row, and counts nothing;
the next successful commit writes the row once.
================
*/
func TestFailedCommitKeepsTheRowPending(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	c := newLedgerCharacter(t, s, "LedgerFail", 100)
	s.FailCommits(errors.New("disk failure"))
	setGoldIn(s, c, "npc-repair", 90)
	if rows := doorRows(t, s); len(rows) != 0 || s.EconomyCounters()["gold.repair.loss"] != 0 {
		t.Fatalf("a failed commit wrote %+v", rows)
	}
	s.FailCommits(nil)
	s.UpdateCharacter(c, "move", func() bool {
		c.Level = new(int64)
		return true
	})
	s.UpdateCharacter(c, "move", func() bool {
		level := int64(1)
		c.Level = &level
		return true
	})
	rows := doorRows(t, s)
	if len(rows) != 1 || rows[0].door != "npc-repair" || rows[0].gold != -10 || s.EconomyCounters()["gold.repair.loss"] != 10 {
		t.Fatalf("after recovery %+v", rows)
	}
}

/*
================
TestOwnTransactionDoorWritesItsRow

The warehouse commits in its own transaction; its row joins that
transaction, and a later door does not count the change again.
================
*/
func TestOwnTransactionDoorWritesItsRow(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	c := newLedgerCharacter(t, s, "LedgerStore", 500)
	if _, err := s.TransactStorage(c, func(next *domain.Character, storage *domain.AccountStorage) error {
		remaining := *next.Gold - 120
		next.Gold = &remaining
		storage.Gold += 120
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.UpdateCharacter(c, "move", func() bool {
		c.Level = new(int64)
		return true
	})
	rows := doorRows(t, s)
	if len(rows) != 1 || rows[0].reason != "storage_deposit" || rows[0].gold != -120 || rows[0].balance != 380 {
		t.Fatalf("warehouse rows %+v", rows)
	}
}

/*
================
TestLedgerUpgradeAddsTheTable

A schema 24 authority at layout 9 upgrades to 25 at layout 10 with an
empty ledger, its records untouched, and the next door writes rows.
================
*/
func TestLedgerUpgradeAddsTheTable(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	c := newLedgerCharacter(t, s, "LedgerUp", 40)
	var before string
	if err := s.db.QueryRow("SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, c.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	s.Close()
	downgradeToLayout9(t, dir)
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, preLedgerVersion)
	if backup, err := UpgradeAuthority(dir, true); err != nil || backup == "" {
		t.Fatalf("upgrade %q: %v", backup, err)
	}
	reopened := openTest(t, dir, newTestClock())
	var after string
	if err := reopened.db.QueryRow("SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, c.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after || len(ledgerRows(t, reopened)) != 0 {
		t.Fatal("the upgrade rewrote the character or kept ledger rows")
	}
	var live *domain.Character
	reopened.ReadCharacters(testDivision, func(characters []*domain.Character) {
		for _, candidate := range characters {
			if candidate.ID == c.ID {
				live = candidate
			}
		}
	})
	setGoldIn(reopened, live, "pickup-gold", 70)
	if rows := ledgerRows(t, reopened); len(rows) != 1 || rows[0].gold != 30 {
		t.Fatalf("rows after the upgrade %+v", rows)
	}
	reopened.Close()
}

/*
================
TestMostWantedBountyIsATransfer

The bounty taken from a wanted victim (door most-wanted-bounty) is a
transfer on both sides: the ledger mints nothing for it.
================
*/
func TestMostWantedBountyIsATransfer(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	killer := newLedgerCharacter(t, s, "LedgerKiller", 100)
	victim := newLedgerCharacter(t, s, "LedgerVictim", 500)
	s.UpdateCharacters([]*domain.Character{killer, victim}, "most-wanted-bounty", func() bool {
		kg, vg := int64(400), int64(200)
		killer.Gold, victim.Gold = &kg, &vg
		return true
	})
	got := doorRows(t, s)
	if len(got) != 2 {
		t.Fatalf("bounty rows %+v", got)
	}
	for _, row := range got {
		if row.reason != "most_wanted_transfer" || domain.EconomyReasonKind(row.reason) != domain.EconomyKindTransfer {
			t.Fatalf("bounty row %+v", row)
		}
	}
}
