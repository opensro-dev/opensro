"""
===========================================================================

deploy.py - replace the running server with verified release inputs.

The receiver (receiver.py) owns the SSH capability and the host lock and
calls in here: publish_server for a server alone, the coordinated owner
(coordinated.py) for a server released together with its client. Nomad owns
rollout health and job reversion. No live database is copied back.

===========================================================================
"""

import http.client
import ipaddress
import json
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time
from urllib.parse import urlsplit

from bundle import FILES, MAX_ARCHIVE_BYTES, unpack
from release_state import admit, begin, complete, identity, read_state, write_state
from retention import preserve

CONFIG = Path("/etc/opensro-release/config.json")
CHUNK_BYTES = 1 << 20
HTTP_TIMEOUT = 20
NOTICE_SECONDS = 120


# ================
# receive
#
# Bound uploads while streaming; no complete archive needs to fit in memory.
# ================
def receive(stream, target):
	total = 0
	with target.open("xb") as output:
		while chunk := stream.read(CHUNK_BYTES):
			total += len(chunk)
			if total > MAX_ARCHIVE_BYTES:
				raise ValueError("release exceeds upload limit")
			output.write(chunk)


# ================
# announce
#
# Never include a webhook URL in an exception or follow a redirect with it.
# ================
def announce(path, message):
	endpoint = urlsplit(Path(path).read_text().strip())
	if endpoint.scheme != "https" or endpoint.netloc != "discord.com" or not endpoint.path.startswith("/api/webhooks/"):
		raise ValueError("invalid Discord webhook configuration")
	connection = http.client.HTTPSConnection("discord.com", timeout=HTTP_TIMEOUT)
	try:
		body = json.dumps({"content": message, "allowed_mentions": {"parse": []}})
		connection.request("POST", endpoint.path, body, {"Content-Type": "application/json"})
		response = connection.getresponse()
		if response.status not in (200, 204):
			raise RuntimeError("Discord notification failed: HTTP " + str(response.status))
		response.read()
	finally:
		connection.close()


# ================
# run
#
# Pass arguments directly to the executable and let failures stop the rollout.
# ================
def run(arguments, *, cwd=None, env=None, capture=False):
	return subprocess.run(arguments, cwd=cwd, env=env, check=True, capture_output=capture, text=True)


# ================
# copy_inputs
#
# Task binaries already live in Nomad's immutable release tree. Replacing
# deployer inputs therefore does not modify a running executable.
# ================
def copy_inputs(source, module):
	for name in FILES:
		target = module / name
		target.parent.mkdir(parents=True, exist_ok=True)
		temporary = target.with_name(target.name + ".incoming")
		shutil.copyfile(source / name, temporary)
		temporary.chmod(0o755 if FILES[name].startswith("bin/") else 0o644)
		os.replace(temporary, target)


# ================
# warning
#
# The first upgrade can introduce the notice endpoint only while no players
# are connected. Later upgrades require successful in-game announcements.
# ================
def warning(config, module, executable):
	arguments = [str(executable), "notice", "-state-dir", str(module / ".state/cluster"),
		"-catalog", str(module / "config/shards.json"), "-message", "Server restart in two minutes. Please find a safe place."]
	result = subprocess.run(arguments, cwd=module, capture_output=True, text=True)
	if result.returncode == 0:
		return
	if not config.get("bootstrap_notice", False):
		raise RuntimeError("in-game maintenance notice failed; restart refused")
	for shard in json.loads((module / "config/shards.json").read_text())["shards"]:
		if not shard["enabled"]:
			continue
		endpoint = urlsplit(shard["controlUrl"])
		if endpoint.scheme != "http" or not ipaddress.ip_address(endpoint.hostname).is_loopback:
			raise RuntimeError("initial notice installation requires loopback control listeners")
		connection = http.client.HTTPConnection(endpoint.hostname, endpoint.port, timeout=HTTP_TIMEOUT)
		try:
			connection.request("GET", "/internal/operations/notice", headers={"X-SRO-Local-Diagnostics": "1"})
			response = connection.getresponse()
			if response.status != 404:
				raise RuntimeError("notice endpoint exists but rejected the announcement; restart refused")
			response.read()
		finally:
			connection.close()
	# Bootstrap is a one-time operator setting, removed after a successful rollout.
	connection = http.client.HTTPConnection("127.0.0.1", 8787, timeout=HTTP_TIMEOUT)
	try:
		connection.request("GET", "/title/servers")
		response = connection.getresponse()
		if response.status != 200:
			raise RuntimeError("cannot verify an empty fleet for initial notice installation")
		rows = json.load(response)
		if not rows or any(row["onlinePlayers"] != 0 for row in rows):
			raise RuntimeError("players are connected; initial notice installation refused")
	finally:
		connection.close()
	print("Initial notice installation: fleet is empty.", flush=True)


