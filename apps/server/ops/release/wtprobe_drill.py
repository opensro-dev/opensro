#!/usr/bin/env python3
"""
===========================================================================

wtprobe_drill.py - the announced WebTransport-only break test (#1984)

Proves the standing probe pages when only the WebTransport leg breaks, and
recovers when it returns. It fires a real staff page, so the coordinator
announces it first and runs it outside a release maintenance window (which
would suppress the page by design); --announced records that this was done.

On the game host, as root:

  1. preflight: the wtprobe and fleet monitors are both up, no release
     operation is open;
  2. arm a fallback: a transient systemd timer deletes the drill table
     after MAX_BREAK_SECONDS whatever happens to this process;
  3. break: an nftables table drops inbound UDP 443 before the redirect
     sees it; TCP (HTTPS, the WebSocket path) is untouched;
  4. wait for the wtprobe monitor to report "down" and to have notified it,
     while the fleet monitor stays up (only the WT leg is broken);
  5. restore: delete the table (also on any failure), then wait for the
     recovery to be notified.

The report is one JSON line. Exit 0 only when both the page and the
recovery were observed and the fleet stayed up throughout.

===========================================================================
"""

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import time

DRILL_TABLE = "opensro_wtdrill"
PUBLIC_PORT = 443
# Two failed probe runs at a 60 s cadence plus the monitor's 30 s poll:
# about three minutes to the page; the bound leaves margin for one slow run.
PAGE_TIMEOUT_SECONDS = 360
RECOVERY_TIMEOUT_SECONDS = 360
# The fallback removal: the page and the recovery both fit well inside it.
MAX_BREAK_SECONDS = 600
POLL_SECONDS = 5
WTPROBE_STATE = Path("/var/lib/opensro-release/public/health/wtprobe.json")
FLEET_STATE = Path("/var/lib/opensro-release/public/health/fleet.json")
PRODUCTION_STATE = Path("/var/lib/opensro-release/public/production.json")


# ================
# break_rules
#
# Drop inbound UDP 443 at raw priority, ahead of the QUIC redirect's dstnat
# hook, in a table of its own so deleting it restores exactly what was.
# ================
def break_rules():
	return ("table inet " + DRILL_TABLE + " {\n"
		"\tchain prerouting {\n"
		"\t\ttype filter hook prerouting priority raw; policy accept;\n"
		"\t\tudp dport " + str(PUBLIC_PORT) + " drop\n"
		"\t}\n"
		"}\n")


# ================
# Drill
#
# The host operations, injected so tests capture every command and drive
# the monitor states on a fake clock.
# ================
class Drill:
	def __init__(self, run=subprocess.run, read=lambda path: json.loads(Path(path).read_text()),
			clock=time.time, sleep=time.sleep):
		self.run, self.read, self.clock, self.sleep = run, read, clock, sleep

	# ================
	# preflight
	# ================
	def preflight(self):
		wtprobe, fleet = self.read(WTPROBE_STATE), self.read(FLEET_STATE)
		if wtprobe.get("phase") != "up" or fleet.get("phase") != "up":
			raise RuntimeError("both monitors must be up before the drill (wtprobe %s, fleet %s)" % (wtprobe.get("phase"), fleet.get("phase")))
		if self.read(PRODUCTION_STATE).get("operation"):
			raise RuntimeError("a release operation is open: its maintenance window would suppress the page")

	# ================
	# wait_for
	#
	# Polls the wtprobe state until notified equals want; the fleet must stay
	# up the whole time, or the drill broke more than the WT leg.
	# ================
	def wait_for(self, want, timeout):
		deadline = self.clock() + timeout
		while True:
			if self.read(FLEET_STATE).get("phase") != "up":
				raise RuntimeError("the fleet monitor left 'up': the drill broke more than WebTransport")
			if self.read(WTPROBE_STATE).get("notified") == want:
				return self.clock()
			if self.clock() >= deadline:
				raise RuntimeError("no '%s' notification within %d s" % (want, timeout))
			self.sleep(POLL_SECONDS)

	# ================
	# execute
	# ================
	def execute(self):
		self.preflight()
		started = self.clock()
		self.run(["systemd-run", "--unit", DRILL_TABLE + "-fallback", "--on-active=" + str(MAX_BREAK_SECONDS),
			"/usr/sbin/nft", "delete", "table", "inet", DRILL_TABLE], check=True)
		report = {"started": started}
		try:
			self.run(["/usr/sbin/nft", "-f", "-"], input=break_rules(), text=True, check=True)
			report["paged"] = self.wait_for("down", PAGE_TIMEOUT_SECONDS) - started
		finally:
			self.run(["/usr/sbin/nft", "delete", "table", "inet", DRILL_TABLE], check=False)
			self.run(["systemctl", "stop", DRILL_TABLE + "-fallback.timer"], check=False)
		restored = self.clock()
		report["recovered"] = self.wait_for("up", RECOVERY_TIMEOUT_SECONDS) - restored
		return report


# ================
# main
# ================
def main(argv=None):
	parser = argparse.ArgumentParser(description="The announced WebTransport-only break test.")
	parser.add_argument("--announced", action="store_true", required=True,
		help="confirms the coordinator announced this drill: it pages staff")
	parser.parse_args(argv)
	if os.geteuid() != 0:
		raise RuntimeError("the drill requires root: it changes nftables")
	print(json.dumps(Drill().execute(), sort_keys=True))
	return 0


if __name__ == "__main__":
	sys.exit(main())
