/*
===========================================================================

tickbatch_test.go - the tick's batched commit keeps the disk consistent

The disk witness is a second connection to the database: what it reads is
what a process killed at that moment would boot from, since a kill loses
only what was never committed.

===========================================================================
*/
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
)

/*
================
batchFixture

A store with two characters, each with 100 gold on disk.
================
*/
func batchFixture(t *testing.T) (*Store, *enterworld.Character, *enterworld.Character) {
	t.Helper()
	s := openTest(t, t.TempDir(), newTestClock())
	a, b := &enterworld.Character{Name: "BatchA", Gold: int64Ptr(100)}, &enterworld.Character{Name: "BatchB", Gold: int64Ptr(100)}
	for _, c := range []*enterworld.Character{a, b} {
		if err := s.CreateCharacter(testDivision, "batch-account", c); err != nil {
			t.Fatal(err)
		}
	}
	return s, a, b
}

/*
================
diskOpen

The witness connection; closed with the test.
================
*/
func diskOpen(t *testing.T, s *Store) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", s.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

/*
================
diskGold

The gold a reboot would load for one character.
================
*/
func diskGold(t *testing.T, db *sql.DB, name string) int64 {
	t.Helper()
	var record string
	if err := db.QueryRow("SELECT record FROM characters WHERE division = ? AND name_lower = ?", testDivision, strings.ToLower(name)).Scan(&record); err != nil {
		t.Fatal(err)
	}
	var c enterworld.Character
	if err := json.Unmarshal([]byte(record), &c); err != nil {
		t.Fatal(err)
	}
	if c.Gold == nil {
		return 0
	}
	return *c.Gold
}

