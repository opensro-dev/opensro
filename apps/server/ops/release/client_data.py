"""
===========================================================================

client_data.py - browser releases that carry new asset data.

An application candidate reuses the live asset data byte for byte. A data
candidate replaces it: the asset packs are built on the operator's machine
from the licensed client, so they reach the host as content-addressed
payload batches the stage role verifies and keeps in the payload store,
and a small candidate archive (manifest, plan, application files) stages
them. Staging reuses every served file the live release already holds by
hard link, writes the rest from the store with the same route rules the
first release used, and records the sha256 of every served file so that
publication can re-verify the whole candidate.

Publication (client_deploy.promote) and release admission treat a data
candidate like any client candidate, except that it may change the asset
schema: it brings the data that schema describes.

===========================================================================
"""

import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import tarfile
import tempfile
import time

from client_bundle import (
	HASH_PATTERN,
	MAX_MANIFEST_BYTES,
	application_files,
	require_declared_contract,
	safe_name,
)
from release_state import admit, identity, read_state, write_state

FORMAT = "opensro-client-data-candidate-v1"
BATCH_FORMAT = "opensro-client-payload-v1"
MAX_BATCH_BYTES = 256 << 20
MAX_PAYLOAD_FILE_BYTES = 128 << 20
MAX_BATCH_FILES = 4096
STORE_RETENTION_SECONDS = 14 * 24 * 3600
COPY_CHUNK_BYTES = 1 << 20


# ================
# digest
# ================
def digest(path):
	with Path(path).open("rb") as stream:
		return hashlib.file_digest(stream, "sha256").hexdigest()


# ================
# data_rows
#
# The manifest's data inventory, keyed by path. Every row names its sha256;
# the payload store holds each file under that digest.
# ================
def data_rows(manifest):
	rows = {}
	for row in manifest["files"]:
		if row["kind"] == "application":
			continue
		name = safe_name(row["path"])
		if name in rows or not HASH_PATTERN.fullmatch(row.get("sha256", "")):
			raise ValueError("invalid data inventory row: " + name)
		if type(row["length"]) is not int or not 0 <= row["length"] <= MAX_PAYLOAD_FILE_BYTES:
			raise ValueError("invalid data file size: " + name)
		rows[name] = row
	return rows


# ================
# outputs
#
# The served files one release materializes, as {relative path: (file,
# offset, length, decode)}. These are the first release's rules
# (stage_static.py): a gzip-encoded route serves its decoded bytes plus the
# exact gzip sidecar, and a route with a gzip representation serves that
# sidecar, unless the sidecar URL is itself a route.
# ================
def outputs(manifest):
	names = {route["url"] for route in manifest["routes"]}
	result = {}

	def add(path, source):
		path = safe_name(path)
		if path in result:
			raise ValueError("two routes serve one file: " + path)
		result[path] = source

	for route in manifest["routes"]:
		if route["file"].startswith("application/"):
			continue
		target = route["url"].removeprefix("/")
		plain = (route["file"], route["offset"], route["length"], False)
		if route.get("encoding") == "gzip":
			add(target, (route["file"], route["offset"], route["length"], True))
			if route["url"] + ".gz" not in names:
				add(target + ".gz", plain)
		else:
			add(target, plain)
		if route.get("gzip") and route["url"] + ".gz" not in names:
			encoded = route["gzip"]
			add(target + ".gz", (encoded["file"], encoded["offset"], encoded["length"], False))
	return result


