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
import re
import shutil
import socket
import subprocess
import sys
import tempfile
import time
from urllib.parse import urlsplit

from bundle import FILES, MAX_ARCHIVE_BYTES, release_files, unpack
from module_inputs import copy_inputs, restore_inputs, snapshot_inputs
from release_state import admit, begin, complete, identity, read_state, store_upgrade_required, write_state
from retention import preserve
import quic_host
import server_data

CONFIG = Path("/etc/opensro-release/config.json")
# The GameWorld's Nomad task user (raw_exec): the deployer passes it as
# -task-user, and the TLS pair must be readable by it.
TASK_USER = "sro"
# Every deployer command runs with only this environment (plus its own).
CLEAN_ENV = {"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME": "/root"}
# validate -assume-stopped opens each line naming a credential variable that
# needs the fleet stopped with this word (sro-nomad variables.go).
REQUIRES_STOP = "requires-stop"
# materialize's answer when the release's sro-nomad predates -assume-stopped.
LEGACY_VALIDATE = None
# Cluster state a rehearsal copy leaves out: Nomad's immutable release tree
# (binaries validate never reads) and the maintenance gate.
REHEARSAL_SKIPPED_STATE = ("releases", "maintenance-gate.json")
# The shard authorities a rehearsal validates against, linked, never copied.
REHEARSAL_SHARDS = ".state/shards"
# How much of a refused rehearsal's output reaches the staging log.
REHEARSAL_REASON_CHARS = 800
SECRET_LIKE = re.compile(r"[A-Za-z0-9+/_=-]{32,}")
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
# This deployment's shard settings (shard_settings), installed beside this file.
SHARD_SETTINGS = Path(__file__).with_name("shard-settings.json")
SHARD_SETTING_NAME = re.compile(r"SRO_[A-Z0-9_]+")
# The SRO_ variables deployer derives itself; a shard setting never names one.
DEPLOYMENT_VARIABLES = frozenset({"SRO_SERVER_GAME_DATA_ROOT", "SRO_STACK_SIZES", "SRO_BUG_REPORT_DISCORD_WEBHOOK",
	"SRO_WEBTRANSPORT_PUBLIC", "SRO_STAFF_DISCORD_WEBHOOK"})
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
# staff_environment
#
# The staff channel the GameWorld pages for ops-only alerts (the served
# WebTransport pair, #1447): the same root-only file deploy.py already
# announces staff events through. No key, or a missing or empty file,
# names no webhook, and sro-nomad keeps the one already stored; sro-nomad
# warns about, and ignores, a value that is not a Discord webhook, so this
# can never refuse a release. The value is passed on, never logged.
# ================
def staff_environment(config):
	if not config.get("staff_webhook"):
		return {}
	try:
		value = Path(config["staff_webhook"]).read_text().strip()
	except FileNotFoundError:
		return {}
	return {"SRO_STAFF_DISCORD_WEBHOOK": value} if value else {}


# ================
# shard_settings
#
# This deployment's own shard settings (#606). The public job file and the
# settings table (internal/config/shardsettings) default to native, so the
# beta values this shard runs are named here, shipped and hashed with the
# controls, and reach sro-nomad as its environment, which it validates
# against the table. A missing or malformed file refuses the release before
# any token, backup or stop: a silent native fallback would change the live
# economy.
# ================
def shard_settings(path=None):
	settings = json.loads(Path(path or SHARD_SETTINGS).read_text(encoding="utf-8"))
	if not isinstance(settings, dict):
		raise ValueError("shard settings must be a JSON object")
	for name, value in settings.items():
		if not SHARD_SETTING_NAME.fullmatch(name) or not isinstance(value, str):
			raise ValueError("shard setting " + repr(name) + " must be an SRO_ name with a string value")
		if name in DEPLOYMENT_VARIABLES:
			raise ValueError("shard setting " + name + " is derived by the deployment")
	return settings


