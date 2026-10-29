"""
===========================================================================
test_deploy.py - failed preflight cannot restart the fleet or retain its token
===========================================================================
"""

import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from bundle import FILES
import deploy
from gameworld_memory import gameworld_memory_mb


# ================
# write_shards / rendered_job
#
# One enabled shard, and the job Nomad registers for it: the task's
# reservation and the GOMEMLIMIT the job's env formula renders from it.
# ================
def write_shards(module):
	(module / "config").mkdir(parents=True, exist_ok=True)
	(module / "config/shards.json").write_text(json.dumps({"shards": [{"id": "global-official", "enabled": True}]}))


def rendered_job(memory, limit=None):
	task = {"Name": "gameworld", "Env": {"GOMEMLIMIT": limit or str(memory * 3 // 4) + "MiB", "GOGC": "200"},
		"Resources": {"MemoryMB": memory}}
	return {"Job": {"ID": "sro-gameworld-global-official", "TaskGroups": [{"Name": "gameworld", "Tasks": [task]}]}}


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
	# test_staff_webhook_comes_from_the_staff_file
	#
	# The GameWorld's ops alerts page the channel deploy.py already
	# announces staff events through; nothing configured passes nothing.
	# ================
	def test_staff_webhook_comes_from_the_staff_file(self):
		with tempfile.TemporaryDirectory() as directory:
			path = Path(directory) / "discord-webhook"
			self.assertEqual(deploy.staff_environment({}), {})
			config = {"staff_webhook": str(path)}
			self.assertEqual(deploy.staff_environment(config), {})
			path.write_text("\n")
			self.assertEqual(deploy.staff_environment(config), {})
			path.write_text("https://discord.com/api/webhooks/1/abc\n")
			self.assertEqual(deploy.staff_environment(config),
				{"SRO_STAFF_DISCORD_WEBHOOK": "https://discord.com/api/webhooks/1/abc"})
		self.assertIn("SRO_STAFF_DISCORD_WEBHOOK", deploy.DEPLOYMENT_VARIABLES,
			"a shard setting must never name the staff webhook")

	# ================
	# test_shard_settings_refuse_a_malformed_profile
	#
	# The deployment's beta values come only from shard-settings.json (#606):
	# anything but an object of SRO_ names to strings is refused, a name the
	# deployer derives itself included, and a missing file refuses the
	# release before any token is issued rather than deploy native values.
	# ================
	def test_shard_settings_refuse_a_malformed_profile(self):
		with tempfile.TemporaryDirectory() as directory:
			path = Path(directory) / "shard-settings.json"
			for text in ("[]", '{"beta_drop_rate": "20"}', '{"SRO_BETA_DROP_RATE": 20}',
				'{"SRO_STACK_SIZES": "arrow=2500"}', '{"SRO_WEBTRANSPORT_PUBLIC": "on"}', '{"SRO_beta": "on"}'):
				with self.subTest(text=text):
					path.write_text(text)
					with self.assertRaises(ValueError):
						deploy.shard_settings(path)
			path.write_text('{"SRO_BETA_DROP_RATE": "20"}')
			self.assertEqual(deploy.shard_settings(path), {"SRO_BETA_DROP_RATE": "20"})
			missing = Path(directory) / "absent.json"
			with patch.object(deploy, "SHARD_SETTINGS", missing), patch.object(deploy, "run") as run:
				with self.assertRaises(FileNotFoundError):
					deploy.deployer({"nomad_bootstrap": str(path)}, "")
				run.assert_not_called()

	# ================
	# test_webtransport_posture_comes_from_release_config
	#
	# The public WebTransport posture is release configuration: absent keys
	# deploy exactly as before, "on" carries the switch in the environment
	# and the bind and pair as arguments, and every half-posture is refused.
	# ================
	def test_webtransport_posture_comes_from_release_config(self):
		self.assertEqual(deploy.webtransport_deployer({}), ({}, []))
		self.assertEqual(deploy.webtransport_deployer({"webtransport_public": "off"}),
			({"SRO_WEBTRANSPORT_PUBLIC": "off"}, []))
		full = {"webtransport_public": "on", "transport_wt_addr": "[::]:18443",
			"transport_cert_file": "/etc/opensro-tls/fullchain.pem",
			"transport_key_file": "/etc/opensro-tls/privkey.pem"}
		environment, arguments = deploy.webtransport_deployer(full)
		self.assertEqual(environment, {"SRO_WEBTRANSPORT_PUBLIC": "on"})
		self.assertEqual(arguments, ["-transport-wt-addr", "[::]:18443",
			"-transport-cert-file", "/etc/opensro-tls/fullchain.pem",
			"-transport-key-file", "/etc/opensro-tls/privkey.pem",
			"-transport-wt-public-port", "443"])
		# The GameWorld runs unprivileged: a bind it cannot hold is refused
		# here, before any deploy, and so is a bind without a port.
		for refused in ("[::]:443", "[::]:1023"):
			with self.assertRaisesRegex(ValueError, "bind port must be 1024-65535"):
				deploy.webtransport_deployer(dict(full, transport_wt_addr=refused))
		# The redirect delivers to the interface address, IPv4 and IPv6: any
		# host but the dual-stack wildcard misses players silently.
		for refused in ("127.0.0.1:18443", "203.0.113.7:18443", "0.0.0.0:18443", "[2001:db8::1]:18443"):
			with self.assertRaisesRegex(ValueError, "bind host must be \\[::\\]"):
				deploy.webtransport_deployer(dict(full, transport_wt_addr=refused))
		for malformed in ("203.0.113.7", ":18443", "[::]:port"):
			with self.assertRaisesRegex(ValueError, "must be host:port"):
				deploy.webtransport_deployer(dict(full, transport_wt_addr=malformed))
		self.assertEqual(deploy.wt_bind("[::]:18443"), ("[::]", 18443))
		with self.assertRaisesRegex(ValueError, "webtransport_public"):
			deploy.webtransport_deployer({"webtransport_public": "yes"})
		incomplete = dict(full)
		del incomplete["transport_wt_addr"]
		with self.assertRaisesRegex(ValueError, "transport_wt_addr"):
			deploy.webtransport_deployer(incomplete)
		with self.assertRaisesRegex(ValueError, "requires webtransport_public on"):
			deploy.webtransport_deployer({"transport_wt_addr": "203.0.113.7:443"})
		with self.assertRaisesRegex(ValueError, "requires webtransport_public on"):
			deploy.webtransport_deployer({"webtransport_public": "off",
				"transport_cert_file": "/etc/opensro-tls/fullchain.pem"})

	# ================
	# test_wt_arguments_the_staged_binary_lacks_are_dropped
	#
	# A revert stages the retained release's sro-nomad, built before the
	# WebTransport posture flags existed ("flag provided but not defined"
	# stopped one). The posture is all-or-nothing, so a binary missing
	# any posture flag loses every posture flag with its value and keeps
	# the rest; a binary that defines them all keeps them all; a
	# deployment without posture arguments never asks the binary; a
	# binary that will not report its flags refuses instead of guessing.
	# ================
	def test_wt_arguments_the_staged_binary_lacks_are_dropped(self):
		arguments = ["-namespace", "sro", "-transport-wt-addr", "[::]:18443",
			"-transport-cert-file", "/etc/opensro-tls/fullchain.pem",
			"-transport-key-file", "/etc/opensro-tls/privkey.pem",
			"-transport-wt-public-port", "443"]
		old_usage = "Usage of validate:\n  -namespace string\n\tNomad namespace\n  -module-root string\n"
		new_usage = old_usage + "".join(f"  {flag} value\n" for flag in deploy.WT_ARGUMENT_FLAGS)
		partial_usage = old_usage + "  -transport-wt-addr value\n  -transport-wt-public-port value\n"

		def probe(usage):
			def run(command, **kwargs):
				raise subprocess.CalledProcessError(2, command, stderr=usage)
			return run

		with patch.object(deploy, "run", side_effect=probe(old_usage)):
			self.assertEqual(deploy.without_undefined_wt_arguments("sro-nomad", arguments),
				["-namespace", "sro"])
		with patch.object(deploy, "run", side_effect=probe(partial_usage)):
			self.assertEqual(deploy.without_undefined_wt_arguments("sro-nomad", arguments),
				["-namespace", "sro"])
		with patch.object(deploy, "run", side_effect=probe(new_usage)):
			self.assertEqual(deploy.without_undefined_wt_arguments("sro-nomad", arguments), arguments)
		with patch.object(deploy, "run") as run:
			self.assertEqual(deploy.without_undefined_wt_arguments("sro-nomad", ["-namespace", "sro"]),
				["-namespace", "sro"])
			run.assert_not_called()
		with patch.object(deploy, "run", side_effect=probe("")):
			with self.assertRaisesRegex(RuntimeError, "did not report its flags"):
				deploy.without_undefined_wt_arguments("sro-nomad", arguments)

	# ================
	# test_public_webtransport_waits_for_the_host
	#
	# A public deploy stops before any token is issued while the host lacks
	# the redirect (quic_host.problems names the gap); a ready host, or a
	# deploy with the switch off, goes ahead without asking the host.
	# ================
	def test_public_webtransport_waits_for_the_host(self):
		on = {"webtransport_public": "on", "transport_wt_addr": "[::]:18443"}
		with patch.object(deploy.quic_host, "problems", return_value=["no live nftables redirect"]) as asked:
			with self.assertRaisesRegex(RuntimeError, "host is not ready: no live nftables redirect"):
				deploy.require_quic_host(on, {"SRO_WEBTRANSPORT_PUBLIC": "on"})
		asked.assert_called_once_with(18443)
		with patch.object(deploy.quic_host, "problems", return_value=[]):
			deploy.require_quic_host(on, {"SRO_WEBTRANSPORT_PUBLIC": "on"})
		with patch.object(deploy.quic_host, "problems") as asked:
			deploy.require_quic_host({}, {})
			deploy.require_quic_host({"webtransport_public": "off"}, {"SRO_WEBTRANSPORT_PUBLIC": "off"})
		asked.assert_not_called()
		full = dict(on, transport_cert_file="/etc/opensro-tls/fullchain.pem",
			transport_key_file="/etc/opensro-tls/privkey.pem", nomad_bootstrap="/nonexistent")
		with patch.object(deploy, "shard_settings", return_value={}), patch.object(deploy, "run") as run, 				patch.object(deploy.quic_host, "problems", return_value=["gap"]):
			with self.assertRaisesRegex(RuntimeError, "host is not ready"):
				deploy.deployer(full, "")
			run.assert_not_called()

	# ================
	# test_public_webtransport_pair_must_be_readable_by_the_task_user
	#
	# The deploy runs as root; the GameWorld as deploy.TASK_USER. A pair only
	# root can read is refused before any token, naming the file and the fix.
	# ================
	def test_public_webtransport_pair_must_be_readable_by_the_task_user(self):
		config = {"transport_cert_file": "/etc/opensro-tls/fullchain.pem",
			"transport_key_file": " /etc/opensro-tls/privkey.pem "}
		on = {"SRO_WEBTRANSPORT_PUBLIC": "on"}
		calls = []
		def readable(command, check):
			calls.append(command)
			return subprocess.CompletedProcess(command, 0)
		deploy.require_pair_readable(config, on, run_as=readable)
		self.assertEqual(calls, [
			["runuser", "-u", deploy.TASK_USER, "--", "test", "-r", "/etc/opensro-tls/fullchain.pem"],
			["runuser", "-u", deploy.TASK_USER, "--", "test", "-r", "/etc/opensro-tls/privkey.pem"]])
		def key_unreadable(command, check):
			return subprocess.CompletedProcess(command, 1 if command[-1].endswith("privkey.pem") else 0)
		with self.assertRaisesRegex(RuntimeError, "transport_key_file /etc/opensro-tls/privkey.pem is not readable by sro.*0640"):
			deploy.require_pair_readable(config, on, run_as=key_unreadable)
		# Off or absent: nothing to read, nothing asked.
		calls.clear()
		deploy.require_pair_readable({}, {}, run_as=readable)
		deploy.require_pair_readable(config, {"SRO_WEBTRANSPORT_PUBLIC": "off"}, run_as=readable)
		self.assertEqual(calls, [])

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
				write_shards(module)
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
					"gameworld_memory_mb": 3072, "public_webhook": "public-fixture", "staff_webhook": "staff-fixture",
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
					if arguments[:3] == ["nomad", "job", "inspect"]:
						return SimpleNamespace(stdout=json.dumps(rendered_job(3072)))
					if arguments[0].endswith("sro-nomad"):
						self.assertEqual(options["env"]["NOMAD_TOKEN"], "scoped-fixture")
						self.assertEqual(options["env"]["SRO_STACK_SIZES"], stack_sizes or "")
						for name, value in deploy.shard_settings().items():
							self.assertEqual(options["env"][name], value)
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
					inspected = [call for call in calls if call[:3] == ["nomad", "job", "inspect"]]
					self.assertEqual(inspected, [["nomad", "job", "inspect", "-namespace", "sro", "-json", "sro-gameworld-global-official"]]
						if failure is None else [])

	# ================
	# test_gameworld_memory_never_falls_below_the_job_default
	#
	# The host config may raise the reservation, never lower it: the 1024
	# left behind after #1946 kept the live soft limit at 768MiB (#2027).
	# A low value refuses before the backup runs or anything is touched.
	# ================
	def test_gameworld_memory_never_falls_below_the_job_default(self):
		self.assertEqual(gameworld_memory_mb({}), 3072)
		self.assertEqual(gameworld_memory_mb({"gameworld_memory_mb": 4096}), 4096)
		for value in (1024, 3071, "3072", True, 3072.0):
			with self.subTest(value=value), self.assertRaisesRegex(ValueError, "gameworld_memory_mb"):
				gameworld_memory_mb({"gameworld_memory_mb": value})
		with patch.object(deploy, "run") as run, self.assertRaisesRegex(ValueError, "gameworld_memory_mb"):
			deploy.deploy({"gameworld_memory_mb": 1024, "module": "/nonexistent"}, None, {})
		run.assert_not_called()

	# ================
	# test_validation_runs_before_any_stop_and_only_credentials_stop_first
	#
	# sro-nomad refuses to replace a credential variable while its job is
	# registered (2026-10-11); validate -assume-stopped reports it instead.
	# Every deployment validates before anything stops. A changed credential
	# then stops the fleet (maintenance only) and validates again; a plain
	# release with one is refused, and any other refusal stops nothing.
	# ================
	def test_validation_runs_before_any_stop_and_only_credentials_stop_first(self):
		for maintenance, credential, refusal in ((True, True, False), (False, True, False), (True, False, False), (True, False, True)):
			with self.subTest(maintenance=maintenance, credential=credential, refusal=refusal), \
				tempfile.TemporaryDirectory() as directory:
				root = Path(directory)
				staging, module = root / "staging", root / "module"
				(module / ".state/cluster").mkdir(parents=True)
				write_shards(module)
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
					"gameworld_memory_mb": 3072, "public_webhook": "public-fixture",
				}
				manifest = {"commit": "a" * 40, "files": {name: "digest" for name in FILES}}
				calls, registered = [], [True]

				# ================
				# execute
				#
				# A fake sro-nomad with the real refusal: a plain validate
				# refuses a changed credential until stop has run.
				# ================
				def execute(arguments, **_options):
					calls.append(arguments)
					if arguments[:2] == ["nomad", "version"]:
						return SimpleNamespace(stdout="Nomad v2.0.7\n")
					if arguments[:4] == ["nomad", "acl", "token", "create"]:
						return SimpleNamespace(stdout=json.dumps({"SecretID": "scoped", "AccessorID": "accessor"}))
					if arguments[:3] == ["nomad", "job", "inspect"]:
						return SimpleNamespace(stdout=json.dumps(rendered_job(3072)))
					if arguments[0].endswith("sro-nomad") and arguments[1] == "stop":
						registered[0] = False
					if arguments[0].endswith("sro-nomad") and arguments[1] == "validate":
						if refusal:
							raise subprocess.CalledProcessError(1, arguments)
						if credential and registered[0]:
							if "-assume-stopped" not in arguments:
								raise subprocess.CalledProcessError(1, arguments)
							return SimpleNamespace(stdout="requires-stop sro-gameworld-x nomad/jobs/sro-gameworld-x/gameworld/gameworld\n")
					return SimpleNamespace(stdout="")

				with patch.object(deploy, "run", side_effect=execute), patch.object(deploy.shutil, "chown"), \
					patch.object(deploy.server_data, "choose", return_value=None), \
					patch.object(deploy, "warning", return_value=False):
					if refusal or (credential and not maintenance):
						with self.assertRaises((RuntimeError, subprocess.CalledProcessError)):
							deploy.deploy(config, staging, manifest, notice=False, maintenance=maintenance)
					else:
						deploy.deploy(config, staging, manifest, notice=False, maintenance=maintenance)
				verbs = [call[1] for call in calls if call[0].endswith("sro-nomad")]
				first = next(call for call in calls if call[0].endswith("sro-nomad") and call[1] == "validate")
				self.assertIn("-assume-stopped", first)
				self.assertEqual(calls[-1], ["nomad", "acl", "token", "delete", "accessor"])
				if refusal or (credential and not maintenance):
					self.assertNotIn("stop", verbs)
					self.assertNotIn("deploy", verbs)
				elif credential:
					self.assertEqual(verbs[:3], ["validate", "stop", "validate"])
					self.assertEqual(verbs[-2:], ["deploy", "status"])
				else:
					self.assertNotIn("stop", verbs)
					self.assertEqual(verbs[-2:], ["deploy", "status"])

	# ================
	# test_a_release_older_than_assume_stopped_still_deploys
	#
	# A revert target built before validate learned -assume-stopped rejects
	# the flag: a maintenance deployment then stops before validating, and a
	# plain one validates before anything stops.
	# ================
	def test_a_release_older_than_assume_stopped_still_deploys(self):
		for maintenance in (True, False):
			with self.subTest(maintenance=maintenance), tempfile.TemporaryDirectory() as directory:
				root = Path(directory)
				staging, module = root / "staging", root / "module"
				(module / ".state/cluster").mkdir(parents=True)
				write_shards(module)
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
					"gameworld_memory_mb": 3072, "public_webhook": "public-fixture",
				}
				manifest = {"commit": "a" * 40, "files": {name: "digest" for name in FILES}}
				calls = []

				# ================
				# execute
				# ================
				def execute(arguments, **_options):
					calls.append(arguments)
					if arguments[:2] == ["nomad", "version"]:
						return SimpleNamespace(stdout="Nomad v2.0.7")
					if arguments[:4] == ["nomad", "acl", "token", "create"]:
						return SimpleNamespace(stdout=json.dumps({"SecretID": "scoped", "AccessorID": "accessor"}))
					if arguments[:3] == ["nomad", "job", "inspect"]:
						return SimpleNamespace(stdout=json.dumps(rendered_job(3072)))
					if arguments[0].endswith("sro-nomad") and "-assume-stopped" in arguments:
						raise subprocess.CalledProcessError(2, arguments, stderr="flag provided but not defined: -assume-stopped")
					return SimpleNamespace(stdout="")

				with patch.object(deploy, "run", side_effect=execute), patch.object(deploy.shutil, "chown"), \
					patch.object(deploy.server_data, "choose", return_value=None), \
					patch.object(deploy, "warning", return_value=False):
					deploy.deploy(config, staging, manifest, notice=False, maintenance=maintenance)
				verbs = [call[1] for call in calls if call[0].endswith("sro-nomad")]
				if maintenance:
					self.assertEqual(verbs, ["validate", "stop", "validate", "deploy", "status"])
				else:
					self.assertEqual(verbs, ["validate", "validate", "deploy", "status"])

	# ================
	# test_a_validate_refusal_keeps_its_reason
	#
	# materialize captures validate's output, so a refusal's reason must be
	# carried in the raised error (masked) or it is lost, as on 2026-10-11.
	# ================
	def test_a_validate_refusal_keeps_its_reason(self):
		token = "f" * 64
		refusal = subprocess.CalledProcessError(1, ["sro-nomad", "validate"],
			stderr="sro-nomad validate: job sro-agent spec invalid near token " + token)
		with tempfile.TemporaryDirectory() as directory, patch.object(deploy, "run", side_effect=refusal):
			config = {"game_data": str(Path(directory) / "server.srogz")}
			for assume_stopped in (True, False):
				with self.subTest(assume_stopped=assume_stopped):
					with self.assertRaises(RuntimeError) as raised:
						deploy.materialize(config, Path(directory), "sro-nomad", [], {}, assume_stopped=assume_stopped)
					self.assertIn("job sro-agent spec invalid", str(raised.exception))
					self.assertNotIn(token, str(raised.exception))

	# ================
	# test_a_refused_restore_stops_nothing
	#
	# A maintenance revert checks its journaled backups before the stop that
	# now precedes validation: a missing backup refuses with the fleet still
	# running, and the scoped token is still revoked.
	# ================
	def test_a_refused_restore_stops_nothing(self):
		with tempfile.TemporaryDirectory() as directory:
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
				"gameworld_memory_mb": 3072, "public_webhook": "public-fixture",
			}
			manifest = {"commit": "a" * 40, "files": {name: "digest" for name in FILES}}
			calls = []

			# ================
			# execute
			# ================
			def execute(arguments, **_options):
				calls.append(arguments)
				if arguments[:2] == ["nomad", "version"]:
					return SimpleNamespace(stdout="Nomad v2.0.7\n")
				if arguments[:4] == ["nomad", "acl", "token", "create"]:
					return SimpleNamespace(stdout=json.dumps({"SecretID": "scoped", "AccessorID": "accessor"}))
				return SimpleNamespace(stdout="")

			with patch.object(deploy, "run", side_effect=execute), patch.object(deploy.shutil, "chown"), \
				patch.object(deploy.server_data, "choose", return_value=None), \
				patch.object(deploy, "warning", return_value=False):
				with self.assertRaisesRegex(RuntimeError, "no journaled upgrade backup"):
					deploy.deploy(config, staging, manifest, notice=False, restore={"global-official": None})
			verbs = [call[1] for call in calls if call[0].endswith("sro-nomad")]
			self.assertNotIn("stop", verbs)
			self.assertEqual(calls[-1], ["nomad", "acl", "token", "delete", "accessor"])

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


