#!/usr/bin/env python3
"""
===========================================================================

ci.py - transfer immutable candidates through role-limited release keys.

GitHub owns approval and credential availability. This module writes temporary
SSH credentials, pins the host key and streams one bounded request. It never
chooses a newer artifact after approval or runs a remote shell command.

A coordinated release (coordinated.py) takes three publication requests:
coordinate puts the pair live, the workflow runs the browser smoke against
it and records the evidence with the staging key, and confirm or revert ends
the operation. coordinate --maintenance publishes a maintenance pair (both
candidates built with --maintenance); the flag only asserts what the plans
already declare, so a workflow cannot publish one by mistake.

===========================================================================
"""

import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile
import urllib.request

from plan import require_current
from release_state import identity

FETCH_TIMEOUT_SECONDS = 60


# ================
# transfer
#
# The account's root-owned forced command determines the capability. Secrets
# are private temporary files and are removed even when the transfer fails.
# ================
def transfer(source, role):
	with tempfile.TemporaryDirectory(prefix="release-ssh-") as directory:
		root = Path(directory)
		key = root / "key"
		known = root / "known_hosts"
		key.write_text(os.environ["RELEASE_KEY"].strip() + "\n", encoding="utf-8")
		key.chmod(0o600)
		known.write_text(os.environ["RELEASE_KNOWN_HOSTS"].strip() + "\n", encoding="utf-8")
		host = os.environ["RELEASE_HOST"]
		if not host or any(character not in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-" for character in host):
			raise ValueError("invalid configured release host")
		account = "sro-stage" if role == "stage" else "sro-release"
		with Path(source).open("rb") as stream:
			result = subprocess.run([
				"ssh", "-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes",
				"-o", "ConnectTimeout=15", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=4",
				"-o", "UserKnownHostsFile=" + str(known), "-i", str(key), account + "@" + host,
			], stdin=stream, stdout=subprocess.PIPE, text=True, check=True, timeout=1500)
		lines = result.stdout.strip().splitlines()
		if not lines:
			raise RuntimeError("release receiver returned no result")
		return json.loads(lines[-1])


# ================
# send
#
# One JSON request through the given role.
# ================
def send(request, role):
	with tempfile.TemporaryDirectory(prefix="release-request-") as directory:
		path = Path(directory) / "request.json"
		path.write_text(json.dumps(request), encoding="utf-8")
		return transfer(path, role)


# ================
# staged_rows
#
# The host's public candidate list.
# ================
def staged_rows():
	origin = os.environ["RELEASE_ORIGIN"].rstrip("/")
	with urllib.request.urlopen(origin + "/releases/candidates.json", timeout=FETCH_TIMEOUT_SECONDS) as response:
		return json.loads(response.read())["candidates"]


# ================
# staged_client_commit
#
# A coordinated client is staged by the operator, not by a preparation run,
# so its source commit comes from the host's public candidate list. The
# freshness rule is the same as for every other candidate. With maintenance,
# both staged candidates must have been built for a maintenance release.
# ================
def staged_client_commit(candidate, maintenance=False, server=None, rows=None):
	rows = staged_rows() if rows is None else rows
	row = next((row for row in rows if row["candidate"] == candidate), None)
	if row is None or row["component"] != "client" or not row.get("coordinated"):
		raise ValueError("the client is not a staged coordinated candidate")
	if maintenance:
		peer = next((peer for peer in rows if peer["candidate"] == server), None)
		if not row.get("maintenance") or peer is None or peer["component"] != "server" or not peer.get("maintenance"):
			raise ValueError("both candidates must be staged maintenance candidates")
	return identity(row["commit"])


# ================
# stage
#
# Staging records the candidate identifier as a job output; a server also
# records its Linux evidence (or, for a rollback, its retained provenance).
# ================
def stage(component, source, reason=None):
	if reason is None:
		result = transfer(source, "stage")
	else:
		result = send({"operation": "prepare-rollback", "component": component,
			"release": identity(source), "reason": reason}, "stage")
	Path("candidate.json").write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
	with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as output:
		output.write("candidate=" + identity(result["candidate"]) + "\n")
	if component != "server":
		return result
	proof = {"verification": "retained-production"} if reason is not None else {"linuxTests": True}
	return send({"operation": "server-tests", "report": {**result, **proof, "verdict": "PASS"}}, "stage")


# ================
# main
#
# Publication checks main again after approval, then sends only the exact
# candidate identifiers.
# ================
def main():
	parser = argparse.ArgumentParser()
	commands = parser.add_subparsers(dest="role", required=True)
	command = commands.add_parser("stage")
	command.add_argument("component", choices=("client", "server"))
	command.add_argument("source")
	command = commands.add_parser("rollback")
	command.add_argument("component", choices=("client", "server"))
	command.add_argument("source")
	command.add_argument("--reason", required=True)
	command = commands.add_parser("evidence")
	command.add_argument("component", choices=("client",))
	command.add_argument("source")
	command = commands.add_parser("publish")
	command.add_argument("component", choices=("client", "server"))
	command.add_argument("source")
	command.add_argument("--commit", required=True, help="Public source commit, independent of the workflow repository")
	command = commands.add_parser("coordinate")
	command.add_argument("server")
	command.add_argument("client")
	command.add_argument("--commit", required=True, help="Public source commit of the server candidate")
	command.add_argument("--maintenance", action="store_true", help="a maintenance pair: closed window and store upgrade")
	command = commands.add_parser("confirm")
	command = commands.add_parser("revert")
	command.add_argument("--reason", required=True)
	arguments = parser.parse_args()
	if arguments.role == "stage":
		result = stage(arguments.component, arguments.source)
	elif arguments.role == "rollback":
		result = stage(arguments.component, arguments.source, arguments.reason)
	elif arguments.role == "evidence":
		result = send({"operation": "client-smoke", "report": json.loads(Path(arguments.source).read_text())}, "stage")
	elif arguments.role == "publish":
		require_current(arguments.component, arguments.commit)
		result = send({"operation": "publish-" + arguments.component, "candidate": identity(arguments.source)}, "publish")
	elif arguments.role == "coordinate":
		require_current("server", arguments.commit)
		require_current("client", staged_client_commit(identity(arguments.client), arguments.maintenance,
			identity(arguments.server)))
		result = send({"operation": "publish-coordinated", "server": identity(arguments.server),
			"client": identity(arguments.client)}, "publish")
		# The browser smoke loads the live client by its candidate identity.
		Path("candidate.json").write_text(json.dumps(result["client"], indent=2) + "\n", encoding="utf-8")
	elif arguments.role == "confirm":
		result = send({"operation": "confirm-coordinated"}, "publish")
	else:
		result = send({"operation": "revert-coordinated", "reason": arguments.reason}, "publish")
	print(json.dumps(result))


if __name__ == "__main__":
	main()
