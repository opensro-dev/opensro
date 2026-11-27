"""
===========================================================================

test_build_progress.py - every second of the build sits in a named span

build_progress.py (#4337, blind spots B3/B4): a phase prints when it opens
and how long it ran when the next opens; the heartbeat names the running
step and phase, or the step the build is waiting after; the variant runner
marks each step, even one that raises.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import contextlib
import io
import threading
import unittest

import build_progress


class BuildProgressTest(unittest.TestCase):
	def setUp(self):
		build_progress.end_step()
		build_progress._position.update(after=None)

	# ================
	# test_phases_close_each_other
	# ================
	def test_phases_close_each_other(self):
		out = io.StringIO()
		with contextlib.redirect_stdout(out):
			phases = build_progress.Phases("merge")
			phases.enter("map")
			phases.enter("data")
			phases.done()
		lines = out.getvalue().splitlines()
		self.assertEqual(lines[0], "merge: map ...")
		self.assertTrue(lines[1].startswith("merge: map done in "), lines)
		self.assertEqual(lines[2], "merge: data ...")
		self.assertTrue(lines[3].startswith("merge: data done in "), lines)
		self.assertEqual(len(lines), 4)

	# ================
	# test_position_names_step_and_phase
	# ================
	def test_position_names_step_and_phase(self):
		self.assertEqual(build_progress.position_line(), "build: between steps (after start)")
		build_progress.begin_step("later-layers")
		with contextlib.redirect_stdout(io.StringIO()):
			build_progress.Phases("Roc merge").enter("availability")
		line = build_progress.position_line()
		self.assertTrue(line.startswith("step later-layers: running "), line)
		self.assertIn("phase Roc merge/availability", line)
		build_progress.end_step()
		self.assertEqual(build_progress.position_line(), "build: between steps (after later-layers)")

	# ================
	# test_heartbeat_speaks_until_stopped
	# ================
	def test_heartbeat_speaks_until_stopped(self):
		said = []
		spoke = threading.Event()

		def say(line):
			said.append(line)
			spoke.set()

		build_progress.begin_step("families")
		beat = build_progress.Heartbeat(interval=0.01, say=say).start()
		self.assertTrue(spoke.wait(5), "the heartbeat never spoke")
		beat.stop()
		count = len(said)
		self.assertTrue(said[0].startswith("step families: running "), said)
		spoke.clear()
		self.assertFalse(spoke.wait(0.05), "the heartbeat spoke after stop")
		self.assertEqual(len(said), count)

	# ================
	# test_runner_marks_a_step_that_raises
	#
	# The variant runner's step span closes even when the step fails, so a
	# heartbeat after a failure never names a step that is not running.
	# ================
	def test_runner_marks_a_step_that_raises(self):
		import variant_steps

		seen = []

		class Failing(variant_steps.Runner):
			def __init__(self):
				self.steps = [{"name": "broken"}]

			def settle_step(self, index):
				seen.append(build_progress.position_line())
				raise RuntimeError("boom")

		with self.assertRaises(RuntimeError):
			Failing().result(0)
		self.assertTrue(seen[0].startswith("step broken: running "), seen)
		self.assertEqual(build_progress.position_line(), "build: between steps (after broken)")


if __name__ == "__main__":
	unittest.main()
