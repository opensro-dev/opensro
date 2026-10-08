"""
===========================================================================

data_release.py - stage a browser release that carries new asset data.

Runs on the operator's machine, which alone holds the licensed asset build.
It reads the live release identity from the public origin, bundles the
built package against it (client_data.bundle), sends each payload batch to
the stage role until the host holds it, and stages the candidate. The
stage role verifies everything it receives; this tool never publishes.

	python data_release.py PACKAGE OUTPUT --origin https://host \\
		--ssh-target sro-stage@host --identity ~/.ssh/key [--max-batch-mib 32]

On a slow link (a VPN), smaller batches let each upload finish within its
size-scaled timeout and a rerun resume after the last stored batch.

--server-data PATH stages the server game-data archive too (server_data.py):
the next server publication checks it with its own code and installs it
inside its maintenance window. Omit PACKAGE and OUTPUT to stage it alone.

===========================================================================
"""

import argparse
import hashlib
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import time
import urllib.request

import client_data
import server_data
from plan import build_plan
from release_state import compatibility

ATTEMPTS = 5
RETRY_SECONDS = 15
UPLOAD_TIMEOUT_SECONDS = 900
# The slowest link an upload is given time for: below this rate a batch
# times out and is retried as a transport failure.
MIN_UPLOAD_BYTES_PER_SECOND = 32 << 10
FETCH_TIMEOUT_SECONDS = 60
SSH_TRANSPORT_FAILURE = 255
# receiver.request's answer to an operation its role or its controls lack.
OPERATION_REFUSED = "operation is not allowed for this release key"


# ================
# fetch_json
# ================
def fetch_json(origin, path):
	request = urllib.request.Request(origin.rstrip("/") + path, headers={"Cache-Control": "no-cache"})
	with urllib.request.urlopen(request, timeout=FETCH_TIMEOUT_SECONDS) as response:
		return json.loads(response.read())


