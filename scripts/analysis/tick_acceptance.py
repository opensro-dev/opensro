"""
===========================================================================

tick_acceptance.py - the #1820 acceptance re-measure, as one report

The issue's baseline was taken by hand from the GameWorld's loopback
/transport/metrics and the tick-spike journal on the production host.
This tool turns two metrics snapshots (window start and window end)
plus the journal into the same table, so the re-measure after the
spike fixes ship is one prepared run instead of another improvisation:

    curl -s http://127.0.0.1:<gameworld-loopback>/transport/metrics > before.json
    ... play window ...
    curl -s http://127.0.0.1:<gameworld-loopback>/transport/metrics > after.json
    python scripts/analysis/tick_acceptance.py --before before.json \
        --after after.json --spikes tick-spikes.jsonl \
        --window-start 2026-10-28T13:05:26Z --window-end 2026-10-28T15:06:25Z

Everything is a window delta: tick counts, overrun share, the tick
histogram's percentiles recomputed from bucket deltas, slow-hook and
slow-division hits per 10k ticks, and the journal bursts whose
tick_count_end falls inside the window. The journal outlives the
process while tick_count restarts at every boot, so pass the
window's wall-clock span too: a burst must intersect it, on its
ts_start/ts_end, to count. The filed baseline (7.9 days,
~35 sessions, ~6.8M ticks at the 100 ms interval) is printed beside
each row. Verdict thresholds are proposals stated in the code, for
the acceptance reader to ratify - the table is the deliverable.

Read-only: it opens the three files and changes nothing.

===========================================================================
"""
import argparse
import json
import math
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path

# The filed baseline (#1820, production host, 7.9 days uptime, ~35
# sessions). Tick-count denominator derived from the uptime at the
# 100 ms tick interval: 7.9 * 86400 * 10 = 6,825,600 ticks.
BASELINE_TICKS = 6_825_600
BASELINE = {
    "tick_p50_ms": 25.0,
    "tick_p90_ms": 50.0,
    "tick_p99_ms": 80.0,
    "tick_p999_ms": 160.0,
    "tick_max_ms": 2900.0,
    "overruns": 27_425,
    "slow_return_scrolls": 7_420,
    "slow_divisions_total": 781,
}


# ================
# percentile_from_buckets
#
# The histogram snapshot's percentile for one window: bucket counts
# are window deltas, a percentile reads as its bucket's upper bound,
# and the open last bucket reads as the window's spike max when known.
# A bounded bucket never takes the spike max: the journal is
# thresholded and the histogram is not, so a quiet journal must not
# pull a percentile below its own bucket.
# ================
def percentile_from_buckets(bounds, buckets, q, open_value):
    total = sum(buckets)
    if total == 0:
        return 0.0
    rank = math.ceil(q * total)
    seen = 0
    for i, n in enumerate(buckets):
        seen += n
        if seen >= rank:
            if i < len(bounds):
                return bounds[i]
            return open_value if open_value else bounds[-1]
    return open_value if open_value else bounds[-1]


# ================
# histogram_counts
#
# One snapshot's bucket counts. The served metrics name the field
# "counts" (bounds_ms plus one open last bucket, with the server's own
# percentiles beside it); "buckets" is accepted for snapshots taken
# before the rename. Reading only "buckets" made every real window
# delta empty, so the report refused live input as sample-less.
# ================
def histogram_counts(hist):
    return hist.get("counts") or hist.get("buckets") or []


# ================
# window_histogram
#
# Percentiles of the tick histogram over the window between the two
# snapshots. max comes from the spike journal, not the since-boot
# tick_max_ms, which a window cannot delta.
# ================
def window_histogram(before, after, spike_max_ms):
    hb = before.get("tick_ms_histogram") or {}
    ha = after.get("tick_ms_histogram") or {}
    bounds = ha.get("bounds_ms") or hb.get("bounds_ms") or []
    ba = histogram_counts(ha)
    bb = histogram_counts(hb) or [0] * len(ba)
    deltas = [max(0, a - b) for a, b in zip(ba, bb)]
    return {
        "p50_ms": percentile_from_buckets(bounds, deltas, 0.50, spike_max_ms),
        "p90_ms": percentile_from_buckets(bounds, deltas, 0.90, spike_max_ms),
        "p99_ms": percentile_from_buckets(bounds, deltas, 0.99, spike_max_ms),
        "p999_ms": percentile_from_buckets(bounds, deltas, 0.999, spike_max_ms),
        "samples": sum(deltas),
    }


