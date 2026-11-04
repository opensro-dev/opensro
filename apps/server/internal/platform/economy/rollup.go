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
	"database/sql"
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
================
*/
func readRollup(db *sql.DB) ([]rollupEntry, error) {
	rows, err := db.Query(`SELECT (at_ms / 1000 / ?) * ? AS t, division, reason, gold > 0, SUM(gold), MIN(gold), MAX(gold), COUNT(*)
FROM economy_ledger GROUP BY t, division, reason, gold > 0 ORDER BY t`, hourSeconds, hourSeconds)
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
rollupGroups

The window's groups from the folded entries: filtered by division,
folded into the window's buckets (an exact fold at both contract
sizes, since both divide the day into whole hours), oldest bucket
first, settled like readGroups settles. The window counts whole
buckets, which is the shape every caller sends (a bucket-aligned
from, a to at or after the snapshot's newest row).
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
