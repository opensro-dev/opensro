"""
===========================================================================

test_variant_tree_reads.py - a variant step reruns on what it read of the tree

variant_steps keys a step by what it read of the tree in front of it
(variant_tree_reads.py), not by every earlier effect: a step that never
read a changed file replays, while a step that read it, listed its
directory, or tested whether it exists reruns. A step that starts a
process keeps the chain key, and a journal written before reads were
recorded replays unchanged.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import contextlib
import io
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

import state_document
import variant_steps
import variant_tree_reads
from test_variant_steps import run_build


class TreeView(unittest.TestCase):

	def test_files_directories_and_removal(self):
		view = variant_tree_reads.TreeView()
		view.apply([["extracted/A/b.txt", "s1"], ["extracted/A/c.txt", "s2"], ["extracted/d.txt", "s3"]])
		self.assertEqual(view.value("extracted/a/B.TXT"), "s1")
		self.assertEqual(view.value("extracted/A"), "dir")
		self.assertEqual(view.listing("extracted/a"), "b.txt\0c.txt")
		self.assertEqual(view.listing("extracted"), "A\0d.txt")
		view.apply([["extracted/A/b.txt", None], ["extracted/A/c.txt", None]])
		# A directory goes with its last file.
		self.assertEqual(view.value("extracted/A"), "gone")
		self.assertEqual(view.listing("extracted"), "d.txt")

	def test_an_opaque_or_unrecorded_step_has_no_tree_key(self):
		view = variant_tree_reads.TreeView()
		self.assertIsNone(variant_tree_reads.tree_key(view, None))
		self.assertIsNone(variant_tree_reads.tree_key(view, variant_tree_reads.OPAQUE))
		self.assertIsNotNone(variant_tree_reads.tree_key(view, {"paths": [], "lists": []}))


class TreeReads(unittest.TestCase):

	def setUp(self):
		self.temp = tempfile.TemporaryDirectory()
		self.variant = Path(self.temp.name) / "variant"
		self.source = Path(self.temp.name) / "sources"
		self.source.mkdir()
		(self.source / "s.txt").write_bytes(b"one")
		self.counters: dict = {}

	def tearDown(self):
		self.temp.cleanup()

	def tree(self, name: str) -> Path:
		return self.variant / "extracted" / name

	def count(self, name: str):
		self.counters[name] = self.counters.get(name, 0) + 1

	# ================
	# writer
	#
	# The first step: lays a.txt from the source, and flag.txt only when
	# the source says "flag".
	# ================
	def writer(self):
		self.count("writer")
		data = (self.source / "s.txt").read_bytes()
		self.tree("a.txt").write_bytes(data)
		if data == b"flag":
			self.tree("flag.txt").write_bytes(b"f")
		return {}

	def build(self, *steps):
		with contextlib.redirect_stdout(io.StringIO()):
			run_build(self.variant, [("writer", self.writer, ()), *steps], roots=(self.source,))

	# ================
	# test_a_step_that_never_read_the_change_replays
	# ================
	def test_a_step_that_never_read_the_change_replays(self):
		def apart():
			self.count("apart")
			self.tree("apart.txt").write_bytes(b"x")
			return {}

		def reader():
			self.count("reader")
			self.tree("echo.txt").write_bytes(self.tree("a.txt").read_bytes())
			return {}

		steps = (("apart", apart, ()), ("reader", reader, ()))
		self.build(*steps)
		(self.source / "s.txt").write_bytes(b"two")
		self.build(*steps)
		self.assertEqual(self.counters, {"writer": 2, "apart": 1, "reader": 2})
		self.assertEqual(self.tree("echo.txt").read_bytes(), b"two")
		self.assertEqual(self.tree("apart.txt").read_bytes(), b"x")

	# ================
	# test_a_listing_reruns_its_step_when_an_entry_comes
	# ================
	def test_a_listing_reruns_its_step_when_an_entry_comes(self):
		def lister():
			self.count("lister")
			names = sorted(path.name for path in (self.variant / "extracted").iterdir() if path.name != "names.txt")
			self.tree("names.txt").write_text(",".join(names))
			return {}

		self.build(("lister", lister, ()))
		(self.source / "s.txt").write_bytes(b"two")
		# a.txt's bytes changed, but not the names: the listing holds.
		self.build(("lister", lister, ()))
		self.assertEqual(self.counters["lister"], 1)
		(self.source / "s.txt").write_bytes(b"flag")
		self.build(("lister", lister, ()))
		self.assertEqual(self.counters["lister"], 2)
		self.assertEqual(self.tree("names.txt").read_text(), "a.txt,flag.txt")

	# ================
	# test_an_existence_test_reruns_its_step
	# ================
	def test_an_existence_test_reruns_its_step(self):
		def tester():
			self.count("tester")
			self.tree("seen.txt").write_text("yes" if self.tree("flag.txt").exists() else "no")
			return {}

		self.build(("tester", tester, ()))
		(self.source / "s.txt").write_bytes(b"flag")
		self.build(("tester", tester, ()))
		self.assertEqual(self.counters["tester"], 2)
		self.assertEqual(self.tree("seen.txt").read_text(), "yes")

	# ================
	# test_a_step_that_starts_a_process_keeps_the_chain
	#
	# The child's reads are its own: the step reruns on any earlier change.
	# ================
	def test_a_step_that_starts_a_process_keeps_the_chain(self):
		def spawner():
			self.count("spawner")
			subprocess.run([sys.executable, "-c", "pass"], check=True)
			self.tree("spawned.txt").write_bytes(b"s")
			return {}

		self.build(("spawner", spawner, ()))
		self.build(("spawner", spawner, ()))
		self.assertEqual(self.counters["spawner"], 1)
		(self.source / "s.txt").write_bytes(b"two")
		self.build(("spawner", spawner, ()))
		self.assertEqual(self.counters["spawner"], 2)

	# ================
	# test_a_journal_without_tree_reads_replays
	#
	# Entries journaled before reads were recorded are keyed by the chain,
	# exactly as they were: the change of keying reruns nothing.
	# ================
	def test_a_journal_without_tree_reads_replays(self):
		def apart():
			self.count("apart")
			self.tree("apart.txt").write_bytes(b"x")
			return {}

		steps = (("apart", apart, ()),)
		self.build(*steps)
		journal = variant_steps.journal_path(self.variant)
		body = state_document.read_document(
			journal, variant_steps.JOURNAL_FORMAT, "journal", variant_steps.JOURNAL_VERSION
		)
		state = variant_steps.EMPTY_STATE
		for entry in body["steps"]:
			entry["reads"].pop("tree")
			entry["parts"]["state"] = state
			entry["key"] = variant_steps.parts_key(entry["name"], entry["parts"])
			state = variant_steps.chain(state, entry["effect"])
		state_document.write_document(
			journal, variant_steps.JOURNAL_FORMAT, "journal", body, variant_steps.JOURNAL_VERSION
		)
		self.build(*steps)
		self.assertEqual(self.counters, {"writer": 1, "apart": 1})


if __name__ == "__main__":
	unittest.main()
