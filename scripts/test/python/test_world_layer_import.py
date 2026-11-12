"""
===========================================================================

test_world_layer_import.py - the later client's seasonal layers land in the variant

A synthetic Map archive (layer tables, object catalog, two region files)
against a synthetic variant tree: only the switched-off DECO layers'
members are copied, with the variant's object ids and free uids, the
table names each one, and a second run and an off run leave no stale
record.

===========================================================================
"""
import json
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import sro_pk2  # noqa: E402
import world_layer_import as layers  # noqa: E402

JANGAN = (97, 168)
JANGAN_REGION = 97 << 8 | 168

DEFINITIONS = "\n".join([
	'1 0 1 "BASIC"',
	'0 4 0 "Halloween (MESH)"',
	'0 5 1 "Halloween (DECO)"',
	'1 2 0 "guard post (MESH)"',
	'0 7 1 "Christmas (DECO)"'
]) + "\n"


# ================
# record
# ================
def record(object_id, uid, region=JANGAN_REGION, x=100.0):
	return (object_id, x, 10.0, 200.0, 0, 1.5, uid, 0, 0, 0, region)


# ================
# o2
# ================
def o2(placements):
	"""A region file with each (group index, record) placed."""
	groups = [[] for _ in range(layers.O2_BLOCKS * layers.O2_LOD_GROUPS)]
	for index, row in placements:
		groups[index].append(row)
	return layers.write_o2(groups)


# ================
# layer_list
# ================
def layer_list(rows):
	lines = [layers.LAYER_LIST_SIGNATURE, str(len(rows))]
	for uid, layer in rows:
		lines.append(f"0x{JANGAN_REGION << 16 | uid:08x} {layer} {JANGAN[1]} {JANGAN[0]} 0x0 0x0 0x0 0x0 0")
	return ("\n".join(lines) + "\n").encode("cp949")


# ================
# later_archive
# ================
def later_archive():
	"""(bytes, {path: Entry}) of a Map.pk2 payload set."""
	later_objects = "\n".join([
		"JMXVOBJI1000", "3",
		'00010 0x00000000 "res\\bldg\\china\\jangan\\gate.bsr"',
		'00020 0x00000000 "res\\etc\\halloween\\halloween_pu03.bsr"',
		'00030 0x00000000 "res\\etc\\x_mas\\x_mas_tree_s01.bsr"'
	]) + "\n"
	files = {
		layers.LAYER_DEFINITIONS: DEFINITIONS.encode("cp949"),
		# uid 7: Halloween DECO; 8: Halloween MESH (left out); 9: Christmas;
		# 3: a placement of no layer.
		layers.LAYER_LIST: layer_list([(7, 5), (8, 4), (9, 7)]),
		layers.OBJECT_CATALOG: later_objects.encode("cp949"),
		"97/168.o2": o2([(0, record(10, 3)), (5, record(20, 7)), (5, record(20, 8)), (9, record(30, 9))]),
		# A neighbour file the variant does not have, holding Jangan's
		# pumpkin as the native files hold a placement that crosses regions.
		"50/50.o2": o2([(0, record(20, 7))])
	}
	blob, index = b"", {}
	for path, raw in files.items():
		index[path] = sro_pk2.Entry(path, len(blob), len(raw))
		blob += raw
	return blob, index


# ================
# variant_tree
# ================
def variant_tree(root):
	"""The variant's Jangan: the gate under id 1 and a native uid 7 already."""
	map_root = root / "Map_extracted"
	(map_root / "97").mkdir(parents=True)
	(map_root / "97" / "168.o2").write_bytes(o2([(0, record(1, 7)), (0, record(1, 3))]))
	(map_root / "object.ifo").write_bytes((
		"JMXVOBJI1000\n2\n"
		'00000 0x00000001 "res\\bldg\\china\\jangan\\wall.bsr"\n'
		'00001 0x00000001 "res\\bldg\\china\\jangan\\gate.bsr"\n'
	).encode("cp949"))
	return map_root


