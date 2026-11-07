"""
===========================================================================

release_measure.py - the post-release re-measure for #2142 and #2148

One prepared read answers the two release criticals from a live boot,
instead of another improvisation at the metrics endpoint:

  (a) #2142, tick stalls: tick spikes over 100 ms with their named
      slowest steps - tick_acceptance.py's window report (two
      /transport/metrics snapshots plus the authority directory's
      tick-spikes.jsonl), fetched and filed by this script.

  (b) #2148, lost answers: client incidents per class per session-hour.
      Classes are the history fingerprints of the client_incident
      events the Agent records (/client/incident); transport stalls
      split by the lane the client names (movement, inventory,
      targeting - #2585). The denominator is connected session time
      in the window from the authority history's intervals, the same
      overlap-safe accounting the console shows.

At release-live, take the window's opening snapshot:

    py -3 scripts/analysis/release_measure.py snapshot \
        --out measure/before.json

Later, the whole read is one command:

    py -3 scripts/analysis/release_measure.py report \
        --authority <shard authority dir> \
        --agent-history <agent history.sqlite> \
        --before measure/before.json --label "first evening"

Tonight's backup window is the same report with its own bounds
(--window-start/--window-end); spikes and incidents are then read for
exactly that span. Without --before the read is since boot: the tick
window opens at tick zero and the incident window at the earliest
record in either store. Read-only: the only file written is the
snapshot `snapshot` is asked for.

===========================================================================
"""
import argparse
import json
import re
import sqlite3
import sys
import tempfile
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import tick_acceptance

DEFAULT_METRICS_URL = "http://127.0.0.1:8788/transport/metrics"
LANE_PATTERN = re.compile(r"the (movement|inventory|targeting) lane waited")
LANES = ("movement", "inventory", "targeting")


# ================
# fetch_metrics
#
# One /transport/metrics snapshot as served. The endpoint is loopback
# on the GameWorld host; a failure here is the operator's to see, so
# it raises and main reports it plainly.
# ================
def fetch_metrics(url):
    with urllib.request.urlopen(url, timeout=15) as response:
        return json.loads(response.read().decode("utf-8"))


# ================
# snapshot
#
# The window's opening metrics, with the wall-clock moment beside
# them: report reads the moment back as the default window start, so
# the operator never transcribes a timestamp at release time.
# ================
def snapshot(url, out):
    metrics = fetch_metrics(url)
    out = Path(out)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(metrics), encoding="utf-8")
    meta = {"taken_at": datetime.now(timezone.utc).isoformat(), "url": url,
            "tick_count": int(metrics.get("tick_count", 0))}
    out.with_suffix(".meta.json").write_text(json.dumps(meta), encoding="utf-8")
    print(f"snapshot: {out} at {meta['taken_at']} (tick {meta['tick_count']:,})")
    return 0


# ================
# lane_of
#
# The lane a transport-stall incident names (#2585: "the movement lane
# waited 8016 ms ..."), or "" when the message names none - a silent
# link stalls no lane, and the read must not invent one.
# ================
def lane_of(message):
    match = LANE_PATTERN.search(message or "")
    return match.group(1) if match else ""


# ================
# open_ro
#
# A history store opened read-only: the measure never writes to a
# live boot's evidence.
# ================
def open_ro(path):
    return sqlite3.connect(f"file:{Path(path).as_posix()}?mode=ro", uri=True)


# ================
# incident_rows
#
# The window's client incidents from the Agent history, grouped the
# way the console groups them (fingerprint: the structured code/opcode
# group, generic errors by message), transport stalls split by lane.
# degraded rows are cosmetic skips the client survived; they are
# counted apart from the failures and never enter the rate verdicts.
# ================
def incident_rows(agent_history, lo_ms, hi_ms):
    groups = {}
    if not agent_history:
        return groups
    conn = open_ro(agent_history)
    try:
        rows = conn.execute(
            "SELECT at, category, fingerprint, data FROM events "
            "WHERE kind = 'client_incident' AND at >= ? AND at <= ? ORDER BY at",
            (lo_ms, hi_ms))
        for at, category, fingerprint, data in rows:
            try:
                event = json.loads(data) if data else {}
            except json.JSONDecodeError:
                event = {}
            message = event.get("message", "")
            lane = lane_of(message)
            key = (category or "unknown", fingerprint or event.get("code", ""), lane)
            group = groups.setdefault(key, {"count": 0, "newest_at": 0, "example": ""})
            group["count"] += 1
            if at >= group["newest_at"]:
                group["newest_at"] = at
                group["example"] = message[:160]
    finally:
        conn.close()
    return groups


