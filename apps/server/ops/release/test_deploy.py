"""
===========================================================================
test_deploy.py - failed preflight cannot restart the fleet or retain its token
===========================================================================
"""

import json
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from bundle import FILES
import deploy


# ================
# DeployTests
# ================
class DeployTests(unittest.TestCase):
	# ================
	# test_bug_report_webhook_comes_from_the_host_file
	#
	# A release runs with a clean environment: the webhook reaches sro-nomad
	# only from the root-only file, and a missing or empty file names none.
	# ================
	def test_bug_report_webhook_comes_from_the_host_file(self):
		with tempfile.TemporaryDirectory() as directory:
			path = Path(directory) / "bug-report-webhook"
			config = {"bug_report_webhook": str(path)}
			self.assertEqual(deploy.bug_report_environment(config), {})
			path.write_text("\n")
			self.assertEqual(deploy.bug_report_environment(config), {})
			path.write_text("https://discord.com/api/webhooks/1/abc\n")
			self.assertEqual(deploy.bug_report_environment(config),
				{"SRO_BUG_REPORT_DISCORD_WEBHOOK": "https://discord.com/api/webhooks/1/abc"})

	# ================
	# test_preflight_and_token_lifecycle
	# ================
	def test_preflight_and_token_lifecycle(self):
		for failure in (None, "validate", "notice", "deploy"):
			with self.subTest(failure=failure), tempfile.TemporaryDirectory() as directory:
				root = Path(directory)
				staging, module = root / "staging", root / "module"
				(module / ".state/cluster").mkdir(parents=True)
				for name in FILES:
					path = staging / name
					path.parent.mkdir(parents=True, exist_ok=True)
					path.write_text("fixture")
				bootstrap = root / "bootstrap.json"
				bootstrap.write_text(json.dumps({"SecretID": "management-fixture"}))
				config = {
					"module": str(module), "game_data": str(root / "server.srogz"),
					"nomad_version": "2.0.7", "nomad_bootstrap": str(bootstrap),
					"origin": "https://example.test", "agent_memory_mb": 256,
					"gameworld_memory_mb": 1024, "public_webhook": "public-fixture", "staff_webhook": "staff-fixture",
				}
				calls = []
				manifest = {"commit": "a" * 40, "files": {name: "digest" for name in FILES}}

				# ================
				# execute
				# ================
				def execute(arguments, **options):
					calls.append(arguments)
					if arguments[:2] == ["nomad", "version"]:
						return SimpleNamespace(stdout="Nomad v2.0.7\n")
					if arguments[:4] == ["nomad", "acl", "token", "create"]:
						self.assertEqual(options["env"]["NOMAD_TOKEN"], "management-fixture")
						return SimpleNamespace(stdout=json.dumps({"SecretID": "scoped-fixture", "AccessorID": "accessor-fixture"}))
					if arguments[0].endswith("sro-nomad"):
						self.assertEqual(options["env"]["NOMAD_TOKEN"], "scoped-fixture")
						if arguments[1] == failure:
							raise subprocess.CalledProcessError(1, arguments)
					return SimpleNamespace(stdout="")

				with patch.object(deploy, "run", side_effect=execute), patch.object(deploy.shutil, "chown"), \
					patch.object(deploy, "warning", side_effect=RuntimeError("refused") if failure == "notice" else None), \
					patch.object(deploy, "announce") as announcement, patch.object(deploy, "NOTICE_SECONDS", 0):
					if failure:
						with self.assertRaises((RuntimeError, subprocess.CalledProcessError)):
							deploy.deploy(config, staging, manifest)
					else:
						deploy.deploy(config, staging, manifest)
					self.assertEqual(calls[-1], ["nomad", "acl", "token", "delete", "accessor-fixture"])
					deployment_calls = [call for call in calls if len(call) > 1 and call[1] == "deploy"]
					if failure in ("validate", "notice"):
						self.assertEqual(deployment_calls, [])
						announcement.assert_not_called()
					else:
						self.assertEqual(len(deployment_calls), 1)
					self.assertEqual((module / "release.json").exists(), failure is None)

	# ================
	# test_store_upgrade_stops_the_fleet_then_upgrades_enabled_shards
	#
	# The fleet stops first (the upgrade refuses a live authority), each enabled
	# shard is validated and then committed as the database owner, and the
	# disabled shard is left alone.
	# ================
	def test_store_upgrade_stops_the_fleet_then_upgrades_enabled_shards(self):
		with tempfile.TemporaryDirectory() as directory:
			module = Path(directory)
			(module / "config").mkdir()
			(module / "config/shards.json").write_text(json.dumps({"shards": [
				{"id": "global-official", "enabled": True}, {"id": "test", "enabled": False}]}))
			authority = module / ".state/shards/global-official/authority"
			authority.mkdir(parents=True)
			(authority / "state.db").write_text("fixture")
			calls = []
			owner = SimpleNamespace(getpwuid=lambda uid: SimpleNamespace(pw_name="sro"))
			with patch.dict(sys.modules, {"pwd": owner}), 				patch.object(deploy, "run", side_effect=lambda arguments, **options: calls.append(arguments)):
				deploy.upgrade_authorities(module, "sro-nomad", ["-namespace", "sro"], {})
			upgrader = ["runuser", "-u", "sro", "--", str(module / "sro-authority-upgrade"), "-authority-dir", str(authority)]
			self.assertEqual(calls, [["sro-nomad", "stop", "-namespace", "sro"], upgrader, upgrader + ["-commit"]])

	# ================
	# test_inputs_follow_the_deployed_release
	#
	# A release without a later-added file removes the stale copy, and an
	# upgrade release that lacks the upgrader is refused before anything runs.
	# ================
	def test_inputs_follow_the_deployed_release(self):
		with tempfile.TemporaryDirectory() as directory:
			root = Path(directory)
			staging, module = root / "staging", root / "module"
			names = [name for name in FILES if name != "sro-authority-upgrade"]
			for name in FILES:
				for base in (staging, module):
					path = base / name
					path.parent.mkdir(parents=True, exist_ok=True)
					path.write_text("fixture")
			deploy.copy_inputs(staging, module, names)
			self.assertFalse((module / "sro-authority-upgrade").exists())
			self.assertTrue((module / "agent").exists())
			with patch.object(deploy, "run") as run, self.assertRaisesRegex(ValueError, "sro-authority-upgrade"):
				deploy.deploy({"module": str(module)}, staging, {"commit": "a" * 40, "files": dict.fromkeys(names, "d")}, upgrade=True)
			run.assert_not_called()


if __name__ == "__main__":
	unittest.main()
