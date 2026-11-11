/*
===========================================================================

division_delta_test.go - a reader's copy plus the delta is the division

DivisionChangesSince must hand a reader exactly what turns its copy at
one revision into the division at the next (division_delta.go). These
tests replay random mutations against a reader that only ever applies
deltas and compare it with All after every step, then pin the cases
where the log must refuse: a reader behind the log, a reader from
before a Restore, a revision the registry never issued.

===========================================================================
*/
package grounditem

import (
	"math/rand"
	"reflect"
	"slices"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
)

/*
================
deltaReader

A reader that keeps one division by gid and follows it by deltas
alone, falling back to a snapshot only when the registry says so.
================
*/
type deltaReader struct {
	revision  uint64
	rows      map[uint32]Item
	snapshots int
}

func (reader *deltaReader) follow(t *testing.T, registry *Registry, division string) {
	t.Helper()
	delta, ok := registry.DivisionChangesSince(division, reader.revision)
	if !ok {
		items, revision := registry.DivisionSnapshot(division)
		reader.rows = make(map[uint32]Item, len(items))
		for _, item := range items {
			reader.rows[item.Gid] = item
		}
		reader.revision = revision
		reader.snapshots++
		return
	}
	for _, gid := range delta.Removed {
		if _, held := reader.rows[gid]; !held {
			t.Fatalf("the delta removes gid %d, which the reader never held", gid)
		}
		delete(reader.rows, gid)
	}
	for _, item := range delta.Changed {
		reader.rows[item.Gid] = item
	}
	reader.revision = delta.Revision
}

func (reader *deltaReader) sorted() []Item {
	gids := make([]uint32, 0, len(reader.rows))
	for gid := range reader.rows {
		gids = append(gids, gid)
	}
	slices.Sort(gids)
	out := make([]Item, 0, len(gids))
	for _, gid := range gids {
		out = append(out, reader.rows[gid])
	}
	return out
}

/*
================
TestDeltasRebuildTheDivision

Two thousand random mutations of every kind the registry has. After
each one the reader applies the delta and must equal All. It starts
empty at revision zero, where the division is empty too, and never
takes a whole copy.
================
*/
func TestDeltasRebuildTheDivision(t *testing.T) {
	random := rand.New(rand.NewSource(3164))
	registry := NewRegistry()
	reader := &deltaReader{rows: map[uint32]Item{}}
	instance := Population{World: 7, Generation: 1}
	clock := time.Unix(1_800_000_000, 0)
	var live []uint32
	for step := 0; step < 2000; step++ {
		clock = clock.Add(time.Second)
		switch choice := random.Intn(100); {
		case choice < 45 || len(live) == 0:
			item := Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", StackCount: uint16(1 + random.Intn(50)),
				MagicOptions: []uint64{uint64(step)}, DroppedAt: clock,
				Position: Point{RegionID: 0x6a9a, X: float32(random.Intn(1900)), Z: float32(random.Intn(1900))}}
			if random.Intn(4) == 0 {
				item.Population = instance
			}
			if random.Intn(3) == 0 {
				item.OwnerJID = uint32(1 + random.Intn(9))
			}
			live = append(live, registry.Add("main", item).Gid)
		case choice < 75:
			at := random.Intn(len(live))
			registry.Remove("main", live[at])
			live = slices.Delete(live, at, at+1)
		case choice < 88:
			registry.SetStackCount("main", live[random.Intn(len(live))], uint16(1+random.Intn(900)))
		case choice < 93:
			registry.ReleaseExpiredOwners("main", clock, 30*time.Second)
		case choice < 97:
			for _, gone := range registry.ExpireItems("main", clock, 120*time.Second) {
				live = slices.DeleteFunc(live, func(gid uint32) bool { return gid == gone.Gid })
			}
		case choice < 99:
			for _, gone := range registry.RemovePopulation("main", instance) {
				live = slices.DeleteFunc(live, func(gid uint32) bool { return gid == gone.Gid })
			}
		default:
			// Another division's edits never reach this reader.
			registry.Add("other", Item{RefObjID: 62})
		}
		reader.follow(t, registry, "main")
		if want := registry.All("main"); !reflect.DeepEqual(reader.sorted(), want) {
			t.Fatalf("step %d: the reader holds %d rows, the division %d, and they differ", step, len(reader.rows), len(want))
		}
	}
	if reader.snapshots != 0 {
		t.Fatalf("the reader took %d whole copies; a reader that keeps up never needs one", reader.snapshots)
	}
}

