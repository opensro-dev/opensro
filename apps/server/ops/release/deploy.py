"""
===========================================================================

deploy.py - replace the running server with verified release inputs.

The receiver (receiver.py) owns the SSH capability and the host lock and
calls in here: publish_server for a server alone, the coordinated owner
(coordinated.py) for a server released together with its client. Nomad owns
rollout health and job reversion. A database is copied back only by the
revert of a maintenance release, from the backup its own upgrade journaled
(restore_authorities); nothing else restores state.

===========================================================================
"""

import http.client
import ipaddress
import json
import hashlib
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import time
from urllib.parse import urlsplit

from bundle import FILES, MAX_ARCHIVE_BYTES, release_files, unpack
from release_state import admit, begin, complete, identity, read_state, store_upgrade_required, write_state
from retention import preserve
import server_data

CONFIG = Path("/etc/opensro-release/config.json")
CHUNK_BYTES = 1 << 20
HTTP_TIMEOUT = 20
NOTICE_SECONDS = 120
LISTENER_TIMEOUT = 5
# Served by the edge for /api/title/servers while the Agent is down
# (sro-nomad maintenance-list); beside production.json in the public root.
MAINTENANCE_SERVERS = "maintenance-servers.json"
# Root-only, one line: the bug reporter's Discord webhook (bug_report_environment).
BUG_REPORT_WEBHOOK = "/etc/opensro-release/bug-report-webhook"
# The maintenance gate the GameWorlds read before minting an EnterWorld token
# (SRO_MAINTENANCE_GATE_PATH, sro-nomad); in the cluster directory, which the
# task user may traverse but not list.
MAINTENANCE_GATE = ".state/cluster/maintenance-gate.json"
# sro-authority-upgrade -commit names the backup it kept on this line.
UPGRADE_BACKUP_PREFIX = "Upgrade backup path: "
AUTHORITY_DB = "state.db"


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
# bug_report_environment
#
# The in-game bug reporter's Discord webhook, from a root-only file on the
# host (beside the other webhooks): the deploy runs with a clean environment,
# so without it every release left bug reports off. A missing or empty file
# names no webhook, and sro-nomad then keeps the one already stored; "off"
# removes it. The value is passed on, never logged.
# ================
def bug_report_environment(config):
	path = Path(config.get("bug_report_webhook", BUG_REPORT_WEBHOOK))
	try:
		value = path.read_text().strip()
	except FileNotFoundError:
		return {}
	return {"SRO_BUG_REPORT_DISCORD_WEBHOOK": value} if value else {}


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
# deployer inputs therefore does not modify a running executable. A file the
# deployed release does not carry (a revert to a release built before it was
# added) is removed, so the module holds exactly that release's inputs.
# ================
def copy_inputs(source, module, names):
	for name in names:
		target = module / name
		target.parent.mkdir(parents=True, exist_ok=True)
		temporary = target.with_name(target.name + ".incoming")
		shutil.copyfile(source / name, temporary)
		temporary.chmod(0o755 if FILES[name].startswith("bin/") else 0o644)
		os.replace(temporary, target)
	for name in set(FILES) - set(names):
		(module / name).unlink(missing_ok=True)


# ================
# shard_listening
#
# Whether a shard's GameWorld is running: its loopback control listener
# accepts a connection. Players reach a shard only through that process.
# ================
def shard_listening(shard):
	endpoint = urlsplit(shard["controlUrl"])
	if endpoint.scheme != "http" or not ipaddress.ip_address(endpoint.hostname).is_loopback:
		raise RuntimeError("shard control listeners must be loopback")
	try:
		socket.create_connection((endpoint.hostname, endpoint.port), timeout=LISTENER_TIMEOUT).close()
		return True
	except OSError:
		return False


