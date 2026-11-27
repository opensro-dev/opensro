"""
===========================================================================

test_variant_reuse.py - a rebuild lays unchanged files back by rename

variant_reuse.py (#4337 items 29/31): a base rerun used to copy the whole
native tree from H: again after rollback deleted it. Now a removed file is
kept beside the tree and link_tree renames it back while its source is
unchanged. A source that changed is linked or copied fresh, a fresh build's
clear moves the tree aside in one rename, and a rollback keeps what it
removes. The tests stand in for the cross-volume case by refusing links.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import contextlib
import io
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import variant_files
import variant_reuse


class VariantReuseTest(unittest.TestCase):
	def setUp(self):
		self.scratch = tempfile.TemporaryDirectory()
		root = Path(self.scratch.name)
		self.native = root / "native" / "Media_extracted"
		(self.native / "sub").mkdir(parents=True)
		(self.native / "a.txt").write_bytes(b"alpha")
		(self.native / "sub" / "b.txt").write_bytes(b"beta")
		self.variant = root / "variant"
		self.target = self.variant / "extracted" / "Media_extracted"
		self.reuse = variant_reuse.reuse_root(self.variant) / "extracted" / "Media_extracted"

	def tearDown(self):
		self.scratch.cleanup()

	def link(self):
		# Links refused, as from the native H: tree to the D: staging.
		with mock.patch.object(variant_files.os, "link", side_effect=OSError("cross-device")), \
				mock.patch.object(variant_files.shutil, "copy2", wraps=variant_files.shutil.copy2) as copy, \
				contextlib.redirect_stdout(io.StringIO()):
			count = variant_files.link_tree(self.native, self.target, self.reuse)
		return count, copy.call_count

	# ================
	# test_an_unchanged_tree_is_renamed_back_not_copied
	# ================
	def test_an_unchanged_tree_is_renamed_back_not_copied(self):
		self.assertEqual(self.link(), (2, 2))
		for relative in ("Media_extracted/a.txt", "Media_extracted/sub/b.txt"):
			variant_reuse.stash(self.variant, "extracted/" + relative)
		self.assertFalse((self.target / "a.txt").exists())
		self.assertEqual(self.link(), (2, 0))
		self.assertEqual((self.target / "sub" / "b.txt").read_bytes(), b"beta")
		self.assertFalse((self.reuse / "a.txt").exists())

	# ================
	# test_a_changed_source_is_copied_fresh
	# ================
	def test_a_changed_source_is_copied_fresh(self):
		self.link()
		variant_reuse.stash(self.variant, "extracted/Media_extracted/a.txt")
		variant_reuse.stash(self.variant, "extracted/Media_extracted/sub/b.txt")
		(self.native / "a.txt").write_bytes(b"ALPHA, re-extracted")
		stat = (self.native / "a.txt").stat()
		os.utime(self.native / "a.txt", ns=(stat.st_atime_ns, stat.st_mtime_ns + 10_000_000))
		self.assertEqual(self.link(), (2, 1))
		self.assertEqual((self.target / "a.txt").read_bytes(), b"ALPHA, re-extracted")

	# ================
	# test_a_fresh_clear_moves_the_tree_aside_and_clear_drops_it
	# ================
	def test_a_fresh_clear_moves_the_tree_aside_and_clear_drops_it(self):
		self.link()
		variant_reuse.stash_tree(self.variant, "extracted")
		self.assertFalse((self.variant / "extracted").exists())
		self.assertEqual(self.link(), (2, 0))
		variant_reuse.clear(self.variant)
		self.assertFalse(variant_reuse.reuse_root(self.variant).exists())


if __name__ == "__main__":
	unittest.main()
