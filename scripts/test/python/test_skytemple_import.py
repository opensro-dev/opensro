"""
===========================================================================
test_skytemple_import.py - the Sky Temple import's admission and build rules.
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import json
import os
import struct
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import import_vsro_alexandria as tables
import skytemple_gates as gates
import skytemple_import as sky
import sro_pk2


# ================
# utf16
# ================
def utf16(rows):
	return b"\xff\xfe" + ("\r\n".join("\t".join(row) for row in rows) + "\r\n").encode("utf-16-le")


# ================
# character
#
# One later (104-column) characterdata row.
# ================
def character(identity, code, level=150, grade=0, model="mob\\skytemple\\test.bsr", skill=0):
	row = ["0"] * 104
	row[0], row[1], row[2] = "1", str(identity), code
	row[5] = "SN_" + code
	row[15], row[52], row[57] = str(grade), model, str(level)
	if skill:
		row[83] = str(skill)
	return row


# ================
# skill
# ================
def skill(identity, code):
	row = ["0"] * 118
	row[0], row[1], row[3] = "1", str(identity), code
	return row


# ================
# sectors
#
# The four fields as the source lays them out: 72 sectors each.
# ================
def sectors():
	found = []
	for letter, (x0, y0) in (("A", (217, 82)), ("B", (225, 82)), ("C", (217, 92)), ("D", (225, 92))):
		for y in range(y0, y0 + 9):
			for x in range(x0, x0 + 8):
				found.append((letter, x, y))
	return found


# ================
# region_id
# ================
def region_id(x, y):
	return (y << 8) | x


# ================
# media_archive
# ================
def media_archive(files):
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
# chained_folder
#
# One folder's entries over as many directory blocks as they need: a
# block holds twenty entries and the last one's chain links the next
# block, so a folder chunk carries nineteen.
# ================
def chained_folder(builder, items, chains, first=None):
	per = sro_pk2.ENTRIES_PER_BLOCK - 1
	chunks = [items[at:at + per] for at in range(0, max(len(items), 1), per)]
	offsets = [first if first is not None else builder.folder([])]
	offsets += [builder.folder([]) for _ in chunks[1:]]
	for at, offset in enumerate(offsets):
		builder.blocks[offset] = chunks[at]
		if at + 1 < len(offsets):
			chains[offset] = offsets[at + 1]
	return offsets[0]


# ================
# map_archive
# ================
def map_archive(files):
	from test_client_preparation import ArchiveBuilder
	builder = ArchiveBuilder()
	root = builder.folder([])
	chains = {}
	folder_offsets = {}
	for folder in sorted({name.split("/")[0] for name in files if "/" in name}):
		items = [(2, name.split("/")[1].encode("ascii"), *builder.add_payload(raw))
			for name, raw in files.items() if name.startswith(folder + "/")]
		folder_offsets[folder] = chained_folder(builder, items, chains)
	root_items = [(1, name.encode("ascii"), folder_offsets[name], 0) for name in sorted(folder_offsets)]
	root_items += [(2, name.encode("ascii"), *builder.add_payload(raw)) for name, raw in files.items() if "/" not in name]
	chained_folder(builder, root_items, chains, root)
	return builder.build(chains)


# ================
# data_archive
# ================
def data_archive(files):
	return map_archive(files)


# ================
# terrain_m
# ================
def terrain_m():
	return b"JMXVMAPM1000" + bytes(36 * 2575)


# ================
# placement_o2
# ================
def placement_o2(object_id=None):
	out = bytearray(b"JMXVMAPO1001")
	for block in range(36):
		for group in range(4):
			if block == 0 and group == 0 and object_id is not None:
				out += struct.pack("<H", 1) + struct.pack("<I", object_id) + bytes(26)
			else:
				out += struct.pack("<H", 0)
	return bytes(out)


# ================
# mapinfo
# ================
def mapinfo():
	return b"JMXVMFO 1000" + struct.pack("<HH", 256, 128) + bytes(8) + bytes(4096)


# ================
# write_table
# ================
def write_table(root, name, rows):
	path = root / name
	path.parent.mkdir(parents=True, exist_ok=True)
	path.write_bytes(utf16(rows))


# ================
# read_rows
# ================
def read_rows(path):
	return tables.table_rows(path.read_bytes())


class SkytempleHelpersTest(unittest.TestCase):
	# ================
	# test_field_regions_and_placements
	# ================
	def test_field_regions_and_placements(self):
		rows = [["1", str(region_id(x, y)), f"RN_OTHER_SKYTEMPLE_{letter}_01", ""] for letter, x, y in sectors()]
		rows.append(["1", "25000", "RN_CH_JANGAN", ""])
		fields, named = sky.field_regions(rows)
		self.assertEqual(len(fields), 288)
		self.assertEqual(set(fields.values()), {"A", "B", "C", "D"})
		found = sky.placements(
			[["40001", "21209", "10", "1", "20"], ["40001", "21209", "10", "1", "20"],
				["40001", "25000", "1", "1", "1"], ["99999", "21209", "1", "1", "1"],
				["40001", "-100", "1", "1", "1"]],
			{"40001": "MOB_SKYTEMPLE_A_TEST", "99999": "MOB_OTHER"}, fields)
		self.assertEqual(found, [("MOB_SKYTEMPLE_A_TEST", 21209, 10.0, 1.0, 20.0)])

	# ================
	# test_select_kinds_window
	# ================
	def test_select_kinds_window(self):
		later = {"MOB_SKYTEMPLE_A_TEST": character(40001, "MOB_SKYTEMPLE_A_TEST")}
		refs = sky.select_kinds(later, {"MOB_SKYTEMPLE_A_TEST"})
		self.assertEqual(len(refs["MOB_SKYTEMPLE_A_TEST"]), 120)
		for level in (130, 179):
			later = {"MOB_SKYTEMPLE_A_TEST": character(40001, "MOB_SKYTEMPLE_A_TEST", level=level)}
			with self.assertRaisesRegex(ValueError, "outside 131-178"):
				sky.select_kinds(later, {"MOB_SKYTEMPLE_A_TEST"})
		with self.assertRaisesRegex(ValueError, "no later character row"):
			sky.select_kinds({}, {"MOB_SKYTEMPLE_A_TEST"})

	# ================
	# test_population_posture
	# ================
	def test_population_posture(self):
		found = [("MOB_SKYTEMPLE_A_TEST", 21210, 3.0, 1.0, 4.0), ("MOB_SKYTEMPLE_A_TEST", 21209, 1.0, 1.0, 2.0)]
		population = sky.build_population(found, {"MOB_SKYTEMPLE_A_TEST": []})
		self.assertEqual(population["Version"], 2)
		self.assertEqual([row["NestID"] for row in population["Rows"]], [1, 2])
		self.assertEqual(population["Rows"][0]["RegionID"], 21209)
		row = population["Rows"][0]
		self.assertEqual((row["MaxCount"], row["RespawnDelayMinSec"], row["RespawnDelayMaxSec"]), (1, 1, 3))
		self.assertEqual(row["Controls"]["SightRange"], 100)
		self.assertIsNone(row["Champion"])

	# ================
	# test_o2_round_trip
	# ================
	def test_o2_round_trip(self):
		data = placement_o2(777)
		self.assertEqual(sky.o2_object_ids(data), {777})
		patched = sky.patch_o2_objects(data, {777: 901})
		self.assertEqual(sky.o2_object_ids(patched), {901})
		with self.assertRaisesRegex(ValueError, "parsed"):
			sky.o2_object_ids(data + b"x")

	# ================
	# test_object_catalog_remap
	# ================
	def test_object_catalog_remap(self):
		current = 'hdr\n2\n00777 0 "res\\native\\other.bsr"\n00900 0 "res\\native\\far.bsr"\n'.encode("cp949")
		later = 'hdr\n2\n00777 0 "res\\skytemple\\obj777.bsr"\n00800 0 "res\\skytemple\\new.bsr"\n'.encode("cp949")
		merged_raw, remap = sky.merge_object_catalog_remap(current, later, {777})
		merged = tables.object_catalog(merged_raw)
		self.assertEqual(remap, {777: 901})
		self.assertEqual(merged[901][1], "res\\skytemple\\obj777.bsr")
		self.assertEqual(merged[800][1], "res\\skytemple\\new.bsr")
		self.assertEqual(merged[777][1], "res\\native\\other.bsr")
		_, no_remap = sky.merge_object_catalog_remap(current, later, set())
		self.assertEqual(no_remap, {})

	# ================
	# test_d_arrival_picks_the_fullest_region
	# ================
	def test_d_arrival_picks_the_fullest_region(self):
		fields = {21209: "A", 23000: "D", 23001: "D"}
		found = [("MOB_SKYTEMPLE_D_A", 23001, 5.0, 1.0, 6.0), ("MOB_SKYTEMPLE_D_A", 23000, 1.0, 1.0, 1.0),
			("MOB_SKYTEMPLE_D_A", 23001, 3.0, 1.0, 2.0), ("MOB_SKYTEMPLE_D_A", 21209, 0.0, 0.0, 0.0)]
		self.assertEqual(gates.d_arrival(found, fields), ("23001", "3", "1", "2"))


# ================
# gate_tables
#
# The later gates and the native anchor, as prepare_gates consumes them.
# ================
def gate_tables():
	data = []
	for identity, code, building, region, x, y, z, world in (
		(348, "GATE_TO_OTHER_SKYTEMPLE", 43802, 27011, 1836, 10, 694, 1),
		(349, "GATE_OTHER_SKYTEMPLE_A", 43803, 25567, 1295, 40, 1282, 132),
		(350, "GATE_OTHER_SKYTEMPLE_B", 43804, 25576, 1348, 50, 1339, 133),
		(351, "GATE_OTHER_SKYTEMPLE_C", 43805, 23007, 1301, 50, 1307, 134),
	):
		row = ["0"] * 13
		row[0], row[1], row[2], row[3] = "1", str(identity), code, str(building)
		row[4] = "SN_ZONE_X"
		row[5], row[6], row[7], row[8] = str(region), str(x), str(y), str(z)
		row[12] = str(world)
		data.append(row)
	buildings = []
	for identity, code, region in ((43802, "STORE_TK_TO_OTHER_TEMLPE_GATE", 27011),
		(43803, "STORE_OTHER_SKYTEMPLE_A_TO_TOWN", 25567), (43804, "STORE_OTHER_SKYTEMPLE_B_TO_TOWN", 25576),
		(43805, "STORE_OTHER_SKYTEMPLE_C_TO_TOWN", 23007)):
		row = ["0"] * 58
		row[0], row[1], row[2] = "1", str(identity), code
		row[5] = "SN_" + code
		row[41] = str(region)
		row[43], row[44], row[45] = "100", "1", "200"
		buildings.append(row)
	links = []
	for source, target in ((348, 349), (349, 348), (348, 350)):
		row = ["0"] * 23
		row[0], row[1], row[2] = "1", str(source), str(target)
		row[6], row[7], row[8] = "1", "125", "999"
		row[21], row[22] = "125", "3"
		links.append(row)
	return data, buildings, links


class SkytempleGatesTest(unittest.TestCase):
	# ================
	# test_prepare_gates_rehomes_and_authors
	# ================
	def test_prepare_gates_rehomes_and_authors(self):
		data, buildings, links = gate_tables()
		native_buildings = [["1", "2094", "STORE_CH_GATE"] + ["0"] * 38 + ["25000", "0", "1254", "-6", "1374"] + ["0"] * 12]
		native_data = [["1", "1", "GATE_CH", "2094", "SN_ZONE_CH", "25000", "969", "0", "1369", "0", "0", "0", "1"]]
		later = {"teleportdata.txt": data, "teleportbuilding.txt": buildings,
			"teleportlink.txt": links, "text": b""}
		encoded, codes = gates.prepare_gates(later, native_buildings, native_data, {"1", "3480"}, {"2094"}, ("23001", "100", "5", "200"))
		self.assertEqual(codes, gates.GATE_CODES)
		rows = {row[2]: row for row in tables.table_rows(encoded["teleportdata.txt"])}
		self.assertEqual(rows["GATE_TO_OTHER_SKYTEMPLE"][5:9], ["25000", "1264", "-6", "1374"])
		self.assertEqual(rows["GATE_TO_OTHER_SKYTEMPLE"][4], "SN_ZONE_CH")
		self.assertTrue(all(row[12] == "1" for row in rows.values()))
		d_row = rows["GATE_OTHER_SKYTEMPLE_D"]
		self.assertEqual(d_row[5:9], ["23001", "100", "5", "200"])
		built = {row[2]: row for row in tables.table_rows(encoded["teleportbuilding.txt"])}
		entry_building = built["STORE_TK_TO_OTHER_TEMLPE_GATE"]
		self.assertEqual([entry_building[41], entry_building[43], entry_building[44], entry_building[45]],
			["25000", "1264", "-6", "1374"])
		self.assertEqual(built["STORE_OTHER_SKYTEMPLE_D_TO_TOWN"][41], "23001")
		link_rows = tables.table_rows(encoded["teleportlink.txt"])
		self.assertEqual({len(row) for row in link_rows}, {21})
		pairs = {(row[1], row[2]) for row in link_rows}
		entry_id = rows["GATE_TO_OTHER_SKYTEMPLE"][1]
		self.assertIn((entry_id, rows["GATE_OTHER_SKYTEMPLE_C"][1]), pairs)
		self.assertIn((rows["GATE_OTHER_SKYTEMPLE_D"][1], entry_id), pairs)

	# ================
	# test_prepare_gates_renumbers_on_collision
	# ================
	def test_prepare_gates_renumbers_on_collision(self):
		data, buildings, links = gate_tables()
		later = {"teleportdata.txt": data, "teleportbuilding.txt": buildings,
			"teleportlink.txt": links, "text": b""}
		encoded, _ = gates.prepare_gates(later, [["1", "2094", "STORE_CH_GATE"] + ["0"] * 38 + ["25000", "0", "1", "2", "3"] + ["0"] * 12],
			[], {"348"}, {"43802"}, ("23001", "1", "1", "1"))
		rows = tables.table_rows(encoded["teleportdata.txt"])
		self.assertNotIn("348", {row[1] for row in rows})
		self.assertNotIn("43802", {row[3] for row in rows})


# ================
# fake_closure
# ================
class FakeClosure:
	def __init__(self):
		self.seeds = None

	def __call__(self, seeds, archives, native, output):
		self.seeds = set(seeds)
		return {"files": [], "missingSounds": [], "missingEffects": []}


# ================
# build_trees
#
# A native extracted root, a variant output root and a v1.696 source
# root in tmp, small but complete. Returns (base, native, output, source).
# ================
def build_trees(base):
	native, output, source = base / "native", base / "output", base / "source"
	text = sky.TEXT_ROOT
	native_buildings = [["1", "2094", "STORE_CH_GATE"] + ["0"] * 38 + ["25000", "0", "1254", "-6", "1374"] + ["0"] * 12]
	write_table(native / text, "teleportbuilding.txt", native_buildings)
	write_table(native / text, "teleportdata.txt",
		[["1", "1", "GATE_CH", "2094", "SN_ZONE_CH", "25000", "969", "0", "1369", "0", "0", "0", "1"]])

	write_table(output / text, "characterdata_5000.txt", [[str(v) for v in range(120)]])
	native_char = ["0"] * 120
	native_char[0], native_char[1], native_char[2] = "1", "100", "MOB_NATIVE_TEST"
	write_table(output / text, "characterdata_5000.txt", [native_char])
	write_table(output / text, "skilldata.txt", [["skilldata_5000.txt"]])
	write_table(output / text, "skilldata_5000.txt", [skill(50, "SKILL_NATIVE")])
	write_table(output / text, "textdataname.txt", [["1", "SN_NATIVE", "x", "", "", "Native", "", "", "", "", ""]])
	(output / text / "skilleffect.txt").write_bytes(b"\xff\xfe" + (
		"#section\tcharacterinfo\r\nMOB_NATIVE\t0\r\n#section\tskillaniset2\r\n1\tname\tSKILL_NATIVE\tANI_TEST\r\n"
	).encode("utf-16-le"))
	write_table(output / text, "teleportdata.txt",
		[["1", "1", "GATE_CH", "2094", "SN_ZONE_CH", "25000", "969", "0", "1369", "0", "0", "0", "1"]])
	write_table(output / text, "teleportbuilding.txt", native_buildings)
	write_table(output / text, "teleportlink.txt", [["1", "1", "1"] + ["0"] * 18])
	region_rows = [["1", "25000", "RN_CH_JANGAN", ""], ["1", "21209", "xxx", ""]]
	write_table(output / text, "regioncode.txt", region_rows)
	write_table(output / text, "textzonename.txt", [["1", "25000", "x", "", "", "Jangan", "", "", "", "", ""]])
	(output / "Map_extracted").mkdir(parents=True)
	(output / "Map_extracted" / "tile2d.ifo").write_bytes(
		'sig\n2\n00000 0 "nature" "old00.ddj"\n00005 0 "nature" "keep.ddj"\n'.encode("cp949"))
	(output / "Map_extracted" / "object.ifo").write_bytes(
		'sig\n2\n00777 0 "res\\native\\other.bsr"\n00900 0 "res\\native\\far.bsr"\n'.encode("cp949"))
	(output / "Data_extracted" / "navmesh").mkdir(parents=True)
	(output / "Data_extracted" / "navmesh" / "mapinfo.mfo").write_bytes(mapinfo())

	source.mkdir(parents=True)
	data_rows, building_rows, link_rows = gate_tables()
	monster = character(40001, "MOB_SKYTEMPLE_A_TEST", skill=700)
	media_files = {
		"characterdata.txt": utf16([["characterdata_5000.txt"]]),
		"characterdata_5000.txt": utf16([monster]),
		"skilldata.txt": utf16([["skilldata_5000.txt"]]),
		"skilldata_5000.txt": utf16([skill(700, "MSKILL_SKYTEMPLE_TEST")]),
		"npcpos.txt": utf16([["40001", "21209", "10", "1", "20"], ["40001", "21209", "10", "1", "20"],
			["40001", str(region_id(225, 92)), "30", "2", "40"], ["40001", "25000", "1", "1", "1"]]),
		"regioncode.txt": utf16([["1", str(region_id(x, y)), f"RN_OTHER_SKYTEMPLE_{letter}_01", ""]
			for letter, x, y in sectors()]),
		"textzonename.txt": utf16([["1", "RN_OTHER_SKYTEMPLE_A_01", "x", "", "", "Temple A", "", "", "", "", ""]]),
		"textdata_object.txt": utf16([["TextData_Object_1000.txt"]]),
		"textdata_object_1000.txt": utf16([["1", "11880", "SN_MOB_SKYTEMPLE_A_TEST", "Korean", "0", "0", "0", "0", "0",
			"Test Monster", "0", "0", "0", "Test Monster", "Test Monster", "Test Monster", "Test Monster"]]),
		"skilleffect.txt": b"\xff\xfe" + (
			"#section\tcharacterinfo\r\n" + "\t".join(["MOB_SKYTEMPLE_A_TEST"] + ["0"] * 12) + "\r\n"
			"#section\tskillaniset2\r\n1\tname\tMSKILL_SKYTEMPLE_TEST\tANI_TEST\r\n"
		).encode("utf-16-le"),
		"teleportdata.txt": utf16(data_rows),
		"teleportbuilding.txt": utf16(building_rows),
		"teleportlink.txt": utf16(link_rows),
	}
	(source / "Media.pk2").write_bytes(media_archive(media_files))

	map_files = {"tile2d.ifo": 'sig\n1\n00000 0 "nature" "tile00.ddj"\n'.encode("cp949"),
		"object.ifo": 'sig\n1\n00777 0 "res\\skytemple\\obj777.bsr"\n'.encode("cp949"),
		"tile2d/tile00.ddj": b"texture"}
	for letter, x, y in sectors():
		map_files[f"{y}/{x}.m"] = terrain_m()
		map_files[f"{y}/{x}.t"] = b"t"
		map_files[f"{y}/{x}.o"] = b"o"
		map_files[f"{y}/{x}.o2"] = placement_o2(777 if (x, y) == (217, 82) else None)
	(source / "Map.pk2").write_bytes(map_archive(map_files))
	data_files = {f"navmesh/nv_{region_id(x, y):04x}.nvm": b"nvm" for _letter, x, y in sectors()}
	(source / "Data.pk2").write_bytes(data_archive(data_files))
	(source / "Particles.pk2").write_bytes(data_archive({}))
	return native, output, source


class SkytempleImportTest(unittest.TestCase):
	# The fixture's archives are synthetic; the v1.696 pin file
	# (v1696_archive_pins.json, #3250) must not reach them, or the
	# importer's archive verification refuses the fixture before
	# anything is read. Point the pin at an absent file - the same
	# "nothing to verify against" posture a pre-pin tree has.
	def setUp(self):
		stub = mock.patch.object(sky, "PIN_FILE", Path(tempfile.gettempdir()) / "opensro-no-v1696-pins.json")
		stub.start()
		self.addCleanup(stub.stop)

	# ================
	# test_on_then_off
	# ================
	def test_on_then_off(self):
		with tempfile.TemporaryDirectory() as tmp:
			native, output, source = build_trees(Path(tmp))
			text = output / sky.TEXT_ROOT
			closure = FakeClosure()
			with mock.patch.dict(os.environ, {sky.FLAG: "1"}):
				summary = sky.add_skytemple(native, output, source, closure)
			self.assertTrue(summary["enabled"])
			self.assertEqual((summary["kinds"], summary["nests"], len(summary["regions"])), (1, 2, 288))

			shard = read_rows(text / sky.CHARACTER_SHARD)
			self.assertEqual(len(shard), 1)
			self.assertEqual((shard[0][1], shard[0][2], len(shard[0])), ("101", "MOB_SKYTEMPLE_A_TEST", 120))
			population = json.loads((text / sky.POPULATION_FILE).read_text())
			self.assertEqual([row["RegionID"] for row in population["Rows"]], [21209, region_id(225, 92)])
			gates = {row[2]: row for row in read_rows(text / "teleportdata.txt")}
			self.assertEqual(gates["GATE_TO_OTHER_SKYTEMPLE"][5], "25000")
			self.assertIn("GATE_OTHER_SKYTEMPLE_D", gates)
			links = read_rows(text / "teleportlink.txt")
			self.assertTrue(any(row[6] == "1" and row[7] == "90" for row in links if len(row) > 7))
			regions = read_rows(text / "regioncode.txt")
			self.assertEqual(sum(1 for row in regions if row[2].startswith("RN_OTHER_SKYTEMPLE")), 288)
			self.assertIn("res/mob/skytemple/test.bsr", closure.seeds)
			self.assertTrue(any("obj777.bsr" in seed for seed in closure.seeds))
			terrain = (output / "Map_extracted" / "82" / "217.m").read_bytes()
			self.assertEqual(tables.terrain_tile_ids(terrain), {6})
			self.assertEqual(sky.o2_object_ids((output / "Map_extracted" / "82" / "217.o2").read_bytes()), {901})
			mfo = (output / "Data_extracted" / "navmesh" / "mapinfo.mfo").read_bytes()
			rid = region_id(217, 82)
			self.assertTrue(mfo[24 + (rid >> 3)] & (0x80 >> (rid & 7)))
			self.assertTrue((output / sky.MARKER).exists())

			with mock.patch.dict(os.environ, {sky.FLAG: "off"}):
				off = sky.add_skytemple(native, output, source, closure)
			self.assertEqual(off, {"enabled": False})
			for name in (sky.CHARACTER_SHARD, sky.SKILL_SHARD, sky.POPULATION_FILE):
				self.assertFalse((text / name).exists(), name)
			self.assertFalse((output / sky.MARKER).exists())
			self.assertFalse((output / "Map_extracted" / "82" / "217.m").exists())
			self.assertEqual([row[0] for row in read_rows(text / "skilldata.txt")], ["skilldata_5000.txt"])
			gates = {row[2] for row in read_rows(text / "teleportdata.txt")}
			self.assertNotIn("GATE_OTHER_SKYTEMPLE_D", gates)
			regions = read_rows(text / "regioncode.txt")
			self.assertFalse(any(row[2].startswith("RN_OTHER_SKYTEMPLE") for row in regions))
			names = {row[1] for row in read_rows(text / "textdataname.txt") if len(row) > 1}
			self.assertNotIn("SN_MOB_SKYTEMPLE_A_TEST", names)

	# ================
	# test_off_without_marker_is_a_noop
	# ================
	def test_off_without_marker_is_a_noop(self):
		with tempfile.TemporaryDirectory() as tmp:
			native, output, source = build_trees(Path(tmp))
			with mock.patch.dict(os.environ, {sky.FLAG: "off"}):
				self.assertEqual(sky.add_skytemple(native, output, source, FakeClosure()), {"enabled": False})
			self.assertFalse((output / sky.POPULATION_FILE).exists())


if __name__ == "__main__":
	unittest.main()
