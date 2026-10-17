"""
===========================================================================
test_cap110_avatars.py - the cap-110 avatar importer's row handling

Synthetic rows exercise set matching, CRLF-only record splitting (a bare LF
belongs to a cell) and the donor-to-extraction Item Mall row adaptation.
===========================================================================
"""

import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2]))
import import_cap110_avatars as importer


# ================
# SetPatternTests
# ================
class SetPatternTests(unittest.TestCase):
	# ================
	# test_matches_dress_hat_attach_and_recolours
	# ================
	def test_matches_dress_hat_attach_and_recolours(self):
		pattern = importer.set_pattern("piratecrew")
		for name in ("avatar_m_piratecrew", "avatar_w_piratecrew_hat", "avatar_m_piratecrew_attach",
			"avatar_m_piratecrew_2", "avatar_w_piratecrew_hat_2", "AVATAR_M_PIRATECREW"):
			self.assertTrue(pattern.search(name), name)

	# ================
	# test_leaves_other_sets_alone
	# ================
	def test_leaves_other_sets_alone(self):
		pattern = importer.set_pattern("piratecrew")
		for name in ("avatar_m_pirate", "avatar_m_piratecrew2", "avatar_m_piratecrew_hat2", "avatar_m_piratecrew_x"):
			self.assertIsNone(pattern.search(name), name)


# ================
# RecordTests
# ================
class RecordTests(unittest.TestCase):
	# ================
	# test_bare_lf_stays_inside_its_cell
	# ================
	def test_bare_lf_stays_inside_its_cell(self):
		text = "1\tA\tfirst\nsecond\r\n1\tB\tthird\r\n"
		self.assertEqual(importer.records(text), ["1\tA\tfirst\nsecond", "1\tB\tthird"])

	# ================
	# test_encoding_round_trips
	# ================
	def test_encoding_round_trips(self):
		rows = ["1\tA\tone\ntwo", "1\tB\té"]
		data = importer.encode_text(rows)
		self.assertEqual(data[:2], b"\xff\xfe")
		self.assertEqual(importer.records(importer.read_text(data)), rows)


# ================
# PriceTests
# ================
class PriceTests(unittest.TestCase):
	# ================
	# test_follows_the_v1150_outfit_tiers
	# ================
	def test_follows_the_v1150_outfit_tiers(self):
		self.assertEqual(importer.silk_price("PACKAGE_ITEM_MALL_AVATAR_M_PIRATECREW"), "130")
		self.assertEqual(importer.silk_price("PACKAGE_ITEM_MALL_AVATAR_W_PIRATECREW_2"), "130")
		self.assertEqual(importer.silk_price("PACKAGE_ITEM_MALL_AVATAR_M_PIRATECREW_HAT"), "30")
		self.assertEqual(importer.silk_price("PACKAGE_ITEM_MALL_AVATAR_W_PIRATECREW_HAT_2"), "30")
		self.assertEqual(importer.silk_price("PACKAGE_ITEM_MALL_AVATAR_M_PIRATECREW_ATTACH"), "15")


if __name__ == "__main__":
	unittest.main()