# ================
# deploy
#
# Back up durable state and validate scoped credentials before the announced
# maintenance window. Nomad alone owns service replacement and health checks.
# A revert passes notice=False: it restores the retained server at once, and
# the release it replaces may not be able to announce anything.
# ================
def deploy(config, staging, manifest, notice=True):
	module = Path(config["module"])
	clean_env = {"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME": "/root"}
	version = run(["nomad", "version"], capture=True).stdout.splitlines()[0]
	if version != "Nomad v" + config["nomad_version"]:
		raise RuntimeError("Nomad version does not match the approved host configuration")
	# Complete a verified remote backup before touching deployment inputs.
	run(["/usr/local/sbin/opensro-backup", "run"], env=clean_env)
	management = dict(clean_env, NOMAD_ADDR="http://127.0.0.1:4646",
		NOMAD_TOKEN=json.loads(Path(config["nomad_bootstrap"]).read_text())["SecretID"])
	issued = run(["nomad", "acl", "token", "create", "-name", "github-server-release", "-type", "client",
		"-global=false", "-policy", "sro-deployer", "-ttl", "1h", "-json"], env=management, capture=True)
	token = json.loads(issued.stdout)
	environment = dict(clean_env, NOMAD_ADDR="http://127.0.0.1:4646", NOMAD_NAMESPACE="sro",
		NOMAD_TOKEN=token["SecretID"], SRO_SERVER_GAME_DATA_ROOT=config["game_data"])
	arguments = ["-namespace", "sro", "-task-user", "sro", "-allowed-origins", config["origin"],
		"-agent-memory-mb", str(config["agent_memory_mb"]), "-gameworld-memory-mb", str(config["gameworld_memory_mb"])]
	executable = str(module / "sro-nomad")
	try:
		copy_inputs(staging, module)
		run([str(module / "sro-provision-identity"), "-state-dir", str(module / ".state/cluster")], cwd=module, env=clean_env)
		cluster = module / ".state/cluster"
		shutil.chown(cluster, user="root", group="sro")
		cluster.chmod(0o710)
		run([executable, "validate", *arguments], cwd=module, env=environment)
		cache = Path(config["game_data"]).parent / ".game-data-cache"
		if cache.is_dir():
			for path in [cache, *cache.rglob("*")]:
				path.chmod(0o755 if path.is_dir() else 0o644)
		if notice:
			warning(config, module, executable)
			announce(config["public_webhook"], "OpenSRO will restart in two minutes for a server update. Please find a safe place.")
			deadline = time.monotonic() + NOTICE_SECONDS
			while time.monotonic() < deadline:
				time.sleep(min(10, max(0, deadline - time.monotonic())))
			# On the first installation, check again after the warning window.
			if config.get("bootstrap_notice", False):
				warning(config, module, executable)
		run([executable, "deploy", *arguments], cwd=module, env=environment)
		run([executable, "status", "-namespace", "sro"], cwd=module, env=environment)
		write_state(module / "release.json", manifest)
		if config.pop("bootstrap_notice", False):
			temporary = CONFIG.with_suffix(".incoming")
			temporary.write_text(json.dumps(config, indent=2) + "\n")
			temporary.chmod(0o600)
			os.replace(temporary, CONFIG)
		# The independent monitor announces recovery after the journal commits.
		# Discord availability cannot turn a healthy rollout into a failed one.
		print("Server release " + manifest["commit"] + " passed Nomad deployment health checks.", flush=True)
	finally:
		run(["nomad", "acl", "token", "delete", token["AccessorID"]], env=management, capture=True)


