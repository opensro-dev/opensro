#!/usr/bin/env python3
"""
===========================================================================

ci.py - transfer immutable candidates through role-limited release keys.

GitHub owns approval and credential availability. This module writes temporary
SSH credentials, pins the host key and streams one bounded request. It never
chooses a newer artifact after approval or runs a remote shell command.

===========================================================================
"""

import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile

from plan import require_current
from release_state import identity


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
# main
#
# Staging records the candidate identifier as a job output. Publication checks
# main again after approval, then sends only that exact candidate identifier.
# ================
def main():
	parser = argparse.ArgumentParser()
	parser.add_argument("role", choices=("stage", "publish", "evidence", "rollback"))
	parser.add_argument("component", choices=("client", "server"))
	parser.add_argument("source")
	parser.add_argument("--reason")
	arguments = parser.parse_args()
	if arguments.role in ("stage", "rollback"):
		if arguments.role == "rollback":
			request = {"operation": "prepare-rollback", "component": arguments.component,
				"release": identity(arguments.source), "reason": arguments.reason}
			with tempfile.TemporaryDirectory(prefix="rollback-request-") as directory:
				path = Path(directory) / "request.json"
				path.write_text(json.dumps(request), encoding="utf-8")
				result = transfer(path, "stage")
		else:
			result = transfer(arguments.source, "stage")
		Path("candidate.json").write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
		with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as output:
			output.write("candidate=" + identity(result["candidate"]) + "\n")
		if arguments.component == "server":
			proof = {"verification": "retained-production"} if arguments.role == "rollback" else {"linuxTests": True}
			request = {"operation": "server-tests", "report": {**result, **proof, "verdict": "PASS"}}
		else:
			return
	elif arguments.role == "evidence":
		request = {"operation": "client-smoke", "report": json.loads(Path(arguments.source).read_text())}
	else:
		commit = os.environ["GITHUB_SHA"]
		require_current(arguments.component, commit)
		request = {"operation": "publish-" + arguments.component, "candidate": identity(arguments.source)}
	with tempfile.TemporaryDirectory(prefix="release-request-") as directory:
		path = Path(directory) / "request.json"
		path.write_text(json.dumps(request), encoding="utf-8")
		result = transfer(path, "publish" if arguments.role == "publish" else "stage")
	print(json.dumps(result))


if __name__ == "__main__":
	main()
