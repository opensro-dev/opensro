"""
===========================================================================

client_deploy.py - immutable staging and approved browser publication.

Staging may create a candidate and its hashed application assets, but cannot
change the live symlink or production generation. Promotion checks the staged
artifact, its successful browser evidence and the live generation under the
same deployment lock used by the server receiver.

===========================================================================
"""

import hashlib
import gzip
import http.client
import io
import json
import os
from pathlib import Path
import re
import shutil
import tempfile
import time
from urllib.parse import urlsplit

from client_bundle import application_files, safe_name, unpack, validate_base
from release_state import admit, begin, complete, identity, read_state, write_state
from retention import preserve

HTTP_TIMEOUT_SECONDS = 20
MAX_ENTRY_BYTES = 1 << 20
APPLICATION_ASSET = re.compile(r"assets/[A-Za-z0-9_-]+-[A-Za-z0-9_-]{8,}\.(?:js|css)")


# ================
# digest
#
# Hash a regular file without loading retained asset packs into memory.
# ================
def digest(path):
	with Path(path).open("rb") as stream:
		return hashlib.file_digest(stream, "sha256").hexdigest()


# ================
# live_directory
#
# Legacy releases use their release digest; new releases use their immutable
# candidate directory. Only recorded identifiers participate in path selection.
# ================
def live_directory(config, row):
	if row.get("candidate"):
		return Path(config["client_candidates"]) / identity(row["candidate"])
	return Path(config["client_releases"]) / identity(row["release"])


# ================
# atomic_bytes
#
# Replace a file without mutating an inode shared with another release through
# a hard link. Every caller supplies an admitted destination inside its owner.
# ================
def atomic_bytes(path, data):
	path = Path(path)
	path.parent.mkdir(parents=True, exist_ok=True)
	temporary = path.with_name(path.name + ".incoming")
	with temporary.open("xb") as output:
		output.write(data)
		output.flush()
		os.fsync(output.fileno())
	temporary.chmod(0o644)
	os.replace(temporary, path)


# ================
# public_parents
#
# The receiver uses a private umask. Explicitly make only admitted public
# directories traversable, without relaxing the private candidate records.
# ================
def public_parents(root, name):
	current = Path(root)
	current.mkdir(parents=True, exist_ok=True)
	current.chmod(0o755)
	for part in Path(safe_name(name)).parent.parts:
		current /= part
		current.mkdir(exist_ok=True)
		current.chmod(0o755)


# ================
# application_routes
#
# Restrict application routes to complete files. Data slices retain their base
# authority; an application member cannot impersonate an arbitrary data route.
# ================
def application_routes(manifest, payloads):
	outputs = {}
	for route in manifest["routes"]:
		if not route["file"].startswith("application/"):
			continue
		name = safe_name(route["url"].removeprefix("/"))
		if name != "index.html" and not APPLICATION_ASSET.fullmatch(name):
			raise ValueError("application route is outside the browser application")
		data = payloads[route["file"]]
		if route["offset"] != 0 or route["length"] != len(data) or name in outputs:
			raise ValueError("invalid application route extent or duplicate")
		outputs[name] = data
		if "gzip" in route:
			encoded = route["gzip"]
			body = payloads[encoded["file"]]
			if encoded["offset"] != 0 or encoded["length"] != len(body):
				raise ValueError("invalid compressed application extent")
			with gzip.GzipFile(fileobj=io.BytesIO(body)) as stream:
				if stream.read(len(data) + 1) != data:
					raise ValueError("compressed application differs from its identity bytes")
			outputs[name + ".gz"] = body
	if "index.html" not in outputs:
		raise ValueError("application has no entry route")
	return outputs


