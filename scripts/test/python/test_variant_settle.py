"""
===========================================================================

test_variant_settle.py - a rebuilt variant keeps unchanged files' times

A variant rebuild rewrites most files with the bytes they already held.
settle() gives those back their recorded mtime, keeps a changed file's new
one, trusts an untouched file without reading it, and forgets files that
left the tree.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import os
import tempfile
import unittest
from pathlib import Path

import variant_settle

# A later time than any file in the scratch tree, in nanoseconds.
LATER_NS = 4_000_000_000_000_000_000


class Settle(unittest.TestCase):

	def setUp(self):
		self.temp = tempfile.TemporaryDirectory()
		self.root = Path(self.temp.name)
		self.data = self.root / "extracted" / "Media_extracted" / "a.txt"
		self.data.parent.mkdir(parents=True)
		self.data.write_bytes(b"native")
		self.first = variant_settle.settle(self.root, 2)
		self.recorded = self.data.stat().st_mtime_ns

	def tearDown(self):
		self.temp.cleanup()

	def rewrite(self, content):
		self.data.write_bytes(content)
		os.utime(self.data, ns=(LATER_NS, LATER_NS))

	def test_first_run_records_every_file(self):
		self.assertEqual(self.first["files"], 1)
		self.assertEqual(self.first["changed"], 1)
		self.assertTrue(variant_settle.ledger_path(self.root).exists())

	def test_untouched_file_is_trusted_unread(self):
		counts = variant_settle.settle(self.root, 2)
		self.assertEqual((counts["unread"], counts["hashed"]), (1, 0))

	def test_same_bytes_get_their_old_time_back(self):
		self.rewrite(b"native")
		counts = variant_settle.settle(self.root, 2)
		self.assertEqual(counts["restored"], 1)
		self.assertEqual(self.data.stat().st_mtime_ns, self.recorded)

	def test_changed_bytes_keep_the_new_time(self):
		self.rewrite(b"imported")
		counts = variant_settle.settle(self.root, 2)
		self.assertEqual(counts["changed"], 1)
		self.assertEqual(self.data.stat().st_mtime_ns, LATER_NS)
		# The next run trusts the new state it recorded.
		self.assertEqual(variant_settle.settle(self.root, 2)["unread"], 1)

	def test_a_file_linked_from_another_tree_keeps_its_time(self):
		# The native extraction holds the other link: re-timing the variant's
		# link would re-time the native file too.
		native = self.root / "native-a.txt"
		self.data.unlink()
		native.write_bytes(b"native")
		os.link(native, self.data)
		variant_settle.settle(self.root, 2)
		os.utime(native, ns=(LATER_NS, LATER_NS))
		counts = variant_settle.settle(self.root, 2)
		self.assertEqual((counts["restored"], counts["changed"]), (0, 1))
		self.assertEqual(native.stat().st_mtime_ns, LATER_NS)

	def test_removed_file_leaves_the_ledger(self):
		self.data.unlink()
		variant_settle.settle(self.root, 2)
		self.assertEqual(variant_settle.load_ledger(variant_settle.ledger_path(self.root)), {})

	def test_foreign_ledger_is_a_cold_start(self):
		variant_settle.ledger_path(self.root).write_text('{"format":"other"}', encoding="utf-8")
		self.assertEqual(variant_settle.settle(self.root, 2)["hashed"], 1)


if __name__ == "__main__":
	unittest.main()