# ================
# session_window
#
# Connected session time inside the window, overlap-safe: each
# interval contributes only its overlap, and an interval still open
# (finish at or before its start) runs only to its session's end or
# last-seen mark - never to the window end. A session the boot lost
# track of was not connected until tonight: with no activity past
# its start it contributes nothing. Sessions started and
# ended in the window and the transport fallback reports (#2148's
# other face) ride along as context.
# ================
def session_window(authority, lo_ms, hi_ms):
    conn = open_ro(Path(authority) / "history.sqlite")
    try:
        sessions = {row[0]: (row[1], row[2], row[3]) for row in conn.execute(
            "SELECT id, started, seen, ended FROM sessions")}
        seconds = 0.0
        for start, finish, session_id in conn.execute("SELECT start, finish, session FROM intervals"):
            end = finish if finish and finish > start else 0
            if session_id in sessions:
                _, seen, ended = sessions[session_id]
                cap = ended if ended and ended > start else seen
                if cap and cap > start:
                    end = min(end, cap) if end else cap
                elif not end:
                    end = start
            elif not end:
                end = start
            overlap = min(end, hi_ms) - max(start, lo_ms)
            if overlap > 0:
                seconds += overlap / 1000.0
        started = conn.execute(
            "SELECT COUNT(*) FROM sessions WHERE started >= ? AND started <= ?",
            (lo_ms, hi_ms)).fetchone()[0]
        ended = conn.execute(
            "SELECT COUNT(*) FROM sessions WHERE ended >= ? AND ended <= ? AND ended > 0",
            (lo_ms, hi_ms)).fetchone()[0]
        fallbacks = conn.execute(
            "SELECT COUNT(*) FROM events WHERE kind = 'transport_reported' AND at >= ? AND at <= ?",
            (lo_ms, hi_ms)).fetchone()[0]
    finally:
        conn.close()
    return {"hours": seconds / 3600.0, "started": started, "ended": ended,
            "fallbacks": fallbacks}


# ================
# earliest_record
#
# The default window start without --before: the earliest timestamp
# any of the three stores carries for this boot, so "since boot"
# means the same span for spikes and incidents alike.
# ================
def earliest_record(authority, agent_history):
    marks = []
    journal = Path(authority) / "tick-spikes.jsonl"
    if journal.exists():
        for line in journal.read_text(encoding="utf-8", errors="replace").splitlines():
            try:
                row = json.loads(line)
            except json.JSONDecodeError:
                continue
            start = tick_acceptance.parse_ts(row.get("ts_start"))
            if start is not None:
                marks.append(start.timestamp() * 1000)
                break
    for path in (Path(authority) / "history.sqlite", agent_history):
        if path and Path(path).exists():
            conn = open_ro(path)
            try:
                first = conn.execute("SELECT MIN(at) FROM events").fetchone()[0]
                if first:
                    marks.append(first)
            finally:
                conn.close()
    return int(min(marks)) if marks else 0


# ================
# incident_report
#
# Section (b) of the read. Rates are per session-hour; a window with
# no connected time prints counts and says so instead of a rate.
# ================
def incident_report(groups, sessions, lo_ms, hi_ms):
    hours = sessions["hours"]
    print("=== #2148 client incidents per class per session-hour ===")
    print(f"window: {iso(lo_ms)} .. {iso(hi_ms)}; sessions started {sessions['started']}, "
          f"ended {sessions['ended']}, transport fallback reports {sessions['fallbacks']}; "
          f"connected time {hours:.3f} session-hours")
    failures = {k: v for k, v in groups.items() if k[0] != "degraded"}
    degraded = sum(v["count"] for k, v in groups.items() if k[0] == "degraded")
    if not failures:
        print("  no client incidents in the window")
    for (category, fingerprint, lane), group in sorted(
            failures.items(), key=lambda kv: -kv[1]["count"]):
        name = fingerprint or "(ungrouped)"
        if lane:
            name += f" [{lane} lane]"
        rate = f"{group['count'] / hours:8.2f}/h" if hours > 0 else "     n/a"
        print(f"  {category:<10} {name:<58} {group['count']:>5} {rate}")
        if group["example"]:
            print(f"             newest: {group['example']}")
    if degraded:
        print(f"  degraded (cosmetic, session kept): {degraded}")
    return failures


