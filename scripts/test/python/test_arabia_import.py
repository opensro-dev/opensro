"""
===========================================================================
test_arabia_import.py - the Arabia continent import's contract.

The family admits only the pinned population export, converts the
v1.696 kinds its nests name inside the level window, writes the field
and dungeon population files, counts the NPC nests without projecting
them, and a switch off reaps exactly what the marker enrolled.
===========================================================================
"""
import hashlib
import json
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import arabia_import as arabia  # noqa: E402
import jupiter_import as jupiter  # noqa: E402
import import_vsro_alexandria as tables  # noqa: E402

DUNGEON_REGIONS = [22618, 22619]


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
# nest
# ================
def nest(code, nest_id, world, region, spawn_type=0, cap=0, count=3):
	return {"Codename": code, "NestID": nest_id, "WorldID": world, "Continent": "ARABIA_FIELD_01",
		"RegionID": region, "X": 100.0, "Y": 50.0, "Z": 200.0, "Radius": 30, "GenerateRadius": 20,
		"MaxCount": count, "RespawnDelayMinSec": 10, "RespawnDelayMaxSec": 20, "Respawn": True,
		"InitialDir": 0, "SpawnType": spawn_type, "NestFlag": 0, "ChampionGenPercentage": 0,
		"Controls": {"ID": 1}, "Champion": None,
		"Hive": {"OverwriteMaxTotalCount": cap, "KeepMonsterCountType": 0}}


# ================
# export_document
# ================
def export_document(rows):
	return {
		"source": "fixture", "sourceSha256": arabia.POPULATION_SOURCE_SHA256,
		"worlds": {
			"112": {"CodeName": "GROUP_ARABIA_DUNGEON", "Type": 0, "Regions": list(DUNGEON_REGIONS)},
			"114": {"CodeName": "GROUP_ARABIA_PETRA", "Type": 0, "Regions": []},
		},
		"rows": rows,
	}


