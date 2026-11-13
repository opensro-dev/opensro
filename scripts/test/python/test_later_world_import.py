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
import json
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
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


if __name__ == "__main__":
	unittest.main()
