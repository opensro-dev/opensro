/*
===========================================================================

ground_changes_test.go - the commit writes only the changed ground rows

The registry stamps each gid it changes and the commit persists those
rows, acknowledging them only once its transaction has committed
(ground_changes.go, #2040). These pins hold the table equal to the
registry through every registry mutator, a vanished division, a moved
gid counter, a rolled-back tick flush and a crash between the commit
and its acknowledgement - and hold a one-drop change to one row.

===========================================================================
*/
package store

import (
	"reflect"
	"testing"
	"time"

	"opensro.online/server/internal/game/item/grounditem"
)

/*
================
groundChangeFixture

A store with a registry of drops in two divisions, committed once, so
every later commit starts from an acknowledged plane.
================
*/
type groundChangeFixture struct {
	dir   string
	clock *testClock
	s     *Store
	r     *grounditem.Registry
	gids  []uint32
}

func newGroundChangeFixture(t *testing.T) *groundChangeFixture {
	t.Helper()
	f := &groundChangeFixture{dir: t.TempDir(), clock: newTestClock()}
	f.s = openTest(t, f.dir, f.clock)
	f.r = grounditem.NewRegistry()
	f.s.AttachGround(f.r)
	for i := 0; i < 40; i++ {
		f.gids = append(f.gids, f.r.Add(testDivision, grounditem.Item{RefObjID: 3117, StackCount: 1,
			Position: grounditem.Point{RegionID: 0x6a9a, X: float32(i)}}).Gid)
	}
	f.r.Add("other", grounditem.Item{RefObjID: 62, GoldAmount: 10})
	f.s.Mutate("ground-change-seed", nil)
	if rows := f.r.Changes().Rows; len(rows) != 0 {
		t.Fatalf("the seed commit left %d unacknowledged rows", len(rows))
	}
	return f
}

/*
================
groundChangeFixture::commitAndMatch

Commits, then requires the table - and a reopened store's plane - to
equal the registry's snapshot, and the registry to hold no stamp.
================
*/
func (f *groundChangeFixture) commitAndMatch(t *testing.T, what string) {
	t.Helper()
	f.s.Mutate("ground-change", nil)
	want := f.r.Snapshot().Divisions
	if got := readGroundTable(t, f.s); !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: the table differs from the registry\n got %v\nwant %v", what, got, want)
	}
	if changes := f.r.Changes(); changes.Full || len(changes.Rows) != 0 {
		t.Fatalf("%s: the committed changes were not acknowledged (full %t, %d rows)", what, changes.Full, len(changes.Rows))
	}
	if !reflect.DeepEqual(f.s.ground.loadedRecords, want) {
		t.Fatalf("%s: loadedRecords no longer mirrors the table", what)
	}
}

/*
================
groundChangeFixture::reopenMatches
================
*/
func (f *groundChangeFixture) reopenMatches(t *testing.T) {
	t.Helper()
	want := f.r.Snapshot()
	f.s.Close()
	reopened := openTest(t, f.dir, f.clock)
	got := reopened.GroundSnapshotForRestore()
	if !reflect.DeepEqual(got.Divisions, want.Divisions) || got.GidCounter != want.GidCounter {
		t.Fatalf("the reopened plane differs from the registry (counter %d, want %d)", got.GidCounter, want.GidCounter)
	}
}

/*
================
TestOneChangedDropIsOneRow

A one-drop change is one stamped row - the commit no longer diffs the
plane - and it persists.
================
*/
func TestOneChangedDropIsOneRow(t *testing.T) {
	f := newGroundChangeFixture(t)
	f.r.SetStackCount(testDivision, f.gids[9], 4)
	changes := f.r.Changes()
	if changes.Full || len(changes.Rows) != 1 || changes.Rows[0].Gid != f.gids[9] || changes.Rows[0].Record.StackCount != 4 {
		t.Fatalf("one changed drop reported %+v", changes)
	}
	f.commitAndMatch(t, "one changed drop")
	f.reopenMatches(t)
}

