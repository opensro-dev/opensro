"""
===========================================================================
test_magic_cube_import.py - the Magic Cube import's contract.

Behind SRO_MAGIC_CUBE the import stages exactly the later client's
sixteen Magic Cube rows in their own itemdata shard under marker-kept
identities, with their icons and names from the later client; a switch
off reaps the shard, its manifest line, the names, the later icons and
the marker; a native collision or a miscast row refuses.
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import cap110_item_import as shared  # noqa: E402
import cap110_items as items  # noqa: E402
import jupiter_import as jupiter  # noqa: E402
import magic_cube_import as cubes  # noqa: E402
import mall_avatar_import as avatars  # noqa: E402
import sro_pk2  # noqa: E402
from test_nasrun_import import archive_bytes, utf16  # noqa: E402

NATIVE_ID = 5000


# ================
# cube_row
#
# One v1.696 Magic Cube row: TID 3/2/3/1, Param9 capacity, Param10 filter.
# ================
def cube_row(code, identity, capacity, category):
	row = ["0"] * items.ITEM_COLUMNS
	row[items.SERVICE], row[items.IDENTITY], row[items.CODENAME] = "1", str(identity), code
	row[items.NAME_KEY], row[items.DESCRIPTION_KEY] = "SN_" + code, "SN_" + code + "_TT_DESC"
	row[items.TYPE_FIRST:items.TYPE_FOURTH + 1] = list(cubes.CUBE_TYPE)
	row[items.MODEL] = items.UNUSED
	row[items.DROP_MODEL] = "item\\etc\\drop_ch_bag.bsr"
	row[items.ICON] = "item\\etc\\" + code.lower() + ".ddj"
	row[cubes.PARAM_CAPACITY] = str(capacity)
	row[cubes.PARAM_FILTER] = str(category)
	return row


# ================
# text_row
# ================
def text_row(key, english):
	row = ["1", key] + ["0"] * 13
	row[8] = english
	return row


# ================
# MagicCubeImportTest
# ================
class MagicCubeImportTest(unittest.TestCase):
	# ================
	# fixture
	#
	# A scratch world: a fake v1.696 Media.pk2 holding the sixteen cubes
	# and a decoy, and an output tree with one native item and a name
	# table.
	# ================
	def fixture(self, base, drop=None, mutate=None):
		later, native, output = base / "later", base / "native", base / "output"
		text_root = output / cubes.TEXT_ROOT
		text_root.mkdir(parents=True)
		later.mkdir()
		native.mkdir()
		rows = []
		identity = 40000
		for code, capacity, category in cubes.CUBES:
			if code == drop:
				continue
			row = cube_row(code, identity, capacity, category)
			identity += 1
			if mutate:
				mutate(row)
			rows.append(row)
		decoy = cube_row("ITEM_PRE_MALL_MAGIC_CUBE_DECOY", 50000, 10, 1)
		decoy[items.TYPE_FIRST:items.TYPE_FOURTH + 1] = ["3", "3", "1", "1"]
		names = [text_row("SN_" + row[items.CODENAME], "Cube " + row[items.CODENAME]) for row in rows]
		self.media = archive_bytes({
			"itemdata.txt": utf16([["itemdata_40000.txt"]]),
			"itemdata_40000.txt": utf16(rows + [decoy]),
			"textdata_equip&skill.txt": utf16(names),
		})
		native_row = ["0"] * items.ITEM_COLUMNS
		native_row[0], native_row[1], native_row[2] = "1", str(NATIVE_ID), "ITEM_NATIVE_THING"
		(text_root / "itemdata.txt").write_bytes(utf16([["itemdata_5000.txt"]]))
		(text_root / "itemdata_5000.txt").write_bytes(utf16([native_row]))
		(text_root / cubes.NAME_TABLE).write_bytes(utf16([text_row("SN_ITEM_NATIVE_THING", "Native thing")]))
		(native / cubes.TEXT_ROOT).mkdir(parents=True)
		(native / cubes.TEXT_ROOT / cubes.NAME_TABLE).write_bytes(utf16([text_row("SN_ITEM_NATIVE_THING", "Native thing")]))
		return later, native, output, text_root

	# ================
	# run_import
	# ================
	def run_import(self, later, native, output, env):
		data = self.media
		index = {shared.normalized_path(entry.path): entry for entry in sro_pk2.read_directory(data)}

		def open_archives(stack, root, source):
			self.assertIs(source, avatars.V1696_SOURCE)
			return {"Media": (data, index)}, {"Media": "fixture"}

		def copy_asset_closure(rows, native_root, output_root, archives, worn=True):
			self.assertFalse(worn)
			records = []
			for row in rows:
				path = "icon/" + row[items.ICON].replace("\\", "/")
				target = output_root / shared.FOLDERS["Media"] / path
				target.parent.mkdir(parents=True, exist_ok=True)
				target.write_bytes(b"icon")
				records.append({"archive": "Media", "path": path, "source": "later", "sha256": "x"})
			return records

		# The fixture Media has no icon payloads (same as the Nasrun
		# tests); resolve_cube_icon only needs the path to stand.
		def keep_icon(icon, media_index):
			return icon.replace("/", "\\")

		with mock.patch.dict(os.environ, env), \
				mock.patch.object(avatars, "open_archives", open_archives), \
				mock.patch.object(jupiter, "require_v1696_pin", lambda root, names: None), \
				mock.patch.object(cubes, "resolve_cube_icon", keep_icon), \
				mock.patch.object(shared, "copy_asset_closure", copy_asset_closure):
			return cubes.add_magic_cubes(native, output, later)

	# ================
	# test_import_stages_the_sixteen_cubes
	# ================
	def test_import_stages_the_sixteen_cubes(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, text_root = self.fixture(Path(root))
			summary = self.run_import(later, native, output, {cubes.FLAG: "on"})
			self.assertEqual(len(summary["items"]), 16)
			self.assertNotIn("ITEM_PRE_MALL_MAGIC_CUBE_DECOY", summary["items"])
			shard = (text_root / cubes.ITEM_SHARD).read_bytes().decode("utf-16")
			self.assertIn("ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_10", shard)
			manifest = (text_root / "itemdata.txt").read_bytes().decode("utf-16")
			self.assertIn(cubes.ITEM_SHARD, manifest)
			marker = json.loads((output / cubes.MARKER).read_text(encoding="utf-8"))
			self.assertEqual(marker["items"], summary["items"])
			self.assertTrue(all(identity > NATIVE_ID for identity in summary["items"].values()))
			self.assertTrue(all(not 40000 <= identity <= 50000 for identity in summary["items"].values()))
			names = (text_root / cubes.NAME_TABLE).read_bytes().decode("utf-16")
			self.assertIn("SN_ITEM_NATIVE_THING", names)
			self.assertIn("Cube ITEM_PRE_MALL_MAGIC_CUBE_SHIELD_30", names)
			self.assertEqual(len(marker["texts"]), 16)
			self.assertEqual(len(summary["assets"]), 16)
			again = self.run_import(later, native, output, {cubes.FLAG: "on"})
			self.assertEqual(again["items"], summary["items"])
			self.assertEqual((text_root / cubes.NAME_TABLE).read_bytes().decode("utf-16").count(
				"SN_ITEM_PRE_MALL_MAGIC_CUBE_WEAPON\t"), 1)

	# ================
	# test_off_rebuild_reaps_owned_outputs
	# ================
	def test_off_rebuild_reaps_owned_outputs(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, text_root = self.fixture(Path(root))
			summary = self.run_import(later, native, output, {cubes.FLAG: "on"})
			icon = output / shared.FOLDERS["Media"] / summary["assets"][0]["path"]
			self.assertTrue(icon.exists())
			self.assertEqual(self.run_import(later, native, output, {cubes.FLAG: ""}), {"enabled": False})
			self.assertFalse((text_root / cubes.ITEM_SHARD).exists())
			self.assertFalse((output / cubes.MARKER).exists())
			self.assertNotIn(cubes.ITEM_SHARD, (text_root / "itemdata.txt").read_bytes().decode("utf-16"))
			self.assertFalse(icon.exists())
			names = (text_root / cubes.NAME_TABLE).read_bytes().decode("utf-16")
			self.assertIn("SN_ITEM_NATIVE_THING", names)
			self.assertNotIn("ITEM_PRE_MALL_MAGIC_CUBE_WEAPON", names)

	# ================
	# test_missing_row_refuses
	# ================
	def test_missing_row_refuses(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, _ = self.fixture(Path(root), drop="ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_10")
			with self.assertRaisesRegex(ValueError, "ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_10"):
				self.run_import(later, native, output, {cubes.FLAG: "on"})

	# ================
	# test_miscast_capacity_refuses
	# ================
	def test_miscast_capacity_refuses(self):
		with tempfile.TemporaryDirectory() as root:
			def mutate(row):
				if row[items.CODENAME] == "ITEM_PRE_MALL_MAGIC_CUBE_WEAPON":
					row[cubes.PARAM_CAPACITY] = "99"

			later, native, output, _ = self.fixture(Path(root), mutate=mutate)
			with self.assertRaisesRegex(ValueError, "capacity"):
				self.run_import(later, native, output, {cubes.FLAG: "on"})


if __name__ == "__main__":
	unittest.main()
