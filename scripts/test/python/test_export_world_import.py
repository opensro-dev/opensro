"""
===========================================================================

test_export_world_import.py - the export-world layer over a synthetic archive

Port-only, not native. A synthetic export packs to a pinned archive,
which merges into a synthetic variant tree: the sector files, the
navmesh, the tiles, the availability bits and the regioninfo rows
land, a rebuild keeps the same tree, and a switch off reaps every
byte back. Off, unpinned or unnamed, nothing is written; a named
archive that fails its pin, or lacks a whole sector, refuses.

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
import export_world_import as exporter
import pack_content

MAPINFO_SIZE = 8192


def write_table(path, rows):
	path.parent.mkdir(parents=True, exist_ok=True)
	path.write_bytes(b"\xff\xfe" + text_tables.table_text(rows).encode("utf-16-le"))


def export_tree(root):
	"""One complete area, alpha (sectors 182,96 and 183,96), in export layout."""
	for sector_x in (182, 183):
		for extension in (".t", ".m", ".o", ".o2"):
			path = root / "96" / f"{sector_x}{extension}"
			path.parent.mkdir(parents=True, exist_ok=True)
			path.write_bytes(f"sector {sector_x} {extension}".encode())
		navmesh = root / "navmesh" / f"nv_{pack_content.region_id(sector_x, 96):04x}.nvm"
		navmesh.parent.mkdir(parents=True, exist_ok=True)
		navmesh.write_bytes(b"nvm")
	tile = root / "map" / "map_world_182x96.ddj"
	tile.parent.mkdir(parents=True, exist_ok=True)
	tile.write_bytes(b"tile")
	regioninfo = "\r\n".join([
		"#FIELD\texport\tEXPORT",
		"182\t96\tALL",
		"183\t96\tALL",
		"55\t55\tALL",
		"",
	])
	(root / "regioninfo.txt").write_bytes(regioninfo.encode("cp949"))
	dungeon = root / "dungeon" / "dungeoninfo.txt"
	dungeon.parent.mkdir(parents=True, exist_ok=True)
	dungeon.write_bytes(b"dungeoninfo")
	write_table(root / "characterdata_45000.txt", [
		["1", "1001", "MOB_TEST_ONE", "0"],
		["1", "1004", "NPC_TEST_TWO", "0"],
	])
	return root


def variant_tree(root):
	"""The native/variant pair: a regioninfo without alpha's sectors, clear bits."""
	for tree in ("native", "out"):
		base = root / tree
		regioninfo = "\r\n".join(["#TOWN\tnative\t", "90\t90\tALL", ""])
		path = base / "Media_extracted" / "textdata" / "regioninfo.txt"
		path.parent.mkdir(parents=True, exist_ok=True)
		path.write_bytes(regioninfo.encode("cp949"))
		mapinfo = base / "Data_extracted" / "navmesh" / "mapinfo.mfo"
		mapinfo.parent.mkdir(parents=True, exist_ok=True)
		mapinfo.write_bytes(b"JMXVMFO 1000" + bytes(MAPINFO_SIZE - 12))
	return root / "native", root / "out"


