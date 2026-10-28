#!/usr/bin/env python3
"""
===========================================================================

wtprobe_verdict.py - one off-host WebTransport probe run, left as a verdict

The standing outside check of the WebTransport leg (#1984). Run from a host
that is not the game host, on that host's own timer: each invocation runs
sro-wtprobe -health once against the public origin and writes one JSON
verdict {v, checkedAt, ok, detail}. With --push the verdict is copied to
the game host beside the monitor, under a sibling .incoming name and then
renamed, so the monitor (monitor.py, kind wtprobe) never reads half a file.

The probe itself is unchanged and credential-free: it proves a stranger's
QUIC session opens and its HELLO is refused, nothing more. Any failure to
run it - a timeout, a missing binary - becomes a failed verdict naming the
stage, never a crash, so the monitor sees a reason rather than silence.

===========================================================================
"""

import argparse
import json
import os
from pathlib import Path
import shlex
import subprocess
import tempfile
import time

VERDICT_VERSION = 1
# A whole -health run: QUIC handshake, certificate pin and one refused
# HELLO. Longer than the probe's own default budget, shorter than the
# 60-second runner cadence.
PROBE_TIMEOUT_SECONDS = 45
PUSH_TIMEOUT_SECONDS = 30
MAX_DETAIL_CHARACTERS = 240


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
# push_commands
#
# The two commands that publish a local verdict to user@host:/path: copy
# it to a sibling .incoming name, then rename it over the target there.
# ================
def push_commands(local, target):
	destination, separator, remote = target.rpartition(":")
	if not separator or not destination or not remote.startswith("/"):
		raise ValueError("--push wants user@host:/absolute/path")
	incoming = remote + ".incoming"
	return [
		["scp", "-q", "-o", "BatchMode=yes", str(local), destination + ":" + incoming],
		["ssh", "-o", "BatchMode=yes", destination, "mv -f %s %s" % (shlex.quote(incoming), shlex.quote(remote))],
	]


# ================
# push_verdict
# ================
def push_verdict(local, target, run=subprocess.run):
	for command in push_commands(local, target):
		run(command, check=True, timeout=PUSH_TIMEOUT_SECONDS)


# ================
# main
#
# One probe run per invocation; the runner host's timer owns the cadence.
# ================
def main():
	parser = argparse.ArgumentParser()
	parser.add_argument("--probe", required=True, help="the sro-wtprobe binary")
	parser.add_argument("--origin", required=True, help="the public HTTPS origin, e.g. https://play.example.net")
	parser.add_argument("--out", required=True, help="the local verdict file")
	parser.add_argument("--push", help="user@host:/path of the monitor's verdict file")
	arguments = parser.parse_args()
	ok, detail = run_probe(arguments.probe, arguments.origin)
	verdict = make_verdict(ok, detail, time.time())
	write_verdict(arguments.out, verdict)
	if arguments.push:
		push_verdict(arguments.out, arguments.push)
	print(json.dumps(verdict, sort_keys=True))


if __name__ == "__main__":
	main()
