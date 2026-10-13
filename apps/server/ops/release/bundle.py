#!/usr/bin/env python3
"""
===========================================================================
bundle.py - freeze Linux server binaries and their jobs into one verified tar

No state, account catalogs, game data or host configuration enters a release.

Every server binary of a forward release must carry Go's own build stamp for
the release commit from a clean checkout (vcs.revision, vcs.modified=false).
A hosted runner used to vouch for that; a release built on the operator's
machine is checked by its bytes instead, when it is bundled and again when
the host unpacks it.
===========================================================================
"""

import argparse
import hashlib
import io
import json
from pathlib import Path
import re
import subprocess
import tarfile

from plan import build_plan
from release_state import read_state

FILES = {
	"agent": "bin/agent",
	"gameworld": "bin/gameworld",
	"sro-nomad": "bin/sro-nomad",
	"sro-provision-identity": "bin/sro-provision-identity",
	"sro-authority-upgrade": "bin/sro-authority-upgrade",
	# The release's verdict on the host's server game data (server_data.py).
	"sro-game-data-check": "bin/sro-game-data-check",
	"go.mod": "go.mod",
	"ops/nomad/jobs/agent.nomad.hcl": "ops/nomad/jobs/agent.nomad.hcl",
	"ops/nomad/jobs/gameworld.nomad.hcl": "ops/nomad/jobs/gameworld.nomad.hcl",
}
# Files added to FILES after the first release. A release built before one was
# added does not carry it; every other name is required. FILES only grows.
ADDED_FILES = frozenset({"sro-authority-upgrade", "sro-game-data-check"})
# Go's build information blob (debug/buildinfo): a 16-byte-aligned header
# that opens with this magic, its pointer size and flags at 14 and 15, and
# with the inline flag the version and module strings as uvarint-prefixed
# bytes from offset 32. The module text sits between two 16-byte sentinels.
GO_BUILDINFO_MAGIC = b"\xff Go buildinf:"
GO_BUILDINFO_ALIGN = 16
GO_BUILDINFO_HEADER = 32
GO_BUILDINFO_INLINE = 0x2
GO_MODINFO_SENTINEL = 16
GO_STRING_LIMIT = 1 << 20
MAX_ARCHIVE_BYTES = 256 << 20
MAX_MANIFEST_BYTES = 256 << 10
COPY_CHUNK_BYTES = 1 << 20


# ================
# bundle
#
# Seal the deployment plan together with the exact binaries. Approval of this
# archive cannot be reused with another base generation or compatibility range.
# ================
def bundle(module, destination, commit, plan):
	if not re.fullmatch(r"[0-9a-f]{40}", commit):
		raise ValueError("release commit must be a full Git SHA")
	validate_plan(plan, commit)
	if plan["mode"] != "forward":
		raise ValueError("new builds cannot impersonate retained rollback artifacts")
	manifest = {"format": "opensro-server-v2", "commit": commit, "plan": plan, "files": {}}
	with tarfile.open(destination, "w") as archive:
		for name, source in FILES.items():
			data = (module / source).read_bytes()
			if source.startswith("bin/"):
				require_clean_build(name, data, commit)
			manifest["files"][name] = hashlib.sha256(data).hexdigest()
			entry = tarfile.TarInfo(name)
			entry.size = len(data)
			entry.mode = 0o755 if name in FILES and "/" not in name and name != "go.mod" else 0o644
			archive.addfile(entry, io.BytesIO(data))
		data = json.dumps(manifest, sort_keys=True).encode()
		if len(data) > MAX_MANIFEST_BYTES:
			raise ValueError("oversized release manifest")
		entry = tarfile.TarInfo("release.json")
		entry.size = len(data)
		archive.addfile(entry, io.BytesIO(data))
	if destination.stat().st_size > MAX_ARCHIVE_BYTES:
		raise ValueError("release exceeds the upload limit")


# ================
# go_build_settings
#
# The version and "build" settings Go stamped into one binary, read from its
# bytes the way debug/buildinfo does for an inline (Go 1.18+) blob. Anything
# else is refused rather than guessed at.
# ================
def go_build_settings(data):
	position = data.find(GO_BUILDINFO_MAGIC)
	while position >= 0 and position % GO_BUILDINFO_ALIGN:
		position = data.find(GO_BUILDINFO_MAGIC, position + 1)
	if position < 0 or len(data) < position + GO_BUILDINFO_HEADER:
		raise ValueError("binary carries no Go build information")
	if not data[position + 15] & GO_BUILDINFO_INLINE:
		raise ValueError("binary carries an unsupported Go build information layout")
	cursor = position + GO_BUILDINFO_HEADER
	strings = []
	for _ in range(2):
		length, shift = 0, 0
		while True:
			if cursor >= len(data) or shift > 63:
				raise ValueError("truncated Go build information")
			byte = data[cursor]
			cursor += 1
			length |= (byte & 0x7f) << shift
			shift += 7
			if not byte & 0x80:
				break
		if length > GO_STRING_LIMIT or cursor + length > len(data):
			raise ValueError("truncated Go build information")
		strings.append(data[cursor:cursor + length])
		cursor += length
	version, modules = strings
	# The sentinels are binary bytes: strip them before decoding the text.
	if len(modules) >= 2 * GO_MODINFO_SENTINEL + 1 and modules[-GO_MODINFO_SENTINEL - 1] == ord("\n"):
		modules = modules[GO_MODINFO_SENTINEL:-GO_MODINFO_SENTINEL]
	version, modules = version.decode("utf-8"), modules.decode("utf-8")
	settings = {}
	for line in modules.split("\n"):
		if line.startswith("build\t"):
			key, _, value = line[len("build\t"):].partition("=")
			settings[key] = value
	return version, settings


