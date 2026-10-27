#!/usr/bin/env python3
"""
===========================================================================

quic_host.py - the host half of public WebTransport: UDP 443 to the bind

The GameWorld runs unprivileged (Nomad raw_exec as the sro user) and the
host lets only root bind ports below 1024, so the public WebTransport
listener binds a high port and the host redirects public UDP 443 to it
(#1447). This module owns that host state and nothing else:

  - the QUIC socket-buffer sysctls (install.quic_sysctls),
  - an nftables table that redirects UDP 443 to the bind port,
  - a oneshot unit that loads the table at boot,
  - the firewall opening for the bind port (the filter sees the
    redirected port, not 443).

The release controls are replaced without install.main (opensro-ops
release_controls.py), so nothing here runs implicitly: the operator runs
"quic_host.py apply --port N" once, in the owner-approved window, and
deploy.py refuses a public WebTransport deploy while "check" finds a gap.
"remove" undoes the redirect, the unit and the firewall opening; the
sysctl drop-in stays, inert without a listener.

===========================================================================
"""

import argparse
import os
from pathlib import Path
import re
import subprocess
import sys

from install import install_file, quic_sysctls

PUBLIC_PORT = 443
BIND_HOST = "[::]"
UNPRIVILEGED_PORT_FLOOR = 1024
QUIC_BUFFER_FLOOR = 7500000
REDIRECT_TABLE = "opensro_quic"
REDIRECT_RULES = Path("/etc/opensro-release/quic-redirect.nft")
REDIRECT_UNIT_NAME = "opensro-quic-redirect.service"
REDIRECT_UNIT = Path("/etc/systemd/system") / REDIRECT_UNIT_NAME
BUFFERS = (Path("/proc/sys/net/core/rmem_max"), Path("/proc/sys/net/core/wmem_max"))


# ================
# require_bind_port
#
# A bind port the unprivileged GameWorld can hold and the redirect can
# target: 1024 or above, and never the public port itself.
# ================
def require_bind_port(port):
	if not isinstance(port, int) or not UNPRIVILEGED_PORT_FLOOR <= port < 65536:
		raise ValueError("the WebTransport bind port must be " + str(UNPRIVILEGED_PORT_FLOOR) + "-65535, not " + repr(port))


# ================
# require_bind
#
# The whole public bind: a usable port (require_bind_port) on the IPv6
# wildcard. The redirect delivers to the receiving interface's address,
# for IPv4 and IPv6 alike (inet table), so only "[::]" (dual-stack in the
# GameWorld) receives every redirected packet; loopback, one address or
# the IPv4-only 0.0.0.0 would pass every other check and drop players
# silently onto the WebSocket fallback.
# ================
def require_bind(host, port):
	if host != BIND_HOST:
		raise ValueError("the WebTransport bind host must be " + BIND_HOST + ", not " + repr(host) +
			": the UDP " + str(PUBLIC_PORT) + " redirect delivers to the interface address, IPv4 and IPv6")
	require_bind_port(port)


# ================
# redirect_rules
#
# The nftables file the unit loads. It declares the table, deletes it and
# defines it again in one transaction, so a reload replaces the rule
# instead of adding a second one. The inet family covers IPv4 and IPv6.
# REDIRECT sends a packet to the receiving interface's own address, so the
# bind is the dual-stack wildcard (require_bind).
# ================
def redirect_rules(port):
	require_bind_port(port)
	return ("# opensro: public WebTransport (UDP " + str(PUBLIC_PORT) + ") to the GameWorld bind (quic_host.py).\n"
		"table inet " + REDIRECT_TABLE + "\n"
		"delete table inet " + REDIRECT_TABLE + "\n"
		"table inet " + REDIRECT_TABLE + " {\n"
		"\tchain prerouting {\n"
		"\t\ttype nat hook prerouting priority dstnat; policy accept;\n"
		"\t\tudp dport " + str(PUBLIC_PORT) + " redirect to :" + str(port) + "\n"
		"\t}\n"
		"}\n")