# ================
# retain_shared_asset
#
# Vite names identify decoded application bytes, not their HTTP encoding. gzip
# can differ by operating-system header or compressor version for the same URL.
# Keep an existing valid representation unchanged; reject different content and
# bound decompression by the already-verified identity body's length.
# ================
def retain_shared_asset(root, name, outputs):
	public_parents(root, name)
	shared = Path(root) / name
	data = outputs[name]
	if not shared.exists():
		atomic_bytes(shared, data)
		return
	if digest(shared) == hashlib.sha256(data).hexdigest():
		return
	if not name.endswith(".gz"):
		raise ValueError("hashed application filename collision: " + name)
	identity_bytes = outputs[name.removesuffix(".gz")]
	try:
		with gzip.open(shared, "rb") as stream:
			retained = stream.read(len(identity_bytes) + 1)
	except (OSError, EOFError) as error:
		raise ValueError("stored application encoding is corrupt: " + name) from error
	if retained != identity_bytes:
		raise ValueError("hashed application filename collision: " + name)


# ================
# stage
#
# Admission happens before copying assets. A candidate is made visible only
# after every new file is complete; production continues to resolve the old root.
# ================
def stage(config, archive):
	state = read_state(config["production_state"])
	candidate, manifest, raw, payloads = unpack(archive)
	admit(state, candidate["plan"])
	current = live_directory(config, state["client"])
	if Path(config["client_link"]).resolve() != current.resolve():
		raise RuntimeError("live client symlink drift requires reconciliation")
	base_path = Path(config["client_manifest"])
	base = json.loads(base_path.read_text(encoding="utf-8"))
	if base["releaseId"] != state["client"]["release"]:
		raise RuntimeError("live asset manifest drift requires reconciliation")
	validate_base(base, manifest)
	outputs = application_routes(manifest, payloads)
	candidate_id = digest(archive)
	record = Path(config["candidate_records"]) / candidate_id
	destination = Path(config["client_candidates"]) / candidate_id
	result = {
		"candidate": candidate_id,
		"release": manifest["releaseId"],
		"commit": candidate["plan"]["commit"],
		"entrySha256": hashlib.sha256(outputs["index.html"]).hexdigest(),
	}
	if record.exists() or destination.exists():
		# A browser or network failure may require another preparation attempt.
		# Reuse only a complete, unchanged candidate; never overwrite its identity.
		verify_candidate(config, candidate_id)
		return result
	with tempfile.TemporaryDirectory(prefix="client-", dir=config["client_candidates"]) as directory:
		staging = Path(directory) / "site"
		shutil.copytree(current, staging, copy_function=os.link)
		for name, data in outputs.items():
			public_parents(staging, name)
			atomic_bytes(staging / name, data)
			if name.startswith("assets/"):
				retain_shared_asset(config["application_assets"], name, outputs)
		staging.chmod(0o755)
		os.rename(staging, destination)
		record.mkdir(mode=0o700)
		atomic_bytes(record / "release.json", raw)
		write_state(record / "candidate.json", candidate)
		shutil.copyfile(archive, record / "client.tar")
		(record / "client.tar").chmod(0o600)
	return result


# ================
# verify_candidate
#
# Recheck the actual staged application files before publication. A passing
# historical smoke report does not excuse changed or corrupted served bytes.
# ================
def verify_candidate(config, candidate_id):
	record = Path(config["candidate_records"]) / candidate_id
	archive = record / "client.tar"
	if digest(archive) != candidate_id:
		raise ValueError("staged archive no longer matches the approved identity")
	candidate, manifest, raw, payloads = unpack(archive)
	if raw != (record / "release.json").read_bytes():
		raise ValueError("staged manifest changed after validation")
	if candidate != json.loads((record / "candidate.json").read_text()):
		raise ValueError("staged approval plan changed after validation")
	for name, data in application_routes(manifest, payloads).items():
		target = Path(config["client_candidates"]) / candidate_id / name
		if digest(target) != hashlib.sha256(data).hexdigest():
			raise ValueError("staged application bytes changed after validation")
	return candidate, manifest


