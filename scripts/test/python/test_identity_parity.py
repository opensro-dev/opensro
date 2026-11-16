"""
===========================================================================
test_identity_parity.py - the client/server identity comparison.
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
from pathlib import Path
import sys
import tempfile
import unittest

import check_identity_parity as parity  # noqa: E402


# ================
# write_table
#
# One textdata file as the clients ship them: UTF-16 with a byte-order mark.
# ================
def write_table(folder, name, rows):
	text = "".join("\t".join(row) + "\r\n" for row in rows)
	(Path(folder) / name).write_bytes(b"\xff\xfe" + text.encode("utf-16-le"))


# ================
# character
# ================
def character(identity, code):
	return ["1", str(identity), code, "0"]


# ================
# ParityTest
# ================
class ParityTest(unittest.TestCase):
	# ================
	# test_matching_tables_are_clean
	# ================
	def test_matching_tables_are_clean(self):
		with tempfile.TemporaryDirectory() as client, tempfile.TemporaryDirectory() as server:
			for folder in (client, server):
				write_table(folder, "characterdata_5000.txt", [character(26850, "MOB_SD_SANDBUG")])
			report = parity.compare_identities(parity.read_identities(client), parity.read_identities(server))
		self.assertFalse(any(rows for found in report.values() for rows in found.values()))

	# ================
	# test_a_shifted_identity_is_reported_by_codename
	#
	# The Weneg case: the same codename holds a different ID on each side.
	# ================
	def test_a_shifted_identity_is_reported_by_codename(self):
		with tempfile.TemporaryDirectory() as client, tempfile.TemporaryDirectory() as server:
			write_table(client, "characterdata_120000.txt", [character(26858, "MOB_SD_WENEG"), character(26857, "MOB_SD_URAEUS")])
			write_table(server, "characterdata_120000.txt", [character(26857, "MOB_SD_WENEG"), character(26856, "MOB_SD_URAEUS")])
			report = parity.compare_identities(parity.read_identities(client), parity.read_identities(server))
		self.assertEqual(report["characterdata"]["differing"], [("MOB_SD_URAEUS", 26857, 26856), ("MOB_SD_WENEG", 26858, 26857)])

	# ================
	# test_one_sided_codenames_and_shard_index_files
	# ================
	def test_one_sided_codenames_and_shard_index_files(self):
		with tempfile.TemporaryDirectory() as client, tempfile.TemporaryDirectory() as server:
			write_table(client, "characterdata.txt", [["characterdata_5000.txt"]])
			write_table(client, "characterdata_5000.txt", [character(1, "MOB_A"), character(2, "MOB_B")])
			write_table(server, "characterdata_5000.txt", [character(1, "MOB_A"), character(3, "MOB_C")])
			report = parity.compare_identities(parity.read_identities(client), parity.read_identities(server))
		self.assertEqual(report["characterdata"]["clientOnly"], ["MOB_B"])
		self.assertEqual(report["characterdata"]["serverOnly"], ["MOB_C"])
		self.assertEqual(report["characterdata"]["differing"], [])

	# ================
	# test_each_table_uses_its_own_codename_column
	# ================
	def test_each_table_uses_its_own_codename_column(self):
		skill = ["1", "100", "x", "SKILL_ONE"]
		item = ["1", "200", "ITEM_ONE", "x"]
		with tempfile.TemporaryDirectory() as client:
			write_table(client, "skilldata_5000.txt", [skill])
			write_table(client, "itemdata_5000.txt", [item])
			identities = parity.read_identities(client)
		self.assertEqual(identities["skilldata"], {"SKILL_ONE": 100})
		self.assertEqual(identities["itemdata"], {"ITEM_ONE": 200})


if __name__ == "__main__":
	unittest.main()