# ================
# warning
#
# True once the players were warned; False when there is nobody to warn:
# no enabled shard's GameWorld is running (the 2026-10-05 outage recovery
# could not publish onto a stopped shard because the notice needs the very
# process that was down). A notice a running shard refuses still refuses
# the restart. The first upgrade can introduce the notice endpoint only
# while no players are connected.
# ================
def warning(config, module, executable):
	arguments = [str(executable), "notice", "-state-dir", str(module / ".state/cluster"),
		"-catalog", str(module / "config/shards.json"), "-message", "Server restart in two minutes. Please find a safe place."]
	result = subprocess.run(arguments, cwd=module, capture_output=True, text=True)
	if result.returncode == 0:
		return True
	enabled = [shard for shard in json.loads((module / "config/shards.json").read_text())["shards"] if shard["enabled"]]
	if not any(shard_listening(shard) for shard in enabled):
		print("No shard is running: nobody to warn; restarting without a notice.", flush=True)
		return False
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
	return True


# ================
# materialize
#
# Unpack the game data as root (sro-nomad validate resolves it) and let the
# GameWorld's user read the cache: it cannot create the extraction itself.
# ================
def materialize(config, module, executable, arguments, environment):
	run([executable, "validate", *arguments], cwd=module, env=environment)
	cache = Path(config["game_data"]).parent / ".game-data-cache"
	if cache.is_dir():
		for path in [cache, *cache.rglob("*")]:
			path.chmod(0o755 if path.is_dir() else 0o644)


# ================
# publish_maintenance_list
#
# The server list the edge serves while the Agent is down, from this
# release's own code and the host catalog. A release built before the
# command keeps the previous list.
# ================
def publish_maintenance_list(config, module, executable):
	if "production_state" not in config:
		return
	target = Path(config["production_state"]).parent / MAINTENANCE_SERVERS
	try:
		run([str(executable), "maintenance-list", "-catalog", str(module / "config/shards.json"), "-out", str(target)],
			cwd=module, capture=True)
	except (subprocess.CalledProcessError, OSError) as error:
		print("Maintenance server list not refreshed: " + type(error).__name__, flush=True)


# ================
# enabled_shards
# ================
def enabled_shards(module):
	return [shard["id"] for shard in json.loads((module / "config/shards.json").read_text())["shards"] if shard["enabled"]]


# ================
# authority_directory
#
# A shard's authority directory, always derived from the module, never from
# a path read back from the journal.
# ================
def authority_directory(module, shard_id):
	return module / ".state/shards" / shard_id / "authority"


# ================
# file_sha256
# ================
def file_sha256(path):
	with Path(path).open("rb") as stream:
		return hashlib.file_digest(stream, "sha256").hexdigest()


# ================
# upgrade_authorities
#
# A release whose server cannot open the live database stops the fleet and
# runs its own offline upgrade on every enabled shard's authority, as the
# database owner so the game server keeps its file access. The upgrade keeps
# a backup beside the database and refuses an authority still in use; an
# authority already upgraded by an interrupted attempt is left as it is.
# on_upgrade, when given, journals each shard before its commit (None) and
# after it (the backup the upgrade kept, with its sha256), so a revert knows
# what to restore even if the release fails between two shards.
# ================
def upgrade_authorities(module, executable, arguments, environment, on_upgrade=None):
	import pwd
	run([executable, "stop", *arguments], cwd=module, env=environment)
	upgrader = str(module / "sro-authority-upgrade")
	for shard in enabled_shards(module):
		authority = authority_directory(module, shard)
		owner = pwd.getpwuid((authority / AUTHORITY_DB).stat().st_uid).pw_name
		command = ["runuser", "-u", owner, "--", upgrader, "-authority-dir", str(authority)]
		run(command, cwd=module, env=environment)
		if not on_upgrade:
			run(command + ["-commit"], cwd=module, env=environment)
		else:
			on_upgrade(shard, None)
			output = run(command + ["-commit"], cwd=module, env=environment, capture=True).stdout
			print(output, end="", flush=True)
			on_upgrade(shard, upgrade_backup(authority, output, shard))
		print("Shard " + shard + " authority upgraded.", flush=True)