# ================
# upload_timeout
#
# A fixed limit cannot fit every link: 80 MiB through a 90 KiB/s VPN takes
# about 15 minutes. The limit grows with the archive at the slowest
# supported rate and never drops below the base.
# ================
def upload_timeout(size):
	return max(UPLOAD_TIMEOUT_SECONDS, size // MIN_UPLOAD_BYTES_PER_SECOND)


# ================
# send
#
# One archive to the stage role's forced command, retried on transport
# failure. The host refuses a changed or unverifiable archive outright, and
# a refusal is not retried.
# ================
def send(archive, target, identity, run=subprocess.run):
	command = ["ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=20", "-o", "ServerAliveInterval=15",
		"-i", str(identity), target]
	timeout = upload_timeout(Path(archive).stat().st_size)
	for attempt in range(1, ATTEMPTS + 1):
		try:
			with Path(archive).open("rb") as stream:
				result = run(command, stdin=stream, capture_output=True, timeout=timeout)
		except subprocess.TimeoutExpired:
			# An unfinished upload is a transport failure; the host keeps
			# nothing from it, so the retry sends the archive again.
			print(f"{Path(archive).name}: attempt {attempt} timed out after {timeout} s", flush=True)
			time.sleep(RETRY_SECONDS)
			continue
		output = result.stdout.decode("utf-8", "replace").strip().splitlines()
		if result.returncode == 0 and output:
			return json.loads(output[-1])
		message = result.stderr.decode("utf-8", "replace").strip()
		# ssh reports its own transport failures as 255; any other status is
		# the receiver's verdict on the archive, which a retry cannot change.
		if result.returncode != SSH_TRANSPORT_FAILURE:
			raise RuntimeError(f"host refused {Path(archive).name}: {message[-2000:]}")
		print(f"{Path(archive).name}: attempt {attempt} failed ({message[-200:]})", flush=True)
		time.sleep(RETRY_SECONDS)
	raise RuntimeError(f"{Path(archive).name} was not delivered after {ATTEMPTS} attempts")


# ================
# server_data_upload
#
# The stage upload for one server game-data archive: its declaration, then
# the archive (server_data.stage verifies both).
# ================
def server_data_upload(archive, directory):
	with archive.open("rb") as stream:
		sha = hashlib.file_digest(stream, "sha256").hexdigest()
	declaration = json.dumps({"format": server_data.FORMAT, "sha256": sha, "length": archive.stat().st_size}).encode()
	directory.mkdir(parents=True, exist_ok=True)
	target = directory / "server-data.tar"
	with tarfile.open(target, "w") as package:
		entry = tarfile.TarInfo(server_data.DECLARATION)
		entry.size = len(declaration)
		package.addfile(entry, io.BytesIO(declaration))
		package.add(archive, arcname=server_data.ARCHIVE)
	return target


# ================
# stored_payloads
#
# Asks the host which needed payloads it already stores (receiver
# payload-inventory), in requests under its limit, so a rerun after an
# interrupted or superseded staging sends only what is missing. A host that
# refuses the operation stops the release: there is no full-upload path.
# ================
def stored_payloads(needed, target, identity, send=send):
	present = set()
	with tempfile.TemporaryDirectory(prefix="sro-inventory-") as directory:
		request = Path(directory) / "inventory-request.json"
		for start in range(0, len(needed), client_data.MAX_INVENTORY_FILES):
			chunk = needed[start:start + client_data.MAX_INVENTORY_FILES]
			request.write_text(json.dumps({"operation": "payload-inventory", "files": chunk}), encoding="utf-8")
			try:
				answer = send(request, target, identity)
			except RuntimeError as error:
				if OPERATION_REFUSED not in str(error):
					raise
				raise RuntimeError(
					"the host refused payload-inventory: its release controls predate it, or "
					f"{identity} is not a staging key. Reinstall the controls from this commit "
					"(README, Host installation: install.py --source-commit) before staging"
				) from error
			present.update(answer["present"])
	return present


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser()
	parser.add_argument("package", type=Path, nargs="?")
	parser.add_argument("output", type=Path, nargs="?")
	parser.add_argument("--origin", required=True)
	parser.add_argument("--ssh-target", required=True)
	parser.add_argument("--identity", type=Path, required=True)
	parser.add_argument("--coordinated", action="store_true", help="publish only together with a server candidate")
	parser.add_argument("--max-batch-mib", type=int, default=client_data.MAX_BATCH_BYTES >> 20,
		help="split the payload into smaller uploads for a slow link")
	parser.add_argument("--server-data", type=Path, help="also stage this server game-data archive (server.srogz)")
	arguments = parser.parse_args()
	if (arguments.package is None) != (arguments.output is None):
		parser.error("PACKAGE and OUTPUT go together")
	if arguments.package is None and not arguments.server_data:
		parser.error("nothing to stage: give PACKAGE OUTPUT and/or --server-data")
	if arguments.server_data:
		upload = server_data_upload(arguments.server_data, arguments.output or arguments.server_data.parent)
		print("server data", json.dumps(send(upload, arguments.ssh_target, arguments.identity), sort_keys=True),
			flush=True)
		if arguments.package is None:
			return
	state = fetch_json(arguments.origin, "/releases/production.json")
	base = fetch_json(arguments.origin, "/releases/client.json")
	if base["releaseId"] != state["client"]["release"]:
		raise RuntimeError("the origin's live manifest and production state disagree; retry later")
	compatibility(state["client"]["compatibility"], "client")
	release = json.loads((arguments.package / "release.json").read_bytes())["releaseId"]
	plan = build_plan("client", release, state, {"kind": "data", "coordinated": arguments.coordinated})
	if arguments.max_batch_mib < 1:
		parser.error("--max-batch-mib must be at least 1")
	present = stored_payloads(client_data.needed_payloads(arguments.package, base), arguments.ssh_target,
		arguments.identity)
	print(f"{len(present)} payloads already stored on the host", flush=True)
	batches = client_data.bundle(arguments.package, base, plan, arguments.output, arguments.max_batch_mib << 20,
		present)
	total = sum(path.stat().st_size for path in batches)
	print(f"{len(batches)} payload batches, {total / 1048576:.1f} MiB to send", flush=True)
	for batch in batches:
		print(batch.name, send(batch, arguments.ssh_target, arguments.identity), flush=True)
	print(json.dumps(send(arguments.output / "candidate.tar", arguments.ssh_target, arguments.identity), sort_keys=True))


if __name__ == "__main__":
	main()
