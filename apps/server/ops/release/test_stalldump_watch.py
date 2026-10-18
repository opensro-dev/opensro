"""
===========================================================================

test_stalldump_watch.py - baseline, single-page and failed-delivery drills.

Inject notification delivery so no test pages a real channel. File ages
are set explicitly; alert timing never depends on sleeping.

===========================================================================
"""

import json
import os
from pathlib import Path
import tempfile
import unittest

from stalldump_watch import dump_header, poll

DUMP_BODY = ("tick stall dump\n"
	"at:      2026-10-18T19:21:17.123Z\n"
	"stalled: 45.2s\n"
	"phase:   movement\n"
	"hook:    onTick\n"
	"pid:     4242\n"
	"\n== goroutines waiting on locks ==\n")


# ================
# write_dump
#
# A dump old enough to page, under a fabricated name in a temp directory.
# ================
def write_dump(directory, name, aged_at):
	path = Path(directory) / name
	path.write_text(DUMP_BODY, encoding="utf-8")
	os.utime(path, (aged_at, aged_at))
	return path


# ================
# StallDumpWatchTests
# ================
class StallDumpWatchTests(unittest.TestCase):
	# ================
	# config
	# ================
	def config(self, directory):
		return {"dir": str(directory), "state": str(Path(directory) / "watch.json"),
			"webhook": "fixture", "label": "global-official"}

	# ================
	# test_first_run_adopts_the_baseline_without_paging
	#
	# The stall that already happened (tonight's dump) must never page:
	# the first observation records what exists and stays silent.
	# ================
	def test_first_run_adopts_the_baseline_without_paging(self):
		with tempfile.TemporaryDirectory() as directory:
			write_dump(directory, "tick-stall-20261018T192117Z.txt", 100)
			pages = []
			state = poll(self.config(directory), 1000, notify=lambda _p, m: pages.append(m))
			self.assertEqual(pages, [])
			self.assertEqual(state["seen"], ["tick-stall-20261018T192117Z.txt"])

	# ================
	# test_a_new_dump_pages_once_with_its_header
	# ================
	def test_a_new_dump_pages_once_with_its_header(self):
		with tempfile.TemporaryDirectory() as directory:
			write_dump(directory, "tick-stall-20261018T192117Z.txt", 100)
			pages = []
			notify = lambda _p, m: pages.append(m)
			poll(self.config(directory), 1000, notify=notify)
			write_dump(directory, "tick-stall-20261018T220501Z.txt", 1000)
			Path(directory, "notes.txt").write_text("not a dump", encoding="utf-8")
			poll(self.config(directory), 1030, notify=notify)
			self.assertEqual(len(pages), 1)
			self.assertIn("tick-stall-20261018T220501Z.txt", pages[0])
			self.assertIn("stalled: 45.2s", pages[0])
			self.assertIn("phase: movement", pages[0])
			self.assertIn("hook: onTick", pages[0])
			poll(self.config(directory), 1060, notify=notify)
			self.assertEqual(len(pages), 1, "a paged dump never pages twice")

	# ================
	# test_failed_delivery_is_retried_not_marked_seen
	# ================
	def test_failed_delivery_is_retried_not_marked_seen(self):
		with tempfile.TemporaryDirectory() as directory:
			pages = []
			notify = lambda _p, m: pages.append(m)
			poll(self.config(directory), 1000, notify=notify)
			write_dump(directory, "tick-stall-20261018T220501Z.txt", 1000)

			# ================
			# failed_delivery
			# ================
			def failed_delivery(_path, _message):
				raise RuntimeError("notification transport failed")

			with self.assertRaisesRegex(RuntimeError, "transport failed"):
				poll(self.config(directory), 1030, notify=failed_delivery)
			state = json.loads(Path(directory, "watch.json").read_text())
			self.assertNotIn("tick-stall-20261018T220501Z.txt", state["seen"])
			poll(self.config(directory), 1060, notify=notify)
			self.assertEqual(len(pages), 1)

	# ================
	# test_a_still_flushing_dump_waits_for_the_next_tick
	# ================
	def test_a_still_flushing_dump_waits_for_the_next_tick(self):
		with tempfile.TemporaryDirectory() as directory:
			pages = []
			notify = lambda _p, m: pages.append(m)
			poll(self.config(directory), 1000, notify=notify)
			write_dump(directory, "tick-stall-20261018T220501Z.txt", 1029)
			poll(self.config(directory), 1030, notify=notify)
			self.assertEqual(pages, [])
			poll(self.config(directory), 1060, notify=notify)
			self.assertEqual(len(pages), 1)

	# ================
	# test_header_parsing_skips_the_stacks
	# ================
	def test_header_parsing_skips_the_stacks(self):
		with tempfile.TemporaryDirectory() as directory:
			path = write_dump(directory, "tick-stall-20261018T220501Z.txt", 100)
			self.assertEqual(dump_header(path), {"at": "2026-10-18T19:21:17.123Z",
				"stalled": "45.2s", "phase": "movement", "hook": "onTick"})


if __name__ == "__main__":
	unittest.main()
