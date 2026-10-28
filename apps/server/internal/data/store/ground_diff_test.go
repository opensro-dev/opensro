/*
===========================================================================

ground_diff_test.go - the ground plane persists by row diff

A ground-dirty commit writes only the rows that changed since the last
committed plane, and the table still reads back as exactly the live
registry after a reopen. The benchmark compares the diff with the old
whole-plane replace at the live plane's size.

===========================================================================
*/
package store

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/grounditem"
)

// livePlaneRows is the prod ground plane's size on 2026-10-28.
const livePlaneRows = 1600

/*
================
groundPlane
================
*/
func groundPlane(rows int) map[string][]domain.GroundItemRecord {
	plane := make([]domain.GroundItemRecord, rows)
	for i := range plane {
		plane[i] = domain.GroundItemRecord{Gid: uint32(1000 + i), RefObjID: 3117, TypeFlags: 1, RegionID: 25000,
			X: float32(i), Y: 1, Z: 2, DroppedBy: "hunter", DroppedAtMs: int64(i), MagicOptions: []uint64{uint64(i)}}
	}
	return map[string][]domain.GroundItemRecord{testDivision: plane}
}

/*
================
clonePlane
================
*/
func clonePlane(plane map[string][]domain.GroundItemRecord) map[string][]domain.GroundItemRecord {
	out := make(map[string][]domain.GroundItemRecord, len(plane))
	for divisionID, rows := range plane {
		copied := make([]domain.GroundItemRecord, len(rows))
		for i, row := range rows {
			row.MagicOptions = append([]uint64(nil), row.MagicOptions...)
			copied[i] = row
		}
		out[divisionID] = copied
	}
	return out
}

/*
================
readGroundTable
================
*/
func readGroundTable(t testing.TB, s *Store) map[string][]domain.GroundItemRecord {
	t.Helper()
	rows, err := s.db.Query("SELECT division, record FROM ground_items ORDER BY division, gid")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string][]domain.GroundItemRecord{}
	for rows.Next() {
		var division, record string
		if err := rows.Scan(&division, &record); err != nil {
			t.Fatal(err)
		}
		var item domain.GroundItemRecord
		if err := json.Unmarshal([]byte(record), &item); err != nil {
			t.Fatal(err)
		}
		out[division] = append(out[division], item)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

/*
================
applyGroundDiff
================
*/
func applyGroundDiff(t testing.TB, s *Store, committed, next map[string][]domain.GroundItemRecord) (int, int) {
	t.Helper()
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	written, deleted, err := writeGroundDiffTx(tx, &s.encodeScratch, committed, next)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return written, deleted
}

/*
================
TestGroundDiffWritesOnlyChangedRows

One changed stack, one picked-up drop and one new drop on a 1,600-row
plane: three statements, and the table equals the next plane. A
division that empties is deleted whole; an unsorted plane is still
diffed by gid.
================
*/
func TestGroundDiffWritesOnlyChangedRows(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	base := groundPlane(livePlaneRows)
	if written, deleted := applyGroundDiff(t, s, nil, base); written != livePlaneRows || deleted != 0 {
		t.Fatalf("first write %d/%d, want %d/0", written, deleted, livePlaneRows)
	}
	next := clonePlane(base)
	rows := next[testDivision]
	rows[10].StackCount = 7
	rows = append(rows[:20], rows[21:]...)
	rows = append(rows, domain.GroundItemRecord{Gid: 9000, RefObjID: 1})
	next[testDivision] = rows
	if written, deleted := applyGroundDiff(t, s, base, next); written != 2 || deleted != 1 {
		t.Fatalf("diff wrote %d and deleted %d, want 2 and 1", written, deleted)
	}
	if got := readGroundTable(t, s); !reflect.DeepEqual(got, next) {
		t.Fatalf("table differs from the next plane: %d rows, want %d", len(got[testDivision]), len(next[testDivision]))
	}
	if written, deleted := applyGroundDiff(t, s, next, clonePlane(next)); written != 0 || deleted != 0 {
		t.Fatalf("an unchanged plane wrote %d and deleted %d", written, deleted)
	}

	shuffled := clonePlane(next)
	shuffled[testDivision][0], shuffled[testDivision][len(rows)-1] = shuffled[testDivision][len(rows)-1], shuffled[testDivision][0]
	if written, deleted := applyGroundDiff(t, s, next, shuffled); written != 0 || deleted != 0 {
		t.Fatalf("an unsorted copy of the same plane wrote %d and deleted %d", written, deleted)
	}

	if written, deleted := applyGroundDiff(t, s, next, nil); written != 0 || deleted != len(rows) {
		t.Fatalf("emptying wrote %d and deleted %d, want 0 and %d", written, deleted, len(rows))
	}
	if got := readGroundTable(t, s); len(got) != 0 {
		t.Fatalf("an emptied plane left %d divisions", len(got))
	}
}

/*
================
TestGroundDiffPersistsTheLiveRegistry

Through the store's own commit: drops added, one picked up, one stack
changed, one added later. After each commit and after a reopen the
table is exactly the registry's snapshot.
================
*/
func TestGroundDiffPersistsTheLiveRegistry(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	r := grounditem.NewRegistry()
	s.AttachGround(r)
	gids := []uint32{}
	for i := 0; i < 40; i++ {
		gids = append(gids, r.Add(testDivision, grounditem.Item{RefObjID: 3117, StackCount: 1}).Gid)
	}
	s.Mutate("ground-diff-test", nil)
	r.Remove(testDivision, gids[5])
	r.SetStackCount(testDivision, gids[9], 4)
	r.Add(testDivision, grounditem.Item{RefObjID: 62, MagicOptions: []uint64{7}})
	s.Mutate("ground-diff-test", nil)
	want := r.Snapshot().Divisions
	if got := readGroundTable(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("table after the second commit differs from the registry")
	}
	s.Close()
	s = openTest(t, dir, clock)
	if got := s.GroundSnapshotForRestore().Divisions; !reflect.DeepEqual(got, want) {
		t.Fatalf("reopened plane differs from the registry")
	}
}

/*
================
BenchmarkGroundCommit

The ground write of one commit after one drop changed, at the live
plane's size: the old whole-plane replace against the diff.
================
*/
func BenchmarkGroundCommit(b *testing.B) {
	for _, mode := range []string{"replace", "diff"} {
		b.Run(fmt.Sprintf("%s/%d", mode, livePlaneRows), func(b *testing.B) {
			s := openTest(b, b.TempDir(), newTestClock())
			base := groundPlane(livePlaneRows)
			applyGroundDiff(b, s, nil, base)
			planes := [2]map[string][]domain.GroundItemRecord{base, clonePlane(base)}
			planes[1][testDivision][len(base[testDivision])/2].StackCount = 9
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				committed, next := planes[n%2], planes[(n+1)%2]
				tx, err := s.db.Begin()
				if err != nil {
					b.Fatal(err)
				}
				if mode == "replace" {
					err = replaceGroundTx(tx, &s.encodeScratch, next)
				} else {
					_, _, err = writeGroundDiffTx(tx, &s.encodeScratch, committed, next)
				}
				if err != nil {
					b.Fatal(err)
				}
				if err := tx.Commit(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
