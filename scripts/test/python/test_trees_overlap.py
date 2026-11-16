"""
===========================================================================

test_trees_overlap.py - sro_paths.trees_overlap, the importers' guard

Every variant importer refuses an output that is its source, inside it, or
holds it; a sibling folder that only shares a name prefix is separate.

===========================================================================
"""
import unittest
from pathlib import Path

import script_paths  # noqa: F401  (puts scripts/ on sys.path)
from sro_paths import trees_overlap

ROOT = Path("C:/game").resolve()


class TreesOverlapTest(unittest.TestCase):
	def test_the_same_nested_or_enclosing_folder_overlaps(self):
		self.assertTrue(trees_overlap(ROOT, ROOT))
		self.assertTrue(trees_overlap(ROOT, ROOT / "extracted"))
		self.assertTrue(trees_overlap(ROOT / "extracted" / "media", ROOT))

	def test_siblings_do_not_overlap(self):
		self.assertFalse(trees_overlap(ROOT / "extracted", ROOT / "variant"))
		self.assertFalse(trees_overlap(ROOT / "extracted", ROOT / "extracted-variant"))


if __name__ == "__main__":
	unittest.main()
