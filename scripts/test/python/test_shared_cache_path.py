"""
===========================================================================

test_shared_cache_path.py - where a Python build step keeps a content cache

sro_paths.shared_cache_path follows generatedRoot.mjs sharedCachePath: under
SRO_SHARED_CACHE_ROOT when it is set (the release's --build-cache, which
outlives every staging), else under the tree's build state. The root must
be absolute, as on the Node side.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import tempfile
import unittest
from pathlib import Path

import sro_paths  # noqa: E402


class SharedCachePathTests(unittest.TestCase):
	def test_the_shared_root_holds_the_cache_when_set(self):
		root = Path(self.enterContext(tempfile.TemporaryDirectory()))
		got = sro_paths.shared_cache_path("vsro-import", {sro_paths.SHARED_CACHE_ROOT_ENV: str(root)})
		self.assertEqual(got, root.resolve() / "vsro-import")

	def test_unset_it_stays_with_the_tree(self):
		for env in ({}, {sro_paths.SHARED_CACHE_ROOT_ENV: ""}):
			self.assertEqual(sro_paths.shared_cache_path("vsro-import", env), sro_paths.build_state_path("vsro-import"))

	def test_a_relative_root_is_refused(self):
		with self.assertRaises(SystemExit):
			sro_paths.shared_cache_path("vsro-import", {sro_paths.SHARED_CACHE_ROOT_ENV: "relative/cache"})


if __name__ == "__main__":
	unittest.main()
