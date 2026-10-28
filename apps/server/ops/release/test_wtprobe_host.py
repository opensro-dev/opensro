"""
===========================================================================

test_wtprobe_host.py - the game host's half of the off-host probe (#1984)

The push key, the monitor config, apply/remove and the readiness check,
with every host command and file write captured: nothing here creates an
account, writes /etc or touches systemd.

===========================================================================
"""

import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import wtprobe_host

KEY = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleExampleExampleExampleExampleExamp runner@vps0"


class WTProbeHostTest(unittest.TestCase):
	# ================
	# test_the_push_key_can_only_receive_a_verdict
	# ================
	def test_the_push_key_can_only_receive_a_verdict(self):
		keys = wtprobe_host.push_keys(KEY + "\n", "/usr/local/lib/opensro-release-controls/v1")
		self.assertEqual(keys.count("\n"), 1)
		self.assertTrue(keys.startswith('restrict,command="/usr/bin/python3 /usr/local/lib/opensro-release-controls/v1/wtprobe_verdict.py --receive '
			+ wtprobe_host.VERDICT.as_posix() + '" ssh-ed25519 '))
		self.assertNotIn("runner@vps0", keys)
		for bad in ("", "ssh-rsa AAAAB3Nza runner", "ssh-ed25519"):
			with self.subTest(key=bad), self.assertRaises(ValueError):
				wtprobe_host.push_keys(bad, "/modules")

	# ================
	# test_the_monitor_config_shares_the_fleet_webhook_and_journal
	# ================
	def test_the_monitor_config_shares_the_fleet_webhook_and_journal(self):
		settings = wtprobe_host.monitor_settings("/var/lib/opensro-release/public/production.json")
		self.assertEqual(settings["kind"], "wtprobe")
		self.assertEqual(settings["verdict"], wtprobe_host.VERDICT.as_posix())
		self.assertEqual(settings["webhook"], "/etc/opensro-release/monitor-webhook")
		self.assertEqual(settings["production_state"], "/var/lib/opensro-release/public/production.json")
		self.assertTrue(settings["state"].endswith("/public/health/wtprobe.json"))

	# ================
	# test_apply_installs_the_key_config_and_timer
	# ================
	def test_apply_installs_the_key_config_and_timer(self):
		ran = []
		with tempfile.TemporaryDirectory() as directory:
			root = Path(directory)
			modules = root / "modules"
			modules.mkdir()
			for name in wtprobe_host.UNIT_NAMES:
				(modules / name).write_text("ExecStart=@RELEASE_MODULES@/monitor.py\n")
			config = root / "config.json"
			config.write_text(json.dumps({"production_state": "/state/production.json"}))
			webhook = root / "webhook"
			webhook.write_text("https://discord.invalid/hook")
			with patch.object(wtprobe_host, "ROOT", root), patch.object(wtprobe_host, "VERDICT_DIR", root / "wtprobe"), \
					patch.object(wtprobe_host, "CONFIG", config), patch.object(wtprobe_host, "WEBHOOK", webhook), \
					patch.object(wtprobe_host, "ensure_user"), patch.object(wtprobe_host.shutil, "chown"), \
					patch.object(wtprobe_host, "install_file") as installed:
				wtprobe_host.apply(KEY, modules, run=lambda argv, **_: ran.append(argv))
		written = {Path(call.args[0]).name: call.args[1].decode() for call in installed.call_args_list}
		self.assertIn("--receive", written["authorized_keys"])
		self.assertEqual(json.loads(written["monitor-wtprobe.json"])["production_state"], "/state/production.json")
		self.assertEqual(written["opensro-monitor-wtprobe.service"], "ExecStart=" + str(modules) + "/monitor.py\n")
		self.assertEqual(ran, [["systemctl", "daemon-reload"], ["systemctl", "enable", "--now", "opensro-monitor-wtprobe.timer"]])

	# ================
	# test_apply_refuses_a_host_without_the_fleet_monitor
	# ================
	def test_apply_refuses_a_host_without_the_fleet_monitor(self):
		with patch.object(wtprobe_host, "WEBHOOK", Path("/nonexistent/webhook")), patch.object(wtprobe_host, "install_file") as installed:
			with self.assertRaisesRegex(RuntimeError, "monitor-webhook|webhook"):
				wtprobe_host.apply(KEY, "/modules", run=lambda *_a, **_o: None)
		installed.assert_not_called()

	# ================
	# test_check_names_every_gap
	# ================
	def test_check_names_every_gap(self):
		with tempfile.TemporaryDirectory() as directory:
			root = Path(directory)
			with patch.object(wtprobe_host, "ROOT", root), patch.object(wtprobe_host, "VERDICT", root / "wtprobe/verdict.json"), \
					patch.object(wtprobe_host, "MONITOR_CONFIG", root / "monitor-wtprobe.json"):
				answer = lambda argv, **_: subprocess.CompletedProcess(argv, 1, stdout="disabled\n", stderr="")
				self.assertEqual(len(wtprobe_host.problems(run=answer)), 4)
				(root / "wtprobe").mkdir()
				(root / "wtprobe/verdict.json").write_text("{}")
				(root / "monitor-wtprobe.json").write_text("{}")
				(root / "wtprobe-push/.ssh").mkdir(parents=True)
				(root / "wtprobe-push/.ssh/authorized_keys").write_text(KEY)
				ready = lambda argv, **_: subprocess.CompletedProcess(argv, 0, stdout="enabled\n", stderr="")
				self.assertEqual(wtprobe_host.problems(run=ready), [])

	# ================
	# test_remove_stops_the_timer_and_deletes_everything
	# ================
	def test_remove_stops_the_timer_and_deletes_everything(self):
		ran = []
		with tempfile.TemporaryDirectory() as directory:
			root = Path(directory)
			(root / "wtprobe").mkdir()
			(root / "wtprobe/verdict.json").write_text("{}")
			(root / "wtprobe-push/.ssh").mkdir(parents=True)
			(root / "wtprobe-push/.ssh/authorized_keys").write_text(KEY)
			with patch.object(wtprobe_host, "ROOT", root), patch.object(wtprobe_host, "VERDICT_DIR", root / "wtprobe"), \
					patch.object(wtprobe_host, "SYSTEMD", root / "systemd"), patch.object(wtprobe_host, "MONITOR_CONFIG", root / "absent.json"):
				wtprobe_host.remove(run=lambda argv, **_: ran.append(argv))
			self.assertFalse((root / "wtprobe").exists())
			self.assertFalse((root / "wtprobe-push/.ssh/authorized_keys").exists())
		self.assertEqual(ran, [["systemctl", "disable", "--now", "opensro-monitor-wtprobe.timer"], ["systemctl", "daemon-reload"]])


if __name__ == "__main__":
	unittest.main()