# ================
# slow_totals
#
# Slow-hook and slow-division window deltas: hits (>= 100 ms) per name
# and in total. max_ms is the since-boot worst from the after snapshot:
# a maximum cannot be windowed from two snapshots, so it is reported
# under that name and never as the window's worst.
# ================
def slow_totals(before_map, after_map):
    rows = {}
    total_hits = 0
    worst = 0.0
    for name, a in sorted((after_map or {}).items()):
        b = (before_map or {}).get(name) or {}
        hits = int(a.get("count", 0)) - int(b.get("count", 0))
        if hits <= 0 and name not in (before_map or {}):
            hits = int(a.get("count", 0))
        if hits < 0:
            hits = 0
        max_ms = float(a.get("max_ms", 0.0))
        rows[name] = {"hits": hits, "max_ms": max_ms}
        total_hits += hits
        worst = max(worst, max_ms)
    return rows, total_hits, worst


# ================
# parse_ts
#
# One journal timestamp as an aware datetime, or None when the value
# is absent or unreadable. Journal rows carry Go's RFC 3339 form; a
# trailing Z and numeric offsets both parse, and a value with no
# offset reads as UTC so naive and aware rows still compare.
# ================
def parse_ts(value):
    if not isinstance(value, str) or not value:
        return None
    try:
        parsed = datetime.fromisoformat(value[:-1] + "+00:00" if value.endswith("Z") else value)
    except ValueError:
        return None
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=timezone.utc)
    return parsed


# ================
# ts_intersects
#
# Whether a burst's [ts_start, ts_end] span touches the wall-clock
# window. A row whose timestamps are absent or unreadable makes no
# wall-clock claim and keeps the tick_count bound alone.
# ================
def ts_intersects(row, win_lo, win_hi):
    start = parse_ts(row.get("ts_start"))
    end = parse_ts(row.get("ts_end"))
    if start is None or end is None:
        return True
    if win_hi is not None and start > win_hi:
        return False
    if win_lo is not None and end < win_lo:
        return False
    return True


# ================
# read_spikes
#
# Journal bursts inside the window, by tick_count_end: the burst a
# reader sees is the one that closed between the two snapshots. The
# journal outlives the process while tick_count restarts at every
# boot, so a previous boot's burst can carry a tick_count_end inside
# a later window; when the caller passes the window's wall-clock
# bounds, a burst must also intersect them to count.
# ================
def read_spikes(path, tick_lo, tick_hi, win_lo=None, win_hi=None):
    bursts = []
    if not path:
        return bursts
    for line in Path(path).read_text(encoding="utf-8", errors="replace").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            row = json.loads(line)
        except json.JSONDecodeError:
            continue
        if not isinstance(row, dict):
            continue
        end = row.get("tick_count_end", 0)
        if not tick_lo < end <= tick_hi:
            continue
        if win_lo is not None or win_hi is not None:
            if not ts_intersects(row, win_lo, win_hi):
                continue
        bursts.append(row)
    return bursts


# ================
# per_10k
# ================
def per_10k(count, ticks):
    if ticks <= 0:
        return 0.0
    return count * 10_000.0 / ticks


# Proposed verdict thresholds, for the acceptance reader to ratify.
OVERRUN_PASS_PER_10K = 4.0  # 10x under the baseline's ~40 per 10k
# Baseline slow-hook total: the 7,420 return-scroll hits plus the 678
# hits of the eight smaller named hooks in the filing.
BASELINE_SLOW_HOOK_HITS = 8_098


class WindowError(Exception):
    pass