# The WebTransport posture's sro-nomad flags, in webtransport_deployer's
# order. A release built before them refuses them all, so a deployment
# that stages such a binary must not pass them (without_undefined_wt_arguments).
WT_ARGUMENT_FLAGS = ("-transport-wt-addr", "-transport-cert-file", "-transport-key-file",
	"-transport-wt-public-port")


# ================
# webtransport_deployer
#
# The public WebTransport posture as release configuration (issue #1447):
# the switch rides the deployer's environment, which sro-nomad captures
# as the webtransport_public job variable, and the public UDP bind and
# TLS pair ride its arguments. A config without the keys deploys exactly
# as before. A switch of "on" with an incomplete posture is refused here,
# before the deployer runs; sro-nomad refuses it again on its side, and
# the GameWorld again at boot. The posture keys without the switch are a
# half-posture and deploy nothing either.
#
# The GameWorld runs unprivileged (Nomad raw_exec as the sro user), and the
# host only lets root bind ports below 1024, so a public bind on UDP 443
# would fail and the GameWorld would refuse to start. The bind port must be
# 1024 or above and the host redirects public UDP 443 to it (quic_host.py);
# sro-nomad is told the public port, so the advertised 443 still checks.
# ================
def webtransport_deployer(config):
	public = config.get("webtransport_public", "")
	if public not in ("", "on", "off"):
		raise ValueError("webtransport_public must be on, off or unset")
	keys = ("transport_wt_addr", "transport_cert_file", "transport_key_file")
	if public != "on":
		for key in keys:
			if config.get(key):
				raise ValueError(key + " requires webtransport_public on; a half-posture deploys nothing")
		return ({"SRO_WEBTRANSPORT_PUBLIC": public} if public else {}), []
	arguments = []
	for key, flag in zip(keys, WT_ARGUMENT_FLAGS):
		value = config.get(key, "")
		if not isinstance(value, str) or not value.strip():
			raise ValueError("webtransport_public is on but " + key + " is not configured")
		arguments += [flag, value.strip()]
	quic_host.require_bind(*wt_bind(config["transport_wt_addr"]))
	arguments += [WT_ARGUMENT_FLAGS[3], str(quic_host.PUBLIC_PORT)]
	return {"SRO_WEBTRANSPORT_PUBLIC": "on"}, arguments


# ================
# require_quic_host
#
# A public WebTransport deploy goes ahead only when the host already
# carries the redirect, firewall opening and buffers for its bind port
# (quic_host.problems); otherwise the listener would bind and no player
# would reach it. Checked before any token is issued.
# ================
def require_quic_host(config, wt_environment):
	if wt_environment.get("SRO_WEBTRANSPORT_PUBLIC") != "on":
		return
	gaps = quic_host.problems(wt_bind(config["transport_wt_addr"])[1])
	if gaps:
		raise RuntimeError("public WebTransport host is not ready: " + "; ".join(gaps))


# ================
# require_pair_readable
#
# The deploy runs as root, so sro-nomad's own load of the TLS pair proves
# only that root can read it. The GameWorld runs as TASK_USER, and a key
# root can read but it cannot (a synced Let's Encrypt key left 0600 root)
# would pass every deploy check and fail its boot. Each file is read as
# that user before any token is issued. (At boot the GameWorld then serves
# WebSocket without WebTransport rather than failing, #1447; this keeps the
# release from shipping into that state.)
# ================
def require_pair_readable(config, wt_environment, run_as=subprocess.run):
	if wt_environment.get("SRO_WEBTRANSPORT_PUBLIC") != "on":
		return
	for key in ("transport_cert_file", "transport_key_file"):
		path = config[key].strip()
		if run_as(["runuser", "-u", TASK_USER, "--", "test", "-r", path], check=False).returncode != 0:
			raise RuntimeError(key + " " + path + " is not readable by " + TASK_USER +
				", the GameWorld's user; it would boot without WebTransport (grant it, e.g. root:" + TASK_USER + " 0640)")


