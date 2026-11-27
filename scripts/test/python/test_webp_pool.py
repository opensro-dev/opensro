"""
===========================================================================

test_webp_pool.py - the catalogue's WebP pool encodes each image once

webp_pool.encode_pairs must write every target with the encoder's exact
bytes, encode identical sources once (two workers once raced on one cache
entry and Windows refused the second rename), answer a second run from the
cache, encode again when the settings change, and place each target as a
hard link to its entry (a copy where links fail) without ever writing
through an existing link into the cache.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import os
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

from PIL import Image

import webp_pool  # noqa: E402

# Distinct images and how many copies of each the pairs name.
DISTINCT = 6
COPIES = 5


# ================
# encode_lossless
#
# The encode under test, module-level so the pool's workers import it.
# ================
def encode_lossless(payload, target):
	import io
	with Image.open(io.BytesIO(payload)) as image:
		image.save(target, "WEBP", lossless=True, method=4)


class WebpPoolTest(unittest.TestCase):
	# ================
	# setUp
	#
	# DISTINCT small PNGs, each named by COPIES pairs.
	# ================
	def setUp(self):
		self.root = Path(tempfile.mkdtemp(prefix="sro-webp-pool-"))
		self.pairs = []
		for image in range(DISTINCT):
			for copy in range(COPIES):
				source = self.root / "src" / f"{image}-{copy}.png"
				source.parent.mkdir(parents=True, exist_ok=True)
				Image.new("RGBA", (16, 16), (image * 40, 10, 200, 255)).save(source)
				self.pairs.append([str(source), str(self.root / "out" / str(image) / f"{copy}.webp")])

	def tearDown(self):
		shutil.rmtree(self.root, ignore_errors=True)

	# ================
	# expected
	#
	# What a plain serial encode writes for one source.
	# ================
	def expected(self, source):
		target = self.root / "expected.webp"
		with open(source, "rb") as stream:
			encode_lossless(stream.read(), str(target))
		return target.read_bytes()

	def test_identical_sources_encode_once_and_every_target_matches(self):
		cache = self.root / "cache"
		counts = webp_pool.encode_pairs(self.pairs, encode_lossless, "lossless", str(cache))
		self.assertEqual(counts, {"written": DISTINCT * COPIES, "encoded": DISTINCT, "cached": 0})
		for source, target in self.pairs:
			self.assertEqual(Path(target).read_bytes(), self.expected(source), target)
		self.assertEqual(len(os.listdir(cache)), DISTINCT, "one entry per distinct image, no temporaries")

	def test_a_second_run_copies_from_the_cache(self):
		cache = str(self.root / "cache")
		webp_pool.encode_pairs(self.pairs, encode_lossless, "lossless", cache)
		shutil.rmtree(self.root / "out")
		counts = webp_pool.encode_pairs(self.pairs, encode_lossless, "lossless", cache)
		self.assertEqual(counts, {"written": DISTINCT * COPIES, "encoded": 0, "cached": DISTINCT * COPIES})
		for source, target in self.pairs:
			self.assertEqual(Path(target).read_bytes(), self.expected(source), target)

	def test_changed_settings_encode_again(self):
		cache = str(self.root / "cache")
		webp_pool.encode_pairs(self.pairs, encode_lossless, "lossless", cache)
		counts = webp_pool.encode_pairs(self.pairs, encode_lossless, "lossless v2", cache)
		self.assertEqual(counts["encoded"], DISTINCT)

	def test_without_a_cache_targets_still_match(self):
		counts = webp_pool.encode_pairs(self.pairs, encode_lossless, "lossless", None)
		self.assertEqual(counts["encoded"], DISTINCT)
		for source, target in self.pairs:
			self.assertEqual(Path(target).read_bytes(), self.expected(source), target)
		leftovers = [name for _, _, names in os.walk(self.root / "out") for name in names if name.startswith("tmp")]
		self.assertEqual(leftovers, [])

	def test_targets_are_the_cache_entry_on_one_volume(self):
		cache = self.root / "cache"
		webp_pool.encode_pairs(self.pairs, encode_lossless, "lossless", str(cache))
		entries = [cache / name for name in os.listdir(cache)]
		for _, target in self.pairs:
			self.assertTrue(any(os.path.samefile(target, entry) for entry in entries), target)

	def test_placing_over_a_linked_target_leaves_the_entry_alone(self):
		entry, other, target = self.root / "entry.webp", self.root / "other.webp", self.root / "target.webp"
		entry.write_bytes(b"entry")
		other.write_bytes(b"other")
		os.link(entry, target)
		webp_pool.place(str(other), str(target))
		self.assertEqual(entry.read_bytes(), b"entry")
		self.assertEqual(target.read_bytes(), b"other")

	def test_a_volume_without_links_gets_a_copy(self):
		entry, target = self.root / "entry.webp", self.root / "target.webp"
		entry.write_bytes(b"entry")
		original = os.link

		def refuse(source, destination):
			raise OSError(18, "cross-device link")

		os.link = refuse
		try:
			webp_pool.place(str(entry), str(target))
		finally:
			os.link = original
		self.assertEqual(target.read_bytes(), b"entry")
		self.assertFalse(os.path.samefile(entry, target))


if __name__ == "__main__":
	unittest.main()
