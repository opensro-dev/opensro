"""
===========================================================================

test_shared_assets.py - preserve immutable URLs across gzip representations.

Windows and Linux gzip headers can describe identical decoded application
bytes. Shared HTTP assets retain their first valid encoding while candidate
directories continue to preserve the exact bytes covered by their manifests.

===========================================================================
"""

import gzip
from pathlib import Path
import tempfile
import unittest

from client_deploy import retain_shared_asset

ASSET = "assets/entry-abcdefgh.js"
ENCODED = ASSET + ".gz"
BODY = b"export const fixture = 'same application bytes';\n" * 100
GZIP_OS_OFFSET = 9
GZIP_WINDOWS = 10
GZIP_UNIX = 3


# ================
# SharedAssetTests
#
# Real files exercise the shared-store boundary without needing POSIX symlinks.
# ================
class SharedAssetTests(unittest.TestCase):
	# ================
	# setUp
	# ================
	def setUp(self):
		directory = tempfile.TemporaryDirectory()
		self.addCleanup(directory.cleanup)
		self.root = Path(directory.name)
		self.outputs = {ASSET: BODY, ENCODED: gzip.compress(BODY, mtime=0)}

	# ================
	# test_first_representation_and_identical_retry_preserve_bytes
	# ================
	def test_first_representation_and_identical_retry_preserve_bytes(self):
		for name in self.outputs:
			retain_shared_asset(self.root, name, self.outputs)
			retain_shared_asset(self.root, name, self.outputs)
			self.assertEqual((self.root / name).read_bytes(), self.outputs[name])

	# ================
	# test_platform_header_changes_preserve_the_existing_representation
	# ================
	def test_platform_header_changes_preserve_the_existing_representation(self):
		windows = bytearray(self.outputs[ENCODED])
		windows[GZIP_OS_OFFSET] = GZIP_WINDOWS
		linux = bytearray(windows)
		linux[GZIP_OS_OFFSET] = GZIP_UNIX
		self.outputs[ENCODED] = bytes(windows)
		for name in self.outputs:
			retain_shared_asset(self.root, name, self.outputs)
		self.outputs[ENCODED] = bytes(linux)
		retain_shared_asset(self.root, ENCODED, self.outputs)
		self.assertEqual((self.root / ENCODED).read_bytes(), bytes(windows))
		self.assertEqual(gzip.decompress(bytes(linux)), BODY)

	# ================
	# test_compression_level_changes_preserve_the_existing_representation
	# ================
	def test_compression_level_changes_preserve_the_existing_representation(self):
		retained = gzip.compress(BODY, compresslevel=1, mtime=1)
		self.outputs[ENCODED] = retained
		retain_shared_asset(self.root, ENCODED, self.outputs)
		self.outputs[ENCODED] = gzip.compress(BODY, compresslevel=9, mtime=2)
		self.assertNotEqual(self.outputs[ENCODED], retained)
		retain_shared_asset(self.root, ENCODED, self.outputs)
		self.assertEqual((self.root / ENCODED).read_bytes(), retained)

	# ================
	# test_raw_collision_rejects_without_overwriting_the_retained_asset
	# ================
	def test_raw_collision_rejects_without_overwriting_the_retained_asset(self):
		retain_shared_asset(self.root, ASSET, self.outputs)
		self.outputs[ASSET] = b"different application"
		with self.assertRaisesRegex(ValueError, "filename collision"):
			retain_shared_asset(self.root, ASSET, self.outputs)
		self.assertEqual((self.root / ASSET).read_bytes(), BODY)

	# ================
	# test_wrong_or_oversized_decoded_content_rejects
	# ================
	def test_wrong_or_oversized_decoded_content_rejects(self):
		for body in (b"different application", BODY + b"extra"):
			with self.subTest(size=len(body)):
				path = self.root / ENCODED
				path.parent.mkdir(parents=True, exist_ok=True)
				path.write_bytes(gzip.compress(body, mtime=0))
				with self.assertRaisesRegex(ValueError, "filename collision"):
					retain_shared_asset(self.root, ENCODED, self.outputs)

	# ================
	# test_corrupt_encoding_rejects_without_repairing_shared_bytes
	# ================
	def test_corrupt_encoding_rejects_without_repairing_shared_bytes(self):
		retain_shared_asset(self.root, ENCODED, self.outputs)
		path = self.root / ENCODED
		path.write_bytes(b"not a gzip member")
		with self.assertRaisesRegex(ValueError, "encoding is corrupt"):
			retain_shared_asset(self.root, ENCODED, self.outputs)
		self.assertEqual(path.read_bytes(), b"not a gzip member")


if __name__ == "__main__":
	unittest.main()
