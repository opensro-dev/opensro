"""
===========================================================================
test_later_world_import.py - the shared later-world engine's reap.

One engine serves the later-world imports: jupiter_import and
god_import whole, muhan_import and hunting2_import for the region
merge under their own monster passes; their own suites pin those
passes. What is new with the engine is the region enrolment: a
switch off must undo the region merge a previous build enrolled in
the marker - the files it wrote, the availability bits it set, and
the region rows it merged - beside the monster outputs the imports
always reaped. The self-driven worlds enter that reap through
their public add functions, pinned below.
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import json
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import hunting2_import as hunting2  # noqa: E402
import jupiter_import as jupiter  # noqa: E402
import later_world_import as later  # noqa: E402
import muhan_import as muhan  # noqa: E402


# ================
# utf16
# ================
def utf16(rows):
	return b"\xff\xfe" + ("\r\n".join("\t".join(row) for row in rows) + "\r\n").encode("utf-16-le")


# ================
# LaterWorldReapTest
# ================
class LaterWorldReapTest(unittest.TestCase):
	# ================
	# fixture
	#
	# A native textdata (a placeholder region row, one native region,
	# one native zone) and a variant the Jupiter merge has been into:
	# the placeholder replaced by the later code row, the region's
	# zone rows joined, the enrolled files written, one availability
	# bit set - and one bit of the tree's own that must survive.
	# ================
	def fixture(self, base):
		native, out = base / "native", base / "out"
		native_tables = native / jupiter.TEXT_ROOT
		native_tables.mkdir(parents=True)
		(native_tables / "regioncode.txt").write_bytes(utf16([
			["0", "21707", "xxx", "0"], ["0", "12345", "NATIVE_CODE", "0"]]))
		(native_tables / "textzonename.txt").write_bytes(utf16([
			["0", "NATIVE_ZONE"] + ["x"] * 8]))
		text_root = out / jupiter.TEXT_ROOT
		text_root.mkdir(parents=True)
		(text_root / "regioncode.txt").write_bytes(utf16([
			["0", "12345", "NATIVE_CODE", "0"], ["0", "21707", "JUPITER_FIELD", "0"]]))
		(text_root / "textzonename.txt").write_bytes(utf16([
			["0", "NATIVE_ZONE"] + ["x"] * 8,
			["0", "21707"] + ["x"] * 8,
			["0", "JUPITER_FIELD_EAST"] + ["x"] * 8]))
		(text_root / "skilleffect.txt").write_bytes(utf16([
			["#section", "characterinfo"], ["MOB_NATIVE"] + ["0"] * 12, ["#section", "skillaniset"]]))
		(text_root / "textdataname.txt").write_bytes(utf16([["1", "UIO_OBJ_MOB_NATIVE", "Native"]]))
		(text_root / "skilldata.txt").write_bytes(utf16([["skilldata_5000.txt"]]))
		(text_root / jupiter.CHARACTER_SHARD).write_bytes(utf16([]))
		for relative in ("Map_extracted/85/129.m", "Data_extracted/navmesh/nv_55f1.nvm",
				"Media_extracted/minimap/129x85.ddj", "Map_extracted/keep.txt"):
			target = out / relative
			target.parent.mkdir(parents=True, exist_ok=True)
			target.write_bytes(b"x")
		mapinfo = bytearray(b"JMXVMFO 1000" + bytes(12) + bytes(8192))

		def set_bit(sector_x, sector_y):
			region_id = (sector_y << 8) | sector_x
			mapinfo[24 + (region_id >> 3)] |= 0x80 >> (region_id & 7)

		set_bit(129, 85)
		set_bit(1, 1)
		(out / "Data_extracted" / "navmesh" / "mapinfo.mfo").write_bytes(bytes(mapinfo))
		(out / jupiter.MARKER).write_text(json.dumps({
			"characters": {},
			"regionMerge": {
				"files": ["map_extracted/85/129.m", "data_extracted/navmesh/nv_55f1.nvm",
					"media_extracted/minimap/129x85.ddj"],
				"sectors": [[129, 85]],
				"regions": [21707],
				"prefixes": ["JUPITER_FIELD_"],
			},
		}), encoding="utf-8")
		return native, out, text_root

	# ================
	# test_off_reaps_the_enrolled_region_merge
	# ================
	def test_off_reaps_the_enrolled_region_merge(self):
		with tempfile.TemporaryDirectory() as root:
			native, out, text_root = self.fixture(Path(root))
			later.reap_world(jupiter.SPEC, native, out)
			for relative in ("Map_extracted/85/129.m", "Data_extracted/navmesh/nv_55f1.nvm",
					"Media_extracted/minimap/129x85.ddj", jupiter.CHARACTER_SHARD):
				self.assertFalse((out / relative).exists() if relative != jupiter.CHARACTER_SHARD
					else (text_root / relative).exists())
			self.assertTrue((out / "Map_extracted" / "keep.txt").exists())
			self.assertFalse((out / jupiter.MARKER).exists())
			mapinfo = (out / "Data_extracted" / "navmesh" / "mapinfo.mfo").read_bytes()

			def available(sector_x, sector_y):
				region_id = (sector_y << 8) | sector_x
				return bool(mapinfo[24 + (region_id >> 3)] & (0x80 >> (region_id & 7)))

			self.assertFalse(available(129, 85))
			self.assertTrue(available(1, 1))
			regioncode = (text_root / "regioncode.txt").read_bytes().decode("utf-16")
			self.assertIn("xxx", regioncode)
			self.assertIn("NATIVE_CODE", regioncode)
			self.assertNotIn("JUPITER_FIELD", regioncode)
			zonename = (text_root / "textzonename.txt").read_bytes().decode("utf-16")
			self.assertIn("NATIVE_ZONE", zonename)
			self.assertNotIn("21707", zonename)
			self.assertNotIn("JUPITER_FIELD_EAST", zonename)
			# A second off build has nothing enrolled and stays quiet.
			later.reap_world(jupiter.SPEC, native, out)

	# ================
	# test_off_keeps_a_sector_native_already_enables
	#
	# An export region that is also a native sector: the merge set its
	# availability bit again, so the reap must not clear it.
	# ================
	def test_off_keeps_a_sector_native_already_enables(self):
		with tempfile.TemporaryDirectory() as root:
			native, out, _ = self.fixture(Path(root))
			native_map = bytearray(b"JMXVMFO 1000" + bytes(12) + bytes(8192))
			region_id = (85 << 8) | 129
			native_map[24 + (region_id >> 3)] |= 0x80 >> (region_id & 7)
			(native / "Data_extracted" / "navmesh").mkdir(parents=True)
			(native / "Data_extracted" / "navmesh" / "mapinfo.mfo").write_bytes(bytes(native_map))
			later.reap_world(jupiter.SPEC, native, out)
			mapinfo = (out / "Data_extracted" / "navmesh" / "mapinfo.mfo").read_bytes()
			self.assertTrue(mapinfo[24 + (region_id >> 3)] & (0x80 >> (region_id & 7)))

	# ================
	# test_off_refuses_an_enrolled_path_outside_the_overlay
	# ================
	def test_off_refuses_an_enrolled_path_outside_the_overlay(self):
		with tempfile.TemporaryDirectory() as root:
			native, out, _ = self.fixture(Path(root))
			marker = json.loads((out / jupiter.MARKER).read_text(encoding="utf-8"))
			marker["regionMerge"]["files"] = ["../native/keep.txt"]
			(out / jupiter.MARKER).write_text(json.dumps(marker), encoding="utf-8")
			(native / "keep.txt").write_bytes(b"x")
			with self.assertRaises(ValueError):
				later.reap_world(jupiter.SPEC, native, out)
			self.assertTrue((native / "keep.txt").exists())

	# ================
	# test_off_without_a_prior_build_is_quiet
	# ================
	def test_off_without_a_prior_build_is_quiet(self):
		with tempfile.TemporaryDirectory() as root:
			base = Path(root)
			later.reap_world(jupiter.SPEC, base / "native", base / "out")
			self.assertFalse((base / "out" / jupiter.MARKER).exists())

	# ================
	# enrolled_fixture
	#
	# One self-driven world's post-merge tree, fabricated as
	# merge_world would have left it: the region row replaced, the
	# zone rows joined, the enrolled sector file written, its
	# availability bit set, and the world's record in its marker.
	# ================
	def enrolled_fixture(self, base, module, region_id, sector, code, zone):
		native, out = base / "native", base / "out"
		native_tables = native / module.TEXT_ROOT
		native_tables.mkdir(parents=True)
		(native_tables / "regioncode.txt").write_bytes(utf16([
			["0", str(region_id), "xxx", "0"], ["0", "12345", "NATIVE_CODE", "0"]]))
		(native_tables / "textzonename.txt").write_bytes(utf16([
			["0", "NATIVE_ZONE"] + ["x"] * 8]))
		text_root = out / module.TEXT_ROOT
		text_root.mkdir(parents=True)
		(text_root / "regioncode.txt").write_bytes(utf16([
			["0", "12345", "NATIVE_CODE", "0"], ["0", str(region_id), code, "0"]]))
		(text_root / "textzonename.txt").write_bytes(utf16([
			["0", "NATIVE_ZONE"] + ["x"] * 8,
			["0", str(region_id)] + ["x"] * 8,
			["0", zone] + ["x"] * 8]))
		(text_root / module.CHARACTER_SHARD).write_bytes(utf16([]))
		(text_root / "skilleffect.txt").write_bytes(utf16([
			["#section", "characterinfo"], ["MOB_NATIVE"] + ["0"] * 12, ["#section", "skillaniset"]]))
		(text_root / "textdataname.txt").write_bytes(utf16([["1", "UIO_OBJ_MOB_NATIVE", "Native"]]))
		(text_root / "skilldata.txt").write_bytes(utf16([["skilldata_5000.txt"]]))
		sector_relative = f"Map_extracted/{sector[1]}/{sector[0]}.m"
		for relative in (sector_relative, "Map_extracted/keep.txt"):
			target = out / relative
			target.parent.mkdir(parents=True, exist_ok=True)
			target.write_bytes(b"x")
		mapinfo = bytearray(b"JMXVMFO 1000" + bytes(12) + bytes(8192))
		region_key = (sector[1] << 8) | sector[0]
		mapinfo[24 + (region_key >> 3)] |= 0x80 >> (region_key & 7)
		nav_dir = out / "Data_extracted" / "navmesh"
		nav_dir.mkdir(parents=True, exist_ok=True)
		(nav_dir / "mapinfo.mfo").write_bytes(bytes(mapinfo))
		(out / module.MARKER).write_text(json.dumps({
			"regionMerge": {
				"files": [sector_relative.lower()],
				"sectors": [[sector[0], sector[1]]],
				"regions": [region_id],
				"prefixes": [code + "_"],
			},
		}), encoding="utf-8")
		return native, out, text_root, sector_relative, region_key

	# ================
	# check_off_reaps_the_region_merge
	#
	# The #3326 promise at a self-driven world's own door: a switch
	# off through its public add function undoes the enrolled region
	# merge, not only the monster outputs, and a second off build
	# stays quiet. Before the move onto the engine the off path ran
	# the monster reap alone and every assertion on the region state
	# below failed.
	# ================
	def check_off_reaps_the_region_merge(self, module, flag, entry, region_id, sector, code, zone):
		with tempfile.TemporaryDirectory() as root:
			native, out, text_root, sector_relative, region_key = self.enrolled_fixture(
				Path(root), module, region_id, sector, code, zone)
			with mock.patch.dict(os.environ, {flag: ""}):
				self.assertEqual(entry(native, out), {"enabled": False})
			self.assertFalse((out / sector_relative).exists())
			self.assertTrue((out / "Map_extracted" / "keep.txt").exists())
			self.assertFalse((text_root / module.CHARACTER_SHARD).exists())
			self.assertFalse((out / module.MARKER).exists())
			mapinfo = (out / "Data_extracted" / "navmesh" / "mapinfo.mfo").read_bytes()
			self.assertFalse(mapinfo[24 + (region_key >> 3)] & (0x80 >> (region_key & 7)))
			regioncode = (text_root / "regioncode.txt").read_bytes().decode("utf-16")
			self.assertIn("xxx", regioncode)
			self.assertIn("NATIVE_CODE", regioncode)
			self.assertNotIn(code, regioncode)
			zonename = (text_root / "textzonename.txt").read_bytes().decode("utf-16")
			self.assertIn("NATIVE_ZONE", zonename)
			self.assertNotIn(str(region_id), zonename)
			with mock.patch.dict(os.environ, {flag: ""}):
				self.assertEqual(entry(native, out), {"enabled": False})

	# ================
	# test_muhan_off_reaps_the_region_merge
	# ================
	def test_muhan_off_reaps_the_region_merge(self):
		self.check_off_reaps_the_region_merge(
			muhan, "SRO_MUHAN", muhan.add_muhan, 25001, (129, 85), "MUHAN_FIELD", "MUHAN_FIELD_EAST")

	# ================
	# test_hunting2_off_reaps_the_region_merge
	# ================
	def test_hunting2_off_reaps_the_region_merge(self):
		self.check_off_reaps_the_region_merge(
			hunting2, "SRO_HUNTING2", hunting2.add_hunting2, 25002, (130, 86), "HUNT_FIELD", "HUNT_FIELD_EAST")


# ================
# pk2
#
# A test archive holding files at nested paths ({"a/b.txt": bytes}).
# ================
def pk2(files):
	from test_client_preparation import ArchiveBuilder
	builder = ArchiveBuilder()
	tree = {}
	for path, raw in files.items():
		node = tree
		*folders, name = path.split("/")
		for folder in folders:
			node = node.setdefault(folder, {})
		node[name] = raw

	def emit(node):
		offset = builder.folder([])
		entries = []
		for name, child in node.items():
			if isinstance(child, dict):
				entries.append((1, name.encode("ascii"), emit(child), 0))
			else:
				entries.append((2, name.encode("ascii"), *builder.add_payload(child)))
		builder.blocks[offset] = entries
		return offset

	emit(tree)
	return builder.build()


# ================
# MergeWorldParticleTest
#
# merge_world itself runs the particle walk: a model its Data pass
# copies seeds the walk, the walk's file lands in the overlay, and the
# region record enrols it beside the model. Only the resource walk is
# stood in for; the archives, the seeds and the enrolment are real.
# ================
class MergeWorldParticleTest(unittest.TestCase):
	MODEL = "res/mob/jupiter/babilion.bsr"
	EFFECT = "monster/jupiter/skill_jupiter_babilion_at11_body.efp"

	def test_a_merged_model_carries_its_effect_program_into_the_record(self):
		import skytemple_import
		textdata = "server_dep/silkroad/textdata/"
		# One native row each: the merges number new rows past the highest.
		catalog = b'JMXV2DTI1001\r\n1\r\n00000 0x00000000 "ok" "native.ddj"\r\n'
		with tempfile.TemporaryDirectory() as temp:
			base = Path(temp)
			v1696, native, out = base / "v1696", base / "native", (base / "out").resolve()
			v1696.mkdir()
			(v1696 / "Map.pk2").write_bytes(pk2({"tile2d.ifo": catalog, "object.ifo": catalog}))
			(v1696 / "Data.pk2").write_bytes(pk2({self.MODEL: b"JMXVBSR 0109"}))
			(v1696 / "Media.pk2").write_bytes(pk2({textdata + "regioncode.txt": utf16([]),
				textdata + "textzonename.txt": utf16([])}))
			(v1696 / "Particles.pk2").write_bytes(pk2({self.EFFECT: b"JMXVEFF"}))
			for folder in ("Data_extracted", "Media_extracted", "Particles_extracted"):
				(native / folder).mkdir(parents=True)
			for name in ("tile2d.ifo", "object.ifo"):
				target = out / "Map_extracted" / name
				target.parent.mkdir(parents=True, exist_ok=True)
				target.write_bytes(catalog)
			mapinfo = out / "Data_extracted" / "navmesh" / "mapinfo.mfo"
			mapinfo.parent.mkdir(parents=True)
			mapinfo.write_bytes(b"JMXVMFO 1000" + bytes(12) + bytes(8192))
			text_root = out / jupiter.TEXT_ROOT
			text_root.mkdir(parents=True)
			for name in ("regioncode.txt", "textzonename.txt"):
				(text_root / name).write_bytes(utf16([]))
			walks = []

			def walk(seeds, archives, walk_native, output, tolerate_gaps=False):
				# The real walk parses the model and finds this program in
				# the Particles archive; the archive read stays real.
				data, index = archives["Particles"]
				walks.append((seeds, sorted(archives), tolerate_gaps))
				target = Path(output) / "Particles_extracted" / self.EFFECT
				target.parent.mkdir(parents=True, exist_ok=True)
				target.write_bytes(later.sro_pk2.payload(data, index[self.EFFECT]))
				return {"files": ["Particles_extracted/" + self.EFFECT], "missingEffects": [],
					"missingFiles": [], "unparsedFiles": []}

			def monsters():
				(out / jupiter.MARKER).write_text("{}", encoding="utf-8")
				return {"enabled": True}

			with mock.patch.object(skytemple_import, "copy_resource_closure", walk):
				result = later.merge_world(jupiter.SPEC, native, out, v1696, [], monsters)
			self.assertEqual(walks, [([self.MODEL], ["Data", "Particles"], True)])
			self.assertEqual((out / "Particles_extracted" / self.EFFECT).read_bytes(), b"JMXVEFF")
			self.assertEqual(result["particleFilesAdded"], 1)
			record = json.loads((out / jupiter.MARKER).read_text(encoding="utf-8"))["regionMerge"]
			self.assertIn("data_extracted/" + self.MODEL, record["files"])
			self.assertIn("particles_extracted/" + self.EFFECT, record["files"])


# ================
# ParticleClosureTest
#
# The region merge copies the v1.696 models the native tree lacks; their
# particle sets name effect programs the client's effect build refuses to
# miss (r196: ten .efp files of merged Jupiter, Arabia and event models).
# The merge walks every enrolled model and compound object through the
# shared closure, over the Data and Particles archives, and enrols what
# the walk writes so the switch-off reap removes it.
# ================
class ParticleClosureTest(unittest.TestCase):
	ENROLLED = {
		"data_extracted/res/mob/jupiter/babilion.bsr",
		"data_extracted/res/cos/p_ghost.bsr",
		"data_extracted/res/dun/jupiter/hide/hide01_boss.cpd",
		"data_extracted/prim/mesh/mob/jupiter/babilion.bms",
		"map_extracted/1/2.m",
		"media_extracted/minimap/1x2.ddj",
	}

	# ================
	# fake_archives
	#
	# Stands in for the v1.696 archive pair and records that it was
	# opened and closed.
	# ================
	def fake_archives(self, log):
		import contextlib

		@contextlib.contextmanager
		def opened(root):
			log.append(("open", root))
			try:
				yield {"Data": ("data", {}), "Particles": ("particles", {})}
			finally:
				log.append(("close", root))
		return opened

	# ================
	# test_every_enrolled_model_seeds_the_walk_over_both_archives
	# ================
	def test_every_enrolled_model_seeds_the_walk_over_both_archives(self):
		calls, log = [], []

		def closure(seeds, archives, native, output, tolerate_gaps=False):
			calls.append((seeds, sorted(archives), native, output, tolerate_gaps))
			return {"files": ["Particles_extracted/monster/jupiter/skill_jupiter_babilion_at11_body.efp",
				"Particles_extracted/system/event_summer_ghost_pick.efp"],
				"missingEffects": ["monster/system_appear.efp"], "missingFiles": [], "unparsedFiles": []}

		result = later.particle_closure("native", "out", "v1696", self.ENROLLED, closure, self.fake_archives(log))
		self.assertEqual(calls, [(["res/cos/p_ghost.bsr", "res/dun/jupiter/hide/hide01_boss.cpd",
			"res/mob/jupiter/babilion.bsr"], ["Data", "Particles"], "native", "out", True)])
		self.assertEqual(log, [("open", "v1696"), ("close", "v1696")])
		# The record's form: lower-case and overlay-relative, so
		# reap_regions unlinks them with the Data files on a switch off.
		self.assertEqual(result["added"], [
			"particles_extracted/monster/jupiter/skill_jupiter_babilion_at11_body.efp",
			"particles_extracted/system/event_summer_ghost_pick.efp"])
		self.assertEqual(result["gaps"], {"missingEffects": ["monster/system_appear.efp"]})

	# ================
	# test_a_merge_without_models_opens_no_archive
	# ================
	def test_a_merge_without_models_opens_no_archive(self):
		log = []

		def closure(*_args, **_kwargs):
			raise AssertionError("no seed, no walk")

		result = later.particle_closure("native", "out", "v1696", {"map_extracted/1/2.m"}, closure,
			self.fake_archives(log))
		self.assertEqual(result, {"added": [], "gaps": {}})
		self.assertEqual(log, [])

	# ================
	# test_the_reap_removes_the_enrolled_particle_files
	# ================
	def test_the_reap_removes_the_enrolled_particle_files(self):
		with tempfile.TemporaryDirectory() as temp:
			base = Path(temp)
			native, out = base / "native", (base / "out").resolve()
			native.mkdir()
			effect = out / "Particles_extracted" / "system" / "event_summer_ghost_pick.efp"
			effect.parent.mkdir(parents=True)
			effect.write_bytes(b"JMXVEFF")
			later.reap_regions(jupiter.SPEC, native, out,
				{"files": ["particles_extracted/system/event_summer_ghost_pick.efp"]})
			self.assertFalse(effect.exists())

	# ================
	# test_the_self_driven_merges_pin_the_particles_archive
	#
	# The walk reads Particles.pk2, so the set each caller verifies
	# before merge_world runs must name it.
	# ================
	def test_the_self_driven_merges_pin_the_particles_archive(self):
		self.assertIn("Particles", later.MERGE_ARCHIVES)
		cases = ((hunting2, "SRO_HUNTING2", lambda: hunting2.add_hunting2("native", "out", "v1696")),
			(muhan, "SRO_MUHAN", lambda: muhan.add_muhan("native", "out", "v1696", "exports")))
		for module, flag, run in cases:
			with self.subTest(flag=flag), mock.patch.dict(os.environ, {flag: "on"}), \
					mock.patch.object(module, "require_v1696_pin", side_effect=SystemExit("pinned")) as pin:
				with self.assertRaisesRegex(SystemExit, "pinned"):
					run()
				self.assertEqual(pin.call_args.args[1], later.MERGE_ARCHIVES)



# ================
# ZoneNameMergeTest
#
# v1.696 keeps textzonename.txt as an index of TextZoneName_<n>.txt
# shards whose rows carry a text id after the service cell. The merge
# read the index as the table and joined no name, so Arabia and Jupiter
# shipped with empty minimap titles (2026-11-17).
# ================
class ZoneNameMergeTest(unittest.TestCase):
	def later_row(self, serial, key, english):
		return ["1", serial, key, "Korean", "0", "0", "0", "0", "0", english, "0", "0", "0",
			english, english, english, english]

	def test_the_merge_reads_the_sharded_v1696_table(self):
		native = {
			"regioncode.txt": utf16([["1", "20054", "xxx", "0"]]),
			"textzonename.txt": utf16([["1", "NATIVE_ZONE"] + ["x"] * 9]),
		}
		sharded = {
			"regioncode.txt": utf16([["1", "20054", "RN_ARABIA_FIELD_03", "0"]]),
			"textzonename.txt": utf16([["TextZoneName_200.txt"], ["TextZoneName_3000.txt"]]),
			"textzonename_200.txt": utf16([self.later_row("10", "99999", "Elsewhere")]),
			"textzonename_3000.txt": utf16([
				self.later_row("3025", "20054", "Arabia Coast"),
				self.later_row("3810", "RN_ARABIA_FIELD_03_01", "Arabian Sea"),
			]),
		}
		merged, summary, _prefixes = later.merge_region_tables_record(native, sharded, {20054})
		names = {row[1]: row[8] for row in later.tables.table_rows(merged["textzonename.txt"]) if len(row) > 8}
		self.assertEqual(names["20054"], "Arabia Coast")
		self.assertEqual(names["RN_ARABIA_FIELD_03_01"], "Arabian Sea")
		self.assertNotIn("99999", names)
		self.assertNotIn("3025", names)
		self.assertEqual(summary["zoneNames"], 2)

	def test_a_single_vsro_table_still_reads_as_the_table(self):
		native = {"regioncode.txt": utf16([["1", "20054", "xxx", "0"]]), "textzonename.txt": utf16([])}
		vsro = {
			"regioncode.txt": utf16([["1", "20054", "RN_ARABIA_FIELD_03", "0"]]),
			"textzonename.txt": utf16([["1", "20054", "Korean", "", "x", "x", "x", "x", "Arabia Coast", "x"]]),
		}
		merged, _summary, _prefixes = later.merge_region_tables_record(native, vsro, {20054})
		names = {row[1]: row[8] for row in later.tables.table_rows(merged["textzonename.txt"]) if len(row) > 8}
		self.assertEqual(names["20054"], "Arabia Coast")


# ================
# ClientRootTest
#
# The one resolver of the v1.696 folder: an empty or unset value refuses
# with the switch's own sentence, never "." (str(Path("")) is ".").
# ================
class ClientRootTest(unittest.TestCase):
	def test_unset_and_empty_refuse(self):
		for env in ({}, {"SRO_V1696_ROOT": ""}, {"SRO_V1696_ROOT": "  "}):
			with self.assertRaisesRegex(SystemExit, "SRO_V1696_ROOT"):
				later.client_root(None, "SRO_V1696_ROOT", env)
		with self.assertRaisesRegex(SystemExit, "SRO_V1696_ROOT"):
			later.client_root("", "SRO_V1696_ROOT", {})

	def test_the_caller_wins_over_the_environment(self):
		env = {"SRO_V1696_ROOT": "from-env"}
		self.assertEqual(later.client_root("given", "SRO_V1696_ROOT", env), Path("given"))
		self.assertEqual(later.client_root(None, "SRO_V1696_ROOT", env), Path("from-env"))


# ================
# MissingExportTest
#
# A later world switched on without its population export refuses by
# the switch and the variable, before any archive or pin is read, and
# says how to build without it: a contributor's clone has no export.
# ================
class MissingExportTest(unittest.TestCase):
	def assert_names_the_way_out(self, refusal, flag, env):
		message = str(refusal.exception)
		self.assertIn(f"{flag} is on, but {env} is not set", message)
		self.assertIn("not in the repository or a public clone", message)
		self.assertIn(f"turn {flag} off in apps/server/config/shard-settings.json", message)

	def test_a_nest_driven_world_refuses_before_its_pins(self):
		spec = jupiter.SPEC
		env = {key: value for key, value in os.environ.items() if key != spec.source_env}
		env[spec.flag] = "1"
		if spec.requires:
			env[spec.requires] = "1"
		pins = mock.Mock()
		with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(os.environ, env, clear=True):
			with self.assertRaises(SystemExit) as refusal:
				later.add_world(spec, Path(tmp) / "native", Path(tmp) / "out", "v1696", None, False, pins)
		self.assert_names_the_way_out(refusal, spec.flag, spec.source_env)
		pins.assert_not_called()

	def test_muhan_refuses_before_its_pins(self):
		env = {key: value for key, value in os.environ.items() if key != muhan.SOURCE_ENV}
		env[muhan.FLAG] = "1"
		with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(os.environ, env, clear=True), 				mock.patch.object(muhan, "require_v1696_pin") as pin:
			with self.assertRaises(SystemExit) as refusal:
				muhan.add_muhan(Path(tmp) / "native", Path(tmp) / "out", "v1696")
		self.assert_names_the_way_out(refusal, muhan.FLAG, muhan.SOURCE_ENV)
		pin.assert_not_called()



# ================
# MapWorldTest
#
# The map-only path (add_map_world): a world with no population owns
# only its spec and its region rule; the engine owns the switch, the
# pin, the Media read and the empty monster pass.
# ================
class MapWorldTest(unittest.TestCase):
	SPEC = later.WorldImport(label="mapworld", flag="SRO_TEST_MAP_WORLD", root_env="SRO_TEST_MAP_ROOT",
		character_shard="characterdata_990000.txt", skill_shard="skilldata_990000.txt",
		marker=".opensro-mapworld.json", pin_file=Path("pins.json"), pin_sentence="test",
		region_label="Map world", population_files=())

	def test_the_marker_keeps_only_the_region_record(self):
		with tempfile.TemporaryDirectory() as temp:
			marker = Path(temp) / self.SPEC.marker
			marker.write_text(json.dumps({"regionMerge": {"regions": [7]}, "stale": 1}))
			self.assertEqual(later.write_map_marker(self.SPEC, temp), {"enabled": True, "characters": 0})
			self.assertEqual(json.loads(marker.read_text()), {"regionMerge": {"regions": [7]}, "enabled": True})

	def test_off_reaps_and_reads_no_archive(self):
		with tempfile.TemporaryDirectory() as temp:
			native, out = Path(temp) / "native", Path(temp) / "out"
			with mock.patch.dict(os.environ, {self.SPEC.flag: ""}), 				mock.patch.object(later, "reap_world") as reap, 				mock.patch.object(later, "require_pin") as pin, 				mock.patch.object(later.tables, "open_archive") as opened, 				mock.patch.object(later, "merge_world") as merge:
				self.assertEqual(later.add_map_world(self.SPEC, native, out, temp, lambda rows_of: [1]),
					{"enabled": False})
			reap.assert_called_once_with(self.SPEC, native, out)
			pin.assert_not_called()
			opened.assert_not_called()
			merge.assert_not_called()

	def test_on_pins_before_reading_then_merges_the_rule_s_regions(self):
		order = []
		with tempfile.TemporaryDirectory() as temp:
			media, handle = mock.MagicMock(), mock.MagicMock()
			with mock.patch.dict(os.environ, {self.SPEC.flag: "on"}), 				mock.patch.object(later, "require_pin", side_effect=lambda *a: order.append("pin")) as pin, 				mock.patch.object(later.tables, "open_archive",
					side_effect=lambda *a: order.append("open") or (None, handle, media)), 				mock.patch.object(later.tables, "archive_index", return_value={}), 				mock.patch.object(later.tables, "table_text", return_value="text"), 				mock.patch.object(later.tables, "table_rows", side_effect=lambda text: [["row", text]]), 				mock.patch.object(later, "merge_world", return_value={"enabled": True}) as merge:
				seen = []

				def rule(rows_of):
					seen.append(rows_of("teleportdata.txt"))
					return [25580]
				self.assertEqual(later.add_map_world(self.SPEC, "n", "o", temp, rule), {"enabled": True})
			self.assertEqual(order, ["pin", "open"])
			self.assertEqual(pin.call_args.args[2], later.MERGE_ARCHIVES)
			self.assertEqual(seen, [[["row", "text"]]])
			media.close.assert_called_once()
			handle.close.assert_called_once()
			spec, _native, _out, root, regions, _monsters = merge.call_args.args
			self.assertIs(spec, self.SPEC)
			self.assertEqual((root, regions), (Path(temp), [25580]))


if __name__ == "__main__":
	unittest.main()
