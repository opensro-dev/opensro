"""
===========================================================================
test_deploy.py - failed preflight cannot restart the fleet or retain its token
===========================================================================
"""

import hashlib
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
		for failure, stack_sizes in ((None, None), (None, ""), (None, "potion=2000,elixir=50"),
			("validate", "potion=2000"), ("notice", "potion=2000"), ("deploy", "potion=2000")):
			with self.subTest(failure=failure, stack_sizes=stack_sizes), tempfile.TemporaryDirectory() as directory:
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
				if stack_sizes is not None:
					config["stack_sizes"] = stack_sizes
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
						self.assertEqual(options["env"]["SRO_STACK_SIZES"], stack_sizes or "")
						if arguments[1] == failure:
							raise subprocess.CalledProcessError(1, arguments)
					return SimpleNamespace(stdout="")

				with patch.object(deploy, "run", side_effect=execute), patch.object(deploy.shutil, "chown"), \
					patch.object(deploy.server_data, "choose", return_value=None), \
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
	# test_invalid_stack_sizes_type_changes_nothing
	# ================
	def test_invalid_stack_sizes_type_changes_nothing(self):
		for value in (None, 2000, {"potion": 2000}):
			with self.subTest(value=value), patch.object(deploy, "run") as run:
				with self.assertRaisesRegex(ValueError, "stack_sizes must be a string"):
					deploy.deploy({"stack_sizes": value}, None, {})
				run.assert_not_called()

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

	# ================
	# test_refused_game_data_changes_nothing
	#
	# The release's own game-data verdict comes before the backup, the inputs
	# and the notice: a refusal leaves the running server as it is.
	# ================
	def test_refused_game_data_changes_nothing(self):
		with tempfile.TemporaryDirectory() as directory:
			module = Path(directory)
			calls = []

			# ================
			# execute
			# ================
			def execute(arguments, **options):
				calls.append(arguments)
				return SimpleNamespace(stdout="Nomad v2.0.7\n")

			refusal = RuntimeError("this release refuses the installed server game data")
			with patch.object(deploy, "run", side_effect=execute), \
				patch.object(deploy.server_data, "choose", side_effect=refusal), \
				patch.object(deploy, "copy_inputs") as copied, patch.object(deploy, "announce") as announcement, \
				self.assertRaisesRegex(RuntimeError, "refuses the installed server game data"):
				deploy.deploy({"module": str(module), "nomad_version": "2.0.7"}, module,
					{"commit": "a" * 40, "files": dict.fromkeys(FILES, "d")})
			self.assertEqual(calls, [["nomad", "version"]])
			copied.assert_not_called()
			announcement.assert_not_called()

	# ================
	# test_nobody_to_warn_when_no_shard_runs
	#
	# A failed notice with every enabled shard's GameWorld down is a restart
	# with nobody to warn; with one running, it still refuses the restart.
	# ================
	def test_nobody_to_warn_when_no_shard_runs(self):
		with tempfile.TemporaryDirectory() as directory:
			module = Path(directory)
			(module / "config").mkdir()
			(module / "config/shards.json").write_text(json.dumps({"shards": [
				{"id": "global-official", "enabled": True, "controlUrl": "http://127.0.0.1:8791"},
				{"id": "test", "enabled": False, "controlUrl": "http://127.0.0.1:8792"}]}))
			failed = SimpleNamespace(returncode=1, stdout="", stderr="connection refused")
			with patch.object(deploy.subprocess, "run", return_value=failed):
				with patch.object(deploy, "shard_listening", return_value=False) as listening:
					self.assertFalse(deploy.warning({}, module, "sro-nomad"))
					self.assertEqual(listening.call_count, 1, "only enabled shards are probed")
				with patch.object(deploy, "shard_listening", return_value=True), \
					self.assertRaisesRegex(RuntimeError, "restart refused"):
					deploy.warning({}, module, "sro-nomad")
			delivered = SimpleNamespace(returncode=0, stdout="", stderr="")
			with patch.object(deploy.subprocess, "run", return_value=delivered):
				self.assertTrue(deploy.warning({}, module, "sro-nomad"))

	# ================
	# test_shard_listening_reads_the_loopback_control_port
	# ================
	def test_shard_listening_reads_the_loopback_control_port(self):
		import socket
		with socket.socket() as listener:
			listener.bind(("127.0.0.1", 0))
			listener.listen()
			port = listener.getsockname()[1]
			self.assertTrue(deploy.shard_listening({"controlUrl": f"http://127.0.0.1:{port}"}))
		self.assertFalse(deploy.shard_listening({"controlUrl": f"http://127.0.0.1:{port}"}))
		with self.assertRaisesRegex(RuntimeError, "loopback"):
			deploy.shard_listening({"controlUrl": "http://10.0.0.1:8791"})


