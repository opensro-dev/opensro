"""
===========================================================================

test_tree_scan.py - the listing scan sees what os.walk and os.stat see

tree_scan.scan_files stands in for os.walk plus a stat per file in the
variant's no-op checks, and the recorded fingerprints and ledgers were
taken the old way: the scan must find the same files, under the same
relative paths, with the same size and mtime, leave out only the named
top-level folders, and never descend a linked folder.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import os
import tempfile
import unittest
from pathlib import Path

import tree_scan

# A fixed time, in nanoseconds, set on one file so the scan must carry it.
SET_NS = 1_700_000_000_123_456_700
# A later time than any file in the scratch tree (test_variant_settle.py's).
LATER_NS = 4_000_000_000_000_000_000


class ScanFiles(unittest.TestCase):

	def setUp(self):
		self.temp = tempfile.TemporaryDirectory()
		self.root = Path(self.temp.name) / "tree"
		for relative, text in (
			("a.txt", "a"),
			("Data/b.bin", "bb"),
			("Data/deep/c.txt", "ccc"),
			("skip/d.txt", "d"),
			("Data/skip/e.txt", "e"),
		):
			path = self.root / relative
			path.parent.mkdir(parents=True, exist_ok=True)
			path.write_text(text)
		os.utime(self.root / "Data/deep/c.txt", ns=(SET_NS, SET_NS))

	def tearDown(self):
		self.temp.cleanup()

	def walked(self, skip=()):
		rows = []
		for directory, folders, names in os.walk(self.root):
			if Path(directory) == self.root:
				folders[:] = [folder for folder in folders if folder not in skip]
			for name in names:
				absolute = Path(directory, name)
				stat = os.stat(absolute)
				rows.append((absolute.relative_to(self.root).as_posix(), stat.st_size, stat.st_mtime_ns))
		return sorted(rows)

	def scanned(self, skip=()):
		return [(relative, stat.st_size, stat.st_mtime_ns) for relative, _, stat in tree_scan.scan_files(self.root, skip)]

	def test_the_scan_matches_walk_and_stat(self):
		self.assertEqual(self.scanned(), self.walked())
		self.assertIn(("Data/deep/c.txt", 3, SET_NS), self.scanned())

	def test_only_a_top_level_folder_is_skipped(self):
		rows = [relative for relative, _, _ in self.scanned(("skip",))]
		self.assertNotIn("skip/d.txt", rows)
		self.assertIn("Data/skip/e.txt", rows)
		self.assertEqual(self.scanned(("skip",)), self.walked(("skip",)))

	def test_absolute_paths_name_the_files(self):
		for relative, absolute, _ in tree_scan.scan_files(self.root):
			self.assertEqual(absolute, self.root / relative)

	def test_a_new_time_and_a_write_through_another_link_are_seen(self):
		# The contract the stat pass keeps: a re-timed file and a file
		# re-timed through another link read their new time. Whether a
		# directory entry lags depends on NTFS's update timing, which this
		# scratch case does not reliably hit; test_variant_settle.py's
		# same-bytes restore is the case that fails on entry stats.
		target = self.root / "a.txt"
		tree_scan.scan_files(self.root)
		target.write_bytes(b"a")
		os.utime(target, ns=(LATER_NS, LATER_NS))
		self.assertIn(("a.txt", 1, LATER_NS), self.scanned())
		# The tree holds the link; the file is re-timed through its other name
		# (the native extraction's), as a variant's linked files are.
		original = Path(self.temp.name) / "native.bin"
		original.write_bytes(b"nn")
		os.link(original, self.root / "Data/linked.bin")
		tree_scan.scan_files(self.root)
		os.utime(original, ns=(LATER_NS + 100, LATER_NS + 100))
		self.assertIn(("Data/linked.bin", 2, LATER_NS + 100), self.scanned())

	def test_a_missing_root_has_no_files(self):
		self.assertEqual(tree_scan.scan_files(self.root / "absent"), [])

	def test_a_linked_folder_is_not_descended(self):
		outside = Path(self.temp.name) / "outside"
		outside.mkdir()
		(outside / "f.txt").write_text("f")
		try:
			os.symlink(outside, self.root / "link", target_is_directory=True)
		except OSError:
			self.skipTest("this account cannot create symbolic links")
		rows = [relative for relative, _, _ in self.scanned()]
		self.assertFalse(any(relative.startswith("link/") for relative in rows))
		self.assertEqual(self.scanned(), self.walked())


if __name__ == "__main__":
	unittest.main()
