"""
===========================================================================

test_wtprobe_verdict.py - the off-host runner's verdicts and publication

The probe and the copy commands are injected: no test runs sro-wtprobe,
opens a network connection or reaches another host.

===========================================================================
"""

import json
from pathlib import Path
import subprocess
import tempfile
import unittest

from monitor import check_wtprobe
from wtprobe_verdict import make_verdict, push_commands, run_probe, write_verdict


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
	# test_publication_copies_aside_then_renames
	# ================
	def test_publication_copies_aside_then_renames(self):
		commands = push_commands("verdict.json", "probe@game.example:/var/lib/opensro/wtprobe verdict.json")
		self.assertEqual(commands[0][-1], "probe@game.example:/var/lib/opensro/wtprobe verdict.json.incoming")
		self.assertEqual(commands[1][:3], ["ssh", "-o", "BatchMode=yes"])
		self.assertEqual(commands[1][-1], "mv -f '/var/lib/opensro/wtprobe verdict.json.incoming' '/var/lib/opensro/wtprobe verdict.json'")
		for bad in ("verdict.json", "probe@game.example:relative.json", ":/abs.json"):
			with self.subTest(target=bad), self.assertRaises(ValueError):
				push_commands("verdict.json", bad)


if __name__ == "__main__":
	unittest.main()