# ================
# store_payload
#
# Keep a batch of payload files in the store, each verified against its
# declared digest and length. Files already present are skipped, so an
# interrupted upload resumes by sending the rest. Stale entries of
# abandoned uploads are pruned.
# ================
def store_payload(config, archive):
	store = Path(config["payload_store"])
	store.mkdir(mode=0o700, parents=True, exist_ok=True)
	if Path(archive).stat().st_size > MAX_BATCH_BYTES:
		raise ValueError("payload batch exceeds limit")
	stored = present = 0
	with tarfile.open(archive, "r:") as batch:
		members = batch.getmembers()
		if len(members) > MAX_BATCH_FILES + 1 or not members or members[0].name != "payload.json":
			raise ValueError("payload batch must begin with its declaration")
		declaration = json.load(batch.extractfile(members[0]))
		if declaration.get("format") != BATCH_FORMAT:
			raise ValueError("invalid payload batch")
		declared = {row["sha256"]: row["length"] for row in declaration["files"]}
		for member in members[1:]:
			if not HASH_PATTERN.fullmatch(member.name) or member.name not in declared or not member.isfile():
				raise ValueError("undeclared or non-file payload member")
			if member.size != declared[member.name] or member.size > MAX_PAYLOAD_FILE_BYTES:
				raise ValueError("payload length differs from its declaration")
			target = store / member.name
			if target.exists() and digest(target) == member.name:
				present += 1
				continue
			temporary = store / (member.name + ".incoming")
			hasher = hashlib.sha256()
			with batch.extractfile(member) as source, temporary.open("wb") as output:
				while chunk := source.read(COPY_CHUNK_BYTES):
					hasher.update(chunk)
					output.write(chunk)
			if hasher.hexdigest() != member.name:
				temporary.unlink()
				raise ValueError("payload bytes differ from their digest: " + member.name)
			os.replace(temporary, target)
			stored += 1
	cutoff = time.time() - STORE_RETENTION_SECONDS
	for entry in store.iterdir():
		if entry.stat().st_mtime < cutoff:
			entry.unlink()
	return {"stored": stored, "present": present}


# ================
# unpack
#
# Validate a data candidate's member set, plan and declared contract before
# returning any payload. The manifest's data files are not members: they
# arrive through the payload store.
# ================
def unpack(source):
	if Path(source).stat().st_size > MAX_BATCH_BYTES:
		raise ValueError("data candidate exceeds upload limit")
	with tarfile.open(source, "r:") as archive:
		members = {member.name: member for member in archive.getmembers()}
		for name, member in members.items():
			safe_name(name)
			if not member.isfile():
				raise ValueError("non-file data candidate member")
		for name in ("candidate.json", "release.json"):
			if name not in members or members[name].size > MAX_MANIFEST_BYTES:
				raise ValueError("missing or oversized data candidate metadata")
		candidate = json.load(archive.extractfile("candidate.json"))
		raw = archive.extractfile("release.json").read()
		if candidate.get("format") != FORMAT or hashlib.sha256(raw).hexdigest() != candidate.get("manifestSha256"):
			raise ValueError("invalid data candidate identity")
		manifest = json.loads(raw)
		files = application_files(manifest)
		data_rows(manifest)
		plan = candidate["plan"]
		if plan.get("component") != "client" or plan.get("kind") != "data" or plan.get("release") != manifest["releaseId"]:
			raise ValueError("candidate and approval identify different data releases")
		identity(plan.get("commit"))
		if plan.get("baseRelease") != candidate.get("baseRelease"):
			raise ValueError("candidate and approval identify different bases")
		require_declared_contract(manifest, plan)
		if set(members) != set(files) | {"candidate.json", "release.json"}:
			raise ValueError("unlisted or missing data candidate member")
		payloads = {}
		for name, row in files.items():
			data = archive.extractfile(name).read()
			if len(data) != row["length"] or hashlib.sha256(data).hexdigest() != row["sha256"]:
				raise ValueError("application payload hash mismatch")
			payloads[name] = data
	return candidate, manifest, raw, payloads


# ================
# materialize
#
# Write one served file from a verified store payload. Files staging creates
# are world-readable; hard links into the live release keep their metadata.
# ================
def materialize(store, source, target):
	sha, offset, length, decode = source
	payload = Path(store) / sha
	with payload.open("rb") as stream:
		stream.seek(offset)
		data = stream.read(length)
	if len(data) != length:
		raise ValueError("truncated data slice: " + sha)
	target.parent.mkdir(parents=True, exist_ok=True)
	with target.open("xb") as output:
		output.write(gzip.decompress(data) if decode else data)
	target.chmod(0o644)