class ExportWorldImportTest(unittest.TestCase):
	def setUp(self):
		self.scratch = tempfile.TemporaryDirectory()
		self.addCleanup(self.scratch.cleanup)
		self.base = Path(self.scratch.name)
		self.export = export_tree(self.base / "export")
		self.areas = self.base / "areas.json"
		self.areas.write_text(json.dumps({
			"format": pack_content.AREAS_FORMAT,
			"areas": {"alpha": {"regions": [[182, 96], [183, 96]]}},
		}), encoding="utf-8")
		self.archive = self.base / "content.zip"
		report = pack_content.pack(self.export, pack_content.read_areas(self.areas), self.archive)
		self.pin = self.base / "pins.json"
		self.pin.write_text(json.dumps({
			"format": content_archive.PIN_FORMAT, "sha256": report["sha256"],
		}), encoding="utf-8")
		self.native, self.out = variant_tree(self.base)
		self.pristine_regioninfo = (self.out / exporter.REGIONINFO).read_bytes()
		self.pristine_mapinfo = (self.out / "Data_extracted" / "navmesh" / "mapinfo.mfo").read_bytes()
		self.addCleanup(setattr, exporter, "PIN_FILE", exporter.PIN_FILE)
		exporter.PIN_FILE = self.pin

	def on(self, **extra):
		env = {"SRO_EXPORT_WORLD": "on", "SRO_CONTENT_ZIP": str(self.archive)}
		env.update(extra)
		return exporter.add_export_world(self.native, self.out, env)

	def test_off_writes_nothing(self):
		self.assertEqual(exporter.add_export_world(self.native, self.out, {}), {"enabled": False})
		self.assertFalse((self.out / exporter.MARKER).exists())
		self.assertEqual((self.out / exporter.REGIONINFO).read_bytes(), self.pristine_regioninfo)

	def test_on_without_a_pinned_archive_writes_nothing(self):
		summary = exporter.add_export_world(self.native, self.out, {"SRO_EXPORT_WORLD": "on"})
		self.assertFalse(summary["enabled"])
		exporter.PIN_FILE = self.base / "no-pin.json"
		summary = self.on()
		self.assertFalse(summary["enabled"])
		self.assertFalse((self.out / exporter.MARKER).exists())

	def test_the_packed_area_merges(self):
		summary = self.on()
		self.assertEqual(summary, {"enabled": True, "areas": {"alpha": {
			"regions": 2, "files": 11, "navmesh": 2, "tiles": 1, "regionRows": 3,
		}}})
		self.assertEqual((self.out / "Map_extracted" / "96" / "182.m").read_bytes(), b"sector 182 .m")
		region = pack_content.region_id(182, 96)
		self.assertEqual(
			(self.out / "Data_extracted" / "navmesh" / f"nv_{region:04x}.nvm").read_bytes(), b"nvm")
		self.assertEqual((self.out / "Media_extracted" / "map" / "map_world_182x96.ddj").read_bytes(), b"tile")
		mapinfo = (self.out / "Data_extracted" / "navmesh" / "mapinfo.mfo").read_bytes()
		at = 24 + (region >> 3)
		self.assertTrue(mapinfo[at] & (0x80 >> (region & 7)))
		text = (self.out / exporter.REGIONINFO).read_bytes().decode("cp949")
		self.assertIn("#FIELD\talpha\talpha", text)
		self.assertIn("182\t96\tALL", text)
		self.assertNotIn("55\t55\tALL", text)
		marker = json.loads((self.out / exporter.MARKER).read_text(encoding="utf-8"))
		self.assertEqual(marker["sectors"], [[182, 96], [183, 96]])
		manifest = json.loads(
			(self.out / "Media_extracted" / "textdata" / "export-world.json").read_text(encoding="utf-8"))
		self.assertEqual(manifest, {"areas": {"alpha": {
			"regions": [pack_content.region_id(182, 96), pack_content.region_id(183, 96)],
			"kinds": [],
		}}})

	def test_a_rebuild_keeps_the_same_tree(self):
		self.on()
		regioninfo = (self.out / exporter.REGIONINFO).read_bytes()
		self.on()
		self.assertEqual((self.out / exporter.REGIONINFO).read_bytes(), regioninfo)
		self.assertEqual(regioninfo.decode("cp949").count("182\t96\tALL"), 1)

	def test_switching_off_reaps_every_byte(self):
		self.on()
		self.assertEqual(exporter.add_export_world(self.native, self.out, {}), {"enabled": False})
		self.assertFalse((self.out / exporter.MARKER).exists())
		self.assertEqual([p for p in (self.out / "Map_extracted").rglob("*") if p.is_file()], [])
		self.assertEqual((self.out / exporter.REGIONINFO).read_bytes(), self.pristine_regioninfo)
		self.assertEqual(
			(self.out / "Data_extracted" / "navmesh" / "mapinfo.mfo").read_bytes(), self.pristine_mapinfo)

	def test_a_named_archive_that_fails_its_pin_refuses(self):
		self.pin.write_text(json.dumps({
			"format": content_archive.PIN_FORMAT, "sha256": "0" * 64,
		}), encoding="utf-8")
		with self.assertRaises(SystemExit):
			self.on()

	def test_an_area_missing_a_whole_sector_refuses(self):
		entries = {
			"manifest.json": json.dumps({
				"format": content_archive.ARCHIVE_FORMAT, "portOnly": True,
				"areas": {"alpha": {"regions": [[182, 96]], "files": 1}},
				"tables": ["regioninfo.txt"],
			}).encode(),
			"tables/regioninfo.txt": "182\t96\tALL\r\n".encode("cp949"),
			"areas/alpha/96/182.m": b"mesh only",
		}
		broken = self.base / "broken.zip"
		content_archive.write_archive(broken, entries)
		self.pin.write_text(json.dumps({
			"format": content_archive.PIN_FORMAT, "sha256": content_archive.file_digest(broken),
		}), encoding="utf-8")
		with self.assertRaises(SystemExit):
			self.on(SRO_CONTENT_ZIP=str(broken))


if __name__ == "__main__":
	unittest.main()
