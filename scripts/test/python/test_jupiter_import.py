"""
===========================================================================
test_jupiter_import.py - the Jupiter population import's contract.

The family admits only the pinned population export, converts the
v1.696 kinds its nests name, and a switch off reaps exactly what the
marker enrolled.
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
import jupiter_import as jupiter  # noqa: E402

CONTROLS = {
	"ID": 1, "ObjectID": 2, "AIQoS": 0, "MaxStamina": 150, "StaminaVariance": 50,
	"SightRange": 200, "AggressType": 0, "AggressData": 0, "ChangeTarget": 2,
	"HelpRequest": 2, "HelpResponse": 2, "BattleStyle": 0, "BattleStyleData": 0,
	"DiversionBasis": 5, "DiversionKeepBasis": 4, "KeepDistance": 0, "KeepDistanceData": 0,
	"TraceType": 0, "TraceBoundary": 1, "TraceData": 500, "HomingType": 0, "HomingData": 0,
	"AggressOnHoming": 1, "FleeType": 0, "ChampionID": 0, "Flags": 0,
	"DiversionBasisData": [0] * 8, "DiversionKeepBasisData": [0] * 8,
}
WORLDS = {
	"86": {"CodeName": "GROUP_JUPITER_A", "Type": 0, "Regions": [31723]},
	"87": {"CodeName": "GROUP_JUPITER_B", "Type": 0, "Regions": [32790]},
	"88": {"CodeName": "GROUP_JUPITER_A2", "Type": 0, "Regions": [31723]},
	"89": {"CodeName": "GROUP_JUPITER_B2", "Type": 0, "Regions": [32790]},
	"90": {"CodeName": "GROUP_JUPITER_FIELD", "Type": 0, "Regions": [21707]},
	"93": {"CodeName": "GROUP_JUPITER_A3", "Type": 0, "Regions": [31723]},
	"94": {"CodeName": "GROUP_JUPITER_B3", "Type": 0, "Regions": [32790]},
}


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
def nest(nest_id, code, world, region=21707, spawn_type=0):
	return {
		"Codename": code, "NestID": nest_id, "WorldID": world, "Continent": "JUPITER_FIELD",
		"RegionID": region, "X": 100.0, "Y": 50.0, "Z": 200.0, "Radius": 30, "GenerateRadius": 20,
		"MaxCount": 3, "RespawnDelayMinSec": 10, "RespawnDelayMaxSec": 20, "Respawn": True,
		"InitialDir": 0, "SpawnType": spawn_type, "NestFlag": 0, "ChampionGenPercentage": 0,
		"Controls": dict(CONTROLS), "Champion": None,
		"Hive": {"OverwriteMaxTotalCount": 0, "KeepMonsterCountType": 0},
	}


# ================
# JupiterImportTest
# ================
class JupiterImportTest(unittest.TestCase):
	def setUp(self):
		jupiter._verified_archives.clear()

	# ================
	# fixture
	#
	# A scratch world: a fake v1.696 root (one Media.pk2), a native
	# root, an output textdata with native rows, and a population
	# export naming one field kind and one temple kind.
	# ================
	def fixture(self, base, rows, export_rows, extra_rows=()):
		v1696, native, output, source = base / "v1696", base / "native", base / "output", base / "source"
		text_root = output / jupiter.TEXT_ROOT
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
		field = later_row("MOB_JUPITER_FIELDKIND", "UIO_OBJ_MOB_JUPITER_FIELDKIND", 115)
		field[89] = "7001"
		temple = later_row("MOB_JUPITER_TEMPLEKIND", "UIO_OBJ_MOB_JUPITER_TEMPLEKIND", 118, grade=3)
		extra = list(extra_rows)
		media = {
			"characterdata.txt": utf16([["characterdata_6000.txt"]]),
			"characterdata_6000.txt": utf16([field, temple] + extra),
			"skilldata.txt": utf16([["skilldata_6000.txt"]]),
			"skilldata_6000.txt": utf16([skill_row(7001, "MSKILL_JUPITER_TEST")]),
			"textdata_object.txt": utf16([
				["1", "UIO_OBJ_MOB_JUPITER_FIELDKIND", "x", "x", "Field kind", "f", "x", "a", "b", "c"],
				["2", "UIO_OBJ_MOB_JUPITER_TEMPLEKIND", "x", "x", "Temple kind", "t", "x", "a", "b", "c"]]
				+ [[str(3 + at), row[5], "x", "x", row[5], "f", "x", "a", "b", "c"] for at, row in enumerate(extra)]),
			"skilleffect.txt": utf16([
				["#section", "characterinfo"],
				["MOB_JUPITER_FIELDKIND"] + ["0"] * 12,
				["MOB_JUPITER_TEMPLEKIND"] + ["0"] * 12]
				+ [[row[2]] + ["0"] * 12 for row in extra]
				+ [["#section", "skillaniset"]]),
		}
		media_bytes = archive_bytes(media)
		(v1696 / "Media.pk2").write_bytes(media_bytes)
		(source / jupiter.POPULATION_NAME).write_text(json.dumps({
			"source": "fixture", "sourceSha256": jupiter.POPULATION_SOURCE_SHA256,
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
			(source / jupiter.POPULATION_NAME).write_text(json.dumps({
				"sourceSha256": "0" * 64, "worlds": WORLDS, "rows": [],
			}), encoding="utf-8")
			with self.assertRaises(ValueError):
				jupiter.read_population(source)
			(source / jupiter.POPULATION_NAME).write_text(json.dumps({
				"sourceSha256": jupiter.POPULATION_SOURCE_SHA256, "worlds": {"90": WORLDS["90"]}, "rows": [],
			}), encoding="utf-8")
			with self.assertRaises(ValueError):
				jupiter.read_population(source)

	# ================
	# test_projection_strips_extraction_facts_and_carries_spawn_type
	# ================
	def test_projection_strips_extraction_facts_and_carries_spawn_type(self):
		rows = [nest(2, "MOB_JUPITER_TEMPLEKIND", 86, region=31723, spawn_type=1),
			nest(1, "MOB_JUPITER_FIELDKIND", 90)]
		projected = jupiter.project_population(rows, (90,))
		self.assertEqual([row["NestID"] for row in projected["Rows"]], [1])
		self.assertNotIn("SpawnType", projected["Rows"][0])
		self.assertNotIn("Continent", projected["Rows"][0])
		self.assertNotIn("Hive", projected["Rows"][0])
		temple = jupiter.project_population(rows, jupiter.TEMPLE_WORLDS)
		self.assertEqual(temple["Rows"][0]["SpawnType"], 1)
		bad = nest(3, "MOB_JUPITER_FIELDKIND", 90)
		bad["Hive"] = {"OverwriteMaxTotalCount": 0, "KeepMonsterCountType": 1}
		with self.assertRaises(ValueError):
			jupiter.project_population([bad], (90,))
		bad = nest(3, "MOB_JUPITER_FIELDKIND", 90)
		bad["NestFlag"] = 1
		with self.assertRaises(ValueError):
			jupiter.project_population([bad], (90,))
		# A hive total cap projects onto the kind's first nest (lowest
		# NestID), with the cap as its count; the other spots drop.
		spots = [nest(6, jupiter.GUEST_CODE, 90), nest(5, jupiter.GUEST_CODE, 90)]
		for spot in spots:
			spot["Hive"] = {"OverwriteMaxTotalCount": 1, "KeepMonsterCountType": 0}
			spot["MaxCount"] = 3
		projected = jupiter.project_population(spots, (90,))
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
		text = jupiter.later_object_text(lambda name: files[name.lower()])
		self.assertIn("SN_ONE", text)
		self.assertIn("SN_TWO", text)
		flat = {"textdata_object.txt": utf16([["1", "SN_FLAT", "x"], ["2", "SN_FLAT2", "y"]])}
		self.assertIn("SN_FLAT2", jupiter.later_object_text(lambda name: flat[name.lower()]))

	# ================
	# test_later_text_row_maps_the_17_column_layout
	# ================
	def test_later_text_row_maps_the_17_column_layout(self):
		row = ["1", "800", "SN_MOB_TEST", "한국어"] + ["0"] * 5 + ["English"] + ["0"] * 3 + ["English"] * 4
		self.assertEqual(len(row), 17)
		mapped = jupiter.later_text_row(row)
		self.assertEqual(len(mapped), 15)
		self.assertEqual(mapped[1], "SN_MOB_TEST")
		self.assertEqual(mapped[3], "")
		self.assertEqual(mapped[4], "English")
		self.assertEqual(mapped[8], "English")
		vsro = ["1", "SN_MOB_TEST"] + ["x"] * 13
		self.assertEqual(jupiter.later_text_row(vsro), vsro)

	# ================
	# test_family_admits_the_authored_window_only
	# ================
	def test_family_admits_the_authored_window_only(self):
		self.assertTrue(jupiter.family_admits("MOB_JUPITER_A", later_row("MOB_JUPITER_A", "S", 111)))
		self.assertTrue(jupiter.family_admits("MOB_JUPITER_A", later_row("MOB_JUPITER_A", "S", 120, grade=8)))
		self.assertFalse(jupiter.family_admits("MOB_JUPITER_A", later_row("MOB_JUPITER_A", "S", 110)))
		self.assertFalse(jupiter.family_admits("MOB_JUPITER_A", later_row("MOB_JUPITER_A", "S", 115, grade=2)))
		self.assertFalse(jupiter.family_admits("MOB_SD_A", later_row("MOB_SD_A", "S", 115)))
		# The field's named guest joins on the same window, by name only.
		self.assertTrue(jupiter.family_admits(jupiter.GUEST_CODE, later_row(jupiter.GUEST_CODE, "S", 120, grade=3)))
		self.assertFalse(jupiter.family_admits(jupiter.GUEST_CODE, later_row(jupiter.GUEST_CODE, "S", 121, grade=3)))
		self.assertFalse(jupiter.family_admits(jupiter.GUEST_CODE, later_row(jupiter.GUEST_CODE, "S", 120, grade=2)))
		self.assertFalse(jupiter.family_admits("MOB_ARABIA_SOLDIER", later_row("MOB_ARABIA_SOLDIER", "S", 115)))
		self.assertFalse(jupiter.family_admits("NPC_JUPITER_SOLDIER_01", later_row("NPC_JUPITER_SOLDIER_01", "S", 0)))

	# ================
	# test_temple_requires_the_field_flag
	# ================
	def test_temple_requires_the_field_flag(self):
		with tempfile.TemporaryDirectory() as root:
			base = Path(root)
			with mock.patch.dict(os.environ, {"SRO_JUPITER": "", "SRO_JUPITER_TEMPLE": "on"}):
				with self.assertRaises(ValueError):
					jupiter.add_monsters(base / "v1696", base / "native", base / "output", base / "source")

	# ================
	# test_import_writes_shard_populations_and_marker
	# ================
	def test_import_writes_shard_populations_and_marker(self):
		rows = [nest(1, "MOB_JUPITER_FIELDKIND", 90),
			nest(2, "MOB_JUPITER_TEMPLEKIND", 86, region=31723, spawn_type=1)]
		with tempfile.TemporaryDirectory() as root:
			v1696, native, output, source, text_root, pins = self.fixture(Path(root), None, rows)
			with mock.patch.object(jupiter, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, {"SRO_JUPITER": "on", "SRO_JUPITER_TEMPLE": ""}):
					summary = jupiter.add_monsters(v1696, native, output, source)
				self.assertEqual(summary["families"]["field"]["nests"], 1)
				self.assertNotIn("temple", summary["families"])
				self.assertFalse((text_root / jupiter.TEMPLE_FILE).exists())
				field_file = json.loads((text_root / jupiter.FIELD_FILE).read_text(encoding="utf-8"))
				self.assertEqual(field_file["Version"], 2)
				self.assertEqual(field_file["Rows"][0]["WorldID"], 90)
				shard = (text_root / jupiter.CHARACTER_SHARD).read_bytes().decode("utf-16")
				self.assertIn("MOB_JUPITER_FIELDKIND", shard)
				self.assertNotIn("MOB_JUPITER_TEMPLEKIND", shard)
				converted = [line.split("\t") for line in shard.splitlines() if "MOB_JUPITER_FIELDKIND" in line][0]
				self.assertEqual(len(converted), 120)
				self.assertEqual(converted[1], "101")
				names = (text_root / "textdataname.txt").read_bytes().decode("utf-16")
				self.assertIn("UIO_OBJ_MOB_JUPITER_FIELDKIND", names)
				with mock.patch.dict(os.environ, {"SRO_JUPITER": "on", "SRO_JUPITER_TEMPLE": "on"}):
					summary = jupiter.add_monsters(v1696, native, output, source)
				self.assertEqual(summary["families"]["temple"]["nests"], 1)
				temple_file = json.loads((text_root / jupiter.TEMPLE_FILE).read_text(encoding="utf-8"))
				self.assertEqual(temple_file["Rows"][0]["SpawnType"], 1)
				self.assertEqual(summary["characters"]["MOB_JUPITER_FIELDKIND"], 101)

	# ================
	# test_off_rebuild_reaps_owned_outputs
	# ================
	def test_off_rebuild_reaps_owned_outputs(self):
		rows = [nest(1, "MOB_JUPITER_FIELDKIND", 90)]
		with tempfile.TemporaryDirectory() as root:
			v1696, native, output, source, text_root, pins = self.fixture(Path(root), None, rows)
			with mock.patch.object(jupiter, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, {"SRO_JUPITER": "on", "SRO_JUPITER_TEMPLE": ""}):
					jupiter.add_monsters(v1696, native, output, source)
				with mock.patch.dict(os.environ, {"SRO_JUPITER": "", "SRO_JUPITER_TEMPLE": ""}):
					self.assertEqual(jupiter.add_monsters(v1696, native, output, source), {"enabled": False})
			for name in (jupiter.CHARACTER_SHARD, jupiter.SKILL_SHARD, jupiter.FIELD_FILE, jupiter.MARKER):
				self.assertFalse((text_root / name).exists() if name != jupiter.MARKER else (output / name).exists())
			names = (text_root / "textdataname.txt").read_bytes().decode("utf-16")
			self.assertIn("UIO_OBJ_MOB_NATIVE", names)
			self.assertNotIn("MOB_JUPITER_FIELDKIND", names)
			effects_text = (text_root / "skilleffect.txt").read_bytes().decode("utf-16")
			self.assertIn("MOB_NATIVE", effects_text)
			self.assertNotIn("MOB_JUPITER_FIELDKIND", effects_text)

	# ================
	# test_guest_joins_the_field_and_npcs_stay_out
	#
	# The real export places one Arabia guest and the area's NPCs in
	# the field: the guest is imported by name, the NPC rows (roster
	# content) never reach the population or the shard.
	# ================
	def test_guest_joins_the_field_and_npcs_stay_out(self):
		rows = [nest(1, "MOB_JUPITER_FIELDKIND", 90),
			nest(2, jupiter.GUEST_CODE, 90),
			nest(3, "NPC_JUPITER_SOLDIER_01", 90)]
		extra = [later_row(jupiter.GUEST_CODE, "UIO_OBJ_MOB_ARABIA_KIDEMONAS", 120, grade=3),
			later_row("NPC_JUPITER_SOLDIER_01", "UIO_OBJ_NPC_JUPITER_SOLDIER_01", 0)]
		with tempfile.TemporaryDirectory() as root:
			v1696, native, output, source, text_root, pins = self.fixture(Path(root), None, rows, extra)
			with mock.patch.object(jupiter, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, {"SRO_JUPITER": "on", "SRO_JUPITER_TEMPLE": ""}):
					summary = jupiter.add_monsters(v1696, native, output, source)
			self.assertEqual(summary["npcRows"], 1)
			self.assertEqual(summary["families"]["field"]["nests"], 2)
			self.assertIn(jupiter.GUEST_CODE, summary["characters"])
			self.assertNotIn("NPC_JUPITER_SOLDIER_01", summary["characters"])
			field_file = json.loads((text_root / jupiter.FIELD_FILE).read_text(encoding="utf-8"))
			self.assertEqual({row["Codename"] for row in field_file["Rows"]},
				{"MOB_JUPITER_FIELDKIND", jupiter.GUEST_CODE})
			shard = (text_root / jupiter.CHARACTER_SHARD).read_bytes().decode("utf-16")
			self.assertIn(jupiter.GUEST_CODE, shard)
			self.assertNotIn("NPC_JUPITER_SOLDIER_01", shard)

	# ================
	# test_unadmitted_or_missing_claimed_kind_refuses
	# ================
	def test_unadmitted_or_missing_claimed_kind_refuses(self):
		rows = [nest(1, "MOB_JUPITER_FIELDKIND", 90), nest(2, "MOB_JUPITER_GHOST", 90)]
		with tempfile.TemporaryDirectory() as root:
			v1696, native, output, source, text_root, pins = self.fixture(Path(root), None, rows)
			with mock.patch.object(jupiter, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, {"SRO_JUPITER": "on", "SRO_JUPITER_TEMPLE": ""}):
					with self.assertRaises(ValueError):
						jupiter.add_monsters(v1696, native, output, source)


if __name__ == "__main__":
	unittest.main()
