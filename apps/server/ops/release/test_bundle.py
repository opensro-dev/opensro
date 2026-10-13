"""
===========================================================================
test_bundle.py - artifact integrity and extraction boundaries
===========================================================================
"""

import hashlib
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest

from bundle import FILES, GO_BUILDINFO_MAGIC, bundle, go_build_settings, unpack
from deploy import receive
from test_release_state import candidate


# ================
# uvarint
# ================
def uvarint(value):
	out = bytearray()
	while True:
		byte = value & 0x7f
		value >>= 7
		out.append(byte | (0x80 if value else 0))
		if not value:
			return bytes(out)


# ================
# go_binary
#
# A stand-in server binary carrying Go's inline build information blob, laid
# out as the linker writes it (aligned header, uvarint strings, binary
# sentinels around the module text), stamped for commit.
# ================
def go_binary(name, commit, modified="false", version="go1.27.2"):
	text = ("path\topensro.online/server/cmd/" + name + "\nbuild\t-trimpath=true\nbuild\tvcs=git\n" +
		"build\tvcs.revision=" + commit + "\nbuild\tvcs.modified=" + modified + "\n").encode()
	modules = bytes.fromhex("3077af0c9274080241e1c107e6d618e6") + text + bytes.fromhex("f932433186182072008242104116d8f2")
	header = GO_BUILDINFO_MAGIC + bytes([8, 2]) + bytes(16)
	return (("fixture: " + name).encode().ljust(48, b"\0") + header + uvarint(len(version)) + version.encode() +
		uvarint(len(modules)) + modules)


# ================
# fixture_file
#
# One bundled file's bytes: a stamped binary for bin/, plain text otherwise.
# ================
def fixture_file(name, source, commit):
	return go_binary(name, commit) if source.startswith("bin/") else ("fixture: " + name).encode()


