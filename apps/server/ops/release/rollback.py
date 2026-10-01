"""
===========================================================================

rollback.py - prepare an explicit rollback from retained production bytes.

The request names a historical release and a reason. It creates a new approval
plan against today's generation; replaying an old approval is never rollback.
Persistent game state is neither copied nor downgraded by this operation.

===========================================================================
"""

import hashlib
import json
from pathlib import Path
import tarfile

from bundle import release_files
from client_bundle import FORMAT, add_bytes, application_files, validate_base
from release_state import PLAN_FORMAT, admit, identity, read_state
from retention import directory


# ================
# prepare
#
# Rehash retained inputs and bind them to current compatibility and generation.
# Client candidates must still pass the normal browser smoke before approval.
# ================
def prepare(config, request, scratch):
	state = read_state(config["production_state"])
	component = request.get("component")
	if component not in ("client", "server"):
		raise ValueError("invalid rollback component")
	release = identity(request.get("release"))
	reason = request.get("reason")
	if not isinstance(reason, str) or not reason.strip() or len(reason) > 512:
		raise ValueError("rollback requires a bounded operator reason")
	target = next((row for row in reversed(state.get("history", []))
		if row["component"] == component and row["release"] == release), None)
	if target is None:
		raise ValueError("rollback target is not recorded production history")
	plan = {"format": PLAN_FORMAT, "component": component, "release": release, "commit": target["commit"],
		"compatibility": target["compatibility"], "baseRelease": state[component]["release"],
		"baseGeneration": state[component]["generation"], "mode": "rollback", "reason": reason.strip()}
	admit(state, plan)
	retained = directory(config, component, release)
	raw = (retained / "release.json").read_bytes()
	manifest = json.loads(raw)
	archive_path = Path(scratch) / "rollback.tar"
	if component == "client":
		files = application_files(manifest)
		validate_base(json.loads(Path(config["client_manifest"]).read_bytes()), manifest)
		metadata = {"format": FORMAT, "manifestSha256": hashlib.sha256(raw).hexdigest(),
			"baseRelease": plan["baseRelease"], "plan": plan}
		digests = {name: row["sha256"] for name, row in files.items()}
	else:
		if manifest["commit"] != target["commit"]:
			raise ValueError("retained server identity mismatch")
		release_files(manifest["files"])
		manifest.update(format="opensro-server-v2", plan=plan)
		raw = json.dumps(manifest).encode()
		digests = manifest["files"]
	with tarfile.open(archive_path, "w") as archive:
		add_bytes(archive, "release.json", raw)
		if component == "client":
			add_bytes(archive, "candidate.json", json.dumps(metadata).encode())
		for name, digest in digests.items():
			data = (retained / name).read_bytes()
			if hashlib.sha256(data).hexdigest() != digest:
				raise ValueError("rollback input differs from the verified production bytes")
			add_bytes(archive, name, data)
	return archive_path