# ================
# record_smoke
#
# The staging workflow records evidence against the exact archive and served
# entry. Promotion will not accept a passing result from a different candidate.
# ================
def record_smoke(config, report):
	candidate_id = identity(report["candidate"])
	record = Path(config["candidate_records"]) / candidate_id
	candidate = json.loads((record / "candidate.json").read_text())
	if report.get("release") != candidate["plan"]["release"] or report.get("verdict") != "PASS":
		raise ValueError("browser evidence does not pass for this candidate")
	entry = Path(config["client_candidates"]) / candidate_id / "index.html"
	if report.get("entrySha256") != digest(entry):
		raise ValueError("browser evidence loaded a different entry")
	for phase in ("title", "login", "roster", "world", "gameplay", "resume"):
		if report.get("phases", {}).get(phase) != "PASS":
			raise ValueError("browser evidence is missing phase: " + phase)
	if report.get("errors"):
		raise ValueError("browser evidence contains runtime errors")
	write_state(record / "smoke.json", report)


# ================
# switch
#
# One atomic symlink replacement publishes a complete client generation.
# ================
def switch(link, target):
	link = Path(link)
	temporary = link.with_name(link.name + ".incoming")
	os.symlink(target, temporary, target_is_directory=True)
	try:
		os.replace(temporary, link)
	finally:
		# A failed rename must not leave our temporary link blocking recovery.
		temporary.unlink(missing_ok=True)


# ================
# check_entry
#
# Verify publication through the real HTTPS edge, including routing and caches.
# The bounded response must match the exact entry that passed staged testing.
# ================
def check_entry(origin, expected):
	endpoint = urlsplit(origin)
	if endpoint.scheme != "https" or endpoint.username or endpoint.password:
		raise ValueError("publication health check requires the configured HTTPS origin")
	connection = http.client.HTTPSConnection(endpoint.hostname, endpoint.port, timeout=HTTP_TIMEOUT_SECONDS)
	try:
		connection.request("GET", "/play", headers={"Cache-Control": "no-cache"})
		response = connection.getresponse()
		body = response.read(MAX_ENTRY_BYTES + 1)
		if response.status != 200 or hashlib.sha256(body).hexdigest() != expected:
			raise RuntimeError("published HTTPS entry does not match the approved client")
	finally:
		connection.close()


# ================
# promote
#
# No compiler or asset generator runs after approval. A failed edge check
# restores the previous symlink and manifest; the generation still advances so
# an approval made before this attempted publication cannot be replayed.
# ================
def promote(config, candidate_id, verify=check_entry):
	candidate_id = identity(candidate_id)
	record = Path(config["candidate_records"]) / candidate_id
	candidate, manifest = verify_candidate(config, candidate_id)
	plan = candidate["plan"]
	smoke = json.loads((record / "smoke.json").read_text())
	record_smoke(config, smoke)
	state = read_state(config["production_state"])
	admit(state, plan)
	old = live_directory(config, state["client"])
	link = Path(config["client_link"])
	if link.resolve() != old.resolve():
		raise RuntimeError("live client changed outside the release owner")
	new = Path(config["client_candidates"]) / candidate_id
	manifest_path = Path(config["client_manifest"])
	original_manifest = manifest_path.read_bytes()
	base = json.loads(original_manifest)
	if base["releaseId"] != state["client"]["release"]:
		raise ValueError("live client manifest drift requires reconciliation")
	validate_base(base, manifest)
	preserve(config, "client", state["client"], old, original_manifest)
	pending = begin(state, plan, time.time())
	write_state(config["production_state"], pending)
	try:
		switch(link, new)
		atomic_bytes(manifest_path, (record / "release.json").read_bytes())
		verify(config["origin"], smoke["entrySha256"])
	except Exception:
		switch(link, old)
		atomic_bytes(manifest_path, original_manifest)
		try:
			verify(config["origin"], digest(old / "index.html"))
		except Exception:
			pending["operation"]["phase"] = "rollback-unverified"
			write_state(config["production_state"], pending)
			raise
		state["client"]["generation"] += 1
		state["lastFailure"] = {"component": "client", "candidate": candidate_id, "at": time.time()}
		write_state(config["production_state"], state)
		raise
	result = complete(pending, plan, time.time())
	result["client"]["candidate"] = candidate_id
	result["client"]["entrySha256"] = smoke["entrySha256"]
	write_state(config["production_state"], result)
	return result
