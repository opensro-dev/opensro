"""
===========================================================================
test_tomb_box_import.py - the tomb treasure guards import's contract (#4810).

On, the two admitted kinds stage in the import's own shard with their
skills, names and characterinfo; a kind the pinned client authors
differently, lacks, or the native tree already carries, refuses by
name. Off, exactly what the marker enrolled is reaped - and the native
name stays. On without the VSRO client refuses before any read.
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import later_fixtures  # noqa: E402
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import import_vsro_alexandria as tables  # noqa: E402
import tomb_box_import as tomb  # noqa: E402
from test_hunting2_import import archive_bytes, later_row, skill_row, utf16  # noqa: E402

NATIVE_CODE = "MOB_TQ_TOMBARCHER"
NATIVE_SYMBOL = "SN_" + NATIVE_CODE
GUARD_SKILL = 7201
GUARD_SKILL_CODE = "MSKILL_TQ_TREASURE_TEST"


# ================
# symbol_of
#
# The name symbol each fixture kind carries (SN_ + codename, as the
# pinned client authors both).
# ================
def symbol_of(code):
	return "SN_" + code


# ================
# TombBoxImportTest
# ================
class TombBoxImportTest(unittest.TestCase):
	# ================
	# fixture
	#
	# A scratch world: a fake VSRO root (one Media.pk2) carrying the
	# two kinds (the guard with one skill), a native root, and an
	# output textdata with one native kind. overrides replaces kind
	# rows by codename; kinds narrows the client's kinds.
	# ================
	def fixture(self, base, overrides=None, kinds=None):
		vsro, native, output = base / "vsro", base / "native", base / "output"
		text_root = output / tomb.TEXT_ROOT
		text_root.mkdir(parents=True)
		vsro.mkdir()
		(native / tomb.TEXT_ROOT).mkdir(parents=True)
		native_char = ["0"] * 104
		native_char[1], native_char[2], native_char[5] = "100", NATIVE_CODE, NATIVE_SYMBOL
		later_fixtures.write_sharded(text_root, "characterdata.txt", {"characterdata_5000.txt": [native_char]})
		later_fixtures.write_sharded(text_root, "skilldata.txt", {"skilldata_5000.txt": []})
		(text_root / "textdataname.txt").write_bytes(utf16([["1", NATIVE_SYMBOL, "Tomb Archer"]]))
		(text_root / "skilleffect.txt").write_bytes(utf16([
			["#section", "characterinfo"], [NATIVE_CODE] + ["0"] * 12, ["#section", "skillaniset"]]))
		rows = []
		for at, (code, (grade, level)) in enumerate((kinds or tomb.KINDS).items()):
			rows.append(later_row(code, symbol_of(code), level, grade=grade, identity=9000 + at))
		for row in rows:
			if row[2] == "MOB_TQ_TREASURE_GUARD":
				row[89] = str(GUARD_SKILL)
		for code, row in (overrides or {}).items():
			rows = [row if old[2] == code else old for old in rows]
		media = {
			**later_fixtures.sharded("characterdata.txt", {"characterdata_6000.txt": rows}),
			"skilldata.txt": later_fixtures.index("skilldata_6000.txt"),
			"skilldata_6000.txt": utf16([skill_row(GUARD_SKILL, GUARD_SKILL_CODE)]),
			"textdata_object.txt": utf16([[str(at), symbol_of(code), "x", "x", code, "f", "x", "a", "Treasure Guardian", "c"]
				for at, code in enumerate(tomb.KINDS)]),
			"skilleffect.txt": utf16([["#section", "characterinfo"]]
				+ [[code] + ["0"] * 12 for code in tomb.KINDS] + [["#section", "skillaniset"]]),
		}
		(vsro / "Media.pk2").write_bytes(archive_bytes(media))
		return vsro, native, output, text_root

	# ================
	# run_import
	#
	# One add_tomb_boxes call with the pin check recorded, never run.
	# ================
	def run_import(self, vsro, native, output, flag="on"):
		self.pinned = []
		env = {tomb.FLAG: flag, tables.ROOT_ENV: str(vsro)}
		with mock.patch.object(tables, "require_vsro_pin", lambda root, names: self.pinned.append(tuple(names))):
			return tomb.add_tomb_boxes(native, output, env)

	# ================
	# shard_rows
	# ================
	def shard_rows(self, text_root):
		return [row for row in tables.table_rows((text_root / tomb.CHARACTER_SHARD).read_bytes()) if len(row) > 2]

	# ================
	# test_flag_values
	# ================
	def test_flag_values(self):
		for value in ("1", "on", "true", " ON "):
			self.assertTrue(tomb.flag_enabled({tomb.FLAG: value}), value)
		for value in ("", "0", "off", "yes"):
			self.assertFalse(tomb.flag_enabled({tomb.FLAG: value}), value)
		self.assertFalse(tomb.flag_enabled({}))

	# ================
	# test_on_stages_the_two_guards
	# ================
	def test_on_stages_the_two_guards(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root = self.fixture(Path(root))
			summary = self.run_import(vsro, native, output)
			self.assertEqual(self.pinned, [("Media",)])
			self.assertEqual(sorted(summary["characters"]), sorted(tomb.KINDS))
			self.assertTrue(all(identity > 100 for identity in summary["characters"].values()))
			self.assertEqual(sorted(summary["skills"]), [GUARD_SKILL_CODE])
			rows = {row[2]: row for row in self.shard_rows(text_root)}
			self.assertEqual(sorted(rows), sorted(tomb.KINDS))
			self.assertTrue(all(len(row) == 120 for row in rows.values()))
			# The skill slot (89-98 of the converted row) names the merged identity.
			self.assertIn(str(summary["skills"][GUARD_SKILL_CODE]), rows["MOB_TQ_TREASURE_GUARD"][89:99])
			self.assertNotIn(str(GUARD_SKILL), rows["MOB_TQ_TREASURE_GUARD"][89:99])
			names = (text_root / "textdataname.txt").read_bytes().decode("utf-16")
			for code in tomb.KINDS:
				self.assertIn(symbol_of(code), names)
				self.assertIn(symbol_of(code), summary["nameSymbols"])
			self.assertIn(NATIVE_SYMBOL, names)
			self.assertNotIn(NATIVE_SYMBOL, summary["nameSymbols"])
			effects = (text_root / "skilleffect.txt").read_bytes().decode("utf-16")
			for code in tomb.KINDS:
				self.assertIn(code, effects)
			manifest = [row[0] for row in tables.table_rows((text_root / "skilldata.txt").read_bytes()) if row]
			self.assertIn(tomb.SKILL_SHARD, manifest)
			marker = json.loads((output / tomb.MARKER).read_text(encoding="utf-8"))
			self.assertEqual(marker["characters"], summary["characters"])

	# ================
	# test_rebuild_keeps_identities
	# ================
	def test_rebuild_keeps_identities(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root = self.fixture(Path(root))
			first = self.run_import(vsro, native, output)
			second = self.run_import(vsro, native, output)
			self.assertEqual(first["characters"], second["characters"])
			self.assertEqual(first["nameSymbols"], second["nameSymbols"])
			self.assertEqual(len(self.shard_rows(text_root)), 2)
			names = [row for row in tables.table_rows((text_root / "textdataname.txt").read_bytes()) if len(row) > 1]
			self.assertEqual([row[1] for row in names].count(symbol_of("MOB_TQ_TREASURE_GUARD")), 1)

	# ================
	# test_moved_kind_refuses
	# ================
	def test_moved_kind_refuses(self):
		moved = later_row("MOB_TQ_TREASURE_GUARD_CLON", symbol_of("MOB_TQ_TREASURE_GUARD_CLON"), 88, grade=6, identity=9001)
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root = self.fixture(Path(root), {"MOB_TQ_TREASURE_GUARD_CLON": moved})
			with self.assertRaisesRegex(ValueError, "MOB_TQ_TREASURE_GUARD_CLON"):
				self.run_import(vsro, native, output)

	# ================
	# test_moved_grade_refuses
	# ================
	def test_moved_grade_refuses(self):
		moved = later_row("MOB_TQ_TREASURE_GUARD", symbol_of("MOB_TQ_TREASURE_GUARD"), 97, grade=0, identity=9000)
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root = self.fixture(Path(root), {"MOB_TQ_TREASURE_GUARD": moved})
			with self.assertRaisesRegex(ValueError, "MOB_TQ_TREASURE_GUARD"):
				self.run_import(vsro, native, output)

	# ================
	# test_absent_kind_refuses
	# ================
	def test_absent_kind_refuses(self):
		only_guard = {"MOB_TQ_TREASURE_GUARD": tomb.KINDS["MOB_TQ_TREASURE_GUARD"]}
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root = self.fixture(Path(root), kinds=only_guard)
			with self.assertRaisesRegex(ValueError, "MOB_TQ_TREASURE_GUARD_CLON"):
				self.run_import(vsro, native, output)

	# ================
	# test_native_kind_refuses
	# ================
	def test_native_kind_refuses(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root = self.fixture(Path(root))
			row = ["0"] * 120
			row[1], row[2] = "200", "MOB_TQ_TREASURE_GUARD"
			(text_root / "characterdata.txt").write_bytes(
				later_fixtures.index("characterdata_5000.txt", "characterdata_7000.txt"))
			(text_root / "characterdata_7000.txt").write_bytes(utf16([row]))
			with self.assertRaisesRegex(ValueError, "already stands"):
				self.run_import(vsro, native, output)

	# ================
	# test_off_reaps_exactly_the_marker
	# ================
	def test_off_reaps_exactly_the_marker(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root = self.fixture(Path(root))
			self.run_import(vsro, native, output)
			self.assertEqual(self.run_import(vsro, native, output, ""), {"enabled": False})
			for name in (tomb.CHARACTER_SHARD, tomb.SKILL_SHARD):
				self.assertFalse((text_root / name).exists())
			self.assertFalse((output / tomb.MARKER).exists())
			names = (text_root / "textdataname.txt").read_bytes().decode("utf-16")
			self.assertIn(NATIVE_SYMBOL, names)
			for code in tomb.KINDS:
				self.assertNotIn(symbol_of(code), names)
			effects = (text_root / "skilleffect.txt").read_bytes().decode("utf-16")
			self.assertIn(NATIVE_CODE, effects)
			for code in tomb.KINDS:
				self.assertNotIn(code, effects)
			manifest = [row[0] for row in tables.table_rows((text_root / "skilldata.txt").read_bytes()) if row]
			self.assertNotIn(tomb.SKILL_SHARD, manifest)

	# ================
	# test_off_reads_no_archive
	# ================
	def test_off_reads_no_archive(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root = self.fixture(Path(root))
			(vsro / "Media.pk2").unlink()
			self.assertEqual(self.run_import(vsro, native, output, ""), {"enabled": False})
			self.assertEqual(self.pinned, [])

	# ================
	# test_on_without_the_client_root_refuses
	# ================
	def test_on_without_the_client_root_refuses(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root = self.fixture(Path(root))
			with self.assertRaisesRegex(SystemExit, tables.ROOT_ENV):
				tomb.add_tomb_boxes(native, output, {tomb.FLAG: "on"})


if __name__ == "__main__":
	unittest.main()
