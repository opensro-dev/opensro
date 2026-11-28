"""
===========================================================================

test_later_fortress_import.py - the later siege fortresses' engine and specs

later_fortress_import.py places a fortress the v1.150 client never placed
(Constantinople from v1.696; Bandit and Hotan from v1.188, #3886). These
tests pin what the engine owns beyond the Constantinople suite: the off
path puts native bytes back, the staff's standings come and go,
the v1.188 name rows read in the shared shape, the resource walk admits
only the gaps the variant already ships, the three specs never share
a band, a placement file or a marker, the Bandit fortress ships with no
switch (#3586), and a published world-map page joins its tables and
leaves them again.

===========================================================================
"""

import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import later_tables  # noqa: E402
import dataclasses
import re
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import bandit_fortress_import as bandit  # noqa: E402
import constantinople_import as constantinople  # noqa: E402
import hotan_fortress_import as hotan  # noqa: E402
import later_fortress_import as fortress  # noqa: E402
import cap110_monsters as monsters  # noqa: E402
import import_vsro_alexandria as tables  # noqa: E402

SPECS = (constantinople.SPEC, bandit.SPEC, hotan.SPEC)

# structureZones.mjs PORT_ONLY_OBJECT_STRING: the bundle only merges a
# placement file whose name this pattern takes.
BUNDLE_PLACEMENT_NAME = re.compile(r"^objectstring\.(?:[a-z0-9_]+\.)?portonly\.ifo$")


class SpecsTest(unittest.TestCase):
	"""
	================
	SpecsTest

	Two fortresses sharing a band, a placement file or a marker would
	overwrite each other's rows or reap each other on the off path.
	================
	"""

	def test_no_two_fortresses_share_an_owned_file(self):
		for field in ("flag", "marker", "character_shard", "portonly_ifo", "fortress_code", "world"):
			values = [getattr(spec, field) for spec in SPECS]
			# Two switchless fortresses share no flag; the rest must differ.
			values = [value for value in values if value is not None]
			self.assertEqual(len(values), len(set(values)), field)

	def test_every_placement_file_is_one_the_bundle_merges(self):
		for spec in SPECS:
			self.assertRegex(spec.portonly_ifo, BUNDLE_PLACEMENT_NAME)

	def test_bands_are_the_ones_the_headers_state(self):
		self.assertEqual(bandit.CHARACTER_SHARD, "characterdata_290000.txt")
		self.assertEqual(hotan.CHARACTER_SHARD, "characterdata_300000.txt")

	def test_each_fortress_stages_its_own_staff(self):
		# The Bandit fortress shares Jangan's official (v1.188 stands one);
		# Hotan's official is its own. No staff code is staged twice.
		self.assertNotIn("NPC_CH_FORTRESS_OFFICIAL", bandit.SPEC.staff_codes)
		self.assertIn("NPC_KT_FORTRESS_OFFICIAL", hotan.SPEC.staff_codes)
		for spec in (bandit.SPEC, hotan.SPEC):
			self.assertEqual(len(spec.staff_codes), len(set(spec.staff_codes)))
			self.assertTrue(all("FORTRESS_" in code for code in spec.staff_codes))
		self.assertEqual(constantinople.SPEC.staff_codes, ())

	def test_model_fallbacks_name_backslash_paths(self):
		for spec in (bandit.SPEC, hotan.SPEC):
			for authored, shipped in spec.model_fallbacks:
				self.assertNotEqual(authored, shipped)
				for path in (authored, shipped):
					self.assertTrue(path.endswith(".bsr"), path)
					self.assertNotRegex(path, r"[\x00-\x1f]")
					self.assertIn("\\", path)

	def test_the_flagged_fortress_is_off_unless_on(self):
		self.assertFalse(fortress.enabled(hotan.SPEC, {}))
		self.assertFalse(fortress.enabled(hotan.SPEC, {hotan.SPEC.flag: "off"}))
		self.assertTrue(fortress.enabled(hotan.SPEC, {hotan.SPEC.flag: "on"}))

	def test_the_bandit_fortress_has_no_switch(self):
		# Later-native content (#3586): no flag, enabled under any
		# environment, including one that names the retired switch.
		self.assertIsNone(bandit.SPEC.flag)
		self.assertTrue(fortress.enabled(bandit.SPEC, {}))
		self.assertTrue(fortress.enabled(bandit.SPEC, {"SRO_FORT_BIJEOKDAN": "off"}))

	def test_a_missing_client_folder_stops_the_on_path(self):
		with self.assertRaises(SystemExit):
			bandit.vsro_root({})
		with self.assertRaises(SystemExit):
			hotan.vsro_root({})


