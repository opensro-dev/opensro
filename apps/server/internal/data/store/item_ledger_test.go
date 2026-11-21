/*
===========================================================================

item_ledger_test.go - every item entering or leaving a character is
recorded at its commit (#3307)

The item ledger (item_ledger.go) names the door, the item and the signed
count; a move inside the character writes nothing; the bulk doors land
in hourly buckets; rows join their commit, wait through a failed one,
keep their own door's label inside a tick batch, and ride an
own-transaction door's transaction.

===========================================================================
*/
package store

import (
	"errors"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
)

/*
================
storedItemRow
================
*/
type storedItemRow struct {
	charID int64
	door   string
	item   string
	count  int64
}

/*
================
itemLedgerRows

The exact rows after every fixture's creation rows, oldest first.
================
*/
func itemLedgerRows(t *testing.T, s *Store) []storedItemRow {
	t.Helper()
	rows, err := s.syncDBLocked().Query("SELECT char_id, door, item, count FROM item_ledger WHERE door != 'create-character' ORDER BY seq")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []storedItemRow
	for rows.Next() {
		var row storedItemRow
		if err := rows.Scan(&row.charID, &row.door, &row.item, &row.count); err != nil {
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
newItemCharacter

A stored character holding the fixture's one sword.
================
*/
func newItemCharacter(t *testing.T, s *Store, name string) *domain.Character {
	t.Helper()
	c := seededCharacter()
	c.Name = name
	if err := s.CreateCharacter(testDivision, "item-account-"+name, c); err != nil {
		t.Fatal(err)
	}
	return c
}

/*
================
potionRow
================
*/
func potionRow(slot, count int64) domain.InventoryRow {
	return domain.InventoryRow{Slot: slot, RefObjID: 4, Codename: "ITEM_ETC_HP_POTION_01", StackCount: count}
}

/*
================
TestItemLedgerCreationRecordsTheStartingItems
================
*/
func TestItemLedgerCreationRecordsTheStartingItems(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	c := newItemCharacter(t, s, "ItemBorn")
	rows, err := s.ItemLedger(c.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Door != "create-character" || rows[0].Item != "ITEM_EU_SWORD_01_A" || rows[0].Count != 1 {
		t.Fatalf("creation rows %+v", rows)
	}
}

/*
================
TestItemLedgerDoorRows

A grant writes a gain under its door (the door word only), a destroy a
loss; a slot move and a door that touches no item write nothing.
================
*/
func TestItemLedgerDoorRows(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	c := newItemCharacter(t, s, "ItemDoors")
	s.UpdateCharacter(c, "quest-reward 7", func() bool {
		c.MissionInventory = append(c.MissionInventory, potionRow(14, 5))
		return true
	})
	s.UpdateCharacter(c, "inv-move", func() bool {
		c.MissionInventory[1].Slot = 20
		return true
	})
	s.UpdateCharacter(c, "move", func() bool {
		c.Level = new(int64)
		return true
	})
	s.UpdateCharacter(c, "ground-drop", func() bool {
		c.MissionInventory = c.MissionInventory[1:]
		return true
	})
	want := []storedItemRow{
		{c.ID, "quest-reward", "ITEM_ETC_HP_POTION_01", 5},
		{c.ID, "ground-drop", "ITEM_EU_SWORD_01_A", -1},
	}
	if got := itemLedgerRows(t, s); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("rows %+v, want %+v", got, want)
	}
}

/*
================
TestItemLedgerBulkDoorBuckets

A consumption door's changes fold into the character's hourly bucket and
keep no row.
================
*/
func TestItemLedgerBulkDoorBuckets(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	c := newItemCharacter(t, s, "ItemBulk")
	s.UpdateCharacter(c, "operator-grant-items", func() bool {
		c.MissionInventory = append(c.MissionInventory, potionRow(14, 5))
		return true
	})
	for range 2 {
		s.UpdateCharacter(c, "item-use", func() bool {
			c.MissionInventory[1].StackCount--
			return true
		})
	}
	if rows := itemLedgerRows(t, s); len(rows) != 1 || rows[0].door != "operator-grant-items" {
		t.Fatalf("a bulk door kept rows: %+v", rows)
	}
	hours, err := s.ItemLedgerHours(c.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hours) != 1 || hours[0].Door != "item-use" || hours[0].Item != "ITEM_ETC_HP_POTION_01" || hours[0].Total != -2 || hours[0].N != 2 {
		t.Fatalf("bulk buckets %+v", hours)
	}
}

/*
================
TestItemLedgerTransferWritesBothSides

One door moving an item between two characters writes each side.
================
*/
func TestItemLedgerTransferWritesBothSides(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	a := newItemCharacter(t, s, "ItemGiver")
	b := newItemCharacter(t, s, "ItemTaker")
	s.UpdateCharacters([]*domain.Character{a, b}, "exchange", func() bool {
		sword := a.MissionInventory[0]
		a.MissionInventory = nil
		sword.Slot = 14
		b.MissionInventory = append(b.MissionInventory, sword)
		return true
	})
	got := itemLedgerRows(t, s)
	byChar := map[int64]storedItemRow{}
	for _, row := range got {
		byChar[row.charID] = row
	}
	if len(got) != 2 || byChar[a.ID].count != -1 || byChar[b.ID].count != 1 || byChar[a.ID].door != "exchange" {
		t.Fatalf("exchange rows %+v", got)
	}
}

/*
================
TestItemLedgerTickBatchKeepsEachDoorsLabel

Deferred doors share one commit, but each row names its own door, and
none is written before the flush.
================
*/
func TestItemLedgerTickBatchKeepsEachDoorsLabel(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	c := newItemCharacter(t, s, "ItemTick")
	s.BeginTickBatch()
	s.UpdateCharacterDeferred(c, "quest-reward", func() bool {
		c.MissionInventory = append(c.MissionInventory, potionRow(14, 3))
		return true
	})
	s.UpdateCharacterDeferred(c, "ground-drop", func() bool {
		c.MissionInventory = c.MissionInventory[1:]
		return true
	})
	if rows := itemLedgerRows(t, s); len(rows) != 0 {
		t.Fatalf("rows before the flush: %+v", rows)
	}
	if stats := s.FlushTickBatch(); !stats.Flushed || stats.Err != nil {
		t.Fatalf("flush %+v", stats)
	}
	rows := itemLedgerRows(t, s)
	if len(rows) != 2 || rows[0].door != "quest-reward" || rows[0].count != 3 || rows[1].door != "ground-drop" || rows[1].count != -1 {
		t.Fatalf("batched rows %+v", rows)
	}
}

/*
================
TestItemLedgerFailedCommitKeepsTheRowsPending

A failed commit writes no row; the next successful commit writes it
once.
================
*/
func TestItemLedgerFailedCommitKeepsTheRowsPending(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	c := newItemCharacter(t, s, "ItemFail")
	s.FailCommits(errors.New("disk failure"))
	s.UpdateCharacter(c, "quest-reward", func() bool {
		c.MissionInventory = append(c.MissionInventory, potionRow(14, 2))
		return true
	})
	if rows := itemLedgerRows(t, s); len(rows) != 0 {
		t.Fatalf("a failed commit wrote %+v", rows)
	}
	s.FailCommits(nil)
	for range 2 {
		s.UpdateCharacter(c, "move", func() bool {
			c.Level = new(int64)
			return true
		})
	}
	if rows := itemLedgerRows(t, s); len(rows) != 1 || rows[0].door != "quest-reward" || rows[0].count != 2 {
		t.Fatalf("after recovery %+v", rows)
	}
}

/*
================
TestItemLedgerOwnTransactionDoorWritesItsRow

A warehouse deposit commits in its own transaction: its row names the
door, and a later door does not count the change again. A deposit whose
commit fails leaves no row and no moved baseline.
================
*/
func TestItemLedgerOwnTransactionDoorWritesItsRow(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	c := newItemCharacter(t, s, "ItemStore")
	deposit := func(next *domain.Character, storage *domain.AccountStorage) error {
		row := next.MissionInventory[0]
		next.MissionInventory = nil
		row.Slot = 0
		storage.Rows = append(storage.Rows, row)
		return nil
	}
	s.FailCommits(errors.New("disk failure"))
	if _, err := s.TransactStorage(c, deposit); err == nil {
		t.Fatal("a failing deposit committed")
	}
	s.FailCommits(nil)
	if rows := itemLedgerRows(t, s); len(rows) != 0 {
		t.Fatalf("a failed deposit wrote %+v", rows)
	}
	if _, err := s.TransactStorage(c, deposit); err != nil {
		t.Fatal(err)
	}
	s.UpdateCharacter(c, "move", func() bool {
		c.Level = new(int64)
		return true
	})
	rows := itemLedgerRows(t, s)
	if len(rows) != 1 || rows[0].door != "storage-transfer" || rows[0].item != "ITEM_EU_SWORD_01_A" || rows[0].count != -1 {
		t.Fatalf("warehouse rows %+v", rows)
	}
}

/*
================
TestItemLedgerDeletionWritesNoRows

A matured deletion archives the character with what it held: the
archive keeps the record's bytes and a restore brings the items back,
so the reap writes no item row, and no door stays open.
================
*/
func TestItemLedgerDeletionWritesNoRows(t *testing.T) {
	t.Parallel()

	clock := newTestClock()
	s := openTest(t, t.TempDir(), clock)
	c := newItemCharacter(t, s, "ItemGone")
	s.MutateCharacter(c, "delete-reserve ItemGone", func() {
		c.DeletePending = true
		c.DeleteReservedAt = clock.Now().Add(-8 * 24 * time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")
	})
	if reaped := s.ReapMaturedDeletions(); len(reaped) != 1 {
		t.Fatalf("reaped %v, want the matured character", reaped)
	}
	if rows := itemLedgerRows(t, s); len(rows) != 0 {
		t.Fatalf("a deletion wrote item rows %+v", rows)
	}
	if open := openDoorCount(s); open != 0 {
		t.Fatalf("%d snapshots left open after the reap", open)
	}
}
