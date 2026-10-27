"""
===========================================================================

test_import_vsro_optional_teleport.py - the reverse return map table importer

Synthetic tables, no game data: row typing and English names, refusal of
a malformed or repeated row, and the sha256 pins.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import hashlib
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import import_vsro_optional_teleport as importer  # noqa: E402


# ================
# utf16_table
# ================
def utf16_table(rows):
	text = "\r\n".join("\t".join(row) for row in rows) + "\r\n"
	return b"\xff\xfe" + text.encode("utf-16-le")


# ================
# teleport_row
# ================
def teleport_row(identity, zone, region, map_point="1", group="-1", level_min="0", level_max="0"):
	return ["1", str(identity), "korean", zone, str(region), "100", "5", "200", "1", group, map_point,
		level_min, level_max, "-1", "xxx", "-1", "xxx", "-1", "xxx"]


NAMES = utf16_table([
	["//Service", "CodeName128", "Korean", "", "", "", "", "", "English"],
	["1", "SN_ZONE_22001", "k", "", "", "", "", "", "Jangan"],
	["1", "SN_ZONE_25057", "k", "", "", "", "", "", "Test Ruins"]
])


class ParseTests(unittest.TestCase):
	def test_rows_are_typed_named_and_sorted_by_id(self):
		table = utf16_table([
			teleport_row(26, "SN_ZONE_22001", 25000),
			teleport_row(4, "SN_ZONE_25057", 26100, level_min="95", level_max="999"),
			teleport_row(29, "xxx", 26200, map_point="1", group="1")
		])
		rows = importer.parse_optional_teleport(table, NAMES)
		self.assertEqual([row["id"] for row in rows], [4, 26, 29])
		self.assertEqual(rows[0]["name"], "Test Ruins")
		self.assertEqual((rows[0]["levelMin"], rows[0]["levelMax"]), (95, 999))
		self.assertEqual((rows[1]["regionId"], rows[1]["x"], rows[1]["y"], rows[1]["z"]), (25000, 100.0, 5.0, 200.0))
		# A code with no English row keeps an empty name; the server decides.
		self.assertEqual((rows[2]["name"], rows[2]["regionGroup"]), ("", 1))

	def test_a_short_row_refuses_the_table(self):
		table = utf16_table([teleport_row(1, "SN_ZONE_22001", 25000)[:18]])
		with self.assertRaisesRegex(ValueError, "columns"):
			importer.parse_optional_teleport(table, NAMES)

	def test_a_repeated_id_refuses_the_table(self):
		table = utf16_table([teleport_row(1, "SN_ZONE_22001", 25000), teleport_row(1, "SN_ZONE_25057", 26100)])
		with self.assertRaisesRegex(ValueError, "new positive id"):
			importer.parse_optional_teleport(table, NAMES)

	def test_a_non_numeric_cell_refuses_the_table(self):
		row = teleport_row(1, "SN_ZONE_22001", 25000)
		row[5] = "east"
		with self.assertRaisesRegex(ValueError, "row 1"):
			importer.parse_optional_teleport(utf16_table([row]), NAMES)


class PinTests(unittest.TestCase):
	def test_the_pinned_bytes_pass(self):
		payload = b"table"
		importer.check_pins({"a": payload}, {"a": hashlib.sha256(payload).hexdigest()})

	def test_other_bytes_are_refused(self):
		with self.assertRaisesRegex(SystemExit, "not the pinned"):
			importer.check_pins({"a": b"edited"}, {"a": hashlib.sha256(b"table").hexdigest()})

	def test_a_missing_file_is_refused(self):
		with self.assertRaisesRegex(SystemExit, "lacks"):
			importer.check_pins({}, {"a": "00"})


class MainPinTests(unittest.TestCase):
	def setUp(self):
		importer.stage1._verified_archives.clear()
		self.root = tempfile.TemporaryDirectory()
		self.addCleanup(self.root.cleanup)
		self.vsro = Path(self.root.name)

	def run_main(self):
		argv = ["import_vsro_optional_teleport.py", "--vsro-root", str(self.vsro)]
		with patch.object(sys, "argv", argv):
			importer.main()

	def test_main_refuses_a_client_that_is_not_the_pinned_one(self):
		(self.vsro / "Media.pk2").write_bytes(b"some other client")
		with self.assertRaisesRegex(SystemExit, "does not match the pinned"):
			self.run_main()

	def test_main_refuses_a_missing_archive(self):
		with self.assertRaisesRegex(SystemExit, "not found"):
			self.run_main()


if __name__ == "__main__":
	unittest.main()
