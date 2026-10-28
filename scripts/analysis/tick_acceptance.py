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
        --after after.json --spikes tick-spikes.jsonl

Everything is a window delta: tick counts, overrun share, the tick
histogram's percentiles recomputed from bucket deltas, slow-hook and
slow-division hits per 10k ticks, and the journal bursts whose
tick_count_end falls inside the window. The filed baseline (7.9 days,
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
                return min(bounds[i], open_value) if open_value else bounds[i]
            return open_value if open_value else bounds[-1]
    return open_value if open_value else bounds[-1]


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
    bb = hb.get("buckets") or [0] * len(ha.get("buckets") or [])
    ba = ha.get("buckets") or []
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
# Slow-hook and slow-division window deltas: hits (>= 100 ms) and the
# worst single hit, per name and in total.
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
# read_spikes
#
# Journal bursts inside the window, by tick_count_end: the burst a
# reader sees is the one that closed between the two snapshots.
# ================
def read_spikes(path, tick_lo, tick_hi):
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
        end = row.get("tick_count_end", 0)
        if tick_lo < end <= tick_hi:
            bursts.append(row)
    return bursts


# ================
# per_10k
# ================
def per_10k(count, ticks):
    if ticks <= 0:
        return 0.0
    return count * 10_000.0 / ticks


# ================
# report
# ================
def report(before, after, bursts):
    ticks = int(after.get("tick_count", 0)) - int(before.get("tick_count", 0))
    overruns = int(after.get("tick_overruns", 0)) - int(before.get("tick_overruns", 0))
    spike_max = max((float(b.get("max_ms", 0.0)) for b in bursts), default=0.0)
    hist = window_histogram(before, after, spike_max)
    hooks, hook_hits, hook_worst = slow_totals(before.get("slow_hooks"), after.get("slow_hooks"))
    divs, div_hits, div_worst = slow_totals(before.get("slow_divisions"), after.get("slow_divisions"))

    print("=== #1820 tick acceptance - window report ===")
    print(f"window: {ticks:,} ticks, {len(bursts)} spike bursts, "
          f"sessions {before.get('live_sessions', '?')} -> {after.get('live_sessions', '?')}")
    print()
    print(f"{'measure':<34}{'baseline':>12}{'window':>12}")
    rows = [
        ("tick p50 ms", BASELINE["tick_p50_ms"], hist["p50_ms"]),
        ("tick p90 ms", BASELINE["tick_p90_ms"], hist["p90_ms"]),
        ("tick p99 ms", BASELINE["tick_p99_ms"], hist["p99_ms"]),
        ("tick p99.9 ms", BASELINE["tick_p999_ms"], hist["p999_ms"]),
        ("tick max ms (spike journal)", BASELINE["tick_max_ms"], spike_max),
        ("overruns per 10k ticks", per_10k(BASELINE["overruns"], BASELINE_TICKS), per_10k(overruns, ticks)),
        # Baseline slow-hook total: the 7,420 return-scroll hits plus
        # the 678 hits of the eight smaller named hooks in the filing.
        ("slow hooks per 10k ticks", per_10k(8_098, BASELINE_TICKS), per_10k(hook_hits, ticks)),
        ("slow divisions per 10k ticks", per_10k(BASELINE["slow_divisions_total"], BASELINE_TICKS), per_10k(div_hits, ticks)),
    ]
    for name, base, win in rows:
        print(f"{name:<34}{base:>12.2f}{win:>12.2f}")

    print()
    print("slow hooks over 100 ms in window (hits, worst ms, per 10k ticks):")
    for name, row in sorted(hooks.items(), key=lambda kv: -kv[1]["hits"]):
        if row["hits"] > 0:
            print(f"  {name:<44} {row['hits']:>6} {row['max_ms']:>9.1f} {per_10k(row['hits'], ticks):>8.2f}")
    scroll = [(n, r) for n, r in hooks.items() if "eturnscroll" in n.lower()]
    print()
    print("filed culprits:")
    print(f"  return scrolls: baseline {BASELINE['slow_return_scrolls']} hits in 7.9 days; "
          f"window {sum(r['hits'] for _, r in scroll)} hits "
          f"({', '.join(n for n, _ in scroll) or 'no slow return-scroll step recorded'})")
    steps = {}
    for b in bursts:
        s = b.get("slowest_step") or "(unnamed)"
        steps[s] = steps.get(s, 0) + 1
    print(f"  spike slowest steps: {dict(sorted(steps.items(), key=lambda kv: -kv[1])) or 'no bursts'}")

    print()
    print("proposed verdicts (ratify at acceptance):")
    print(f"  overrun share {'PASS' if per_10k(overruns, ticks) < 4 else 'CHECK'} "
          f"(< 4 per 10k = 10x under the baseline's ~40)")
    print(f"  return scrolls {'PASS' if sum(r['hits'] for _, r in scroll) == 0 else 'CHECK'} "
          f"(mechanism closed in #1846: zero slow hits expected)")
    print(f"  tick p99 {'PASS' if hist['p99_ms'] <= BASELINE['tick_p99_ms'] else 'CHECK'} "
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
        "tick_ms_histogram": {"bounds_ms": bounds, "buckets": [900, 80, 15, 4, 1]},
        "slow_hooks": {"advanceReturnScrolls": {"count": 5, "max_ms": 300.0}},
        "slow_divisions": {},
    }
    after = {
        "tick_count": 3000, "tick_overruns": 12, "live_sessions": 5,
        "tick_ms_histogram": {"bounds_ms": bounds, "buckets": [2700, 220, 55, 20, 5]},
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
    assert hist["p99_ms"] == 50.0, hist
    assert hist["p999_ms"] == 700.0, hist
    hooks, hits, worst = slow_totals(before["slow_hooks"], after["slow_hooks"])
    assert hits == 2 and worst == 300.0, (hits, worst)
    assert hooks["advanceReturnScrolls"]["hits"] == 0
    spikes = [{"tick_count_end": 1500, "max_ms": 700.0, "slowest_step": "x"},
              {"tick_count_end": 900, "max_ms": 900.0, "slowest_step": "y"}]
    # The journal fixture lives in a private temp dir, never in the
    # caller's working directory.
    with tempfile.TemporaryDirectory() as tmp:
        journal = Path(tmp) / "tick-spikes.jsonl"
        journal.write_text("\n".join(json.dumps(s) for s in spikes), encoding="utf-8")
        got = read_spikes(str(journal), 1000, 3000)
    assert len(got) == 1 and got[0]["max_ms"] == 700.0, got
    print("self-test PASS")


# ================
# main
# ================
def main(argv):
    parser = argparse.ArgumentParser(description="the #1820 tick acceptance window report")
    parser.add_argument("--before", help="metrics snapshot at window start (JSON)")
    parser.add_argument("--after", help="metrics snapshot at window end (JSON)")
    parser.add_argument("--spikes", help="tick-spikes.jsonl copied at window end")
    parser.add_argument("--self-test", action="store_true", help="run the synthetic self-test")
    args = parser.parse_args(argv)
    if args.self_test:
        self_test()
        return 0
    if not args.before or not args.after:
        parser.error("--before and --after are required (or --self-test)")
    before = json.loads(Path(args.before).read_text(encoding="utf-8"))
    after = json.loads(Path(args.after).read_text(encoding="utf-8"))
    bursts = read_spikes(args.spikes, int(before.get("tick_count", 0)), int(after.get("tick_count", 0)))
    return report(before, after, bursts)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
