"""
===========================================================================
test_caravan_evidence.py - continent normalization at the SQL import boundary

Synthetic SQL rows exercise the same decoder as the real backup. Expected
zones follow Caravan_GetContinentZone's ten case-insensitive comparisons.
===========================================================================
"""

import pathlib
import struct
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2] / "build"))
from import_caravan_evidence import read_zones


# ================
# RegionBackup
# ================
class RegionBackup:
	# ================
	# __init__
	# ================
	def __init__(self, continents):
		self.continents = continents

	# ================
	# named_rows
	# ================
	def named_rows(self, name):
		assert name == "_RefRegion"
		for region, continent in enumerate(self.continents, start=1):
			page = bytearray(8192)
			at = 96
			encoded = continent.encode("ascii")
			struct.pack_into("<H", page, at + 2, 8)
			struct.pack_into("<H", page, at + 4, region)
			struct.pack_into("<H", page, at + 8, 1)
			struct.pack_into("<H", page, at + 11, 1)
			struct.pack_into("<H", page, at + 13, 15 + len(encoded))
			page[at + 15:at + 15 + len(encoded)] = encoded
			yield 0, at, bytes(page)


# ================
# CaravanContinentTests
# ================
class CaravanContinentTests(unittest.TestCase):
	# ================
	# test_native_case_insensitive_continents
	# ================
	def test_native_case_insensitive_continents(self):
		for continent, expected in (
			("CHINA", 0), ("West_China", 0), ("Oasis_Kingdom", 0), ("Roc", 0),
			("Eu", 1), ("Am", 1), ("Ca", 1), ("DELTA", 1), ("SD", 1), ("KingsValley", 1),
			("Pharaoh", 2), ("CHINA_SUFFIX", 2), ("", 2),
		):
			with self.subTest(continent=continent):
				variants = [continent, continent.lower(), continent.upper(), continent.swapcase()]
				self.assertEqual(read_zones(RegionBackup(variants)), {index: expected for index in range(1, 5)})


if __name__ == "__main__":
	unittest.main()
