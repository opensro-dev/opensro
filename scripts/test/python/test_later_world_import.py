"""
===========================================================================
test_later_world_import.py - the shared later-world engine's reap.

One engine serves both later-world imports (jupiter_import,
god_import); their own suites pin the monster passes. What is new
with the engine is the region enrolment: a switch off must undo the
region merge a previous build enrolled in the marker - the files it
wrote, the availability bits it set, and the region rows it merged -
beside the monster outputs the imports always reaped.
===========================================================================
"""
import json
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import jupiter_import as jupiter  # noqa: E402
import later_world_import as later  # noqa: E402


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
	# test_off_without_a_prior_build_is_quiet
	# ================
	def test_off_without_a_prior_build_is_quiet(self):
		with tempfile.TemporaryDirectory() as root:
			base = Path(root)
			later.reap_world(jupiter.SPEC, base / "native", base / "out")
			self.assertFalse((base / "out" / jupiter.MARKER).exists())


if __name__ == "__main__":
	unittest.main()
