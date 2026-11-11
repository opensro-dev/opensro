/*
===========================================================================

rollup_buckets_test.go - the bucket rollup answers what the row fold
answered (#3187)

The store sums gold changes into hourly buckets at commit; the
snapshot's rollup reads the buckets, plus the bulk rows its fold lane
has not eaten yet. Over a fixed fixture the two folds must give the
console the same hourly numbers.

===========================================================================
*/
package economy

import (
	"cmp"
	"database/sql"
	"reflect"
	"slices"
	"testing"
)

/*
================
byEntry

Canonical entry order, so two folds compare by content, not by the
order their GROUP BY happened to emit within an hour.
================
*/
func byEntry(entries []rollupEntry) []rollupEntry {
	slices.SortFunc(entries, func(a, b rollupEntry) int {
		if c := cmp.Compare(a.t, b.t); c != 0 {
			return c
		}
		if c := cmp.Compare(a.division, b.division); c != 0 {
			return c
		}
		if c := cmp.Compare(a.reason, b.reason); c != 0 {
			return c
		}
		if a.gain != b.gain {
			if a.gain {
				return 1
			}
			return -1
		}
		return 0
	})
	return entries
}

/*
================
foldFixtureIntoBuckets

Creates the store's bucket tables over the seeded ledger and folds
every row into them, the way the store's fold lane does for history,
with the watermark at the newest row.
================
*/
func foldFixtureIntoBuckets(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE economy_ledger_hours (
  hour_ms INTEGER NOT NULL, division TEXT NOT NULL, reason TEXT NOT NULL, gain INTEGER NOT NULL,
  total INTEGER NOT NULL, low INTEGER NOT NULL, high INTEGER NOT NULL, n INTEGER NOT NULL,
  PRIMARY KEY (hour_ms, division, reason, gain))`,
		`CREATE TABLE economy_ledger_char_hours (
  hour_ms INTEGER NOT NULL, char_id INTEGER NOT NULL, reason TEXT NOT NULL,
  total INTEGER NOT NULL, n INTEGER NOT NULL,
  PRIMARY KEY (hour_ms, char_id, reason))`,
		`INSERT INTO economy_ledger_hours
SELECT (at_ms / 3600000) * 3600000, division, reason, gold > 0, SUM(gold), MIN(gold), MAX(gold), COUNT(*)
FROM economy_ledger GROUP BY 1, division, reason, gold > 0`,
		`INSERT INTO economy_ledger_char_hours
SELECT (at_ms / 3600000) * 3600000, char_id, reason, SUM(gold), COUNT(*)
FROM economy_ledger WHERE reason IN ('monster_gold', 'npc_sale') GROUP BY 1, char_id, reason`,
		`INSERT INTO meta (key, value) VALUES ('economyFoldSeq', (SELECT COALESCE(MAX(seq), 0) FROM economy_ledger))`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
}

/*
================
TestBucketRollupMatchesRowFold

The console's fixed-window bar (#3187): the same fixture folded from
its rows (the reference) and from its buckets (production since the
change) gives the same hourly entries, and the same series and flows
through the window folds.
================
*/
func TestBucketRollupMatchesRowFold(t *testing.T) {
	db := seedRollupLedger(t)
	want, err := foldRowRollup(db, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	foldFixtureIntoBuckets(t, db)
	got, err := readRollup(db)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(byEntry(want), byEntry(got)) {
		t.Fatalf("bucket rollup differs from the row fold\nwant %+v\ngot  %+v", want, got)
	}
	windows := []window{
		{bucket: "1h", size: 3600, from: dayStart + 2*86400 + 5*3600, to: dayStart + 2*86400 + 35*3600 - 1},
		{bucket: "1d", size: 86400, from: dayStart, to: dayStart + 10*86400 - 1},
	}
	for _, win := range windows {
		for _, division := range []string{"", "d1", "d2"} {
			w := win
			w.division = division
			wantGroups, err := readGroups(db, w)
			if err != nil {
				t.Fatal(err)
			}
			gotGroups := rollupGroups(got, w)
			wantBuckets, wantReasons := foldFlows(w, wantGroups)
			gotBuckets, gotReasons := foldFlows(w, gotGroups)
			if !reflect.DeepEqual(wantBuckets, gotBuckets) || !reflect.DeepEqual(wantReasons, gotReasons) {
				t.Fatalf("%s division %q: bucket flows differ from the row fold", w.bucket, division)
			}
		}
	}
}

/*
================
TestBucketRollupFoldsTheUnfoldedTail

Bulk rows past the fold watermark (written before the change, or by
an older binary in a rollback window) are not in the buckets yet:
the rollup folds them in live and still answers the whole ledger,
while a keep-reason row the commit path already bucketed is never
counted twice.
================
*/
func TestBucketRollupFoldsTheUnfoldedTail(t *testing.T) {
	db := seedRollupLedger(t)
	foldFixtureIntoBuckets(t, db)
	add := func(day, hour int64, division, reason string, gold int64) {
		atMs := (dayStart+(day*24+hour)*3600+7*60+11)*1000 + 123
		if _, err := db.Exec("INSERT INTO economy_ledger (at_ms, division, char_id, reason, door, gold, balance) VALUES (?, ?, 7, ?, 'door', ?, 0)",
			atMs, division, reason, gold); err != nil {
			t.Fatal(err)
		}
	}
	// History the fold has not reached yet.
	add(11, 3, "d1", "monster_gold", 4242)
	add(11, 3, "d1", "npc_sale", 111)
	// A keep-reason change a bucketing commit already summed: its row
	// stays as evidence and its bucket carries the sum.
	add(11, 3, "d1", "shop_buy", -777)
	if _, err := db.Exec(`INSERT INTO economy_ledger_hours (hour_ms, division, reason, gain, total, low, high, n)
VALUES (?, 'd1', 'shop_buy', 0, -777, -777, -777, 1)`, (dayStart+(11*24+3)*3600)*1000); err != nil {
		t.Fatal(err)
	}
	want, err := foldRowRollup(db, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	got, err := readRollup(db)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(byEntry(want), byEntry(got)) {
		t.Fatalf("rollup with an unfolded tail differs from the row fold\nwant %+v\ngot  %+v", want, got)
	}
}

/*
================
TestMergeSurvivesAGrowingSlice

The bucket entries arrive in a slice with no room to spare. A tail
entry with a new key makes the merge grow it, which moves the slice;
a tail entry for an existing key that follows must still land in the
result. With pointers into the old array it was added there and lost:
the hour read 70 where it holds 100.
================
*/
func TestMergeSurvivesAGrowingSlice(t *testing.T) {
	entries := make([]rollupEntry, 2)
	entries[0] = rollupEntry{t: 1000, division: "main", reason: "monster_gold", gain: true, sum: 70, min: 5, max: 40, count: 3}
	entries[1] = rollupEntry{t: 2000, division: "main", reason: "monster_gold", gain: true, sum: 10, min: 10, max: 10, count: 1}
	tail := []rollupEntry{
		{t: 3000, division: "main", reason: "npc_sale", gain: true, sum: 8, min: 8, max: 8, count: 1},
		{t: 1000, division: "main", reason: "monster_gold", gain: true, sum: 30, min: 2, max: 28, count: 2},
		{t: 3000, division: "main", reason: "npc_sale", gain: true, sum: 4, min: 4, max: 4, count: 1},
	}
	merged := mergeRollupEntries(entries, tail)
	if len(merged) != 3 {
		t.Fatalf("the merge returned %d entries, want 3", len(merged))
	}
	first := merged[0]
	if first.t != 1000 || first.sum != 100 || first.min != 2 || first.max != 40 || first.count != 5 {
		t.Fatalf("the hour merged after the slice grew reads %+v, want sum 100, min 2, max 40, count 5", first)
	}
	last := merged[2]
	if last.t != 3000 || last.sum != 12 || last.count != 2 {
		t.Fatalf("the appended hour reads %+v, want sum 12, count 2", last)
	}
}