# ================
# require_clean_build
#
# A forward release's binary must be stamped with the release commit and an
# unmodified tree: the stamp, not the machine that built it, is the evidence.
# ================
def require_clean_build(name, data, commit):
	_, settings = go_build_settings(data)
	if settings.get("vcs.revision") != commit or settings.get("vcs.modified") != "false":
		raise ValueError(name + " is not built from a clean checkout of the release commit")


# ================
# validate_plan
#
# Artifact identity and approval identity must describe the same server build.
# Live generation, ancestry and peer compatibility are checked by the receiver.
# ================
def validate_plan(plan, commit):
	if not isinstance(plan, dict) or plan.get("format") != "opensro-deployment-v1":
		raise ValueError("server release requires a deployment plan")
	if plan.get("component") != "server" or plan.get("commit") != commit or plan.get("release") != commit:
		raise ValueError("server artifact and deployment plan disagree")
	if plan.get("mode") not in ("forward", "rollback"):
		raise ValueError("unknown server deployment mode")


# ================
# release_files
#
# The files one release carries, in FILES order: its own manifest decides, so
# a release retained before a file was added stays deployable. Unknown names
# and missing original names are refused.
# ================
def release_files(names):
	names = set(names)
	if not set(FILES) - ADDED_FILES <= names <= set(FILES):
		raise ValueError("invalid release file list")
	return [name for name in FILES if name in names]


# ================
# unpack
#
# Validate names, types, multiplicity and hashes before writing any file.
# Tar extraction helpers are deliberately unnecessary for this flat contract.
# ================
def unpack(source, destination):
	if source.stat().st_size > MAX_ARCHIVE_BYTES:
		raise ValueError("release exceeds the upload limit")
	with tarfile.open(source, "r:") as archive:
		members = archive.getmembers()
		if any(not item.isfile() or item.size < 0 or item.size > MAX_ARCHIVE_BYTES for item in members):
			raise ValueError("release contains a non-file or oversized member")
		if "release.json" not in {item.name for item in members}:
			raise ValueError("release has no manifest")
		if archive.getmember("release.json").size > MAX_MANIFEST_BYTES:
			raise ValueError("oversized release manifest")
		manifest = json.load(archive.extractfile("release.json"))
		if manifest.get("format") != "opensro-server-v2" or not re.fullmatch(r"[0-9a-f]{40}", manifest.get("commit", "")):
			raise ValueError("invalid release identity")
		validate_plan(manifest.get("plan"), manifest["commit"])
		names = release_files(manifest.get("files", {}))
		if len(members) != len(names) + 1 or {item.name for item in members} != set(names) | {"release.json"}:
			raise ValueError("unexpected or duplicate release members")
		for name, digest in manifest["files"].items():
			if hashlib.file_digest(archive.extractfile(name), "sha256").hexdigest() != digest:
				raise ValueError("release digest mismatch: " + name)
		# A rollback restores bytes production already ran; only a new build
		# must prove its own provenance.
		if manifest["plan"]["mode"] == "forward":
			for name in names:
				if FILES[name].startswith("bin/"):
					require_clean_build(name, archive.extractfile(name).read(), manifest["commit"])
		for name in names:
			path = destination / name
			path.parent.mkdir(parents=True, exist_ok=True)
			with path.open("xb") as output:
				with archive.extractfile(name) as content:
					while chunk := content.read(COPY_CHUNK_BYTES):
						output.write(chunk)
			path.chmod(0o755 if FILES[name].startswith("bin/") else 0o644)
	return manifest


# ================
# main
#
# Production state is downloaded before the build and supplied explicitly.
# Local working-tree state can never silently stand in for the deployed state.
# ================
def main():
	parser = argparse.ArgumentParser()
	parser.add_argument("archive", type=Path)
	parser.add_argument("state")
	parser.add_argument("--coordinated", action="store_true", help="publish only together with a client candidate")
	parser.add_argument("--maintenance", action="store_true", help="a coordinated pair for a maintenance window (implies --coordinated)")
	arguments = parser.parse_args()
	module = Path(__file__).resolve().parents[2]
	commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=module, text=True).strip()
	plan = build_plan("server", commit, read_state(arguments.state),
		{"coordinated": arguments.coordinated, "maintenance": arguments.maintenance})
	bundle(module, arguments.archive, commit, plan)


if __name__ == "__main__":
	main()