# ================
# content_sources
#
# outputs() keyed by content: (payload sha256, offset, length, decode). A
# data file may keep its path while its bytes change (publication.json), so
# reuse is decided by the digest, never the name.
# ================
def content_sources(manifest):
	rows = data_rows(manifest)
	return {
		path: (rows[file]["sha256"], offset, length, decode)
		for path, (file, offset, length, decode) in outputs(manifest).items()
	}


# ================
# stage
#
# Build the candidate's served tree beside the live one. Returns the same
# identity fields an application candidate reports.
# ================
def stage(config, archive, verify_stored=True):
	from client_deploy import application_routes, live_directory, public_parents, retain_shared_asset
	state = read_state(config["production_state"])
	candidate, manifest, raw, payloads = unpack(archive)
	admit(state, candidate["plan"])
	live = live_directory(config, state["client"])
	if Path(config["client_link"]).resolve() != live.resolve():
		raise RuntimeError("live client symlink drift requires reconciliation")
	base = json.loads(Path(config["client_manifest"]).read_text(encoding="utf-8"))
	if base["releaseId"] != state["client"]["release"]:
		raise RuntimeError("live asset manifest drift requires reconciliation")
	store = Path(config["payload_store"])
	base_by_source = {source: path for path, source in content_sources(base).items()}
	wanted = content_sources(manifest)
	needed = {source[0] for source in wanted.values() if source not in base_by_source}
	missing = sorted(sha for sha in needed if not (store / sha).is_file())
	if missing:
		raise ValueError(f"payload store lacks {len(missing)} data files; upload them first")
	if verify_stored:
		for sha in needed:
			if digest(store / sha) != sha:
				raise ValueError("stored payload changed: " + sha)
	candidate_id = digest(archive)
	record = Path(config["candidate_records"]) / candidate_id
	destination = Path(config["client_candidates"]) / candidate_id
	application = application_routes(manifest, payloads)
	result = {
		"candidate": candidate_id,
		"release": manifest["releaseId"],
		"commit": candidate["plan"]["commit"],
		"entrySha256": hashlib.sha256(application["index.html"]).hexdigest(),
	}
	if record.exists() or destination.exists():
		verify(config, candidate_id)
		return result
	with tempfile.TemporaryDirectory(prefix="client-data-", dir=config["client_candidates"]) as directory:
		staging = Path(directory) / "site"
		staging.mkdir()
		for path, source in wanted.items():
			target = staging / path
			if source in base_by_source:
				target.parent.mkdir(parents=True, exist_ok=True)
				os.link(live / base_by_source[source], target)
			else:
				materialize(store, source, target)
		for name, data in application.items():
			public_parents(staging, name)
			(staging / name).write_bytes(data)
			(staging / name).chmod(0o644)
			if name.startswith("assets/"):
				retain_shared_asset(config["application_assets"], name, application)
		served = {}
		for path in sorted(p for p in staging.rglob("*") if p.is_file()):
			served[path.relative_to(staging).as_posix()] = digest(path)
		for path in staging.rglob("*"):
			if path.is_dir():
				path.chmod(0o755)
		staging.chmod(0o755)
		os.rename(staging, destination)
		record.mkdir(mode=0o700)
		(record / "release.json").write_bytes(raw)
		write_state(record / "candidate.json", candidate)
		write_state(record / "served.json", served)
		shutil.copyfile(archive, record / "data.tar")
		(record / "data.tar").chmod(0o600)
	return result


