"""
===========================================================================

retention.py - retain verified application inputs before replacing a component.

Rollback retains executables and browser application bytes, never databases.
Snapshots are immutable and checked against the inspected production manifest.
Only recorded production history can later authorize one as a rollback target.

===========================================================================
"""

import hashlib
import json
import os
from pathlib import Path
import shutil
import tempfile

from bundle import release_files
from client_bundle import application_files, safe_name
from release_state import identity


# ================
# directory
#
# Retained source lives outside public web roots and outside SSH user ownership.
# ================
def directory(config, component, release):
	if component not in ("client", "server"):
		raise ValueError("invalid retention component")
	return Path(config["candidate_records"]).parent / "retained" / component / identity(release)


# ================
# preserve
#
# Copy every verified input into a fresh generation and rename only after all
# hashes pass. Existing snapshots are reused only after their bytes revalidate.
# ================
def preserve(config, component, row, live, raw_manifest):
	manifest = json.loads(raw_manifest)
	inputs = {}
	if component == "server":
		if manifest["commit"] != row["commit"]:
			raise ValueError("server retention identity mismatch")
		inputs = {name: (Path(live) / name, manifest["files"][name]) for name in release_files(manifest["files"])}
	else:
		if manifest["releaseId"] != row["release"]:
			raise ValueError("client retention identity mismatch")
		files = application_files(manifest)
		for route in manifest["routes"]:
			if not route["file"].startswith("application/"):
				continue
			name = safe_name(route["url"].removeprefix("/"))
			inputs[route["file"]] = (Path(live) / name, files[route["file"]]["sha256"])
			if route.get("gzip"):
				encoded = route["gzip"]["file"]
				inputs[encoded] = (Path(live) / (name + ".gz"), files[encoded]["sha256"])
		if set(inputs) != set(files):
			raise ValueError("client retention has unserved application files")
	target = directory(config, component, row["release"])
	target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
	if target.exists():
		if (target / "release.json").read_bytes() != raw_manifest:
			raise ValueError("retained manifest changed")
		for name, (_source, expected) in inputs.items():
			with (target / name).open("rb") as stream:
				if hashlib.file_digest(stream, "sha256").hexdigest() != expected:
					raise ValueError("retained input changed")
		return target
	with tempfile.TemporaryDirectory(prefix="retain-", dir=target.parent) as temporary:
		staging = Path(temporary) / "files"
		staging.mkdir(mode=0o700)
		for name, (source, expected) in inputs.items():
			destination = staging / name
			destination.parent.mkdir(parents=True, exist_ok=True)
			shutil.copyfile(source, destination)
			with destination.open("rb") as stream:
				if hashlib.file_digest(stream, "sha256").hexdigest() != expected:
					raise ValueError("live input differs from the retained release manifest")
		(staging / "release.json").write_bytes(raw_manifest)
		os.rename(staging, target)
	return target
