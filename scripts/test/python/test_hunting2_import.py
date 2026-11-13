"""
===========================================================================
test_hunting2_import.py - the second hunting grounds import's contract.

The set's monsters are the client's own npcpos placements in its
regions, at the levels characterdata authors; placements become
one-monster nests on the default world, and a switch off reaps
exactly what the marker enrolled.
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import hashlib
import json
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import hunting2_import as hunting2  # noqa: E402
import jupiter_import as jupiter  # noqa: E402

REGION_A = 25000
REGION_B = 25001
OUTSIDE = 24000


# ================
# utf16
# ================
def utf16(rows):
	return b"\xff\xfe" + ("\r\n".join("\t".join(row) for row in rows) + "\r\n").encode("utf-16-le")


# ================
# archive_bytes
# ================
def archive_bytes(files):
	from test_client_preparation import ArchiveBuilder
	builder = ArchiveBuilder()
	root = builder.folder([])
	server = builder.folder([])
	silkroad = builder.folder([])
	textdata = builder.folder([])
	builder.blocks[root] = [(1, b"server_dep", server, 0)]
	builder.blocks[server] = [(1, b"silkroad", silkroad, 0)]
	builder.blocks[silkroad] = [(1, b"textdata", textdata, 0)]
	builder.blocks[textdata] = [(2, name.encode("ascii"), *builder.add_payload(raw)) for name, raw in files.items()]
	return builder.build()


# ================
# later_row
# ================
def later_row(code, symbol, level, grade=0, identity=9000):
	row = ["0"] * 104
	row[1], row[2], row[5] = str(identity), code, symbol
	row[15], row[57] = str(grade), str(level)
	return row


# ================
# skill_row
# ================
def skill_row(identity, code):
	row = ["0"] * 118
	row[0], row[1], row[3] = "1", str(identity), code
	return row


# ================
# Hunting2ImportTest
# ================
class Hunting2ImportTest(unittest.TestCase):
	def setUp(self):
		jupiter._verified_archives.clear()

	# ================
	# fixture
	#
	# A scratch world: a fake v1.696 root (one Media.pk2) whose
	# regioncode names two hunting regions and one outside region,
	# npcpos placing two kinds inside (one twice, one at a signed
	# region) and one outside; a native root and an output textdata
	# with native rows.
	# ================
	def fixture(self, base, extra_rows=(), extra_placements=()):
		v1696, native, output = base / "v1696", base / "native", base / "output"
		text_root = output / hunting2.TEXT_ROOT
		text_root.mkdir(parents=True)
		v1696.mkdir()
		native.mkdir()
		native_char = ["0"] * 104
		native_char[1], native_char[2] = "100", "MOB_NATIVE"
		(text_root / "characterdata_5000.txt").write_bytes(utf16([native_char]))
		(text_root / "skilldata.txt").write_bytes(utf16([["skilldata_5000.txt"]]))
		(text_root / "skilldata_5000.txt").write_bytes(utf16([]))
		(text_root / "textdataname.txt").write_bytes(utf16([["1", "UIO_OBJ_MOB_NATIVE", "Native"]]))
		(text_root / "skilleffect.txt").write_bytes(utf16([
			["#section", "characterinfo"], ["MOB_NATIVE"] + ["0"] * 12, ["#section", "skillaniset"]]))
		inside = later_row("MOB_TQ_INSIDE", "UIO_OBJ_MOB_TQ_INSIDE", 80, identity=9001)
		inside[89] = "7001"
		restaged = later_row("MOB_SD_RESTAGED", "UIO_OBJ_MOB_SD_RESTAGED", 140, grade=1, identity=9002)
		outside = later_row("MOB_TQ_OUTSIDE", "UIO_OBJ_MOB_TQ_OUTSIDE", 90, identity=9003)
		extra = list(extra_rows)
		media = {
			"regioncode.txt": utf16([
				["1", str(REGION_A), hunting2.REGION_CODE],
				["1", str(REGION_B), hunting2.REGION_CODE],
				["1", str(OUTSIDE), "RN_FIELD_OTHER"]]),
			"npcpos.txt": utf16([
				["9001", str(REGION_A), "10", "20", "30"],
				["9001", str(REGION_A), "11", "21", "31"],
				["9001", str(REGION_A), "10", "20", "30"],
				["9002", str(-(65536 - REGION_B)), "40", "50", "60"],
				["9003", str(OUTSIDE), "1", "2", "3"]] + list(extra_placements)),
			"characterdata.txt": utf16([["characterdata_6000.txt"]]),
			"characterdata_6000.txt": utf16([inside, restaged, outside] + extra),
			"skilldata.txt": utf16([["skilldata_6000.txt"]]),
			"skilldata_6000.txt": utf16([skill_row(7001, "MSKILL_HUNTING2_TEST")]),
			"textdata_object.txt": utf16([
				["1", "UIO_OBJ_MOB_TQ_INSIDE", "x", "x", "Inside kind", "f", "x", "a", "b", "c"],
				["2", "UIO_OBJ_MOB_SD_RESTAGED", "x", "x", "Restaged kind", "t", "x", "a", "b", "c"]]
				+ [[str(3 + at), row[5], "x", "x", row[5], "f", "x", "a", "b", "c"] for at, row in enumerate(extra)]),
			"skilleffect.txt": utf16([
				["#section", "characterinfo"],
				["MOB_TQ_INSIDE"] + ["0"] * 12,
				["MOB_SD_RESTAGED"] + ["0"] * 12]
				+ [[row[2]] + ["0"] * 12 for row in extra]
				+ [["#section", "skillaniset"]]),
		}
		media_bytes = archive_bytes(media)
		(v1696 / "Media.pk2").write_bytes(media_bytes)
		pins = {"Media": hashlib.sha256(media_bytes).hexdigest()}
		return v1696, native, output, text_root, pins

	# ================
	# test_regions_come_from_the_regioncode_row
	# ================
	def test_regions_come_from_the_regioncode_row(self):
		rows = [["1", str(REGION_B), hunting2.REGION_CODE],
			["1", str(REGION_A), hunting2.REGION_CODE],
			["1", str(REGION_A), hunting2.REGION_CODE],
			["1", str(OUTSIDE), "RN_FIELD_OTHER"],
			["1", str(0x8000 + 9), hunting2.REGION_CODE]]
		with self.assertRaises(ValueError):
			hunting2.hunting_regions(rows)
		self.assertEqual(hunting2.hunting_regions(rows[:4]), [REGION_A, REGION_B])

	# ================
	# test_family_admits_the_authored_window_only
	# ================
	def test_family_admits_the_authored_window_only(self):
		self.assertTrue(hunting2.family_admits("MOB_TQ_A", later_row("MOB_TQ_A", "S", 49)))
		self.assertTrue(hunting2.family_admits("MOB_SD_B", later_row("MOB_SD_B", "S", 140, grade=1)))
		self.assertFalse(hunting2.family_admits("MOB_TQ_A", later_row("MOB_TQ_A", "S", 48)))
		self.assertFalse(hunting2.family_admits("MOB_TQ_A", later_row("MOB_TQ_A", "S", 141)))
		self.assertFalse(hunting2.family_admits("MOB_TQ_A", later_row("MOB_TQ_A", "S", 90, grade=2)))
		self.assertFalse(hunting2.family_admits("NPC_TOWN", later_row("NPC_TOWN", "S", 90)))

	# ================
	# test_placements_join_inside_the_regions_only
	# ================
	def test_placements_join_inside_the_regions_only(self):
		later = {"MOB_TQ_INSIDE": later_row("MOB_TQ_INSIDE", "S", 80, identity=9001)}
		raw = utf16([
			["9001", str(REGION_A), "10", "20", "30"],
			["9001", str(REGION_A), "10", "20", "30"],
			["9001", str(OUTSIDE), "1", "2", "3"],
			["9999", str(REGION_A), "4", "5", "6"]])
		placed = hunting2.hunting_placements(raw, later, {REGION_A, REGION_B})
		self.assertEqual(placed, [("MOB_TQ_INSIDE", REGION_A, 10.0, 20.0, 30.0)])

	# ================
	# test_import_writes_shard_population_and_marker
	# ================
	def test_import_writes_shard_population_and_marker(self):
		with tempfile.TemporaryDirectory() as root:
			v1696, native, output, text_root, pins = self.fixture(Path(root))
			with mock.patch.object(jupiter, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, {"SRO_HUNTING2": "on"}):
					summary = hunting2.add_monsters(v1696, native, output)
			self.assertEqual(summary["families"]["field"]["nests"], 3)
			self.assertEqual(summary["families"]["field"]["regions"], [REGION_A, REGION_B])
			self.assertEqual(summary["sourceRegions"], [REGION_A, REGION_B])
			population = json.loads((text_root / hunting2.FIELD_FILE).read_text(encoding="utf-8"))
			self.assertEqual(population["Version"], 2)
			self.assertTrue(all(row["WorldID"] == 1 for row in population["Rows"]))
			self.assertTrue(all(row["MaxCount"] == 1 and row["Radius"] == hunting2.cap110.CLIENT_NEST_RADIUS and
				row["GenerateRadius"] == hunting2.cap110.CLIENT_NEST_GENERATE_RADIUS for row in population["Rows"]))
			self.assertEqual(population["Rows"][0]["Controls"]["SightRange"], 100)
			shard = (text_root / hunting2.CHARACTER_SHARD).read_bytes().decode("utf-16")
			self.assertIn("MOB_TQ_INSIDE", shard)
			self.assertIn("MOB_SD_RESTAGED", shard)
			self.assertNotIn("MOB_TQ_OUTSIDE", shard)
			converted = [line.split("\t") for line in shard.splitlines() if "MOB_TQ_INSIDE" in line][0]
			self.assertEqual(len(converted), 120)
			names = (text_root / "textdataname.txt").read_bytes().decode("utf-16")
			self.assertIn("UIO_OBJ_MOB_TQ_INSIDE", names)

	# ================
	# test_off_rebuild_reaps_owned_outputs
	# ================
	def test_off_rebuild_reaps_owned_outputs(self):
		with tempfile.TemporaryDirectory() as root:
			v1696, native, output, text_root, pins = self.fixture(Path(root))
			with mock.patch.object(jupiter, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, {"SRO_HUNTING2": "on"}):
					hunting2.add_monsters(v1696, native, output)
				with mock.patch.dict(os.environ, {"SRO_HUNTING2": ""}):
					self.assertEqual(hunting2.add_monsters(v1696, native, output), {"enabled": False})
			for name in (hunting2.CHARACTER_SHARD, hunting2.SKILL_SHARD, hunting2.FIELD_FILE):
				self.assertFalse((text_root / name).exists())
			self.assertFalse((output / hunting2.MARKER).exists())
			names = (text_root / "textdataname.txt").read_bytes().decode("utf-16")
			self.assertIn("UIO_OBJ_MOB_NATIVE", names)
			self.assertNotIn("MOB_TQ_INSIDE", names)

	# ================
	# test_unadmitted_placed_kind_refuses
	# ================
	def test_unadmitted_placed_kind_refuses(self):
		extra = [later_row("MOB_TQ_TOOHIGH", "UIO_OBJ_MOB_TQ_TOOHIGH", 141, identity=9004)]
		placed = [["9004", str(REGION_A), "7", "8", "9"]]
		with tempfile.TemporaryDirectory() as root:
			v1696, native, output, text_root, pins = self.fixture(Path(root), extra, placed)
			with mock.patch.object(jupiter, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, {"SRO_HUNTING2": "on"}):
					with self.assertRaises(ValueError):
						hunting2.add_monsters(v1696, native, output)


if __name__ == "__main__":
	unittest.main()
