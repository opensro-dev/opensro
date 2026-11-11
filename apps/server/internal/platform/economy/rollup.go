/*
===========================================================================

rollup.go - the ledger folded once per snapshot, not once per request

A snapshot's series, flows and flags all read the same ledger groups;
folding them per request re-scanned millions of ledger rows per page
(2.8-6.7 s a panel against the 6,000-leg profile of #2477, four folds
contending for one disk). The snapshot already changes only every
SnapshotEvery, so Service.Snapshot folds the ledger once into the
hourly entries below and publishes them with the snapshot time; every
request then folds its window from memory and touches no SQLite.

The fold is the readGroups aggregate at the 1h bucket: same sums,
minima, maxima and counts per reason and direction, with the loss
side stored as magnitudes, plus the division readGroups filters on.
Daily buckets fold from hourly rows exactly: sums and counts add,
minima and maxima combine. settleTwoSidedTransfers and foldSeries
then take the window's groups as they always did.

===========================================================================
*/
package economy

import (
	"cmp"
	"database/sql"
	"slices"
)

// hourSeconds is the rollup's bucket: the finest the contract answers.
const hourSeconds = 3600

/*
================
rollupEntry

One hour's rows of one division, reason and direction: the group
readGroups returns for that bucket, before the transfer settle.
================
*/
type rollupEntry struct {
	t        int64
	division string
	reason   string
	gain     bool
	sum      int64
	min, max int64
	count    int64
}

/*
================
readRollup

The whole ledger folded by hour, division, reason and direction,
oldest hour first. Runs once per snapshot, inside the snapshot pass;
like readGroups, a loss group is stored as its magnitude.

Since #3187 the sums live in the store's hourly buckets
(economy_ledger_hours), summed at commit, so the rollup reads those.
Bulk-reason rows the store's fold lane has not eaten yet (seq past
its watermark in the snapshot copy's meta, written before the change
or by an older binary in a rollback window) are folded in live, so
the answer is exact at every moment of the migration. A copy with no
bucket tables at all (a fixture, a pre-change spare) folds its rows
whole, as this function always did.
================
*/
func readRollup(db *sql.DB) ([]rollupEntry, error) {
	var tables int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema
WHERE type = 'table' AND name = 'economy_ledger_hours'`).Scan(&tables); err != nil {
		return nil, err
	}
	if tables == 0 {
		return foldRowRollup(db, 0, true)
	}
	rows, err := db.Query(`SELECT hour_ms / 1000, division, reason, gain, total, low, high, n
FROM economy_ledger_hours ORDER BY hour_ms`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rollupEntry
	for rows.Next() {
		var e rollupEntry
		var gain int
		if err := rows.Scan(&e.t, &e.division, &e.reason, &gain, &e.sum, &e.min, &e.max, &e.count); err != nil {
			return nil, err
		}
		e.gain = gain != 0
		if !e.gain {
			e.sum = -e.sum
			e.min, e.max = -e.max, -e.min
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var foldSeq int64
	// The store's fold watermark (store metaKeyEconomyFoldSeq): bulk
	// rows above it are not in the buckets yet and fold in live.
	_ = db.QueryRow("SELECT CAST(value AS INTEGER) FROM meta WHERE key = 'economyFoldSeq'").Scan(&foldSeq)
	tail, err := foldRowRollup(db, foldSeq, false)
	if err != nil {
		return nil, err
	}
	return mergeRollupEntries(out, tail), nil
}

/*
================
foldRowRollup

The rollup asked of the ledger's rows directly: every reason when
allReasons, else only the two bulk reasons above afterSeq.
================
*/
func foldRowRollup(db *sql.DB, afterSeq int64, allReasons bool) ([]rollupEntry, error) {
	query := `SELECT (at_ms / 1000 / ?) * ? AS t, division, reason, gold > 0, SUM(gold), MIN(gold), MAX(gold), COUNT(*)
FROM economy_ledger WHERE seq > ?`
	args := []any{hourSeconds, hourSeconds, afterSeq}
	if !allReasons {
		query += " AND reason IN ('monster_gold', 'npc_sale')"
	}
	query += " GROUP BY t, division, reason, gold > 0 ORDER BY t"
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rollupEntry
	for rows.Next() {
		var e rollupEntry
		if err := rows.Scan(&e.t, &e.division, &e.reason, &e.gain, &e.sum, &e.min, &e.max, &e.count); err != nil {
			return nil, err
		}
		if !e.gain {
			e.sum = -e.sum
			e.min, e.max = -e.max, -e.min
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

/*
================
mergeRollupEntries

Folds tail entries into the bucket entries by hour, division, reason
and direction: totals and counts add, minima and maxima combine.
Both sides are oldest first; the result stays oldest first.
================
*/
func mergeRollupEntries(entries, tail []rollupEntry) []rollupEntry {
	if len(tail) == 0 {
		return entries
	}
	type key struct {
		t        int64
		division string
		reason   string
		gain     bool
	}
	index := make(map[key]*rollupEntry, len(entries))
	for i := range entries {
		index[key{entries[i].t, entries[i].division, entries[i].reason, entries[i].gain}] = &entries[i]
	}
	out := entries
	for _, e := range tail {
		k := key{e.t, e.division, e.reason, e.gain}
		if have := index[k]; have != nil {
			have.sum += e.sum
			if e.min < have.min {
				have.min = e.min
			}
			if e.max > have.max {
				have.max = e.max
			}
			have.count += e.count
			continue
		}
		out = append(out, e)
		index[k] = &out[len(out)-1]
	}
	slices.SortFunc(out, func(a, b rollupEntry) int {
		return cmp.Compare(a.t, b.t)
	})
	return out
}

/*
================
rollupGroups

The window's groups from the folded entries: filtered by division,
folded into the window's buckets (an exact fold at both contract
sizes, since both divide the day into whole hours), oldest bucket
first, settled like readGroups settles. The window counts whole
buckets: parseWindow aligns every window to its buckets before a
caller sees it, so the fold and the ledger query count the same rows.
================
*/
func rollupGroups(entries []rollupEntry, w window) []group {
	type key struct {
		t      int64
		reason string
		gain   bool
	}
	merged := map[key]*group{}
	var order []key
	first, last := w.from/w.size*w.size, w.to/w.size*w.size
	for _, e := range entries {
		if w.division != "" && e.division != w.division {
			continue
		}
		k := key{t: e.t / w.size * w.size, reason: e.reason, gain: e.gain}
		if k.t < first || k.t > last {
			continue
		}
		g := merged[k]
		if g == nil {
			g = &group{t: k.t, reason: e.reason, gain: e.gain}
			merged[k] = g
			order = append(order, k)
		}
		if g.count == 0 || e.min < g.min {
			g.min = e.min
		}
		if g.count == 0 || e.max > g.max {
			g.max = e.max
		}
		g.sum += e.sum
		g.count += e.count
	}
	out := make([]group, 0, len(order))
	for _, k := range order {
		out = append(out, *merged[k])
	}
	return settleTwoSidedTransfers(out)
}
