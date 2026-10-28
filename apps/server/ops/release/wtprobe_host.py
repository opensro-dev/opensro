#!/usr/bin/env python3
"""
===========================================================================

wtprobe_host.py - the game host's half of the off-host WebTransport probe

The off-host runner (wtprobe_verdict.py) pushes one verdict per probe run;
the availability monitor (monitor.py, kind wtprobe) reads it (#1984). This
module owns the game-host state that joins them, and nothing else:

  - the push account, whose only key runs wtprobe_verdict.py --receive
    as a forced command (no shell, no forwarding, no choice of path),
  - the verdict directory: written by the push account, read by the
    monitor,
  - the second monitor config and its 30-second service and timer.

Like quic_host.py, nothing here runs implicitly: the release controls are
replaced without install.main, so the operator runs "wtprobe_host.py apply
--public-key FILE" once, in an owner-approved window. "check" lists what is
missing; "remove" undoes it all. Owner decisions, 2026-10-28: the runner
lives on vps0, probes every 60 s, a verdict goes stale at 180 s, and a
stopped runner pages.

===========================================================================
"""

import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

from install import CONFIG, ROOT, install_file

PUSH_USER = "opensro-wtprobe"
MONITOR_USER = "opensro-monitor"
VERDICT_DIR = ROOT / "wtprobe"
VERDICT = VERDICT_DIR / "verdict.json"
MONITOR_CONFIG = Path("/etc/opensro-release/monitor-wtprobe.json")
WEBHOOK = Path("/etc/opensro-release/monitor-webhook")
UNIT_NAMES = ("opensro-monitor-wtprobe.service", "opensro-monitor-wtprobe.timer")
SYSTEMD = Path("/etc/systemd/system")
LABEL = "WebTransport (off-host probe)"


# ================
# push_keys
#
# The push account's authorized_keys: each Ed25519 key may run only the
# verdict receiver, with every other ssh capability restricted.
# ================
def push_keys(public_key, modules):
	keys = [line.split() for line in public_key.splitlines() if line.strip()]
	if not keys or any(len(key) < 2 or key[0] != "ssh-ed25519" for key in keys):
		raise ValueError("the probe push account requires Ed25519 public keys")
	command = "/usr/bin/python3 " + (Path(modules) / "wtprobe_verdict.py").as_posix() + " --receive " + VERDICT.as_posix()
	return "".join('restrict,command="' + command + '" ' + " ".join(key[:2]) + "\n" for key in keys)


# ================
# monitor_settings
#
# The wtprobe monitor's config: the fleet monitor's webhook and release
# journal, so release maintenance suppresses these pages exactly as it
# suppresses fleet pages.
# ================
def monitor_settings(production_state):
	return {"kind": "wtprobe", "verdict": VERDICT.as_posix(), "label": LABEL,
		"state": (ROOT / "public/health/wtprobe.json").as_posix(), "webhook": WEBHOOK.as_posix(),
		"production_state": production_state}


# ================
# ensure_user
# ================
def ensure_user(name, home, run=subprocess.run):
	import pwd
	try:
		pwd.getpwnam(name)
	except KeyError:
		run(["useradd", "--system", "--home-dir", str(home), "--create-home", "--shell", "/bin/sh", name], check=True)


# ================
# apply
#
# Install every piece and start the timer. Idempotent: a second apply
# replaces the keys, the config and the units in place.
# ================
def apply(public_key, modules, run=subprocess.run):
	if not WEBHOOK.exists():
		raise RuntimeError(WEBHOOK.as_posix() + " is missing: install the release controls' monitor first")
	production_state = json.loads(CONFIG.read_text())["production_state"]
	home = ROOT / "wtprobe-push"
	ensure_user(PUSH_USER, home, run)
	VERDICT_DIR.mkdir(parents=True, exist_ok=True)
	VERDICT_DIR.chmod(0o750)
	shutil.chown(VERDICT_DIR, user=PUSH_USER, group=MONITOR_USER)
	(home / ".ssh").mkdir(parents=True, exist_ok=True)
	(home / ".ssh").chmod(0o755)
	install_file(home / ".ssh/authorized_keys", push_keys(public_key, modules).encode())
	install_file(MONITOR_CONFIG, (json.dumps(monitor_settings(production_state), indent=2) + "\n").encode())
	for name in UNIT_NAMES:
		body = (Path(modules) / name).read_text().replace("@RELEASE_MODULES@", str(modules))
		install_file(SYSTEMD / name, body.encode())
	run(["systemctl", "daemon-reload"], check=True)
	run(["systemctl", "enable", "--now", "opensro-monitor-wtprobe.timer"], check=True)


# ================
# remove
#
# Stop the timer and delete the units, the config, the keys and the
# verdicts. The push account itself stays, with no key it is inert.
# ================
def remove(run=subprocess.run):
	run(["systemctl", "disable", "--now", "opensro-monitor-wtprobe.timer"], check=False)
	for path in (*(SYSTEMD / name for name in UNIT_NAMES), MONITOR_CONFIG, ROOT / "wtprobe-push/.ssh/authorized_keys"):
		if path.exists():
			path.unlink()
	if VERDICT_DIR.exists():
		shutil.rmtree(VERDICT_DIR)
	run(["systemctl", "daemon-reload"], check=True)


# ================
# problems
#
# Every gap between the host and a standing probe: the timer, the config,
# the push key and a fresh verdict. Empty means ready.
# ================
def problems(run=subprocess.run, now=None):
	found = []
	enabled = run(["systemctl", "is-enabled", "opensro-monitor-wtprobe.timer"], capture_output=True, text=True)
	if enabled.stdout.strip() != "enabled":
		found.append("opensro-monitor-wtprobe.timer is not enabled")
	if not MONITOR_CONFIG.exists():
		found.append(MONITOR_CONFIG.as_posix() + " is missing")
	if not (ROOT / "wtprobe-push/.ssh/authorized_keys").exists():
		found.append("the push account has no key, so no runner can deliver")
	if not VERDICT.exists():
		found.append("no verdict has arrived yet: start the runner (wtprobe_verdict.py --push " + PUSH_USER + "@this-host)")
	return found


# ================
# main
# ================
def main(argv=None):
	parser = argparse.ArgumentParser(description="Game-host state for the off-host WebTransport probe.")
	parser.add_argument("action", choices=("apply", "check", "remove"))
	parser.add_argument("--public-key", help="apply: the runner's Ed25519 public key file")
	arguments = parser.parse_args(argv)
	if os.geteuid() != 0:
		raise RuntimeError(arguments.action + " requires root")
	modules = Path(__file__).resolve().parent
	if arguments.action == "apply":
		if not arguments.public_key:
			parser.error("apply needs --public-key")
		apply(Path(arguments.public_key).read_text(), modules)
	elif arguments.action == "remove":
		remove()
		return 0
	gaps = problems()
	for gap in gaps:
		print("not ready: " + gap)
	if not gaps:
		print("ready: the WebTransport probe verdict reaches the monitor")
	return 1 if gaps else 0


if __name__ == "__main__":
	sys.exit(main())