# ================
# wt_bind
#
# The host and port of a host:port WebTransport bind ("[::]:18443"); a
# value without a host or a numeric port is refused.
# ================
def wt_bind(address):
	host, separator, port = address.strip().rpartition(":")
	if not separator or not host or not port.isdigit() or not 0 < int(port) < 65536:
		raise ValueError("transport_wt_addr must be host:port, not " + repr(address))
	return host, int(port)


# ================
# executable_flags
#
# The flags the staged sro-nomad defines, from its own usage: the binary
# answers -h by printing its flag set and exiting nonzero (Go's flag
# package), so the refusal carries the list. Every subcommand shares the
# one flag set (sro-nomad parseOptions).
# ================
def executable_flags(executable):
	try:
		run([executable, "validate", "-h"], env=CLEAN_ENV, capture=True)
	except subprocess.CalledProcessError as error:
		text = (error.stderr or "") + (error.stdout or "")
		return set(re.findall(r"(?m)^\s+-([a-z][a-z0-9-]*)", text))
	raise RuntimeError(executable + " answered -h without a flag list")


# ================
# without_undefined_wt_arguments
#
# The deployment's arguments minus the WebTransport posture. deploy()
# stages the release being deployed into the module before it runs
# anything (copy_inputs), so on a revert that binary is the retained
# release's, built before the posture flags existed: passing them stops
# the revert with "flag provided but not defined". The posture is
# all-or-nothing (webtransport_deployer refuses a half-posture: "a
# half-posture deploys nothing"), so if the staged sro-nomad lacks ANY
# posture flag, every posture flag is dropped with its value, and the
# rest of the arguments stand. The SRO_WEBTRANSPORT_PUBLIC environment
# stays: a binary that does not read it ignores it. A binary that will
# not report its flags refuses the deployment rather than guessing its
# arguments.
# ================
def without_undefined_wt_arguments(executable, arguments):
	if not any(argument in WT_ARGUMENT_FLAGS for argument in arguments):
		return arguments
	defined = executable_flags(executable)
	if not defined:
		raise RuntimeError(executable + " did not report its flags; refusing to guess its WebTransport arguments")
	if all(flag[1:] in defined for flag in WT_ARGUMENT_FLAGS):
		return arguments
	return [argument for index, argument in enumerate(arguments)
		if argument not in WT_ARGUMENT_FLAGS and (index == 0 or arguments[index - 1] not in WT_ARGUMENT_FLAGS)]


# ================
# run
#
# Pass arguments directly to the executable and let failures stop the rollout.
# ================
def run(arguments, *, cwd=None, env=None, capture=False):
	return subprocess.run(arguments, cwd=cwd, env=env, check=True, capture_output=capture, text=True)


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
# requires_stop
#
# The credential variables validate -assume-stopped reports as needing the
# fleet stopped, as (job, path) pairs.
# ================
def requires_stop(output):
	stops = []
	for line in output.splitlines():
		words = line.split()
		if len(words) == 3 and words[0] == REQUIRES_STOP:
			stops.append((words[1], words[2]))
	return stops


