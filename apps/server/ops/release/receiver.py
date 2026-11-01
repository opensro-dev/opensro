#!/usr/bin/env python3
"""
===========================================================================

receiver.py - separate staging and publication capabilities for release SSH keys.

Forced commands choose the role; uploaded content cannot grant itself another
role. Staging can write immutable candidates and test evidence. Only the
production key, released after GitHub approval, can change a live component.

A publication request never runs inside the SSH session: a dropped session
(the publisher's PC asleep, a proxy restart) would hang up a deploy halfway.
The receiver files the request, starts it as its own transient systemd unit,
then follows the unit and prints its result; the work finishes and records
its result whether or not anyone is still following. The roles run (that
unit) and deadman (coordinated.expire, from a host timer) are reached only
from root on the host: the SSH forced commands name stage or publish.

===========================================================================
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time

from bundle import unpack as unpack_server
from client_deploy import promote, record_smoke, stage
import client_data
import server_data
import coordinated
from deploy import masked, publish_server, receive, rehearse
from release_state import admit, identity, read_state, write_state
from rollback import prepare as prepare_rollback

try:
	import syslog
except ImportError:
	# The receiver runs on Linux; its tests also run on Windows.
	syslog = None

CONFIG = Path("/etc/opensro-release/config.json")
MAX_REQUEST_BYTES = 1 << 20
# A failure's reason kept in the host journal is bounded and masks anything
# that could be a token (any run of 32 or more hex or base64url characters).
MAX_FAILURE_LOG_CHARS = 800
LOCK = Path("/run/lock/opensro-release.lock")
REQUESTS = Path("/var/lib/opensro-release/requests")
UNIT_PREFIX = "opensro-release-"
JOB_PATTERN = re.compile(r"[0-9a-f]{32}")
FOLLOW_POLL_SECONDS = 2
# Filed requests and their results stay this long for an operator to read.
JOB_RETENTION_SECONDS = 7 * 24 * 3600


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
		# A coordinated client takes its browser evidence after the switch.
		if (record / "smoke.json").exists() or (plan.get("coordinated") and plan["component"] == "client"):
			phase = "ready-for-approval"
		if current["release"] == plan["release"]:
			phase = "live"
		elif current["generation"] != plan["baseGeneration"]:
			phase = "superseded"
		row = {
			"candidate": record.name,
			"component": plan["component"],
			"release": plan["release"],
			"commit": plan["commit"],
			"baseRelease": plan["baseRelease"],
			"phase": phase,
			"mode": plan["mode"],
			"createdAt": record.stat().st_mtime,
			"coordinated": bool(plan.get("coordinated")),
			"maintenance": bool(plan.get("maintenance")),
			"restartRequired": plan["component"] == "server" or bool(plan.get("coordinated")),
		}
		# The browser smoke checks the served entry against this digest; the
		# host checks the evidence against the staged file itself.
		entry = Path(config["client_candidates"]) / record.name / "index.html"
		if plan["component"] == "client" and entry.is_file():
			row["entrySha256"] = hashlib.sha256(entry.read_bytes()).hexdigest()
		rows.append(row)
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
	state = read_state(config["production_state"])
	admit(state, manifest["plan"])
	stops = rehearse(config, scratch / "server", manifest)
	if stops and not can_stop_for_credentials(manifest["plan"]):
		raise ValueError("release replaces credential variable(s) " + ", ".join(path for _, path in stops) +
			" while their jobs run; only a maintenance release may stop the fleet for that,"
			" so this release would fail inside its window")
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
# can_stop_for_credentials
#
# Whether this release's deployment may stop the fleet to replace a
# credential variable (deploy.py deploy): every maintenance pair may,
# whether or not its store upgrades (coordinated.py passes maintenance to
# the publish and the revert). A plain release validates with the fleet
# running, so a credential variable it replaces refuses it.
# ================
def can_stop_for_credentials(plan):
	return bool(plan.get("maintenance"))


# ================
# stage_upload
#
# A stage-role archive is one of five kinds, named by its declaration member:
# a batch of data payloads for the store, server game data, a data
# candidate, an application candidate or a server candidate.
# ================
def stage_upload(config, archive, scratch):
	with tarfile.open(archive, "r:") as package:
		names = package.getnames()
		declaration = None
		if "candidate.json" in names:
			declaration = json.load(package.extractfile("candidate.json")).get("format")
	if names and names[0] == "payload.json":
		return client_data.store_payload(config, archive)
	if names and names[0] == server_data.DECLARATION:
		return server_data.stage(config, archive)
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
	if role == "stage" and operation == "payload-inventory":
		return client_data.payload_inventory(config, value)
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
	if role == "publish" and operation == "publish-coordinated":
		return coordinated.publish(config, value, scratch)
	if role == "publish" and operation == "confirm-coordinated":
		return coordinated.confirm(config, value)
	if role == "publish" and operation == "revert-coordinated":
		return coordinated.revert_request(config, value)
	if role == "publish" and operation == "resume-coordinated":
		return coordinated.resume(config, value)
	raise ValueError("operation is not allowed for this release key")


# ================
# main
#
# One host lock covers both components. No shell command from SSH is executed,
# and stage uploads have no path to the production publication functions.
# ================
# ================
# record_failure
#
# The receiver's own stderr reaches only the operator's SSH session, which a
# publisher may discard: a failed publication on 2026-10-11 left no reason on
# the host at all. The reason now also goes to the host journal (identifier
# opensro-release), bounded and with secret-like runs masked.
# ================
def record_failure(role, error):
	if syslog is None:
		return
	reason = masked(type(error).__name__ + ": " + str(error))[:MAX_FAILURE_LOG_CHARS]
	syslog.openlog("opensro-release")
	syslog.syslog(syslog.LOG_ERR, role + " request failed: " + reason)


def main():
	parser = argparse.ArgumentParser()
	parser.add_argument("role", choices=("stage", "publish", "run", "deadman"))
	parser.add_argument("job", nargs="?")
	arguments = parser.parse_args()
	if os.geteuid() != 0:
		raise RuntimeError("release receiver requires its root-owned forced command")
	os.umask(0o077)
	config = json.loads(CONFIG.read_text())
	try:
		if arguments.role == "run":
			run_job(config, arguments.job)
		elif arguments.role == "deadman":
			deadman(config)
		elif arguments.role == "publish":
			serve_detached(config)
		else:
			serve(config, arguments.role)
	except UnitFailure:
		# The unit already journaled this reason; the follower only relays it.
		raise
	except Exception as error:
		record_failure(arguments.role, error)
		raise


# ================
# UnitFailure
#
# A detached job's own failure, relayed to the SSH session by its follower.
# ================
class UnitFailure(RuntimeError):
	pass


# ================
# locked
#
# The one host lock every release step holds. Without blocking it returns
# None while another step holds it (the dead-man never waits).
# ================
def locked(blocking=True):
	import fcntl
	stream = LOCK.open("w")
	try:
		fcntl.flock(stream, fcntl.LOCK_EX if blocking else fcntl.LOCK_EX | fcntl.LOCK_NB)
	except BlockingIOError:
		stream.close()
		return None
	return stream


# ================
# requests_directory
#
# Where publication requests are filed. The receiver owns this default: a
# host whose controls were installed without install.main (every release
# since F3) has no "requests" key and no directory, and must still publish.
# ================
def requests_directory(config):
	directory = Path(config.get("requests", REQUESTS))
	directory.mkdir(mode=0o700, parents=True, exist_ok=True)
	directory.chmod(0o700)
	return directory


# ================
# job_paths
#
# A filed publication request and its result, by the receiver's own job id.
# ================
def job_paths(config, job):
	if not isinstance(job, str) or not JOB_PATTERN.fullmatch(job):
		raise ValueError("invalid release job")
	directory = requests_directory(config)
	return directory / (job + ".json"), directory / (job + ".result.json")


# ================
# launch_unit
#
# Start the job as a transient unit owned by systemd, not by the SSH
# session. Its output goes to the host journal as opensro-release.
# ================
def launch_unit(job):
	subprocess.run(["systemd-run", "--unit", UNIT_PREFIX + job, "--collect", "--quiet",
		"--property=StandardOutput=journal", "--property=StandardError=journal",
		"--property=SyslogIdentifier=opensro-release",
		sys.executable, str(Path(__file__).resolve()), "run", job], check=True)


# ================
# unit_running
# ================
def unit_running(job):
	state = subprocess.run(["systemctl", "is-active", UNIT_PREFIX + job], capture_output=True, text=True)
	return state.stdout.strip() in ("active", "activating", "reloading")


# ================
# serve_detached
#
# The publication key's request: bounded, filed, run as its own unit and
# followed. Only this follower belongs to the SSH session; losing it loses
# the printed result, never the work.
# ================
def serve_detached(config, launch=launch_unit, running=unit_running, poll=FOLLOW_POLL_SECONDS):
	with tempfile.TemporaryDirectory(prefix="opensro-request-", dir=config.get("scratch", "/var/tmp")) as directory:
		upload = Path(directory) / "upload"
		receive(sys.stdin.buffer, upload)
		data = upload.read_bytes()
	if data[:1] != b"{":
		raise ValueError("production key accepts only an approved candidate identity")
	if len(data) > MAX_REQUEST_BYTES:
		raise ValueError("release request exceeds limit")
	job = secrets.token_hex(16)
	request_path, result_path = job_paths(config, job)
	write_state(request_path, json.loads(data))
	request_path.chmod(0o600)
	launch(job)
	print("Release work runs as unit " + UNIT_PREFIX + job + "; its log: journalctl -u " + UNIT_PREFIX + job,
		file=sys.stderr, flush=True)
	while not result_path.exists():
		# The result is written before the unit exits; look once more after.
		if not running(job) and not result_path.exists():
			raise RuntimeError("release unit " + UNIT_PREFIX + job + " ended without a result")
		time.sleep(poll)
	result = json.loads(result_path.read_text())
	if not result["ok"]:
		raise UnitFailure(result["error"])
	print(json.dumps(result["result"], sort_keys=True))
	return result["result"]


# ================
# run_job
#
# The unit's body: one filed publication request under the host lock. The
# result, or the failure's masked reason, is recorded for the follower. The
# filed copy then drops the operation's nonce: the week it is kept for an
# operator needs what was asked, not the proof of ownership.
# ================
def run_job(config, job, lock=locked):
	request_path, result_path = job_paths(config, job)
	value = json.loads(request_path.read_text())
	try:
		with lock(), tempfile.TemporaryDirectory(prefix="opensro-candidate-", dir=config.get("scratch", "/var/tmp")) as directory:
			try:
				result = request(config, "publish", value, Path(directory))
				candidate_status(config)
			except Exception as error:
				reason = masked(type(error).__name__ + ": " + str(error))[:MAX_FAILURE_LOG_CHARS]
				write_state(result_path, {"ok": False, "error": reason})
				raise
			write_state(result_path, {"ok": True, "result": result})
	finally:
		if "nonce" in value:
			write_state(request_path, {key: item for key, item in value.items() if key != "nonce"})
			request_path.chmod(0o600)
	return result


# ================
# deadman
#
# The host timer's tick: unless a release step holds the lock, expire a
# coordinated release its publisher left, then prune old filed jobs.
# ================
def deadman(config, lock=locked, now=time.time):
	held = lock(blocking=False)
	if held is None:
		return None
	with held:
		outcome = coordinated.expire(config, now())
		if outcome:
			candidate_status(config)
		for path in requests_directory(config).glob("*.json"):
			if now() - path.stat().st_mtime > JOB_RETENTION_SECONDS:
				path.unlink()
	return outcome


# ================
# serve
#
# One stage request or upload under the release lock.
# ================
def serve(config, role):
	# A simultaneous upload is not a deployment failure. The kernel queues
	# this owner; admission runs after the previous operation releases it.
	with locked():
		with tempfile.TemporaryDirectory(prefix="opensro-candidate-", dir="/var/tmp") as directory:
			scratch = Path(directory)
			archive = scratch / "upload"
			receive(sys.stdin.buffer, archive)
			with archive.open("rb") as stream:
				is_request = stream.read(1) == b"{"
			if is_request:
				if archive.stat().st_size > MAX_REQUEST_BYTES:
					raise ValueError("release request exceeds limit")
				result = request(config, role, json.loads(archive.read_bytes()), scratch)
			else:
				if role != "stage":
					raise ValueError("production key accepts only an approved candidate identity")
				result = stage_upload(config, archive, scratch)
			candidate_status(config)
			print(json.dumps(result, sort_keys=True))


if __name__ == "__main__":
	main()
