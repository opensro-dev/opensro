#!/usr/bin/env python3
"""
===========================================================================

receiver.py - separate staging and publication capabilities for release SSH keys.

Forced commands choose the role; uploaded content cannot grant itself another
role. Staging can write immutable candidates and test evidence. Only the
production key, released after GitHub approval, can change a live component.

===========================================================================
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import sys
import tarfile
import tempfile
import time

from bundle import unpack as unpack_server
from client_deploy import promote, record_smoke, stage
import client_data
from deploy import deploy_approved, receive
from release_state import admit, identity, read_state, write_state
from rollback import prepare as prepare_rollback

CONFIG = Path("/etc/opensro-release/config.json")
MAX_REQUEST_BYTES = 1 << 20


# ================
# candidate_status
#
# The public overview contains release identities and test outcomes only.
# Credentials, filesystem configuration and private diagnostics never enter it.
# ================
def candidate_status(config):
	state = read_state(config["production_state"])
	rows = []
	plans = []
	for record in sorted(Path(config["candidate_records"]).iterdir()):
		if not record.is_dir() or not (record / "candidate.json").exists():
			continue
		candidate = json.loads((record / "candidate.json").read_text())
		plan = candidate["plan"]
		plans.append(plan)
		current = state[plan["component"]]
		phase = "awaiting-tests"
		if (record / "smoke.json").exists():
			phase = "ready-for-approval"
		if current["release"] == plan["release"]:
			phase = "live"
		elif current["generation"] != plan["baseGeneration"]:
			phase = "superseded"
		rows.append({
			"candidate": record.name,
			"component": plan["component"],
			"release": plan["release"],
			"commit": plan["commit"],
			"baseRelease": plan["baseRelease"],
			"phase": phase,
			"mode": plan["mode"],
			"createdAt": record.stat().st_mtime,
			"restartRequired": plan["component"] == "server",
		})
	for row in rows:
		if row["phase"] == "live" or row["mode"] == "rollback":
			continue
		if any(plan["component"] == row["component"] and plan["commit"] != row["commit"]
			and row["commit"] in plan.get("ancestors", []) for plan in plans):
			row["phase"] = "superseded"
	rows.sort(key=lambda row: row["createdAt"], reverse=True)
	write_state(Path(config["production_state"]).with_name("candidates.json"), {
		"checkedAt": time.time(), "candidates": rows,
	})


# ================
# stage_server
#
# Store the verified server archive before approval. Later promotion uses these
# exact bytes; it never rebuilds binaries or silently picks a newer commit.
# ================
def stage_server(config, archive, scratch):
	manifest = unpack_server(archive, scratch / "server")
	admit(read_state(config["production_state"]), manifest["plan"])
	with archive.open("rb") as stream:
		candidate_id = hashlib.file_digest(stream, "sha256").hexdigest()
	record = Path(config["candidate_records"]) / candidate_id
	metadata = {"format": "opensro-server-candidate-v1", "plan": manifest["plan"]}
	result = {"candidate": candidate_id, "release": manifest["commit"], "commit": manifest["commit"],
		"mode": manifest["plan"]["mode"]}
	if record.exists():
		# A retried CI job must not replace retained bytes or silently accept drift.
		with (record / "server.tar").open("rb") as stream:
			if hashlib.file_digest(stream, "sha256").hexdigest() != candidate_id:
				raise ValueError("retained server archive changed before staging retry")
		if json.loads((record / "candidate.json").read_text()) != metadata:
			raise ValueError("retained server plan changed before staging retry")
		return result
	record.mkdir(mode=0o700)
	shutil.copyfile(archive, record / "server.tar")
	(record / "server.tar").chmod(0o600)
	write_state(record / "candidate.json", metadata)
	return result


# ================
# publish_server
#
# Reverify the retained archive and current production generation under the
# receiver lock. Nomad deployment and its health checks remain server-owned.
# ================
def publish_server(config, candidate_id, scratch):
	candidate_id = identity(candidate_id)
	record = Path(config["candidate_records"]) / candidate_id
	archive = record / "server.tar"
	with archive.open("rb") as stream:
		if hashlib.file_digest(stream, "sha256").hexdigest() != candidate_id:
			raise ValueError("server archive changed after approval preparation")
	manifest = unpack_server(archive, scratch / "server")
	evidence = json.loads((record / "smoke.json").read_text())
	if evidence.get("candidate") != candidate_id or evidence.get("release") != manifest["commit"]:
		raise ValueError("server evidence identifies a different artifact")
	retained = manifest["plan"]["mode"] == "rollback" and evidence.get("verification") == "retained-production"
	if evidence.get("verdict") != "PASS" or not (evidence.get("linuxTests") is True or retained):
		raise ValueError("server candidate has not passed Linux verification")
	deploy_approved(config, scratch / "server", manifest)
	return read_state(config["production_state"])


# ================
# stage_upload
#
# A stage-role archive is one of four kinds, named by its declaration member:
# a batch of data payloads for the store, a data candidate, an application
# candidate or a server candidate.
# ================
def stage_upload(config, archive, scratch):
	with tarfile.open(archive, "r:") as package:
		names = package.getnames()
		declaration = None
		if "candidate.json" in names:
			declaration = json.load(package.extractfile("candidate.json")).get("format")
	if names and names[0] == "payload.json":
		return client_data.store_payload(config, archive)
	if declaration == client_data.FORMAT:
		return client_data.stage(config, archive)
	if "candidate.json" in names:
		return stage(config, archive)
	return stage_server(config, archive, scratch)


# ================
# request
#
# A staging key cannot publish by changing a JSON operation name. The forced
# role is checked before dispatch, and every operation has a bounded payload.
# ================
def request(config, role, value, scratch):
	operation = value.get("operation")
	if role == "stage" and operation == "prepare-rollback":
		archive = prepare_rollback(config, value, scratch)
		return stage(config, archive) if value["component"] == "client" else stage_server(config, archive, scratch)
	if role == "stage" and operation == "client-smoke":
		record_smoke(config, value["report"])
		return {"recorded": True}
	if role == "stage" and operation == "server-tests":
		report = value["report"]
		record = Path(config["candidate_records"]) / identity(report["candidate"])
		candidate = json.loads((record / "candidate.json").read_text())
		if candidate["plan"]["component"] != "server" or candidate["plan"]["release"] != report.get("release"):
			raise ValueError("server verification identifies a different candidate")
		retained = candidate["plan"]["mode"] == "rollback" and report.get("verification") == "retained-production"
		if report.get("verdict") != "PASS" or not (report.get("linuxTests") is True or retained):
			raise ValueError("server Linux verification did not pass")
		write_state(record / "smoke.json", report)
		return {"recorded": True}
	if role == "publish" and operation == "publish-client":
		return promote(config, value["candidate"])
	if role == "publish" and operation == "publish-server":
		return publish_server(config, value["candidate"], scratch)
	raise ValueError("operation is not allowed for this release key")


# ================
# main
#
# One host lock covers both components. No shell command from SSH is executed,
# and stage uploads have no path to the production publication functions.
# ================
def main():
	import fcntl
	parser = argparse.ArgumentParser()
	parser.add_argument("role", choices=("stage", "publish"))
	arguments = parser.parse_args()
	if os.geteuid() != 0:
		raise RuntimeError("release receiver requires its root-owned forced command")
	os.umask(0o077)
	config = json.loads(CONFIG.read_text())
	with Path("/run/lock/opensro-release.lock").open("w") as lock:
		# A simultaneous upload is not a deployment failure. The kernel queues
		# this owner; admission runs after the previous operation releases it.
		fcntl.flock(lock, fcntl.LOCK_EX)
		with tempfile.TemporaryDirectory(prefix="opensro-candidate-", dir="/var/tmp") as directory:
			scratch = Path(directory)
			archive = scratch / "upload"
			receive(sys.stdin.buffer, archive)
			with archive.open("rb") as stream:
				is_request = stream.read(1) == b"{"
			if is_request:
				if archive.stat().st_size > MAX_REQUEST_BYTES:
					raise ValueError("release request exceeds limit")
				result = request(config, arguments.role, json.loads(archive.read_bytes()), scratch)
			else:
				if arguments.role != "stage":
					raise ValueError("production key accepts only an approved candidate identity")
				result = stage_upload(config, archive, scratch)
			candidate_status(config)
			print(json.dumps(result, sort_keys=True))


if __name__ == "__main__":
	main()