/*
================
TestAnUnchangedDivisionAnswersEmpty
================
*/
func TestAnUnchangedDivisionAnswersEmpty(t *testing.T) {
	registry := NewRegistry()
	registry.Add("main", Item{RefObjID: 62})
	_, revision := registry.DivisionSnapshot("main")
	registry.Add("other", Item{RefObjID: 62})
	delta, ok := registry.DivisionChangesSince("main", revision)
	if !ok || delta.Revision != revision || len(delta.Changed) != 0 || len(delta.Removed) != 0 {
		t.Fatalf("an unchanged division answered %+v, ok %v", delta, ok)
	}
}

/*
================
TestTheDeltaOwnsItsRows

A changed row is the reader's own copy: writing through it must not
reach the registry.
================
*/
func TestTheDeltaOwnsItsRows(t *testing.T) {
	registry := NewRegistry()
	_, revision := registry.DivisionSnapshot("main")
	added := registry.Add("main", Item{RefObjID: 62, MagicOptions: []uint64{1, 2},
		Summon: &domain.CharacterCOS{RefObjID: 9}})
	delta, ok := registry.DivisionChangesSince("main", revision)
	if !ok || len(delta.Changed) != 1 {
		t.Fatalf("the add is missing from the delta: %+v, ok %v", delta, ok)
	}
	delta.Changed[0].MagicOptions[0] = 99
	delta.Changed[0].Summon.RefObjID = 99
	held, _ := registry.Get("main", added.Gid)
	if held.MagicOptions[0] != 1 || held.Summon.RefObjID != 9 {
		t.Fatal("a write through the delta's row reached the registry")
	}
}

/*
================
TestAReaderBehindTheLogIsRefused

The log is bounded. A reader that fell behind it must be told, not
handed half the changes; a reader inside it is still served.
================
*/
func TestAReaderBehindTheLogIsRefused(t *testing.T) {
	registry := NewRegistry()
	_, behind := registry.DivisionSnapshot("main")
	var gids []uint32
	for range divisionLogLimit + 40 {
		gids = append(gids, registry.Add("main", Item{RefObjID: 62}).Gid)
	}
	if _, ok := registry.DivisionChangesSince("main", behind); ok {
		t.Fatal("a reader behind the bounded log was served a delta")
	}
	_, current := registry.DivisionSnapshot("main")
	registry.Remove("main", gids[0])
	delta, ok := registry.DivisionChangesSince("main", current)
	if !ok || !slices.Equal(delta.Removed, []uint32{gids[0]}) || len(delta.Changed) != 0 {
		t.Fatalf("a reader inside the log got %+v, ok %v", delta, ok)
	}
}

/*
================
TestARestoreOrAnUnknownRevisionIsRefused

A Restore replaces the plane; no log describes the step. A revision
past the division's own was never issued.
================
*/
func TestARestoreOrAnUnknownRevisionIsRefused(t *testing.T) {
	registry := NewRegistry()
	registry.Add("main", Item{RefObjID: 62})
	_, before := registry.DivisionSnapshot("main")
	if _, ok := registry.DivisionChangesSince("main", before+1); ok {
		t.Fatal("a revision the registry never issued was served")
	}
	registry.Restore(registry.Snapshot())
	if _, ok := registry.DivisionChangesSince("main", before); ok {
		t.Fatal("a reader from before the Restore was served a delta")
	}
	items, after := registry.DivisionSnapshot("main")
	if len(items) != 1 {
		t.Fatalf("the restored division holds %d rows, want 1", len(items))
	}
	registry.Add("main", Item{RefObjID: 62})
	if delta, ok := registry.DivisionChangesSince("main", after); !ok || len(delta.Changed) != 1 {
		t.Fatalf("a reader from after the Restore got %+v, ok %v", delta, ok)
	}
}

/*
================
TestAClearedDivisionIsAllRemoved
================
*/
func TestAClearedDivisionIsAllRemoved(t *testing.T) {
	registry := NewRegistry()
	first := registry.Add("gone", Item{RefObjID: 62}).Gid
	second := registry.Add("gone", Item{RefObjID: 62}).Gid
	_, revision := registry.DivisionSnapshot("gone")
	registry.Clear("gone")
	delta, ok := registry.DivisionChangesSince("gone", revision)
	if !ok || !slices.Equal(delta.Removed, []uint32{first, second}) || len(delta.Changed) != 0 {
		t.Fatalf("a cleared division answered %+v, ok %v", delta, ok)
	}
}
