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
import sys
import tempfile
import unittest
from unittest.mock import patch

import install
from install import CONTROL_FILES, authorized_keys, install_version
from release_state import write_state
from test_release_state import production


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
	# test_quic_sysctls_install_the_udp_buffers
	#
	# The public WebTransport listener's socket buffers land as a sysctl
	# drop-in and are applied; the exact values are the contract.
	# ================
	def test_quic_sysctls_install_the_udp_buffers(self):
		with patch.object(install, "install_file") as installed, patch("install.subprocess.run") as applied:
			install.quic_sysctls()
		path, body = installed.call_args.args
		self.assertEqual(path, install.QUIC_SYSCTL_PATH)
		self.assertEqual(body.decode(), install.QUIC_SYSCTL_BODY)
		self.assertIn("net.core.rmem_max = 7500000", install.QUIC_SYSCTL_BODY)
		self.assertIn("net.core.wmem_max = 7500000", install.QUIC_SYSCTL_BODY)
		applied.assert_called_once_with(["sysctl", "--system"], check=True)

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
# StalldumpAccessTests
#
# The watcher (opensro-monitor) must reach dumps inside the GameWorld's
# mode-700 authority; install grants exactly the traversal, the
# directory read and the inheritance that make that work.
# ================
@unittest.skipIf(os.name == "nt", "POSIX ACL grants are verified on Linux")
class StalldumpAccessTests(unittest.TestCase):
	# ================
	# setUp
	# ================
	def setUp(self):
		self.directory = tempfile.TemporaryDirectory()
		self.addCleanup(self.directory.cleanup)
		self.root = Path(self.directory.name)
		self.calls = []

	# ================
	# record
	# ================
	def record(self, arguments, check=False):
		self.calls.append(list(arguments))

	# ================
	# test_grants_traverse_read_and_inheritance
	# ================
	def test_grants_traverse_read_and_inheritance(self):
		authority = self.root / "authority"
		dumps = authority / "stall-dumps"
		dumps.mkdir(parents=True)
		authority.chmod(0o700)
		dumps.chmod(0o750)
		dump = dumps / "tick-stall-20261018T000000Z.txt"
		dump.write_text("tick stall dump\n", encoding="utf-8")
		install.stalldump_access(dumps, "opensro-monitor", run=self.record)
		self.assertIn(["setfacl", "-m", "u:opensro-monitor:--x", str(authority)], self.calls)
		self.assertIn(["setfacl", "-m", "u:opensro-monitor:r-x", str(dumps)], self.calls)
		self.assertIn(["setfacl", "-d", "-m", "u:opensro-monitor:r--", str(dumps)], self.calls)
		self.assertIn(["setfacl", "-m", "u:opensro-monitor:r--", str(dump)], self.calls)
		for call in self.calls:
			self.assertNotEqual(call[-1], str(self.root.parent), "world-traversable ancestors are left alone")

	# ================
	# test_missing_directory_is_skipped
	#
	# The GameWorld creates the directory on first start; an install
	# before that must not fail or invent shard state.
	# ================
	def test_missing_directory_is_skipped(self):
		install.stalldump_access(self.root / "authority" / "stall-dumps", "opensro-monitor", run=self.record)
		self.assertEqual(self.calls, [])


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

	# ================
	# test_controls_install_only_between_releases
	#
	# An operation opened by older controls (no nonce digest) could be ended
	# by the new ones only by takeover, so installation refuses before it
	# changes anything; a first install and a closed journal proceed.
	# ================
	def test_controls_install_only_between_releases(self):
		directory = tempfile.TemporaryDirectory()
		self.addCleanup(directory.cleanup)
		root = Path(directory.name) / "release"
		state_path = root / "public/production.json"
		state_path.parent.mkdir(parents=True)
		install.require_closed_journal(state_path)
		write_state(state_path, production())
		install.require_closed_journal(state_path)
		state = production()
		state["operation"] = {"component": "release", "phase": "verifying", "startedAt": 1.0}
		write_state(state_path, state)
		arguments = ["install.py", "--client-manifest", "m", "--client-commit", "c", "--stage-key", "s",
			"--publish-key", "p", "--source-commit", "x"]
		with patch.object(install, "ROOT", root), patch.object(install, "install_version") as version, \
			patch.object(install.os, "geteuid", return_value=0, create=True), \
			patch.object(sys, "argv", arguments):
			with self.assertRaisesRegex(RuntimeError, r"operation is open \(release, verifying\)"):
				install.main()
		version.assert_not_called()