# ================
# upgrade_backup
#
# The backup one committed upgrade kept, named on its report line; it must
# lie in that shard's own authority directory.
# ================
def upgrade_backup(authority, output, shard):
	named = [line[len(UPGRADE_BACKUP_PREFIX):].strip() for line in output.splitlines() if line.startswith(UPGRADE_BACKUP_PREFIX)]
	if len(named) != 1 or Path(named[0]).parent.resolve() != authority.resolve():
		raise RuntimeError("shard " + shard + " upgrade named no backup in its authority directory")
	backup = Path(named[0])
	return {"backup": backup.name, "sha256": file_sha256(backup)}


# ================
# restore_authorities
#
# The revert of a maintenance release: with the fleet stopped, every shard
# the upgrade touched gets back the database its upgrade backed up. Each
# backup must still match its journaled sha256 before anything is moved;
# a shard journaled as started but without a backup refuses the restore,
# since only an operator can tell which state it is in. The upgraded
# database (and any WAL beside it) is kept as state.failed-<time>.db.
# ================
def restore_authorities(module, executable, arguments, environment, authorities):
	backups = {}
	for shard, row in sorted(authorities.items()):
		if row is None:
			raise RuntimeError("shard " + shard + " has no journaled upgrade backup; restore it by hand")
		authority = authority_directory(module, shard)
		backup = authority / Path(row["backup"]).name
		if not backup.is_file() or file_sha256(backup) != row["sha256"]:
			raise RuntimeError("shard " + shard + " upgrade backup is missing or changed; restore refused")
		backups[shard] = backup
	run([executable, "stop", *arguments], cwd=module, env=environment)
	stamp = time.strftime("%Y%m%d%H%M%S", time.gmtime())
	for shard, backup in backups.items():
		authority = authority_directory(module, shard)
		database = authority / AUTHORITY_DB
		# The restored file keeps the database owner's identity; a missing
		# database takes the backup's, which the upgrade wrote as that owner.
		status = (database if database.exists() else backup).stat()
		for suffix in ("", "-wal", "-shm"):
			current = authority / (AUTHORITY_DB + suffix)
			if current.exists():
				os.replace(current, authority / ("state.failed-" + stamp + ".db" + suffix))
		temporary = authority / (AUTHORITY_DB + ".restoring")
		shutil.copyfile(backup, temporary)
		os.chown(temporary, status.st_uid, status.st_gid)
		temporary.chmod(status.st_mode & 0o777)
		with temporary.open("rb+") as stream:
			os.fsync(stream.fileno())
		os.replace(temporary, database)
		print("Shard " + shard + " authority restored from " + backup.name + ".", flush=True)


# ================
# gate_accounts
#
# The accounts a closed gate still admits: maintenance_accounts in the host
# configuration, the release probe. A maintenance release refuses to start
# without them.
# ================
def gate_accounts(config):
	accounts = config.get("maintenance_accounts")
	if not isinstance(accounts, list) or not accounts or not all(isinstance(account, str) and account.strip() == account and account for account in accounts):
		raise ValueError("a maintenance release needs maintenance_accounts in the host configuration")
	return accounts


# ================
# open_gate
#
# Keep players out of a maintenance release until it is confirmed: the
# GameWorlds mint EnterWorld tokens only for the listed accounts (the
# release probe). Readable by the task group, replaced atomically.
# ================
def open_gate(config):
	accounts = gate_accounts(config)
	path = Path(config["module"]) / MAINTENANCE_GATE
	temporary = path.with_name(path.name + ".incoming")
	temporary.write_text(json.dumps({"accounts": accounts}) + "\n")
	own_gate(temporary)
	os.replace(temporary, path)


# ================
# own_gate
#
# The GameWorlds run as the task user in group sro.
# ================
def own_gate(path):
	shutil.chown(path, user="root", group="sro")
	path.chmod(0o640)


# ================
# close_gate
# ================
def close_gate(config):
	(Path(config["module"]) / MAINTENANCE_GATE).unlink(missing_ok=True)


