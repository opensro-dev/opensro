"""
===========================================================================

test_pack_avatars.py - packing a newer client's avatars into the archive

A synthetic export tree covers selection, row and text normalization,
the model screen, closure exclusions, determinism and the pin the
importer verifies. The screen and the asset closure are faked at their
seams; the real ones run against the real export in the pack report.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch
import zipfile

import cap110_items as items  # noqa: E402
import mall_avatar_import as importer  # noqa: E402
import pack_avatars as packer  # noqa: E402
import sro_pk2  # noqa: E402
from test_cap110_items import utf16  # noqa: E402
from test_mall_avatar_import import avatar, native_row  # noqa: E402

FIRST_ID = 24371
GOOD = "ITEM_CREDDY_AVATAR_M_SUIT"
SCREENED = "ITEM_CREDDY_AVATAR_M_GHOST"
NO_TEXT = "ITEM_EVENT_AVATAR_W_BALL"
NATIVE_CODE = "ITEM_MALL_AVATAR_M_TURKEY"


# ================
# export_text
#
# One export-shaped text row: 19 cells, the English text at index 9.
# ================
def export_text(key, english):
	return ["1", key, "ko"] + ["0"] * 6 + [english] + ["0"] * 9


# ================
# fake_screen
# ================
def fake_screen(refused):
	def screen(rows, archives, natives):
		return {code: "environment-mapped material" for code in refused}
	return screen


# ================
# fake_completeness
#
# Stands in for the closure walk: every row survives but the named.
# ================
def fake_completeness(dropped=()):
	def completeness(rows, archives, natives):
		kept = [row for row in rows if row[items.CODENAME] not in dropped]
		return kept, [{"code": code, "missing": ["missing Data resource: res/item/gone.bsr"]} for code in dropped]
	return completeness


# ================
# fake_copy
#
# Stands in for the importer's closure copy: writes the two resource
# files every row shares, one of them native-owned.
# ================
def fake_copy(rows, native, staging, archives):
	records = []
	for archive, path, origin in (("Data", "res/item/avatar.bsr", "later"), ("Media", "icon/item/native.ddj", "native")):
		target = staging / packer.shared.FOLDERS[archive] / path
		target.parent.mkdir(parents=True, exist_ok=True)
		target.write_bytes(b"bytes of " + path.encode())
		records.append({"archive": archive, "path": path, "source": origin, "sha256": "x"})
	return records


class PackTests(unittest.TestCase):
	# ================
	# stage
	#
	# An export tree with a good row, a screened row, a textless row, a
	# native row and an over-wide row, and the native tree they pack
	# against.
	# ================
	def stage(self, root):
		export, native = root / "export", root / "native"
		text_root = export / "server_dep" / "silkroad" / "textdata"
		text_root.mkdir(parents=True)
		(export / "res" / "item").mkdir(parents=True)
		(export / "res" / "item" / "avatar.bsr").write_bytes(b"model")
		(export / "res" / "item" / "drop.bsr").write_bytes(b"drop")
		(export / "icon" / "item").mkdir(parents=True)
		(export / "icon" / "item" / "avatar.ddj").write_bytes(b"icon")
		rows = [
			avatar(GOOD, FIRST_ID) + ["", ""],
			avatar(SCREENED, FIRST_ID + 1),
			avatar(NO_TEXT, FIRST_ID + 2),
			avatar(NATIVE_CODE, FIRST_ID + 3),
			avatar("ITEM_CREDDY_AVATAR_M_WIDE", FIRST_ID + 4) + ["", "junk"],
		]
		(text_root / "itemdata.txt").write_bytes(utf16([["ItemData_5000.txt"]]))
		(text_root / "ItemData_5000.txt").write_bytes(utf16(rows))
		texts = [export_text(key, "Suit") for code in (GOOD, SCREENED, NATIVE_CODE, "ITEM_CREDDY_AVATAR_M_WIDE")
			for key in ("SN_" + code, "SN_" + code + "_TT_DESC")]
		(text_root / "textdata_object.txt").write_bytes(utf16(texts))
		(text_root / "textdata_equip&skill.txt").write_bytes(utf16([]))
		base = native / importer.TEXT_ROOT
		base.mkdir(parents=True)
		for folder in packer.shared.FOLDERS.values():
			(native / folder).mkdir(parents=True, exist_ok=True)
		(base / "itemdata.txt").write_bytes(utf16([["itemdata_5000.txt"]]))
		(base / "itemdata_5000.txt").write_bytes(utf16([native_row(10), avatar(NATIVE_CODE, 99)]))
		return export, native

	# ================
	# packed_rows
	# ================
	def packed_rows(self, path):
		with zipfile.ZipFile(path) as archive:
			names = archive.namelist()
			shard = archive.read("media/" + importer.tables.TEXTDATA + packer.SHARD_NAME)
		return names, importer.tables.table_rows(shard)

	# ================
	# test_pack_selects_normalizes_and_reports
	# ================
	def test_pack_selects_normalizes_and_reports(self):
		with tempfile.TemporaryDirectory() as folder:
			export, native = self.stage(Path(folder))
			output = Path(folder) / "opensro_avatars.zip"
			with patch.object(packer.shared, "copy_asset_closure", fake_copy):
				report = packer.pack(export, native, output, screen=fake_screen({SCREENED}), completeness=fake_completeness())
			self.assertEqual(report["rows"], 1)
			excluded = {entry["code"]: entry["missing"] for entry in report["excluded"]}
			self.assertEqual(excluded[SCREENED], ["environment-mapped material"])
			self.assertEqual(excluded[NO_TEXT], ["name text"])
			self.assertEqual(excluded["ITEM_CREDDY_AVATAR_M_WIDE"], ["row wider than the native layout"])
			self.assertNotIn(NATIVE_CODE, excluded)
			names, rows = self.packed_rows(output)
			self.assertEqual([row[items.CODENAME] for row in rows], [GOOD])
			self.assertTrue(all(len(row) == items.ITEM_COLUMNS for row in rows))
			self.assertIn("data/res/item/avatar.bsr", names)
			self.assertNotIn("media/icon/item/native.ddj", names)
			with zipfile.ZipFile(output) as archive:
				texts = importer.tables.table_rows(archive.read("media/" + importer.tables.TEXTDATA + "textdata_object.txt"))
			self.assertEqual(len(texts), 2)
			self.assertTrue(all(len(row) == importer.VSRO_TEXT_COLUMNS for row in texts))
			by_key = {row[1]: row for row in texts}
			self.assertEqual(by_key["SN_" + GOOD][importer.TEXT_ENGLISH], "Suit")

	# ================
	# test_pack_is_deterministic
	# ================
	def test_pack_is_deterministic(self):
		with tempfile.TemporaryDirectory() as folder:
			export, native = self.stage(Path(folder))
			first, second = Path(folder) / "a.zip", Path(folder) / "b.zip"
			with patch.object(packer.shared, "copy_asset_closure", fake_copy):
				one = packer.pack(export, native, first, screen=fake_screen(set()), completeness=fake_completeness())
				two = packer.pack(export, native, second, screen=fake_screen(set()), completeness=fake_completeness())
			self.assertEqual(one["sha256"], two["sha256"])
			self.assertEqual(first.read_bytes(), second.read_bytes())

	# ================
	# test_closure_exclusion_drops_only_its_row
	# ================
	def test_closure_exclusion_drops_only_its_row(self):
		with tempfile.TemporaryDirectory() as folder:
			export, native = self.stage(Path(folder))
			output = Path(folder) / "opensro_avatars.zip"
			with patch.object(packer.shared, "copy_asset_closure", fake_copy):
				report = packer.pack(export, native, output, screen=fake_screen(set()), completeness=fake_completeness({GOOD}))
			self.assertEqual(report["rows"], 1)
			_, rows = self.packed_rows(output)
			self.assertEqual([row[items.CODENAME] for row in rows], [SCREENED])

	# ================
	# test_completeness_drops_only_the_row_that_misses_a_file
	#
	# The walk itself: two rows share a model, each names its own
	# texture through it, and one texture is absent from both trees.
	# ================
	def test_completeness_drops_only_the_row_that_misses_a_file(self):
		rows = [avatar(GOOD, FIRST_ID), avatar(SCREENED, FIRST_ID + 1)]
		rows[1][items.MODEL] = "item/ghost.bsr"

		def dependencies(requests):
			if any("weapon" in request for request in requests):
				return [["res/item/avatar.bsr"], ["res/item/ghost.bsr"]]
			deps = {"res/item/avatar.bsr": ["res/item/good.ddj"], "res/item/ghost.bsr": ["res/item/gone.ddj"]}
			return [deps.get(request["path"], []) for request in requests]

		files = {"res/item/avatar.bsr": b"a", "res/item/ghost.bsr": b"g", "res/item/good.ddj": b"t",
			"res/item/drop.bsr": b"d", "icon/item/avatar.ddj": b"i"}
		blob = packer.FileBlob()
		index = {}
		archives = {name: (blob, index) for name in packer.shared.FOLDERS}
		natives = {name: {} for name in packer.shared.FOLDERS}
		with tempfile.TemporaryDirectory() as folder:
			for name, raw in files.items():
				path = Path(folder) / name.replace("/", "_")
				path.write_bytes(raw)
				index[name] = sro_pk2.Entry(name, blob.add(path, len(raw)), len(raw))
			with patch.object(packer.shared, "resource_dependencies", dependencies):
				surviving, dropped = packer.closure_completeness(rows, archives, natives)
		self.assertEqual([row[items.CODENAME] for row in surviving], [GOOD])
		self.assertEqual(dropped, [{"code": SCREENED, "missing": ["missing Data resource: res/item/gone.ddj"]}])

	# ================
	# test_the_importer_verifies_the_pin
	# ================
	def test_the_importer_verifies_the_pin(self):
		with tempfile.TemporaryDirectory() as folder:
			export, native = self.stage(Path(folder))
			output = Path(folder) / "opensro_avatars.zip"
			with patch.object(packer.shared, "copy_asset_closure", fake_copy):
				report = packer.pack(export, native, output, screen=fake_screen(set()), completeness=fake_completeness())
			pin = Path(folder) / "pins.json"
			pin.write_text(json.dumps({"format": importer.AVATARS_PIN_FORMAT, "sha256": report["sha256"]}), encoding="utf-8")
			archives, identities = packer.avatar_archive.open_archive(output, pin)
			self.assertEqual(identities, {"archive": report["sha256"]})
			_, index = archives["Media"]
			self.assertIn(importer.tables.TEXTDATA + packer.SHARD_NAME, index)
			pin.write_text(json.dumps({"format": importer.AVATARS_PIN_FORMAT, "sha256": "0" * 64}), encoding="utf-8")
			with self.assertRaises(SystemExit):
				packer.avatar_archive.open_archive(output, pin)
			self.assertEqual(sro_pk2.digest(output.read_bytes()), report["sha256"])


if __name__ == "__main__":
	unittest.main()