# ================
# window_measures
#
# Every number the report prints, computed once so the self-test can
# pin it. A window whose tick delta or histogram sample count is not
# positive (a restart between snapshots, swapped files) is refused:
# its rates would all read zero and print green verdicts.
# ================
def window_measures(before, after, bursts):
    ticks = int(after.get("tick_count", 0)) - int(before.get("tick_count", 0))
    if ticks <= 0:
        raise WindowError(f"window has {ticks:,} ticks: the snapshots are swapped "
                          "or the GameWorld restarted between them")
    overruns = int(after.get("tick_overruns", 0)) - int(before.get("tick_overruns", 0))
    spike_max = max((float(b.get("max_ms", 0.0)) for b in bursts), default=0.0)
    hist = window_histogram(before, after, spike_max)
    if hist["samples"] <= 0:
        raise WindowError("the tick histogram recorded no samples in the window")
    hooks, hook_hits, _ = slow_totals(before.get("slow_hooks"), after.get("slow_hooks"))
    _, div_hits, _ = slow_totals(before.get("slow_divisions"), after.get("slow_divisions"))
    scroll = [(n, r) for n, r in hooks.items() if "eturnscroll" in n.lower()]
    scroll_hits = sum(r["hits"] for _, r in scroll)
    rows = [
        ("tick p50 ms", BASELINE["tick_p50_ms"], hist["p50_ms"]),
        ("tick p90 ms", BASELINE["tick_p90_ms"], hist["p90_ms"]),
        ("tick p99 ms", BASELINE["tick_p99_ms"], hist["p99_ms"]),
        ("tick p99.9 ms", BASELINE["tick_p999_ms"], hist["p999_ms"]),
        ("tick max ms (spike journal)", BASELINE["tick_max_ms"], spike_max),
        ("overruns per 10k ticks", per_10k(BASELINE["overruns"], BASELINE_TICKS), per_10k(overruns, ticks)),
        ("slow hooks per 10k ticks", per_10k(BASELINE_SLOW_HOOK_HITS, BASELINE_TICKS), per_10k(hook_hits, ticks)),
        ("slow divisions per 10k ticks", per_10k(BASELINE["slow_divisions_total"], BASELINE_TICKS),
         per_10k(div_hits, ticks)),
    ]
    verdicts = {
        "overrun share": per_10k(overruns, ticks) < OVERRUN_PASS_PER_10K,
        "return scrolls": scroll_hits == 0,
        "tick p99": hist["p99_ms"] <= BASELINE["tick_p99_ms"],
    }
    return {
        "ticks": ticks,
        "rows": rows,
        "hooks": hooks,
        "scroll": scroll,
        "scroll_hits": scroll_hits,
        "verdicts": verdicts,
    }


# ================
# report
# ================
def report(before, after, bursts):
    m = window_measures(before, after, bursts)
    ticks = m["ticks"]

    print("=== #1820 tick acceptance - window report ===")
    print(f"window: {ticks:,} ticks, {len(bursts)} spike bursts, "
          f"sessions {before.get('live_sessions', '?')} -> {after.get('live_sessions', '?')}")
    print()
    print(f"{'measure':<34}{'baseline':>12}{'window':>12}")
    for name, base, win in m["rows"]:
        print(f"{name:<34}{base:>12.2f}{win:>12.2f}")

    print()
    print("slow hooks over 100 ms (window hits, worst ms since boot, window hits per 10k ticks):")
    for name, row in sorted(m["hooks"].items(), key=lambda kv: -kv[1]["hits"]):
        if row["hits"] > 0:
            print(f"  {name:<44} {row['hits']:>6} {row['max_ms']:>9.1f} {per_10k(row['hits'], ticks):>8.2f}")
    print()
    print("filed culprits:")
    print(f"  return scrolls: baseline {BASELINE['slow_return_scrolls']} hits in 7.9 days; "
          f"window {m['scroll_hits']} hits "
          f"({', '.join(n for n, _ in m['scroll']) or 'no slow return-scroll step recorded'})")
    steps = {}
    for b in bursts:
        s = b.get("slowest_step") or "(unnamed)"
        steps[s] = steps.get(s, 0) + 1
    print(f"  spike slowest steps: {dict(sorted(steps.items(), key=lambda kv: -kv[1])) or 'no bursts'}")
    # #2295: the phase and hook that took the burst's time. Records from
    # before the attribution fix carry neither and tally as unattributed.
    phases = {}
    hooks = {}
    for b in bursts:
        p = b.get("slowest_phase") or "(unattributed)"
        phases[p] = phases.get(p, 0) + 1
        h = b.get("slowest_hook") or "(unattributed)"
        hooks[h] = hooks.get(h, 0) + 1
    print(f"  spike slowest phases: {dict(sorted(phases.items(), key=lambda kv: -kv[1])) or 'no bursts'}")
    print(f"  spike slowest hooks: {dict(sorted(hooks.items(), key=lambda kv: -kv[1])) or 'no bursts'}")

    v = m["verdicts"]
    print()
    print("proposed verdicts (ratify at acceptance):")
    print(f"  overrun share {'PASS' if v['overrun share'] else 'CHECK'} "
          f"(< {OVERRUN_PASS_PER_10K:g} per 10k = 10x under the baseline's ~40)")
    print(f"  return scrolls {'PASS' if v['return scrolls'] else 'CHECK'} "
          f"(mechanism closed in #1846: zero slow hits expected)")
    print(f"  tick p99 {'PASS' if v['tick p99'] else 'CHECK'} "
          f"(at or under the baseline's 80 ms, bucketed)")
    return 0