# ================
# deploy
#
# Back up durable state and validate scoped credentials before the announced
# maintenance window. Nomad alone owns service replacement and health checks.
# A revert passes notice=False: it restores the retained server at once, and
# the release it replaces may not be able to announce anything. upgrade runs
# the candidate's offline store upgrade inside the announced window, journaled
# through on_upgrade; restore (a maintenance revert) puts the journaled
# backups back with the fleet stopped, before the retained server deploys.
# ================
def deploy(config, staging, manifest, notice=True, upgrade=False, on_upgrade=None, restore=None):
	module = Path(config["module"])
	names = release_files(manifest["files"])
	if upgrade and "sro-authority-upgrade" not in names:
		raise ValueError("a store upgrade release must carry sro-authority-upgrade")
	if upgrade and restore:
		raise ValueError("a deployment upgrades or restores, never both")
	clean_env = {"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME": "/root"}
	version = run(["nomad", "version"], capture=True).stdout.splitlines()[0]
	if version != "Nomad v" + config["nomad_version"]:
		raise RuntimeError("Nomad version does not match the approved host configuration")
	# The release's own verdict on the game data it will open, before any
	# backup, notice or stop: a refusal leaves the running server untouched.
	game_data = server_data.choose(config, staging)
	# Complete a verified remote backup before touching deployment inputs.
	run(["/usr/local/sbin/opensro-backup", "run"], env=clean_env)
	management = dict(clean_env, NOMAD_ADDR="http://127.0.0.1:4646",
		NOMAD_TOKEN=json.loads(Path(config["nomad_bootstrap"]).read_text())["SecretID"])
	issued = run(["nomad", "acl", "token", "create", "-name", "github-server-release", "-type", "client",
		"-global=false", "-policy", "sro-deployer", "-ttl", "1h", "-json"], env=management, capture=True)
	token = json.loads(issued.stdout)
	environment = dict(clean_env, NOMAD_ADDR="http://127.0.0.1:4646", NOMAD_NAMESPACE="sro",
		NOMAD_TOKEN=token["SecretID"], SRO_SERVER_GAME_DATA_ROOT=config["game_data"],
		**bug_report_environment(config))
	arguments = ["-namespace", "sro", "-task-user", "sro", "-allowed-origins", config["origin"],
		"-agent-memory-mb", str(config["agent_memory_mb"]), "-gameworld-memory-mb", str(config["gameworld_memory_mb"])]
	executable = str(module / "sro-nomad")
	try:
		copy_inputs(staging, module, names)
		run([str(module / "sro-provision-identity"), "-state-dir", str(module / ".state/cluster")], cwd=module, env=clean_env)
		cluster = module / ".state/cluster"
		shutil.chown(cluster, user="root", group="sro")
		cluster.chmod(0o710)
		materialize(config, module, executable, arguments, environment)
		publish_maintenance_list(config, module, executable)
		if notice and warning(config, module, executable):
			announce(config["public_webhook"], "OpenSRO will restart in two minutes for a server update. Please find a safe place.")
			deadline = time.monotonic() + NOTICE_SECONDS
			while time.monotonic() < deadline:
				time.sleep(min(10, max(0, deadline - time.monotonic())))
			# On the first installation, check again after the warning window.
			if config.get("bootstrap_notice", False):
				warning(config, module, executable)
		if upgrade:
			upgrade_authorities(module, executable, arguments, environment, on_upgrade)
		if restore:
			restore_authorities(module, executable, arguments, environment, restore)
		if game_data:
			server_data.install(config, game_data)
			materialize(config, module, executable, arguments, environment)
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
def rollout(config, staging, manifest, notice=True, upgrade=False, on_upgrade=None, restore=None):
	try:
		# The maintenance hooks travel only when set (a plain release passes none).
		hooks = {name: value for name, value in (("on_upgrade", on_upgrade), ("restore", restore)) if value}
		deploy(config, staging, manifest, notice, upgrade, **hooks)
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
	upgrade = store_upgrade_required(state, plan)
	retain_server(config, state)
	pending = begin(state, plan, time.time())
	write_state(state_path, pending)
	try:
		warning = rollout(config, staging, manifest, upgrade=upgrade)
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
