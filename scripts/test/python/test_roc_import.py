"""
===========================================================================
test_roc_import.py - the Roc range import's contract (#3399 step 1).

On, the nine admitted kinds stage in the import's own shard with their
skills and names; a kind the pinned client authors differently, or one
the native tree already carries, refuses by name. Off, exactly what the
marker enrolled is reaped - and a native name a later kind reuses
(MOB_RM_TAHOMET_ROC names itself SN_MOB_RM_TAHOMET) stays.

Step 1b: the four gates and their buildings merge, the summit link
carries its key item under the variant's identity - or stays out while
the variant lacks the item - and the wait room is the one region handed
to the region merge. merge_world itself has its own tests over real
archives (test_later_world_import); here it is stubbed to run the
staging step it is given.
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
# The key item: its id in the source client, and the one a variant
# that carries it gives it.
KEY_ITEM = "ITEM_QNO_RM_MEETROC_6_01"
SOURCE_KEY_ID = "23973"
VARIANT_KEY_ID = "50100"
MOUNTAIN_BUILDING = "24296"


# ================
# gate_row / building_row / link_row
#
# The source client's Roc rows, in its own column layout (13, 58, 21).
# ================
def gate_row(identity, code, building, world):
	return ["1", identity, code, building, "SN_ZONE_" + identity, "23406", "1382", "4056", "1558", "300", "0", "0", world]


def building_row(identity, code):
	row = ["0"] * 58
	row[0], row[1], row[2], row[5] = "1", identity, code, "SN_" + code
	return row


def link_row(source, target, conditions):
	row = ["1", source, target, "0", "0", "0"] + ["0"] * 15
	for at, condition in enumerate(conditions):
		row[6 + 3 * at:9 + 3 * at] = condition
	return row


SOURCE_GATES = [
	gate_row("160", "GATE_RC_MOUNTAIN_GATE", MOUNTAIN_BUILDING, "1"),
	gate_row("161", "GATE_RC_TAHOMET_GATE", "0", "41"),
	gate_row("162", "GATE_RC_ROC_GATE", "0", "40"),
	gate_row("163", "GATE_RC_ROC_FRONT_GATE", "24297", "1"),
]
SOURCE_LINKS = [
	link_row("160", "162", [["4", SOURCE_KEY_ID, "0"], ["5", SOURCE_KEY_ID, "0"], ["1", "80", "102"], ["6", "0", "0"]]),
	link_row("162", "160", []),
]


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
		(text_root / "teleportdata.txt").write_bytes(utf16([gate_row("1", "GATE_CH", "2094", "1")]))
		(text_root / "teleportbuilding.txt").write_bytes(utf16([]))
		(text_root / "teleportlink.txt").write_bytes(utf16([]))
		(text_root / "itemdata.txt").write_bytes(utf16([["itemdata_5000.txt"]]))
		(text_root / "itemdata_5000.txt").write_bytes(utf16([["1", "7", "ITEM_ETC_NATIVE"]]))
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
				for at, code in enumerate(roc.KINDS) if code != "MOB_RM_TAHOMET_ROC"]
				+ [["1", "SN_STORE_RC_TAHOMET_GATE", "k", "", "", "s", "t", "j", "Summit Gate", "v"]]),
			"teleportdata.txt": utf16(SOURCE_GATES),
			"teleportbuilding.txt": utf16([building_row(MOUNTAIN_BUILDING, "STORE_RC_TAHOMET_GATE"),
				building_row("24297", "STORE_ROC_FRONT_GATE")]),
			"teleportlink.txt": utf16(SOURCE_LINKS),
			"itemdata.txt": utf16([["itemdata_20000.txt"]]),
			"itemdata_20000.txt": utf16([["1", SOURCE_KEY_ID, KEY_ITEM]]),
			"skilleffect.txt": utf16([["#section", "characterinfo"]]
				+ [[code] + ["0"] * 12 for code in roc.KINDS] + [["#section", "skillaniset"]]),
		}
		media_bytes = archive_bytes(media)
		(vsro / "Media.pk2").write_bytes(media_bytes)
		pins = {"Media": hashlib.sha256(media_bytes).hexdigest()}
		return vsro, native, output, text_root, pins

	# ================
	# run_import
	#
	# One add_roc call. On, the region merge is stubbed to run the staging
	# step and record what it was handed; the staging summary returns.
	# ================
	def run_import(self, vsro, native, output, pins, flag):
		self.merged = []
		self.pinned = []

		def merge_world(spec, native_root, out, root, regions, build_monsters):
			self.merged.append((spec.label, list(regions)))
			return {"enabled": True, "regions": len(regions), "monsters": build_monsters()}

		with mock.patch.object(tables, "archive_pins", return_value=pins), \
			mock.patch.object(tables, "require_vsro_pin", lambda root, names: self.pinned.append(tuple(names))), \
			mock.patch.object(roc.later_world_import, "merge_world", merge_world), \
			mock.patch.dict(os.environ, {roc.FLAG: flag}):
			result = roc.add_roc(vsro, native, output)
		return result["monsters"] if result.get("enabled") else result

	# ================
	# links
	# ================
	def links(self, text_root):
		return [row for row in tables.table_rows((text_root / "teleportlink.txt").read_bytes()) if len(row) > 2]

	# ================
	# test_on_merges_the_wait_room_and_pins_every_archive
	# ================
	def test_on_merges_the_wait_room_and_pins_every_archive(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			self.run_import(vsro, native, output, pins, "on")
			self.assertEqual(self.merged, [("roc", [roc.WAIT_REGION])])
			self.assertEqual(self.pinned, [roc.MERGE_ARCHIVES])

	# ================
	# test_on_adds_the_four_gates_and_their_buildings
	# ================
	def test_on_adds_the_four_gates_and_their_buildings(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			summary = self.run_import(vsro, native, output, pins, "on")
			codes = [row[2] for row in tables.table_rows((text_root / "teleportdata.txt").read_bytes()) if len(row) > 2]
			self.assertEqual(codes, ["GATE_CH"] + list(roc.GATE_CODES))
			buildings = [row[2] for row in tables.table_rows((text_root / "teleportbuilding.txt").read_bytes())
				if len(row) > 2]
			self.assertEqual(sorted(buildings), ["STORE_RC_TAHOMET_GATE", "STORE_ROC_FRONT_GATE"])
			self.assertEqual(summary["gates"]["buildings"], ["STORE_RC_TAHOMET_GATE", "STORE_ROC_FRONT_GATE"])
			names = (text_root / "textdataname.txt").read_bytes().decode("utf-16")
			self.assertIn("SN_STORE_RC_TAHOMET_GATE", names)

	# ================
	# test_the_summit_link_stays_out_without_its_key_item
	#
	# The variant does not carry the key item yet (step 3 imports it):
	# the summit link is left out, never opened without its condition,
	# and the marker says why. The way back is unconditioned and stays.
	# ================
	def test_the_summit_link_stays_out_without_its_key_item(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			summary = self.run_import(vsro, native, output, pins, "on")
			self.assertEqual([row[1:3] for row in self.links(text_root)], [["162", "160"]])
			self.assertEqual(summary["gates"]["omittedLinks"], [{"from": "160", "to": "162", "items": [SOURCE_KEY_ID]}])
			self.assertEqual(summary["gates"]["keyItems"], {})
			self.assertTrue(any("key item" in limit for limit in summary["limits"]))

	# ================
	# test_the_summit_link_names_the_variant_key_item
	# ================
	def test_the_summit_link_names_the_variant_key_item(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			(text_root / "itemdata.txt").write_bytes(utf16([["itemdata_5000.txt"], ["itemdata_25000.txt"]]))
			(text_root / "itemdata_25000.txt").write_bytes(utf16([["1", VARIANT_KEY_ID, KEY_ITEM]]))
			summary = self.run_import(vsro, native, output, pins, "on")
			summit = next(row for row in self.links(text_root) if row[1:3] == ["160", "162"])
			self.assertEqual(summit[6:18],
				["4", VARIANT_KEY_ID, "0", "5", VARIANT_KEY_ID, "0", "1", "80", "102", "6", "0", "0"])
			self.assertEqual(summary["gates"]["keyItems"], {SOURCE_KEY_ID: VARIANT_KEY_ID})
			self.assertNotIn("omittedLinks", summary["gates"])
			self.assertFalse(any("key item" in limit for limit in summary["limits"]))

	# ================
	# test_a_building_identity_the_variant_holds_refuses
	# ================
	def test_a_building_identity_the_variant_holds_refuses(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			(text_root / "itemdata_5000.txt").write_bytes(utf16([["1", MOUNTAIN_BUILDING, "ITEM_MALL_HAT"]]))
			with self.assertRaisesRegex(ValueError, f"{MOUNTAIN_BUILDING} \\(ITEM_MALL_HAT\\)"):
				self.run_import(vsro, native, output, pins, "on")

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
			# A second pass replaces the first's gates, never stands them twice.
			codes = [row[2] for row in tables.table_rows((text_root / "teleportdata.txt").read_bytes()) if len(row) > 2]
			self.assertEqual(codes, ["GATE_CH"] + list(roc.GATE_CODES))
			self.assertEqual(len(self.links(text_root)), 1)

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
