#!/usr/bin/env python3
"""
===========================================================================

install.py - install release controls without publishing either game component.

The operator supplies an inspected live client manifest and its source commit.
Initial state is imported only after application and server hashes match disk.
Existing credentials and game state remain owned by their current services.

===========================================================================
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

from bundle import FILES
from client_bundle import application_files
from release_state import STATE_FORMAT, identity, write_state

INSTALL = Path("/usr/local/lib/opensro-release-controls")
ROOT = Path("/var/lib/opensro-release")
CONFIG = Path("/etc/opensro-release/config.json")
MODULES = ("release_state.py", "plan.py", "bundle.py", "client_bundle.py", "retention.py",
	"client_deploy.py", "deploy.py", "rollback.py", "monitor.py", "receiver.py")
CONTROL_FILES = (*MODULES, "install.py", "compatibility.json", "overview.html", "routes.caddy",
	"opensro-monitor.service", "opensro-monitor.timer")


# ================
# install_file
#
# Install a complete file by rename. Root owns executable policy and private
# records; explicit permissions are independent of the invoking shell's umask.
# ================
def install_file(path, data, mode=0o644):
	path = Path(path)
	path.parent.mkdir(parents=True, exist_ok=True)
	temporary = path.with_name(path.name + ".installing")
	with temporary.open("xb") as stream:
		stream.write(data)
		stream.flush()
		os.fsync(stream.fileno())
	temporary.chmod(mode)
	os.replace(temporary, path)


# ================
# install_version
#
# Complete an immutable version directory before changing any entry point. An
# older invocation keeps importing its own version while new invocations start
# from the replacement wrapper. Reinstalling the same commit verifies its bytes.
# ================
def install_version(source, destination, commit):
	if len(identity(commit)) != 40:
		raise ValueError("installation requires a full source commit")
	source, destination = Path(source), Path(destination)
	payloads = {name: (source / name).read_bytes() for name in CONTROL_FILES}
	manifest = {"format": "opensro-controls-v1", "commit": commit,
		"files": {name: hashlib.sha256(data).hexdigest() for name, data in payloads.items()}}
	version = destination / commit
	if version.exists():
		if json.loads((version / "installed.json").read_text()) != manifest:
			raise ValueError("installed source identity has different control bytes")
		for name, digest in manifest["files"].items():
			if hashlib.sha256((version / name).read_bytes()).hexdigest() != digest:
				raise ValueError("installed control bytes drifted: " + name)
		return version
	destination.mkdir(parents=True, exist_ok=True)
	destination.chmod(0o755)
	with tempfile.TemporaryDirectory(prefix="install-", dir=destination) as directory:
		staging = Path(directory) / "version"
		staging.mkdir(mode=0o755)
		staging.chmod(0o755)
		for name, data in payloads.items():
			install_file(staging / name, data)
		write_state(staging / "installed.json", manifest)
		os.rename(staging, version)
	return version


# ================
# inspect_live
#
# Import observed bytes, never a guessed Git checkout. This is intentionally
# strict: any pre-existing drift must be resolved before release ownership moves.
# ================
def inspect_live(config, manifest, client_commit, contracts):
	files = application_files(manifest)
	live = Path("/var/www/opensro/client").resolve(strict=True)
	if live.name != manifest["releaseId"]:
		raise ValueError("inspected manifest does not identify the live client directory")
	for route in manifest["routes"]:
		if not route["file"].startswith("application/"):
			continue
		row = files[route["file"]]
		path = live / route["url"].removeprefix("/")
		if hashlib.sha256(path.read_bytes()).hexdigest() != row["sha256"]:
			raise ValueError("live client application differs from its manifest")
	module = Path(config["module"])
	server = json.loads((module / "release.json").read_text())
	for name in FILES:
		with (module / name).open("rb") as stream:
			if hashlib.file_digest(stream, "sha256").hexdigest() != server["files"][name]:
				raise ValueError("live server input differs from its release: " + name)
	return {
		"format": STATE_FORMAT,
		"client": {"release": manifest["releaseId"], "commit": identity(client_commit), "generation": 1,
			"compatibility": contracts["client"], "entrySha256": hashlib.sha256((live / "index.html").read_bytes()).hexdigest()},
		"server": {"release": identity(server["commit"]), "commit": server["commit"], "generation": 1,
			"compatibility": contracts["server"]},
		"operation": None, "history": [],
	}


# ================
# account
#
# SSH users cannot modify the root-owned receiver, configuration or key policy.
# A forced command plus a no-arguments sudo rule grants only one capability.
# ================
def account(name, role, public_key, version):
	import pwd
	try:
		pwd.getpwnam(name)
	except KeyError:
		subprocess.run(["useradd", "--system", "--home-dir", str(ROOT / name), "--shell", "/bin/sh", name], check=True)
	home = Path(pwd.getpwnam(name).pw_dir)
	(home / ".ssh").mkdir(parents=True, exist_ok=True)
	(home / ".ssh").chmod(0o755)
	wrapper = "/usr/local/sbin/opensro-" + role
	body = "#!/bin/sh\nexec /usr/bin/python3 " + str(version / "receiver.py") + " " + role + "\n"
	install_file(wrapper, body.encode(), 0o755)
	policy = name + " ALL=(root) NOPASSWD: " + wrapper + ' ""\n'
	policy_path = Path("/etc/sudoers.d/opensro-" + role)
	install_file(policy_path, policy.encode(), 0o440)
	subprocess.run(["visudo", "-cf", str(policy_path)], check=True)
	key = public_key.strip().split()
	if len(key) < 2 or key[0] != "ssh-ed25519":
		raise ValueError("release capability requires an Ed25519 public key")
	line = 'restrict,command="sudo -n ' + wrapper + '" ' + " ".join(key[:2]) + "\n"
	install_file(home / ".ssh/authorized_keys", line.encode())


# ================
# monitor
#
# The timer can update only its heartbeat directory. It can read the release
# journal for bounded maintenance suppression but cannot change release state.
# ================
def monitor(version, config):
	import pwd
	name = "opensro-monitor"
	try:
		pwd.getpwnam(name)
	except KeyError:
		subprocess.run(["useradd", "--system", "--no-create-home", "--shell", "/usr/sbin/nologin", name], check=True)
	health = ROOT / "public/health"
	shutil.chown(health, user=name, group=name)
	webhook = Path("/etc/opensro-release/monitor-webhook")
	install_file(webhook, Path(config["public_webhook"]).read_bytes(), 0o640)
	shutil.chown(webhook, user="root", group=name)
	settings = {"kind": "fleet", "url": config["origin"] + "/api/title/servers", "shard": config["shard"],
		"readiness_url": config["origin"] + "/shards/" + config["shard"] + "/transport/readyz",
		"label": "game server", "state": str(health / "fleet.json"), "webhook": str(webhook),
		"production_state": config["production_state"]}
	install_file("/etc/opensro-release/monitor.json", (json.dumps(settings, indent=2) + "\n").encode())
	for name in ("opensro-monitor.service", "opensro-monitor.timer"):
		body = (version / name).read_text().replace("@RELEASE_MODULES@", str(version))
		install_file(Path("/etc/systemd/system") / name, body.encode())
	subprocess.run(["systemctl", "daemon-reload"], check=True)
	subprocess.run(["systemctl", "enable", "--now", "opensro-monitor.timer"], check=True)


# ================
# main
#
# Reinstallation updates controls but never resets generations. Caddy's route
# import is activated separately after validation against the existing edge.
# ================
def main():
	parser = argparse.ArgumentParser()
	parser.add_argument("--client-manifest", required=True)
	parser.add_argument("--client-commit", required=True)
	parser.add_argument("--stage-key", required=True)
	parser.add_argument("--publish-key", required=True)
	parser.add_argument("--source-commit", required=True)
	arguments = parser.parse_args()
	if os.geteuid() != 0:
		raise RuntimeError("installation requires root")
	source = Path(__file__).resolve().parent
	version = install_version(source, INSTALL, arguments.source_commit)
	config = json.loads(CONFIG.read_text())
	manifest = json.loads(Path(arguments.client_manifest).read_bytes())
	contracts = json.loads((source / "compatibility.json").read_text())
	state_path = ROOT / "public/production.json"
	initial = inspect_live(config, manifest, arguments.client_commit, contracts) if not state_path.exists() else None
	ROOT.mkdir(parents=True, exist_ok=True)
	ROOT.chmod(0o755)
	shutil.chown(ROOT, user="root", group="root")
	CONFIG.parent.chmod(0o755)
	for directory in (ROOT / "public", ROOT / "public/health", Path("/var/www/opensro/candidates"), Path("/var/www/opensro/application-assets")):
		directory.mkdir(parents=True, exist_ok=True)
		directory.chmod(0o755)
	(ROOT / "records").mkdir(exist_ok=True)
	(ROOT / "records").chmod(0o700)
	config.update(production_state=str(state_path), client_link="/var/www/opensro/client",
		client_releases="/var/www/opensro/releases", client_candidates="/var/www/opensro/candidates",
		candidate_records=str(ROOT / "records"), application_assets="/var/www/opensro/application-assets",
		client_manifest=str(ROOT / "public/client.json"))
	if initial:
		write_state(state_path, initial)
		install_file(config["client_manifest"], Path(arguments.client_manifest).read_bytes())
		write_state(ROOT / "public/candidates.json", {"candidates": []})
	install_file(ROOT / "public/index.html", (version / "overview.html").read_bytes())
	install_file("/etc/caddy/opensro-releases.caddy", (version / "routes.caddy").read_bytes())
	install_file(CONFIG, (json.dumps(config, indent=2) + "\n").encode(), 0o600)
	account("sro-stage", "stage", Path(arguments.stage_key).read_text(), version)
	account("sro-release", "publish", Path(arguments.publish_key).read_text(), version)
	monitor(version, config)
	write_state(ROOT / "installed-controls.json", json.loads((version / "installed.json").read_text()))
	print("Release controls installed; live client and server were not published.")


if __name__ == "__main__":
	main()
