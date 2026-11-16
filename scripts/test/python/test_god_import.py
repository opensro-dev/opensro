"""
===========================================================================
test_god_import.py - the GOD temple worlds import's contract.

Behind SRO_CAP110_TEMPLE (and only with its cap-110 pairing) the import
admits the pinned population export's own worlds, each family at its
authored levels, into the one cap110-temple.txt the server reads; the
switch off reaps exactly what the marker enrolled.
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

import god_import as god  # noqa: E402

CONTROLS = {
	"ID": 1, "ObjectID": 2, "AIQoS": 0, "MaxStamina": 150, "StaminaVariance": 50,
	"SightRange": 200, "AggressType": 0, "AggressData": 0, "ChangeTarget": 2,
	"HelpRequest": 2, "HelpResponse": 2, "BattleStyle": 0, "BattleStyleData": 0,
	"DiversionBasis": 5, "DiversionKeepBasis": 4, "KeepDistance": 0, "KeepDistanceData": 0,
	"TraceType": 0, "TraceBoundary": 1, "TraceData": 500, "HomingType": 0, "HomingData": 0,
	"AggressOnHoming": 1, "FleeType": 0, "ChampionID": 0, "Flags": 0,
	"DiversionBasisData": [0] * 8, "DiversionKeepBasisData": [0] * 8,
}
WORLDS = {str(world): {"CodeName": f"GROUP_GOD_{world}", "Type": 0, "Regions": [24929]}
	for world in god.ALL_WORLDS}
ON = {"SRO_CAP110_TEMPLE": "on", "SRO_CAP110_MONSTERS": "on"}
OFF = {"SRO_CAP110_TEMPLE": "", "SRO_CAP110_MONSTERS": ""}


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
def nest(nest_id, code, world, region=24929, spawn_type=0):
	return {
		"Codename": code, "NestID": nest_id, "WorldID": world, "Continent": "GOD_TOGUI",
		"RegionID": region, "X": 100.0, "Y": 50.0, "Z": 200.0, "Radius": 30, "GenerateRadius": 20,
		"MaxCount": 3, "RespawnDelayMinSec": 10, "RespawnDelayMaxSec": 20, "Respawn": True,
		"InitialDir": 0, "SpawnType": spawn_type, "NestFlag": 0, "ChampionGenPercentage": 0,
		"Controls": dict(CONTROLS), "Champion": None,
		"Hive": {"OverwriteMaxTotalCount": 0, "KeepMonsterCountType": 0},
	}


# ================
# GodImportTest
# ================
class GodImportTest(unittest.TestCase):
	def setUp(self):
		god._verified_archives.clear()

	# ================
	# fixture
	#
	# A scratch world: a fake v1.696 root (one Media.pk2), a native
	# root, an output textdata with native rows, and a population
	# export naming one kind per world.
	# ================
	def fixture(self, base, export_rows, extra_rows=()):
		v1696, native, output, source = base / "v1696", base / "native", base / "output", base / "source"
		text_root = output / god.TEXT_ROOT
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
		kinds = [later_row("MOB_GOD_TOGUI_KIND", "UIO_OBJ_MOB_GOD_TOGUI_KIND", 50),
			later_row("MOB_GOD_FLAME_KIND", "UIO_OBJ_MOB_GOD_FLAME_KIND", 80, grade=3),
			later_row("MOB_GOD_GHOST_KIND", "UIO_OBJ_MOB_GOD_GHOST_KIND", 100)]
		kinds[0][89] = "7001"
		extra = list(extra_rows)
		media = {
			"characterdata.txt": utf16([["characterdata_6000.txt"]]),
			"characterdata_6000.txt": utf16(kinds + extra),
			"skilldata.txt": utf16([["skilldata_6000.txt"]]),
			"skilldata_6000.txt": utf16([skill_row(7001, "MSKILL_GOD_TEST")]),
			"textdata_object.txt": utf16([
				[str(at + 1), row[5], "x", "x", row[5], "f", "x", "a", "b", "c"]
				for at, row in enumerate(kinds + extra)]),
			"skilleffect.txt": utf16([
				["#section", "characterinfo"]]
				+ [[row[2]] + ["0"] * 12 for row in kinds + extra]
				+ [["#section", "skillaniset"]]),
		}
		media_bytes = archive_bytes(media)
		(v1696 / "Media.pk2").write_bytes(media_bytes)
		(source / god.POPULATION_NAME).write_text(json.dumps({
			"source": "fixture", "sourceSha256": god.POPULATION_SOURCE_SHA256,
			"worlds": WORLDS, "rows": export_rows,
		}), encoding="utf-8")
		pins = {"Media": hashlib.sha256(media_bytes).hexdigest()}
		return v1696, native, output, source, text_root, pins

	# ================
	# test_only_the_pinned_population_export_is_admitted
	# ================
	def test_only_the_pinned_population_export_is_admitted(self):
		with tempfile.TemporaryDirectory() as root:
			source = Path(root)
			(source / god.POPULATION_NAME).write_text(json.dumps({
				"sourceSha256": "0" * 64, "worlds": WORLDS, "rows": [],
			}), encoding="utf-8")
			with self.assertRaises(ValueError):
				god.read_population(source)
			(source / god.POPULATION_NAME).write_text(json.dumps({
				"sourceSha256": god.POPULATION_SOURCE_SHA256, "worlds": {"63": WORLDS["63"]}, "rows": [],
			}), encoding="utf-8")
			with self.assertRaises(ValueError):
				god.read_population(source)

	# ================
	# test_projection_strips_extraction_facts_and_carries_spawn_type
	# ================
	def test_projection_strips_extraction_facts_and_carries_spawn_type(self):
		rows = [nest(2, "MOB_GOD_GHOST_KIND", 77, spawn_type=1),
			nest(1, "MOB_GOD_TOGUI_KIND", 63)]
		projected = god.project_population(rows, god.AREAS["togui"]["worlds"])
		self.assertEqual([row["NestID"] for row in projected["Rows"]], [1])
		self.assertNotIn("SpawnType", projected["Rows"][0])
		self.assertNotIn("Continent", projected["Rows"][0])
		self.assertNotIn("Hive", projected["Rows"][0])
		wreck = god.project_population(rows, god.AREAS["wreck"]["worlds"])
		self.assertEqual(wreck["Rows"][0]["SpawnType"], 1)
		bad = nest(3, "MOB_GOD_TOGUI_KIND", 63)
		bad["Hive"] = {"OverwriteMaxTotalCount": 0, "KeepMonsterCountType": 1}
		with self.assertRaises(ValueError):
			god.project_population([bad], god.AREAS["togui"]["worlds"])
		bad = nest(3, "MOB_GOD_TOGUI_KIND", 63)
		bad["NestFlag"] = 1
		with self.assertRaises(ValueError):
			god.project_population([bad], god.AREAS["togui"]["worlds"])
		# A hive total cap projects onto the kind's first nest (lowest
		# NestID), with the cap as its count; the other spots drop.
		spots = [nest(6, "MOB_GOD_TOGUI_KIND", 63), nest(5, "MOB_GOD_TOGUI_KIND", 63)]
		for spot in spots:
			spot["Hive"] = {"OverwriteMaxTotalCount": 1, "KeepMonsterCountType": 0}
			spot["MaxCount"] = 3
		projected = god.project_population(spots, god.AREAS["togui"]["worlds"])
		self.assertEqual([row["NestID"] for row in projected["Rows"]], [5])
		self.assertEqual(projected["Rows"][0]["MaxCount"], 1)

	# ================
	# test_later_object_text_follows_a_shard_index
	# ================
	def test_later_object_text_follows_a_shard_index(self):
		files = {
			"textdata_object.txt": utf16([["TextData_Object_1000.txt"], ["TextData_Object_2000.txt"]]),
			"textdata_object_1000.txt": utf16([["1", "SN_ONE"]]),
			"textdata_object_2000.txt": utf16([["2", "SN_TWO"]]),
		}
		text = god.later_object_text(lambda name: files[name.lower()])
		self.assertIn("SN_ONE", text)
		self.assertIn("SN_TWO", text)
		flat = {"textdata_object.txt": utf16([["1", "SN_FLAT", "x"], ["2", "SN_FLAT2", "y"]])}
		self.assertIn("SN_FLAT2", god.later_object_text(lambda name: flat[name.lower()]))

	# ================
	# test_later_text_row_maps_the_17_column_layout
	# ================
	def test_later_text_row_maps_the_17_column_layout(self):
		row = ["1", "800", "SN_MOB_TEST", "한국어"] + ["0"] * 5 + ["English"] + ["0"] * 3 + ["English"] * 4
		self.assertEqual(len(row), 17)
		mapped = god.later_text_row(row)
		self.assertEqual(len(mapped), 15)
		self.assertEqual(mapped[1], "SN_MOB_TEST")
		self.assertEqual(mapped[3], "")
		self.assertEqual(mapped[4], "English")
		self.assertEqual(mapped[8], "English")
		vsro = ["1", "SN_MOB_TEST"] + ["x"] * 13
		self.assertEqual(god.later_text_row(vsro), vsro)

	# ================
	# test_family_admits_the_authored_window_only
	# ================
	def test_family_admits_the_authored_window_only(self):
		row = later_row("MOB_GOD_TOGUI_A", "S", 36)
		self.assertTrue(god.family_admits("togui", "MOB_GOD_TOGUI_A", row))
		self.assertTrue(god.family_admits("togui", "MOB_GOD_TOGUI_A", later_row("MOB_GOD_TOGUI_A", "S", 68, grade=8)))
		self.assertFalse(god.family_admits("togui", "MOB_GOD_TOGUI_A", later_row("MOB_GOD_TOGUI_A", "S", 35)))
		self.assertFalse(god.family_admits("togui", "MOB_GOD_TOGUI_A", later_row("MOB_GOD_TOGUI_A", "S", 69)))
		self.assertFalse(god.family_admits("togui", "MOB_GOD_TOGUI_A", later_row("MOB_GOD_TOGUI_A", "S", 50, grade=2)))
		# One world's window never admits another world's family.
		self.assertFalse(god.family_admits("togui", "MOB_GOD_FLAME_A", later_row("MOB_GOD_FLAME_A", "S", 50)))
		self.assertFalse(god.family_admits("flame", "MOB_GOD_TOGUI_A", later_row("MOB_GOD_TOGUI_A", "S", 80)))
		self.assertTrue(god.family_admits("flame", "MOB_GOD_FLAME_A", later_row("MOB_GOD_FLAME_A", "S", 71)))
		self.assertTrue(god.family_admits("flame", "MOB_GOD_FLAME_A", later_row("MOB_GOD_FLAME_A", "S", 89)))
		self.assertTrue(god.family_admits("wreck", "MOB_GOD_GHOST_A", later_row("MOB_GOD_GHOST_A", "S", 91)))
		self.assertTrue(god.family_admits("wreck", "MOB_GOD_GHOST_A", later_row("MOB_GOD_GHOST_A", "S", 109)))
		self.assertFalse(god.family_admits("wreck", "MOB_GOD_GHOST_A", later_row("MOB_GOD_GHOST_A", "S", 110)))
		self.assertFalse(god.family_admits("togui", "NPC_INS_A", later_row("NPC_INS_A", "S", 0)))

	# ================
	# test_temple_switch_writes_the_one_population
	#
	# One switch, one population file for all 22 worlds - the file the
	# server's temple loader already reads - and one kind shard.
	# ================
	def test_temple_switch_writes_the_one_population(self):
		rows = [nest(1, "MOB_GOD_TOGUI_KIND", 63),
			nest(2, "MOB_GOD_FLAME_KIND", 69),
			nest(3, "MOB_GOD_GHOST_KIND", 77, spawn_type=1)]
		with tempfile.TemporaryDirectory() as root:
			v1696, native, output, source, text_root, pins = self.fixture(Path(root), rows)
			with mock.patch.object(god, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, ON):
					summary = god.add_monsters(v1696, native, output, source)
			self.assertEqual(sorted(summary["areas"]), ["flame", "togui", "wreck"])
			self.assertEqual(summary["nests"], 3)
			population = json.loads((text_root / god.POPULATION_FILE).read_text(encoding="utf-8"))
			self.assertEqual(population["Version"], 2)
			self.assertEqual([row["WorldID"] for row in population["Rows"]], [63, 69, 77])
			self.assertEqual(population["Rows"][2]["SpawnType"], 1)
			shard = (text_root / god.CHARACTER_SHARD).read_bytes().decode("utf-16")
			for code in ("MOB_GOD_TOGUI_KIND", "MOB_GOD_FLAME_KIND", "MOB_GOD_GHOST_KIND"):
				self.assertIn(code, shard)
			converted = [line.split("\t") for line in shard.splitlines() if "MOB_GOD_TOGUI_KIND" in line][0]
			self.assertEqual(len(converted), 120)
			# Identities allocate in codename order above the native 100.
			self.assertEqual(converted[1], "103")

	# ================
	# test_temple_without_its_pairing_refuses
	# ================
	def test_temple_without_its_pairing_refuses(self):
		rows = [nest(1, "MOB_GOD_TOGUI_KIND", 63)]
		with tempfile.TemporaryDirectory() as root:
			v1696, native, output, source, text_root, pins = self.fixture(Path(root), rows)
			with mock.patch.object(god, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, {"SRO_CAP110_TEMPLE": "on", "SRO_CAP110_MONSTERS": ""}):
					with self.assertRaisesRegex(ValueError, "SRO_CAP110_TEMPLE requires SRO_CAP110_MONSTERS"):
						god.add_monsters(v1696, native, output, source)
			self.assertFalse((text_root / god.POPULATION_FILE).exists())

	# ================
	# test_off_rebuild_reaps_owned_outputs
	# ================
	def test_off_rebuild_reaps_owned_outputs(self):
		rows = [nest(1, "MOB_GOD_TOGUI_KIND", 63)]
		with tempfile.TemporaryDirectory() as root:
			v1696, native, output, source, text_root, pins = self.fixture(Path(root), rows)
			with mock.patch.object(god, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, ON):
					god.add_monsters(v1696, native, output, source)
				with mock.patch.dict(os.environ, OFF):
					self.assertEqual(god.add_monsters(v1696, native, output, source), {"enabled": False})
			for name in (god.CHARACTER_SHARD, god.SKILL_SHARD, god.POPULATION_FILE):
				self.assertFalse((text_root / name).exists())
			self.assertFalse((output / god.MARKER).exists())
			names = (text_root / "textdataname.txt").read_bytes().decode("utf-16")
			self.assertIn("UIO_OBJ_MOB_NATIVE", names)
			self.assertNotIn("MOB_GOD_TOGUI_KIND", names)
			effects_text = (text_root / "skilleffect.txt").read_bytes().decode("utf-16")
			self.assertIn("MOB_NATIVE", effects_text)
			self.assertNotIn("MOB_GOD_TOGUI_KIND", effects_text)

	# ================
	# test_npcs_stay_out
	#
	# The real export places the worlds' NPC attendants beside the
	# monsters: roster content that never reaches a population or the
	# shard.
	# ================
	def test_npcs_stay_out(self):
		rows = [nest(1, "MOB_GOD_TOGUI_KIND", 63),
			nest(2, "NPC_INS_TOGUI_01", 63)]
		extra = [later_row("NPC_INS_TOGUI_01", "UIO_OBJ_NPC_INS_TOGUI_01", 0)]
		with tempfile.TemporaryDirectory() as root:
			v1696, native, output, source, text_root, pins = self.fixture(Path(root), rows, extra)
			with mock.patch.object(god, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, ON):
					summary = god.add_monsters(v1696, native, output, source)
			self.assertEqual(summary["npcRows"], 1)
			self.assertEqual(summary["areas"]["togui"]["nests"], 1)
			self.assertNotIn("NPC_INS_TOGUI_01", summary["characters"])
			shard = (text_root / god.CHARACTER_SHARD).read_bytes().decode("utf-16")
			self.assertNotIn("NPC_INS_TOGUI_01", shard)

	# ================
	# test_unadmitted_or_missing_claimed_kind_refuses
	# ================
	def test_unadmitted_or_missing_claimed_kind_refuses(self):
		rows = [nest(1, "MOB_GOD_TOGUI_KIND", 63), nest(2, "MOB_GOD_TOGUI_GHOST", 63)]
		with tempfile.TemporaryDirectory() as root:
			v1696, native, output, source, text_root, pins = self.fixture(Path(root), rows)
			with mock.patch.object(god, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, ON):
					with self.assertRaises(ValueError):
						god.add_monsters(v1696, native, output, source)


if __name__ == "__main__":
	unittest.main()
