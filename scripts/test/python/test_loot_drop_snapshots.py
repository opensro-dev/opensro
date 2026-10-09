"""
===========================================================================
test_loot_drop_snapshots.py - drop assignment provenance at the SQL import

A backup keeps older generations of a _RefDropItemAssign row on other
pages, sometimes enabled together. The importer keeps every generation with
its source item id, page offset, record offset and page LSN, so the catalog
generator can choose between them and audit the choice.
===========================================================================
"""

import pathlib
import struct
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2] / "build"))
from import_loot_evidence import PAGE_LSN_OFFSET, read_assignments

ASSIGN_RECORD_AT = 96


# ================
# AssignBackup
#
# One synthetic page per row: (page offset, LSN triple, six row ints).
# ================
class AssignBackup:
	# ================
	# __init__
	# ================
	def __init__(self, rows):
		self.rows = rows

	# ================
	# named_rows
	# ================
	def named_rows(self, name):
		assert name == "_RefDropItemAssign"
		for offset, lsn, values in self.rows:
			page = bytearray(8192)
			struct.pack_into("<IIH", page, PAGE_LSN_OFFSET, *lsn)
			struct.pack_into("<6i", page, ASSIGN_RECORD_AT + 4, *values)
			yield offset, ASSIGN_RECORD_AT, bytes(page)


# ================
# AssignmentProvenanceTest
# ================
class AssignmentProvenanceTest(unittest.TestCase):
	# ================
	# test_conflicting_enabled_rows_keep_every_generation
	# ================
	def test_conflicting_enabled_rows_keep_every_generation(self):
		backup = AssignBackup([
			(0x6000, (900, 12, 3), (1, 7001, 40, 0, 2, 1)),
			(0x2000, (500, 4, 1), (1, 7001, 25, 0, 2, 1)),
			(0x4000, (900, 12, 3), (1, 7001, 30, 0, 2, 1)),
		])
		rows = read_assignments(backup, {7001: "ITEM_TEST"}, {"items": {"ITEM_TEST"}})
		self.assertEqual([(row["page"], row["record"], row["lsn"], row["weight"]) for row in rows], [
			(0x2000, ASSIGN_RECORD_AT, [500, 4, 1], 25),
			(0x4000, ASSIGN_RECORD_AT, [900, 12, 3], 30),
			(0x6000, ASSIGN_RECORD_AT, [900, 12, 3], 40),
		])
		self.assertTrue(all(row["item"] == 7001 and row["service"] == 1 and row["client"] for row in rows))

	# ================
	# test_unknown_item_keeps_its_source_id
	# ================
	def test_unknown_item_keeps_its_source_id(self):
		rows = read_assignments(AssignBackup([(0x2000, (1, 1, 1), (0, 9999, 5, 0, -1, 1))]), {}, {"items": set()})
		self.assertEqual((rows[0]["codename"], rows[0]["item"], rows[0]["client"]), (None, 9999, False))


if __name__ == "__main__":
	unittest.main()
