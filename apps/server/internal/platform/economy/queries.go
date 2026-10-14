/*
===========================================================================

queries.go - the ledger's bucketed series, flows and flags (contract v1)

Every answer reads the newest snapshot's economy_ledger, grouped by
bucket, reason and direction in one query; the metric is folded from
those groups here. A mint sums the gains of mint reasons, a burn the
losses of burn reasons; transfers and unattributed changes count in
neither. Amounts are decimal strings.

===========================================================================
*/
package economy

import (
	"database/sql"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"opensro.online/server/internal/domain"
)

const (
	// maxBuckets is the most buckets one answer holds (contract v1).
	maxBuckets = 720
	// netMintWindow is how many earlier days a day's net mint is held
	// against (the net_mint_p95 flag).
	netMintWindow = 7
)

// bucketSeconds are the contract's bucket names.
var bucketSeconds = map[string]int64{"1h": 3600, "1d": 86400}

/*
================
group

One bucket's rows of one reason and direction.
================
*/
type group struct {
	t        int64
	reason   string
	gain     bool
	sum      int64
	min, max int64
	count    int64
}

/*
================
Point
================
*/
type Point struct {
	T     int64  `json:"t"`
	Sum   string `json:"sum"`
	Min   string `json:"min"`
	Max   string `json:"max"`
	Count int64  `json:"count"`
}

/*
================
window

A validated request range: from and to in unix seconds, the bucket size.
================
*/
type window struct {
	bucket   string
	size     int64
	from, to int64
	division string
}

/*
================
queryError

A contract error: its code and message.
================
*/
type queryError struct {
	code, message string
}

/*
================
queryError.Error
================
*/
func (e queryError) Error() string { return e.code + ": " + e.message }

/*
================
parseWindow

The bucket (default 1d), to (default now), from (default: the most
buckets that end at to), refused when the buckets it touches exceed
maxBuckets.
================
*/
func parseWindow(bucket, from, to, division string, now time.Time) (window, error) {
	if bucket == "" {
		bucket = "1d"
	}
	size, known := bucketSeconds[bucket]
	if !known {
		return window{}, queryError{"bad_bucket", "bucket is 1h or 1d"}
	}
	w := window{bucket: bucket, size: size, division: division, to: now.Unix()}
	var err error
	if to != "" {
		if w.to, err = strconv.ParseInt(to, 10, 64); err != nil || w.to < 0 {
			return window{}, queryError{"bad_bucket", "to is unix seconds"}
		}
	}
	w.from = w.to - size*(maxBuckets-1)
	if from != "" {
		if w.from, err = strconv.ParseInt(from, 10, 64); err != nil || w.from < 0 {
			return window{}, queryError{"bad_bucket", "from is unix seconds"}
		}
	}
	if w.from > w.to {
		return window{}, queryError{"bad_bucket", "from is after to"}
	}
	if w.to/size-w.from/size+1 > maxBuckets {
		return window{}, queryError{"range_too_wide", fmt.Sprintf("more than %d buckets", maxBuckets)}
	}
	return w, nil
}

