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

===========================================================================
"""

import argparse
import json
from pathlib import Path
import subprocess
import time
import urllib.request

import client_data
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
# main
# ================
def main():
	parser = argparse.ArgumentParser()
	parser.add_argument("package", type=Path)
	parser.add_argument("output", type=Path)
	parser.add_argument("--origin", required=True)
	parser.add_argument("--ssh-target", required=True)
	parser.add_argument("--identity", type=Path, required=True)
	parser.add_argument("--coordinated", action="store_true", help="publish only together with a server candidate")
	parser.add_argument("--max-batch-mib", type=int, default=client_data.MAX_BATCH_BYTES >> 20,
		help="split the payload into smaller uploads for a slow link")
	arguments = parser.parse_args()
	state = fetch_json(arguments.origin, "/releases/production.json")
	base = fetch_json(arguments.origin, "/releases/client.json")
	if base["releaseId"] != state["client"]["release"]:
		raise RuntimeError("the origin's live manifest and production state disagree; retry later")
	compatibility(state["client"]["compatibility"], "client")
	release = json.loads((arguments.package / "release.json").read_bytes())["releaseId"]
	plan = build_plan("client", release, state, {"kind": "data", "coordinated": arguments.coordinated})
	if arguments.max_batch_mib < 1:
		parser.error("--max-batch-mib must be at least 1")
	batches = client_data.bundle(arguments.package, base, plan, arguments.output, arguments.max_batch_mib << 20)
	total = sum(path.stat().st_size for path in batches)
	print(f"{len(batches)} payload batches, {total / 1048576:.1f} MiB to send", flush=True)
	for batch in batches:
		print(batch.name, send(batch, arguments.ssh_target, arguments.identity), flush=True)
	print(json.dumps(send(arguments.output / "candidate.tar", arguments.ssh_target, arguments.identity), sort_keys=True))


if __name__ == "__main__":
	main()
