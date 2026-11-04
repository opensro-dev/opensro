/*
===========================================================================

rollup_test.go - the hourly fold answers what the ledger query answered

The equivalence suite seeds one ledger and asks both paths the same
questions: the per-request SQL grouping (readGroups) and the snapshot
fold (readRollup + rollupGroups) must give identical series, flows
and flags for 1h and 1d windows, with and without a division. The
benchmark prices one Economy page's whole ledger work (two series, the
flows and the flags over a 7-day window) off the fold: #2477's target
is under 60 ms where the SQL grouping measured about 6 s on the live
snapshot.

===========================================================================
*/
package economy

import (
	"database/sql"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"
)

/*
================
seedRollupLedger

A ledger database of the snapshot's shape: ten days of gains, losses,
two-sided transfers (one stall bucket paying its commission), a
one-sided transfer reason, and a day-8 mint spike that must raise the
net_mint_p95 flag. Rows land mid-hour and mid-second; divisions d1,
d2 and the empty division all appear.
================
*/
func seedRollupLedger(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE economy_ledger (seq INTEGER PRIMARY KEY, at_ms INTEGER NOT NULL, division TEXT NOT NULL,
  char_id INTEGER NOT NULL, reason TEXT NOT NULL, door TEXT NOT NULL, gold INTEGER NOT NULL, balance INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	add := func(day, hour int64, division, reason string, gold int64) {
		atMs := (dayStart+(day*24+hour)*3600+7*60+11)*1000 + 123
		if _, err := db.Exec("INSERT INTO economy_ledger (at_ms, division, char_id, reason, door, gold, balance) VALUES (?, ?, 7, ?, 'door', ?, 0)",
			atMs, division, reason, gold); err != nil {
			t.Fatal(err)
		}
	}
	for day := int64(0); day < 10; day++ {
		for _, hour := range []int64{0, 5, 13} {
			base := (day+1)*1000 + hour
			add(day, hour, "d1", "monster_gold", base)
			add(day, hour, "d1", "shop_buy", -300)
			add(day, hour, "d1", "exchange", 500)
			add(day, hour, "d1", "exchange", -500)
			add(day, hour, "d1", "storage_deposit", -250)
			if hour == 5 {
				add(day, hour, "d2", "monster_gold", base*3)
				add(day, hour, "d2", "shop_buy", -900)
			}
		}
	}
	add(3, 5, "d1", "stall_sale", 990)
	add(3, 5, "d1", "stall_sale", -1000)
	add(4, 5, "d1", "stall_sale", 400)
	add(4, 5, "d1", "stall_sale", -400)
	add(2, 13, "", "monster_gold", 77)
	add(8, 5, "d1", "monster_gold", 5_000_000)
	return db
}

/*
================
referenceFlags

The flags the old per-request path computed: 1d gold_net groups read
from the ledger, folded and held against the netMintWindow percentile.
================
*/
func referenceFlags(t *testing.T, db *sql.DB, w window, division string) []Flag {
	t.Helper()
	days := window{bucket: "1d", size: bucketSeconds["1d"], from: w.from - netMintWindow*bucketSeconds["1d"], to: w.to, division: division}
	groups, err := readGroups(db, days)
	if err != nil {
		t.Fatal(err)
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
	return flags
}

/*
================
TestRollupMatchesReadGroups

Every answer the fold gives equals the ledger query's, at both
contract buckets, for the whole shard and per division; the day-8
spike raises the same flags on both paths.
================
*/
func TestRollupMatchesReadGroups(t *testing.T) {
	db := seedRollupLedger(t)
	entries, err := readRollup(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("the fold came back empty over a seeded ledger")
	}
	metrics := []string{"gold_minted", "gold_burned", "gold_net", "reason:monster_gold", "reason:stall_sale"}
	windows := []window{
		{bucket: "1h", size: 3600, from: dayStart + 2*86400 + 5*3600, to: dayStart + 2*86400 + 35*3600 - 1},
		{bucket: "1d", size: 86400, from: dayStart, to: dayStart + 10*86400 - 1},
	}
	for _, win := range windows {
		for _, division := range []string{"", "d1", "d2"} {
			w := win
			w.division = division
			want, err := readGroups(db, w)
			if err != nil {
				t.Fatal(err)
			}
			got := rollupGroups(entries, w)
			for _, metric := range metrics {
				if a, b := foldSeries(metric, want), foldSeries(metric, got); !reflect.DeepEqual(a, b) {
					t.Fatalf("%s %s division %q: series differ\nwant %+v\ngot  %+v", w.bucket, metric, division, a, b)
				}
			}
			wantBuckets, wantReasons := foldFlows(w, want)
			gotBuckets, gotReasons := foldFlows(w, got)
			if !reflect.DeepEqual(wantBuckets, gotBuckets) || !reflect.DeepEqual(wantReasons, gotReasons) {
				t.Fatalf("%s division %q: flows differ", w.bucket, division)
			}
		}
	}
	flagWin := window{bucket: "1d", size: 86400, from: dayStart + 8*86400, to: dayStart + 10*86400 - 1}
	for _, division := range []string{"", "d1", "d2"} {
		want := referenceFlags(t, db, flagWin, division)
		got := netMintFlags(entries, flagWin, division)
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("division %q: flags differ\nwant %+v\ngot  %+v", division, want, got)
		}
		if division != "d2" && len(got) == 0 {
			t.Fatalf("division %q: the day-8 spike raised no flag", division)
		}
	}
}

/*
================
TestParseWindowCountsWholeBuckets

Whole buckets are the contract, not the caller's habit: a parsed
window with an unaligned from, and the default window with neither
bound sent, both land on bucket edges, and on those aligned windows
the fold and the ledger query still agree - series, flows, and the
flags, whose lookback from an aligned from is whole days on both
paths by construction.
================
*/
func TestParseWindowCountsWholeBuckets(t *testing.T) {
	db := seedRollupLedger(t)
	entries, err := readRollup(db)
	if err != nil {
		t.Fatal(err)
	}
	metrics := []string{"gold_minted", "gold_burned", "gold_net", "reason:monster_gold", "reason:stall_sale"}
	now := time.Unix(dayStart+9*86400+13*3600+42*60+17, 0)
	cases := []struct {
		name     string
		bucket   string
		from, to string
		wantFrom int64
		wantTo   int64
	}{
		{"unaligned from and to", "1h",
			strconv.FormatInt(dayStart+2*86400+5*3600+301, 10), strconv.FormatInt(dayStart+2*86400+13*3600+301, 10),
			dayStart + 2*86400 + 5*3600, dayStart + 2*86400 + 14*3600 - 1},
		{"default window", "1h", "", "",
			(now.Unix() - 3600*(maxBuckets-1)) / 3600 * 3600, (now.Unix()/3600+1)*3600 - 1},
		{"unaligned from, a day window", "1d",
			strconv.FormatInt(dayStart+7*86400+3600, 10), "",
			dayStart + 7*86400, dayStart + 10*86400 - 1},
	}
	for _, tc := range cases {
		parsed, err := parseWindow(tc.bucket, tc.from, tc.to, "", now)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if parsed.from != tc.wantFrom || parsed.to != tc.wantTo {
			t.Fatalf("%s: window %d..%d, want whole buckets %d..%d", tc.name, parsed.from, parsed.to, tc.wantFrom, tc.wantTo)
		}
		for _, division := range []string{"", "d1", "d2"} {
			w := parsed
			w.division = division
			want, err := readGroups(db, w)
			if err != nil {
				t.Fatal(err)
			}
			got := rollupGroups(entries, w)
			for _, metric := range metrics {
				if a, b := foldSeries(metric, want), foldSeries(metric, got); !reflect.DeepEqual(a, b) {
					t.Fatalf("%s %s %s division %q: series differ\nwant %+v\ngot  %+v", tc.name, w.bucket, metric, division, a, b)
				}
			}
			wantBuckets, wantReasons := foldFlows(w, want)
			gotBuckets, gotReasons := foldFlows(w, got)
			if !reflect.DeepEqual(wantBuckets, gotBuckets) || !reflect.DeepEqual(wantReasons, gotReasons) {
				t.Fatalf("%s %s division %q: flows differ", tc.name, w.bucket, division)
			}
		}
	}
	// The day case's flags on the aligned window: both paths hold the
	// day-8 spike against the same whole earlier days.
	w, err := parseWindow("1d", strconv.FormatInt(dayStart+7*86400+3600, 10), "", "d1", now)
	if err != nil {
		t.Fatal(err)
	}
	want := referenceFlags(t, db, w, "d1")
	got := netMintFlags(entries, w, "d1")
	if !reflect.DeepEqual(want, got) || len(got) == 0 {
		t.Fatalf("flags on the aligned window differ or never fired\nwant %+v\ngot  %+v", want, got)
	}
}

// foldSink keeps the benchmark's answers from being optimised away.
var foldSink int

/*
================
BenchmarkRollupEconomyPage

One Economy page's ledger work over a 7-day 1h window - the two gold
series, the flows and the flags (which folds the 1d lookback too) -
against a fold of ninety days, two divisions and a dozen reasons:
about one hundred thousand hourly entries, the live snapshot's shape.
#2477's bar is 60 ms for what the SQL grouping answered in about 6 s.
================
*/
func BenchmarkRollupEconomyPage(b *testing.B) {
	reasons := []string{"monster_gold", "npc_sale", "quest_reward", "shop_buy", "repair", "alchemy",
		"exchange", "stall_sale", "storage_deposit", "skill_learn", "teleport_fee", "guild_fee"}
	var entries []rollupEntry
	for day := int64(0); day < 90; day++ {
		for hour := int64(0); hour < 24; hour++ {
			for _, division := range []string{"", "d1"} {
				for i, reason := range reasons {
					for _, gain := range []bool{true, false} {
						value := int64(1000 + day + hour + int64(i))
						entries = append(entries, rollupEntry{
							t: dayStart + (day*24+hour)*3600, division: division, reason: reason, gain: gain,
							sum: value, min: value / 2, max: value * 2, count: 7,
						})
					}
				}
			}
		}
	}
	win := window{bucket: "1h", size: 3600, from: dayStart + 83*86400, to: dayStart + 90*86400 - 1, division: "d1"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		groups := rollupGroups(entries, win)
		points := foldSeries("gold_minted", groups)
		points = append(points, foldSeries("gold_burned", groups)...)
		_, flows := foldFlows(win, groups)
		flags := netMintFlags(entries, win, "d1")
		foldSink += len(points) + len(flows) + len(flags)
	}
}
