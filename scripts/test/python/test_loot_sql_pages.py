"""
===========================================================================

test_loot_sql_pages.py - the backup reader's catalog-driven rows

Builds a small backup in memory: the five catalog tables the reader
consults (sysschobjs, sysrowsets, sysallocunits, syscolpars, sysrscols)
and one data table, then reads it back. The data table keeps its key
column ahead of a lower-numbered one, as the shard's _RefSkill does, so
a reader that walks the fixed columns in column order reads the wrong
bytes and fails here.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import struct
import tempfile
import unittest
from pathlib import Path

import loot_sql_pages as pages  # noqa: E402

TABLE = "_RefSample"
OBJECT_ID = 900
ROWSET_ID = 72057594058375168
DATA_ALLOCATION = 1234
# A primary record (type 0) with a null bitmap and variable fields.
STATUS = 0x10 | 0x20


# ================
# record
#
# One record: status, the fixed part, the column count, the null bitmap
# and the variable fields with their end offsets.
# ================
def record(fixed, columns, texts=(), nulls=()):
	bitmap = bytearray((columns + 7) // 8)
	for bit in nulls:
		bitmap[bit // 8] |= 1 << (bit % 8)
	head = bytes([STATUS, 0]) + struct.pack("<H", 4 + len(fixed)) + fixed
	body = head + struct.pack("<H", columns) + bytes(bitmap) + struct.pack("<H", len(texts))
	end = len(body) + 2 * len(texts)
	ends = b""
	for text in texts:
		end += len(text)
		ends += struct.pack("<H", end)
	return body + ends + b"".join(texts)


# ================
# fixed_record
#
# A catalog record: the given fields at their offsets, padded to the
# fixed length the reader checks, with variable fields after it.
# ================
def fixed_record(length, fields, texts=()):
	fixed = bytearray(length - 4)
	for offset, form, value in fields:
		struct.pack_into(form, fixed, offset - 4, value)
	return record(bytes(fixed), 1 + len(texts), texts)


# ================
# page
# ================
def page(allocation, rows):
	raw = bytearray(pages.PAGE_SIZE)
	raw[0], raw[1] = 1, 1
	struct.pack_into("<H", raw, 22, len(rows))
	struct.pack_into("<I", raw, 24, allocation)
	at = pages.PAGE_HEADER_SIZE
	for index, row in enumerate(rows):
		raw[at:at + len(row)] = row
		struct.pack_into("<H", raw, pages.PAGE_SIZE - 2 - 2 * index, at)
		at += len(row)
	return bytes(raw)


# ================
# catalog
#
# The catalog pages for one table whose columns are (name, type id,
# length, offset or -variable index, null bit).
# ================
def catalog(columns):
	name = TABLE.encode("utf-16le")
	return [
		page(34, [fixed_record(8, [(4, "<I", OBJECT_ID)], [name])]),
		page(5, [fixed_record(57, [(4, "<Q", ROWSET_ID), (13, "<I", OBJECT_ID), (17, "<I", 1)])]),
		page(7, [fixed_record(73, [(4, "<Q", DATA_ALLOCATION << 16), (12, "<B", 1), (13, "<Q", ROWSET_ID)])]),
		page(41, [fixed_record(40, [(4, "<I", OBJECT_ID), (10, "<I", index + 1), (14, "<B", kind), (19, "<h", length)],
			[label.encode("utf-16le")]) for index, (label, kind, length, _, _) in enumerate(columns)]),
		page(3, [fixed_record(60, [(4, "<Q", ROWSET_ID), (12, "<I", index + 1), (44, "<h", offset), (48, "<h", bit)])
			for index, (_, _, _, offset, bit) in enumerate(columns)]),
	]


# Service (tinyint) is column 1, ID (int) column 2 but stored first;
# Code and Name are the first and second variable fields.
COLUMNS = [("Service", 48, 1, 8, 1), ("ID", 56, 4, 4, 2), ("Code", 167, 128, -1, 3), ("Name", 167, 128, -2, 4)]


# ================
# sample_row
# ================
def sample_row(identity, service, texts, nulls=()):
	return record(struct.pack("<iB", identity, service), len(COLUMNS), texts, nulls)


# ================
# TypedRowsTests
# ================
class TypedRowsTests(unittest.TestCase):
	# ================
	# read
	# ================
	def read(self, data_rows, columns=COLUMNS):
		with tempfile.TemporaryDirectory() as root:
			path = Path(root) / "sample.bak"
			path.write_bytes(b"".join(catalog(columns) + [page(DATA_ALLOCATION, data_rows)]))
			backup = pages.Backup(path)
			try:
				return list(backup.table_rows(TABLE))
			finally:
				backup.close()

	# ================
	# test_fixed_columns_read_at_their_catalog_offsets
	# ================
	def test_fixed_columns_read_at_their_catalog_offsets(self):
		rows = self.read([sample_row(124, 1, ["SKILL_A".encode("cp949"), "불꽃".encode("cp949")])])
		self.assertEqual(rows, [{"Service": 1, "ID": 124, "Code": "SKILL_A", "Name": "불꽃"}])

	# ================
	# test_an_omitted_trailing_text_is_empty_and_a_null_is_none
	# ================
	def test_an_omitted_trailing_text_is_empty_and_a_null_is_none(self):
		rows = self.read([sample_row(7, 0, [b"ONLY_CODE"]), sample_row(8, 1, [b"X", b"Y"], nulls=[0])])
		self.assertEqual(rows[0], {"Service": 0, "ID": 7, "Code": "ONLY_CODE", "Name": ""})
		self.assertIsNone(rows[1]["Service"])
		self.assertEqual(rows[1]["Name"], "Y")

	# ================
	# test_an_unknown_type_or_layout_refuses
	# ================
	def test_an_unknown_type_or_layout_refuses(self):
		with self.assertRaisesRegex(ValueError, "Unsupported column type"):
			self.read([sample_row(1, 1, [b"A", b"B"])], COLUMNS[:3] + [("When", 61, 8, -2, 4)])
		with self.assertRaisesRegex(ValueError, "row layout"):
			self.read([record(struct.pack("<i", 1), len(COLUMNS), [b"A", b"B"])])
		with self.assertRaisesRegex(ValueError, "Missing reference table"):
			with tempfile.TemporaryDirectory() as root:
				path = Path(root) / "empty.bak"
				path.write_bytes(b"".join(catalog(COLUMNS)))
				backup = pages.Backup(path)
				try:
					backup.columns("_RefOther")
				finally:
					backup.close()


if __name__ == "__main__":
	unittest.main()
