"""
===========================================================================
test_gears_stone_import.py - the gears magic stones import's contract.

Behind SRO_MALL_CATEGORIES the import stages exactly the later
client's 112 leveled magic stones in their own itemdata shard under
marker-kept identities; a switch off reaps the shard, its manifest
line and the marker, and a native collision or a miscast row refuses.
===========================================================================
"""
import hashlib
import json
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import gears_stone_import as gears  # noqa: E402
import jupiter_import as jupiter  # noqa: E402


# ================
# utf16
# ================
def utf16(rows):
	return b"\xff\xfe" + ("\r\n".join("\t".join(row) for row in rows) + "\r\n").encode("utf-16-le")


# ================
# archive_bytes
# ================
def archive_bytes(files):
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


OPTIONS = {
	"STR": "MATTR_STR", "INT": "MATTR_INT", "HP": "MATTR_HP", "MP": "MATTR_MP",
	"HR": "MATTR_HR", "DUR": "MATTR_DUR", "ER": "MATTR_ER", "SOLID": "MATTR_SOLID",
	"BURN": "MATTR_RESIST_BURN", "POISON": "MATTR_RESIST_POISON",
	"FROSTBITE": "MATTR_RESIST_FROSTBITE", "ZOMBIE": "MATTR_RESIST_ZOMBIE",
	"ESHOCK": "MATTR_RESIST_ESHOCK", "EVADE_BLOCK": "MATTR_EVADE_BLOCK",
	"EVADE_CRITICAL": "MATTR_EVADE_CRITICAL", "LUCK": "MATTR_LUCK",
}


# ================
# stone_row
# ================
def stone_row(code, identity, degree, option):
	row = ["0"] * gears.ITEM_COLUMNS
	row[0], row[1], row[2] = "1", str(identity), code
	row[9:13] = ["3", "3", "11", "1"]
	row[118], row[119] = str(degree), option
	return row


# ================
# GearsStoneImportTest
# ================
class GearsStoneImportTest(unittest.TestCase):
	def setUp(self):
		jupiter._verified_archives.clear()

	# ================
	# fixture
	#
	# A scratch world: a fake v1.696 root (one Media.pk2) whose
	# itemdata carries the 112 gears stones plus a decoy, and a native
	# tree with one shipped item row.
	# ================
	def fixture(self, base, drop=None, mutate=None):
		v1696, native, output = base / "v1696", base / "native", base / "output"
		text_root = output / gears.TEXT_ROOT
		text_root.mkdir(parents=True)
		v1696.mkdir()
		native.mkdir()
		rows = []
		identity = 30000
		for code in gears.stone_codenames():
			if code == drop:
				continue
			kind, _, degree = code[len(gears.PREFIX):].rpartition("_")
			row = stone_row(code, identity, int(degree), OPTIONS[kind])
			identity += 1
			if mutate:
				mutate(row)
			rows.append(row)
		decoy = stone_row("ITEM_ETC_ARCHEMY_MAGICSTONE_STR_05", 40000, 5, "MATTR_STR")
		media = {
			"itemdata.txt": utf16([["itemdata_6000.txt"]]),
			"itemdata_6000.txt": utf16(rows + [decoy]),
		}
		media_bytes = archive_bytes(media)
		(v1696 / "Media.pk2").write_bytes(media_bytes)
		native_row = ["0"] * gears.ITEM_COLUMNS
		native_row[0], native_row[1], native_row[2] = "1", "5000", "ITEM_NATIVE_THING"
		(text_root / "itemdata.txt").write_bytes(utf16([["itemdata_5000.txt"]]))
		(text_root / "itemdata_5000.txt").write_bytes(utf16([native_row]))
		pins = {"Media": hashlib.sha256(media_bytes).hexdigest()}
		return v1696, native, output, text_root, pins

	# ================
	# test_import_stages_the_112_stones
	# ================
	def test_import_stages_the_112_stones(self):
		with tempfile.TemporaryDirectory() as root:
			v1696, native, output, text_root, pins = self.fixture(Path(root))
			with mock.patch.object(jupiter, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, {"SRO_MALL_CATEGORIES": "on"}):
					summary = gears.add_stones(native, output, v1696)
			self.assertEqual(len(summary["items"]), 112)
			self.assertNotIn("ITEM_ETC_ARCHEMY_MAGICSTONE_STR_05", summary["items"])
			shard = (text_root / gears.ITEM_SHARD).read_bytes().decode("utf-16")
			self.assertIn("ITEM_ETC_ARCHEMY_LEVEL_MAGICSTONE_STR_05", shard)
			manifest = (text_root / "itemdata.txt").read_bytes().decode("utf-16")
			self.assertIn(gears.ITEM_SHARD, manifest)
			marker = json.loads((output / gears.MARKER).read_text(encoding="utf-8"))
			self.assertEqual(marker["items"], summary["items"])
			# Fresh identities above every published native row, never
			# the later client's own numbers.
			self.assertTrue(all(identity > 5000 for identity in summary["items"].values()))
			self.assertTrue(all(not 30000 <= identity <= 40000 for identity in summary["items"].values()))
			# A rebuild keeps the published identities.
			with mock.patch.object(jupiter, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, {"SRO_MALL_CATEGORIES": "on"}):
					again = gears.add_stones(native, output, v1696)
			self.assertEqual(again["items"], summary["items"])

	# ================
	# test_off_rebuild_reaps_owned_outputs
	# ================
	def test_off_rebuild_reaps_owned_outputs(self):
		with tempfile.TemporaryDirectory() as root:
			v1696, native, output, text_root, pins = self.fixture(Path(root))
			with mock.patch.object(jupiter, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, {"SRO_MALL_CATEGORIES": "on"}):
					gears.add_stones(native, output, v1696)
				with mock.patch.dict(os.environ, {"SRO_MALL_CATEGORIES": ""}):
					self.assertEqual(gears.add_stones(native, output, v1696), {"enabled": False})
			self.assertFalse((text_root / gears.ITEM_SHARD).exists())
			self.assertFalse((output / gears.MARKER).exists())
			manifest = (text_root / "itemdata.txt").read_bytes().decode("utf-16")
			self.assertNotIn(gears.ITEM_SHARD, manifest)
			self.assertIn("itemdata_5000.txt", manifest)

	# ================
	# test_missing_or_miscast_stone_refuses
	# ================
	def test_missing_or_miscast_stone_refuses(self):
		with tempfile.TemporaryDirectory() as root:
			v1696, native, output, text_root, pins = self.fixture(
				Path(root), drop="ITEM_ETC_ARCHEMY_LEVEL_MAGICSTONE_LUCK_07")
			with mock.patch.object(jupiter, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, {"SRO_MALL_CATEGORIES": "on"}):
					with self.assertRaises(ValueError):
						gears.add_stones(native, output, v1696)
		with tempfile.TemporaryDirectory() as root:
			def miscast(row):
				if row[2].endswith("STR_01"):
					row[12] = "2"
			v1696, native, output, text_root, pins = self.fixture(Path(root), mutate=miscast)
			with mock.patch.object(jupiter, "archive_pins", return_value=pins):
				with mock.patch.dict(os.environ, {"SRO_MALL_CATEGORIES": "on"}):
					with self.assertRaises(ValueError):
						gears.add_stones(native, output, v1696)


if __name__ == "__main__":
	unittest.main()
