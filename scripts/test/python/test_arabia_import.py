"""
===========================================================================
test_arabia_import.py - the Arabia import's admission and build rules.
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import arabia_gates as gates
import arabia_import as arabia
import cap110_item_import
import import_vsro_alexandria as tables
import skytemple_import
import sro_pk2
from test_skytemple_import import (
	FakeClosure,
	character,
	data_archive,
	map_archive,
	mapinfo,
	media_archive,
	placement_o2,
	read_rows,
	region_navmesh,
	skill,
	terrain_m,
	utf16,
	write_table,
)

# One region per family, at the source's own sector spacing.
# The town sector's one placed object: an id the native object.ifo
# holds as another model, so the import moves it past the native ids.
TOWN_REGION = 22106
TOWN_OBJECT = 777
MOVED_OBJECT = 901
REGIONS = [(22106, "TOWN"), (19792, "FIELD_01"), (20048, "FIELD_02"),
	(20559, "FIELD_03"), (21325, "PETRA"), (23378, "DUNGEON"), (31734, "GARDEN")]
REGION_IDS = {region for region, _family in REGIONS}


# ================
# controls
# ================
def controls(**overrides):
	row = {key: 0 for key in arabia.CONTROL_KEYS}
	row["SightRange"] = 100
	row["DiversionBasisData"] = [0] * 8
	row["DiversionKeepBasisData"] = [0] * 8
	row.update(overrides)
	return row


# ================
# nest
#
# One extraction row in the research file's shape.
# ================
def nest(identity, code, region, world=1, spawn=0, continent="ARABIA_FIELD_01", hive=None):
	return {
		"Codename": code, "NestID": identity, "WorldID": world, "RegionID": region,
		"Continent": continent, "X": 100.5, "Y": 0, "Z": 200.25, "Radius": 0,
		"GenerateRadius": 0, "MaxCount": 1, "RespawnDelayMinSec": 60,
		"RespawnDelayMaxSec": 120, "Respawn": True, "InitialDir": 7,
		"SpawnType": spawn, "NestFlag": 0, "ChampionGenPercentage": 0,
		"Controls": controls(), "Champion": None,
		"Hive": hive or {"OverwriteMaxTotalCount": 0, "KeepMonsterCountType": 0},
	}


# ================
# extraction
# ================
def extraction():
	rows = [
		nest(11, "MOB_AR_TEST", 19792),
		nest(12, "MOB_AR_TEST", 23378, world=112, spawn=1, continent="ARABIA_DUNGEON"),
		nest(13, "MOB_ARABIA_UNIQUE", 20048, continent="ARABIA_FIELD_02",
			hive={"OverwriteMaxTotalCount": 1, "KeepMonsterCountType": 0}),
		nest(14, "MOB_ARABIA_UNIQUE", 20559, continent="ARABIA_FIELD_03",
			hive={"OverwriteMaxTotalCount": 1, "KeepMonsterCountType": 0}),
		nest(15, "NPC_AR_TEST", 22106, continent="ARABIA_TOWN"),
	]
	return {"source": "fake.bak", "sourceSha256": "ab" * 32,
		"worlds": {"112": {"CodeName": "GROUP_ARABIA_DUNGEON", "Type": 0, "Regions": [23378]}},
		"rows": rows}


# Jangan's native gate, which lists Baghdad among its destinations.
NATIVE_GATE_DATA = [["1", "1", "GATE_CH", "2094", "SN_ZONE_CH", "25000", "969", "0", "1369", "0", "0", "0", "1"]]


# Baghdad's own Portals: (teleport id, code, building id, name key).
CITY_GATES = [
	("239", "GATE_AR_M", "40790", "SN_NPC_AR_GATE_04"),
	("240", "GATE_AR_N", "40791", "SN_NPC_AR_GATE_02"),
	("241", "GATE_AR_W", "40792", "SN_NPC_AR_GATE_01"),
	("242", "GATE_AR_S", "40793", "SN_NPC_AR_GATE_03"),
]


# ================
# gate_tables
# ================
def gate_tables():
	def building(identity, code, name, region, x, y, z):
		row = ["0"] * 58
		row[0], row[1], row[2], row[5] = "1", identity, code, name
		row[41], row[43], row[44], row[45] = region, x, y, z
		return row

	data = [
		["1", "246", "GATE_ARABIA_DUNGEON_START", "40797", "SN_NPC_AR_DSTARTPORTAL", "23378", "1577", "-430", "1105", "0", "0", "0", "112"],
		["1", "247", "GATE_ARABIA_DUNGEON_END", "40798", "SN_NPC_AR_DSTARTPORTAL", "23891", "959", "-236", "527", "0", "0", "0", "112"],
		["1", "249", "GATE_BAGH_TO_ARABIA_DUNGEON", "40489", "SN_NPC_AR_KAREMPORTAL", "22106", "443", "-432", "1529", "0", "0", "0", "1"],
		["1", "353", "GATE_TO_LAST_FIELD_IN_BAGHDAD", "43990", "SN_ZONE_22010", "22106", "1579", "-433", "1738", "0", "0", "0", "1"],
	] + [["1", identity, code, building_id, name, "22106", "1316", "-432", "832", "0", "0", "0", "1"]
		for identity, code, building_id, name in CITY_GATES]
	buildings = [
		building("40797", "STORE_ARABIA_DUNGEON_START_GATE", "SN_STORE_ARABIA_DUNGEON_START_GATE", "23378", "1589", "-430", "1200"),
		building("40798", "STORE_ARABIA_DUNGEON_END_GATE", "SN_STORE_ARABIA_DUNGEON_END_GATE", "23891", "1012", "-236", "606"),
	] + [building(building_id, "STORE_AR_GATE" + code[7:], "SN_STORE_AR_GATE" + code[7:], "22106", "1224", "-432", "767")
		for _identity, code, building_id, _name in CITY_GATES]
	link = ["1", "249", "246", "0", "1", "1", "121", "999"] + ["0"] * 15
	links = [link, ["1", "246", "249"] + link[3:], ["1", "247", "249"] + link[3:]]
	# The city network: every Portal to the other three, condition-free.
	city_ids = [identity for identity, _code, _building, _name in CITY_GATES]
	links += [["1", a, b] + ["0"] * 20 for a in city_ids for b in city_ids if a != b]
	return data, buildings, links


class ArabiaHelpersTest(unittest.TestCase):
	# ================
	# test_continent_regions
	# ================
	def test_continent_regions(self):
		rows = [["1", str(region), f"RN_ARABIA_{family}", ""] for region, family in REGIONS]
		rows.append(["1", "25000", "RN_CH_JANGAN", ""])
		regions = arabia.continent_regions(rows)
		self.assertEqual(regions, {region: family for region, family in REGIONS})
		with self.assertRaisesRegex(ValueError, "unknown Arabia family"):
			arabia.continent_regions(rows + [["1", "9999", "RN_ARABIA_NOWHERE", ""]])
		with self.assertRaisesRegex(ValueError, "lacks Arabia families"):
			arabia.continent_regions([row for row in rows if row[2] != "RN_ARABIA_GARDEN"])

	# ================
	# test_load_nests_projects_the_envelope
	# ================
	def test_load_nests_projects_the_envelope(self):
		with tempfile.TemporaryDirectory() as tmp:
			path = Path(tmp) / "nests.json"
			path.write_text(json.dumps(extraction()))
			loaded = arabia.load_nests(path, REGION_IDS)
		self.assertEqual([row["NestID"] for row in loaded["monsters"]], [11, 12, 13, 14])
		self.assertEqual(len(loaded["npcs"]), 1)
		self.assertEqual(loaded["hiveCapped"], [13, 14])
		dungeon = loaded["monsters"][1]
		self.assertEqual((dungeon["WorldID"], dungeon["SpawnType"]), (112, 1))
		capped = loaded["monsters"][2]
		self.assertEqual((capped["HiveKey"], capped["HiveMaxCount"]), ("MOB_ARABIA_UNIQUE", 1))
		for row in loaded["monsters"]:
			self.assertLessEqual(set(row), arabia.ROW_KEYS)
			self.assertNotIn("Continent", row)
			self.assertNotIn("Hive", row)

	# ================
	# test_load_nests_normalises_inverted_delays
	# ================
	def test_load_nests_normalises_inverted_delays(self):
		data = extraction()
		data["rows"][0]["RespawnDelayMinSec"] = 7
		data["rows"][0]["RespawnDelayMaxSec"] = 1
		with tempfile.TemporaryDirectory() as tmp:
			path = Path(tmp) / "nests.json"
			path.write_text(json.dumps(data))
			loaded = arabia.load_nests(path, REGION_IDS)
		row = loaded["monsters"][0]
		self.assertEqual((row["RespawnDelayMinSec"], row["RespawnDelayMaxSec"]), (7, 7))
		self.assertEqual(loaded["delaysNormalised"], [11])

	# ================
	# test_load_nests_refuses_contract_breaks
	# ================
	def test_load_nests_refuses_contract_breaks(self):
		def broken(mutate):
			data = extraction()
			mutate(data["rows"])
			with tempfile.TemporaryDirectory() as tmp:
				path = Path(tmp) / "nests.json"
				path.write_text(json.dumps(data))
				with self.assertRaises(ValueError):
					arabia.load_nests(path, REGION_IDS)

		broken(lambda rows: rows[0].update({"WorldID": 113}))
		broken(lambda rows: rows[0].update({"RegionID": 25000}))
		broken(lambda rows: rows[0].update({"NestFlag": 1}))
		broken(lambda rows: rows[0].update({"Codename": "MOB_SD_BASTET"}))
		broken(lambda rows: rows[0]["Controls"].update({"Bogus": 1}))
		broken(lambda rows: rows[0].update({"Hive": {"OverwriteMaxTotalCount": 0, "KeepMonsterCountType": 1}}))

	# ================
	# test_npc_placements_union
	# ================
	def test_npc_placements_union(self):
		placed = arabia.npc_placements(extraction()["rows"], [
			["50003", "22106", "100.5", "0", "200.25"],
			["50003", "21325", "10", "1", "20"],
			["50001", "21325", "10", "1", "20"],
		], {"50001": "MOB_AR_TEST", "50003": "NPC_AR_TEST"}, REGION_IDS)
		# The authored town standing wins over the client's duplicate,
		# the client's Petra standing joins, the monster stays out.
		self.assertEqual(placed, [
			("NPC_AR_TEST", 21325, 10.0, 1.0, 20.0),
			("NPC_AR_TEST", 22106, 100.5, 0.0, 200.25),
		])

	# ================
	# test_prepare_gates_authors_the_chain
	# ================
	def test_prepare_gates_authors_the_chain(self):
		data, buildings, links = gate_tables()
		later = {"teleportdata.txt": data, "teleportbuilding.txt": buildings,
			"teleportlink.txt": links, "text": b""}
		encoded, codes = gates.prepare_gates(later, NATIVE_GATE_DATA, {"1"}, {"2094"})
		self.assertEqual(codes, gates.GATE_CODES)
		rows = {row[2]: row for row in tables.table_rows(encoded["teleportdata.txt"])}
		# Baghdad is a destination of Jangan's own gate: no second
		# gate or building stands in Jangan to cover it.
		self.assertNotIn("GATE_CH", codes)
		jangan = rows["GATE_CH"]
		self.assertEqual(jangan, NATIVE_GATE_DATA[0])
		self.assertEqual(rows["GATE_ARABIA_DUNGEON_START"][12], "112")
		self.assertEqual(rows["GATE_ARABIA_DUNGEON_END"][12], "112")
		self.assertEqual(rows["GATE_BAGH_TO_ARABIA_DUNGEON"][12], "1")
		built = {row[2]: row for row in tables.table_rows(encoded["teleportbuilding.txt"])}
		self.assertFalse({row[41] for row in built.values()} & {"25000"})
		# The two town arrivals stand without a building: the ones
		# the source names were never shipped, and an authored one
		# had no model (an invisible click target, #3877).
		self.assertEqual(rows["GATE_TO_LAST_FIELD_IN_BAGHDAD"][3], "0")
		self.assertEqual(rows["GATE_BAGH_TO_ARABIA_DUNGEON"][3], "0")
		self.assertEqual(set(built), {"STORE_ARABIA_DUNGEON_START_GATE", "STORE_ARABIA_DUNGEON_END_GATE",
			"STORE_AR_GATE_M", "STORE_AR_GATE_N", "STORE_AR_GATE_W", "STORE_AR_GATE_S"})
		self.assertEqual(built["STORE_ARABIA_DUNGEON_START_GATE"][1], "40797")
		for _identity, code, building_id, _name in CITY_GATES:
			self.assertEqual(rows[code][3], building_id)
			self.assertEqual(built["STORE_AR_GATE" + code[7:]][1], building_id)
		link_rows = tables.table_rows(encoded["teleportlink.txt"])
		self.assertEqual({len(row) for row in link_rows}, {21})
		links = {(row[1], row[2]): row for row in link_rows}
		town = rows["GATE_TO_LAST_FIELD_IN_BAGHDAD"][1]
		arrival = rows["GATE_BAGH_TO_ARABIA_DUNGEON"][1]
		start = rows["GATE_ARABIA_DUNGEON_START"][1]
		# Jangan lists Baghdad; nothing leaves from an arrival.
		self.assertIn((jangan[1], town), links)
		self.assertFalse({pair for pair in links if pair[0] in (town, arrival)})
		# The dungeon's gates still lead back to the town arrival.
		self.assertIn((start, arrival), links)
		self.assertIn((rows["GATE_ARABIA_DUNGEON_END"][1], arrival), links)
		# Every Portal lists the other three, Jangan and the dungeon,
		# the last two on the source's level-121 entry link.
		city = [rows[code][1] for _identity, code, _building, _name in CITY_GATES]
		for source in city:
			self.assertEqual({pair[1] for pair in links if pair[0] == source},
				{other for other in city if other != source} | {jangan[1], start})
			self.assertEqual(links[(source, start)][6], "121")
			self.assertEqual(links[(source, jangan[1])][6], "121")
		self.assertEqual(links[(city[0], city[1])][6], "0")

	# ================
	# test_prepare_gates_refuses_unshipped_city_buildings
	# ================
	def test_prepare_gates_refuses_unshipped_city_buildings(self):
		data, buildings, links = gate_tables()
		later = {"teleportdata.txt": data, "teleportbuilding.txt": [row for row in buildings if row[1] != "40793"],
			"teleportlink.txt": links, "text": b""}
		with self.assertRaisesRegex(ValueError, "GATE_AR_S"):
			gates.prepare_gates(later, NATIVE_GATE_DATA, {"1"}, {"2094"})

	# ================
	# test_prepare_gates_renumbers_on_collision
	# ================
	def test_prepare_gates_renumbers_on_collision(self):
		data, buildings, links = gate_tables()
		later = {"teleportdata.txt": data, "teleportbuilding.txt": buildings,
			"teleportlink.txt": links, "text": b""}
		encoded, _ = gates.prepare_gates(later, NATIVE_GATE_DATA, {"246"}, {"40797"})
		rows = tables.table_rows(encoded["teleportdata.txt"])
		self.assertNotIn("246", {row[1] for row in rows})
		self.assertNotIn("40797", {row[3] for row in rows})


# ================
# build_trees
#
# A native extracted root, a variant output root and a v1.696 source
# root in tmp, small but complete. Returns (native, output, source, nests).
# ================
def build_trees(base):
	native, output, source = base / "native", base / "output", base / "source"
	text = arabia.TEXT_ROOT
	native_buildings = [["1", "2094", "STORE_CH_GATE"] + ["0"] * 38 + ["25000", "0", "1254", "-6", "1374"] + ["0"] * 12]
	write_table(native / text, "teleportbuilding.txt", native_buildings)
	write_table(native / text, "teleportdata.txt", NATIVE_GATE_DATA)
	write_table(native / text, "textdataname.txt", [
		["1", "SN_NATIVE", "x", "", "", "Native", "", "", "", "", ""],
		["1", "SN_NPC_NATIVE_NAMED", "x", "", "", "Native Named", "", "", "", "", ""],
	])
	write_table(native / text, "textquest.txt", [["1", "SN_NATIVE_QUEST", "x", "", "", "", "", "", "Quest", "", ""]])
	write_table(native / text, "npcchat.txt", [["1", "NPC_NATIVE_NAMED", "SN_NATIVE_CHAT", "0"]])

	native_char = ["0"] * 120
	native_char[0], native_char[1], native_char[2] = "1", "100", "MOB_NATIVE_TEST"
	# An NPC kind the native tables own that Arabia also places
	# (NPC_CH_GACHA_MACHINE's shape): it keeps this row and id 101.
	native_npc = ["0"] * 120
	native_npc[0], native_npc[1], native_npc[2] = "1", "101", "NPC_CH_NATIVE_SHOP"
	write_table(output / text, "characterdata_5000.txt", [native_char, native_npc])
	write_table(output / text, "skilldata.txt", [["skilldata_5000.txt"]])
	write_table(output / text, "skilldata_5000.txt", [skill(50, "SKILL_NATIVE")])
	write_table(output / text, "textdataname.txt", [
		["1", "SN_NATIVE", "x", "", "", "Native", "", "", "", "", ""],
		["1", "SN_NPC_NATIVE_NAMED", "x", "", "", "Native Named", "", "", "", "", ""],
	])
	(output / text / "skilleffect.txt").write_bytes(b"\xff\xfe" + (
		"#section\tcharacterinfo\r\nMOB_NATIVE\t0\r\n#section\tskillaniset2\r\n1\tname\tSKILL_NATIVE\tANI_TEST\r\n"
	).encode("utf-16-le"))
	write_table(output / text, "teleportdata.txt", NATIVE_GATE_DATA)
	write_table(output / text, "teleportbuilding.txt", native_buildings)
	write_table(output / text, "teleportlink.txt", [["1", "1", "1"] + ["0"] * 18])
	write_table(output / text, "regioncode.txt", [["1", "25000", "RN_CH_JANGAN", ""], ["1", "22106", "xxx", ""]])
	write_table(output / text, "textzonename.txt", [["1", "25000", "x", "", "", "Jangan", "", "", "", "", ""]])
	write_table(output / text, "textquest.txt", [["1", "SN_NATIVE_QUEST", "x", "", "", "", "", "", "Quest", "", ""]])
	write_table(output / text, "npcpos.txt", [["100", "25000", "1", "1", "1"], ["101", "25000", "2", "1", "2"]])
	write_table(output / text, "npcchat.txt", [["1", "NPC_NATIVE", "SN_NATIVE", "0"],
		["1", "NPC_NATIVE_NAMED", "SN_NATIVE_CHAT", "0"]])
	(output / "Map_extracted").mkdir(parents=True)
	(output / "Map_extracted" / "tile2d.ifo").write_bytes(
		'sig\n2\n00000 0 "nature" "old00.ddj"\n00005 0 "nature" "keep.ddj"\n'.encode("cp949"))
	(output / "Map_extracted" / "object.ifo").write_bytes(
		'sig\n2\n00777 0 "res\\native\\other.bsr"\n00900 0 "res\\native\\far.bsr"\n'.encode("cp949"))
	(output / "Data_extracted" / "navmesh").mkdir(parents=True)
	(output / "Data_extracted" / "navmesh" / "mapinfo.mfo").write_bytes(mapinfo())

	source.mkdir(parents=True)
	data_rows, building_rows, link_rows = gate_tables()

	def object_row(key, english):
		return ["1", "11880", key, "Korean", "0", "0", "0", "0", "0", english, "0", "0", "0", english, english, english, english]

	media_files = {
		"characterdata.txt": utf16([["characterdata_5000.txt"]]),
		"characterdata_5000.txt": utf16([
			character(50001, "MOB_AR_TEST", level=125),
			character(50002, "MOB_ARABIA_UNIQUE", level=130),
			character(50003, "NPC_AR_TEST", level=1, model="npc\\arabia\\test_1.bsr,npc\\arabia\\test_2.bsr"),
			character(50004, "NPC_AR_UNNAMED", level=1),
			character(50005, "NPC_NATIVE_NAMED", level=1),
			character(50006, "NPC_CH_NATIVE_SHOP", level=1),
		]),
		"skilldata.txt": utf16([["skilldata_5000.txt"]]),
		"skilldata_5000.txt": utf16([skill(70, "SKILL_LATER")]),
		"npcpos.txt": utf16([["50003", "21325", "10", "1", "20"], ["50001", "19792", "1", "1", "1"],
			["50004", "22106", "11", "1", "21"], ["50005", "21325", "12", "1", "22"],
			["50006", "21325", "13", "1", "23"]]),
		"npcchat.txt": utf16([["1", "NPC_AR_TEST", "SN_NPC_AR_TEST_CHAT", "SN_NPC_AR_TEST_PS"],
			["1", "NPC_NATIVE_NAMED", "SN_NATIVE_CHAT", "0"]]),
		"regioncode.txt": utf16([["1", str(region), f"RN_ARABIA_{family}", ""] for region, family in REGIONS]),
		# v1.696 shape: an index of shards, rows with a text id after the service cell.
		"textzonename.txt": utf16([["TextZoneName_3000.txt"]]),
		"textzonename_3000.txt": utf16([
			object_row("RN_ARABIA_TOWN_01", "Baghdad"),
			["1", "3025", str(TOWN_REGION), "Korean", "0", "0", "0", "0", "0", "Baghdad Town",
				"0", "0", "0", "Baghdad Town", "Baghdad Town", "Baghdad Town", "Baghdad Town"],
		]),
		"textdata_object.txt": utf16([["TextData_Object_1000.txt"]]),
		"textdata_object_1000.txt": utf16([
			object_row("SN_MOB_AR_TEST", "Test Beast"),
			object_row("SN_MOB_ARABIA_UNIQUE", "Test Unique"),
			object_row("SN_NPC_AR_TEST", "Test Arab"),
			object_row("SN_NPC_AR_DSTARTPORTAL", "Baghdad Dungeon"),
		]),
		"textquest_speech&name.txt": utf16([["TextQuest_Speech&Name_100.txt"]]),
		"textquest_speech&name_100.txt": utf16([
			["1", "500", "SN_NPC_AR_TEST_CHAT", "Korean", "0", "0", "0", "0", "0", "Greetings",
				"0", "0", "0", "Greetings", "Greetings", "Greetings", "Greetings"],
		]),
		"skilleffect.txt": b"\xff\xfe" + (
			"#section\tcharacterinfo\r\n" + "\t".join(["MOB_AR_TEST"] + ["0"] * 12) + "\r\n"
			"#section\tskillaniset2\r\n1\tname\tSKILL_LATER\tANI_TEST\r\n"
		).encode("utf-16-le"),
		"teleportdata.txt": utf16(data_rows),
		"teleportbuilding.txt": utf16(building_rows),
		"teleportlink.txt": utf16(link_rows),
	}
	(source / "Media.pk2").write_bytes(media_archive(media_files))

	map_files = {"tile2d.ifo": 'sig\n1\n00000 0 "nature" "tile00.ddj"\n'.encode("cp949"),
		"object.ifo": 'sig\n1\n00777 0 "res\\arabia\\obj777.bsr"\n'.encode("cp949"),
		"tile2d/tile00.ddj": b"texture"}
	for region, _family in REGIONS:
		x, y = region & 0xFF, (region >> 8) & 0xFF
		map_files[f"{y}/{x}.m"] = terrain_m()
		map_files[f"{y}/{x}.t"] = b"t"
		map_files[f"{y}/{x}.o"] = b"o"
		# The town places 777, which the native catalogue defines
		# differently: the import moves it to 901 in both files.
		map_files[f"{y}/{x}.o2"] = placement_o2(TOWN_OBJECT if region == TOWN_REGION else None)
	# One sector stands unbuilt behind its region code, the boss
	# lair's shape in miniature: no terrain files, no navmesh.
	garden_x, garden_y = 31734 & 0xFF, (31734 >> 8) & 0xFF
	for extension in (".m", ".t", ".o", ".o2"):
		del map_files[f"{garden_y}/{garden_x}{extension}"]
	(source / "Map.pk2").write_bytes(map_archive(map_files))
	data_files = {f"navmesh/nv_{region:04x}.nvm": region_navmesh(*([TOWN_OBJECT] if region == TOWN_REGION else []))
		for region, _family in REGIONS}
	del data_files[f"navmesh/nv_{31734:04x}.nvm"]
	(source / "Data.pk2").write_bytes(data_archive(data_files))
	(source / "Particles.pk2").write_bytes(data_archive({}))
	nests = base / "nests.json"
	nests.write_text(json.dumps(extraction()))
	return native, output, source, nests


class ArabiaImportTest(unittest.TestCase):
	# The fixture's archives are synthetic; the v1.696 pin file
	# (v1696_archive_pins.json, #3250) must not reach them, or the
	# importer's archive verification refuses the fixture before
	# anything is read (it lands beside the importer in CI's merge
	# tree). Point the pin at an absent file - the same "nothing to
	# verify against" posture a pre-pin tree has.
	def setUp(self):
		stub = mock.patch.object(skytemple_import, "PIN_FILE",
			Path(tempfile.gettempdir()) / "opensro-no-v1696-pins.json")
		stub.start()
		self.addCleanup(stub.stop)

	# ================
	# test_on_then_off
	# ================
	def test_on_then_off(self):
		with tempfile.TemporaryDirectory() as tmp:
			native, output, source, nests = build_trees(Path(tmp))
			text = output / arabia.TEXT_ROOT
			closure = FakeClosure()
			with mock.patch.dict(os.environ, {arabia.FLAG: "1"}):
				summary = arabia.add_arabia(native, output, source, nests, closure)
			self.assertTrue(summary["enabled"])
			self.assertEqual((summary["kinds"], summary["npcs"], summary["nests"]), (2, 2, 4))
			# A comma-joined model cell seeds each variant on its own.
			self.assertIn("res/npc/arabia/test_1.bsr", closure.seeds)
			self.assertIn("res/npc/arabia/test_2.bsr", closure.seeds)
			self.assertNotIn("res/npc/arabia/test_1.bsr,npc/arabia/test_2.bsr", closure.seeds)
			self.assertEqual(summary["hiveCapped"], [13, 14])
			self.assertEqual(summary["sectors"],
				sorted([region & 0xFF, (region >> 8) & 0xFF] for region, _f in REGIONS if region != 31734))

			shard = read_rows(text / arabia.CHARACTER_SHARD)
			self.assertEqual([row[2] for row in shard],
				["MOB_ARABIA_UNIQUE", "MOB_AR_TEST", "NPC_AR_TEST", "NPC_NATIVE_NAMED"])
			self.assertTrue(all(len(row) == 120 for row in shard))
			population = json.loads((text / arabia.POPULATION_FILE).read_text())
			rows = {row["NestID"]: row for row in population["Rows"]}
			self.assertEqual(rows[12]["WorldID"], 112)
			self.assertEqual(rows[12]["SpawnType"], 1)
			self.assertEqual((rows[13]["HiveKey"], rows[13]["HiveMaxCount"]), ("MOB_ARABIA_UNIQUE", 1))
			standings = read_rows(text / "npcpos.txt")
			self.assertEqual(len(standings), 6)
			# A placed kind the native tables own gets no second row
			# under its codename and stands under its native id.
			self.assertNotIn("NPC_CH_NATIVE_SHOP", [row[2] for row in shard])
			self.assertIn(["101", "21325", "13", "1", "23"], [row[:5] for row in standings])
			# The nameless event prop the client's npcpos plants in
			# the town (NPC_AR_UNNAMED has an SN_ key no text table
			# holds) is skipped and logged, never shipped nameless.
			self.assertNotIn("NPC_AR_UNNAMED", [row[2] for row in shard])
			self.assertFalse(any(row[1] == "22106" and row[2] == "11" for row in standings))
			self.assertTrue(any(row[1] == "21325" for row in standings))
			self.assertTrue(any(row[1] == "NPC_AR_TEST" for row in read_rows(text / "npcchat.txt")))
			# The kind the native client already talks for keeps
			# exactly its native row; nothing is duplicated.
			self.assertEqual([row[1] for row in read_rows(text / "npcchat.txt")].count("NPC_NATIVE_NAMED"), 1)
			gates = {row[2]: row for row in read_rows(text / "teleportdata.txt")}
			# Jangan's gate stays one row and lists Baghdad; Baghdad's
			# Portals lead back to it.
			self.assertEqual([row[2] for row in read_rows(text / "teleportdata.txt")].count("GATE_CH"), 1)
			baghdad = gates["GATE_TO_LAST_FIELD_IN_BAGHDAD"][1]
			pairs = {(row[1], row[2]) for row in read_rows(text / "teleportlink.txt") if len(row) > 2}
			self.assertIn(("1", baghdad), pairs)
			self.assertIn((gates["GATE_AR_S"][1], "1"), pairs)
			self.assertFalse({row[41] for row in read_rows(text / "teleportbuilding.txt")
				if len(row) > 41 and row[2] != "STORE_CH_GATE"} & {"25000"})
			self.assertEqual(gates["GATE_ARABIA_DUNGEON_START"][12], "112")
			regions = read_rows(text / "regioncode.txt")
			self.assertEqual(sum(1 for row in regions if row[2].startswith("RN_ARABIA")), 7)
			# The minimap title reads the zone row of the region id; the
			# banner the code's _01 row (both from the sharded v1.696 table).
			zones = {row[1]: row[8] for row in read_rows(text / "textzonename.txt") if len(row) > 8}
			self.assertEqual(zones[str(TOWN_REGION)], "Baghdad Town")
			self.assertEqual(zones["RN_ARABIA_TOWN_01"], "Baghdad")
			# The unbuilt garden sector imports no terrain; its
			# region code row above still imports.
			self.assertFalse((output / "Map_extracted" / "123" / "246.m").exists())
			# The town's navmesh names the remapped id its placements
			# do: the client resolves both through object.ifo, and an
			# unpatched navmesh failed Baghdad ("Missing navigation BSR").
			self.assertEqual(summary["objectRemap"], {str(TOWN_OBJECT): MOVED_OBJECT})
			town_x, town_y = TOWN_REGION & 0xFF, (TOWN_REGION >> 8) & 0xFF
			self.assertEqual(skytemple_import.o2_object_ids(
				(output / "Map_extracted" / str(town_y) / f"{town_x}.o2").read_bytes()), {MOVED_OBJECT})
			self.assertEqual(skytemple_import.nvm_object_ids(
				(output / "Data_extracted" / "navmesh" / f"nv_{TOWN_REGION:04x}.nvm").read_bytes()), {MOVED_OBJECT})
			names = {row[1] for row in read_rows(text / "textdataname.txt") if len(row) > 1}
			self.assertIn("SN_MOB_AR_TEST", names)
			# A kind whose name the native table already owns rides
			# on that row; nothing is written for it.
			self.assertIn("SN_NPC_NATIVE_NAMED", names)
			self.assertIn("SN_NPC_AR_DSTARTPORTAL", names)
			# The talk string lands in textquest.txt, the native talk
			# table, with its English at the native column 8.
			quest = {row[1]: row for row in read_rows(text / "textquest.txt") if len(row) > 8}
			self.assertEqual(quest["SN_NPC_AR_TEST_CHAT"][8], "Greetings")
			# The dangling _PS twin is recorded, never demanded.
			self.assertEqual(summary["speechUnresolved"], ["SN_NPC_AR_TEST_PS"])
			self.assertNotIn("SN_NPC_AR_TEST_PS", quest)

			# A rebuild must keep owning the gate texts the first
			# run wrote (ownership is measured against the native
			# tables, not the current ones).
			with mock.patch.dict(os.environ, {arabia.FLAG: "1"}):
				rebuilt = arabia.add_arabia(native, output, source, nests, closure)
			self.assertIn("SN_NPC_AR_DSTARTPORTAL", rebuilt["gateTextSymbols"])
			# ...and the rebuild re-derives the same gate identities
			# instead of renumbering above its own rows and
			# orphaning the first set.
			rebuilt_gates = [row for row in read_rows(text / "teleportdata.txt")
				if len(row) > 2 and row[2] in rebuilt["gateCodes"]]
			self.assertEqual(len(rebuilt_gates), 8)
			self.assertEqual({row[2]: row[1] for row in rebuilt_gates},
				{row[2]: row[1] for row in gates.values() if row[2] in rebuilt["gateCodes"]})

			with mock.patch.dict(os.environ, {arabia.FLAG: "off"}):
				off = arabia.add_arabia(native, output, source, nests, closure)
			self.assertEqual(off, {"enabled": False})
			for name in (arabia.CHARACTER_SHARD, arabia.SKILL_SHARD, arabia.POPULATION_FILE):
				self.assertFalse((text / name).exists(), name)
			self.assertFalse((output / arabia.MARKER).exists())
			self.assertEqual([row[0] for row in read_rows(text / "skilldata.txt")], ["skilldata_5000.txt"])
			# Off removes the Arabia standing of the native kind by its
			# exact row and keeps the native standing under the same id.
			self.assertEqual([row[:5] for row in read_rows(text / "npcpos.txt")],
				[["100", "25000", "1", "1", "1"], ["101", "25000", "2", "1", "2"]])
			self.assertFalse(any(row[1] == "NPC_AR_TEST" for row in read_rows(text / "npcchat.txt")))
			# The native talk row rides through the whole cycle.
			self.assertTrue(any(row[1] == "NPC_NATIVE_NAMED" for row in read_rows(text / "npcchat.txt")))
			self.assertFalse(any(len(row) > 1 and row[1] == "SN_NPC_AR_TEST_CHAT"
				for row in read_rows(text / "textquest.txt")))
			gates = {row[2] for row in read_rows(text / "teleportdata.txt")}
			self.assertNotIn("GATE_TO_LAST_FIELD_IN_BAGHDAD", gates)
			self.assertEqual({(row[1], row[2]) for row in read_rows(text / "teleportlink.txt") if len(row) > 2},
				{("1", "1")})
			self.assertNotIn("SN_NPC_AR_DSTARTPORTAL",
				{row[1] for row in read_rows(text / "textdataname.txt") if len(row) > 1})
			# ...but the native-owned name row is never claimed, so
			# the strip leaves it standing.
			self.assertIn("SN_NPC_NATIVE_NAMED",
				{row[1] for row in read_rows(text / "textdataname.txt") if len(row) > 1})
			regions = read_rows(text / "regioncode.txt")
			self.assertFalse(any(row[2].startswith("RN_ARABIA") for row in regions))

	# ================
	# test_later_speech_texts_keeps_untranslated_rows
	# ================
	def test_later_speech_texts_keeps_untranslated_rows(self):
		files = {
			"textquest_speech&name.txt": utf16([["TextQuest_Speech&Name_100.txt"]]),
			"textquest_speech&name_100.txt": utf16([
				["1", "500", "SN_NPC_SHORT", "Korean"],
				["1", "501", "SN_NPC_FULL", "Korean", "0", "0", "0", "0", "0", "Hello",
					"0", "0", "0", "Hello", "Hello", "Hello", "Hello"],
			]),
		}
		rows = {row[1]: row for row in arabia.later_speech_texts(lambda name: files[name])}
		self.assertEqual(rows["SN_NPC_SHORT"][8], "")
		self.assertEqual(rows["SN_NPC_FULL"][8], "Hello")

	# ================
	# test_korean_only_talk_takes_the_authored_english
	#
	# 2026-11-15: three Arabia talk rows had no English in any v1.696
	# column, and the asset build refused the variant. The layer's own
	# authored line fills them; a changed Korean refuses as stale.
	# ================
	def test_korean_only_talk_takes_the_authored_english(self):
		key = "SN_LAST_FIELD_PRIEST_BS"
		korean, english = arabia.SPEECH_ENGLISH[key]
		files = {
			"textquest_speech&name.txt": utf16([["TextQuest_Speech&Name_100.txt"]]),
			"textquest_speech&name_100.txt": utf16([["1", "502", key, korean]]),
		}
		rows = {row[1]: row for row in arabia.later_speech_texts(lambda name: files[name])}
		self.assertEqual(rows[key][8], english)
		files["textquest_speech&name_100.txt"] = utf16([["1", "502", key, "changed Korean"]])
		with self.assertRaisesRegex(ValueError, "stale Arabia talk English"):
			arabia.later_speech_texts(lambda name: files[name])

	# ================
	# test_authored_english_never_replaces_the_sources_own
	# ================
	def test_authored_english_never_replaces_the_sources_own(self):
		self.assertEqual(arabia.speech_english("SN_LAST_FIELD_PRIEST_BS", "other", "Shipped"), "Shipped")
		self.assertEqual(arabia.speech_english("SN_NPC_UNLISTED", "Korean", ""), "")

	# ================
	# test_resource_closure_gap_posture
	# ================
	def test_resource_closure_gap_posture(self):
		with tempfile.TemporaryDirectory() as tmp:
			native, output = Path(tmp) / "native", Path(tmp) / "output"
			(output / "Data_extracted").mkdir(parents=True)
			archives = {"Data": (None, {"res/test.bsr": object()}), "Particles": (None, {})}
			seeds = {"res/test.bsr", "res/absent.bsr"}
			payload = mock.patch.object(sro_pk2, "payload", return_value=b"bsr-bytes")
			audit = mock.patch.object(cap110_item_import, "resource_dependencies",
				side_effect=ValueError("audit cannot read it"))
			with payload, audit:
				# The strict default (Sky Temple's) still stops.
				with self.assertRaisesRegex(ValueError, "missing Data resource"):
					skytemple_import.copy_resource_closure(seeds, archives, native, output)
				result = skytemple_import.copy_resource_closure(
					seeds, archives, native, output, tolerate_gaps=True)
			self.assertEqual(result["missingFiles"], ["Data:res/absent.bsr"])
			self.assertEqual(result["unparsedFiles"], ["res/test.bsr"])
			self.assertEqual(result["files"], ["Data_extracted/res/test.bsr"])
			self.assertTrue((output / "Data_extracted" / "res" / "test.bsr").is_file())

	# ================
	# test_off_without_marker_is_a_noop
	# ================
	def test_off_without_marker_is_a_noop(self):
		with tempfile.TemporaryDirectory() as tmp:
			native, output, source, nests = build_trees(Path(tmp))
			with mock.patch.dict(os.environ, {arabia.FLAG: "off"}):
				self.assertEqual(arabia.add_arabia(native, output, source, nests, FakeClosure()), {"enabled": False})
			self.assertFalse((output / arabia.POPULATION_FILE).exists())

	# ================
	# test_on_without_nests_refuses_before_any_archive
	#
	# A contributor's clone has no nest extraction. The import names
	# SRO_ARABIA_NESTS and the switch before it opens an archive (the
	# source root here does not exist), never a FileNotFoundError on a
	# ../research path deep in the build.
	# ================
	def test_on_without_nests_refuses_before_any_archive(self):
		with tempfile.TemporaryDirectory() as tmp:
			native, output, _source, _nests = build_trees(Path(tmp))
			env = {key: value for key, value in os.environ.items() if key != arabia.NESTS_ENV}
			env[arabia.FLAG] = "on"
			with mock.patch.dict(os.environ, env, clear=True), 					mock.patch.object(arabia, "verify_archives") as archives:
				with self.assertRaises(SystemExit) as refusal:
					arabia.add_arabia(native, output, Path(tmp) / "absent", None, FakeClosure())
			archives.assert_not_called()
			message = str(refusal.exception)
			self.assertIn("SRO_ARABIA is on, but SRO_ARABIA_NESTS is not set", message)
			self.assertIn("turn SRO_ARABIA off", message)
			self.assertFalse((output / arabia.POPULATION_FILE).exists())


if __name__ == "__main__":
	unittest.main()