# ================
# MaintenanceDeployTests
#
# The host side of a maintenance release on a real module tree: the upgrade
# journals each shard's backup, the revert puts those backups back, and the
# gate file admits only the configured accounts. Only the commands (run),
# the owner lookup and file ownership are injected.
# ================
class MaintenanceDeployTests(unittest.TestCase):
	# ================
	# setUp
	# ================
	def setUp(self):
		directory = tempfile.TemporaryDirectory()
		self.addCleanup(directory.cleanup)
		self.module = Path(directory.name)
		(self.module / "config").mkdir()
		(self.module / "config/shards.json").write_text(json.dumps({"shards": [
			{"id": "global-official", "enabled": True}, {"id": "test", "enabled": False}]}))
		self.authority = self.module / ".state/shards/global-official/authority"
		self.authority.mkdir(parents=True)
		(self.authority / "state.db").write_bytes(b"schema 17")
		self.calls = []
		owner = SimpleNamespace(getpwuid=lambda uid: SimpleNamespace(pw_name="sro"))
		for patcher in (patch.dict(sys.modules, {"pwd": owner}), patch.object(deploy, "run", side_effect=self.fake_run),
			patch.object(deploy.os, "chown", create=True)):
			patcher.start()
			self.addCleanup(patcher.stop)

	# ================
	# fake_run
	#
	# The upgrade's commit keeps a backup of the old database and reports it,
	# as sro-authority-upgrade does, then leaves an upgraded database and WAL.
	# ================
	def fake_run(self, arguments, **_options):
		self.calls.append(arguments)
		if arguments[-1] != "-commit":
			return SimpleNamespace(stdout="")
		backup = self.authority / "state.before-upgrade-1.db"
		backup.write_bytes((self.authority / "state.db").read_bytes())
		(self.authority / "state.db").write_bytes(b"schema 20")
		(self.authority / "state.db-wal").write_bytes(b"wal")
		return SimpleNamespace(stdout="Upgrade backup path: " + str(backup) + "\nAuthority upgraded.\n")

	# ================
	# upgrade
	# ================
	def upgrade(self):
		journal = []
		deploy.upgrade_authorities(self.module, "sro-nomad", ["-namespace", "sro"], {},
			lambda shard, row: journal.append((shard, row)))
		return journal

	# ================
	# test_the_upgrade_journals_each_backup_before_and_after_its_commit
	# ================
	def test_the_upgrade_journals_each_backup_before_and_after_its_commit(self):
		journal = self.upgrade()
		digest = hashlib.sha256(b"schema 17").hexdigest()
		self.assertEqual(journal, [("global-official", None),
			("global-official", {"backup": "state.before-upgrade-1.db", "sha256": digest})])
		self.assertEqual(self.calls[0], ["sro-nomad", "stop", "-namespace", "sro"])

	# ================
	# test_a_backup_outside_the_authority_is_refused
	# ================
	def test_a_backup_outside_the_authority_is_refused(self):
		output = "Upgrade backup path: " + str(self.module / "state.before-upgrade-1.db") + "\n"
		with self.assertRaisesRegex(RuntimeError, "named no backup"):
			deploy.upgrade_backup(self.authority, output, "global-official")
		with self.assertRaisesRegex(RuntimeError, "named no backup"):
			deploy.upgrade_backup(self.authority, "Authority already in the current format.\n", "global-official")

	# ================
	# test_restore_puts_the_backup_back_and_keeps_the_failed_state
	# ================
	def test_restore_puts_the_backup_back_and_keeps_the_failed_state(self):
		_, (_, row) = self.upgrade()
		self.calls.clear()
		deploy.restore_authorities(self.module, "sro-nomad", ["-namespace", "sro"], {}, {"global-official": row})
		self.assertEqual(self.calls, [["sro-nomad", "stop", "-namespace", "sro"]])
		self.assertEqual((self.authority / "state.db").read_bytes(), b"schema 17")
		self.assertFalse((self.authority / "state.db-wal").exists())
		failed = sorted(path.name for path in self.authority.glob("state.failed-*"))
		self.assertEqual(len(failed), 2)
		self.assertEqual((self.authority / failed[0]).read_bytes(), b"schema 20")
		self.assertTrue((self.authority / "state.before-upgrade-1.db").exists())
		# A shard whose database went missing still gets its backup back.
		(self.authority / "state.db").unlink()
		deploy.restore_authorities(self.module, "sro-nomad", [], {}, {"global-official": row})
		self.assertEqual((self.authority / "state.db").read_bytes(), b"schema 17")

	# ================
	# test_restore_refuses_before_stopping_anything
	#
	# A changed backup or a shard journaled without one stops nothing and
	# moves nothing.
	# ================
	def test_restore_refuses_before_stopping_anything(self):
		_, (_, row) = self.upgrade()
		self.calls.clear()
		for authorities in ({"global-official": dict(row, sha256="0" * 64)}, {"global-official": None},
			{"global-official": dict(row, backup="state.before-upgrade-9.db")}):
			with self.assertRaisesRegex(RuntimeError, "restore"):
				deploy.restore_authorities(self.module, "sro-nomad", [], {}, authorities)
		self.assertEqual(self.calls, [])
		self.assertEqual((self.authority / "state.db").read_bytes(), b"schema 20")

	# ================
	# test_the_gate_lists_the_configured_accounts
	# ================
	def test_the_gate_lists_the_configured_accounts(self):
		config = {"module": str(self.module), "maintenance_accounts": ["release-probe"]}
		(self.module / ".state/cluster").mkdir(parents=True)
		with patch.object(deploy, "own_gate") as owned:
			deploy.shut_gate(config)
		path = self.module / deploy.MAINTENANCE_GATE
		self.assertEqual(json.loads(path.read_text()), {"accounts": ["release-probe"]})
		owned.assert_called_once()
		deploy.lift_gate(config)
		self.assertFalse(path.exists())
		deploy.lift_gate(config)
		for accounts in (None, [], [""], [" probe"], "probe"):
			with self.assertRaisesRegex(ValueError, "maintenance_accounts"):
				deploy.gate_accounts({"maintenance_accounts": accounts})


if __name__ == "__main__":
	unittest.main()
