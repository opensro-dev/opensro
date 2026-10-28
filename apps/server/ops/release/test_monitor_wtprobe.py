"""
===========================================================================

test_monitor_wtprobe.py - the off-host WebTransport probe feeding the monitor

The wtprobe kind (#1984) reads the verdict file the off-host runner leaves.
These drills stage verdicts and drive poll() on an injected clock and
notifier: thresholds count probe runs, not re-polls of one verdict; a
stale or missing verdict pages as a probe-pipeline failure; maintenance
and failed delivery behave as for every other check.

===========================================================================
"""

import json
from pathlib import Path
import tempfile
import unittest

from monitor import MAX_VERDICT_AGE_SECONDS, poll


# ================
# WTProbeMonitorTests
# ================
class WTProbeMonitorTests(unittest.TestCase):
	# ================
	# setUp
	#
	# A monitor config whose verdict and state live in a private directory,
	# and a notifier that records the messages it is given.
	# ================
	def setUp(self):
		self.directory = tempfile.TemporaryDirectory()
		root = Path(self.directory.name)
		self.verdict = root / "wtprobe-verdict.json"
		self.state = root / "wtprobe-state.json"
		self.config = {"kind": "wtprobe", "verdict": str(self.verdict), "state": str(self.state),
			"label": "WebTransport (off-host probe)", "webhook": "fixture"}
		self.messages = []

	# ================
	# tearDown
	# ================
	def tearDown(self):
		self.directory.cleanup()

	# ================
	# stage
	#
	# The runner's verdict for one probe run at checked_at.
	# ================
	def stage(self, checked_at, ok, detail="pass: stranger refused"):
		self.verdict.write_text(json.dumps({"v": 1, "checkedAt": checked_at, "ok": ok, "detail": detail}))

	# ================
	# observe
	# ================
	def observe(self, now, notify=None):
		return poll(self.config, now, notify=notify or (lambda _path, message: self.messages.append(message)))

	# ================
	# test_healthy_runs_page_nothing
	# ================
	def test_healthy_runs_page_nothing(self):
		for run in range(4):
			self.stage(run * 60, True)
			self.observe(run * 60 + 5)
			self.observe(run * 60 + 35)
		self.assertEqual(self.messages, [])

	# ================
	# test_one_bad_run_read_again_and_again_never_pages
	#
	# The timer reads one failed run three times; it is still one run.
	# ================
	def test_one_bad_run_read_again_and_again_never_pages(self):
		self.stage(0, True)
		self.observe(5)
		self.observe(35)
		self.stage(60, False, "FAIL stage=quic: timeout")
		for now in (65, 95, 125):
			self.observe(now)
		self.stage(120, True)
		self.observe(130)
		self.assertEqual(self.messages, [])
		self.assertEqual(json.loads(self.state.read_text())["failures"], 0)

	# ================
	# test_two_bad_runs_page_once_and_name_the_stage
	# ================
	def test_two_bad_runs_page_once_and_name_the_stage(self):
		self.stage(0, False, "FAIL stage=quic: timeout")
		self.observe(5)
		self.observe(35)
		self.stage(60, False, "FAIL stage=quic: timeout")
		self.observe(65)
		self.observe(95)
		self.stage(120, False, "FAIL stage=quic: timeout")
		self.observe(125)
		self.assertEqual(len(self.messages), 1)
		self.assertIn("WebTransport (off-host probe) is unavailable", self.messages[0])
		self.assertIn("FAIL stage=quic", self.messages[0])

	# ================
	# test_recovery_needs_two_good_runs_and_is_announced_once
	# ================
	def test_recovery_needs_two_good_runs_and_is_announced_once(self):
		for run in (0, 60):
			self.stage(run, False, "FAIL stage=hello: admitted")
			self.observe(run + 5)
		self.stage(120, True)
		self.observe(125)
		self.observe(155)
		self.assertEqual(len(self.messages), 1)
		self.stage(180, True)
		self.observe(185)
		self.observe(215)
		self.assertEqual(len(self.messages), 2)
		self.assertIn("has recovered", self.messages[1])

	# ================
	# test_a_silent_runner_pages_as_a_stale_probe
	#
	# The last run passed, then the runner stopped: once the verdict is
	# older than the bound, every poll is a failed observation.
	# ================
	def test_a_silent_runner_pages_as_a_stale_probe(self):
		self.stage(0, True)
		self.observe(5)
		late = MAX_VERDICT_AGE_SECONDS + 1
		self.observe(late)
		self.assertEqual(self.messages, [])
		self.observe(late + 30)
		self.assertEqual(len(self.messages), 1)
		self.assertIn("verdict stale", self.messages[0])
		self.assertIn("last probe result: pass", self.messages[0])

	# ================
	# test_a_missing_verdict_pages_as_unreadable
	# ================
	def test_a_missing_verdict_pages_as_unreadable(self):
		self.observe(0)
		self.observe(30)
		self.assertEqual(len(self.messages), 1)
		self.assertIn("verdict unreadable", self.messages[0])

	# ================
	# test_release_maintenance_suppresses_the_page_until_it_ends
	# ================
	def test_release_maintenance_suppresses_the_page_until_it_ends(self):
		journal = Path(self.directory.name) / "production.json"
		journal.write_text(json.dumps({"operation": {"component": "server", "phase": "deploying", "startedAt": 0}}))
		self.config["production_state"] = str(journal)
		for run in (0, 60):
			self.stage(run, False, "FAIL stage=quic: timeout")
			self.observe(run + 5)
		self.assertEqual(self.messages, [])
		journal.write_text(json.dumps({"operation": None}))
		self.stage(120, False, "FAIL stage=quic: timeout")
		self.observe(125)
		self.assertEqual(len(self.messages), 1)

	# ================
	# test_a_failed_page_is_retried_on_the_next_poll_of_the_same_run
	#
	# Re-polls do not count toward the threshold, but they must still
	# deliver a page the webhook refused.
	# ================
	def test_a_failed_page_is_retried_on_the_next_poll_of_the_same_run(self):
		attempts = []

		# ================
		# refuse_once
		# ================
		def refuse_once(_path, message):
			attempts.append(message)
			if len(attempts) == 1:
				raise RuntimeError("webhook unavailable")

		self.stage(0, False, "FAIL stage=quic: timeout")
		self.observe(5, refuse_once)
		self.stage(60, False, "FAIL stage=quic: timeout")
		with self.assertRaisesRegex(RuntimeError, "webhook unavailable"):
			self.observe(65, refuse_once)
		self.observe(95, refuse_once)
		self.assertEqual(len(attempts), 2)
		self.assertEqual(json.loads(self.state.read_text())["notified"], "down")
		self.observe(125, refuse_once)
		self.assertEqual(len(attempts), 2)


if __name__ == "__main__":
	unittest.main()