class EngineTest(unittest.TestCase):
	"""
	================
	EngineTest
	================
	"""

	def test_resource_key_is_the_archive_index_name(self):
		self.assertEqual(fortress.resource_key(r"Bldg\guild\hotang\big_gate_ht.bsr"),
			"res/bldg/guild/hotang/big_gate_ht.bsr")

	def test_vsro_name_rows_carry_english_in_the_shared_column(self):
		def table(*rows):
			return b"\xff\xfe" + "".join("\t".join(row) + "\r\n" for row in rows).encode("utf-16-le")

		# VSRO's shape: textdataname.txt indexes the three name files,
		# one of them textdata_object.txt, which the reader also names.
		row = ["1", "SN_FORTRESS_HOTAN", "kr", "", "tw", "cn", "de", "jp", "Hotan Fortress", "vn"]
		short = ["1", "SN_SHORT", "kr"]
		blank = ["1", "", "kr", "", "", "", "", "", "Nameless", ""]
		# A name whose cell holds a line separator, as VSRO's Tausert does,
		# last in its file: the record stays one and the cell keeps it.
		wrapped = [["1", "SN_SKILL_FORT_BUFF", "kr", "", "tw", "cn", "de", "jp", "Fortress\u2028 Buff", "vn"]]
		texts = {
			"textdataname.txt": table(["TextData_Equip&Skill.txt"], ["TextData_Object.txt"], ["TextData_New.txt"]),
			"textdata_object.txt": table(row, short, blank),
			"textdata_equip&skill.txt": table(*wrapped),
			"textdata_new.txt": table(),
		}
		rows = fortress.vsro_object_texts(later_tables.Archive.of(texts))
		self.assertEqual(rows, [["1", "SN_FORTRESS_HOTAN", "kr", "", "", "", "", "", "Hotan Fortress", ""],
			["1", "SN_SKILL_FORT_BUFF", "kr", "", "", "", "", "", "Fortress\u2028 Buff", ""]])

	def test_staff_standings_take_the_native_id(self):
		spec = dataclasses.replace(hotan.SPEC, staff_codes=("NPC_KT_FORTRESS_OFFICIAL",))
		npcpos = [["9001", "24199", "154.38", "151.53", "811.03998"], ["9002", "1", "0", "0", "0"]]
		rows = fortress.staff_standings(spec, npcpos, {"NPC_KT_FORTRESS_OFFICIAL": "9001"},
			{"NPC_KT_FORTRESS_OFFICIAL": "22456"})
		self.assertEqual(rows, [["22456", "24199", "154.38", "151.53", "811.03998"]])
		with self.assertRaisesRegex(ValueError, "never places"):
			fortress.staff_standings(spec, npcpos[1:], {"NPC_KT_FORTRESS_OFFICIAL": "9001"},
				{"NPC_KT_FORTRESS_OFFICIAL": "22456"})
		with self.assertRaisesRegex(ValueError, "missing"):
			fortress.staff_standings(spec, npcpos, {}, {"NPC_KT_FORTRESS_OFFICIAL": "22456"})

	def test_restore_puts_native_bytes_back_and_drops_the_rest(self):
		with tempfile.TemporaryDirectory() as tmp:
			native, output = Path(tmp) / "native", Path(tmp) / "out"
			(native / "Map_extracted" / "67").mkdir(parents=True)
			(output / "Map_extracted" / "67").mkdir(parents=True)
			(native / "Map_extracted" / "67" / "77.m").write_bytes(b"native sector")
			(output / "Map_extracted" / "67" / "77.m").write_bytes(b"bandit sector")
			(output / "Map_extracted" / "67" / "77.o2").write_bytes(b"bandit only")
			fortress.restore_or_unlink(native, output, "Map_extracted/67/77.m")
			fortress.restore_or_unlink(native, output, "Map_extracted/67/77.o2")
			self.assertEqual((output / "Map_extracted" / "67" / "77.m").read_bytes(), b"native sector")
			self.assertFalse((output / "Map_extracted" / "67" / "77.o2").exists())

	def test_the_off_path_reaps_the_official_and_the_fortress(self):
		with tempfile.TemporaryDirectory() as tmp:
			native, output = Path(tmp) / "native", Path(tmp) / "out"
			native.mkdir()
			text_root = output / fortress.TEXT_ROOT
			text_root.mkdir(parents=True)
			kept = ["1", "100", "1.0", "2.0", "3.0"]
			staged = ["22456", "24199", "154.38", "151.53", "811.03998"]
			(text_root / "npcpos.txt").write_bytes(monsters.encode_rows([kept, staged]))
			(text_root / "siegefortress.txt").write_bytes(monsters.encode_rows(
				[["1", "1", "FORTRESS_JANGAN"], ["1", "3", "FORTRESS_HOTAN"]]))
			(text_root / hotan.CHARACTER_SHARD).write_bytes(b"rows")
			navmesh = output / "Data_extracted" / "navmesh"
			navmesh.mkdir(parents=True)
			(navmesh / hotan.PORTONLY_IFO).write_bytes(b"JMXVOBJI1000\n0\n")
			prior = {"fortressRow": True, "npcStandings": [staged], "files": []}
			self.assertEqual(fortress.remove_owned(hotan.SPEC, native, output, prior), {"enabled": False})
			self.assertEqual(tables.table_rows((text_root / "npcpos.txt").read_bytes()), [kept])
			self.assertEqual([row[2] for row in tables.table_rows((text_root / "siegefortress.txt").read_bytes())],
				["FORTRESS_JANGAN"])
			self.assertFalse((text_root / hotan.CHARACTER_SHARD).exists())
			self.assertFalse((navmesh / hotan.PORTONLY_IFO).exists())


