"""
===========================================================================
test_constantinople_import.py - the Constantinople fortress import's rules.
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

import constantinople_import as ct
import import_vsro_alexandria as tables
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
def character(identity, code, model="bldg\\guild\\constantinople\\test.bsr"):
	row = ["0"] * 104
	row[0], row[1], row[2] = "1", str(identity), code
	row[5] = "SN_" + code
	row[52] = model
	return row


# ================
# native_character
#
# One shipped (120-column) characterdata row of the output tree.
# ================
def native_character(identity, code):
	row = ["0"] * 120
	row[0], row[1], row[2] = "1", str(identity), code
	row[5] = "SN_" + code
	return row


# ================
# region_id
# ================
def region_id(x, y):
	return (y << 8) | x


# ================
# sectors
#
# The fortress yard: sectors x 90-95, z 59-64.
# ================
def sectors():
	return [(x, y) for y in range(59, 65) for x in range(90, 96)]


# ================
# zone_rows
#
# The 49 fortress zones cycling four default structures; two of them
# the shipped tables lack, two they hold.
# ================
DEFAULTS = ("STRUCTURE_FORT_STONE_CT_01", "STRUCTURE_GATE_PULLEY_CT_01",
	"STRUCTURE_GUARD_TOWER_01", "STRUCTURE_HEADQUARTERS_00")


def zone_rows():
	rows = []
	for index in range(49):
		rows.append(["1", str(371 + index), f"STRUCTURE_POS_CT_ZONE_{index + 1:02d}",
			"FORT_STONE", "1", "0", "0", "0", "0", DEFAULTS[index % len(DEFAULTS)], "INS_FORT_CT"])
	return rows


# ================
# zone_lines
#
# The objectstring.ifo lines placing the 49 zones, plus two Jangan
# decoys the import must leave behind.
# ================
def zone_lines():
	lines = ["JMXVOBJI1000", "51",
		'0x3c5d4403 0x00000000 93 60 0x43c1ea10 0xbfb64737 0x4436ffe6 0x00000000 "STRUCTURE_POS_JA_GATE_01"']
	for index in range(49):
		lines.append('0x3c5d4403 0x00000000 93 60 0x43c1ea10 0xbfb64737 0x4436ffe6 '
			f'0x00000000 "STRUCTURE_POS_CT_ZONE_{index + 1:02d}"')
	lines.append('0x3c5d4403 0x00000000 93 60 0x43c1ea10 0xbfb64737 0x4436ffe6 0x00000000 "STRUCTURE_POS_JA_GATE_02"')
	return lines


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
# root in tmp, small but complete. Returns (native, output, source).
# ================
def build_trees(base):
	native, output, source = base / "native", base / "output", base / "source"
	text = ct.TEXT_ROOT
	write_table(output / text, "characterdata_5000.txt", [
		native_character(100, "MOB_NATIVE_TEST"),
		native_character(22462, "NPC_EU_FORTRESS_MANAGER"),
		native_character(200, "STRUCTURE_GUARD_TOWER_01"),
		native_character(201, "STRUCTURE_HEADQUARTERS_00")])
	write_table(output / text, "eventzonedata.txt",
		[["1", "84", "STRUCTURE_POS_JA_FORT_STONE_01", "FORT_STONE", "1", "0", "0", "0", "0",
			"STRUCTURE_FORT_STONE_JA_01", "INS_FORT_JA"]])
	write_table(output / text, "siegefortress.txt",
		[["1", "1", "FORTRESS_JANGAN", "Korean", "SN_FORTRESS_JANGAN", "GATE_CH", "0", "300",
			"30", "25", "63", "5000000", "etc\\fort_jangan.ddj", "NPC_CH_FORTRESS_OFFICIAL"]])
	write_table(output / text, "regioncode.txt",
		[["1", "25000", "RN_CH_JANGAN", ""], ["1", "15194", "xxx", ""]])
	write_table(output / text, "textzonename.txt", [["1", "25000", "x", "", "", "Jangan", "", "", "", "", ""]])
	write_table(output / text, "textdataname.txt", [["1", "SN_NATIVE", "x", "", "", "Native", "", "", "", "", ""]])
	(output / "Map_extracted").mkdir(parents=True)
	(output / "Map_extracted" / "tile2d.ifo").write_bytes(
		'sig\n2\n00000 0 "nature" "old00.ddj"\n00005 0 "nature" "keep.ddj"\n'.encode("cp949"))
	(output / "Map_extracted" / "object.ifo").write_bytes(
		'sig\n2\n00777 0 "res\\native\\other.bsr"\n00900 0 "res\\native\\far.bsr"\n'.encode("cp949"))
	(output / "Data_extracted" / "navmesh").mkdir(parents=True)
	(output / "Data_extracted" / "navmesh" / "mapinfo.mfo").write_bytes(mapinfo())

	source.mkdir(parents=True)
	later_chars = [character(22461, "NPC_EU_FORTRESS_OFFICIAL"),
		character(22462, "NPC_EU_FORTRESS_MANAGER"),
		character(22544, "STRUCTURE_FORT_STONE_CT_01"),
		character(40940, "STRUCTURE_GATE_PULLEY_CT_01"),
		character(22545, "STRUCTURE_GUARD_TOWER_01"),
		character(22546, "STRUCTURE_HEADQUARTERS_00")]
	names = ["SN_FORTRESS_CONSTANTINOPLE", "SN_STRUCTURE_FORT_STONE_CT_01", "SN_STRUCTURE_GATE_PULLEY_CT_01"]
	media_files = {
		"characterdata.txt": utf16([["characterdata_5000.txt"]]),
		"characterdata_5000.txt": utf16(later_chars),
		"eventzonedata.txt": utf16(zone_rows()),
		"siegefortress.txt": utf16([
			["1", "1", "FORTRESS_JANGAN", "Korean", "SN_FORTRESS_JANGAN", "GATE_CH", "0", "300",
				"30", "25", "63", "5000000", "etc\\fort_jangan.ddj", "NPC_CH_FORTRESS_OFFICIAL"],
			["1", "4", "FORTRESS_CONSTANTINOPLE", "Korean", "SN_FORTRESS_CONSTANTINOPLE", "GATE_EU",
				"0", "300", "30", "25", "63", "5000000", "etc\\fort_constantinople.ddj",
				"NPC_EU_FORTRESS_OFFICIAL"]]),
		"regioncode.txt": utf16([["1", str(region_id(x, y)), "RN_FORT_CONSTANTINOPLE", ""]
			for x, y in sectors()] + [["1", "25000", "RN_CH_JANGAN", ""]]),
		"textzonename.txt": utf16([["1", str(region_id(x, y)), "x", "", "", "Constantinople", "", "", "", "", ""]
			for x, y in sectors()]),
		"npcpos.txt": utf16([["22462", str(region_id(93, 60)), "10", "1", "20"],
			["40940", str(region_id(93, 60)), "11", "1", "21"],
			["99999", str(region_id(93, 60)), "1", "1", "1"],
			["22462", "25000", "1", "1", "1"]]),
		"textdata_object.txt": utf16([["TextData_Object_1000.txt"]]),
		"textdata_object_1000.txt": utf16([["1", str(11880 + at), symbol, "Korean", "0", "0", "0", "0", "0",
			"Text " + symbol, "0", "0", "0", "Text " + symbol, "Text", "Text", "Text"]
			for at, symbol in enumerate(names)]),
	}
	(source / "Media.pk2").write_bytes(media_archive(media_files))

	map_files = {"tile2d.ifo": 'sig\n1\n00000 0 "nature" "tile00.ddj"\n'.encode("cp949"),
		"object.ifo": 'sig\n1\n00777 0 "res\\constantinople\\obj777.bsr"\n'.encode("cp949"),
		"tile2d/tile00.ddj": b"texture"}
	for x, y in sectors():
		map_files[f"{y}/{x}.m"] = terrain_m()
		map_files[f"{y}/{x}.t"] = b"t"
		map_files[f"{y}/{x}.o"] = b"o"
		map_files[f"{y}/{x}.o2"] = placement_o2(777 if (x, y) == (90, 59) else None)
	(source / "Map.pk2").write_bytes(map_archive(map_files))
	data_files = {f"navmesh/nv_{region_id(x, y):04x}.nvm": b"nvm" for x, y in sectors()}
	data_files["navmesh/objectstring.ifo"] = ("\r\n".join(zone_lines()) + "\r\n").encode("latin1")
	(source / "Data.pk2").write_bytes(map_archive(data_files))
	(source / "Particles.pk2").write_bytes(map_archive({}))
	native.mkdir(parents=True)
	return native, output, source


class ConstantinopleImportTest(unittest.TestCase):
	# The fixture's archives are synthetic; the v1.696 pin file must
	# not reach them (the Sky Temple test's posture).
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
			text = output / ct.TEXT_ROOT
			closure = FakeClosure()
			with mock.patch.dict(os.environ, {ct.FLAG: "1"}):
				summary = ct.add_constantinople(native, output, source, closure)
			self.assertTrue(summary["enabled"])
			self.assertEqual((summary["zones"], len(summary["regions"]), len(summary["characters"])), (49, 36, 2))

			shard = read_rows(text / ct.CHARACTER_SHARD)
			self.assertEqual([row[2] for row in shard],
				["STRUCTURE_FORT_STONE_CT_01", "STRUCTURE_GATE_PULLEY_CT_01"])
			self.assertTrue(all(int(row[1]) > 22462 for row in shard))
			self.assertTrue(all(len(row) == 120 for row in shard))

			zones = read_rows(text / "eventzonedata.txt")
			self.assertEqual(len(zones), 50)
			siege = read_rows(text / "siegefortress.txt")
			self.assertEqual([row[2] for row in siege], ["FORTRESS_JANGAN", "FORTRESS_CONSTANTINOPLE"])
			regions = read_rows(text / "regioncode.txt")
			self.assertEqual(sum(1 for row in regions if row[2] == "RN_FORT_CONSTANTINOPLE"), 36)
			names = {row[1] for row in read_rows(text / "textdataname.txt")}
			self.assertIn("SN_FORTRESS_CONSTANTINOPLE", names)
			owned = set(summary["nameSymbols"])
			self.assertIn("SN_FORTRESS_CONSTANTINOPLE", owned)
			self.assertNotIn("SN_NATIVE", owned)

			# A second enabled run appends nothing and keeps the ownership.
			with mock.patch.dict(os.environ, {ct.FLAG: "1"}):
				again = ct.add_constantinople(native, output, source, closure)
			self.assertEqual(set(again["nameSymbols"]), owned)

			ifo = (output / "Data_extracted" / "navmesh" / ct.PORTONLY_IFO).read_text(encoding="latin1")
			ifo_lines = ifo.splitlines()
			self.assertEqual((ifo_lines[0], ifo_lines[1], len(ifo_lines)), ("JMXVOBJI1000", "49", 51))
			self.assertNotIn("STRUCTURE_POS_JA", ifo)

			with mock.patch.dict(os.environ, {ct.FLAG: "off"}):
				off = ct.add_constantinople(native, output, source, closure)
			self.assertEqual(off, {"enabled": False})
			self.assertFalse((text / ct.CHARACTER_SHARD).exists())
			self.assertFalse((output / "Data_extracted" / "navmesh" / ct.PORTONLY_IFO).exists())
			self.assertFalse((output / ct.MARKER).exists())
			self.assertEqual(len(read_rows(text / "eventzonedata.txt")), 1)
			self.assertEqual([row[2] for row in read_rows(text / "siegefortress.txt")], ["FORTRESS_JANGAN"])
			regions = read_rows(text / "regioncode.txt")
			self.assertEqual(sum(1 for row in regions if row[2] == "RN_FORT_CONSTANTINOPLE"), 0)
			names = {row[1] for row in read_rows(text / "textdataname.txt")}
			self.assertEqual(names & owned, set(), "the off run reaps every name row the import added")
			self.assertIn("SN_NATIVE", names)

	# ================
	# test_counts_refuse
	# ================
	def test_counts_refuse(self):
		rows = [["1", str(region_id(x, y)), "RN_FORT_CONSTANTINOPLE", ""] for x, y in sectors()[:-1]]
		with self.assertRaisesRegex(ValueError, "expected 36"):
			ct.fortress_regions(rows)
		with self.assertRaisesRegex(ValueError, "expected 49"):
			ct.fortress_zones(zone_rows()[:-1])

	# ================
	# test_objectstring_lines
	# ================
	def test_objectstring_lines(self):
		names = {f"STRUCTURE_POS_CT_ZONE_{index + 1:02d}" for index in range(49)}
		found = ct.objectstring_lines("\r\n".join(zone_lines()), names)
		self.assertEqual(len(found), 49)
		with self.assertRaisesRegex(ValueError, "places"):
			ct.objectstring_lines("\r\n".join(zone_lines()[:-2] + zone_lines()[-1:]), names)


if __name__ == "__main__":
	unittest.main()