/*
================
readGroups

The ledger's groups inside the window, oldest bucket first.
================
*/
func readGroups(db *sql.DB, w window) ([]group, error) {
	query := `SELECT (at_ms / 1000 / ?) * ? AS t, reason, gold > 0, SUM(gold), MIN(gold), MAX(gold), COUNT(*)
FROM economy_ledger WHERE at_ms >= ? AND at_ms < ?`
	args := []any{w.size, w.size, w.from * 1000, (w.to + 1) * 1000}
	if w.division != "" {
		query += " AND division = ?"
		args = append(args, w.division)
	}
	query += " GROUP BY t, reason, gold > 0 ORDER BY t"
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []group
	for rows.Next() {
		var g group
		if err := rows.Scan(&g.t, &g.reason, &g.gain, &g.sum, &g.min, &g.max, &g.count); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

/*
================
metricPicks

Whether a group counts toward the metric, and with which sign: +1 adds
its sum, -1 adds its magnitude (a burn).
================
*/
func metricPicks(metric string, g group) (sign int64, counted bool) {
	if reason, isReason := strings.CutPrefix(metric, "reason:"); isReason {
		return 1, g.reason == reason
	}
	kind := domain.EconomyReasonKind(g.reason)
	minted := kind == domain.EconomyKindMint && g.gain
	burned := kind == domain.EconomyKindBurn && !g.gain
	switch metric {
	case "gold_minted":
		return 1, minted
	case "gold_burned":
		return -1, burned
	case "gold_net":
		return 1, minted || burned
	}
	return 0, false
}

/*
================
ledgerMetric

Whether metric is one the ledger answers (gold_total reads the history).
================
*/
func ledgerMetric(metric string) bool {
	switch metric {
	case "gold_minted", "gold_burned", "gold_net":
		return true
	}
	reason, isReason := strings.CutPrefix(metric, "reason:")
	return isReason && reason != ""
}

/*
================
foldSeries

The metric's points from the groups; buckets with no row are omitted.
================
*/
func foldSeries(metric string, groups []group) []Point {
	type acc struct {
		sum, min, max, count int64
		seen                 bool
	}
	byBucket := map[int64]*acc{}
	var order []int64
	for _, g := range groups {
		sign, counted := metricPicks(metric, g)
		if !counted {
			continue
		}
		a := byBucket[g.t]
		if a == nil {
			a = &acc{}
			byBucket[g.t] = a
			order = append(order, g.t)
		}
		low, high := g.min*sign, g.max*sign
		if sign < 0 {
			low, high = high, low
		}
		if !a.seen || low < a.min {
			a.min = low
		}
		if !a.seen || high > a.max {
			a.max = high
		}
		a.seen = true
		a.sum += g.sum * sign
		a.count += g.count
	}
	slices.Sort(order)
	points := make([]Point, 0, len(order))
	for _, t := range order {
		a := byBucket[t]
		points = append(points, Point{T: t, Sum: decimal(a.sum), Min: decimal(a.min), Max: decimal(a.max), Count: a.count})
	}
	return points
}

/*
================
foldFlows

Every reason's sum per bucket, aligned to every bucket of the window
("0" for an empty one).
================
*/
func foldFlows(w window, groups []group) (buckets []int64, reasons map[string][]string) {
	start := w.from / w.size * w.size
	for t := start; t <= w.to; t += w.size {
		buckets = append(buckets, t)
	}
	index := make(map[int64]int, len(buckets))
	for i, t := range buckets {
		index[t] = i
	}
	sums := map[string][]int64{}
	for _, g := range groups {
		i, inside := index[g.t]
		if !inside {
			continue
		}
		row := sums[g.reason]
		if row == nil {
			row = make([]int64, len(buckets))
			sums[g.reason] = row
		}
		row[i] += g.sum
	}
	reasons = make(map[string][]string, len(sums))
	for reason, row := range sums {
		out := make([]string, len(row))
		for i, value := range row {
			out[i] = decimal(value)
		}
		reasons[reason] = out
	}
	return buckets, reasons
}

/*
================
Flag
================
*/
type Flag struct {
	ID        string  `json:"id"`
	Rule      string  `json:"rule"`
	At        int64   `json:"at"`
	Division  string  `json:"division"`
	Character *string `json:"character"`
	Ref       *string `json:"ref"`
	Value     string  `json:"value"`
	Threshold string  `json:"threshold"`
}

/*
================
netMintFlags

net_mint_p95: a day whose net mint exceeds the 95th percentile of the
netMintWindow days with ledger rows before it, once that many exist (a
day with no change at all has no point and is skipped). A flag only: no
rule acts. The day nets come from 1d groups starting netMintWindow days
before the window.
================
*/
func netMintFlags(db *sql.DB, w window, division string) ([]Flag, error) {
	days := window{bucket: "1d", size: bucketSeconds["1d"], from: w.from - netMintWindow*bucketSeconds["1d"], to: w.to, division: division}
	groups, err := readGroups(db, days)
	if err != nil {
		return nil, err
	}
	points := foldSeries("gold_net", groups)
	var flags []Flag
	for i, point := range points {
		if point.T < w.from || i < netMintWindow {
			continue
		}
		var earlier []int64
		for _, prior := range points[i-netMintWindow : i] {
			value, _ := strconv.ParseInt(prior.Sum, 10, 64)
			earlier = append(earlier, value)
		}
		slices.Sort(earlier)
		threshold := percentile(earlier, 0.95)
		value, _ := strconv.ParseInt(point.Sum, 10, 64)
		if value <= threshold {
			continue
		}
		flags = append(flags, Flag{
			ID: fmt.Sprintf("net_mint_p95:%s:%d", division, point.T), Rule: "net_mint_p95", At: point.T,
			Division: division, Value: point.Sum, Threshold: decimal(threshold),
		})
	}
	return flags, nil
}

/*
================
ledgerThrough

The newest row the snapshot holds, in unix seconds; 0 for an empty ledger.
================
*/
func ledgerThrough(db *sql.DB) (int64, error) {
	var newest sql.NullInt64
	if err := db.QueryRow("SELECT MAX(at_ms) FROM economy_ledger").Scan(&newest); err != nil {
		return 0, err
	}
	return newest.Int64 / 1000, nil
}

/*
================
decimal
================
*/
func decimal(value int64) string {
	return strconv.FormatInt(value, 10)
}