class WorldLayerImportTest(unittest.TestCase):

	# ================
	# test_definitions_and_list_parse
	# ================
	def test_definitions_and_list_parse(self):
		parsed = layers.parse_layer_definitions(DEFINITIONS.encode("cp949"))
		self.assertEqual(layers.seasonal_layers(parsed), {5, 7})
		members = layers.parse_layer_list(layer_list([(7, 5), (8, 4)]))
		self.assertEqual(members, {(JANGAN_REGION, 7): 5, (JANGAN_REGION, 8): 4})
		wrong = layer_list([(7, 5)]).replace(b" 168 97 ", b" 1 1 ")
		with self.assertRaises(ValueError):
			layers.parse_layer_list(wrong)

	# ================
	# test_only_switched_off_deco_members_land
	# ================
	def test_only_switched_off_deco_members_land(self):
		with tempfile.TemporaryDirectory() as scratch:
			out = Path(scratch)
			map_root = variant_tree(out)
			blob, index = later_archive()
			summary, files, resources = layers.import_layers(blob, index, out)
			groups = layers.read_o2((map_root / "97" / "168.o2").read_bytes())
			placed = [r for records in groups for r in records]
			# The two native records plus Halloween DECO and Christmas DECO.
			self.assertEqual(len(placed), 4)
			_, rows = layers.variant.catalog_lines((map_root / "object.ifo").read_bytes())
			paths = layers.catalog_paths(rows)
			pumpkin = paths["res\\etc\\halloween\\halloween_pu03.bsr"]
			tree = paths["res\\etc\\x_mas\\x_mas_tree_s01.bsr"]
			self.assertEqual((pumpkin, tree), (2, 3), "new rows follow the variant's last id")
			added = {r[0]: r for r in placed if r[0] in (pumpkin, tree)}
			# uid 7 is native in the variant, so the pumpkin moves to a free one.
			self.assertNotIn(added[pumpkin][6], (3, 7))
			self.assertEqual(added[tree][6], 9)
			self.assertEqual(groups[5][0][0], pumpkin, "the record keeps its block and LOD group")
			table = json.loads((map_root / layers.OUTPUT_NAME).read_text(encoding="utf-8"))
			self.assertEqual(sorted(map(tuple, table["placements"])),
				sorted([(JANGAN_REGION, added[pumpkin][6], 5), (JANGAN_REGION, 9, 7)]))
			self.assertEqual([row["id"] for row in table["layers"]], [5, 7])
			self.assertEqual(summary["regionsAbsent"], ["50/50"])
			self.assertEqual(summary["meshRowsLeftOut"], 1)
			self.assertEqual(sorted(p for _, p in resources),
				["res/etc/halloween/halloween_pu03.bsr", "res/etc/x_mas/x_mas_tree_s01.bsr"])
			self.assertEqual(list(files), ["97/168.o2"])

	# ================
	# test_rerun_and_off_leave_no_stale_record
	# ================
	def test_rerun_and_off_leave_no_stale_record(self):
		with tempfile.TemporaryDirectory() as scratch:
			out = Path(scratch)
			map_root = variant_tree(out)
			native = (map_root / "97" / "168.o2").read_bytes()
			blob, index = later_archive()
			_, files, _ = layers.import_layers(blob, index, out)
			report = {"files": files}
			layers.strip_previous(out, report)
			self.assertEqual((map_root / "97" / "168.o2").read_bytes(), native)
			layers.import_layers(blob, index, out)
			once = (map_root / "97" / "168.o2").read_bytes()
			(out / layers.REPORT_NAME).write_text(json.dumps(report), encoding="utf-8")
			self.assertEqual(layers.add_layers(None, out, out, None), {"enabled": False})
			self.assertEqual((map_root / "97" / "168.o2").read_bytes(), native)
			self.assertFalse((map_root / layers.OUTPUT_NAME).exists())
			self.assertNotEqual(once, native)


if __name__ == "__main__":
	unittest.main()
