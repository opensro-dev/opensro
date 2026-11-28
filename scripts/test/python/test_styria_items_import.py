"""
===========================================================================

test_styria_items_import.py - the Styria Clash reward items' contract.

Behind SRO_STYRIA_CLASH the import stages exactly the four reward
items of the v1.696 client (the win's two rune seals, the blue and
black stones every result pays) in their own itemdata shard under
marker-kept identities, with the later client's own English names
and their icons. A rebuild keeps the identities and one copy of
each text; a switch off reaps exactly what the import added. A
missing or miscast row, or a codename already in the tree, refuses.
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import later_fixtures  # noqa: E402
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import cap110_item_import as shared  # noqa: E402
import cap110_items as items  # noqa: E402
import jupiter_import as jupiter  # noqa: E402
import mall_avatar_import as avatars  # noqa: E402
import styria_items_import as styria_items  # noqa: E402
import sro_pk2  # noqa: E402
from test_nasrun_import import archive_bytes, utf16  # noqa: E402

ON = {"SRO_STYRIA_CLASH": "on"}
OFF = {"SRO_STYRIA_CLASH": ""}
CODES = [code for code, _ in styria_items.ITEMS]
NATIVE_ID = 5000


# ================
# later_row
#
# One v1.696 row as the later client authors the reward items: the
# item's own type word, a drop model and an icon under item\etc.
# ================
def later_row(code, identity, kind, icon):
	row = ["0"] * items.ITEM_COLUMNS
	row[items.SERVICE], row[items.IDENTITY], row[items.CODENAME] = "1", str(identity), code
	row[items.NAME_KEY], row[items.DESCRIPTION_KEY] = "SN_" + code, "SN_" + code + "_TT_DESC"
	row[items.TYPE_FIRST:items.TYPE_FOURTH + 1] = kind
	row[items.MODEL] = items.UNUSED
	row[items.DROP_MODEL] = "item\\etc\\drop_trade.bsr"
	row[items.ICON] = "item\\etc\\" + icon
	return row


# ================
# text_row
#
# One later text row: service, key, then the language cells with the
# English one where the later client puts it (native_text_row moves
# it into the v1.150 layout).
# ================
def text_row(key):
	cells = ["1", key] + [""] * 13
	cells[8] = "English " + key
	return cells


# ================
# StyriaItemsImportTest
# ================
class StyriaItemsImportTest(unittest.TestCase):
	# ================
	# fixture
	#
	# A scratch world: a fake v1.696 Media.pk2 holding the four
	# reward rows plus a decoy and their name texts (one item
	# without a description, as the later client leaves some),
	# and an output tree with one native item and a name table.
	# ================
	def fixture(self, base, drop=None, mutate=None):
		later, native, output = base / "later", base / "native", base / "output"
		text_root = output / styria_items.TEXT_ROOT
		text_root.mkdir(parents=True)
		later.mkdir()
		native.mkdir()
		rows, texts = [], []
		for offset, (code, kind) in enumerate(styria_items.ITEMS):
			if code == drop:
				continue
			row = later_row(code, 35000 + offset, kind, code.lower() + ".ddj")
			if mutate:
				mutate(row)
			rows.append(row)
			texts.append(text_row(row[items.NAME_KEY]))
			if code != styria_items.ITEMS[0][0]:
				texts.append(text_row(row[items.DESCRIPTION_KEY]))
		rows.append(later_row("ITEM_EVENT_TRADE_DECOY", 35999, styria_items.STONE_TYPE, "decoy.ddj"))
		self.media = archive_bytes({
			**later_fixtures.sharded("itemdata.txt", {"itemdata_35000.txt": rows}),
			"textdata_object.txt": utf16(texts),
		})
		native_row = ["0"] * items.ITEM_COLUMNS
		native_row[0], native_row[1], native_row[2] = "1", str(NATIVE_ID), "ITEM_NATIVE_THING"
		later_fixtures.write_sharded(text_root, "itemdata.txt", {"itemdata_5000.txt": [native_row]})
		(text_root / styria_items.NAME_TABLE).write_bytes(
			utf16([["1", "SN_NATIVE_A", "", "", "", "", "", "", "Native A"]]))
		return later, native, output, text_root

	# ================
	# run_import
	#
	# One pass on the fixture's archive, the later client present
	# and pinned, the resource copy writing each icon as a later
	# file (its closure walk is the cap-110 import's, tested there).
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

		with mock.patch.dict(os.environ, env), \
				mock.patch.object(avatars, "open_archives", open_archives), \
				mock.patch.object(jupiter, "require_v1696_pin", lambda root, names: None), \
				mock.patch.object(shared, "copy_asset_closure", copy_asset_closure):
			return styria_items.add_styria_items(native, output, later)

	# ================
	# shard_rows
	# ================
	def shard_rows(self, text_root):
		lines = (text_root / styria_items.ITEM_SHARD).read_bytes().decode("utf-16").splitlines()
		return {row[items.CODENAME]: row for row in (line.split("\t") for line in lines if line)}

	# ================
	# names
	# ================
	def names(self, text_root):
		lines = (text_root / styria_items.NAME_TABLE).read_bytes().decode("utf-16").splitlines()
		return {row[1]: row for row in (line.split("\t") for line in lines) if len(row) > 8}

	# ================
	# manifest
	# ================
	def manifest(self, text_root):
		lines = (text_root / "itemdata.txt").read_bytes().decode("utf-16").splitlines()
		return [line.split("\t")[0] for line in lines if line]

	# ================
	# test_on_stages_the_four_reward_items
	# ================
	def test_on_stages_the_four_reward_items(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, text_root = self.fixture(Path(root))
			summary = self.run_import(later, native, output, ON)
			self.assertTrue(summary["enabled"])
			self.assertTrue(summary["portOnly"])
			self.assertEqual(sorted(summary["items"]), sorted(CODES))
			rows = self.shard_rows(text_root)
			self.assertEqual(sorted(rows), sorted(CODES))
			for code, kind in styria_items.ITEMS:
				self.assertEqual(rows[code][items.TYPE_FIRST:items.TYPE_FOURTH + 1], kind)
				self.assertEqual(rows[code][items.ICON], "item\\etc\\" + code.lower() + ".ddj")
				self.assertEqual(rows[code][items.PLACEHOLDER_NAME], items.UNUSED)
				self.assertGreater(int(rows[code][items.IDENTITY]), NATIVE_ID)
				self.assertEqual(int(rows[code][items.IDENTITY]), summary["items"][code])
			self.assertEqual(self.manifest(text_root).count(styria_items.ITEM_SHARD), 1)
			names = self.names(text_root)
			for code in CODES:
				self.assertIn("SN_" + code, names)
				self.assertEqual(names["SN_" + code][8], "English SN_" + code)
			# The one description the later client does not write is
			# reported absent, not invented.
			self.assertEqual(summary["absentDescriptions"], ["SN_" + styria_items.ITEMS[0][0] + "_TT_DESC"])
			marker = json.loads((output / styria_items.MARKER).read_text(encoding="utf-8"))
			self.assertEqual(marker["items"], summary["items"])

	# ================
	# test_a_rebuild_keeps_identities_and_bytes
	# ================
	def test_a_rebuild_keeps_identities_and_bytes(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, text_root = self.fixture(Path(root))
			first = self.run_import(later, native, output, ON)
			shard = (text_root / styria_items.ITEM_SHARD).read_bytes()
			names = (text_root / styria_items.NAME_TABLE).read_bytes()
			second = self.run_import(later, native, output, ON)
			self.assertEqual(second["items"], first["items"])
			self.assertEqual((text_root / styria_items.ITEM_SHARD).read_bytes(), shard)
			self.assertEqual((text_root / styria_items.NAME_TABLE).read_bytes(), names)
			self.assertEqual(self.manifest(text_root).count(styria_items.ITEM_SHARD), 1)

	# ================
	# test_off_reaps_exactly_the_import
	# ================
	def test_off_reaps_exactly_the_import(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, text_root = self.fixture(Path(root))
			native_names = (text_root / styria_items.NAME_TABLE).read_bytes()
			self.run_import(later, native, output, ON)
			icon = output / shared.FOLDERS["Media"] / "icon" / "item" / "etc" / (CODES[0].lower() + ".ddj")
			self.assertTrue(icon.exists())
			summary = self.run_import(later, native, output, OFF)
			self.assertEqual(summary, {"enabled": False})
			self.assertFalse((text_root / styria_items.ITEM_SHARD).exists())
			self.assertNotIn(styria_items.ITEM_SHARD, self.manifest(text_root))
			self.assertFalse((output / styria_items.MARKER).exists())
			self.assertFalse(icon.exists())
			self.assertEqual((text_root / styria_items.NAME_TABLE).read_bytes(), native_names)
			# The native item stands untouched beside the reap.
			self.assertIn(later_fixtures.archive_case("itemdata_5000.txt"), self.manifest(text_root))

	# ================
	# test_off_without_a_prior_import_changes_nothing
	# ================
	def test_off_without_a_prior_import_changes_nothing(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, text_root = self.fixture(Path(root))
			before = {path.name: path.read_bytes() for path in text_root.iterdir()}
			summary = self.run_import(later, native, output, OFF)
			self.assertEqual(summary, {"enabled": False})
			after = {path.name: path.read_bytes() for path in text_root.iterdir()}
			self.assertEqual(after, before)

	# ================
	# test_a_missing_later_row_refuses
	# ================
	def test_a_missing_later_row_refuses(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, text_root = self.fixture(Path(root), drop=CODES[2])
			with self.assertRaisesRegex(ValueError, "no Styria reward row ITEM_TRADE_BLUESTONE"):
				self.run_import(later, native, output, ON)
			self.assertFalse((text_root / styria_items.ITEM_SHARD).exists())

	# ================
	# test_a_miscast_later_row_refuses
	# ================
	def test_a_miscast_later_row_refuses(self):
		def miscast(row):
			if row[items.CODENAME] == CODES[0]:
				row[items.TYPE_FOURTH] = "9"

		with tempfile.TemporaryDirectory() as root:
			later, native, output, text_root = self.fixture(Path(root), mutate=miscast)
			with self.assertRaisesRegex(ValueError, "is not of type 3/3/9/0"):
				self.run_import(later, native, output, ON)

	# ================
	# test_a_codename_already_in_the_tree_refuses
	# ================
	def test_a_codename_already_in_the_tree_refuses(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, text_root = self.fixture(Path(root))
			dupe = ["0"] * items.ITEM_COLUMNS
			dupe[0], dupe[1], dupe[2] = "1", "6000", CODES[3]
			(text_root / "itemdata_5000.txt").write_bytes(utf16([dupe]))
			with self.assertRaisesRegex(ValueError, "already stands in the item tables"):
				self.run_import(later, native, output, ON)


if __name__ == "__main__":
	unittest.main()
