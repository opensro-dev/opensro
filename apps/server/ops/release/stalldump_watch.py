"""
===========================================================================

stalldump_watch.py - page the first tick-stall dump, exactly once.

The GameWorld tick watchdog writes tick-stall-<utc>.txt into the shard
authority's stall-dumps directory and exits; Nomad restarts the process,
so the availability monitor only sees a brief 502. This watch notices the
dump itself. A systemd timer runs it beside opensro-monitor (30 s): the
first run in a directory adopts every dump already there as its baseline
without paging, and each later new dump pages once - the filename plus
the dump's at/stalled/phase/hook header - over the monitor's Discord
webhook. A dump counts as seen only after Discord accepts its page, so a
failed delivery is retried on the next tick instead of lost. One bounded
observation per invocation; the scheduler owns cadence and serialization.

===========================================================================
"""

import argparse
import json
from pathlib import Path
import time

from deploy import announce
from release_state import write_state

DUMP_GLOB = "tick-stall-*.txt"
# A dump younger than this may still be flushing; it pages next tick.
MIN_AGE_SECONDS = 5
# Only the header block is read; the stacks below it stay on disk.
HEADER_BYTES = 4096
HEADER_KEYS = ("at", "stalled", "phase", "hook")


# ================
# dump_header
#
# The dump's at/stalled/phase/hook header lines, or an empty block when
# the file cannot be read yet. The writer's format is
# tick_watchdog.go's writeStallDump: one key, padding, value per line.
# ================
def dump_header(path):
	header = {}
	try:
		text = path.read_bytes()[:HEADER_BYTES].decode("utf-8", "replace")
	except OSError:
		return header
	for line in text.splitlines():
		key, separator, value = line.partition(":")
		if separator and key.strip() in HEADER_KEYS:
			header[key.strip()] = value.strip()
		if len(header) == len(HEADER_KEYS):
			break
	return header


# ================
# new_dumps
#
# Dump files not yet seen, oldest filename first (the filename is the
# UTC second the watchdog wrote it). Files still flushing are left for
# the next tick.
# ================
def new_dumps(directory, seen, now):
	fresh = []
	for path in Path(directory).glob(DUMP_GLOB):
		if path.name in seen or now - path.stat().st_mtime < MIN_AGE_SECONDS:
			continue
		fresh.append(path.name)
	return sorted(fresh)


# ================
# page_message
#
# The page: which dump, and the header an operator triages from.
# ================
def page_message(label, name, header):
	lines = ["OpenSRO " + label + " tick stall dump: " + name]
	for key in HEADER_KEYS:
		if key in header:
			lines.append(key + ": " + header[key])
	return "\n".join(lines)


# ================
# poll
#
# One observation. A missing state file is the baseline adoption: every
# dump already present is recorded seen and none of them pages. The
# heartbeat is persisted before delivery and the seen set after each
# accepted page, mirroring monitor.poll's acknowledgement discipline.
# ================
def poll(config, now, notify=announce):
	path = Path(config["state"])
	directory = Path(config["dir"])
	present = sorted(entry.name for entry in directory.glob(DUMP_GLOB)) if directory.exists() else []
	if not path.exists():
		state = {"baselineAt": now, "checkedAt": now, "seen": present}
		write_state(path, state)
		return state
	state = json.loads(path.read_text())
	seen = set(state.get("seen", []))
	state["checkedAt"] = now
	write_state(path, state)
	for name in new_dumps(directory, seen, now):
		header = dump_header(directory / name)
		notify(config["webhook"], page_message(config.get("label", "game server"), name, header))
		seen.add(name)
		state["seen"] = sorted(seen)
		state["lastPaged"] = name
		write_state(path, state)
	return state


# ================
# main
#
# stalldump_watch.py CONFIG, with CONFIG naming dir, state, webhook and
# label as JSON. Prints the observation state like monitor.py.
# ================
def main():
	parser = argparse.ArgumentParser()
	parser.add_argument("config")
	arguments = parser.parse_args()
	config = json.loads(Path(arguments.config).read_text())
	print(json.dumps(poll(config, time.time()), sort_keys=True))


if __name__ == "__main__":
	main()