# ================
# BundleTests
#
# Build real tar artifacts and mutate archive entries, never implementation
# text, to prove rejection happens before any extraction side effects.
# ================
class BundleTests(unittest.TestCase):
	# ================
	# setUp
	#
	# Give every case a complete private artifact and a fresh extraction target.
	# ================
	def setUp(self):
		self.directory = tempfile.TemporaryDirectory()
		self.addCleanup(self.directory.cleanup)
		self.root = Path(self.directory.name)
		self.module = self.root / "module"
		for name, source in FILES.items():
			path = self.module / source
			path.parent.mkdir(parents=True, exist_ok=True)
			path.write_bytes(fixture_file(name, source, "a" * 40))
		self.archive = self.root / "server.tar"
		plan = candidate("server")
		plan.update(commit="a" * 40, release="a" * 40)
		bundle(self.module, self.archive, "a" * 40, plan)

	# ================
	# rewrite
	#
	# Repack a modified archive so the production reader must verify its content.
	# ================
	def rewrite(self, change):
		with tarfile.open(self.archive, "r:") as archive:
			entries = [(member, archive.extractfile(member).read()) for member in archive.getmembers()]
		change(entries)
		with tarfile.open(self.archive, "w") as archive:
			for member, data in entries:
				archive.addfile(member, io.BytesIO(data))

	# ================
	# replace_binary
	#
	# Swap one binary's bytes and re-seal its digest, as a careless or hostile
	# builder would: only the build stamp can tell.
	# ================
	def replace_binary(self, name, data):
		def change(entries):
			for index, (member, content) in enumerate(entries):
				if member.name == name:
					member.size = len(data)
					entries[index] = (member, data)
			for index, (member, content) in enumerate(entries):
				if member.name == "release.json":
					manifest = json.loads(content)
					manifest["files"][name] = hashlib.sha256(data).hexdigest()
					content = json.dumps(manifest, sort_keys=True).encode()
					member.size = len(content)
					entries[index] = (member, content)
		self.rewrite(change)

	# ================
	# test_build_settings_are_read_from_the_bytes
	# ================
	def test_build_settings_are_read_from_the_bytes(self):
		version, settings = go_build_settings(go_binary("agent", "b" * 40, "true"))
		self.assertEqual(version, "go1.27.2")
		self.assertEqual((settings["vcs.revision"], settings["vcs.modified"], settings["-trimpath"]), ("b" * 40, "true", "true"))
		for data in (b"no build information here", GO_BUILDINFO_MAGIC + bytes([8, 0]) + bytes(64), go_binary("agent", "b" * 40)[:80]):
			with self.subTest(data=data[:20]), self.assertRaises(ValueError):
				go_build_settings(data)

	# ================
	# test_unstamped_or_dirty_binaries_are_refused
	#
	# Both ends refuse: bundle before packing, unpack (the host) before
	# writing anything.
	# ================
	def test_unstamped_or_dirty_binaries_are_refused(self):
		for data in (go_binary("gameworld", "b" * 40), go_binary("gameworld", "a" * 40, "true"), b"fixture: gameworld"):
			with self.subTest(data=data[-40:]):
				self.replace_binary("gameworld", data)
				with self.assertRaisesRegex(ValueError, "build information|clean checkout"):
					unpack(self.archive, self.root / "unpacked")
				self.assertFalse((self.root / "unpacked").exists())
				module = self.root / "dirty"
				for name, source in FILES.items():
					(module / source).parent.mkdir(parents=True, exist_ok=True)
					(module / source).write_bytes(data if name == "gameworld" else fixture_file(name, source, "a" * 40))
				plan = candidate("server")
				plan.update(commit="a" * 40, release="a" * 40)
				with self.assertRaisesRegex(ValueError, "build information|clean checkout"):
					bundle(module, self.root / "dirty.tar", "a" * 40, plan)

	# ================
	# test_a_rollback_restores_bytes_without_a_new_stamp
	#
	# A rollback re-stages bytes production already ran; it is not a build.
	# ================
	def test_a_rollback_restores_bytes_without_a_new_stamp(self):
		self.replace_binary("gameworld", b"retained older build")
		def rollback(entries):
			for index, (member, content) in enumerate(entries):
				if member.name == "release.json":
					manifest = json.loads(content)
					manifest["plan"].update(mode="rollback", reason="fixture")
					content = json.dumps(manifest, sort_keys=True).encode()
					member.size = len(content)
					entries[index] = (member, content)
		self.rewrite(rollback)
		unpack(self.archive, self.root / "unpacked")
		self.assertEqual((self.root / "unpacked" / "gameworld").read_bytes(), b"retained older build")

	# ================
	# test_round_trip
	#
	# Receive and unpack exactly the bytes the builder produced.
	# ================
	def test_round_trip(self):
		destination = self.root / "unpacked"
		manifest = unpack(self.archive, destination)
		self.assertEqual(manifest["commit"], "a" * 40)
		for name, source in FILES.items():
			self.assertEqual((destination / name).read_bytes(), (self.module / source).read_bytes())
		with self.archive.open("rb") as stream:
			receive(stream, self.root / "received.tar")
		self.assertEqual(self.archive.read_bytes(), (self.root / "received.tar").read_bytes())

	# ================
	# test_tampering_writes_nothing
	#
	# A digest mismatch must leave the extraction destination absent.
	# ================
	def test_tampering_writes_nothing(self):
		# ================
		# change
		# Corrupt payload bytes without changing their recorded digest.
		# ================
		def change(entries):
			member, data = entries[0]
			entries[0] = (member, b"X" + data[1:])
		self.rewrite(change)
		with self.assertRaisesRegex(ValueError, "digest mismatch"):
			unpack(self.archive, self.root / "unpacked")
		self.assertFalse((self.root / "unpacked").exists())

	# ================
	# test_path_escape
	# Reject archive paths before any directory creation.
	# ================
	def test_path_escape(self):
		# ================
		# change
		# Attempt to replace a declared member with a parent-relative path.
		# ================
		def change(entries):
			entries[0][0].name = "../outside"
		self.rewrite(change)
		with self.assertRaisesRegex(ValueError, "members"):
			unpack(self.archive, self.root / "unpacked")
		self.assertFalse((self.root / "outside").exists())

	# ================
	# test_symlink
	# Treat every non-regular member as invalid, including empty symlinks.
	# ================
	def test_symlink(self):
		# ================
		# change
		# A link must be rejected even when it names an otherwise expected member.
		# ================
		def change(entries):
			member, _ = entries[0]
			member.type = tarfile.SYMTYPE
			member.linkname = "/etc/passwd"
			member.size = 0
			entries[0] = (member, b"")
		self.rewrite(change)
		with self.assertRaisesRegex(ValueError, "non-file"):
			unpack(self.archive, self.root / "unpacked")

	# ================
	# test_duplicate
	# Duplicate tar names must not override a previously verified member.
	# ================
	def test_duplicate(self):
		self.rewrite(lambda entries: entries.append(entries[0]))
		with self.assertRaisesRegex(ValueError, "duplicate"):
			unpack(self.archive, self.root / "unpacked")

	# ================
	# test_missing_manifest_file
	# Every payload must have a corresponding manifest digest.
	# ================
	def test_missing_manifest_file(self):
		# ================
		# change
		# Preserve valid JSON while removing one required digest.
		# ================
		def change(entries):
			member, data = entries[-1]
			manifest = json.loads(data)
			manifest["files"].pop("agent")
			data = json.dumps(manifest).encode()
			member.size = len(data)
			entries[-1] = (member, data)
		self.rewrite(change)
		with self.assertRaisesRegex(ValueError, "file list"):
			unpack(self.archive, self.root / "unpacked")

	# ================
	# test_release_built_before_an_added_file
	#
	# A release retained before a file joined the bundle still unpacks with its
	# own file list; an undeclared member is still refused.
	# ================
	def test_release_built_before_an_added_file(self):
		# ================
		# drop_upgrader
		# Remove the added file from both the members and the manifest.
		# ================
		def drop_upgrader(entries):
			entries[:] = [entry for entry in entries if entry[0].name != "sro-authority-upgrade"]
			member, data = entries[-1]
			manifest = json.loads(data)
			manifest["files"].pop("sro-authority-upgrade")
			data = json.dumps(manifest).encode()
			member.size = len(data)
			entries[-1] = (member, data)
		self.rewrite(drop_upgrader)
		destination = self.root / "unpacked"
		manifest = unpack(self.archive, destination)
		self.assertNotIn("sro-authority-upgrade", manifest["files"])
		self.assertFalse((destination / "sro-authority-upgrade").exists())
		self.assertTrue((destination / "agent").exists())
		# ================
		# stray
		# A member the manifest does not name.
		# ================
		def stray(entries):
			member = tarfile.TarInfo("sro-authority-upgrade")
			member.size = 1
			entries.insert(0, (member, b"x"))
		self.rewrite(stray)
		with self.assertRaisesRegex(ValueError, "members"):
			unpack(self.archive, self.root / "second")


if __name__ == "__main__":
	unittest.main()