# ================
# self_test
#
# Synthetic snapshots: one window, known deltas. Run with --self-test.
# ================
def self_test():
    bounds = [10.0, 25.0, 50.0, 100.0]
    before = {
        "tick_count": 1000, "tick_overruns": 10, "live_sessions": 3,
        "tick_ms_histogram": {"bounds_ms": bounds, "counts": [900, 80, 15, 4, 1]},
        "slow_hooks": {"advanceReturnScrolls": {"count": 5, "max_ms": 300.0}},
        "slow_divisions": {},
    }
    after = {
        "tick_count": 3000, "tick_overruns": 12, "live_sessions": 5,
        "tick_ms_histogram": {"bounds_ms": bounds, "counts": [2700, 220, 55, 20, 5]},
        "slow_hooks": {"advanceReturnScrolls": {"count": 5, "max_ms": 300.0},
                       "advancePets": {"count": 2, "max_ms": 150.0}},
        "slow_divisions": {"7": {"count": 1, "max_ms": 120.0}},
    }
    # Window buckets [1800, 140, 40, 16, 4]: p50 and p90 in bucket 0
    # (10 ms), p99 rank 1980 ends bucket 2 (50 ms), p99.9 in the open
    # bucket, read as the spike max.
    hist = window_histogram(before, after, 700.0)
    assert hist["samples"] == 2000, hist
    assert hist["p50_ms"] == 10.0, hist
    assert hist["p90_ms"] == 10.0, hist
    assert hist["p99_ms"] == 50.0, hist
    assert hist["p999_ms"] == 700.0, hist
    # A bounded bucket keeps its bound under a quieter journal: p99 in
    # (50, 100] must not read as a 30 ms spike max.
    capped = percentile_from_buckets(bounds, [900, 50, 30, 20, 0], 0.99, 30.0)
    assert capped == 100.0, capped
    # The served shape, from a live snapshot (2026-10-28, the #2033
    # profile window): "counts" with the open bucket last and the
    # server's own percentiles printed beside them. The window math
    # must reproduce those percentiles from the counts alone; on the
    # pre-fix reader ("buckets" only) this window had no samples and
    # the report refused it.
    served_bounds = [2, 5, 10, 12, 14, 16, 18, 20, 25, 30, 40, 50, 60, 70, 80, 90,
                     100, 110, 120, 140, 160, 180, 200, 250, 300, 400, 500, 750, 1000, 2000]
    served_counts = [0, 0, 0, 3, 58, 551, 2389, 6031, 41582, 57781, 95971, 66978,
                     39266, 21472, 10737, 5073, 2338, 1159, 791, 981, 566, 239,
                     85, 46, 13, 15, 13, 28, 0, 0, 0]
    served = window_histogram(
        {"tick_ms_histogram": {"bounds_ms": served_bounds, "counts": [0] * len(served_counts)}},
        {"tick_ms_histogram": {"bounds_ms": served_bounds, "counts": served_counts}}, 0.0)
    assert served["samples"] == 354166, served
    assert (served["p50_ms"], served["p90_ms"], served["p99_ms"], served["p999_ms"]) == (40, 70, 110, 180), served
    hooks, hits, worst = slow_totals(before["slow_hooks"], after["slow_hooks"])
    assert hits == 2 and worst == 300.0, (hits, worst)
    assert hooks["advanceReturnScrolls"]["hits"] == 0
    spikes = [{"tick_count_end": 1500, "max_ms": 700.0, "slowest_step": "x"},
              {"tick_count_end": 900, "max_ms": 900.0, "slowest_step": "y"},
              # A burst from a previous boot: the journal outlives the
              # process while tick_count restarts at zero, so its
              # tick_count_end lands inside this window a boot later.
              # Its wall-clock span sits the day before the window.
              {"tick_count_end": 2000, "max_ms": 5000.0, "slowest_step": "z",
               "ts_start": "2026-10-27T03:21:00Z", "ts_end": "2026-10-27T03:21:02Z"},
              {"tick_count_end": 2500, "max_ms": 800.0, "slowest_step": "w",
               "ts_start": "2026-10-28T13:30:00Z", "ts_end": "2026-10-28T13:30:01Z"}]
    # The journal fixture lives in a private temp dir, never in the
    # caller's working directory. A valid-JSON non-object line is skipped.
    with tempfile.TemporaryDirectory() as tmp:
        journal = Path(tmp) / "tick-spikes.jsonl"
        lines = [json.dumps(s) for s in spikes] + ["42", "not json"]
        journal.write_text("\n".join(lines), encoding="utf-8")
        got = read_spikes(str(journal), 1000, 3000)
        # Tick-count alone counts the previous boot's burst too.
        assert len(got) == 3, got
        # The wall-clock bound drops it and keeps the in-window burst;
        # the row without timestamps keeps only the tick_count bound.
        win_lo = parse_ts("2026-10-28T13:05:26Z")
        win_hi = parse_ts("2026-10-28T15:06:25Z")
        got = read_spikes(str(journal), 1000, 3000, win_lo, win_hi)
    assert [b["max_ms"] for b in got] == [700.0, 800.0], got

    # The report's own numbers: 2 overruns, 2 slow-hook hits and 1 slow
    # division over 2,000 ticks are 10, 10 and 5 per 10k; the baseline
    # overrun row is 27,425 / 6,825,600 = 40.18 per 10k.
    m = window_measures(before, after, got)
    rows = {name: (base, win) for name, base, win in m["rows"]}
    assert m["ticks"] == 2000, m["ticks"]
    assert rows["overruns per 10k ticks"][1] == 10.0, rows
    assert round(rows["overruns per 10k ticks"][0], 2) == 40.18, rows
    assert rows["slow hooks per 10k ticks"][1] == 10.0, rows
    assert rows["slow divisions per 10k ticks"][1] == 5.0, rows
    assert rows["tick max ms (spike journal)"][1] == 800.0, rows
    assert m["verdicts"] == {"overrun share": False, "return scrolls": True, "tick p99": True}, m["verdicts"]

    # A window with no positive tick delta (a restart between snapshots)
    # or no histogram samples (swapped files) is refused, never reported
    # green. Each guard is exercised on its own.
    restarted = dict(after, tick_count=500)
    empty = dict(after, tick_ms_histogram=before["tick_ms_histogram"])
    for name, b, a in (("restarted", before, restarted), ("no samples", before, empty)):
        try:
            window_measures(b, a, [])
        except WindowError:
            continue
        raise AssertionError(f"a {name} window was reported")
    print("self-test PASS")


