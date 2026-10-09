"""
===========================================================================

test_retention.py - retained server inputs keep their executables runnable.

A revert runs the retained release's own sro-game-data-check in place, so a
retained server binary must stay executable, and a snapshot retained by
older controls (as 0600) is repaired when it is reused.

===========================================================================
"""

import hashlib
import json
import os
from pathlib import Path
import stat
import tempfile
import unittest

from bundle import FILES
from retention import RETAINED_EXECUTABLE, RETAINED_FILE, preserve

COMMIT = "a" * 40


# ================
# RetentionModeTest
# ================
@unittest.skipIf(os.name == "nt", "POSIX execute bits; the release controls run on Linux")
class RetentionModeTest(unittest.TestCase):
	def setUp(self):
		self.root = Path(tempfile.mkdtemp())
		self.live = self.root / "module"
		self.config = {"candidate_records": str(self.root / "records")}
		files = {}
		for name in FILES:
			path = self.live / name
			path.parent.mkdir(parents=True, exist_ok=True)
			path.write_bytes(name.encode())
			path.chmod(0o755)
			files[name] = hashlib.sha256(name.encode()).hexdigest()
		self.raw = json.dumps({"commit": COMMIT, "files": files}).encode()
		self.row = {"commit": COMMIT, "release": COMMIT}

	def mode(self, target, name):
		return stat.S_IMODE((target / name).stat().st_mode)

	# ================
	# test_binaries_stay_executable_and_data_stays_private
	# ================
	def test_binaries_stay_executable_and_data_stays_private(self):
		target = preserve(self.config, "server", self.row, self.live, self.raw)
		for name, packaged in FILES.items():
			want = RETAINED_EXECUTABLE if packaged.startswith("bin/") else RETAINED_FILE
			self.assertEqual(self.mode(target, name), want, name)

	# ================
	# test_reuse_repairs_a_snapshot_retained_without_execute
	# ================
	def test_reuse_repairs_a_snapshot_retained_without_execute(self):
		target = preserve(self.config, "server", self.row, self.live, self.raw)
		(target / "sro-game-data-check").chmod(0o600)
		again = preserve(self.config, "server", self.row, self.live, self.raw)
		self.assertEqual(again, target)
		self.assertEqual(self.mode(target, "sro-game-data-check"), RETAINED_EXECUTABLE)


if __name__ == "__main__":
	unittest.main()