/*
================
TestEveryRegistryMutatorPersists

Each registry writer stamps what it changes: after each one alone, the
commit brings the table to the registry. A mutator that forgot its
stamp leaves the table behind and fails here by name.
================
*/
func TestEveryRegistryMutatorPersists(t *testing.T) {
	instance := grounditem.Population{World: 0x1000a, Generation: 2}
	dropped := time.Unix(1000, 0)
	mutators := []struct {
		name   string
		mutate func(f *groundChangeFixture)
	}{
		{"Add", func(f *groundChangeFixture) { f.r.Add(testDivision, grounditem.Item{RefObjID: 62, GoldAmount: 5}) }},
		{"Remove", func(f *groundChangeFixture) { f.r.Remove(testDivision, f.gids[3]) }},
		{"SetStackCount", func(f *groundChangeFixture) { f.r.SetStackCount(testDivision, f.gids[4], 9) }},
		{"ExpireItems", func(f *groundChangeFixture) {
			f.r.Add(testDivision, grounditem.Item{RefObjID: 62, GoldAmount: 1, DroppedAt: dropped})
			f.s.Mutate("ground-change-arm", nil)
			if len(f.r.ExpireItems(testDivision, dropped.Add(time.Hour), time.Minute)) != 1 {
				panic("nothing expired")
			}
		}},
		{"ReleaseExpiredOwners", func(f *groundChangeFixture) {
			f.r.Add(testDivision, grounditem.Item{RefObjID: 62, GoldAmount: 1, OwnerJID: 7, DroppedAt: dropped})
			f.s.Mutate("ground-change-arm", nil)
			if len(f.r.ReleaseExpiredOwners(testDivision, dropped.Add(time.Hour), time.Minute)) != 1 {
				panic("nothing released")
			}
		}},
		{"Clear", func(f *groundChangeFixture) { f.r.Clear("other") }},
		{"RemovePopulation", func(f *groundChangeFixture) {
			f.r.Add(testDivision, grounditem.Item{RefObjID: 3117, Population: instance})
			f.s.Mutate("ground-change-arm", nil)
			if len(f.r.RemovePopulation(testDivision, instance)) != 1 {
				panic("nothing removed")
			}
		}},
	}
	for _, m := range mutators {
		t.Run(m.name, func(t *testing.T) {
			f := newGroundChangeFixture(t)
			m.mutate(f)
			f.commitAndMatch(t, m.name)
			f.reopenMatches(t)
		})
	}
}

/*
================
TestAVanishedDivisionDeletesEveryRow

Clearing a division (an instance closing, a division emptied) stamps
every gid it held: the commit deletes all of them, not just stops
writing the division.
================
*/
func TestAVanishedDivisionDeletesEveryRow(t *testing.T) {
	f := newGroundChangeFixture(t)
	f.r.Clear(testDivision)
	if rows := f.r.Changes().Rows; len(rows) != len(f.gids) {
		t.Fatalf("clearing %d drops stamped %d rows", len(f.gids), len(rows))
	}
	f.commitAndMatch(t, "a cleared division")
	if rows := readGroundTable(t, f.s)[testDivision]; len(rows) != 0 {
		t.Fatalf("the cleared division kept %d rows", len(rows))
	}
	f.reopenMatches(t)
}

/*
================
TestTheGidCounterPersistsWhenItMoves

A drop added and taken between two commits leaves no row, but the
counter it moved must persist: a reopened registry may not hand that
gid out again.
================
*/
func TestTheGidCounterPersistsWhenItMoves(t *testing.T) {
	f := newGroundChangeFixture(t)
	transient := f.r.Add(testDivision, grounditem.Item{RefObjID: 62, GoldAmount: 3})
	f.r.Remove(testDivision, transient.Gid)
	f.commitAndMatch(t, "a transient drop")
	f.s.Close()
	reopened := openTest(t, f.dir, f.clock)
	fresh := grounditem.NewRegistry()
	fresh.Restore(reopened.GroundSnapshotForRestore())
	if next := fresh.Add(testDivision, grounditem.Item{RefObjID: 62, GoldAmount: 1}); next.Gid <= transient.Gid {
		t.Fatalf("a reopened registry reissued gid %d (the transient drop had %d)", next.Gid, transient.Gid)
	}
}