# ================
# materialize
#
# Unpack the game data as root (sro-nomad validate resolves it) and let the
# GameWorld's user read the cache: it cannot create the extraction itself.
# With assume_stopped, validate checks everything but reports, instead of
# refusing, a credential variable that changes while its job is registered;
# those come back to the caller (requires_stop).
# ================
def materialize(config, module, executable, arguments, environment, assume_stopped=False):
	mode = ["-assume-stopped"] if assume_stopped else []
	try:
		output = run([executable, "validate", *mode, *arguments], cwd=module, env=environment, capture=True).stdout or ""
	except subprocess.CalledProcessError as error:
		# A release built before validate learned -assume-stopped (opensro
		# #760) refuses the flag: the caller falls back (LEGACY_VALIDATE).
		if assume_stopped and "assume-stopped" in (error.stderr or "") and "not defined" in (error.stderr or ""):
			return LEGACY_VALIDATE
		# The output was captured: carry the refusal's reason (masked, bounded)
		# to the SSH stream and the journal (record_failure), or it is lost as
		# on 2026-10-11.
		reason = masked((error.stderr or error.stdout or "").strip())[-REHEARSAL_REASON_CHARS:]
		print(reason, file=sys.stderr, flush=True)
		raise RuntimeError("sro-nomad validate refused this release: " + reason) from None
	print(output, end="", flush=True)
	cache = Path(config["game_data"]).parent / ".game-data-cache"
	if cache.is_dir():
		for path in [cache, *cache.rglob("*")]:
			path.chmod(0o755 if path.is_dir() else 0o644)
	return requires_stop(output)


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
# verify_restore
#
# Every journaled backup a restore needs, checked against its sha256 before
# anything stops: a refused restore leaves the running fleet untouched.
# ================
def verify_restore(module, authorities):
	backups = {}
	for shard, row in sorted(authorities.items()):
		if row is None:
			raise RuntimeError("shard " + shard + " has no journaled upgrade backup; restore it by hand")
		authority = authority_directory(module, shard)
		backup = authority / Path(row["backup"]).name
		if not backup.is_file() or file_sha256(backup) != row["sha256"]:
			raise RuntimeError("shard " + shard + " upgrade backup is missing or changed; restore refused")
		backups[shard] = backup
	return backups


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
	backups = verify_restore(module, authorities)
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
# shut_gate
#
# Shut the world to players (write the gate file) until a maintenance
# release is confirmed: the GameWorlds mint EnterWorld tokens only for the
# listed accounts (the release probe). Readable by the task group, replaced
# atomically.
# ================
def shut_gate(config):
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
# lift_gate
#
# Let players back in: remove the gate file (on confirm, or once a revert
# has the old pair back).
# ================
def lift_gate(config):
	(Path(config["module"]) / MAINTENANCE_GATE).unlink(missing_ok=True)


# ================
# masked
#
# Text bound for a log outside the host (the staging log, the journal) with
# anything that could be a token replaced: any run of 32 or more hex or
# base64url characters.
# ================
def masked(text):
	return SECRET_LIKE.sub("<masked>", text)


# ================
# check_nomad_version
#
# The host's Nomad must be the version the configuration approved.
# ================
def check_nomad_version(config):
	version = run(["nomad", "version"], capture=True).stdout.splitlines()[0]
	if version != "Nomad v" + config["nomad_version"]:
		raise RuntimeError("Nomad version does not match the approved host configuration")


# ================
# deployer
#
# A one-hour client token under the sro-deployer policy, and the clean
# environment and arguments sro-nomad runs with. The caller revokes the
# token (revoke) whatever happens.
# ================
def deployer(config, stack_sizes):
	settings = shard_settings()
	wt_environment, wt_arguments = webtransport_deployer(config)
	require_quic_host(config, wt_environment)
	require_pair_readable(config, wt_environment)
	management = dict(CLEAN_ENV, NOMAD_ADDR="http://127.0.0.1:4646",
		NOMAD_TOKEN=json.loads(Path(config["nomad_bootstrap"]).read_text())["SecretID"])
	issued = run(["nomad", "acl", "token", "create", "-name", "github-server-release", "-type", "client",
		"-global=false", "-policy", "sro-deployer", "-ttl", "1h", "-json"], env=management, capture=True)
	token = json.loads(issued.stdout)
	environment = dict(CLEAN_ENV, NOMAD_ADDR="http://127.0.0.1:4646", NOMAD_NAMESPACE="sro",
		NOMAD_TOKEN=token["SecretID"], SRO_SERVER_GAME_DATA_ROOT=config["game_data"],
		SRO_STACK_SIZES=stack_sizes,
		**bug_report_environment(config), **staff_environment(config), **wt_environment)
	# The shard's own settings last; shard_settings refuses every name
	# the deployment derives, so none of them can be overridden here.
	environment.update(settings)
	arguments = ["-namespace", "sro", "-task-user", TASK_USER, "-allowed-origins", config["origin"],
		"-agent-memory-mb", str(config["agent_memory_mb"]), "-gameworld-memory-mb", str(config["gameworld_memory_mb"]),
		*wt_arguments]
	return management, token, environment, arguments


