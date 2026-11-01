"""
===========================================================================

test_wtprobe_verdict.py - the off-host runner's verdicts and publication

The probe and the copy commands are injected: no test runs sro-wtprobe,
opens a network connection or reaches another host.

===========================================================================
"""

import json
import os
from pathlib import Path
import stat
import subprocess
import tempfile
import unittest

from monitor import check_wtprobe
import io

from wtprobe_verdict import make_verdict, parse_verdict, push_command, receive, run_probe, write_verdict


# ================
# finished
#
# A stand-in subprocess.run result.
# ================
def finished(code, stdout="", stderr=""):
	return subprocess.CompletedProcess(args=[], returncode=code, stdout=stdout, stderr=stderr)


# ================
# WTProbeVerdictTests
# ================
class WTProbeVerdictTests(unittest.TestCase):
	# ================
	# test_the_probe_verdict_is_its_exit_status_and_last_line
	# ================
	def test_the_probe_verdict_is_its_exit_status_and_last_line(self):
		calls = []

		# ================
		# run
		# ================
		def run(command, **_options):
			calls.append(command)
			return finished(0, "dialing\npass: stranger refused (stage=hello)\n")

		self.assertEqual(run_probe("sro-wtprobe", "https://play.example.net", run),
			(True, "pass: stranger refused (stage=hello)"))
		self.assertEqual(calls, [["sro-wtprobe", "-health", "-url", "https://play.example.net"]])
		self.assertEqual(run_probe("sro-wtprobe", "https://play.example.net", lambda *_a, **_o: finished(1, "", "FAIL stage=quic: timeout\n")),
			(False, "FAIL stage=quic: timeout"))

	# ================
	# test_a_probe_that_cannot_run_is_a_named_failure
	# ================
	def test_a_probe_that_cannot_run_is_a_named_failure(self):
		# ================
		# timeout
		# ================
		def timeout(command, **_options):
			raise subprocess.TimeoutExpired(command, 45)

		# ================
		# missing
		# ================
		def missing(_command, **_options):
			raise FileNotFoundError("sro-wtprobe")

		ok, detail = run_probe("sro-wtprobe", "https://play.example.net", timeout)
		self.assertFalse(ok)
		self.assertTrue(detail.startswith("FAIL stage=timeout"))
		ok, detail = run_probe("sro-wtprobe", "https://play.example.net", missing)
		self.assertFalse(ok)
		self.assertTrue(detail.startswith("FAIL stage=launch"))
		ok, detail = run_probe("sro-wtprobe", "https://play.example.net", lambda *_a, **_o: finished(1))
		self.assertEqual((ok, detail), (False, "exit status 1"))

	# ================
	# test_the_written_verdict_is_what_the_monitor_reads
	#
	# The runner's file round-trips into the monitor's check: a pass is a
	# fresh healthy observation, a long detail is capped.
	# ================
	def test_the_written_verdict_is_what_the_monitor_reads(self):
		with tempfile.TemporaryDirectory() as directory:
			path = Path(directory) / "verdict.json"
			write_verdict(path, make_verdict(True, "pass", 1000))
			observation, checked_at, fresh = check_wtprobe({"verdict": str(path)}, 1010)
			self.assertEqual((observation["healthy"], checked_at, fresh), (True, 1000, True))
			write_verdict(path, make_verdict(False, "FAIL stage=quic: " + "x" * 500, 1000))
			self.assertEqual(len(json.loads(path.read_text())["detail"]), 240)
			observation, _, fresh = check_wtprobe({"verdict": str(path)}, 1010)
			self.assertFalse(observation["healthy"])
			self.assertTrue(fresh)
			self.assertEqual(sorted(p.name for p in Path(directory).iterdir()), ["verdict.json"])

	# ================
	# test_publication_is_one_ssh_to_the_push_account
	#
	# The verdict travels on stdin; the host key's forced command chooses
	# where it lands, so the runner names only the account.
	# ================
	def test_publication_is_one_ssh_to_the_push_account(self):
		self.assertEqual(push_command("opensro-wtprobe@game.example"), ["ssh", "-o", "BatchMode=yes", "opensro-wtprobe@game.example"])
		for bad in ("game.example", "@game.example", "opensro-wtprobe@", "opensro-wtprobe@game.example:/tmp/x", "a b@host"):
			with self.subTest(target=bad), self.assertRaises(ValueError):
				push_command(bad)

	# ================
	# test_the_receiver_writes_only_a_well_formed_verdict
	#
	# A refused verdict leaves the previous one in place: the monitor then
	# reports staleness, never a forged or truncated result.
	# ================
	def test_the_receiver_writes_only_a_well_formed_verdict(self):
		with tempfile.TemporaryDirectory() as directory:
			path = Path(directory) / "verdict.json"
			good = json.dumps(make_verdict(True, "pass", 1000)).encode()
			receive(path, io.BytesIO(good))
			self.assertEqual(json.loads(path.read_text())["checkedAt"], 1000)
			for bad in (b"not json", b"[]", json.dumps({"v": 1, "checkedAt": 1, "ok": True}).encode(),
					json.dumps({"v": 1, "checkedAt": 1, "ok": "yes", "detail": ""}).encode(),
					json.dumps({"v": 2, "checkedAt": 1, "ok": True, "detail": ""}).encode(),
					json.dumps({"v": 1, "checkedAt": True, "ok": True, "detail": ""}).encode(),
					json.dumps({"v": 1, "checkedAt": 1, "ok": True, "detail": "", "extra": 1}).encode(),
					json.dumps({"v": 1, "checkedAt": 1, "ok": True, "detail": "x" * 241}).encode(),
					b" " * 5000):
				with self.subTest(verdict=bad[:40]), self.assertRaises(ValueError):
					receive(path, io.BytesIO(bad))
			self.assertEqual(json.loads(path.read_text())["checkedAt"], 1000)
			self.assertEqual(parse_verdict(good)["ok"], True)

	# ================
	# test_the_received_verdict_is_group_readable
	#
	# The push account writes the verdict and the monitor reads it through
	# the directory's group: a 0600 file (mkstemp's default) would read as
	# "unreadable" on every poll and page forever.
	# ================
	@unittest.skipUnless(os.name == "posix", "POSIX file modes")
	def test_the_received_verdict_is_group_readable(self):
		with tempfile.TemporaryDirectory() as directory:
			path = Path(directory) / "verdict.json"
			receive(path, io.BytesIO(json.dumps(make_verdict(True, "pass", 1000)).encode()))
			self.assertEqual(stat.S_IMODE(os.stat(path).st_mode), 0o640)
			write_verdict(path, make_verdict(True, "pass", 1000))
			self.assertEqual(stat.S_IMODE(os.stat(path).st_mode), 0o644)


if __name__ == "__main__":
	unittest.main()