/*
================
TestARolledBackFlushAcknowledgesNothing

A tick flush whose transaction fails after the ground rows were written
rolls back: nothing may be acknowledged and loadedRecords must not
move, so the next flush writes the same rows again.
================
*/
func TestARolledBackFlushAcknowledgesNothing(t *testing.T) {
	f := newGroundChangeFixture(t)
	c := seededCharacter()
	if err := f.s.CreateCharacter(testDivision, "ground-account", c); err != nil {
		t.Fatal(err)
	}
	before := readGroundTable(t, f.s)
	f.s.BeginTickBatch()
	f.r.SetStackCount(testDivision, f.gids[7], 6)
	f.s.MutateCharacterDeferred(c, "ground-change-deferred", func() { c.Gold = int64Ptr(77) })
	f.s.beforeSQLiteCommit = func() { panic("injected commit failure") }
	func() {
		defer func() { _ = recover() }()
		f.s.FlushTickBatch()
	}()
	f.s.beforeSQLiteCommit = nil
	if got := readGroundTable(t, f.s); !reflect.DeepEqual(got, before) {
		t.Fatal("the rolled-back flush changed the table")
	}
	if rows := f.r.Changes().Rows; len(rows) != 1 || rows[0].Gid != f.gids[7] {
		t.Fatalf("the rolled-back flush acknowledged its rows: %d left", len(rows))
	}
	if !reflect.DeepEqual(f.s.ground.loadedRecords, before) {
		t.Fatal("the rolled-back flush moved loadedRecords")
	}
	f.commitAndMatch(t, "the retry after a rolled-back flush")
	f.reopenMatches(t)
}

/*
================
TestACrashBeforeTheAcknowledgementRewritesIdempotently

The transaction committed but the process died before Acknowledge: the
stamps survive, and the next commit writes the already-durable rows
again - the same bytes, the table still equal to the registry.
================
*/
func TestACrashBeforeTheAcknowledgementRewritesIdempotently(t *testing.T) {
	f := newGroundChangeFixture(t)
	f.r.SetStackCount(testDivision, f.gids[2], 3)
	f.r.Remove(testDivision, f.gids[11])
	changes := f.r.Changes()
	f.s.mu.Lock()
	tx, err := f.s.db.Begin()
	if err != nil {
		f.s.mu.Unlock()
		t.Fatal(err)
	}
	if err := writeGroundChangesTx(tx, &f.s.encodeScratch, f.s.ground.loadedRecords, &changes); err != nil {
		f.s.mu.Unlock()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		f.s.mu.Unlock()
		t.Fatal(err)
	}
	f.s.mu.Unlock()
	if rows := f.r.Changes().Rows; len(rows) != 2 {
		t.Fatalf("unacknowledged rows = %d, want 2", len(rows))
	}
	f.commitAndMatch(t, "the rewrite after a lost acknowledgement")
	f.reopenMatches(t)
}

/*
================
TestARacingMutationSurvivesTheAcknowledgement

A change stamped after the commit read its revision keeps its stamp
through that commit's acknowledgement, so the next commit writes it.
================
*/
func TestARacingMutationSurvivesTheAcknowledgement(t *testing.T) {
	f := newGroundChangeFixture(t)
	f.r.SetStackCount(testDivision, f.gids[1], 2)
	read := f.r.Changes()
	f.r.SetStackCount(testDivision, f.gids[5], 8)
	f.r.Acknowledge(read.Revision)
	rows := f.r.Changes().Rows
	if len(rows) != 1 || rows[0].Gid != f.gids[5] {
		t.Fatalf("after acknowledging the first read, %d rows remain, want only the racing gid", len(rows))
	}
}
