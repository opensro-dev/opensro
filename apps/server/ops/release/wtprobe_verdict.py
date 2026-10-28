#!/usr/bin/env python3
"""
===========================================================================

wtprobe_verdict.py - one off-host WebTransport probe run, left as a verdict

The standing outside check of the WebTransport leg (#1984). Run from a host
that is not the game host, on that host's own timer: each invocation runs
sro-wtprobe -health once against the public origin and writes one JSON
verdict {v, checkedAt, ok, detail}. With --push user@game-host the verdict
goes over ssh on stdin; on the game host that key's forced command is this
script's --receive mode (wtprobe_host.py installs it), which checks the
verdict and renames it into place, so the monitor (monitor.py, kind
wtprobe) never reads half a file and the push key can do nothing else.

The probe itself is unchanged and credential-free: it proves a stranger's
QUIC session opens and its HELLO is refused, nothing more. Any failure to
run it - a timeout, a missing binary - becomes a failed verdict naming the
stage, never a crash, so the monitor sees a reason rather than silence.

===========================================================================
"""

import argparse
import json
import math
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time

VERDICT_VERSION = 1
# A whole -health run: QUIC handshake, certificate pin and one refused
# HELLO. Longer than the probe's own default budget, shorter than the
# 60-second runner cadence.
PROBE_TIMEOUT_SECONDS = 45
PUSH_TIMEOUT_SECONDS = 30
MAX_DETAIL_CHARACTERS = 240
# A verdict is one small JSON object; the receiver reads no more than this.
MAX_VERDICT_BYTES = 4096


# ================
# run_probe
#
# One -health run. Returns whether it passed and the line that says so:
# the probe's last output line (its pass line, or "FAIL stage=..."), or a
# named stage of our own when the probe could not run at all.
# ================
def run_probe(probe, origin, run=subprocess.run):
	try:
		done = run([probe, "-health", "-url", origin], capture_output=True, text=True, timeout=PROBE_TIMEOUT_SECONDS)
	except subprocess.TimeoutExpired:
		return False, "FAIL stage=timeout: no result within %d s" % PROBE_TIMEOUT_SECONDS
	except OSError as error:
		return False, "FAIL stage=launch: %s" % error
	lines = [line.strip() for line in (done.stdout + "\n" + done.stderr).splitlines() if line.strip()]
	detail = lines[-1] if lines else "exit status %d" % done.returncode
	return done.returncode == 0, detail


# ================
# make_verdict
# ================
def make_verdict(ok, detail, now):
	return {"v": VERDICT_VERSION, "checkedAt": now, "ok": ok, "detail": detail[:MAX_DETAIL_CHARACTERS]}


# ================
# write_verdict
#
# Atomic on one filesystem: a temporary file beside the target, then a
# rename over it.
# ================
def write_verdict(path, verdict):
	path = Path(path)
	descriptor, temporary = tempfile.mkstemp(prefix=path.name + ".", suffix=".tmp", dir=path.parent)
	try:
		with os.fdopen(descriptor, "w", encoding="utf-8") as file:
			json.dump(verdict, file, sort_keys=True)
		os.replace(temporary, path)
	except BaseException:
		Path(temporary).unlink(missing_ok=True)
		raise


# ================
# push_command
#
# The one command that publishes a verdict to user@host: the verdict goes on
# stdin, and the host key's forced command decides where it lands.
# ================
def push_command(target):
	user, separator, host = target.partition("@")
	if not separator or not user or not host or any(c in target for c in ":/ "):
		raise ValueError("--push wants user@host")
	return ["ssh", "-o", "BatchMode=yes", target]


# ================
# push_verdict
# ================
def push_verdict(local, target, run=subprocess.run):
	with open(local, "rb") as stream:
		run(push_command(target), stdin=stream, check=True, timeout=PUSH_TIMEOUT_SECONDS)


# ================
# parse_verdict
#
# A verdict as the receiver accepts it: one JSON object of exactly the
# runner's four fields, a finite probe time and a bounded detail. Anything
# else is refused, never written.
# ================
def parse_verdict(data):
	if len(data) > MAX_VERDICT_BYTES:
		raise ValueError("verdict exceeds %d bytes" % MAX_VERDICT_BYTES)
	verdict = json.loads(data)
	if not isinstance(verdict, dict) or set(verdict) != {"v", "checkedAt", "ok", "detail"}:
		raise ValueError("verdict fields are not v, checkedAt, ok, detail")
	checked_at = verdict["checkedAt"]
	if verdict["v"] != VERDICT_VERSION or isinstance(checked_at, bool) or not isinstance(checked_at, (int, float)) or not math.isfinite(checked_at):
		raise ValueError("verdict version or probe time is unusable")
	if not isinstance(verdict["ok"], bool) or not isinstance(verdict["detail"], str) or len(verdict["detail"]) > MAX_DETAIL_CHARACTERS:
		raise ValueError("verdict result or detail is unusable")
	return verdict


# ================
# receive
#
# The game host's half: one verdict from stdin, checked, then renamed over
# path. A refused verdict leaves the previous one in place, so the monitor
# reports staleness rather than garbage.
# ================
def receive(path, stream):
	write_verdict(path, parse_verdict(stream.read(MAX_VERDICT_BYTES + 1)))


# ================
# main
#
# One probe run per invocation; the runner host's timer owns the cadence.
# ================
def main():
	parser = argparse.ArgumentParser()
	parser.add_argument("--receive", help="game host: read one verdict from stdin into this file")
	parser.add_argument("--probe", help="the sro-wtprobe binary")
	parser.add_argument("--origin", help="the public HTTPS origin, e.g. https://play.example.net")
	parser.add_argument("--out", help="the local verdict file")
	parser.add_argument("--push", help="user@game-host whose key receives the verdict")
	arguments = parser.parse_args()
	if arguments.receive:
		receive(arguments.receive, sys.stdin.buffer)
		return
	if not (arguments.probe and arguments.origin and arguments.out):
		parser.error("a probe run needs --probe, --origin and --out")
	ok, detail = run_probe(arguments.probe, arguments.origin)
	verdict = make_verdict(ok, detail, time.time())
	write_verdict(arguments.out, verdict)
	if arguments.push:
		push_verdict(arguments.out, arguments.push)
	print(json.dumps(verdict, sort_keys=True))


if __name__ == "__main__":
	main()
