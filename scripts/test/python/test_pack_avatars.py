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
import alexandria_npcs as characters  # noqa: E402
import hashlib
import os
import pet_import  # noqa: E402
import pet_rows  # noqa: E402
import mall_avatar_import as importer  # noqa: E402
import pack_avatars as packer  # noqa: E402
import sro_pk2  # noqa: E402
from test_cap110_items import utf16  # noqa: E402
from test_mall_avatar_import import avatar, native_row  # noqa: E402
from test_pet_import import char_row, native_wolf, scroll  # noqa: E402

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
			self.assertEqual(by_key["SN_" + GOOD][characters.TEXT_ENGLISH_COLUMN], "Suit")

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


# ================
# fake_pet_closure
#
# Stands in for the resource closure: the model each request names is
# a later file, except a family whose model is "cos\\cat.bsr", whose
# closure fails as a missing texture would.
# ================
def fake_pet_closure(pending, native, staging, archives, posture=None):
	# The real audit refuses a later model's sound tracks and ambient
	# particles unless the importer declares the posture the pets need.
	if posture != packer.PET_POSTURE:
		raise ValueError(f"{sorted(pending)[0][1]}: sound closure requires an explicit importer")
	if ("Data", "res/cos/cat.bsr") in pending:
		raise ValueError("missing Data resource: res/cos/cat.ddj")
	records = []
	for archive, path in sorted(pending):
		if archive != "Data" or not path.endswith(".bsr"):
			continue
		target = staging / packer.shared.FOLDERS[archive] / path
		target.parent.mkdir(parents=True, exist_ok=True)
		target.write_bytes(b"model")
		records.append({"archive": archive, "path": path, "source": "later", "sha256": "x"})
	return records


class PetPackTests(unittest.TestCase):
	# ================
	# stage
	#
	# The avatar export plus four pet kinds: a fresh dog, a cat whose
	# closure fails, a bird with no model in either tree, and the native
	# wolf. The native tree carries the wolf.
	# ================
	def stage(self, root):
		export, native = PackTests.stage(self, root)
		text_root = export / "server_dep" / "silkroad" / "textdata"
		(export / "res" / "cos").mkdir(parents=True)
		(export / "res" / "cos" / "newpet.bsr").write_bytes(b"dog")
		(export / "res" / "cos" / "cat.bsr").write_bytes(b"cat")
		chars = [
			char_row("COS_P_DOG", 9001),
			char_row("COS_P_CAT", 9003, model="cos\\cat.bsr"),
			char_row("COS_P_BIRD", 9005, model="cos\\bird.bsr"),
			char_row("COS_P_WOLF_001", 6106),
		]
		scrolls = [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG"), scroll("ITEM_PET_CAT", 9004, "COS_P_CAT"),
			scroll("ITEM_PET_BIRD", 9006, "COS_P_BIRD"), scroll("ITEM_PET_WOLF", 3000, "COS_P_WOLF")]
		(text_root / "characterdata.txt").write_bytes(utf16([["CharacterData_5000.txt"]]))
		(text_root / "CharacterData_5000.txt").write_bytes(utf16(chars))
		(text_root / "itemdata.txt").write_bytes(utf16([["ItemData_5000.txt"], ["ItemData_9000.txt"]]))
		(text_root / "ItemData_9000.txt").write_bytes(utf16(scrolls))
		texts = importer.tables.table_rows((text_root / "textdata_object.txt").read_bytes())
		texts += [export_text("SN_" + code, code.title()) for code in ("COS_P_DOG", "ITEM_PET_DOG", "COS_P_CAT", "ITEM_PET_CAT")]
		(text_root / "textdata_object.txt").write_bytes(utf16(texts))
		wolf_chars, wolf_items = native_wolf()
		base = native / importer.TEXT_ROOT
		(base / "characterdata.txt").write_bytes(utf16([["characterdata_5000.txt"]]))
		(base / "characterdata_5000.txt").write_bytes(utf16(wolf_chars))
		rows = importer.tables.table_rows((base / "itemdata_5000.txt").read_bytes())
		(base / "itemdata_5000.txt").write_bytes(utf16(rows + wolf_items))
		return export, native

	# ================
	# pack
	# ================
	def pack(self, folder):
		export, native = self.stage(Path(folder))
		output = Path(folder) / "opensro_avatars.zip"
		with patch.object(packer.shared, "copy_asset_closure", fake_copy), \
				patch.object(packer.shared, "copy_resource_closure", fake_pet_closure):
			report = packer.pack(export, native, output, screen=fake_screen(set()), completeness=fake_completeness())
		return native, output, report

	# ================
	# test_pets_ride_the_archive_as_the_importer_selects_them
	# ================
	def test_pets_ride_the_archive_as_the_importer_selects_them(self):
		with tempfile.TemporaryDirectory() as folder:
			_native, output, report = self.pack(folder)
			self.assertEqual(report["pets"], ["COS_P_DOG"])
			excluded = {entry["family"]: entry["missing"] for entry in report["petsExcluded"]}
			self.assertEqual(excluded["COS_P_CAT"], ["missing Data resource: res/cos/cat.ddj"])
			self.assertEqual(excluded["COS_P_BIRD"], ["model"])
			self.assertNotIn("COS_P_WOLF", excluded)
			text = "media/" + importer.tables.TEXTDATA
			with zipfile.ZipFile(output) as archive:
				names = archive.namelist()
				chars = importer.tables.table_rows(archive.read(text + packer.PET_CHAR_SHARD))
				scrolls = importer.tables.table_rows(archive.read(text + packer.PET_ITEM_SHARD))
				manifest = importer.tables.table_rows(archive.read(text + "itemdata.txt"))
				texts = importer.tables.table_rows(archive.read(text + "textdata_object.txt"))
			self.assertEqual([row[characters.CODENAME_COLUMN] for row in chars], ["COS_P_DOG"])
			# The original identity rides; the importer assigns the built one.
			self.assertEqual(chars[0][characters.ID_COLUMN], "9001")
			self.assertEqual([row[items.CODENAME] for row in scrolls], ["ITEM_PET_DOG"])
			self.assertEqual([row[0] for row in manifest], [packer.SHARD_NAME, packer.PET_ITEM_SHARD])
			self.assertIn("data/res/cos/newpet.bsr", names)
			self.assertNotIn("data/res/cos/cat.bsr", names)
			self.assertTrue({"SN_COS_P_DOG", "SN_ITEM_PET_DOG"} <= {row[1] for row in texts})

	# ================
	# test_the_importer_takes_the_packed_pet
	#
	# End to end: the packed archive, pinned, is what pet_import's archive
	# pass reads, and it imports the dog.
	# ================
	def test_the_importer_takes_the_packed_pet(self):
		with tempfile.TemporaryDirectory() as folder:
			native, output, _report = self.pack(folder)
			pin = Path(folder) / "pins.json"
			pin.write_text(json.dumps({"format": importer.AVATARS_PIN_FORMAT,
				"sha256": hashlib.sha256(output.read_bytes()).hexdigest()}), encoding="utf-8")
			staged = Path(folder) / "staged"
			staged.mkdir()
			with patch.object(pet_import, "AVATARS_PIN_FILE", pin), \
					patch.dict(os.environ, {pet_import.AVATARS_ENV: str(output)}, clear=True):
				imported = pet_import.add_archive_pets(native, staged)
			self.assertEqual([kind["family"] for kind in imported["added"]], ["COS_P_DOG"])
			self.assertTrue((staged / "Data_extracted" / "res" / "cos" / "newpet.bsr").exists())


if __name__ == "__main__":
	unittest.main()