# ================
# verified_server
#
# Reverify a staged server archive and its Linux evidence, and unpack it into
# scratch/server. Later promotion uses these exact bytes; it never rebuilds
# binaries or silently picks a newer commit.
# ================
def verified_server(config, candidate_id, scratch):
	candidate_id = identity(candidate_id)
	record = Path(config["candidate_records"]) / candidate_id
	archive = record / "server.tar"
	with archive.open("rb") as stream:
		if hashlib.file_digest(stream, "sha256").hexdigest() != candidate_id:
			raise ValueError("server archive changed after approval preparation")
	manifest = unpack(archive, Path(scratch) / "server")
	evidence = json.loads((record / "smoke.json").read_text())
	if evidence.get("candidate") != candidate_id or evidence.get("release") != manifest["commit"]:
		raise ValueError("server evidence identifies a different artifact")
	retained = manifest["plan"]["mode"] == "rollback" and evidence.get("verification") == "retained-production"
	if evidence.get("verdict") != "PASS" or not (evidence.get("linuxTests") is True or retained):
		raise ValueError("server candidate has not passed Linux verification")
	return manifest


# ================
# retain_server
#
# Keep the live server inputs for rollback before replacing them. The inputs
# on disk must still be the recorded production release.
# ================
def retain_server(config, state):
	record = Path(config["module"]) / "release.json"
	if json.loads(record.read_text())["commit"] != state["server"]["commit"]:
		raise RuntimeError("server release record drift requires reconciliation")
	return preserve(config, "server", state["server"], config["module"], record.read_bytes())


# ================
# deployed
#
# release.json is written only after Nomad deployment and status pass, so a
# later error (token cleanup) does not undo those completed health checks.
# ================
def deployed(config, manifest):
	return json.loads((Path(config["module"]) / "release.json").read_text()) == manifest


# ================
# rollout
#
# deploy, where an error after the health checks passed (token cleanup) is
# a warning, returned for the journal, not a failed release.
# ================
def rollout(config, staging, manifest, notice=True):
	try:
		deploy(config, staging, manifest, notice)
	except Exception as error:
		if not deployed(config, manifest):
			raise
		print("Deployment is healthy; check scoped token cleanup before the one-hour expiry.", file=sys.stderr)
		return {"component": "server", "detail": "Post-deploy cleanup failed: " + type(error).__name__}
	return None


# ================
# alert_staff
#
# A failed publication tells the staff channel; delivery failure is printed,
# never allowed to hide the original error.
# ================
def alert_staff(config, message):
	try:
		announce(config["staff_webhook"], message)
	except Exception:
		print("Staff alert could not be delivered.", file=sys.stderr)


# ================
# deploy_approved
#
# The receiver's lock covers admission, the restart window and the final
# health result. A failed operation remains visible and blocks blind retries.
# ================
def deploy_approved(config, staging, manifest):
	state_path = Path(config["production_state"])
	state = read_state(state_path)
	plan = manifest["plan"]
	admit(state, plan)
	retain_server(config, state)
	pending = begin(state, plan, time.time())
	write_state(state_path, pending)
	try:
		warning = rollout(config, staging, manifest)
	except Exception:
		pending["operation"]["phase"] = "failed"
		write_state(state_path, pending)
		raise
	result = complete(pending, plan, time.time())
	if warning:
		result["lastWarning"] = warning
	write_state(state_path, result)


# ================
# publish_server
#
# Reverify the retained archive and current production generation under the
# receiver lock, then deploy it.
# ================
def publish_server(config, candidate_id, scratch):
	manifest = verified_server(config, candidate_id, scratch)
	try:
		deploy_approved(config, Path(scratch) / "server", manifest)
	except Exception:
		alert_staff(config, "Server release " + manifest["commit"] + " failed. Check the Actions log and Nomad job status before retrying.")
		raise
	return read_state(config["production_state"])
