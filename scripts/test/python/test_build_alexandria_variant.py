"""
===========================================================================

test_build_alexandria_variant.py - the v1.150 + Alexandria merge

Synthetic catalogs, terrain and availability bitmaps: native rows win, a
VSRO row fills a placeholder, a disagreeing used tile id moves to a new id
and the terrain follows it, and a region's availability bit is set without
disturbing the others. A scratch tree proves the link step never writes
through to the native file.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import struct
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import build_alexandria_variant as variant  # noqa: E402
import import_vsro_alexandria as stage1  # noqa: E402
from test_import_vsro_alexandria import terrain  # noqa: E402

NATIVE_TILES = (
	b'JMXV2DTI1001\r\n4\r\n'
	b'00000 0x00000000 "A" "a.ddj"\r\n'
	b'00001 0x00000000 "X" "xxx.ddj"\r\n'
	b'00002 0x00000000 "B" "b.ddj"\r\n'
	b'00003 0x00000000 "D" "d.ddj"\r\n'
)
VSRO_TILES = (
	b'JMXV2DTI1001\r\n6\r\n'
	b'00000 0x00000000 "A" "a.ddj"\r\n'
	b'00001 0x00000000 "Pharaoh" "pha.ddj"\r\n'
	b'00002 0x00000000 "B" "other.ddj"\r\n'
	b'00003 0x00000000 "D" "dd.ddj"\r\n'
	b'00004 0x0000000a "E" "e.ddj"\r\n'
	b'00005 0x00000000 "F" "f.ddj"\r\n'
)


class CatalogMergeTests(unittest.TestCase):
	def test_native_wins_and_vsro_fills_only_new_ids(self):
		merged, remap = variant.merge_tile_catalog(NATIVE_TILES, VSRO_TILES, {4})
		tiles = stage1.tile_catalog(merged)
		self.assertEqual(remap, {})
		self.assertEqual(tiles[1], ("X", "xxx.ddj"))
		self.assertEqual(tiles[2], ("B", "b.ddj"))
		self.assertEqual(tiles[3], ("D", "d.ddj"))
		self.assertEqual(tiles[4], ("E", "e.ddj"))
		self.assertIn(5, tiles)

	def test_a_used_placeholder_moves_to_a_new_id_too(self):
		merged, remap = variant.merge_tile_catalog(NATIVE_TILES, VSRO_TILES, {1})
		self.assertEqual(remap, {1: 6})
		tiles = stage1.tile_catalog(merged)
		self.assertEqual(tiles[1], ("X", "xxx.ddj"))
		self.assertEqual(tiles[6], ("Pharaoh", "pha.ddj"))

	def test_a_used_disagreement_gets_a_new_id_and_an_unused_one_stays_native(self):
		merged, remap = variant.merge_tile_catalog(NATIVE_TILES, VSRO_TILES, {2})
		self.assertEqual(remap, {2: 6})
		tiles = stage1.tile_catalog(merged)
		self.assertEqual(tiles[6], ("B", "other.ddj"))
		self.assertEqual(tiles[3], ("D", "d.ddj"))
		self.assertTrue(merged.decode("cp949").splitlines()[1] == str(len(tiles)))

	def test_object_catalog_keeps_the_native_flag_on_a_conflict(self):
		native = b'JMXVOBJI1000\r\n2\r\n00001 0x00000001 "a.bsr"\r\n00002 0x00000000 "xxx"\r\n'
		vsro = b'JMXVOBJI1000\r\n3\r\n00001 0x00000000 "a.bsr"\r\n00002 0x00000000 "b.bsr"\r\n00003 0x00000000 "c.bsr"\r\n'
		rows = stage1.object_catalog(variant.merge_object_catalog(native, vsro))
		self.assertEqual(rows, {1: (1, "a.bsr"), 2: (0, "b.bsr"), 3: (0, "c.bsr")})


class TerrainTests(unittest.TestCase):
	def test_remap_changes_the_id_and_keeps_the_scale_bits(self):
		mapped = variant.remap_terrain(terrain([5, 262]), {262: 605})
		self.assertEqual(stage1.terrain_tile_ids(mapped), {5, 605})
		at = stage1.MAPM_SIGNATURE_BYTES + stage1.MAPM_BLOCK_HEAD_BYTES + stage1.MAPM_VERTEX_TEXTURE_OFFSET
		self.assertEqual(struct.unpack_from("<H", mapped, at)[0] & variant.MAPM_TEXTURE_SCALE_MASK, 0x8400)

	def test_an_empty_remap_returns_the_terrain_unchanged(self):
		source = terrain([5])
		self.assertEqual(variant.remap_terrain(source, {}), source)


class AvailabilityTests(unittest.TestCase):
	def test_enable_regions_sets_only_the_named_bits(self):
		mapinfo = variant.MFO_SIGNATURE + bytes(variant.MFO_HEADER_BYTES - len(variant.MFO_SIGNATURE)) + bytes(0x1000)
		enabled = variant.enable_regions(mapinfo, [(48, 90), (46, 89)])
		bits = [
			(index * 8 + bit)
			for index, byte in enumerate(enabled[variant.MFO_HEADER_BYTES:])
			for bit in range(8)
			if byte & (0x80 >> bit)
		]
		self.assertEqual(bits, [(89 << 8) | 46, (90 << 8) | 48])
		self.assertEqual(len(enabled), len(mapinfo))

	def test_a_non_mfo_file_is_refused(self):
		with self.assertRaises(ValueError):
			variant.enable_regions(b"nope" * 10, [(1, 1)])


def table(rows):
	return b"\xff\xfe" + variant.table_text(rows).encode("utf-16-le")


def link(source, target, fee, level_min="0", scheduling="0"):
	row = ["1", source, target, fee, scheduling, "0", "1", level_min, "999", "2", "0", "0"]
	return row + ["0"] * (variant.LINK_COLUMNS - len(row))


def gate(teleport_id, code, building, zone):
	return ["1", teleport_id, code, building, zone, "23088", "663", "863", "526", "50", "1", "0", "1"]


def building(object_id, code, name):
	return ["1", object_id, code, "k", "xxx", name] + ["0"] * (variant.BUILDING_COLUMNS - 6)


def text(key, korean, trad, simp, jp, en):
	return ["1", key, korean, "", trad, simp, "de", jp, en, "vi", "pt", "ru", "tr", "es", "ar"]


# The synthetic tables carry the two dimensional gates only.
SYNTHETIC_GATES = ("GATE_SD1", "GATE_SD2")


class GateTests(unittest.TestCase):
	def tables(self):
		native = {
			"teleportdata.txt": table([gate("1", "GATE_CH", "2094", "SN_Z1"), gate("11", "GATE_KT", "2096", "SN_Z2")]),
			"teleportbuilding.txt": table([building("2094", "STORE_CH_GATE", "SN_B1")]),
			"teleportlink.txt": table([link("1", "11", "5000", "0", "1")]),
			"text": table([["1", "SN_Z1", "k", "", "", "s", "t", "j", "Jangan", "v", ""]])
		}
		vsro = {
			"teleportdata.txt": table([
				gate("1", "GATE_CH", "2094", "SN_Z1"), gate("5", "GATE_KT", "2096", "SN_Z2"),
				gate("175", "GATE_SD1", "27753", "SN_ZONE_15"), gate("176", "GATE_SD2", "27754", "SN_ZONE_16"),
				gate("177", "GATE_NPC_FERRY", "26799", "SN_ZONE_13")
			]),
			"teleportbuilding.txt": table([building("27753", "STORE_SD_GATE1", "SN_GATE_S"), building("27754", "STORE_SD_GATE2", "SN_GATE_N")]),
			"teleportlink.txt": table([
				link("1", "175", "40000", "95"), link("175", "1", "40000"), link("5", "176", "30000", "95"),
				link("175", "176", "0"), link("177", "1", "5000"), link("1", "11", "5000")
			]),
			"text": table([
				text("SN_ZONE_15", "kor15", "trad15", "simp15", "jp15", "South"), text("SN_ZONE_16", "kor16", "t16", "s16", "j16", "North"),
				text("SN_GATE_S", "k", "t", "s", "j", "Gate S"), text("SN_Z1", "other", "t", "s", "j", "Other")
			])
		}
		return native, vsro

	def test_gates_links_buildings_and_texts_are_added_to_the_native_tables(self):
		native, vsro = self.tables()
		merged, summary = variant.merge_gate_tables(native, vsro, codes=SYNTHETIC_GATES)
		self.assertEqual(summary, {
			"gates": ["GATE_SD1", "GATE_SD2"], "links": 4, "buildings": 2, "texts": 3,
			"buildingCodes": ["STORE_SD_GATE1", "STORE_SD_GATE2"]
		})
		codes = [row[2] for row in stage1.table_rows(merged["teleportdata.txt"])]
		self.assertEqual(codes, ["GATE_CH", "GATE_KT", "GATE_SD1", "GATE_SD2"])
		self.assertTrue(merged["teleportdata.txt"].startswith(native["teleportdata.txt"][:20]))
		links = [row[1:4] for row in stage1.table_rows(merged["teleportlink.txt"])]
		# VSRO ids 1 and 5 become native ids 1 and 11; a link to a gate the native table lacks is dropped.
		self.assertEqual(links, [["1", "11", "5000"], ["1", "175", "40000"], ["175", "1", "40000"], ["11", "176", "30000"], ["175", "176", "0"]])

	def test_links_take_the_scheduling_word_the_native_server_accepts_and_clamp_the_level(self):
		native, vsro = self.tables()
		merged, _ = variant.merge_gate_tables(native, vsro, codes=SYNTHETIC_GATES, level_cap=90)
		added = [row for row in stage1.table_rows(merged["teleportlink.txt"])][1:]
		self.assertTrue(all(row[4] == "1" and row[5] == "0" and len(row) == variant.LINK_COLUMNS for row in added))
		entry = next(row for row in added if row[1:3] == ["1", "175"])
		self.assertEqual(entry[6:9], ["1", "90", "999"])
		leave = next(row for row in added if row[1:3] == ["175", "1"])
		self.assertEqual(leave[6:9], ["1", "0", "999"])

	def test_a_condition_kind_the_server_does_not_evaluate_is_dropped_and_the_others_stay(self):
		row = ["1", "3", "4", "5000", "0", "0", "1", "95", "999", "2", "0", "0", "4", "9", "9"] + ["0"] * 6
		columns = variant.translate_link(row, {"3": "3", "4": "4"}, 90)
		self.assertEqual(columns[6:15], ["1", "90", "999", "2", "0", "0", "0", "0", "0"])

	def test_a_dropped_condition_is_reported_with_its_link_and_operands(self):
		row = ["1", "3", "4", "5000", "0", "0", "1", "95", "999", "2", "0", "0", "4", "5", "6"] + ["0"] * 6
		dropped = []
		variant.translate_link(row, {"3": "3", "4": "4"}, 90, dropped)
		self.assertEqual(dropped, [{"from": "3", "to": "4", "kind": 4, "operands": ["5", "6"]}])

	def test_the_thief_suit_condition_kind_survives_unreported(self):
		row = ["1", "3", "4", "5000", "0", "0", "1", "95", "999", "2", "0", "0", "7", "0", "0"] + ["0"] * 6
		dropped = []
		columns = variant.translate_link(row, {"3": "3", "4": "4"}, 90, dropped)
		self.assertEqual(columns[6:15], ["1", "90", "999", "2", "0", "0", "7", "0", "0"])
		self.assertEqual(dropped, [])

	def test_the_gate_summary_reports_every_dropped_condition(self):
		native, vsro = self.tables()
		conditional = link("175", "176", "0")
		conditional[12:15] = ["3", "42", "7"]
		vsro["teleportlink.txt"] = table([conditional])
		merged, summary = variant.merge_gate_tables(native, vsro, codes=SYNTHETIC_GATES)
		self.assertEqual(summary["droppedConditions"],
			[{"from": "175", "to": "176", "kind": 3, "operands": ["42", "7"]}])
		added = [row for row in stage1.table_rows(merged["teleportlink.txt"])][1:]
		self.assertEqual(added[0][12:15], ["0", "0", "0"])

	def test_the_gate_summary_keeps_a_kind_7_condition(self):
		native, vsro = self.tables()
		conditional = link("175", "176", "0")
		conditional[12:15] = ["7", "0", "0"]
		vsro["teleportlink.txt"] = table([conditional])
		merged, summary = variant.merge_gate_tables(native, vsro, codes=SYNTHETIC_GATES)
		self.assertNotIn("droppedConditions", summary)
		added = [row for row in stage1.table_rows(merged["teleportlink.txt"])][1:]
		self.assertEqual(added[0][12:15], ["7", "0", "0"])

	def test_texts_use_the_native_column_layout_and_never_replace_a_native_key(self):
		native, vsro = self.tables()
		merged, _ = variant.merge_gate_tables(native, vsro, codes=SYNTHETIC_GATES)
		rows = {row[1]: row for row in stage1.table_rows(merged["text"])}
		self.assertEqual(rows["SN_ZONE_15"], ["1", "SN_ZONE_15", "kor15", "", "", "simp15", "trad15", "jp15", "South", "vi", ""])
		self.assertEqual(rows["SN_Z1"][8], "Jangan")

	def test_a_gate_id_the_native_table_already_uses_is_refused(self):
		native, vsro = self.tables()
		native["teleportdata.txt"] = table([gate("175", "GATE_CH", "2094", "SN_Z1")])
		with self.assertRaises(SystemExit):
			variant.merge_gate_tables(native, vsro, codes=SYNTHETIC_GATES)

	# ================
	# test_outdoor_crossings_keep_both_directions_and_authored_conditions
	# ================
	def test_outdoor_crossings_keep_both_directions_and_authored_conditions(self):
		native, vsro = self.tables()
		codes = ("GATE_DELTA_GATE_E", "GATE_DELTA_GATE_W", "GATE_STORMDESERT_GATE", "GATE_KINGSVALLEY_GATE")
		gates = [gate(str(178 + i), code, str(27755 + i), "SN_OUTDOOR_" + str(i)) for i, code in enumerate(codes)]
		buildings = [building(str(27755 + i), code.replace("GATE_", "STORE_", 1), "SN_BUILDING_" + str(i)) for i, code in enumerate(codes)]
		links = [link("178", "180", "0", "95"), link("180", "178", "0"), link("179", "181", "0", "95"), link("181", "179", "0")]
		vsro["teleportdata.txt"] = table(gates)
		vsro["teleportbuilding.txt"] = table(buildings)
		vsro["teleportlink.txt"] = table(links)
		vsro["text"] = table([text(r[4], "k", "t", "s", "j", "Outdoor") for r in gates])
		merged, summary = variant.merge_gate_tables(native, vsro, codes=codes, level_cap=110)
		self.assertEqual(summary["buildings"], 4)
		self.assertEqual(summary["links"], 4)
		actual = [r for r in stage1.table_rows(merged["teleportlink.txt"]) if r[1] in {"178", "179", "180", "181"}]
		self.assertEqual([r[1:4] for r in actual], [r[1:4] for r in links])
		self.assertEqual([r[6:9] for r in actual], [r[6:9] for r in links])
		self.assertEqual(list(stage1.table_rows(merged["teleportbuilding.txt"]))[-4:], buildings)
		self.assertTrue(set(codes).issubset(variant.GATE_CODES))

	# ================
	# test_tomb_gates_link_the_entrance_to_three_exits_and_drop_unplaced_rows
	# ================
	def test_tomb_gates_link_the_entrance_to_three_exits_and_drop_unplaced_rows(self):
		native, vsro = self.tables()
		codes = variant.TOMB_GATE_CODES
		self.assertEqual(codes[0], "GATE_TOMB_GATE_IN")
		self.assertEqual(len(codes), 4)
		self.assertFalse(set(codes) & set(variant.GATE_CODES), "tomb gates are opt-in")
		gates = [gate(str(164 + i), code, str(27744 + i), "SN_TOMB_" + str(i)) for i, code in enumerate(codes)]
		for at in (1, 2, 3):
			gates[at][variant.TELEPORT_WORLD_COLUMN] = str(56 + at)  # the source's worlds 57, 58, 59
		unplaced = gate("168", "GATE_TOMBOSIRIS_GATE_OUT_1", "27748", "SN_TOMB_OSIRIS")
		buildings = [building(str(27744 + i), code.replace("GATE_", "STORE_", 1), "SN_TOMB_" + str(i)) for i, code in enumerate(codes)]
		links = [link("164", "165", "0"), link("164", "166", "0"), link("164", "167", "0"),
			link("165", "164", "0"), link("166", "164", "0"), link("167", "164", "0")]
		vsro["teleportdata.txt"] = table(gates + [unplaced])
		vsro["teleportbuilding.txt"] = table(buildings)
		vsro["teleportlink.txt"] = table(links + [link("168", "164", "0")])
		vsro["text"] = table([text(r[4], "k", "t", "s", "j", "Tomb") for r in gates])
		merged, summary = variant.merge_gate_tables(native, vsro, codes=codes, level_cap=110)
		self.assertEqual(summary["buildings"], 4)
		self.assertEqual(summary["links"], 6, "the unplaced Osiris exit's link is not carried")
		added = [r for r in stage1.table_rows(merged["teleportdata.txt"]) if r[1] in {"164", "165", "166", "167", "168"}]
		self.assertEqual(sorted(r[1] for r in added), ["164", "165", "166", "167"])
		# The three exits keep their authored destination worlds 57, 58, 59: the
		# port defines one permanent world per tomb while SRO_PHARAOH_TOMB is on
		# (#1750). The entrance stays in the persistent world, as authored.
		worlds = {r[2]: r[variant.TELEPORT_WORLD_COLUMN] for r in added}
		self.assertEqual(worlds, {
			"GATE_TOMB_GATE_IN": "1", "GATE_TOMB_GATE_OUT_1": "57",
			"GATE_TOMB_GATE_OUT_2": "58", "GATE_TOMB_GATE_OUT_3": "59"})
		self.assertNotIn("worldsFlattened", summary)


def overview_row(left, bottom):
	"""A worldmap_mapinfo overview row (id 0, kind 0) with the given region box."""
	columns, tile_rows = (177 - left + 1) // 4, (113 - bottom + 1) // 4
	row = ["0", "0", "world", "map_worldmap.ddj", "128", "128", "128", "128", str(columns * 128), str(tile_rows * 128)]
	return row + [str(left), "113", "177", str(bottom), "0", "192", "192", "0", "xxx"]


def pin(pin_id, key, map_id="12"):
	row = [pin_id, "1", key, "Alexandria", "label", "map", "0", map_id, "-1", "-1", "10", "20"] + ["0"] * 8
	return row


class MapTests(unittest.TestCase):
	REGIONS = [(48, 90), (49, 90)]

	def tables(self):
		native = {
			"regioncode.txt": table([["1", "23964", "xxx", "xxx"], ["1", str((90 << 8) | 49), "xxx", "xxx"]]),
			"textzonename.txt": table([["1", "25255", "k", "", "", "s", "t", "j", "Jangan", "v", ""]]),
			"worldmap_mapinfo.txt": table([overview_row(66, 82), ["1", "1", "j"] + ["0"] * 16]),
			"worldmap_localinfo.txt": table([pin("11001", "SN_ZONE_11001", "1")]),
			"textdataname.txt": table([["1", "SN_ZONE_11001", "k", "", "", "s", "t", "j", "Shop", "v", ""]])
		}
		mapinfo = ["12", "1", "Alex", "interface\\worldmap\\map\\map_alex.ddj"] + ["0"] * 15 + ["SN_ZONE_21836_6", "1", "xxx", ""]
		vsro = {
			"regioncode.txt": table([
				["1", str((90 << 8) | 48), "RN_SD_EGYPT01", "x"], ["1", str((90 << 8) | 49), "RN_SD_EGYPT01", "x"],
				["1", str((91 << 8) | 48), "RN_SD_EGYPT01", "x"], ["1", str((90 << 8) | 50), "RN_OTHER", "x"]
			]),
			"textzonename.txt": table([
				text(str((90 << 8) | 48), "k", "t", "s", "j", "Alexandria"), text("RN_SD_EGYPT01_01", "k", "t", "s", "j", "Alexandria"),
				text("RN_SD_EGYPT01_02", "k", "t", "s", "j", "Harbor"), text("RN_UNRELATED", "k", "t", "s", "j", "No")
			]),
			"worldmap_mapinfo.txt": table([overview_row(46, 70), mapinfo, ["13", "1", "ship"] + ["0"] * 20]),
			"worldmap_localinfo.txt": table([
				["1"] + pin("26000", "SN_ZONE_23501") + ["0"], ["1"] + pin("91500", "interface\\xy_npc.ddj") + ["0"],
				["1"] + pin("11001", "SN_ZONE_11001") + ["0"], ["1"] + pin("92706", "interface\\xy_gate.ddj") + ["0"]
			]),
			"textdata_object.txt": table([text("SN_ZONE_23501", "k", "t", "s", "j", "Lighthouse"), text("SN_ZONE_11001", "k", "t", "s", "j", "Other")])
		}
		return native, vsro

	def test_region_codes_fill_native_placeholders_for_the_city_regions_only(self):
		native, vsro = self.tables()
		merged, summary = variant.merge_map_tables(native, vsro, self.REGIONS)
		codes = {row[1]: row[2] for row in stage1.table_rows(merged["regioncode.txt"])}
		self.assertEqual(codes[str((90 << 8) | 48)], "RN_SD_EGYPT01")
		self.assertEqual(codes[str((90 << 8) | 49)], "RN_SD_EGYPT01")
		self.assertEqual(codes["23964"], "xxx")
		self.assertNotIn(str((91 << 8) | 48), codes)
		self.assertEqual(summary["regionCodes"], 2)

	def test_zone_names_map_entry_pins_and_texts_are_added_in_the_native_layout(self):
		native, vsro = self.tables()
		merged, summary = variant.merge_map_tables(native, vsro, self.REGIONS)
		zones = {row[1]: row for row in stage1.table_rows(merged["textzonename.txt"])}
		self.assertEqual(sorted(zones), sorted(["25255", str((90 << 8) | 48), "RN_SD_EGYPT01_01", "RN_SD_EGYPT01_02"]))
		self.assertEqual(zones["RN_SD_EGYPT01_01"][8], "Alexandria")
		maps = [row for row in stage1.table_rows(merged["worldmap_mapinfo.txt"]) if row[0] == "12"]
		self.assertEqual([len(row) for row in maps], [variant.MAPINFO_COLUMNS])
		pins = [row[0] for row in stage1.table_rows(merged["worldmap_localinfo.txt"])]
		self.assertEqual(pins, ["11001", "26000", "92706"])
		keys = {row[1]: row for row in stage1.table_rows(merged["textdataname.txt"])}
		self.assertEqual(keys["SN_ZONE_23501"][8], "Lighthouse")
		self.assertEqual(keys["SN_ZONE_11001"][8], "Shop")
		self.assertEqual(
			summary,
			{"regionCodes": 2, "zoneNames": 3, "maps": 1, "pins": 2, "texts": 1, "overview": (46, 113, 177, 70)}
		)

	# ================
	# test_overview_icons_inside_the_widened_box_are_kept
	# ================
	def test_overview_icons_inside_the_widened_box_are_kept(self):
		def icon(icon_id, region, destination="0", size=("40", "28")):
			path = "interface\\worldmap\\map\\map_icon_tel01.ddj"
			return ["1", icon_id, "2", path, "0", "0", "0", destination, "0", str(region[0]), str(region[1]), "6", "9"] \
				+ list(size) + ["0"] * 7
		native, vsro = self.tables()
		vsro["worldmap_localinfo.txt"] = table([
			icon("99001", (52, 85)),
			icon("99005", (74, 82), "2017"),
			icon("91009", (162, 93), "1002"),
			icon("99020", (10, 85)),
			icon("99021", (52, 85), size=("0", "0"))
		])
		merged, summary = variant.merge_map_tables(native, vsro, self.REGIONS)
		kept = [row[0] for row in stage1.table_rows(merged["worldmap_localinfo.txt"])]
		self.assertEqual(kept, ["11001", "99001", "99005"])
		self.assertEqual(summary["pins"], 2)

	def test_texts_already_in_the_base_table_survive_the_map_step(self):
		native, vsro = self.tables()
		gate_texts = [["1", "SN_ZONE_21836_15", "k", "", "", "s", "t", "j", "Alexandria (South)", "v", ""]]
		native["textdataname.txt"] = variant.append_rows(native["textdataname.txt"], gate_texts)
		merged, _ = variant.merge_map_tables(native, vsro, self.REGIONS)
		keys = {row[1] for row in stage1.table_rows(merged["textdataname.txt"])}
		self.assertTrue({"SN_ZONE_21836_15", "SN_ZONE_23501"} <= keys)

	# ================
	# test_authored_city_npc_and_shop_pins_keep_their_source_coordinates
	# ================
	def test_authored_city_npc_and_shop_pins_keep_their_source_coordinates(self):
		native, vsro = self.tables()
		shop = pin("11050", "SN_ZONE_11050")
		npc = pin("91501", "interface\\worldmap\\map\\xy_npc.ddj")
		npc[1], npc[10:14] = "2", ["624", "589", "16", "16"]
		unplaced = pin("91537", "")
		unplaced[1], unplaced[10:14] = "2", ["", "", "", ""]
		zero_size = list(npc)
		zero_size[0], zero_size[13] = "91523", "0"
		other_page = list(npc)
		other_page[0], other_page[7] = "91599", "13"
		vsro["worldmap_localinfo.txt"] = variant.append_rows(vsro["worldmap_localinfo.txt"],
			[["1"] + row + ["0"] for row in [shop, npc, unplaced, zero_size, other_page]])
		vsro["textdata_object.txt"] = variant.append_rows(vsro["textdata_object.txt"],
			[text("SN_ZONE_11050", "k", "t", "s", "j", "Blacksmith")])
		merged, _ = variant.merge_map_tables(native, vsro, self.REGIONS)
		rows = {r[0]: r for r in stage1.table_rows(merged["worldmap_localinfo.txt"])}
		self.assertEqual(rows["91501"], npc)
		self.assertEqual(rows["11050"], shop)
		self.assertTrue({"91537", "91523", "91599"}.isdisjoint(rows))
		names = {r[1]: r[8] for r in stage1.table_rows(merged["textdataname.txt"]) if len(r) > 8}
		self.assertEqual(names["SN_ZONE_11050"], "Blacksmith")

	# ================
	# test_overview_labels_follow_the_imported_field_regions
	# ================
	def test_overview_labels_follow_the_imported_field_regions(self):
		native, vsro = self.tables()
		field = pin("26306", "SN_ZONE_CHAOS", "0")
		field[8:10] = ["48", "90"]
		other = list(field)
		other[0], other[8] = "26399", "99"
		untranslated = list(field)
		untranslated[0], untranslated[2] = "15200", "SN_ZONE_15200"
		vsro["worldmap_localinfo.txt"] = variant.append_rows(vsro["worldmap_localinfo.txt"],
			[["1"] + row + ["0"] for row in [field, other, untranslated]])
		vsro["textdata_object.txt"] = variant.append_rows(vsro["textdata_object.txt"],
			[text("SN_ZONE_CHAOS", "k", "t", "s", "j", "Chaos Maze")])
		merged, _ = variant.merge_map_tables(native, vsro, self.REGIONS)
		rows = {r[0]: r for r in stage1.table_rows(merged["worldmap_localinfo.txt"])}
		self.assertEqual(rows["26306"], field)
		self.assertNotIn("26399", rows)
		self.assertNotIn("15200", rows)
		names = {r[1]: r[8] for r in stage1.table_rows(merged["textdataname.txt"]) if len(r) > 8}
		self.assertEqual(names["SN_ZONE_CHAOS"], "Chaos Maze")

	def test_nothing_is_added_twice(self):
		native, vsro = self.tables()
		merged, _ = variant.merge_map_tables(native, vsro, self.REGIONS)
		again, summary = variant.merge_map_tables(merged, vsro, self.REGIONS)
		self.assertEqual(
			summary,
			{"regionCodes": 0, "zoneNames": 0, "maps": 0, "pins": 0, "texts": 0, "overview": (46, 113, 177, 70)}
		)
		self.assertEqual(again["worldmap_mapinfo.txt"], merged["worldmap_mapinfo.txt"])

	def test_the_overview_row_grows_west_and_south_and_keeps_its_other_columns(self):
		native, vsro = self.tables()
		merged, _ = variant.merge_map_tables(native, vsro, self.REGIONS)
		row = next(row for row in stage1.table_rows(merged["worldmap_mapinfo.txt"]) if row[:2] == ["0", "0"])
		self.assertEqual(row[10:14], ["46", "113", "177", "70"])
		self.assertEqual(row[3], "map_worldmap.ddj")
		# The draw size follows the box: 33 x 11 tiles of 128 pixels.
		self.assertEqual(row[8:10], ["4224", "1408"])
		self.assertEqual(row[14:], ["0", "192", "192", "0", "xxx"])

	def test_the_overview_tiles_are_the_ones_the_wider_box_adds(self):
		wide = variant.overview_tiles((46, 113, 177, 70))
		native = variant.overview_tiles((66, 113, 177, 82))
		self.assertEqual((len(native), len(wide)), (28 * 8, 33 * 11))
		added = variant.map_media_paths([], (46, 113, 177, 70), (66, 113, 177, 82))
		new_tiles = [path for path in added if "map_world_" in path]
		self.assertEqual(len(new_tiles), 33 * 11 - 28 * 8)
		self.assertIn("interface/worldmap/map/map_world_46x113.ddj", new_tiles)
		self.assertIn("interface/worldmap/map/map_world_174x73.ddj", new_tiles)
		self.assertNotIn("interface/worldmap/map/map_world_66x113.ddj", new_tiles)

	def test_media_paths_name_the_map_art_and_one_minimap_tile_per_region(self):
		paths = variant.map_media_paths(self.REGIONS)
		self.assertIn("interface/worldmap/map/map_alex.ddj", paths)
		self.assertIn("minimap/48x90.ddj", paths)
		self.assertIn("minimap/49x90.ddj", paths)

	# ================
	# test_overlapping_overview_tiles_are_replaced_only_for_imported_land
	# ================
	def test_overlapping_overview_tiles_are_replaced_only_for_imported_land(self):
		box, native_box = (46, 113, 177, 70), (66, 113, 177, 82)
		paths = variant.map_media_paths([(67, 84)], box, native_box)
		self.assertIn("interface/worldmap/map/map_world_66x85.ddj", paths)
		self.assertNotIn("interface/worldmap/map/map_world_66x113.ddj", paths)
		self.assertEqual(variant.overview_tiles(box, [(67, 84)]),
			["interface/worldmap/map/map_world_66x85.ddj"])


def cp949(rows):
	return ("\r\n".join(rows) + "\r\n").encode("cp949")


class MusicTests(unittest.TestCase):
	TOWN = "알렉"
	FIELD = "델타"

	def tables(self):
		native = {
			"regioninfo.txt": cp949(["#TOWN\t도적\t\t\t\t\t", "182\t96\tALL\t\t\t\t"]),
			"effectenvsnd.txt": cp949(["<1>\t도적\t\t\t\t\t", '\t"EasternEurope_Field.ogg"\t\t\t\t\t'])
		}
		vsro = {
			"regioninfo.txt": cp949([
				"#TOWN\t" + self.TOWN + "\t\t\t\t\t", "48\t95\tALL\t\t\t\t", "\t\t\t\t\t\t", "\t\t\t\t\t\t",
				"#FIELD\t" + self.FIELD + "\t\t\t\t\t", "50\t93\tALL\t\t\t\t", "#FIELD\tother\t\t\t\t\t", "1\t2\tALL\t\t\t\t"
			]),
			"effectenvsnd.txt": cp949([
				"<1>\t" + self.TOWN + "\t\t\t\t", '\t"Egypt_Town_Alexandria1.ogg"\t\t\t', "\t<2>\t낮\t\t\t",
				'\t\t\t<3>\t"night_wind.wav"\t0~0', "<1>\t" + self.FIELD + "\t\t\t\t", '\t"Egypt_Field_Delta.ogg"\t\t\t',
				"<1>\tother\t\t\t\t", '\t"Other.ogg"\t\t\t'
			])
		}
		return native, vsro

	def test_the_areas_are_appended_in_the_native_layout_and_name_their_tracks(self):
		native, vsro = self.tables()
		merged, tracks = variant.merge_music_tables(native, vsro, areas=(self.TOWN, self.FIELD))
		self.assertEqual(tracks, ["Egypt_Field_Delta.ogg", "Egypt_Town_Alexandria1.ogg"])
		regions = merged["regioninfo.txt"].decode("cp949").split("\r\n")
		self.assertEqual(regions[:2], ["#TOWN\t도적\t\t\t\t\t", "182\t96\tALL\t\t\t\t"])
		self.assertIn("48\t95\tALL\t\t\t\t", regions)
		self.assertIn("50\t93\tALL\t\t\t\t", regions)
		self.assertNotIn("1\t2\tALL\t\t\t\t", regions)
		sounds = merged["effectenvsnd.txt"].decode("cp949").split("\r\n")
		self.assertTrue(all(len(row.split("\t")) == variant.EFFECTENVSND_COLUMNS for row in sounds if row))
		self.assertIn('\t\t\t<3>\t"night_wind.wav"\t0~0\t', sounds)
		self.assertNotIn("<1>\tother\t\t\t\t\t", sounds)

	def test_an_area_the_native_tables_have_is_left_alone(self):
		native, vsro = self.tables()
		merged, _ = variant.merge_music_tables(native, vsro, areas=(self.TOWN,))
		again, tracks = variant.merge_music_tables(merged, vsro, areas=(self.TOWN,))
		self.assertEqual(tracks, [])
		self.assertEqual(again, merged)

	def test_a_missing_area_is_refused(self):
		native, vsro = self.tables()
		with self.assertRaises(SystemExit):
			variant.merge_music_tables(native, vsro, areas=("nowhere",))


class LinkTests(unittest.TestCase):
	def test_replacing_a_linked_file_leaves_the_native_one_alone(self):
		with tempfile.TemporaryDirectory() as scratch:
			root = Path(scratch)
			(root / "native" / "sub").mkdir(parents=True)
			(root / "native" / "sub" / "a.txt").write_bytes(b"native")
			variant.link_tree(root / "native", root / "variant")
			variant.replace_file(root / "variant" / "sub" / "a.txt", b"changed")
			self.assertEqual((root / "native" / "sub" / "a.txt").read_bytes(), b"native")
			self.assertEqual((root / "variant" / "sub" / "a.txt").read_bytes(), b"changed")


if __name__ == "__main__":
	unittest.main()


class GateLevelCapTests(unittest.TestCase):
	"""SRO_LEVEL_CAP is read with exactly the values the server accepts."""

	def test_native_values_keep_the_v1150_cap(self):
		for value in ("", "0", "off", "OFF", "false", "90", " 90 "):
			self.assertEqual(variant.gate_level_cap({variant.LEVEL_CAP_FLAG: value}), variant.NATIVE_LEVEL_CAP, value)
		self.assertEqual(variant.gate_level_cap({}), variant.NATIVE_LEVEL_CAP)

	def test_110_lifts_the_cap(self):
		self.assertEqual(variant.gate_level_cap({variant.LEVEL_CAP_FLAG: "110"}), variant.EXPANSION_LEVEL_CAP)

	def test_a_value_the_server_refuses_is_refused(self):
		for value in ("on", "true", "1", "100", "yes"):
			with self.assertRaises(ValueError, msg=value):
				variant.gate_level_cap({variant.LEVEL_CAP_FLAG: value})


class AddGatesTests(unittest.TestCase):
	"""add_gates runs on its own: no module from a later PR, the cap from SRO_LEVEL_CAP."""

	def run_add_gates(self, cap):
		native_tables, vsro_tables = GateTests().tables()
		vsro_files = {
			variant.TEXTDATA + "teleportdata.txt": vsro_tables["teleportdata.txt"],
			variant.TEXTDATA + "teleportbuilding.txt": vsro_tables["teleportbuilding.txt"],
			variant.TEXTDATA + "teleportlink.txt": vsro_tables["teleportlink.txt"],
			variant.TEXTDATA + "textdata_object.txt": vsro_tables["text"]
		}
		original_read, original_merge = variant.read_vsro_file, variant.merge_gate_tables
		original_cap = variant.os.environ.get(variant.LEVEL_CAP_FLAG)
		with tempfile.TemporaryDirectory() as scratch:
			native, out = Path(scratch) / "native", Path(scratch) / "out"
			textdata = native / "Media_extracted" / variant.TEXTDATA
			textdata.mkdir(parents=True)
			for name in ("teleportdata.txt", "teleportbuilding.txt", "teleportlink.txt"):
				(textdata / name).write_bytes(native_tables[name])
			(textdata / "textdataname.txt").write_bytes(native_tables["text"])
			variant.read_vsro_file = lambda root, archive, name: vsro_files[name]
			# merge_gate_tables binds GATE_CODES at definition; route the synthetic gates in.
			variant.merge_gate_tables = lambda *a, **k: original_merge(*a, **{**k, "codes": SYNTHETIC_GATES})
			variant.os.environ[variant.LEVEL_CAP_FLAG] = cap
			try:
				summary, merged = variant.add_gates(Path(scratch) / "vsro", native, out)
				written = (out / "Media_extracted" / variant.TEXTDATA / "teleportlink.txt").read_bytes()
			finally:
				variant.read_vsro_file, variant.merge_gate_tables = original_read, original_merge
				if original_cap is None:
					variant.os.environ.pop(variant.LEVEL_CAP_FLAG, None)
				else:
					variant.os.environ[variant.LEVEL_CAP_FLAG] = original_cap
		return summary, merged, written

	def entry_level(self, merged):
		rows = stage1.table_rows(merged["teleportlink.txt"])
		return next(row for row in rows if row[1:3] == ["1", "175"])[6:9]

	def test_add_gates_writes_the_merged_tables_at_the_native_cap(self):
		summary, merged, written = self.run_add_gates("")
		self.assertEqual(summary["gates"], ["GATE_SD1", "GATE_SD2"])
		self.assertEqual(written, merged["teleportlink.txt"])
		self.assertEqual(self.entry_level(merged), ["1", "90", "999"])

	def test_add_gates_keeps_the_authored_level_under_the_110_cap(self):
		_, merged, _ = self.run_add_gates("110")
		self.assertEqual(self.entry_level(merged), ["1", "95", "999"])
