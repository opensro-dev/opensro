#!/usr/bin/env python3
"""
===========================================================================
bundle.py - freeze Linux server binaries and their jobs into one verified tar

No state, account catalogs, game data or host configuration enters a release.
===========================================================================
"""

import hashlib
import io
import json
from pathlib import Path
import re
import subprocess
import sys
import tarfile

FILES = {
	"agent": "bin/agent",
	"gameworld": "bin/gameworld",
	"sro-nomad": "bin/sro-nomad",
	"sro-provision-identity": "bin/sro-provision-identity",
	"go.mod": "go.mod",
	"ops/nomad/jobs/agent.nomad.hcl": "ops/nomad/jobs/agent.nomad.hcl",
	"ops/nomad/jobs/gameworld.nomad.hcl": "ops/nomad/jobs/gameworld.nomad.hcl",
}
MAX_ARCHIVE_BYTES = 256 << 20
MAX_MANIFEST_BYTES = 16384


# ================
# bundle
# ================
def bundle(module, destination, commit):
	if not re.fullmatch(r"[0-9a-f]{40}", commit):
		raise ValueError("release commit must be a full Git SHA")
	manifest = {"format": "opensro-server-v1", "commit": commit, "files": {}}
	with tarfile.open(destination, "w") as archive:
		for name, source in FILES.items():
			data = (module / source).read_bytes()
			manifest["files"][name] = hashlib.sha256(data).hexdigest()
			entry = tarfile.TarInfo(name)
			entry.size = len(data)
			entry.mode = 0o755 if name in FILES and "/" not in name and name != "go.mod" else 0o644
			archive.addfile(entry, io.BytesIO(data))
		data = json.dumps(manifest, sort_keys=True).encode()
		entry = tarfile.TarInfo("release.json")
		entry.size = len(data)
		archive.addfile(entry, io.BytesIO(data))
	if destination.stat().st_size > MAX_ARCHIVE_BYTES:
		raise ValueError("release exceeds the upload limit")


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
		if len(members) != len(FILES) + 1 or {item.name for item in members} != set(FILES) | {"release.json"}:
			raise ValueError("unexpected or duplicate release members")
		if any(not item.isfile() or item.size < 0 or item.size > MAX_ARCHIVE_BYTES for item in members):
			raise ValueError("release contains a non-file or oversized member")
		if archive.getmember("release.json").size > MAX_MANIFEST_BYTES:
			raise ValueError("oversized release manifest")
		manifest = json.load(archive.extractfile("release.json"))
		if manifest.get("format") != "opensro-server-v1" or not re.fullmatch(r"[0-9a-f]{40}", manifest.get("commit", "")):
			raise ValueError("invalid release identity")
		if set(manifest.get("files", {})) != set(FILES):
			raise ValueError("invalid release file list")
		for name, digest in manifest["files"].items():
			if hashlib.file_digest(archive.extractfile(name), "sha256").hexdigest() != digest:
				raise ValueError("release digest mismatch: " + name)
		for name in FILES:
			path = destination / name
			path.parent.mkdir(parents=True, exist_ok=True)
			with path.open("xb") as output:
				with archive.extractfile(name) as content:
					while chunk := content.read(1 << 20):
						output.write(chunk)
			path.chmod(0o755 if FILES[name].startswith("bin/") else 0o644)
	return manifest


# ================
# main
# ================
def main():
	module = Path(__file__).resolve().parents[2]
	commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=module, text=True).strip()
	bundle(module, Path(sys.argv[1]), commit)


if __name__ == "__main__":
	main()
