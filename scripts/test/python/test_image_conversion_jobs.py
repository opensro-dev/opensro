"""
===========================================================================

test_image_conversion_jobs.py - image conversion is the same at any width

convert_images.py converts in a process pool sized by SRO_BUILD_JOBS. The
pool must change only the speed: one process and several write the same
images and the same manifest, in input order. Runs main() in-process
against a scratch game root and generated root, without the shared lock.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import os
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

from PIL import Image

SCRATCH = Path(tempfile.mkdtemp(prefix="sro-convert-"))
os.environ["SRO_GAME_ROOT"] = str(SCRATCH / "game")
os.environ["SRO_GENERATED_ROOT"] = str(SCRATCH / "generated")
sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import convert_images  # noqa: E402

SOURCE_COUNT = 40


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
		write_sources()

	@classmethod
	def tearDownClass(cls) -> None:
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