# ================
# verify
#
# Recheck every served file of a staged data candidate against the digests
# recorded when it was built: nothing added, removed or changed.
# ================
def verify(config, candidate_id):
	record = Path(config["candidate_records"]) / identity(candidate_id)
	archive = record / "data.tar"
	if digest(archive) != candidate_id:
		raise ValueError("staged data archive no longer matches the approved identity")
	candidate, manifest, raw, _ = unpack(archive)
	if raw != (record / "release.json").read_bytes():
		raise ValueError("staged manifest changed after validation")
	if candidate != json.loads((record / "candidate.json").read_text()):
		raise ValueError("staged approval plan changed after validation")
	served = json.loads((record / "served.json").read_text())
	root = Path(config["client_candidates"]) / candidate_id
	actual = {p.relative_to(root).as_posix() for p in root.rglob("*") if p.is_file()}
	if actual != set(served):
		raise ValueError("staged data candidate gained or lost served files")
	for path, sha in served.items():
		if digest(root / path) != sha:
			raise ValueError("staged data changed after validation: " + path)
	return candidate, manifest


# ================
# is_data_candidate
# ================
def is_data_candidate(config, candidate_id):
	return (Path(config["candidate_records"]) / identity(candidate_id) / "data.tar").is_file()


# ================
# add_bytes
#
# Stable tar metadata keeps an identical archive byte-for-byte reproducible.
# ================
def add_bytes(archive, name, data):
	entry = tarfile.TarInfo(name)
	entry.size = len(data)
	entry.mode = 0o644
	archive.addfile(entry, io.BytesIO(data))


# ================
# bundle
#
# Operator side: write the data candidate archive for a built release package
# against the live base, and the payload batches holding exactly the data
# files the base cannot supply. The caller builds the plan (plan.build_plan
# with kind "data") against the production state it read with the base.
# Returns the batch paths.
# ================
def bundle(package, base, plan, output):
	package, output = Path(package), Path(output)
	raw = (package / "release.json").read_bytes()
	if len(raw) > MAX_MANIFEST_BYTES:
		raise ValueError("client manifest exceeds limit")
	manifest = json.loads(raw)
	files = application_files(manifest)
	rows = data_rows(manifest)
	if plan.get("kind") != "data" or plan["release"] != manifest["releaseId"] or plan["baseRelease"] != base["releaseId"]:
		raise ValueError("the plan does not declare this data release over this base")
	require_declared_contract(manifest, plan)
	candidate = {
		"format": FORMAT,
		"manifestSha256": hashlib.sha256(raw).hexdigest(),
		"baseRelease": plan["baseRelease"],
		"plan": plan,
	}
	output.mkdir(parents=True, exist_ok=False)
	with tarfile.open(output / "candidate.tar", "w") as archive:
		add_bytes(archive, "candidate.json", json.dumps(candidate, sort_keys=True).encode())
		add_bytes(archive, "release.json", raw)
		for name, row in sorted(files.items()):
			data = (package / name).read_bytes()
			if len(data) != row["length"] or hashlib.sha256(data).hexdigest() != row["sha256"]:
				raise ValueError("application bytes differ from verified manifest")
			add_bytes(archive, name, data)
	reusable = set(content_sources(base).values())
	needed = sorted({source[0] for source in content_sources(manifest).values() if source not in reusable})
	path_of = {row["sha256"]: name for name, row in rows.items()}
	batches, batch, size = [], [], 0
	for sha in needed:
		length = rows[path_of[sha]]["length"]
		if batch and size + length > MAX_BATCH_BYTES - (1 << 20):
			batches.append(batch)
			batch, size = [], 0
		batch.append(sha)
		size += length
	if batch:
		batches.append(batch)
	written = []
	for index, shas in enumerate(batches):
		target = output / f"payload-{index:03d}.tar"
		declaration = {"format": BATCH_FORMAT, "files": [{"sha256": sha, "length": rows[path_of[sha]]["length"]} for sha in shas]}
		with tarfile.open(target, "w") as archive:
			add_bytes(archive, "payload.json", json.dumps(declaration, sort_keys=True).encode())
			for sha in shas:
				data = (package / path_of[sha]).read_bytes()
				if hashlib.sha256(data).hexdigest() != sha:
					raise ValueError("data file differs from its manifest digest: " + path_of[sha])
				add_bytes(archive, sha, data)
		written.append(target)
	return written