# ================
# main
# ================
def main(argv):
    parser = argparse.ArgumentParser(description="the #1820 tick acceptance window report")
    parser.add_argument("--before", help="metrics snapshot at window start (JSON)")
    parser.add_argument("--after", help="metrics snapshot at window end (JSON)")
    parser.add_argument("--spikes", help="tick-spikes.jsonl copied at window end")
    parser.add_argument("--window-start", help="wall-clock window start (ISO 8601); "
                        "journal bursts must intersect the window, not just its tick counts")
    parser.add_argument("--window-end", help="wall-clock window end (ISO 8601)")
    parser.add_argument("--self-test", action="store_true", help="run the synthetic self-test")
    args = parser.parse_args(argv)
    if args.self_test:
        self_test()
        return 0
    if not args.before or not args.after:
        parser.error("--before and --after are required (or --self-test)")
    win_lo = parse_ts(args.window_start) if args.window_start else None
    win_hi = parse_ts(args.window_end) if args.window_end else None
    if args.window_start and win_lo is None:
        parser.error(f"--window-start is not an ISO 8601 timestamp: {args.window_start}")
    if args.window_end and win_hi is None:
        parser.error(f"--window-end is not an ISO 8601 timestamp: {args.window_end}")
    before = json.loads(Path(args.before).read_text(encoding="utf-8"))
    after = json.loads(Path(args.after).read_text(encoding="utf-8"))
    bursts = read_spikes(args.spikes, int(before.get("tick_count", 0)), int(after.get("tick_count", 0)),
                         win_lo, win_hi)
    try:
        return report(before, after, bursts)
    except WindowError as err:
        print(f"tick_acceptance: refused: {err}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
