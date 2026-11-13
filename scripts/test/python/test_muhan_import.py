"""
===========================================================================
test_muhan_import.py - the Muhan wave import's contract.

The family admits only the pinned population export, converts the
v1.696 kinds its waves name by level (exactly one per level), writes
the wave file, and a switch off reaps exactly what the marker enrolled.
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

import jupiter_import as jupiter  # noqa: E402
import muhan_import as muhan  # noqa: E402

WORLDS = {
	"103": {"CodeName": "GROUP_MUHAN_SOLO_75", "Type": 0, "Regions": [27091, 27092]},
	"104": {"CodeName": "GROUP_MUHAN_SOLO_85", "Type": 0, "Regions": [27091, 27092]},
	"105": {"CodeName": "GROUP_MUHAN_PARTY_75", "Type": 0, "Regions": [27091, 27092]},
	"106": {"CodeName": "GROUP_MUHAN_PARTY_85", "Type": 0, "Regions": [27091, 27092]},
	"108": {"CodeName": "GROUP_MUHAN_SOLO_95", "Type": 0, "Regions": [27091, 27092]},
	"109": {"CodeName": "GROUP_MUHAN_SOLO_105", "Type": 0, "Regions": [27091, 27092]},
	"110": {"CodeName": "GROUP_MUHAN_PARTY_95", "Type": 0, "Regions": [27091, 27092]},
	"111": {"CodeName": "GROUP_MUHAN_PARTY_105", "Type": 0, "Regions": [27091, 27092]},
}
# Wave 1 steps one level below the band, wave 20 one above, the rest
# sit on it: the levels the fixture's waves can name.
NEEDED_LEVELS = (74, 75, 76, 84, 85, 86, 94, 95, 96, 104, 105, 106)


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
# wave
# ================
def wave(world, seq, offset=0, region=27091, count=3):
	return {"WorldID": world, "Seq": seq, "RegionID": region, "X": 1248.0, "Y": 378.0, "Z": 1379.0,
		"DelayMs": 5000, "Count": count, "LevelOffset": offset, "Radius": 50, "MonsterType": 0}


# ================
# export_waves
# ================
def export_waves():
	waves = []
	for world in muhan.WAVE_WORLDS:
		for seq in range(1, 21):
			offset = -1 if seq == 1 else (1 if seq == 20 else 0)
			waves.append(wave(world, seq, offset))
	return waves


# ================
# nest
# ================
def nest(code, nest_id, world, region=27091):
	return {
		"Codename": code, "NestID": nest_id, "WorldID": world, "RegionID": region,
		"X": 779.0, "Y": 628.0, "Z": 1108.0, "Radius": 0, "GenerateRadius": 0,
		"MaxCount": 1, "RespawnDelayMinSec": 0, "RespawnDelayMaxSec": 0,
		"Respawn": True, "InitialDir": 0, "SpawnType": 0, "ChampionGenPercentage": 0,
		"Controls": {"ID": 1, "ObjectID": 40421}, "Champion": None,
	}


# ================
# export_document
# ================
def export_document(waves):
	return {
		"source": "fixture", "sourceSha256": muhan.POPULATION_SOURCE_SHA256,
		"worlds": {key: {**row, "Regions": list(row["Regions"])} for key, row in WORLDS.items()},
		"waves": waves,
		"npcNests": [nest("NPC_MUHAN_SOLO_75_MANAGER", 39604, 103), nest("NPC_MUHAN_CHANGE_SOLO_75_A", 39605, 103)],
		"transMask": [{"NpcCodename": "NPC_MUHAN_CHANGE_SOLO_75_A"}] * 2,
	}


# ================
# MuhanImportTest
# ================
class MuhanImportTest(unittest.TestCase):
	def setUp(self):
		jupiter._verified_archives.clear()

	# ================
	# fixture
	#
	# A scratch world: a fake v1.696 root (one Media.pk2), a native
	# root, an output textdata with native rows, and a population
	# export whose waves name twelve levels.
	# ================
	def fixture(self, base, kinds, waves, extra_rows=()):
		v1696, native, output, source = base / "v1696", base / "native", base / "output", base / "source"
		text_root = output / muhan.TEXT_ROOT
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
			"skilldata_6000.txt": utf16([skill_row(7001, "MSKILL_MUHAN_TEST")]),
			"textdata_object.txt": utf16(
				[[str(1 + at), row[5], "x", "x", row[5], "f", "x", "a", "b", "c"] for at, row in enumerate(kinds + extra)]),
			"skilleffect.txt": utf16([
				["#section", "characterinfo"]]
				+ [[row[2]] + ["0"] * 12 for row in kinds + extra]
				+ [["#section", "skillaniset"]]),
		}
		media_bytes = archive_bytes(media)
		(v1696 / "Media.pk2").write_bytes(media_bytes)
		(source / muhan.POPULATION_NAME).write_text(json.dumps(export_document(waves)), encoding="utf-8")
		pins = {"Media": hashlib.sha256(media_bytes).hexdigest()}
		return v1696, native, output, source, text_root, pins

	# ================
	# kinds
	#
	# One combat kind per needed level, the first carrying a skill.
	# ================
	def kinds(self):
		rows = [later_row(f"MOB_MUHAN_L{level}", f"UIO_OBJ_MOB_MUHAN_L{level}", level) for level in NEEDED_LEVELS]
		rows[1][89] = "7001"
		rows.append(later_row("NPC_MUHAN_SOLO_75_MANAGER", "UIO_OBJ_NPC_MUHAN_SOLO_75_MANAGER", 0))
		rows.append(later_row("NPC_MUHAN_CHANGE_SOLO_75_A", "UIO_OBJ_NPC_MUHAN_CHANGE_SOLO_75_A", 0))
		return rows

	# ================
	# test_only_the_pinned_population_export_is_admitted
	# ================
	def test_only_the_pinned_population_export_is_admitted(self):
		with tempfile.TemporaryDirectory() as root:
			source = Path(root)
			document = export_document(export_waves())
			document["sourceSha256"] = "0" * 64
			(source / muhan.POPULATION_NAME).write_text(json.dumps(document), encoding="utf-8")
			with self.assertRaises(ValueError):
				muhan.read_population(source)
			document = export_document(export_waves())
			del document["worlds"]["111"]
			(source / muhan.POPULATION_NAME).write_text(json.dumps(document), encoding="utf-8")
			with self.assertRaises(ValueError):
				muhan.read_population(source)
			document = export_document([row for row in export_waves() if not (row["WorldID"] == 103 and row["Seq"] == 7)])
			(source / muhan.POPULATION_NAME).write_text(json.dumps(document), encoding="utf-8")
			with self.assertRaises(ValueError):
				muhan.read_population(source)

	# ================
	# test_bands_levels_and_admission
	# ================
	def test_bands_levels_and_admission(self):
		export = export_document(export_waves())
		self.assertEqual(muhan.world_band("GROUP_MUHAN_PARTY_105"), 105)
		with self.assertRaises(ValueError):
			muhan.world_band("GROUP_MUHAN_SOLO_A")
		self.assertEqual(muhan.needed_levels(export), set(NEEDED_LEVELS))
		levels = set(NEEDED_LEVELS)
		self.assertTrue(muhan.family_admits("MOB_MUHAN_L75", later_row("MOB_MUHAN_L75", "S", 75), levels))
		# The transform forms, a graded kind and a level no wave names
		# stay out; the arena NPCs join on codename (#3314).
		self.assertFalse(muhan.family_admits("MOB_MUHAN_CHANGE_L75", later_row("MOB_MUHAN_CHANGE_L75", "S", 75), levels))
		self.assertFalse(muhan.family_admits("MOB_MUHAN_L75", later_row("MOB_MUHAN_L75", "S", 75, grade=1), levels))
		self.assertFalse(muhan.family_admits("MOB_MUHAN_L81", later_row("MOB_MUHAN_L81", "S", 81), levels))
		self.assertTrue(muhan.family_admits("NPC_MUHAN_SOLO_75_MANAGER", later_row("NPC_MUHAN_SOLO_75_MANAGER", "S", 0), levels))

	# ================
	# test_projection_refuses_waves_outside_their_world
	# ================
	def test_projection_refuses_waves_outside_their_world(self):
		export = export_document(export_waves())
		projected = muhan.project_waves(export)
		self.assertEqual(projected["Version"], 2)
		self.assertEqual(len(projected["Rows"]), 160)
		self.assertEqual(projected["Rows"][0]["WorldID"], 103)
		bad = export_document(export_waves())
		bad["waves"][0]["RegionID"] = 99999
		with self.assertRaises(ValueError):
			muhan.project_waves(bad)
		bad = export_document(export_waves())
		bad["waves"][0]["Count"] = 0
		with self.assertRaises(ValueError):
			muhan.project_waves(bad)

	# ================
	# test_import_writes_shard_waves_and_marker
	# ================
	def test_import_writes_shard_waves_and_marker(self):
		extra = [
			later_row("MOB_MUHAN_CHANGE_L75", "UIO_OBJ_MOB_MUHAN_CHANGE_L75", 75),
			later_row("MOB_MUHAN_L81", "UIO_OBJ_MOB_MUHAN_L81", 81),
			later_row("MOB_MUHAN_GRADED", "UIO_OBJ_MOB_MUHAN_GRADED", 75, grade=1),
		]
		with tempfile.TemporaryDirectory() as root:
			v1696, native, output, source, text_root, pins = self.fixture(Path(root), self.kinds(), export_waves(), extra)
			with mock.patch.object(jupiter, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, {"SRO_MUHAN": "on"}):
					summary = muhan.add_monsters(v1696, native, output, source)
			self.assertEqual(sorted(summary["characters"].values()), list(range(101, 115)))
			self.assertEqual(summary["levels"], sorted(NEEDED_LEVELS))
			self.assertEqual(summary["waves"], {str(world): 20 for world in muhan.WAVE_WORLDS})
			self.assertEqual(summary["npcRows"], 2)
			self.assertEqual(summary["npcNests"], 2)
			self.assertEqual(summary["transMaskRows"], 2)
			waves_file = json.loads((text_root / muhan.WAVES_FILE).read_text(encoding="utf-8"))
			self.assertEqual(waves_file["Version"], 2)
			self.assertEqual(len(waves_file["Rows"]), 160)
			self.assertEqual(waves_file["Rows"][0], {"WorldID": 103, "Seq": 1, "RegionID": 27091,
				"X": 1248.0, "Y": 378.0, "Z": 1379.0, "DelayMs": 5000, "Count": 3,
				"LevelOffset": -1, "Radius": 50, "MonsterType": 0})
			shard = (text_root / muhan.CHARACTER_SHARD).read_bytes().decode("utf-16")
			self.assertIn("MOB_MUHAN_L75", shard)
			self.assertNotIn("MOB_MUHAN_CHANGE_L75", shard)
			self.assertNotIn("MOB_MUHAN_L81", shard)
			self.assertNotIn("MOB_MUHAN_GRADED", shard)
			self.assertIn("NPC_MUHAN_SOLO_75_MANAGER", shard)
			npcs_file = json.loads((text_root / muhan.NPCS_FILE).read_text(encoding="utf-8"))
			self.assertEqual(len(npcs_file["Rows"]), 2)
			self.assertEqual(npcs_file["Rows"][0]["Codename"], "NPC_MUHAN_SOLO_75_MANAGER")
			self.assertEqual(npcs_file["Rows"][0]["WorldID"], 103)
			converted = [line.split("\t") for line in shard.splitlines() if "MOB_MUHAN_L75" in line][0]
			self.assertEqual(len(converted), 120)
			names = (text_root / "textdataname.txt").read_bytes().decode("utf-16")
			self.assertIn("UIO_OBJ_MOB_MUHAN_L75", names)

	# ================
	# test_a_level_must_resolve_to_exactly_one_kind
	# ================
	def test_a_level_must_resolve_to_exactly_one_kind(self):
		with tempfile.TemporaryDirectory() as root:
			kinds = self.kinds() + [later_row("MOB_MUHAN_L75_TWIN", "UIO_OBJ_MOB_MUHAN_L75_TWIN", 75)]
			v1696, native, output, source, text_root, pins = self.fixture(Path(root), kinds, export_waves())
			with mock.patch.object(jupiter, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, {"SRO_MUHAN": "on"}):
					with self.assertRaises(ValueError):
						muhan.add_monsters(v1696, native, output, source)
		with tempfile.TemporaryDirectory() as root:
			kinds = [row for row in self.kinds() if row[2] != "MOB_MUHAN_L85"]
			v1696, native, output, source, text_root, pins = self.fixture(Path(root), kinds, export_waves())
			with mock.patch.object(jupiter, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, {"SRO_MUHAN": "on"}):
					with self.assertRaises(ValueError):
						muhan.add_monsters(v1696, native, output, source)

	# ================
	# test_off_rebuild_reaps_owned_outputs
	# ================
	def test_off_rebuild_reaps_owned_outputs(self):
		with tempfile.TemporaryDirectory() as root:
			v1696, native, output, source, text_root, pins = self.fixture(Path(root), self.kinds(), export_waves())
			with mock.patch.object(jupiter, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, {"SRO_MUHAN": "on"}):
					muhan.add_monsters(v1696, native, output, source)
				with mock.patch.dict(os.environ, {"SRO_MUHAN": ""}):
					self.assertEqual(muhan.add_monsters(v1696, native, output, source), {"enabled": False})
			for name in (muhan.CHARACTER_SHARD, muhan.SKILL_SHARD, muhan.WAVES_FILE, muhan.NPCS_FILE):
				self.assertFalse((text_root / name).exists())
			self.assertFalse((output / muhan.MARKER).exists())
			names = (text_root / "textdataname.txt").read_bytes().decode("utf-16")
			self.assertIn("UIO_OBJ_MOB_NATIVE", names)
			self.assertNotIn("MOB_MUHAN_L75", names)
			effects_text = (text_root / "skilleffect.txt").read_bytes().decode("utf-16")
			self.assertIn("MOB_NATIVE", effects_text)
			self.assertNotIn("MOB_MUHAN_L75", effects_text)


if __name__ == "__main__":
	unittest.main()
