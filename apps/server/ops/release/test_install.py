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

from install import CONTROL_FILES, authorized_keys, install_version


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


# ================
# AuthorizedKeysTests
# ================
class AuthorizedKeysTests(unittest.TestCase):
	# ================
	# test_every_key_is_confined_to_the_forced_command
	# ================
	def test_every_key_is_confined_to_the_forced_command(self):
		text = "ssh-ed25519 AAAAci ci@github\n\nssh-ed25519 AAAAop operator@pc\n"
		lines = authorized_keys(text, "/usr/local/sbin/opensro-stage").splitlines()
		self.assertEqual(lines, [
			'restrict,command="sudo -n /usr/local/sbin/opensro-stage" ssh-ed25519 AAAAci',
			'restrict,command="sudo -n /usr/local/sbin/opensro-stage" ssh-ed25519 AAAAop',
		])

	# ================
	# test_any_other_key_type_or_no_key_is_refused
	# ================
	def test_any_other_key_type_or_no_key_is_refused(self):
		for text in ("", "ssh-rsa AAAA x\n", "ssh-ed25519 AAAAok\nssh-dss AAAA\n"):
			with self.assertRaisesRegex(ValueError, "Ed25519"):
				authorized_keys(text, "/usr/local/sbin/opensro-stage")


# ================
# InstalledModuleTests
# ================
class InstalledModuleTests(unittest.TestCase):
	# ================
	# test_every_release_module_an_installed_module_imports_is_installed
	#
	# The host runs only installed files; a module left off the list fails
	# there at import, after the release was already under way.
	# ================
	def test_every_release_module_an_installed_module_imports_is_installed(self):
		import ast
		source = Path(__file__).resolve().parent
		local = {path.stem for path in source.glob("*.py") if not path.name.startswith("test_")}
		installed = {name.removesuffix(".py") for name in CONTROL_FILES if name.endswith(".py")}
		for name in sorted(installed):
			tree = ast.parse((source / (name + ".py")).read_text(encoding="utf-8"))
			for node in ast.walk(tree):
				imported = []
				if isinstance(node, ast.Import):
					imported = [alias.name for alias in node.names]
				elif isinstance(node, ast.ImportFrom) and node.module:
					imported = [node.module]
				for module in imported:
					if module in local:
						self.assertIn(module, installed, f"{name}.py imports {module}, which is not installed")
