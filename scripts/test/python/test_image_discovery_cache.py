"""
===========================================================================

test_image_discovery_cache.py - image discovery opens only new or changed files

convert_images.discover_assets classified every candidate by reading its
header, about 7.5 ms per file on the build PC (186 s per asset build). The
discovery cache keeps each answer by path, size and mtime: an unchanged
file is not opened again, a changed one is, and a full scan drops files
that left the tree. convert_images refuses to run unless its roots sit in
SCRATCH (as test_image_conversion_jobs.py).

===========================================================================
"""
import importlib
import os
from pathlib import Path
import shutil
import sys
import tempfile
import unittest

import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import sro_paths  # noqa: E402

SCRATCH = Path(tempfile.mkdtemp(prefix="sro-discovery-"))
SCRATCH_ENV = {"SRO_GAME_ROOT": str(SCRATCH / "game"), "SRO_GENERATED_ROOT": str(SCRATCH / "generated")}
DDJ_HEADER = b"JMXVDDJ 1000" + b"\0" * 8
convert_images = None


# ================
# bind_roots
# ================
def bind_roots():
	importlib.reload(sro_paths)
	module = sys.modules.get("convert_images")
	return importlib.reload(module) if module else importlib.import_module("convert_images")


class ImageDiscoveryCacheTest(unittest.TestCase):
	@classmethod
	def setUpClass(cls) -> None:
		global convert_images
		cls.saved_env = {name: os.environ.get(name) for name in SCRATCH_ENV}
		os.environ.update(SCRATCH_ENV)
		convert_images = bind_roots()
		for root in (convert_images.EXTRACTED_ROOT, convert_images.DISCOVERY_CACHE_PATH):
			if not Path(root).resolve().is_relative_to(SCRATCH.resolve()):
				raise RuntimeError(f"convert_images is bound to {root}, outside the scratch root {SCRATCH}")

	@classmethod
	def tearDownClass(cls) -> None:
		for name, value in cls.saved_env.items():
			if value is None:
				os.environ.pop(name, None)
			else:
				os.environ[name] = value
		bind_roots()
		shutil.rmtree(SCRATCH, ignore_errors=True)

	def setUp(self) -> None:
		self.root = convert_images.EXTRACTED_ROOT / "Media_extracted" / "icon"
		shutil.rmtree(convert_images.EXTRACTED_ROOT, ignore_errors=True)
		convert_images.DISCOVERY_CACHE_PATH.unlink(missing_ok=True)
		self.root.mkdir(parents=True)
		(self.root / "a.ddj").write_bytes(DDJ_HEADER + b"a")
		(self.root / "b.ddj").write_bytes(DDJ_HEADER + b"b")
		(self.root / "not.ddj").write_bytes(b"not a texture")
		self.opened = []
		original = convert_images.is_image_asset

		def counting(path):
			self.opened.append(path.name)
			return original(path)

		convert_images.is_image_asset = counting
		self.addCleanup(setattr, convert_images, "is_image_asset", original)

	def names(self):
		return sorted(asset.relative.name for asset in convert_images.discover_assets())

	def test_an_unchanged_tree_is_not_opened_again(self):
		self.assertEqual(self.names(), ["a.ddj", "b.ddj"])
		self.assertEqual(sorted(self.opened), ["a.ddj", "b.ddj", "not.ddj"])
		self.opened.clear()
		self.assertEqual(self.names(), ["a.ddj", "b.ddj"])
		self.assertEqual(self.opened, [])

	def test_a_changed_file_is_classified_again(self):
		self.names()
		self.opened.clear()
		(self.root / "not.ddj").write_bytes(DDJ_HEADER + b"now a texture")
		self.assertEqual(self.names(), ["a.ddj", "b.ddj", "not.ddj"])
		self.assertEqual(self.opened, ["not.ddj"])

	def test_a_removed_file_leaves_the_cache(self):
		self.names()
		(self.root / "b.ddj").unlink()
		self.assertEqual(self.names(), ["a.ddj"])
		self.assertNotIn("Media_extracted/icon/b.ddj", convert_images.load_discovery_cache())

	def test_a_foreign_cache_is_a_cold_discovery(self):
		convert_images.DISCOVERY_CACHE_PATH.parent.mkdir(parents=True, exist_ok=True)
		convert_images.DISCOVERY_CACHE_PATH.write_text('{"format": "other"}', encoding="utf-8")
		self.assertEqual(self.names(), ["a.ddj", "b.ddj"])
		self.assertEqual(len(self.opened), 3)


if __name__ == "__main__":
	unittest.main()
