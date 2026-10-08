"""
===========================================================================

test_rebuild_lock_paths.py - lock log paths never crash across drives

A worktree on D: shares the generated tree and its lock on H:. Python's
os.path.relpath raises across Windows drives, which crashed every direct
convert_images.py run from such a worktree before the lock was taken.
relative_lock_path must fall back to the absolute path instead.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import os
import sys
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import rebuild_lock  # noqa: E402


class RelativeLockPathTest(unittest.TestCase):
	def test_a_lock_inside_the_checkout_is_relative(self) -> None:
		lock = rebuild_lock.REBUILD_ROOT / ".state" / "locks" / "generated-assets.lock"
		self.assertEqual(rebuild_lock.relative_lock_path(lock), ".state/locks/generated-assets.lock")

	def test_a_lock_on_another_drive_is_printed_absolute(self) -> None:
		lock = Path("H:/rebuild/.state/locks/generated-assets.lock")
		with mock.patch.object(os.path, "relpath", side_effect=ValueError("path is on mount 'H:'")):
			self.assertEqual(rebuild_lock.relative_lock_path(lock), lock.as_posix())


if __name__ == "__main__":
	unittest.main()