def vsro_mapinfo_row(page, image, world):
	return [page, "1", "fortress map", image, "1024", "1024", "964", "1024", "964", "1024",
		"77", "71", "81", "67", "48", "0", "48", "-48", world, "SN_FORTRESS_X", "0", "xxx", ""]


def vsro_localinfo_row(pin, kind, icon, destination, page):
	return ["1", pin, kind, icon, "nation", "fortress", "page name", destination, page,
		"162", "93", "28", "16", "20", "32", "0", "0", "0", "0", "0", "1", "xxx"]


class MapPageTest(unittest.TestCase):
	"""
	================
	MapPageTest

	The world-map page a fortress publishes (#3586): the source page
	row trimmed to the native width, its icon rows converted without
	the source's service column, and the art those rows draw.
	================
	"""

	def read(self, pages, pins):
		sources = {
			"worldmap_mapinfo.txt": monsters.encode_rows(pages),
			"worldmap_localinfo.txt": monsters.encode_rows(pins),
		}
		return later_tables.Archive.of(sources)

	def page_sources(self):
		pages = [vsro_mapinfo_row("1001", r"interface\worldmap\map\map_fort_jangan.ddj", "INS_FORT_JA"),
			vsro_mapinfo_row("1002", r"interface\worldmap\map\map_fort_bijuckdan.ddj", "INS_FORT_BJ"),
			["1002"]]
		pins = [vsro_localinfo_row("91006", "2", r"interface\worldmap\map\fort_worldmap.ddj", "1001", "0"),
			vsro_localinfo_row("91009", "2", r"interface\worldmap\map\fort_small_worldmap.ddj", "1002", "0"),
			vsro_localinfo_row("95017", "2", r"icon\npc\fortress_manager.ddj", "0", "1002"),
			vsro_localinfo_row("95028", "2", r"icon\npc\fortress_manager.ddj", "0", "1003"),
			vsro_localinfo_row("99999", "1", "", "1002", "0"),
			["0"] + vsro_localinfo_row("95018", "2", r"icon\npc\fortress_battleaide.ddj", "0", "1002")[1:]]
		return pages, pins

	def test_the_page_and_its_icons_convert(self):
		pages, pins = fortress.map_page_rows(bandit.SPEC, self.read(*self.page_sources()))
		self.assertEqual([row[0] for row in pages], ["1002"])
		self.assertEqual(len(pages[0]), 19)
		self.assertEqual(pages[0][18], "INS_FORT_BJ")
		self.assertEqual([row[0] for row in pins], ["91009", "95017"])
		for row in pins:
			self.assertEqual(len(row), 20)
		self.assertEqual(pins[0][1], "2")

	def test_a_spec_without_a_page_publishes_nothing(self):
		self.assertEqual(fortress.map_page_rows(hotan.SPEC, self.read([], [])), ([], []))

	def test_a_page_the_source_lacks_is_refused(self):
		with self.assertRaisesRegex(ValueError, "worldmap_mapinfo"):
			fortress.map_page_rows(bandit.SPEC, self.read([], []))
		pages, pins = self.page_sources()
		with self.assertRaisesRegex(ValueError, "worldmap_localinfo"):
			fortress.map_page_rows(bandit.SPEC, self.read(pages, pins[:1]))

	def test_the_art_is_the_picture_the_icons_and_the_crest(self):
		pages, pins = fortress.map_page_rows(bandit.SPEC, self.read(*self.page_sources()))
		siege = ["1", "6", "FORTRESS_BIJEOKDAN", "kr", "SN_FORTRESS_BIJEOKDAN", "GATE_CH",
			"1", "200", "15", "20", "96", "500000", r"etc\fort_bijeokdan.ddj", "NPC_CH_FORTRESS_OFFICIAL"]
		self.assertEqual(fortress.map_art_paths(pages, pins, siege), [
			"icon/etc/fort_bijeokdan.ddj", "icon/npc/fortress_manager.ddj",
			"interface/worldmap/map/fort_small_worldmap.ddj",
			"interface/worldmap/map/map_fort_bijuckdan.ddj"])

	def test_the_off_path_reaps_the_page_and_its_icons(self):
		with tempfile.TemporaryDirectory() as tmp:
			native, output = Path(tmp) / "native", Path(tmp) / "out"
			native.mkdir()
			text_root = output / fortress.TEXT_ROOT
			text_root.mkdir(parents=True)
			(text_root / "worldmap_mapinfo.txt").write_bytes(monsters.encode_rows(
				[["1001", "1"], ["1002", "1"]]))
			(text_root / "worldmap_localinfo.txt").write_bytes(monsters.encode_rows(
				[["91006", "2"], ["91009", "2"]]))
			prior = {"mapPageIds": [1002], "localInfoIds": [91009], "files": []}
			fortress.remove_owned(hotan.SPEC, native, output, prior)
			self.assertEqual([row[0] for row in tables.table_rows(
				(text_root / "worldmap_mapinfo.txt").read_bytes())], ["1001"])
			self.assertEqual([row[0] for row in tables.table_rows(
				(text_root / "worldmap_localinfo.txt").read_bytes())], ["91006"])