/*
================
diskGround

The ground rows a reboot would load.
================
*/
func diskGround(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM ground_items").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

/*
================
setGold
================
*/
func setGold(c *enterworld.Character, gold int64) func() bool {
	return func() bool {
		c.Gold = int64Ptr(gold)
		return true
	}
}

/*
================
TestTickBatchCommitsDeferredRowsOnceAtTheTicksEnd

Three deferred updates over two characters: nothing reaches the disk
until the flush, the flush is one transaction, and the later update to
the same character wins.
================
*/
func TestTickBatchCommitsDeferredRowsOnceAtTheTicksEnd(t *testing.T) {
	s, a, b := batchFixture(t)
	disk := diskOpen(t, s)
	before := s.Health().Commits
	s.BeginTickBatch()
	s.UpdateCharacterDeferred(a, "test", setGold(a, 1))
	s.UpdateCharactersDeferred([]*enterworld.Character{a, b}, "test", func() bool {
		b.Gold = int64Ptr(2)
		return true
	})
	s.UpdateCharacterDeferred(a, "test", setGold(a, 3))
	if s.Health().Commits != before || diskGold(t, disk, "BatchA") != 100 || diskGold(t, disk, "BatchB") != 100 {
		t.Fatal("a deferred update reached the disk before the tick's end")
	}
	stats := s.FlushTickBatch()
	if stats.Err != nil || !stats.Flushed || stats.Deferred != 3 || stats.Rows != 2 {
		t.Fatalf("flush = %+v, want 3 deferred calls over 2 rows", stats)
	}
	if got := s.Health().Commits - before; got != 1 {
		t.Fatalf("%d transactions for one tick, want 1", got)
	}
	if diskGold(t, disk, "BatchA") != 3 || diskGold(t, disk, "BatchB") != 2 {
		t.Fatal("the flush did not land the last update of each row")
	}
	// The next tick has nothing deferred and runs no transaction.
	s.BeginTickBatch()
	if stats := s.FlushTickBatch(); stats.Flushed || s.Health().Commits-before != 1 {
		t.Fatalf("an empty tick flushed: %+v", stats)
	}
}

/*
================
TestTickBatchKillLosesAtMostTheTick

A kill before the flush boots the state the previous tick committed; the
immediate doors keep committing at once inside the batch, and take the
deferred rows with them.
================
*/
func TestTickBatchKillLosesAtMostTheTick(t *testing.T) {
	s, a, b := batchFixture(t)
	disk := diskOpen(t, s)
	s.BeginTickBatch()
	s.UpdateCharacterDeferred(a, "tick-1", setGold(a, 10))
	s.FlushTickBatch()
	s.BeginTickBatch()
	s.UpdateCharacterDeferred(a, "tick-2", setGold(a, 20))
	// Killed here: the disk holds tick 1.
	if diskGold(t, disk, "BatchA") != 10 {
		t.Fatalf("a kill mid-tick would boot gold %d, want tick 1's 10", diskGold(t, disk, "BatchA"))
	}
	// A handler's immediate door commits now, with the deferred row.
	s.UpdateCharacter(b, "handler", setGold(b, 7))
	if diskGold(t, disk, "BatchB") != 7 || diskGold(t, disk, "BatchA") != 20 {
		t.Fatal("an immediate commit inside the batch did not write the whole dirty set")
	}
	if stats := s.FlushTickBatch(); stats.Flushed {
		t.Fatalf("the flush repeated a commit the handler already made: %+v", stats)
	}
}

/*
================
TestTickBatchPickupLandsBothOrNeither

A pet's ground pickup takes the item off the ground and puts its gold in
the bag in one deferred update. A kill at any point leaves both or
neither on disk: never a duplicate, never a loss.
================
*/
func TestTickBatchPickupLandsBothOrNeither(t *testing.T) {
	s, a, _ := batchFixture(t)
	disk := diskOpen(t, s)
	registry := grounditem.NewRegistry()
	s.AttachGround(registry)
	item := registry.Add(testDivision, grounditem.Item{RefObjID: 1, Codename: "ITEM_ETC_GOLD_01", TypeFlags: 0x00C0, Position: grounditem.Point{RegionID: 0x6b4f}})
	s.Mutate("drop", nil)
	if diskGold(t, disk, "BatchA") != 100 || diskGround(t, disk) != 1 {
		t.Fatal("fixture")
	}
	s.BeginTickBatch()
	s.UpdateCharacterDeferred(a, "pet-pickup", func() bool {
		if _, ok := registry.Remove(testDivision, item.Gid); !ok {
			return false
		}
		a.Gold = int64Ptr(150)
		return true
	})
	if diskGold(t, disk, "BatchA") != 100 || diskGround(t, disk) != 1 {
		t.Fatal("killed before the flush: the pickup must be on disk in full or not at all")
	}
	s.FlushTickBatch()
	if diskGold(t, disk, "BatchA") != 150 || diskGround(t, disk) != 0 {
		t.Fatal("after the flush: the item must be off the ground and its gold in the bag")
	}
}

/*
================
TestTickBatchFlushesBeforeEveryWriteOutsideTheDirtySet

Every path that writes outside the dirty set, in its own transaction or
with a direct statement, lands the deferred rows first: a fortress war's
end retires a battle rank (deferred) and then saves the fortress, and the
disk must never hold the fortress without the rank.
================
*/
func TestTickBatchFlushesBeforeEveryWriteOutsideTheDirtySet(t *testing.T) {
	writes := []struct {
		name  string
		write func(s *Store) error
	}{
		{"silk grant (own transaction)", func(s *Store) error {
			_, err := s.GrantSilk("batch-account", 5)
			return err
		}},
		{"fortress", func(s *Store) error {
			return s.Fortresses().SaveFortress(testDivision, domain.FortressRecord{FortressID: 1, GuildID: 7})
		}},
		{"fortress request", func(s *Store) error {
			return s.Fortresses().SaveFortressRequest(testDivision, domain.FortressRequestRecord{FortressID: 1, GuildID: 9}, true)
		}},
		{"fortress structure", func(s *Store) error {
			return s.Fortresses().SaveFortressStructure(testDivision, domain.FortressStructureRecord{FortressID: 1,
				EventStructID: 85, RefObjID: 19536, OwnerGuildID: 7, HP: 120, State: 1}, true)
		}},
		{"alliance", func(s *Store) error {
			return s.Alliances().SaveAlliance(testDivision, domain.AllianceRecord{AllianceID: 7, Guilds: [domain.AllianceSlots]int64{7, 9}}, true)
		}},
		{"beta silk starter", func(s *Store) error {
			_, err := s.GrantBetaSilkStarter("batch-account", 10)
			return err
		}},
	}
	for _, w := range writes {
		t.Run(w.name, func(t *testing.T) {
			s, a, _ := batchFixture(t)
			disk := diskOpen(t, s)
			s.BeginTickBatch()
			s.UpdateCharacterDeferred(a, "tick", setGold(a, 42))
			if err := w.write(s); err != nil {
				t.Fatal(err)
			}
			if diskGold(t, disk, "BatchA") != 42 {
				t.Fatal("a write outside the dirty set landed ahead of the batch's deferred row")
			}
			s.FlushTickBatch()
		})
	}
}

/*
================
TestTickBatchFailedFlushKeepsItsRows

A failed flush keeps the rows dirty and counts on health (readiness reads
it); the next flush writes them.
================
*/
func TestTickBatchFailedFlushKeepsItsRows(t *testing.T) {
	s, a, _ := batchFixture(t)
	disk := diskOpen(t, s)
	s.BeginTickBatch()
	s.UpdateCharacterDeferred(a, "tick", setGold(a, 9))
	s.mu.Lock()
	s.commitFail = errors.New("disk full")
	s.mu.Unlock()
	if stats := s.FlushTickBatch(); stats.Err == nil || !stats.Flushed {
		t.Fatalf("flush = %+v, want a failed transaction", stats)
	}
	if s.Health().FailedWrites != 1 || diskGold(t, disk, "BatchA") != 100 {
		t.Fatal("a failed flush must show on health and leave the disk as it was")
	}
	s.mu.Lock()
	s.commitFail = nil
	s.mu.Unlock()
	s.BeginTickBatch()
	if stats := s.FlushTickBatch(); stats.Err != nil || !stats.Flushed {
		t.Fatalf("retry flush = %+v", stats)
	}
	if s.Health().FailedWrites != 0 || diskGold(t, disk, "BatchA") != 9 {
		t.Fatal("the next flush did not write the kept row")
	}
}

/*
================
TestDeferredDoorOutsideABatchCommitsAtOnce
================
*/
func TestDeferredDoorOutsideABatchCommitsAtOnce(t *testing.T) {
	s, a, _ := batchFixture(t)
	disk := diskOpen(t, s)
	s.UpdateCharacterDeferred(a, "no-batch", setGold(a, 5))
	if diskGold(t, disk, "BatchA") != 5 {
		t.Fatal("a deferred door with no batch open waited")
	}
}

/*
================
diskRecord

The stored record bytes a reboot would load for one character.
================
*/
func diskRecord(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	var record string
	if err := db.QueryRow("SELECT record FROM characters WHERE division = ? AND name_lower = ?", testDivision, strings.ToLower(name)).Scan(&record); err != nil {
		t.Fatal(err)
	}
	return record
}

/*
================
storedPairRecords

One mutation sequence - a pair update on the first two characters,
then a single update on a third the pair never touched - landed
through the immediate doors, or through the deferred doors inside one
tick batch. Returns the three stored records.
================
*/
func storedPairRecords(t *testing.T, deferred bool) (string, string, string) {
	t.Helper()
	s, a, b := batchFixture(t)
	c := &enterworld.Character{Name: "BatchC", Gold: int64Ptr(100)}
	if err := s.CreateCharacter(testDivision, "batch-account", c); err != nil {
		t.Fatal(err)
	}
	disk := diskOpen(t, s)
	pair := func() bool {
		a.Gold = int64Ptr(41)
		b.Gold = int64Ptr(42)
		return true
	}
	if deferred {
		s.BeginTickBatch()
		if !s.UpdateCharactersDeferred([]*enterworld.Character{a, b}, "test", pair) {
			t.Fatal("the deferred pair door refused")
		}
		if !s.UpdateCharacterDeferred(c, "test", setGold(c, 77)) {
			t.Fatal("the deferred door refused")
		}
		if diskGold(t, disk, "BatchA") != 100 || diskGold(t, disk, "BatchB") != 100 || diskGold(t, disk, "BatchC") != 100 {
			t.Fatal("a deferred update reached the disk before the tick's end")
		}
		if stats := s.FlushTickBatch(); stats.Err != nil {
			t.Fatalf("flush: %v", stats.Err)
		}
	} else {
		if !s.UpdateCharacters([]*enterworld.Character{a, b}, "test", pair) {
			t.Fatal("the immediate pair door refused")
		}
		if !s.UpdateCharacter(c, "test", setGold(c, 77)) {
			t.Fatal("the immediate door refused")
		}
	}
	return diskRecord(t, disk, "BatchA"), diskRecord(t, disk, "BatchB"), diskRecord(t, disk, "BatchC")
}

/*
================
TestDeferredDoorStoresTheImmediateDoorsBytes

The tick conversion's behaviour pin: deferring a door changes when its
rows land, never what lands. The same mutations through the deferred
doors and one tick-end flush store byte-identical records to the
immediate doors.
================
*/
func TestDeferredDoorStoresTheImmediateDoorsBytes(t *testing.T) {
	nowA, nowB, nowC := storedPairRecords(t, false)
	laterA, laterB, laterC := storedPairRecords(t, true)
	if nowA != laterA || nowB != laterB || nowC != laterC {
		t.Fatalf("stored bytes differ:\nimmediate: %s / %s / %s\ndeferred:  %s / %s / %s", nowA, nowB, nowC, laterA, laterB, laterC)
	}
}
