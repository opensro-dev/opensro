#!/usr/bin/env python3
"""
===========================================================================

ci.py - transfer immutable candidates through role-limited release keys.

The caller owns approval and credential availability: a workflow passes the
key and pinned host keys as text (RELEASE_KEY, RELEASE_KNOWN_HOSTS), the
operator's machine passes files it already holds (RELEASE_KEY_FILE,
RELEASE_KNOWN_HOSTS_FILE) and, where the host is reached through a local
proxy, the ssh ProxyCommand (RELEASE_SSH_PROXY_COMMAND). Either way the host
key is pinned and one bounded request is streamed. It never chooses a newer
artifact after approval or runs a remote shell command.

A coordinated release (coordinated.py) takes three publication requests:
coordinate puts the pair live, the workflow runs the browser smoke against
it and records the evidence with the staging key, and confirm or revert ends
the operation. resume answers a revert that failed with the candidate
server still live: it puts the candidate client back and returns the
operation to verifying, so fresh evidence and confirm can follow.
coordinate --maintenance publishes a maintenance pair (both
candidates built with --maintenance); the flag only asserts what the plans
already declare, so a workflow cannot publish one by mistake.

coordinate names the operation with a fresh nonce and keeps it in
operation.json; confirm, revert and resume send it back (--nonce, or that file), so
only the publisher that opened an operation ends it. A publisher that lost
the nonce takes the operation over with --takeover REASON instead.

===========================================================================
"""

import argparse
import json
import os
from pathlib import Path
import secrets
import subprocess
import tempfile
import urllib.request

from plan import require_current
from release_state import identity

FETCH_TIMEOUT_SECONDS = 60
OPERATION_FILE = Path("operation.json")


# ================
# owner_claim
#
# How confirm or revert proves it is the operation's publisher: its nonce
# (given, or kept by coordinate), or an explicit takeover with a reason.
# ================
def owner_claim(nonce=None, takeover=None, kept=OPERATION_FILE):
	if takeover is not None:
		return {"takeover": takeover}
	if nonce is None and kept.is_file():
		nonce = json.loads(kept.read_text(encoding="utf-8"))["nonce"]
	if nonce is None:
		raise ValueError("no nonce for this operation: pass --nonce, run from coordinate's directory,"
			" or take it over with --takeover REASON")
	return {"nonce": nonce}


# ================
# credential_file
#
# One credential as a file ssh can read: the operator's own file when
# FILE_VARIABLE names one, else the workflow secret TEXT_VARIABLE written to
# a private file in directory (removed with it). Naming both is refused: one
# run must not silently prefer one source over the other.
# ================
def credential_file(directory, text_variable, file_variable, mode):
	text, path = os.environ.get(text_variable, ""), os.environ.get(file_variable, "")
	if text.strip() and path:
		raise ValueError(text_variable + " and " + file_variable + " are both set")
	if path:
		path = Path(path).expanduser()
		if not path.is_file():
			raise ValueError(file_variable + " names no file")
		return path
	if not text.strip():
		raise ValueError(text_variable + " or " + file_variable + " is required")
	target = Path(directory) / text_variable.lower()
	target.write_text(text.strip() + "\n", encoding="utf-8")
	target.chmod(mode)
	return target


# ================
# ssh_options
#
# The options every release connection uses, from a workflow or from the
# operator's machine. The operator's ssh config, agent and forwardings stay
# out of a role-limited connection (-F none, no agent, only this key); only
# the given known_hosts pins the host (no global file, no key updates); a
# proxy is one ProxyCommand option (RELEASE_SSH_PROXY_COMMAND), never a
# shell string around ssh itself.
# ================
def ssh_options(key, known):
	# UserKnownHostsFile takes a space-separated list: a path with spaces (the
	# operator's checkout) must be quoted. ssh also %-expands both paths, so a
	# quote or a percent sign in either is refused.
	for path in (key, known):
		if '"' in str(path) or "%" in str(path):
			raise ValueError("a release key or known_hosts path cannot contain a double quote or a percent sign")
	options = ["-F", "none", "-T",
		"-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes", "-o", "IdentityAgent=none",
		"-o", "ForwardAgent=no", "-o", "ClearAllForwardings=yes",
		"-o", "StrictHostKeyChecking=yes", "-o", 'UserKnownHostsFile="' + str(known) + '"',
		"-o", "GlobalKnownHostsFile=none", "-o", "UpdateHostKeys=no",
		"-o", "ConnectTimeout=15", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=4"]
	proxy = os.environ.get("RELEASE_SSH_PROXY_COMMAND", "").strip()
	if proxy:
		options += ["-o", "ProxyCommand=" + proxy]
	return options + ["-i", str(key)]


# ================
# ssh_command
#
# The argv for one role's forced command on the configured release host.
# ================
def ssh_command(role, key, known):
	host = os.environ["RELEASE_HOST"]
	if not host or any(character not in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-" for character in host):
		raise ValueError("invalid configured release host")
	account = "sro-stage" if role == "stage" else "sro-release"
	return ["ssh", *ssh_options(key, known), account + "@" + host]


# ================
# transfer
#
# The account's root-owned forced command determines the capability. Secrets
# are private temporary files and are removed even when the transfer fails.
# ================
def transfer(source, role):
	with tempfile.TemporaryDirectory(prefix="release-ssh-") as directory:
		key = credential_file(directory, "RELEASE_KEY", "RELEASE_KEY_FILE", 0o600)
		known = credential_file(directory, "RELEASE_KNOWN_HOSTS", "RELEASE_KNOWN_HOSTS_FILE", 0o644)
		with Path(source).open("rb") as stream:
			result = subprocess.run(ssh_command(role, key, known), stdin=stream, stdout=subprocess.PIPE, text=True,
				check=True, timeout=1500)
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
	# A workflow step publishes the identity as a job output; run by hand,
	# candidate.json and the printed result carry it.
	if os.environ.get("GITHUB_OUTPUT"):
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
	for name in ("confirm", "revert", "resume"):
		command = commands.add_parser(name)
		claim = command.add_mutually_exclusive_group()
		claim.add_argument("--nonce", help="the nonce coordinate printed (default: operation.json)")
		claim.add_argument("--takeover", metavar="REASON", help="end an operation whose nonce is lost; alerted")
		if name == "revert":
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
		nonce = secrets.token_hex(16)
		# Kept before sending: a publisher that loses the answer still owns
		# the operation the host may have opened.
		OPERATION_FILE.write_text(json.dumps({"nonce": nonce}) + "\n", encoding="utf-8")
		OPERATION_FILE.chmod(0o600)
		result = send({"operation": "publish-coordinated", "server": identity(arguments.server),
			"client": identity(arguments.client), "nonce": nonce}, "publish")
		# The browser smoke loads the live client by its candidate identity.
		Path("candidate.json").write_text(json.dumps(result["client"], indent=2) + "\n", encoding="utf-8")
	elif arguments.role == "confirm":
		result = send({"operation": "confirm-coordinated", **owner_claim(arguments.nonce, arguments.takeover)}, "publish")
	elif arguments.role == "resume":
		result = send({"operation": "resume-coordinated", **owner_claim(arguments.nonce, arguments.takeover)}, "publish")
	else:
		result = send({"operation": "revert-coordinated", "reason": arguments.reason,
			**owner_claim(arguments.nonce, arguments.takeover)}, "publish")
	print(json.dumps(result))


if __name__ == "__main__":
	main()
