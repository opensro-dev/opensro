"""
===========================================================================

test_pharaoh_temple.py - the Pharaoh Temple's variant tables, switch on and off

Port-only, not native. Synthetic tables and a scratch tree; no game data is
read. The off value leaves the variant's dungeon and event-zone tables the
native bytes (and puts them back after an on build); the on value adds
exactly the seven dungeons, the two walk-in zones and the four gates, with
the gates' world column flattened.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import sys
import tempfile
import unittest
from pathlib import Path

import build_alexandria_variant as variant  # noqa: E402
import import_vsro_alexandria as stage1  # noqa: E402
import pharaoh_temple as temple  # noqa: E402
from test_build_alexandria_variant import building, gate, link, table, text  # noqa: E402

NATIVE_DUNGEON_INFO = (
	'// 리젠ID\t파일이름\r\n'
	'1\t"Dungeon\\wchina\\Dunhwang_Cv.dof"\r\n'
	'9\t"Dungeon\\wchina\\event.dof"'
).encode("cp949")


def zone(zone_id, code):
	return ["1", zone_id, code, "TELEPORT", "0", "0", "0", "0", "0", code, "xxx", "xxx", "xxx", "xxx"]


NATIVE_ZONES = table([zone("1", "GATE_DUNGEON_DH_IN"), zone("233", "STRUCTURE_POS_EVENT_GATE_RED")])
VSRO_ZONES = table([
	zone("1", "GATE_DUNGEON_DH_IN"), zone("234", "GATE_TEMPLE_MERCHANT_HUNTER_GATE"),
	zone("235", "GATE_TEMPLE_THIEF_GATE")
])


# ================
# TempleSwitchTests
# ================
class TempleSwitchTests(unittest.TestCase):
	def test_unset_and_off_values_are_the_native_build(self):
		for value in ("", "0", "off", "false", " OFF "):
			self.assertFalse(temple.temple_enabled({temple.FLAG: value}), value)
		self.assertFalse(temple.temple_enabled({}))

	def test_on_values_enable_the_temple(self):
		for value in ("1", "on", "true", "ON"):
			self.assertTrue(temple.temple_enabled({temple.FLAG: value}), value)

	def test_a_value_nobody_defined_is_refused(self):
		with self.assertRaises(ValueError):
			temple.temple_enabled({temple.FLAG: "yes please"})


# ================
# DungeonTableTests
# ================
class DungeonTableTests(unittest.TestCase):
	def test_the_seven_dungeons_are_regions_800a_to_8010(self):
		self.assertEqual(temple.dungeon_regions(), list(range(0x800A, 0x8011)))
		self.assertEqual(temple.dungeon_regions()[-1], 0x8010)
		self.assertEqual(temple.DUNGEONS[-1][0], temple.TEMPLE_DUNGEON_ID)

	def test_the_dungeon_ids_collide_with_no_native_row(self):
		merged = temple.merge_dungeon_info(NATIVE_DUNGEON_INFO)
		self.assertTrue(merged.startswith(NATIVE_DUNGEON_INFO))
		ids = [int(line.split("\t")[0]) for line in merged.decode("cp949").splitlines() if line[:1].isdigit()]
		self.assertEqual(ids, [1, 9, 10, 11, 12, 13, 14, 15, 16])

	def test_a_native_id_or_path_that_is_already_taken_is_refused(self):
		taken_id = NATIVE_DUNGEON_INFO + b'\r\n12\t"Dungeon\\other.dof"'
		with self.assertRaises(ValueError):
			temple.merge_dungeon_info(taken_id)
		taken_path = NATIVE_DUNGEON_INFO + b'\r\n99\t"dungeon\\asiam\\r1_cv\\pha_cv.dof"'
		with self.assertRaises(ValueError):
			temple.merge_dungeon_info(taken_path)

	def test_merging_twice_changes_nothing(self):
		once = temple.merge_dungeon_info(NATIVE_DUNGEON_INFO)
		self.assertEqual(temple.merge_dungeon_info(once), once)

	def test_the_dof_paths_are_the_staged_lower_case_files(self):
		paths = [temple.dof_path(dungeon) for dungeon in temple.DUNGEONS]
		self.assertEqual(paths[-1], "dungeon/asiam/r1_cv/pha_cv.dof")
		self.assertIn("dungeon/asiam/r2_cv/st_seth.dof", paths)
		self.assertEqual(len(set(paths)), 7)


# ================
# EventZoneTests
# ================
class EventZoneTests(unittest.TestCase):
	def test_the_two_temple_zones_follow_the_native_rows(self):
		merged = temple.merge_event_zones(NATIVE_ZONES, VSRO_ZONES)
		self.assertTrue(merged.startswith(NATIVE_ZONES[:40]))
		rows = stage1.table_rows(merged)
		self.assertEqual([row[2] for row in rows], [
			"GATE_DUNGEON_DH_IN", "STRUCTURE_POS_EVENT_GATE_RED",
			"GATE_TEMPLE_MERCHANT_HUNTER_GATE", "GATE_TEMPLE_THIEF_GATE"])
		self.assertEqual([row[1] for row in rows][-2:], ["234", "235"])

	def test_a_zone_id_the_native_table_uses_is_refused(self):
		native = table([zone("234", "SOMETHING_ELSE")])
		with self.assertRaises(ValueError):
			temple.merge_event_zones(native, VSRO_ZONES)

	def test_a_source_without_the_zones_is_refused(self):
		with self.assertRaises(ValueError):
			temple.merge_event_zones(NATIVE_ZONES, table([zone("1", "GATE_DUNGEON_DH_IN")]))

	def test_merging_twice_changes_nothing(self):
		once = temple.merge_event_zones(NATIVE_ZONES, VSRO_ZONES)
		self.assertEqual(temple.merge_event_zones(once, VSRO_ZONES), once)


# ================
# TempleGateTests
# ================
class TempleGateTests(unittest.TestCase):
	CODES = temple.GATE_CODES

	def tables(self):
		native = {
			"teleportdata.txt": table([gate("1", "GATE_CH", "2094", "SN_Z1")]),
			"teleportbuilding.txt": table([building("2094", "STORE_CH_GATE", "SN_B1")]),
			"teleportlink.txt": table([link("1", "1", "0")]),
			"text": table([["1", "SN_Z1", "k", "", "", "s", "t", "j", "Jangan", "v", ""]])
		}
		gates = [
			gate("171", self.CODES[0], "27751", "SN_ZONE_21836_9"), gate("172", self.CODES[1], "27752", "SN_ZONE_21836_9"),
			gate("173", self.CODES[2], "0", "SN_ZONE_21837_1"), gate("174", self.CODES[3], "0", "SN_ZONE_21837_1")
		]
		for at in (2, 3):
			gates[at][5] = "-32752"
			gates[at][variant.TELEPORT_WORLD_COLUMN] = "60"
		vsro = {
			"teleportdata.txt": table(gates),
			"teleportbuilding.txt": table([
				building("27751", "STORE_DESERT_MERCHANT_HUNTER_GATE", "SN_B_M"), building("27752", "STORE_DESERT_THIEF_GATE", "SN_B_T")
			]),
			"teleportlink.txt": table([
				link("171", "173", "0", "105"), link("172", "174", "0", "105"),
				link("173", "171", "0", "105"), link("174", "172", "0", "105")
			]),
			"text": table([
				text("SN_ZONE_21836_9", "k", "t", "s", "j", "Desert"), text("SN_ZONE_21837_1", "k", "t", "s", "j", "Temple"),
				text("SN_B_M", "k", "t", "s", "j", "Merchant gate"), text("SN_B_T", "k", "t", "s", "j", "Thief gate")
			])
		}
		return native, vsro

	def test_the_four_gates_are_opt_in_and_named_once(self):
		self.assertEqual(variant.TEMPLE_GATE_CODES, temple.GATE_CODES)
		self.assertEqual(len(set(temple.GATE_CODES)), 4)
		self.assertFalse(set(temple.GATE_CODES) & set(variant.GATE_CODES), "temple gates are opt-in")
		self.assertFalse(set(temple.GATE_CODES) & set(variant.TOMB_GATE_CODES))

	def test_each_desert_gate_links_to_its_temple_gate_and_back_at_level_105(self):
		native, vsro = self.tables()
		merged, summary = variant.merge_gate_tables(
			native, vsro, codes=self.CODES, level_cap=110, flatten=self.CODES)
		self.assertEqual(summary["links"], 4)
		self.assertEqual(summary["buildings"], 2, "the two walk-in gates have no object")
		links = [r for r in stage1.table_rows(merged["teleportlink.txt"]) if r[1] in {"171", "172", "173", "174"}]
		self.assertEqual([(r[1], r[2]) for r in links], [("171", "173"), ("172", "174"), ("173", "171"), ("174", "172")])
		self.assertTrue(all(r[6:9] == ["1", "105", "999"] for r in links))

	def test_the_world_column_is_flattened_to_the_persistent_world(self):
		native, vsro = self.tables()
		merged, summary = variant.merge_gate_tables(
			native, vsro, codes=self.CODES, level_cap=110, flatten=self.CODES)
		added = {r[2]: r for r in stage1.table_rows(merged["teleportdata.txt"]) if r[1] in {"171", "172", "173", "174"}}
		self.assertEqual({code: row[variant.TELEPORT_WORLD_COLUMN] for code, row in added.items()},
			{code: "1" for code in self.CODES})
		self.assertEqual(summary["worldsFlattened"], list(self.CODES))
		# The destination region survives: the Temple's dungeon region 0x8010.
		self.assertEqual(added["GATE_TEMPLE_THIEF_GATE"][5], "-32752")

	def test_a_gate_not_named_in_flatten_keeps_its_authored_world(self):
		native, vsro = self.tables()
		merged, summary = variant.merge_gate_tables(native, vsro, codes=self.CODES, level_cap=110)
		added = {r[2]: r for r in stage1.table_rows(merged["teleportdata.txt"]) if r[1] == "173"}
		self.assertEqual(added["GATE_TEMPLE_MERCHANT_HUNTER_GATE"][variant.TELEPORT_WORLD_COLUMN], "60")
		self.assertNotIn("worldsFlattened", summary)

	def test_the_native_cap_clamps_the_entry_level_like_every_other_gate(self):
		native, vsro = self.tables()
		merged, _ = variant.merge_gate_tables(native, vsro, codes=self.CODES, level_cap=90, flatten=self.CODES)
		first = next(r for r in stage1.table_rows(merged["teleportlink.txt"]) if r[1:3] == ["171", "173"])
		self.assertEqual(first[6:9], ["1", "90", "999"])


# ================
# AddTempleTests
# ================
class AddTempleTests(unittest.TestCase):
	"""add_temple over a scratch native tree and variant tree."""

	def run_add_temple(self, flag, prior=None):
		"""Runs add_temple with the flag at flag; prior is the on call to run first, if any."""
		original_read, original_flag = variant.read_vsro_file, variant.os.environ.get(temple.FLAG)
		variant.read_vsro_file = lambda root, archive, name: VSRO_ZONES
		with tempfile.TemporaryDirectory() as scratch:
			native, out = Path(scratch) / "native", Path(scratch) / "out"
			info = native / "Data_extracted" / "dungeon" / "dungeoninfo.txt"
			zones = native / "Media_extracted" / variant.TEXTDATA / "eventzonedata.txt"
			for path, content in ((info, NATIVE_DUNGEON_INFO), (zones, NATIVE_ZONES)):
				path.parent.mkdir(parents=True)
				path.write_bytes(content)
			for dungeon in temple.DUNGEONS:
				dof = out / "Data_extracted" / temple.dof_path(dungeon)
				dof.parent.mkdir(parents=True, exist_ok=True)
				dof.write_bytes(b"JMXVDOF 0101")
			for relative in (Path("Data_extracted") / "dungeon" / "dungeoninfo.txt",
					Path("Media_extracted") / variant.TEXTDATA / "eventzonedata.txt"):
				(out / relative).parent.mkdir(parents=True, exist_ok=True)
				(out / relative).write_bytes((native / relative).read_bytes())
			try:
				if prior is not None:
					variant.os.environ[temple.FLAG] = prior
					temple.add_temple(Path(scratch) / "vsro", native, out)
				variant.os.environ[temple.FLAG] = flag
				summary = temple.add_temple(Path(scratch) / "vsro", native, out)
				result = (summary, (out / "Data_extracted" / "dungeon" / "dungeoninfo.txt").read_bytes(),
					(out / "Media_extracted" / variant.TEXTDATA / "eventzonedata.txt").read_bytes())
			finally:
				variant.read_vsro_file = original_read
				if original_flag is None:
					variant.os.environ.pop(temple.FLAG, None)
				else:
					variant.os.environ[temple.FLAG] = original_flag
		return result

	def test_off_leaves_both_tables_the_native_bytes(self):
		summary, info, zones = self.run_add_temple("off")
		self.assertEqual(summary, {"enabled": False})
		self.assertEqual(info, NATIVE_DUNGEON_INFO)
		self.assertEqual(zones, NATIVE_ZONES)

	def test_on_adds_exactly_the_seven_dungeons_and_two_zones(self):
		summary, info, zones = self.run_add_temple("on")
		self.assertEqual(summary["dungeons"], [10, 11, 12, 13, 14, 15, 16])
		self.assertEqual(summary["regions"], list(range(0x800A, 0x8011)))
		self.assertEqual(summary["eventZones"], list(temple.TEMPLE_ZONE_CODES))
		self.assertEqual(info, temple.merge_dungeon_info(NATIVE_DUNGEON_INFO))
		self.assertEqual(len(stage1.table_rows(zones)), len(stage1.table_rows(NATIVE_ZONES)) + 2)

	def test_off_after_on_puts_the_native_bytes_back(self):
		summary, info, zones = self.run_add_temple("off", prior="on")
		self.assertEqual(summary, {"enabled": False})
		self.assertEqual(info, NATIVE_DUNGEON_INFO)
		self.assertEqual(zones, NATIVE_ZONES)

	def test_on_twice_is_the_same_as_on_once(self):
		_, once_info, once_zones = self.run_add_temple("on")
		_, twice_info, twice_zones = self.run_add_temple("on", prior="on")
		self.assertEqual((once_info, once_zones), (twice_info, twice_zones))


if __name__ == "__main__":
	unittest.main()