class ShippedClosureTest(unittest.TestCase):
	"""
	================
	ShippedClosureTest

	The v1.188 sectors place models the dependency reader cannot parse;
	the walk admits them only when the variant already ships them.
	================
	"""

	def run_closure(self, result, native_files=(), output_files=()):
		with tempfile.TemporaryDirectory() as tmp:
			native, output = Path(tmp) / "native", Path(tmp) / "out"
			for root, names in ((native, native_files), (output, output_files)):
				for name in names:
					path = root / "Data_extracted" / name
					path.parent.mkdir(parents=True, exist_ok=True)
					path.write_bytes(b"x")
			with mock.patch.object(fortress.sky, "copy_resource_closure", return_value=result) as walk:
				out = fortress.shipped_closure([], {}, native, output)
			self.assertTrue(walk.call_args.kwargs["tolerate_gaps"])
			return out

	def test_a_missing_file_stops(self):
		with self.assertRaisesRegex(ValueError, "missing from every source"):
			self.run_closure({"files": [], "missingFiles": ["Data:res/a.bsr"], "unparsedFiles": []})

	def test_native_and_stage_one_files_pass(self):
		result = {"files": [], "missingFiles": [], "unparsedFiles": ["res/tree/bamboo.bsr", "res/tree/palm.bsr"]}
		self.assertIs(self.run_closure(result, native_files=["res/tree/bamboo.bsr"],
			output_files=["res/tree/palm.bsr"]), result)

	def test_an_unparsable_file_this_walk_brought_stops(self):
		result = {"files": ["Data_extracted/res/new.bsr"], "missingFiles": [], "unparsedFiles": ["res/new.bsr"]}
		with self.assertRaisesRegex(ValueError, "cannot parse: res/new.bsr"):
			self.run_closure(result, output_files=["res/new.bsr"])


if __name__ == "__main__":
	unittest.main()
