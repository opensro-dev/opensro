"""
===========================================================================

test_image_conversion_jobs.py - image conversion is the same at any width

convert_images.py converts in a process pool sized by SRO_BUILD_JOBS. The
pool must change only the speed: one process and several write the same
images and the same manifest, in input order. Runs main() in-process
against a scratch game root and generated root, without the shared lock.

The roots are bound in setUpClass, never at import: unittest discover
imports every test module first, and an earlier module that imported
sro_paths had already fixed the real roots, so this test once converted the
whole licensed corpus (18,221 images) into the checkout's own .generated.
convert_images therefore refuses to run unless its roots sit in SCRATCH.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import importlib
import os
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

from PIL import Image

SCRATCH = Path(tempfile.mkdtemp(prefix="sro-convert-"))
SCRATCH_ENV = {"SRO_GAME_ROOT": str(SCRATCH / "game"), "SRO_GENERATED_ROOT": str(SCRATCH / "generated")}
TOUCHED_ENV = [*SCRATCH_ENV, "SRO_BUILD_JOBS", "SRO_FORCE_IMAGE_CONVERT"]
sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import sro_paths  # noqa: E402

SOURCE_COUNT = 40
convert_images = None


# ================
# bind_roots
#
# Re-resolve sro_paths under the current environment and (re)load
# convert_images on top of it, so its module-level roots follow.
# ================
def bind_roots():
	importlib.reload(sro_paths)
	module = sys.modules.get("convert_images")
	return importlib.reload(module) if module else importlib.import_module("convert_images")


# ================
# write_sources
# ================
def write_sources() -> None:
	root = SCRATCH / "game" / "extracted" / "Media_extracted" / "icon"
	root.mkdir(parents=True, exist_ok=True)
	for index in range(SOURCE_COUNT):
		image = Image.new("RGBA", (8, 8), (index * 5 % 256, 40, 200 - index, 255))
		image.save(root / f"icon_{index:02}.tga")


# ================
# convert
# ================
def convert(jobs: str) -> tuple[bytes, dict[str, bytes]]:
	os.environ["SRO_BUILD_JOBS"] = jobs
	os.environ["SRO_FORCE_IMAGE_CONVERT"] = "1"
	sys.argv = ["convert_images.py"]
	if convert_images.main() != 0:
		raise AssertionError(f"conversion failed with SRO_BUILD_JOBS={jobs}")
	outputs = {
		path.relative_to(convert_images.OUTPUT_ROOT).as_posix(): path.read_bytes()
		for path in sorted(convert_images.OUTPUT_ROOT.rglob("*.png"))
	}
	return convert_images.MANIFEST_PATH.read_bytes(), outputs


class ImageConversionJobsTest(unittest.TestCase):
	@classmethod
	def setUpClass(cls) -> None:
		global convert_images
		cls.saved_env = {name: os.environ.get(name) for name in TOUCHED_ENV}
		os.environ.update(SCRATCH_ENV)
		convert_images = bind_roots()
		for root in (convert_images.EXTRACTED_ROOT, convert_images.OUTPUT_ROOT, convert_images.MANIFEST_PATH):
			if not Path(root).resolve().is_relative_to(SCRATCH.resolve()):
				raise RuntimeError(f"convert_images is bound to {root}, outside the scratch root {SCRATCH}")
		write_sources()

	@classmethod
	def tearDownClass(cls) -> None:
		for name, value in cls.saved_env.items():
			if value is None:
				os.environ.pop(name, None)
			else:
				os.environ[name] = value
		bind_roots()
		shutil.rmtree(SCRATCH, ignore_errors=True)

	def test_one_process_and_many_write_the_same_bytes(self) -> None:
		serial_manifest, serial_outputs = convert("1")
		parallel_manifest, parallel_outputs = convert("3")
		self.assertEqual(len(serial_outputs), SOURCE_COUNT)
		self.assertEqual(serial_manifest, parallel_manifest)
		self.assertEqual(serial_outputs, parallel_outputs)

	def test_the_job_count_must_be_a_positive_integer(self) -> None:
		for raw in ("0", "-1", "two"):
			os.environ["SRO_BUILD_JOBS"] = raw
			with self.assertRaises(SystemExit):
				convert_images.build_jobs()
		os.environ["SRO_BUILD_JOBS"] = ""
		self.assertEqual(convert_images.build_jobs(), max(1, (os.cpu_count() or 2) - 1))


if __name__ == "__main__":
	unittest.main()
