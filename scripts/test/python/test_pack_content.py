"""
===========================================================================

test_pack_content.py - the content archive packer, complete and incomplete areas

Port-only, not native. A synthetic export tree; no game data is read.
The complete area packs whole (terrain, navmesh, tiles, the shared
tables with the third-party brand rows dropped), the incomplete one
is excluded with its gaps named, and the archive re-packs to the same
sha256 and re-opens only against its pin.

===========================================================================
"""
import json
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

import alexandria_npcs as text_tables
import content_archive
import pack_content


def write_table(path, rows):
	path.parent.mkdir(parents=True, exist_ok=True)
	path.write_bytes(b"\xff\xfe" + text_tables.table_text(rows).encode("utf-16-le"))


def export_tree(root):
	"""A two-area export: alpha complete (sectors 182,96 and 183,96), beta short."""
	for sector_x in (182, 183):
		for extension in (".t", ".m", ".o", ".o2"):
			path = root / "96" / f"{sector_x}{extension}"
			path.parent.mkdir(parents=True, exist_ok=True)
			path.write_bytes(f"sector {sector_x} {extension}".encode())
		navmesh = root / "navmesh" / f"nv_{pack_content.region_id(sector_x, 96):04x}.nvm"
		navmesh.parent.mkdir(parents=True, exist_ok=True)
		navmesh.write_bytes(b"nvm")
	short = root / "10" / "10.m"
	short.parent.mkdir(parents=True, exist_ok=True)
	short.write_bytes(b"mesh only")
	tile = root / "map" / "map_world_182x96.ddj"
	tile.parent.mkdir(parents=True, exist_ok=True)
	tile.write_bytes(b"tile")
	(root / "regioninfo.txt").write_bytes(b"regioninfo")
	dungeon = root / "dungeon" / "dungeoninfo.txt"
	dungeon.parent.mkdir(parents=True, exist_ok=True)
	dungeon.write_bytes(b"dungeoninfo")
	write_table(root / "characterdata_45000.txt", [
		["1", "1001", "MOB_TEST_ONE", "0"],
		["1", "1002", "NPC_CREDDY_SILK_SHOP", "0"],
		["1", "1003", "MOB_ORIGIN_TEST", "0"],
		["1", "1004", "NPC_TEST_TWO", "0"],
	])
	return root


def areas_file(root):
	path = root / "areas.json"
	path.write_text(json.dumps({
		"format": pack_content.AREAS_FORMAT,
		"areas": {
			"alpha": {"regions": [[182, 96], [183, 96]]},
			"beta": {"regions": [[10, 10]]},
		},
	}), encoding="utf-8")
	return path


class PackContentTest(unittest.TestCase):
	def setUp(self):
		self.scratch = tempfile.TemporaryDirectory()
		self.addCleanup(self.scratch.cleanup)
		self.base = Path(self.scratch.name)
		self.export = export_tree(self.base / "export")
		self.areas = pack_content.read_areas(areas_file(self.base))

	def test_the_complete_area_packs_and_the_incomplete_one_is_excluded(self):
		report = pack_content.pack(self.export, self.areas, self.base / "out" / "content.zip")
		self.assertEqual(sorted(report["areas"]), ["alpha"])
		# Two sectors of four terrain files and one navmesh each, one tile.
		self.assertEqual(report["areas"]["alpha"]["files"], 2 * 5 + 1)
		self.assertEqual(report["areas"]["alpha"]["regions"], [[182, 96], [183, 96]])
		excluded = report["excludedAreas"]
		self.assertEqual([entry["area"] for entry in excluded], ["beta"])
		self.assertIn("10,10: 10/10.o", excluded[0]["missing"])
		self.assertIn("10,10: navmesh/nv_0a0a.nvm", excluded[0]["missing"])
		self.assertEqual(report["excludedRows"], {"CREDDY": 1, "ORIGIN": 1})

	def test_repacking_is_byte_identical(self):
		first = pack_content.pack(self.export, self.areas, self.base / "one.zip")
		second = pack_content.pack(self.export, self.areas, self.base / "two.zip")
		self.assertEqual(first["sha256"], second["sha256"])
		self.assertEqual((self.base / "one.zip").read_bytes(), (self.base / "two.zip").read_bytes())

	def test_the_archive_opens_only_against_its_pin(self):
		output = self.base / "content.zip"
		report = pack_content.pack(self.export, self.areas, output)
		pin = self.base / "pins.json"
		pin.write_text(json.dumps({"format": content_archive.PIN_FORMAT, "sha256": report["sha256"]}), encoding="utf-8")
		archive = content_archive.open_archive(output, pin)
		self.addCleanup(archive.close)
		self.assertEqual(archive.areas(), ["alpha"])
		self.assertEqual(archive.regions("alpha"), [(182, 96), (183, 96)])
		self.assertEqual(archive.read("areas/alpha/96/182.m"), b"sector 182 .m")
		self.assertEqual(archive.read("tables/regioninfo.txt"), b"regioninfo")
		shard = archive.read("tables/characterdata_45000.txt").decode("utf-16")
		self.assertIn("MOB_TEST_ONE", shard)
		self.assertNotIn("CREDDY", shard)
		self.assertNotIn("ORIGIN", shard)
		self.assertEqual(len(archive.names("alpha")), report["areas"]["alpha"]["files"])
		pin.write_text(json.dumps({"format": content_archive.PIN_FORMAT, "sha256": "0" * 64}), encoding="utf-8")
		with self.assertRaises(SystemExit):
			content_archive.open_archive(output, pin)

	def test_pack_refuses_when_no_area_is_complete(self):
		with self.assertRaises(SystemExit):
			pack_content.pack(self.export, {"beta": [(10, 10)]}, self.base / "none.zip")
		self.assertFalse((self.base / "none.zip").exists())

	def test_the_areas_file_is_checked(self):
		bad = self.base / "bad.json"
		bad.write_text(json.dumps({"format": "other", "areas": {}}), encoding="utf-8")
		with self.assertRaises(SystemExit):
			pack_content.read_areas(bad)
		bad.write_text(json.dumps({
			"format": pack_content.AREAS_FORMAT,
			"areas": {"a/b": {"regions": [[1, 2]]}},
		}), encoding="utf-8")
		with self.assertRaises(SystemExit):
			pack_content.read_areas(bad)
		bad.write_text(json.dumps({
			"format": pack_content.AREAS_FORMAT,
			"areas": {"alpha": {"regions": [[1]]}},
		}), encoding="utf-8")
		with self.assertRaises(SystemExit):
			pack_content.read_areas(bad)

	def test_a_second_character_shard_is_refused(self):
		write_table(self.export / "characterdata_46000.txt", [["1", "1", "MOB_X", "0"]])
		with self.assertRaises(SystemExit):
			pack_content.pack(self.export, self.areas, self.base / "two.zip")


if __name__ == "__main__":
	unittest.main()
