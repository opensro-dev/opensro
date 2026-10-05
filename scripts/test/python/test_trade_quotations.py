"""
===========================================================================
test_trade_quotations.py - SQL quotation admission and row integrity

Synthetic fixed SQL records exercise the production importer without a
licensed backup. Active/disabled rows and conflicting keys are independent.
===========================================================================
"""

import pathlib
import struct
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2] / "build"))
from import_trade_quotations import read_quotations

VALID_ROW = (1, 1, 2010, 2151, 1.1, 1.05, 1.2, 50000, 250, 50000)


# ================
# QuotationBackup
# ================
class QuotationBackup:
	# ================
	# __init__
	# ================
	def __init__(self, rows, fixed_end=44, columns=10, nulls=0):
		self.rows = rows
		self.fixed_end = fixed_end
		self.columns = columns
		self.nulls = nulls

	# ================
	# named_rows
	# ================
	def named_rows(self, name):
		assert name == "_ItemQuotation"
		for row in self.rows:
			page = bytearray(8192)
			at = 96
			struct.pack_into("<H", page, at + 2, self.fixed_end)
			struct.pack_into("<4i3f3i", page, at + 4, *row)
			struct.pack_into("<HH", page, at + 44, self.columns, self.nulls)
			yield 0, at, bytes(page)


# ================
# TradeQuotationTests
# ================
class TradeQuotationTests(unittest.TestCase):
	# ================
	# test_keeps_float32_and_skips_native_disabled_rows
	# ================
	def test_keeps_float32_and_skips_native_disabled_rows(self):
		disabled = list(VALID_ROW)
		disabled[1] = 0
		empty = list(VALID_ROW)
		empty[7] = 0
		rows = read_quotations(QuotationBackup([VALID_ROW, VALID_ROW, disabled, empty]))
		self.assertEqual(len(rows), 1)
		self.assertEqual(rows[0]["base"], struct.unpack("<f", struct.pack("<f", 1.1))[0])
		self.assertEqual((rows[0]["npc"], rows[0]["item"]), (2010, 2151))

	# ================
	# test_conflicting_keys_and_ids_are_rejected
	# ================
	def test_conflicting_keys_and_ids_are_rejected(self):
		for column, value in ((0, 2), (2, 2059), (3, 2152), (4, 1.15), (9, 50001)):
			row = list(VALID_ROW)
			row[column] = value
			with self.subTest(column=column), self.assertRaisesRegex(ValueError, "Conflicting"):
				read_quotations(QuotationBackup([VALID_ROW, row]))

	# ================
	# test_bad_layout_and_nulls_are_rejected
	# ================
	def test_bad_layout_and_nulls_are_rejected(self):
		for options in ({"fixed_end": 40}, {"columns": 9}, {"nulls": 1}, {"nulls": 512}):
			with self.subTest(options=options), self.assertRaises(ValueError):
				read_quotations(QuotationBackup([VALID_ROW], **options))

	# ================
	# test_bad_authority_and_empty_catalog_are_rejected
	# ================
	def test_bad_authority_and_empty_catalog_are_rejected(self):
		for column, value in ((0, 0), (2, -1), (4, float("nan")), (5, 2), (6, float("inf")), (8, 0), (9, -1)):
			row = list(VALID_ROW)
			row[column] = value
			with self.subTest(column=column), self.assertRaises(ValueError):
				read_quotations(QuotationBackup([row]))
		with self.assertRaisesRegex(ValueError, "Empty"):
			read_quotations(QuotationBackup([]))


if __name__ == "__main__":
	unittest.main()