REDIRECT_UNIT_BODY = ("[Unit]\n"
	"Description=opensro: redirect public UDP " + str(PUBLIC_PORT) + " to the WebTransport listener\n"
	"Wants=network-pre.target\n"
	"Before=network-pre.target\n"
	"\n"
	"[Service]\n"
	"Type=oneshot\n"
	"RemainAfterExit=yes\n"
	"ExecStart=/usr/sbin/nft -f " + REDIRECT_RULES.as_posix() + "\n"
	"ExecStop=/usr/sbin/nft delete table inet " + REDIRECT_TABLE + "\n"
	"\n"
	"[Install]\n"
	"WantedBy=multi-user.target\n")


# ================
# apply
#
# Install every piece and load it now. Idempotent: a second apply, or an
# apply with a new port, leaves exactly one redirect to that port. A port
# change leaves the old firewall opening for the operator to delete.
# ================
def apply(port, run=subprocess.run):
	require_bind_port(port)
	quic_sysctls()
	install_file(REDIRECT_RULES, redirect_rules(port).encode())
	install_file(REDIRECT_UNIT, REDIRECT_UNIT_BODY.encode())
	run(["systemctl", "daemon-reload"], check=True)
	run(["systemctl", "enable", REDIRECT_UNIT_NAME], check=True)
	run(["systemctl", "restart", REDIRECT_UNIT_NAME], check=True)
	run(["ufw", "allow", str(port) + "/udp", "comment", "opensro WebTransport, redirected from " + str(PUBLIC_PORT)], check=True)


# ================
# remove
# ================
def remove(port, run=subprocess.run):
	require_bind_port(port)
	run(["systemctl", "disable", "--now", REDIRECT_UNIT_NAME], check=False)
	for path in (REDIRECT_UNIT, REDIRECT_RULES):
		if path.exists():
			path.unlink()
	run(["systemctl", "daemon-reload"], check=True)
	run(["ufw", "delete", "allow", str(port) + "/udp"], check=False)


# ================
# problems
#
# Every gap between the host and a public listener on port: the live
# redirect, the boot unit, the firewall and the buffers. Empty means ready.
# The live ruleset is read, not the file: the file proves nothing about
# what the kernel holds.
# ================
def problems(port, run=subprocess.run, read=lambda path: path.read_text()):
	require_bind_port(port)
	found = []
	listed = run(["nft", "list", "table", "inet", REDIRECT_TABLE], capture_output=True, text=True)
	rule = "udp dport " + str(PUBLIC_PORT) + " redirect to :" + str(port)
	if listed.returncode != 0 or rule not in listed.stdout:
		found.append("no live nftables redirect '" + rule + "' (run quic_host.py apply --port " + str(port) + ")")
	enabled = run(["systemctl", "is-enabled", REDIRECT_UNIT_NAME], capture_output=True, text=True)
	if enabled.stdout.strip() != "enabled":
		found.append(REDIRECT_UNIT_NAME + " is not enabled, so the redirect is lost at reboot")
	status = run(["ufw", "status"], capture_output=True, text=True)
	if not re.search(r"^" + str(port) + r"/udp\s+ALLOW\b", status.stdout, re.MULTILINE):
		found.append("the firewall does not allow " + str(port) + "/udp, so redirected packets are dropped")
	for path in BUFFERS:
		value = int(read(path).strip() or 0)
		if value < QUIC_BUFFER_FLOOR:
			found.append(path.name + " is " + str(value) + ", below " + str(QUIC_BUFFER_FLOOR))
	return found


# ================
# main
# ================
def main(argv=None):
	parser = argparse.ArgumentParser(description="Public WebTransport host state (UDP 443 redirect).")
	parser.add_argument("action", choices=("apply", "check", "remove"))
	parser.add_argument("--port", type=int, required=True, help="the GameWorld's WebTransport bind port")
	arguments = parser.parse_args(argv)
	if os.geteuid() != 0:
		raise RuntimeError(arguments.action + " requires root: nft and ufw read the live state")
	if arguments.action == "apply":
		apply(arguments.port)
	elif arguments.action == "remove":
		remove(arguments.port)
	gaps = problems(arguments.port) if arguments.action != "remove" else []
	for gap in gaps:
		print("not ready: " + gap)
	if not gaps and arguments.action != "remove":
		print("ready: public UDP " + str(PUBLIC_PORT) + " reaches port " + str(arguments.port))
	return 1 if gaps else 0


if __name__ == "__main__":
	sys.exit(main())
