"""
===========================================================================

server_data.py - the server game-data archive the GameWorld opens here

A server release carries binaries, not the licensed game data: the GameWorld
opens the archive at config["game_data"]. Until 2026-10-05 nothing replaced
that archive after the first deploy, and a release that needed newer data
crash-looped after the fleet had stopped (catalogue v2 against v3 code).

This module owns that archive:

- stage: the operator's staging key uploads an archive (data_release.py
  --server-data), stored by digest and marked pending.
- choose: a release checks the archive it will run with its own
  sro-game-data-check before any notice or stop. A pending archive must
  pass; otherwise the installed one; a refused installed archive (a revert
  to an older release) falls back to the newest retained archive that the
  release accepts.
- install: inside the maintenance window, the chosen archive replaces the
  live one atomically; the replaced archive is retained by digest.

===========================================================================
"""

import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile

# One upload limit for every staged archive (receiver.receive enforces it).
from bundle import MAX_ARCHIVE_BYTES

FORMAT = "opensro-server-data-v1"
DECLARATION = "server-data.json"
ARCHIVE = "server.srogz"
COPY_CHUNK_BYTES = 1 << 20
CHECK_TOOL = "sro-game-data-check"
CHECK_TIMEOUT_SECONDS = 600
# Retained archives besides the installed and pending ones.
KEEP_RETAINED = 3


# ================
# store
#
# Archives by digest, beside the live archive (same filesystem: an install
# is a rename).
# ================
def store(config):
	return Path(config["game_data"]).parent / "server-data"


# ================
# digest
# ================
def digest(path):
	with Path(path).open("rb") as stream:
		return hashlib.file_digest(stream, "sha256").hexdigest()


# ================
# pending
#
# The staged archive the next server release installs, or None.
# ================
def pending(config):
	record = store(config) / "pending.json"
	if not record.exists():
		return None
	sha = json.loads(record.read_text())["sha256"]
	path = store(config) / (sha + ".srogz")
	if not path.exists() or digest(path) != sha:
		raise RuntimeError("pending server game data is missing or changed; stage it again")
	return path


# ================
# stage
#
# One upload: server-data.json {format, sha256, length} then server.srogz.
# The bytes are hashed while copied; an archive equal to the live one only
# clears a stale pending mark.
# ================
def stage(config, archive):
	directory = store(config)
	directory.mkdir(mode=0o755, parents=True, exist_ok=True)
	with tarfile.open(archive, "r:") as package:
		members = package.getmembers()
		if [member.name for member in members] != [DECLARATION, ARCHIVE] or not all(m.isfile() for m in members):
			raise ValueError("server data upload must hold exactly its declaration and archive")
		declared = json.load(package.extractfile(members[0]))
		if declared.get("format") != FORMAT or not isinstance(declared.get("sha256"), str) or \
			len(declared["sha256"]) != 64 or declared.get("length") != members[1].size:
			raise ValueError("invalid server data declaration")
		if members[1].size > MAX_ARCHIVE_BYTES:
			raise ValueError("server data archive exceeds its limit")
		sha = declared["sha256"]
		target = directory / (sha + ".srogz")
		if not target.exists():
			temporary = directory / (sha + ".srogz.incoming")
			hasher = hashlib.sha256()
			with package.extractfile(members[1]) as source, temporary.open("wb") as output:
				while chunk := source.read(COPY_CHUNK_BYTES):
					hasher.update(chunk)
					output.write(chunk)
			if hasher.hexdigest() != sha:
				temporary.unlink()
				raise ValueError("server data bytes differ from their declared digest")
			temporary.chmod(0o644)
			os.replace(temporary, target)
	record = directory / "pending.json"
	if sha == digest(config["game_data"]):
		record.unlink(missing_ok=True)
		return {"serverData": sha, "installed": True}
	temporary = record.with_suffix(".incoming")
	temporary.write_text(json.dumps({"format": FORMAT, "sha256": sha}) + "\n")
	os.replace(temporary, record)
	prune(config)
	return {"serverData": sha, "pending": True}


# ================
# check
#
# The release's own verdict on one archive. A release built before the
# tool existed is not asked: an older binary given unknown arguments would
# start a whole server instead of checking.
# ================
def check(staging, archive):
	tool = Path(staging) / CHECK_TOOL
	if not tool.exists():
		print("Release carries no " + CHECK_TOOL + " (built before it); game data not checked.", flush=True)
		return True
	environment = {"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME": "/root",
		"SRO_SERVER_GAME_DATA_ROOT": str(archive)}
	result = subprocess.run([str(tool)], env=environment, capture_output=True, text=True,
		timeout=CHECK_TIMEOUT_SECONDS)
	if result.returncode == 0:
		return True
	lines = (result.stderr or result.stdout).strip().splitlines()
	print("Game data " + Path(archive).name + " refused: " + (lines[-1] if lines else "no output"), flush=True)
	return False


# ================
# choose
#
# The archive this release will run with, checked before anything changes:
# None keeps the installed archive. Refusal raises with the operator's next
# step; the old server keeps running.
# ================
def choose(config, staging):
	live = Path(config["game_data"])
	staged = pending(config)
	if staged:
		if check(staging, staged):
			return staged
		raise RuntimeError("the staged server game data is refused by this release; stage the archive built "
			"from the release's commit with data_release.py --server-data")
	if check(staging, live):
		return None
	installed = digest(live)
	for retained in retained_archives(config, exclude={installed}):
		if check(staging, retained):
			print("Using retained game data " + retained.name + " for this release.", flush=True)
			return retained
	raise RuntimeError("this release refuses the installed server game data; stage the archive built from the "
		"release's commit with data_release.py --server-data, then publish again")


# ================
# install
#
# Inside the maintenance window, before Nomad starts the new jobs: retain
# the live archive by digest, then replace it with one rename.
# ================
def install(config, archive):
	live = Path(config["game_data"])
	directory = store(config)
	directory.mkdir(mode=0o755, parents=True, exist_ok=True)
	retained = directory / (digest(live) + ".srogz")
	if not retained.exists():
		shutil.copy2(live, retained)
	temporary = live.with_name(live.name + ".incoming")
	shutil.copyfile(archive, temporary)
	temporary.chmod(0o644)
	os.replace(temporary, live)
	staged = directory / "pending.json"
	if staged.exists() and json.loads(staged.read_text())["sha256"] == digest(live):
		staged.unlink()
	prune(config)
	print("Installed server game data " + Path(archive).name + ".", flush=True)


# ================
# retained_archives
#
# Newest first, by modification time.
# ================
def retained_archives(config, exclude=frozenset()):
	directory = store(config)
	if not directory.exists():
		return []
	rows = [path for path in directory.glob("*.srogz") if path.stem not in exclude]
	return sorted(rows, key=lambda path: path.stat().st_mtime, reverse=True)


# ================
# prune
#
# Keep the pending archive, the installed one and the newest KEEP_RETAINED.
# ================
def prune(config):
	keep = {digest(config["game_data"])}
	record = store(config) / "pending.json"
	if record.exists():
		keep.add(json.loads(record.read_text())["sha256"])
	for path in retained_archives(config, exclude=keep)[KEEP_RETAINED:]:
		path.unlink()
