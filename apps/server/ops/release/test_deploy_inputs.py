"""
===========================================================================

test_deploy_inputs.py - a refused deployment leaves the module as it was

deploy() replaces the module's inputs before its checks (module_inputs.py).
These tests reproduce 2026-10-27: a revert whose staged sro-nomad refused
left the retained bytes under the live release.json, and the next
publish's retain_server refused.

===========================================================================
"""

import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from bundle import FILES
import deploy


# ================
# DeployInputsTests
#
# deploy() replaces the module's inputs before its checks; a refusal before
# the fleet is touched must put them back (2026-10-27).
# ================
class DeployInputsTests(unittest.TestCase):
	LIVE_COMMIT = "c" * 40
	LIVE_RELEASE = "d" * 64
	DROPPED = "sro-swap-sun"

	# ================
	# fixture
	#
	# A module holding the live release's inputs and its release.json, and a
	# staging directory holding an older retained release that lacks one of
	# the files added later.
	# ================
	def fixture(self, root):
		module, staging = root / "module", root / "staging"
		(module / ".state/cluster").mkdir(parents=True)
		digests = {}
		for name in FILES:
			path = module / name
			path.parent.mkdir(parents=True, exist_ok=True)
			data = ("live " + name).encode()
			path.write_bytes(data)
			path.chmod(0o755 if FILES[name].startswith("bin/") else 0o644)
			digests[name] = hashlib.sha256(data).hexdigest()
			if name != self.DROPPED:
				staged = staging / name
				staged.parent.mkdir(parents=True, exist_ok=True)
				staged.write_text("retained " + name)
		live = json.dumps({"commit": self.LIVE_COMMIT, "files": digests}, indent=2).encode()
		(module / "release.json").write_bytes(live)
		bootstrap = root / "bootstrap.json"
		bootstrap.write_text(json.dumps({"SecretID": "management-fixture"}))
		config = {
			"module": str(module), "game_data": str(root / "server.srogz"),
			"nomad_version": "2.0.7", "nomad_bootstrap": str(bootstrap),
			"origin": "https://example.test", "agent_memory_mb": 256,
			"gameworld_memory_mb": 1024, "public_webhook": "public-fixture",
			"candidate_records": str(root / "records/candidates"),
		}
		retained = {"commit": "e" * 40, "files": {name: "digest" for name in FILES if name != self.DROPPED}}
		return module, staging, config, retained, live

	# ================
	# snapshot
	#
	# Every FILES name's bytes and mode, or None when absent.
	# ================
	def snapshot(self, module):
		return {name: ((module / name).read_bytes(), (module / name).stat().st_mode) if (module / name).exists() else None
			for name in FILES}

	# ================
	# run_deploy
	#
	# deploy() with its host calls faked; refuse names the sro-nomad verb
	# that fails.
	# ================
	def run_deploy(self, config, staging, manifest, refuse):
		def execute(arguments, **_options):
			if arguments[:2] == ["nomad", "version"]:
				return SimpleNamespace(stdout="Nomad v2.0.7")
			if arguments[:4] == ["nomad", "acl", "token", "create"]:
				return SimpleNamespace(stdout=json.dumps({"SecretID": "scoped", "AccessorID": "accessor"}))
			if arguments[0].endswith("sro-nomad") and arguments[1] == refuse:
				raise subprocess.CalledProcessError(2, arguments, stderr="flag provided but not defined: -transport-wt-addr")
			return SimpleNamespace(stdout="")

		with patch.object(deploy, "run", side_effect=execute), patch.object(deploy.shutil, "chown"), \
			patch.object(deploy.server_data, "choose", return_value=None), \
			patch.object(deploy, "warning", return_value=False):
			deploy.deploy(config, staging, manifest, notice=False)

	# ================
	# test_a_refused_revert_leaves_the_module_as_it_was
	#
	# 2026-10-27: a revert staged the retained release, its sro-nomad
	# refused, and the module kept the retained bytes under the live
	# release.json, so the next publish's retain_server refused ("live input
	# differs from the retained release manifest"). The refusal must leave
	# every input, bytes and mode, and release.json unchanged, restore the
	# file the older release lacks, leave no snapshot behind, and let
	# retain_server keep the live release.
	# ================
	def test_a_refused_revert_leaves_the_module_as_it_was(self):
		with tempfile.TemporaryDirectory() as directory:
			root = Path(directory)
			module, staging, config, retained, live = self.fixture(root)
			before = self.snapshot(module)
			with self.assertRaises(RuntimeError):
				self.run_deploy(config, staging, retained, refuse="validate")
			self.assertEqual(self.snapshot(module), before)
			self.assertEqual((module / "release.json").read_bytes(), live)
			self.assertEqual([path.name for path in root.iterdir() if path.name.startswith(".inputs-")], [])
			state = {"server": {"commit": self.LIVE_COMMIT, "release": self.LIVE_RELEASE}}
			target = deploy.retain_server(config, state)
			self.assertEqual((target / "release.json").read_bytes(), live)

	# ================
	# test_a_failure_after_submit_keeps_the_new_inputs
	#
	# Once deploy is submitted to Nomad the module keeps the inputs the
	# submitted jobs were built from; the journal's revert redeploys the
	# retained release from there, and release.json still names the old one.
	# ================
	def test_a_failure_after_submit_keeps_the_new_inputs(self):
		with tempfile.TemporaryDirectory() as directory:
			root = Path(directory)
			module, staging, config, retained, live = self.fixture(root)
			with self.assertRaises(subprocess.CalledProcessError):
				self.run_deploy(config, staging, retained, refuse="deploy")
			self.assertEqual((module / "sro-nomad").read_text(), "retained sro-nomad")
			self.assertFalse((module / self.DROPPED).exists())
			self.assertEqual((module / "release.json").read_bytes(), live)
			self.assertEqual([path.name for path in root.iterdir() if path.name.startswith(".inputs-")], [])


if __name__ == "__main__":
	unittest.main()