def iso(ms):
    return datetime.fromtimestamp(ms / 1000, timezone.utc).isoformat()


# ================
# report
# ================
def report(args):
    authority = Path(args.authority)
    after = (json.loads(Path(args.after).read_text(encoding="utf-8")) if args.after
             else fetch_metrics(args.metrics_url))
    before = (json.loads(Path(args.before).read_text(encoding="utf-8")) if args.before
              else {})
    now_ms = int(datetime.now(timezone.utc).timestamp() * 1000)
    lo_ms = (int(tick_acceptance.parse_ts(args.window_start).timestamp() * 1000)
             if args.window_start else None)
    hi_ms = (int(tick_acceptance.parse_ts(args.window_end).timestamp() * 1000)
             if args.window_end else now_ms)
    if lo_ms is None and args.before:
        meta = Path(args.before).with_suffix(".meta.json")
        if meta.exists():
            taken = tick_acceptance.parse_ts(json.loads(meta.read_text(encoding="utf-8"))["taken_at"])
            lo_ms = int(taken.timestamp() * 1000) if taken else None
    if lo_ms is None:
        lo_ms = earliest_record(authority, args.agent_history)
    if args.label:
        print(f"release re-measure: {args.label}")

    groups = incident_rows(args.agent_history, lo_ms, hi_ms)
    sessions = session_window(authority, lo_ms, hi_ms)
    incident_report(groups, sessions, lo_ms, hi_ms)
    print()

    journal = authority / "tick-spikes.jsonl"
    bursts = tick_acceptance.read_spikes(
        str(journal), int(before.get("tick_count", 0)), int(after.get("tick_count", 0)),
        tick_acceptance.parse_ts(iso(lo_ms)), tick_acceptance.parse_ts(iso(hi_ms)))
    print("=== #2142 tick acceptance (tick_acceptance.py) ===")
    try:
        return tick_acceptance.report(before, after, bursts)
    except tick_acceptance.WindowError as err:
        print(f"tick window refused: {err}", file=sys.stderr)
        return 2


