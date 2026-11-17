"""
===========================================================================
test_roc_import.py - the Roc range import's contract (#3399 step 1).

On, the nine admitted kinds stage in the import's own shard with their
skills and names; a kind the pinned client authors differently, or one
the native tree already carries, refuses by name. Off, exactly what the
marker enrolled is reaped - and a native name a later kind reuses
(MOB_RM_TAHOMET_ROC names itself SN_MOB_RM_TAHOMET) stays.
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import hashlib
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import import_vsro_alexandria as tables  # noqa: E402
import roc_import as roc  # noqa: E402
from test_hunting2_import import archive_bytes, later_row, skill_row, utf16  # noqa: E402

NATIVE_TAHOMET_SYMBOL = "SN_MOB_RM_TAHOMET"
ROC_SKILL = 7101


# ================
# symbol_of
#
# The name symbol each fixture kind carries: the arena's Tahomet reuses
# the native Tahomet's, as the VSRO client authors it.
# ================
def symbol_of(code):
	return NATIVE_TAHOMET_SYMBOL if code == "MOB_RM_TAHOMET_ROC" else "SN_" + code


# ================
# RocImportTest
# ================
class RocImportTest(unittest.TestCase):
	def setUp(self):
		tables._verified_archives.clear()

	# ================
	# fixture
	#
	# A scratch world: a fake VSRO root (one Media.pk2) carrying the
	# nine kinds (Roc with one skill), a native root, and an output
	# textdata with one native kind whose name the arena's Tahomet
	# reuses. overrides replaces kind rows by codename.
	# ================
	def fixture(self, base, overrides=None):
		vsro, native, output = base / "vsro", base / "native", base / "output"
		text_root = output / roc.TEXT_ROOT
		text_root.mkdir(parents=True)
		vsro.mkdir()
		native.mkdir()
		native_char = ["0"] * 104
		native_char[1], native_char[2], native_char[5] = "100", "MOB_RM_TAHOMET", NATIVE_TAHOMET_SYMBOL
		(text_root / "characterdata_5000.txt").write_bytes(utf16([native_char]))
		(text_root / "skilldata.txt").write_bytes(utf16([["skilldata_5000.txt"]]))
		(text_root / "skilldata_5000.txt").write_bytes(utf16([]))
		(text_root / "textdataname.txt").write_bytes(utf16([["1", NATIVE_TAHOMET_SYMBOL, "Tahomet"]]))
		(text_root / "skilleffect.txt").write_bytes(utf16([
			["#section", "characterinfo"], ["MOB_RM_TAHOMET"] + ["0"] * 12, ["#section", "skillaniset"]]))
		rows = []
		for at, (code, (grade, level)) in enumerate(roc.KINDS.items()):
			rows.append(later_row(code, symbol_of(code), level, grade=grade, identity=9000 + at))
		rows[0][89] = str(ROC_SKILL)
		for code, row in (overrides or {}).items():
			rows = [row if old[2] == code else old for old in rows]
		media = {
			"characterdata.txt": utf16([["characterdata_6000.txt"]]),
			"characterdata_6000.txt": utf16(rows),
			"skilldata.txt": utf16([["skilldata_6000.txt"]]),
			"skilldata_6000.txt": utf16([skill_row(ROC_SKILL, "MSKILL_RM_ROC_TEST")]),
			"textdata_object.txt": utf16([[str(at), symbol_of(code), "x", "x", code, "f", "x", "a", "b", "c"]
				for at, code in enumerate(roc.KINDS) if code != "MOB_RM_TAHOMET_ROC"]),
			"skilleffect.txt": utf16([["#section", "characterinfo"]]
				+ [[code] + ["0"] * 12 for code in roc.KINDS] + [["#section", "skillaniset"]]),
		}
		media_bytes = archive_bytes(media)
		(vsro / "Media.pk2").write_bytes(media_bytes)
		pins = {"Media": hashlib.sha256(media_bytes).hexdigest()}
		return vsro, native, output, text_root, pins

	# ================
	# run
	# ================
	def run_import(self, vsro, native, output, pins, flag):
		with mock.patch.object(tables, "archive_pins", return_value=pins):
			with mock.patch.dict(os.environ, {roc.FLAG: flag}):
				return roc.add_roc(vsro, native, output)

	# ================
	# test_on_stages_the_nine_kinds
	# ================
	def test_on_stages_the_nine_kinds(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			summary = self.run_import(vsro, native, output, pins, "on")
			self.assertEqual(sorted(summary["characters"]), sorted(roc.KINDS))
			self.assertTrue(all(identity > 100 for identity in summary["characters"].values()))
			self.assertEqual(list(summary["skills"]), ["MSKILL_RM_ROC_TEST"])
			shard = (text_root / roc.CHARACTER_SHARD).read_bytes().decode("utf-16")
			converted = [line.split("\t") for line in shard.splitlines() if "\tMOB_RM_ROC\t" in line][0]
			self.assertEqual(len(converted), 120)
			# The skill slots (89-98 of the converted row) name the merged identity.
			self.assertIn(str(summary["skills"]["MSKILL_RM_ROC_TEST"]), converted[89:99])
			self.assertNotIn(str(ROC_SKILL), converted[89:99])
			names = (text_root / "textdataname.txt").read_bytes().decode("utf-16")
			self.assertIn("SN_MOB_RM_ROC", names)
			# The native symbol the arena's Tahomet reuses is not the import's.
			self.assertNotIn(NATIVE_TAHOMET_SYMBOL, summary["nameSymbols"])
			self.assertIn("SN_MOB_RM_ROC", summary["nameSymbols"])
			manifest = [row[0] for row in tables.table_rows((text_root / "skilldata.txt").read_bytes()) if row]
			self.assertIn(roc.SKILL_SHARD, manifest)

	# ================
	# test_rebuild_keeps_identities
	# ================
	def test_rebuild_keeps_identities(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			first = self.run_import(vsro, native, output, pins, "on")
			second = self.run_import(vsro, native, output, pins, "on")
			self.assertEqual(first["characters"], second["characters"])
			self.assertEqual(first["nameSymbols"], second["nameSymbols"])

	# ================
	# test_off_reaps_and_keeps_the_native_name
	# ================
	def test_off_reaps_and_keeps_the_native_name(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			self.run_import(vsro, native, output, pins, "on")
			self.assertEqual(self.run_import(vsro, native, output, pins, ""), {"enabled": False})
			for name in (roc.CHARACTER_SHARD, roc.SKILL_SHARD, roc.MARKER):
				self.assertFalse((text_root / name).exists() or (output / name).exists())
			names = (text_root / "textdataname.txt").read_bytes().decode("utf-16")
			self.assertIn(NATIVE_TAHOMET_SYMBOL, names)
			self.assertNotIn("SN_MOB_RM_ROC", names)
			manifest = [row[0] for row in tables.table_rows((text_root / "skilldata.txt").read_bytes()) if row]
			self.assertNotIn(roc.SKILL_SHARD, manifest)

	# ================
	# test_off_reads_no_archive
	# ================
	def test_off_reads_no_archive(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			(vsro / "Media.pk2").unlink()
			self.assertEqual(self.run_import(vsro, native, output, pins, ""), {"enabled": False})

	# ================
	# test_moved_kind_refuses
	# ================
	def test_moved_kind_refuses(self):
		moved = later_row("MOB_RM_ROC", "SN_MOB_RM_ROC", 120, grade=3, identity=9000)
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root), {"MOB_RM_ROC": moved})
			with self.assertRaisesRegex(ValueError, "MOB_RM_ROC"):
				self.run_import(vsro, native, output, pins, "on")

	# ================
	# test_native_kind_refuses
	# ================
	def test_native_kind_refuses(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			row = ["0"] * 120
			row[1], row[2] = "200", "MOB_RM_ROC"
			(text_root / "characterdata_7000.txt").write_bytes(utf16([row]))
			with self.assertRaisesRegex(ValueError, "already stands"):
				self.run_import(vsro, native, output, pins, "on")


if __name__ == "__main__":
	unittest.main()