# ================
# ArabiaImportTest
# ================
class ArabiaImportTest(unittest.TestCase):
	def setUp(self):
		jupiter._verified_archives.clear()

	# ================
	# fixture
	#
	# A scratch world: a fake v1.696 root (one Media.pk2), a native
	# root, an output textdata with native rows, and a population
	# export whose nests name two kinds.
	# ================
	def fixture(self, base, kinds, rows, extra_rows=()):
		v1696, native, output, source = base / "v1696", base / "native", base / "output", base / "source"
		text_root = output / arabia.TEXT_ROOT
		text_root.mkdir(parents=True)
		v1696.mkdir()
		native.mkdir()
		source.mkdir()
		native_char = ["0"] * 104
		native_char[1], native_char[2] = "100", "MOB_NATIVE"
		(text_root / "characterdata_5000.txt").write_bytes(utf16([native_char]))
		(text_root / "skilldata.txt").write_bytes(utf16([["skilldata_5000.txt"]]))
		(text_root / "skilldata_5000.txt").write_bytes(utf16([]))
		(text_root / "textdataname.txt").write_bytes(utf16([["1", "UIO_OBJ_MOB_NATIVE", "Native"]]))
		(text_root / "skilleffect.txt").write_bytes(utf16([
			["#section", "characterinfo"], ["MOB_NATIVE"] + ["0"] * 12, ["#section", "skillaniset"]]))
		extra = list(extra_rows)
		media = {
			"characterdata.txt": utf16([["characterdata_6000.txt"]]),
			"characterdata_6000.txt": utf16(kinds + extra),
			"skilldata.txt": utf16([["skilldata_6000.txt"]]),
			"skilldata_6000.txt": utf16([skill_row(7001, "MSKILL_ARABIA_TEST")]),
			"textdata_object.txt": utf16(
				[[str(1 + at), row[5], "x", "x", row[5], "f", "x", "a", "b", "c"] for at, row in enumerate(kinds + extra)]),
			"skilleffect.txt": utf16([
				["#section", "characterinfo"]]
				+ [[row[2]] + ["0"] * 12 for row in kinds + extra]
				+ [["#section", "skillaniset"]]),
		}
		media_bytes = archive_bytes(media)
		(v1696 / "Media.pk2").write_bytes(media_bytes)
		(source / arabia.POPULATION_NAME).write_text(json.dumps(export_document(rows)), encoding="utf-8")
		pins = {"Media": hashlib.sha256(media_bytes).hexdigest()}
		return v1696, native, output, source, text_root, pins

	# ================
	# kinds
	# ================
	def kinds(self):
		rows = [
			later_row("MOB_AR_TEST_A", "UIO_OBJ_MOB_AR_TEST_A", 125),
			later_row("MOB_ARABIA_TEST_B", "UIO_OBJ_MOB_ARABIA_TEST_B", 130),
		]
		rows[0][89] = "7001"
		return rows

	# ================
	# nests
	# ================
	def nests(self):
		return [
			nest("MOB_AR_TEST_A", 11, 1, 22620),
			nest("MOB_ARABIA_TEST_B", 12, 112, DUNGEON_REGIONS[0], spawn_type=1),
			nest("NPC_AR_TEST", 13, 1, 22620),
		]

	# ================
	# test_only_the_pinned_population_export_is_admitted
	# ================
	def test_only_the_pinned_population_export_is_admitted(self):
		with tempfile.TemporaryDirectory() as root:
			source = Path(root)
			document = export_document(self.nests())
			document["sourceSha256"] = "0" * 64
			(source / arabia.POPULATION_NAME).write_text(json.dumps(document), encoding="utf-8")
			with self.assertRaises(ValueError):
				arabia.read_population(source)
			document = export_document(self.nests())
			del document["worlds"]["112"]
			(source / arabia.POPULATION_NAME).write_text(json.dumps(document), encoding="utf-8")
			with self.assertRaises(ValueError):
				arabia.read_population(source)
			document = export_document(self.nests() + [nest("MOB_AR_TEST_A", 99, 77, 22620)])
			(source / arabia.POPULATION_NAME).write_text(json.dumps(document), encoding="utf-8")
			with self.assertRaises(ValueError):
				arabia.read_population(source)

	# ================
	# test_partition_and_admission
	# ================
	def test_partition_and_admission(self):
		export = export_document(self.nests())
		field, dungeon, npcs = arabia.nest_partition(export)
		self.assertEqual([row["Codename"] for row in field], ["MOB_AR_TEST_A"])
		self.assertEqual([row["Codename"] for row in dungeon], ["MOB_ARABIA_TEST_B"])
		self.assertEqual(npcs, 1)
		needed = {"MOB_AR_TEST_A", "MOB_ARABIA_TEST_B"}
		self.assertTrue(arabia.family_admits("MOB_AR_TEST_A", later_row("MOB_AR_TEST_A", "S", 125), needed))
		# Outside the window, not named by a nest, and the NPC all stay out.
		self.assertFalse(arabia.family_admits("MOB_AR_TEST_A", later_row("MOB_AR_TEST_A", "S", 119), needed))
		self.assertFalse(arabia.family_admits("MOB_AR_OTHER", later_row("MOB_AR_OTHER", "S", 125), needed))
		self.assertFalse(arabia.family_admits("NPC_AR_TEST", later_row("NPC_AR_TEST", "S", 0), needed))

	# ================
	# test_client_regions_reconcile_against_the_dungeon
	# ================
	def test_client_regions_reconcile_against_the_dungeon(self):
		later = {"regioncode.txt": utf16([
			["1", "22620", "RN_ARABIA_FIELD_01", "1"],
			["1", "22618", "RN_ARABIA_DUNGEON_01", "1"],
			["1", "22619", "RN_ARABIA_DUNGEON_02", "1"],
			["1", "22621", "RN_ARABIA_PETRA_01", "1"],
			["1", "9999", "RN_EGYPT_FIELD_01", "1"],
		])}
		groups, ids = arabia.client_regions(later, export_document(self.nests()))
		self.assertEqual(ids, [22618, 22619, 22620, 22621])
		self.assertEqual({name: len(members) for name, members in groups.items()},
			{"FIELD": 1, "DUNGEON": 2, "PETRA": 1})
		bad = {"regioncode.txt": utf16([["1", "22620", "RN_ARABIA_FIELD_01", "1"]])}
		with self.assertRaises(ValueError):
			arabia.client_regions(bad, export_document(self.nests()))

	# ================
	# test_import_writes_shard_populations_and_marker
	# ================
	def test_import_writes_shard_populations_and_marker(self):
		extra = [
			later_row("MOB_AR_UNNAMED", "UIO_OBJ_MOB_AR_UNNAMED", 125),
			later_row("NPC_AR_TEST", "UIO_OBJ_NPC_AR_TEST", 0),
		]
		with tempfile.TemporaryDirectory() as root:
			v1696, native, output, source, text_root, pins = self.fixture(Path(root), self.kinds(), self.nests(), extra)
			with mock.patch.object(jupiter, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, {"SRO_ARABIA": "on"}):
					summary = arabia.add_monsters(v1696, native, output, source)
			self.assertTrue(summary["enabled"])
			self.assertEqual(summary["kinds"], 2)
			self.assertEqual(summary["fieldNests"], 1)
			self.assertEqual(summary["dungeonNests"], 1)
			self.assertEqual(summary["npcRows"], 1)
			shard = [row for row in tables.table_rows((text_root / arabia.CHARACTER_SHARD).read_bytes())]
			self.assertEqual({row[2] for row in shard}, {"MOB_AR_TEST_A", "MOB_ARABIA_TEST_B"})
			field = json.loads((text_root / arabia.FIELD_FILE).read_text(encoding="utf-8"))
			dungeon = json.loads((text_root / arabia.DUNGEON_FILE).read_text(encoding="utf-8"))
			self.assertEqual((field["Version"], len(field["Rows"]), field["Rows"][0]["WorldID"]), (2, 1, 1))
			self.assertEqual((dungeon["Version"], len(dungeon["Rows"]), dungeon["Rows"][0]["WorldID"]), (2, 1, 112))
			self.assertEqual(dungeon["Rows"][0]["SpawnType"], 1)
			self.assertTrue((output / arabia.MARKER).exists())

	# ================
	# test_a_capped_kind_projects_onto_its_first_nest
	# ================
	def test_a_capped_kind_projects_onto_its_first_nest(self):
		rows = [
			nest("MOB_AR_TEST_A", 21, 1, 22620, cap=1, count=5),
			nest("MOB_AR_TEST_A", 22, 1, 22621, cap=1, count=5),
		]
		with tempfile.TemporaryDirectory() as root:
			v1696, native, output, source, text_root, pins = self.fixture(Path(root), self.kinds(), rows)
			with mock.patch.object(jupiter, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, {"SRO_ARABIA": "on"}):
					summary = arabia.add_monsters(v1696, native, output, source)
			self.assertEqual(summary["fieldNests"], 1)
			field = json.loads((text_root / arabia.FIELD_FILE).read_text(encoding="utf-8"))
			self.assertEqual(field["Rows"][0]["MaxCount"], 1)

	# ================
	# test_a_nest_kind_the_client_lacks_refuses
	# ================
	def test_a_nest_kind_the_client_lacks_refuses(self):
		rows = self.nests() + [nest("MOB_AR_GHOST", 31, 1, 22620)]
		with tempfile.TemporaryDirectory() as root:
			v1696, native, output, source, text_root, pins = self.fixture(Path(root), self.kinds(), rows)
			with mock.patch.object(jupiter, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, {"SRO_ARABIA": "on"}):
					with self.assertRaises(ValueError):
						arabia.add_monsters(v1696, native, output, source)

	# ================
	# test_off_reaps_the_owned_outputs
	# ================
	def test_off_reaps_the_owned_outputs(self):
		with tempfile.TemporaryDirectory() as root:
			v1696, native, output, source, text_root, pins = self.fixture(Path(root), self.kinds(), self.nests())
			with mock.patch.object(jupiter, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, {"SRO_ARABIA": "on"}):
					arabia.add_monsters(v1696, native, output, source)
				with mock.patch.dict(os.environ, {"SRO_ARABIA": ""}):
					self.assertEqual(arabia.add_monsters(None, native, output), {"enabled": False})
			for name in (arabia.CHARACTER_SHARD, arabia.SKILL_SHARD, arabia.FIELD_FILE, arabia.DUNGEON_FILE):
				self.assertFalse((text_root / name).exists(), name)
			self.assertFalse((output / arabia.MARKER).exists())


if __name__ == "__main__":
	unittest.main()
