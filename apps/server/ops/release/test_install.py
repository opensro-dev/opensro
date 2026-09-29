"""
===========================================================================

test_install.py - verify immutable control installation without root or services.

Real directories and file hashes prove complete version visibility, reinstall
integrity and isolation between versions. Account and systemd setup are excluded.

===========================================================================
"""

import json
import os
from pathlib import Path
import tempfile
import unittest

from install import CONTROL_FILES, install_version


# ================
# InstallTests
# ================
class InstallTests(unittest.TestCase):
	# ================
	# setUp
	# ================
	def setUp(self):
		self.directory = tempfile.TemporaryDirectory()
		self.addCleanup(self.directory.cleanup)
		self.root = Path(self.directory.name)
		self.source = self.root / "source"
		self.source.mkdir()
		self.destination = self.root / "installed"
		for name in CONTROL_FILES:
			(self.source / name).write_text("fixture " + name, encoding="utf-8")

	# ================
	# test_complete_version_and_identical_reinstall
	# ================
	def test_complete_version_and_identical_reinstall(self):
		version = install_version(self.source, self.destination, "a" * 40)
		manifest = json.loads((version / "installed.json").read_text())
		self.assertEqual(set(manifest["files"]), set(CONTROL_FILES))
		self.assertEqual(install_version(self.source, self.destination, "a" * 40), version)
		self.assertEqual(list(self.destination.iterdir()), [version])

	# ================
	# test_private_umask_keeps_version_traversable_by_the_monitor
	# ================
	@unittest.skipIf(os.name == "nt", "POSIX permissions are verified on Linux")
	def test_private_umask_keeps_version_traversable_by_the_monitor(self):
		previous = os.umask(0o077)
		try:
			version = install_version(self.source, self.destination, "a" * 40)
		finally:
			os.umask(previous)
		self.assertEqual(self.destination.stat().st_mode & 0o777, 0o755)
		self.assertEqual(version.stat().st_mode & 0o777, 0o755)
		self.assertEqual((version / "monitor.py").stat().st_mode & 0o777, 0o644)

	# ================
	# test_new_version_does_not_mutate_the_old_receiver
	# ================
	def test_new_version_does_not_mutate_the_old_receiver(self):
		old = install_version(self.source, self.destination, "a" * 40)
		(self.source / "receiver.py").write_text("new receiver", encoding="utf-8")
		new = install_version(self.source, self.destination, "b" * 40)
		self.assertEqual((old / "receiver.py").read_text(), "fixture receiver.py")
		self.assertEqual((new / "receiver.py").read_text(), "new receiver")

	# ================
	# test_changed_source_cannot_replace_an_existing_identity
	# ================
	def test_changed_source_cannot_replace_an_existing_identity(self):
		install_version(self.source, self.destination, "a" * 40)
		(self.source / "receiver.py").write_text("changed", encoding="utf-8")
		with self.assertRaisesRegex(ValueError, "different control bytes"):
			install_version(self.source, self.destination, "a" * 40)

	# ================
	# test_installed_drift_is_not_silently_repaired
	# ================
	def test_installed_drift_is_not_silently_repaired(self):
		version = install_version(self.source, self.destination, "a" * 40)
		(version / "receiver.py").write_text("corrupted", encoding="utf-8")
		with self.assertRaisesRegex(ValueError, "drifted"):
			install_version(self.source, self.destination, "a" * 40)


if __name__ == "__main__":
	unittest.main()