# ================
# RehearsalTests
#
# The staging-time rehearsal (2026-10-11: F2's maintenance publish failed
# inside its window): the staged sro-nomad validates against production as
# if the fleet were stopped, from a throwaway module copy. Production is
# never written, the token is always revoked, the copy always removed.
# ================
class RehearsalTests(unittest.TestCase):
	# ================
	# fixture
	# ================
	def fixture(self, root):
		staging, module = root / "staging", root / "production" / "module"
		(module / ".state/cluster/releases").mkdir(parents=True)
		(module / ".state/cluster/accounts.json").write_text("production-accounts")
		(module / ".state/cluster/releases/binary").write_text("released")
		(module / "config").mkdir()
		(module / "config/shards.json").write_text("{}")
		(module / "sro-nomad").write_text("production sro-nomad")
		for name in FILES:
			path = staging / name
			path.parent.mkdir(parents=True, exist_ok=True)
			path.write_text("staged " + name)
		bootstrap = root / "bootstrap.json"
		bootstrap.write_text(json.dumps({"SecretID": "management-fixture"}))
		config = {
			"module": str(module), "game_data": str(root / "server.srogz"),
			"nomad_version": "2.0.7", "nomad_bootstrap": str(bootstrap),
			"origin": "https://example.test", "agent_memory_mb": 256, "gameworld_memory_mb": 3072,
		}
		manifest = {"commit": "a" * 40, "files": {name: "digest" for name in FILES}}
		return staging, module, config, manifest

	# ================
	# test_rehearsal_validates_a_copy_assuming_a_stopped_fleet
	# ================
	def test_rehearsal_validates_a_copy_assuming_a_stopped_fleet(self):
		with tempfile.TemporaryDirectory() as directory:
			root = Path(directory)
			staging, module, config, manifest = self.fixture(root)
			before = {path: path.read_bytes() for path in module.rglob("*") if path.is_file()}
			calls, copies = [], []

			# ================
			# execute
			# ================
			def execute(arguments, **options):
				calls.append(arguments)
				if arguments[:2] == ["nomad", "version"]:
					return SimpleNamespace(stdout="Nomad v2.0.7\n")
				if arguments[:4] == ["nomad", "acl", "token", "create"]:
					return SimpleNamespace(stdout=json.dumps({"SecretID": "scoped", "AccessorID": "accessor"}))
				if arguments[0].endswith("sro-provision-identity"):
					copy = Path(options["cwd"])
					copies.append(copy)
					# The provisioner writes into the copy, as at a deployment.
					(copy / ".state/cluster/public-api-token").write_text("new token")
				if arguments[0].endswith("sro-nomad"):
					copy = Path(options["cwd"])
					self.assertEqual(arguments[1:3], ["validate", "-assume-stopped"])
					self.assertEqual(options["env"]["NOMAD_TOKEN"], "scoped")
					# The staged binary runs over the production state, without its releases.
					self.assertEqual((copy / "sro-nomad").read_text(), "staged sro-nomad")
					self.assertEqual((copy / ".state/cluster/accounts.json").read_text(), "production-accounts")
					self.assertFalse((copy / ".state/cluster/releases").exists())
					return SimpleNamespace(stdout="Validated 2 Nomad Variable input(s) without mutation\n"
						"requires-stop sro-gameworld-1 nomad/jobs/sro-gameworld-1/gameworld/gameworld\n")
				return SimpleNamespace(stdout="")

			with patch.object(deploy, "run", side_effect=execute):
				stops = deploy.rehearse(config, staging, manifest)
			self.assertEqual(stops, [("sro-gameworld-1", "nomad/jobs/sro-gameworld-1/gameworld/gameworld")])
			self.assertEqual(calls[-1], ["nomad", "acl", "token", "delete", "accessor"])
			self.assertEqual(len(copies), 1)
			self.assertFalse(copies[0].exists())
			after = {path: path.read_bytes() for path in module.rglob("*") if path.is_file()}
			self.assertEqual(after, before)

	# ================
	# test_rehearsal_validates_against_the_production_shard_authorities
	#
	# F5's staging refusal (2026-10-12): sro-nomad validate requires each
	# shard's authority database under the module it runs in, and the copy
	# had none. The rehearsal links the production shards in, without copying
	# player data, and its cleanup leaves them in place.
	# ================
	@unittest.skipIf(os.name == "nt", "the rehearsal links shards with a POSIX directory symlink")
	def test_rehearsal_validates_against_the_production_shard_authorities(self):
		with tempfile.TemporaryDirectory() as directory:
			root = Path(directory)
			staging, module, config, manifest = self.fixture(root)
			database = module / ".state/shards/global-official/authority/state.db"
			database.parent.mkdir(parents=True)
			database.write_bytes(b"production players")
			copies = []

			# ================
			# execute
			# ================
			def execute(arguments, **options):
				if arguments[:2] == ["nomad", "version"]:
					return SimpleNamespace(stdout="Nomad v2.0.7\n")
				if arguments[:4] == ["nomad", "acl", "token", "create"]:
					return SimpleNamespace(stdout=json.dumps({"SecretID": "scoped", "AccessorID": "accessor"}))
				if arguments[0].endswith("sro-nomad"):
					copy = Path(options["cwd"])
					copies.append(copy)
					# As deployment.go: the authority must be a regular file under the cwd's module.
					required = copy / ".state/shards/global-official/authority/state.db"
					if not required.is_file():
						raise subprocess.CalledProcessError(1, arguments,
							stderr="sro-nomad validate: required file " + str(required) + ": no such file or directory")
					return SimpleNamespace(stdout="Validated 2 Nomad Variable input(s) without mutation\n")
				return SimpleNamespace(stdout="")

			with patch.object(deploy, "run", side_effect=execute):
				self.assertEqual(deploy.rehearse(config, staging, manifest), [])
			self.assertEqual(len(copies), 1)
			self.assertFalse(copies[0].exists())
			self.assertEqual(database.read_bytes(), b"production players")

	# ================
	# test_a_refused_rehearsal_fails_staging_masked_and_cleans_up
	# ================
	def test_a_refused_rehearsal_fails_staging_masked_and_cleans_up(self):
		with tempfile.TemporaryDirectory() as directory:
			root = Path(directory)
			staging, module, config, manifest = self.fixture(root)
			calls, copies = [], []
			secret = "s" * 40

			# ================
			# execute
			# ================
			def execute(arguments, **options):
				calls.append(arguments)
				if arguments[:2] == ["nomad", "version"]:
					return SimpleNamespace(stdout="Nomad v2.0.7\n")
				if arguments[:4] == ["nomad", "acl", "token", "create"]:
					return SimpleNamespace(stdout=json.dumps({"SecretID": "scoped", "AccessorID": "accessor"}))
				if arguments[0].endswith("sro-provision-identity"):
					copies.append(Path(options["cwd"]))
				if arguments[0].endswith("sro-nomad"):
					raise subprocess.CalledProcessError(1, arguments, stderr="sro-nomad validate: bad origin " + secret)
				return SimpleNamespace(stdout="")

			with patch.object(deploy, "run", side_effect=execute):
				with self.assertRaisesRegex(RuntimeError, "staging rehearsal: .*bad origin <masked>") as raised:
					deploy.rehearse(config, staging, manifest)
			self.assertNotIn(secret, str(raised.exception))
			self.assertEqual(calls[-1], ["nomad", "acl", "token", "delete", "accessor"])
			self.assertFalse(copies[0].exists())

	# ================
	# test_no_production_module_has_nothing_to_rehearse
	# ================
	def test_no_production_module_has_nothing_to_rehearse(self):
		with patch.object(deploy, "run") as execute:
			self.assertEqual(deploy.rehearse({}, Path("unused"), {"files": {}}), [])
			self.assertEqual(deploy.rehearse({"module": "/nonexistent-module"}, Path("unused"), {"files": {}}), [])
			execute.assert_not_called()

