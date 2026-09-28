"""
===========================================================================

client_bundle.py - freeze application-only browser candidates over known assets.

The build contains every application byte and a complete public manifest. The
host reuses assets only when their manifest entries match the current verified
asset release exactly. An application update cannot quietly change game data.

===========================================================================
"""

import hashlib
import io
import json
from pathlib import Path, PurePosixPath
import re
import sys
import tarfile

from plan import build_plan
from release_state import identity, read_state

FORMAT = "opensro-client-candidate-v1"
MAX_ARCHIVE_BYTES = 256 << 20
MAX_MANIFEST_BYTES = 16 << 20
MAX_APPLICATION_FILES = 256
HASH_PATTERN = re.compile(r"[0-9a-f]{64}")


# ================
# safe_name
#
# Archive paths and URL paths are admitted with the same strict spelling.
# Percent escapes and platform separators cannot create a second interpretation.
# ================
def safe_name(name):
	if not isinstance(name, str) or not name or re.search(r"[\\:%?#\x00-\x1f]", name):
		raise ValueError("unsafe client release path")
	path = PurePosixPath(name)
	if path.is_absolute() or any(part in ("", ".", "..") for part in name.split("/")):
		raise ValueError("unsafe client release path")
	return name


# ================
# application_files
#
# The declared application inventory owns all uploaded bytes. Data payloads
# remain references to a separately verified base and cannot enter this tar.
# ================
def application_files(manifest):
	if manifest.get("format") != "sro-beta-release-v1":
		raise ValueError("invalid public client manifest")
	if not HASH_PATTERN.fullmatch(manifest.get("releaseId", "")):
		raise ValueError("invalid client release digest")
	identity_bytes = json.dumps({"source": manifest["sourceHash"], "files": manifest["files"],
		"routes": manifest["routes"]}, ensure_ascii=False, separators=(",", ":")).encode()
	if hashlib.sha256(identity_bytes).hexdigest() != manifest["releaseId"]:
		raise ValueError("client manifest content does not match its release digest")
	files = {}
	for row in manifest["files"]:
		if row["kind"] != "application":
			continue
		name = safe_name(row["path"])
		if name in files or not name.startswith(("application/", "encoded/")):
			raise ValueError("invalid application inventory")
		if not HASH_PATTERN.fullmatch(row.get("sha256", "")):
			raise ValueError("invalid application digest")
		if type(row["length"]) is not int or not 0 <= row["length"] <= MAX_ARCHIVE_BYTES:
			raise ValueError("invalid application size")
		files[name] = row
	if not files or len(files) > MAX_APPLICATION_FILES or "application/index.html" not in files:
		raise ValueError("incomplete or oversized application inventory")
	return files


# ================
# validate_base
#
# Compare declared data and routing, not just a claimed asset hash. This also
# prevents a candidate from changing which slice of a retained pack is served.
# ================
def validate_base(base, manifest):
	if manifest.get("assetAuthorityHash") != base.get("assetAuthorityHash"):
		raise ValueError("candidate requires a different verified asset release")
	base_files = [row for row in base["files"] if row["kind"] != "application"]
	new_files = [row for row in manifest["files"] if row["kind"] != "application"]
	base_routes = [row for row in base["routes"] if not row["file"].startswith("application/")]
	new_routes = [row for row in manifest["routes"] if not row["file"].startswith("application/")]
	if base_files != new_files or base_routes != new_routes:
		raise ValueError("application candidate changed verified data or routes")
	urls = [row["url"] for row in manifest["routes"]]
	if len(urls) != len(set(urls)):
		raise ValueError("application candidate duplicates a served route")


# ================
# add_bytes
#
# Stable tar metadata keeps an identical candidate byte-for-byte reproducible.
# ================
def add_bytes(archive, name, data):
	entry = tarfile.TarInfo(name)
	entry.size = len(data)
	entry.mode = 0o644
	archive.addfile(entry, io.BytesIO(data))


# ================
# bundle
#
# Bind the application artifact and production generation into one archive.
# Private source maps and source snapshots are deliberately not members.
# ================
def bundle(package, destination, state):
	raw = (package / "release.json").read_bytes()
	if len(raw) > MAX_MANIFEST_BYTES:
		raise ValueError("client manifest exceeds limit")
	manifest = json.loads(raw)
	files = application_files(manifest)
	plan = build_plan("client", manifest["releaseId"], state)
	candidate = {
		"format": FORMAT,
		"manifestSha256": hashlib.sha256(raw).hexdigest(),
		"baseRelease": state["client"]["release"],
		"plan": plan,
	}
	with tarfile.open(destination, "w") as archive:
		add_bytes(archive, "candidate.json", json.dumps(candidate, sort_keys=True).encode())
		add_bytes(archive, "release.json", raw)
		for name, row in sorted(files.items()):
			data = (package / name).read_bytes()
			if len(data) != row["length"] or hashlib.sha256(data).hexdigest() != row["sha256"]:
				raise ValueError("application bytes differ from verified manifest")
			add_bytes(archive, name, data)
	if destination.stat().st_size > MAX_ARCHIVE_BYTES:
		raise ValueError("client candidate exceeds upload limit")
	return candidate


# ================
# unpack
#
# Validate the entire member set before returning any payload to the staging
# owner. No tar extraction API is allowed to choose a filesystem destination.
# ================
def unpack(source):
	if source.stat().st_size > MAX_ARCHIVE_BYTES:
		raise ValueError("client candidate exceeds upload limit")
	with tarfile.open(source, "r:") as archive:
		members = archive.getmembers()
		if len(members) > MAX_APPLICATION_FILES + 2:
			raise ValueError("too many client archive members")
		names = set()
		for member in members:
			name = safe_name(member.name)
			if name in names or not member.isfile() or not 0 <= member.size <= MAX_ARCHIVE_BYTES:
				raise ValueError("duplicate, oversized or non-file client member")
			names.add(name)
		for name in ("candidate.json", "release.json"):
			if name not in names or archive.getmember(name).size > MAX_MANIFEST_BYTES:
				raise ValueError("missing or oversized candidate metadata")
		candidate = json.load(archive.extractfile("candidate.json"))
		raw = archive.extractfile("release.json").read()
		if candidate.get("format") != FORMAT or hashlib.sha256(raw).hexdigest() != candidate.get("manifestSha256"):
			raise ValueError("invalid client candidate identity")
		manifest = json.loads(raw)
		files = application_files(manifest)
		plan = candidate["plan"]
		if plan.get("component") != "client" or plan.get("release") != manifest["releaseId"]:
			raise ValueError("candidate and approval identify different clients")
		identity(plan.get("commit"))
		if plan.get("baseRelease") != candidate.get("baseRelease"):
			raise ValueError("candidate and approval identify different bases")
		if names != set(files) | {"candidate.json", "release.json"}:
			raise ValueError("unlisted or missing application member")
		payloads = {}
		for name, row in files.items():
			data = archive.extractfile(name).read()
			if len(data) != row["length"] or hashlib.sha256(data).hexdigest() != row["sha256"]:
				raise ValueError("client payload hash mismatch")
			payloads[name] = data
	return candidate, manifest, raw, payloads


# ================
# main
#
# Candidate generation is build-time work; this command never contacts a host.
# ================
def main():
	if len(sys.argv) != 4:
		raise ValueError("Usage: client_bundle.py PACKAGE OUTPUT_TAR PRODUCTION_STATE_JSON")
	bundle(Path(sys.argv[1]), Path(sys.argv[2]), read_state(sys.argv[3]))


if __name__ == "__main__":
	main()