# ================
# self_test
#
# Synthetic stores: known incidents in two lanes, a degraded skip, an
# out-of-window incident, and intervals that overlap the window by a
# known amount. Run with the self-test subcommand.
# ================
def self_test():
    with tempfile.TemporaryDirectory() as tmp:
        agent = Path(tmp) / "agent.sqlite"
        conn = sqlite3.connect(agent)
        conn.execute("CREATE TABLE events (at INTEGER, category TEXT, fingerprint TEXT, data TEXT, kind TEXT)")
        # Columns: at, category, fingerprint, data, kind.
        rows = [
            (1_000, "unknown", "transport_failure",
             {"message": "Transport stalled: the movement lane waited 8016 ms with no frame arriving"},
             "client_incident"),
            (2_000, "unknown", "transport_failure",
             {"message": "Transport stalled: the movement lane waited 9000 ms with no frame arriving"},
             "client_incident"),
            (3_000, "unknown", "transport_failure",
             {"message": "Transport stalled: the inventory lane waited 5000 ms with no frame arriving"},
             "client_incident"),
            (4_000, "degraded", "asset_missing", {"message": "effect skipped"}, "client_incident"),
            (9_999_999, "unknown", "transport_failure",
             {"message": "Transport stalled: the targeting lane waited 1000 ms with no frame arriving"},
             "client_incident"),
            (1_500, "software", "x", {"message": "not an incident"}, "log"),
        ]
        conn.executemany("INSERT INTO events VALUES (?, ?, ?, ?, ?)",
                         [(at, cat, fp, json.dumps(data), kind) for at, cat, fp, data, kind in rows])
        conn.commit()
        conn.close()
        groups = incident_rows(str(agent), 0, 10_000)
        lanes = {(k[0], k[2]): v["count"] for k, v in groups.items()}
        assert lanes[("unknown", "movement")] == 2, lanes
        assert lanes[("unknown", "inventory")] == 1, lanes
        assert lanes[("degraded", "")] == 1, lanes
        assert ("unknown", "targeting") not in lanes, lanes

        authority = Path(tmp) / "authority"
        authority.mkdir()
        conn = sqlite3.connect(authority / "history.sqlite")
        conn.execute("CREATE TABLE intervals (start INTEGER, finish INTEGER, session TEXT)")
        conn.execute("CREATE TABLE sessions (id TEXT, started INTEGER, seen INTEGER, ended INTEGER)")
        conn.execute("CREATE TABLE events (at INTEGER, kind TEXT)")
        # One hour fully inside, one hour half inside, and one interval
        # left open by its session (last seen at the window end):
        # 1 + 0.5 + 1 = 2.5 hours, the open one capped at last-seen,
        # not at tonight.
        conn.executemany("INSERT INTO intervals VALUES (?, ?, ?)",
                         [(0, 3_600_000, "a"), (1_800_000, 5_400_000, "b"), (0, 0, "c"),
                          (1_000_000, 0, "d")])
        conn.executemany("INSERT INTO sessions VALUES (?, ?, ?, ?)",
                         [("a", 0, 3_600_000, 3_600_000), ("b", 1_000, 5_400_000, 5_400_000),
                          ("c", 2_000, 3_600_000, 0), ("d", 500_000, 1_000_000, 0)])
        conn.execute("INSERT INTO events VALUES (1500, 'transport_reported')")
        conn.commit()
        conn.close()
        # Session d's interval is open with no activity past its start:
        # it contributes nothing, not the whole window.
        window = session_window(authority, 0, 3_600_000)
        assert abs(window["hours"] - 2.5) < 1e-9, window
        assert window["started"] == 4 and window["ended"] == 1 and window["fallbacks"] == 1, window
    assert lane_of("the targeting lane waited 5 ms") == "targeting"
    assert lane_of("link silent for 9000 ms") == ""
    print("self-test PASS")
    return 0


# ================
# main
# ================
def main(argv):
    parser = argparse.ArgumentParser(description="post-release re-measure for #2142 and #2148")
    sub = parser.add_subparsers(dest="command", required=True)
    snap = sub.add_parser("snapshot", help="take the window's opening /transport/metrics snapshot")
    snap.add_argument("--metrics-url", default=DEFAULT_METRICS_URL)
    snap.add_argument("--out", required=True, help="where the snapshot JSON goes")
    read = sub.add_parser("report", help="the one-command read over a window")
    read.add_argument("--authority", required=True,
                      help="shard authority dir (tick-spikes.jsonl + history.sqlite)")
    read.add_argument("--agent-history", help="the Agent's history.sqlite (client incidents)")
    read.add_argument("--before", help="snapshot JSON at window start (absent: since boot)")
    read.add_argument("--after", help="snapshot JSON at window end (absent: fetched live)")
    read.add_argument("--metrics-url", default=DEFAULT_METRICS_URL)
    read.add_argument("--window-start", help="ISO 8601; default: the --before snapshot's moment, else earliest record")
    read.add_argument("--window-end", help="ISO 8601; default: now")
    read.add_argument("--label", help="printed at the head of the report")
    sub.add_parser("self-test", help="run the synthetic self-test")
    args = parser.parse_args(argv)
    if args.command == "self-test":
        return self_test()
    if args.command == "snapshot":
        return snapshot(args.metrics_url, args.out)
    for value, name in ((args.window_start, "--window-start"), (args.window_end, "--window-end")):
        if value and tick_acceptance.parse_ts(value) is None:
            parser.error(f"{name} is not an ISO 8601 timestamp: {value}")
    return report(args)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