# ================
# revoke
# ================
def revoke(management, token):
	run(["nomad", "acl", "token", "delete", token["AccessorID"]], env=management, capture=True)


# ================
# rehearse
#
# The staging-time rehearsal (2026-10-11: the F2 maintenance publish failed
# inside its window). The staged server's own sro-nomad validates against
# production as if the fleet were stopped (validate -assume-stopped), from
# a throwaway copy of the module: production's config and cluster state
# with the staged inputs over them, and the staged identity provisioner run
# on that copy as a deployment would run it. Any refusal fails the staging,
# long before a window. Returns the credential variables that need the
# fleet stopped, as (job, path) pairs; the caller decides whether this
# release's deployment will stop before it validates. Production is never
# written: the copy is removed and the scoped token revoked.
# ================
def rehearse(config, staging, manifest):
	# Nothing to rehearse against before the first installation.
	if not config.get("module") or not (Path(config["module"]) / ".state/cluster").is_dir():
		return []
	module = Path(config["module"])
	stack_sizes = config.get("stack_sizes", "")
	if not isinstance(stack_sizes, str):
		raise ValueError("stack_sizes must be a string")
	names = release_files(manifest["files"])
	check_nomad_version(config)
	copy = Path(tempfile.mkdtemp(prefix=".rehearsal-", dir=module.parent))
	try:
		copy.chmod(0o700)
		if (module / "config").is_dir():
			shutil.copytree(module / "config", copy / "config", symlinks=True)
		shutil.copytree(module / ".state/cluster", copy / ".state/cluster", symlinks=True,
			ignore=shutil.ignore_patterns(*REHEARSAL_SKIPPED_STATE))
		# sro-nomad validate requires each shard's authority database under the
		# module it runs in, and only stats it (deployment.go requireRegularFile).
		# Link the production shards into the copy instead of copying them: no
		# player data is duplicated, and the rmtree below unlinks the link,
		# never its target.
		shards = module / REHEARSAL_SHARDS
		if shards.is_dir():
			os.symlink(shards, copy / REHEARSAL_SHARDS, target_is_directory=True)
		copy_inputs(staging, copy, names)
		run([str(copy / "sro-provision-identity"), "-state-dir", str(copy / ".state/cluster")], cwd=copy, env=CLEAN_ENV)
		management, token, environment, arguments = deployer(config, stack_sizes)
		arguments = without_undefined_wt_arguments(str(copy / "sro-nomad"), arguments)
		try:
			output = run([str(copy / "sro-nomad"), "validate", "-assume-stopped", *arguments],
				cwd=copy, env=environment, capture=True).stdout
		except subprocess.CalledProcessError as error:
			reason = masked((error.stderr or error.stdout or "").strip())[-REHEARSAL_REASON_CHARS:]
			raise RuntimeError("staging rehearsal: sro-nomad validate refused this release: " + reason) from None
		finally:
			revoke(management, token)
	finally:
		shutil.rmtree(copy, ignore_errors=True)
	return requires_stop(output)


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
def deploy(config, staging, manifest, notice=True, upgrade=False, on_upgrade=None, restore=None, maintenance=False):
	stack_sizes = config.get("stack_sizes", "")
	if not isinstance(stack_sizes, str):
		raise ValueError("stack_sizes must be a string")
	module = Path(config["module"])
	names = release_files(manifest["files"])
	if upgrade and "sro-authority-upgrade" not in names:
		raise ValueError("a store upgrade release must carry sro-authority-upgrade")
	if upgrade and restore:
		raise ValueError("a deployment upgrades or restores, never both")
	check_nomad_version(config)
	# The release's own verdict on the game data it will open, before any
	# backup, notice or stop: a refusal leaves the running server untouched.
	game_data = server_data.choose(config, staging)
	# Complete a verified remote backup before touching deployment inputs.
	run(["/usr/local/sbin/opensro-backup", "run"], env=CLEAN_ENV)
	management, token, environment, arguments = deployer(config, stack_sizes)
	executable = str(module / "sro-nomad")
	# A refusal before the fleet is touched must leave the module as it was.
	# On 2026-10-27 a revert copied the retained release's inputs, its
	# sro-nomad then refused, and the module kept those bytes under the live
	# release's release.json: retain_server refused the next publish.
	# touched turns true at the first step that changes what Nomad runs
	# (stop, store upgrade or restore, game data install, deploy); from then
	# the inputs stay, since they are what the submitted jobs were built from.
	previous = snapshot_inputs(module)
	touched = False
	try:
		copy_inputs(staging, module, names)
		# The staged binary is the verdict on which arguments it takes: a
		# revert stages the retained release's sro-nomad (copy_inputs),
		# which may predate the posture flags the deployer built.
		arguments = without_undefined_wt_arguments(executable, arguments)
		run([str(module / "sro-provision-identity"), "-state-dir", str(module / ".state/cluster")], cwd=module, env=CLEAN_ENV)
		cluster = module / ".state/cluster"
		shutil.chown(cluster, user="root", group="sro")
		cluster.chmod(0o710)
		# Every check runs before anything stops, so any refusal leaves the
		# server running. sro-nomad refuses to replace a credential variable
		# (a release's new public-api-token, a rotated webhook) while its job
		# is registered (2026-10-11); -assume-stopped reports those instead.
		# Only a maintenance deployment may stop the fleet for them.
		maintenance = maintenance or upgrade or bool(restore)
		stops = materialize(config, module, executable, arguments, environment, assume_stopped=True)
		if stops is LEGACY_VALIDATE:
			# An older release (a revert target) cannot report credential
			# changes: validate as #759 did, after the stop when maintenance
			# stops anyway, before anything stops otherwise.
			if maintenance:
				stops = [("legacy", "unknown")]
			else:
				stops = materialize(config, module, executable, arguments, environment)
		if stops and not maintenance:
			raise RuntimeError("this release changes credential variables (" + ", ".join(path for _, path in stops) +
				"); only a maintenance release can stop the fleet to replace them")
		publish_maintenance_list(config, module, executable)
		if notice and warning(config, module, executable):
			announce(config["public_webhook"], "OpenSRO will restart in two minutes for a server update. Please find a safe place.")
			deadline = time.monotonic() + NOTICE_SECONDS
			while time.monotonic() < deadline:
				time.sleep(min(10, max(0, deadline - time.monotonic())))
			# On the first installation, check again after the warning window.
			if config.get("bootstrap_notice", False):
				warning(config, module, executable)
		if stops:
			# A restore's backups are checked before this stop, so a refused
			# restore still leaves the fleet running. The upgrade and the
			# restore stop again; a second stop finds no job and does nothing
			# (sro-nomad stopJob checks jobExists). With the fleet stopped the
			# plain validate must now pass.
			if restore:
				verify_restore(module, restore)
			touched = True
			run([executable, "stop", *arguments], cwd=module, env=environment)
			materialize(config, module, executable, arguments, environment)
		touched = True
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
	except BaseException:
		if not touched:
			try:
				restore_inputs(module, previous)
			except Exception as error:
				# Never hide the refusal; an operator restores from the
				# retained release (retention.py) if this ever fails.
				print("Deployment inputs could not be restored: " + type(error).__name__, file=sys.stderr, flush=True)
		raise
	finally:
		shutil.rmtree(previous[0], ignore_errors=True)
		revoke(management, token)


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
def rollout(config, staging, manifest, notice=True, upgrade=False, on_upgrade=None, restore=None, maintenance=False):
	try:
		# The maintenance hooks travel only when set (a plain release passes none).
		hooks = {name: value for name, value in (("on_upgrade", on_upgrade), ("restore", restore), ("maintenance", maintenance)) if value}
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
