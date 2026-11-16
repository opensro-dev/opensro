"""
===========================================================================
test_nasrun_import.py - the Devil's Spirit (Nasrun) rows import's contract.

Behind SRO_NASRUN the import stages exactly the fifteen Nasrun rows of
the VSRO client in their own itemdata shard under marker-kept
identities, with their name texts; a switch off gives back the item
manifest and the name table byte for byte. A missing or miscast row,
or a jewel that would reinforce a v1.150 item class, refuses.
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
import mall_avatar_import as avatars  # noqa: E402
import nasrun_import as nasrun  # noqa: E402
import sro_pk2  # noqa: E402

# The jewel's class parameter in the VSRO client: class 14 in the top byte.
SPIRIT_CLASS_PARAM = str(nasrun.SPIRIT_CLASS << 24)
LATER_FIRST_ID = 24405
NATIVE_ID = 5000


# ================
# utf16
# ================
def utf16(rows, newline="\r\n"):
	return b"\xff\xfe" + "".join("\t".join(row) + newline for row in rows).encode("utf-16-le")


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


# ================
# item_row
#
# One later item row as the VSRO client authors a Nasrun row: no worn
# model, a drop model and an icon, its names under SN_<code>.
# ================
def item_row(code, identity, kind):
	row = ["0"] * items.ITEM_COLUMNS
	row[items.SERVICE], row[items.IDENTITY], row[items.CODENAME] = "1", str(identity), code
	row[items.NAME_KEY], row[items.DESCRIPTION_KEY] = "SN_" + code, "SN_" + code + "_TT_DESC"
	row[items.TYPE_FIRST:items.TYPE_FOURTH + 1] = kind
	row[items.MODEL] = items.UNUSED
	row[items.DROP_MODEL] = "item\\etc\\drop_ch_eventbox.bsr"
	row[items.ICON] = "item\\etc\\" + code.lower() + ".ddj"
	if kind is nasrun.JEWEL_TYPE:
		row[nasrun.ELIXIR_CLASS_PARAMS[0]] = SPIRIT_CLASS_PARAM
	return row


# ================
# text_row
#
# One VSRO text row: service, key, then thirteen language cells, the
# English one at 8.
# ================
def text_row(key):
	cells = ["1", key] + [""] * 13
	cells[8] = "English " + key
	return cells


# ================
# NasrunImportTest
# ================
class NasrunImportTest(unittest.TestCase):
	# ================
	# fixture
	#
	# A scratch world: a fake VSRO Media.pk2 whose itemdata carries the
	# fifteen rows plus an event copy (a decoy), the matching name texts,
	# and an output tree with one native item and a name table whose
	# rows mix CRLF and LF endings as the shipped one does.
	# ================
	def fixture(self, base, drop=None, mutate=None):
		vsro, native, output = base / "vsro", base / "native", base / "output"
		text_root = output / nasrun.TEXT_ROOT
		text_root.mkdir(parents=True)
		vsro.mkdir()
		native.mkdir()
		rows, texts = [], []
		for offset, (code, kind) in enumerate(nasrun.ROWS):
			if code == drop:
				continue
			row = item_row(code, LATER_FIRST_ID + offset, kind)
			if mutate:
				mutate(row)
			rows.append(row)
			texts += [text_row(row[items.NAME_KEY]), text_row(row[items.DESCRIPTION_KEY])]
		decoy = item_row("ITEM_EVENT_AVATAR_M_NASRUN", 24413, nasrun.WEARABLE_TYPE)
		media = {
			"itemdata.txt": utf16([["itemdata_25000.txt"]]),
			"itemdata_25000.txt": utf16(rows + [decoy]),
			"textdata_object.txt": utf16(texts),
		}
		self.media = archive_bytes(media)
		native_row = ["0"] * items.ITEM_COLUMNS
		native_row[0], native_row[1], native_row[2] = "1", str(NATIVE_ID), "ITEM_NATIVE_THING"
		(text_root / "itemdata.txt").write_bytes(utf16([["itemdata_5000.txt"]]))
		(text_root / "itemdata_5000.txt").write_bytes(utf16([native_row]))
		names = utf16([["1", "SN_NATIVE_A", "", "", "", "", "", "", "Native A"]]) + utf16(
			[["1", "SN_NATIVE_B", "", "", "", "", "", "", "Native B"]], newline="\n")[2:]
		(text_root / nasrun.NAME_TABLE).write_bytes(names)
		return vsro, native, output, text_root

	# ================
	# run_import
	#
	# One import pass on the fixture's archive, with the VSRO client
	# present and the resource copy recorded instead of performed (its
	# closure walk is the cap-110 import's, tested there).
	# ================
	def run_import(self, vsro, native, output, flag="on"):
		data = self.media
		index = {shared.normalized_path(entry.path): entry for entry in sro_pk2.read_directory(data)}
		self.copied = []

		def open_archives(stack, root, source):
			return {"Media": (data, index)}, {"Media": "fixture"}

		def copy_asset_closure(rows, native_root, output_root, archives, worn=True):
			self.assertFalse(worn)
			self.copied = [row[items.CODENAME] for row in rows]
			return []

		with mock.patch.dict(os.environ, {"SRO_NASRUN": flag}), \
				mock.patch.object(avatars, "source_present", return_value=True), \
				mock.patch.object(avatars, "open_archives", open_archives), \
				mock.patch.object(shared, "copy_asset_closure", copy_asset_closure):
			return nasrun.add_nasrun(vsro, native, output)

	# ================
	# test_import_stages_the_fifteen_rows
	# ================
	def test_import_stages_the_fifteen_rows(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root = self.fixture(Path(root))
			summary = self.run_import(vsro, native, output)
			codes = [code for code, _ in nasrun.ROWS]
			self.assertEqual(sorted(summary["items"]), sorted(codes))
			self.assertNotIn("ITEM_EVENT_AVATAR_M_NASRUN", summary["items"])
			self.assertEqual(sorted(self.copied), sorted(codes))
			# Fresh identities above every published row, never the later
			# client's own numbers.
			self.assertTrue(all(identity > NATIVE_ID for identity in summary["items"].values()))
			self.assertTrue(all(identity < LATER_FIRST_ID for identity in summary["items"].values()))
			shard = (text_root / nasrun.ITEM_SHARD).read_bytes().decode("utf-16")
			self.assertIn("ITEM_MALL_AVATAR_M_NASRUN\t", shard)
			self.assertNotIn(str(LATER_FIRST_ID), shard)
			manifest = (text_root / "itemdata.txt").read_bytes().decode("utf-16")
			self.assertIn(nasrun.ITEM_SHARD, manifest)
			names = (text_root / nasrun.NAME_TABLE).read_bytes().decode("utf-16")
			self.assertIn("SN_ITEM_MALL_NASRUN_EXTENSION\t", names)
			self.assertEqual(len(summary["texts"]), 2 * len(nasrun.ROWS))
			marker = json.loads((output / nasrun.MARKER).read_text(encoding="utf-8"))
			self.assertEqual(marker["items"], summary["items"])
			# A rebuild keeps the published identities and adds no text twice.
			again = self.run_import(vsro, native, output)
			self.assertEqual(again["items"], summary["items"])
			self.assertEqual((text_root / nasrun.NAME_TABLE).read_bytes().decode("utf-16"), names)

	# ================
	# test_off_gives_back_the_tables
	# ================
	def test_off_gives_back_the_tables(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root = self.fixture(Path(root))
			before = {path.name: path.read_bytes() for path in text_root.iterdir()}
			self.run_import(vsro, native, output)
			self.assertEqual(self.run_import(vsro, native, output, flag=""), {"enabled": False})
			self.assertFalse((output / nasrun.MARKER).exists())
			self.assertEqual({path.name: path.read_bytes() for path in text_root.iterdir()}, before)

	# ================
	# test_absent_client_reaps_a_previous_import
	# ================
	def test_absent_client_reaps_a_previous_import(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root = self.fixture(Path(root))
			before = {path.name: path.read_bytes() for path in text_root.iterdir()}
			self.run_import(vsro, native, output)
			with mock.patch.dict(os.environ, {"SRO_NASRUN": "on"}):
				summary = nasrun.add_nasrun(vsro, native, output)
			self.assertFalse(summary["enabled"])
			self.assertEqual({path.name: path.read_bytes() for path in text_root.iterdir()}, before)

	# ================
	# test_missing_or_miscast_row_refuses
	# ================
	def test_missing_or_miscast_row_refuses(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, _ = self.fixture(Path(root), drop="ITEM_ETC_NASRUN_ARCHEMY_STONE_04")
			with self.assertRaisesRegex(ValueError, "no Nasrun row"):
				self.run_import(vsro, native, output)

		# ================
		# miscast
		# ================
		def miscast(row):
			if row[items.CODENAME] == "ITEM_MALL_AVATAR_W_NASRUN":
				row[items.TYPE_THIRD] = "13"

		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, _ = self.fixture(Path(root), mutate=miscast)
			with self.assertRaisesRegex(ValueError, "is not of type"):
				self.run_import(vsro, native, output)

	# ================
	# test_jewel_for_a_native_class_refuses
	#
	# A jewel whose class parameters named a v1.150 class (1, a sword)
	# would reinforce native items: the rows would no longer be inert.
	# ================
	def test_jewel_for_a_native_class_refuses(self):
		def widen(row):
			if row[items.CODENAME] == "ITEM_ETC_NASRUN_ARCHEMY_UPGRADE_STONE":
				row[nasrun.ELIXIR_CLASS_PARAMS[1]] = "1"

		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, _ = self.fixture(Path(root), mutate=widen)
			with self.assertRaisesRegex(ValueError, "reinforces classes"):
				self.run_import(vsro, native, output)

	# ================
	# test_unset_switch_is_off
	# ================
	def test_unset_switch_is_off(self):
		self.assertFalse(nasrun.flag_enabled({}))
		self.assertFalse(nasrun.flag_enabled({"SRO_NASRUN": "off"}))
		self.assertTrue(nasrun.flag_enabled({"SRO_NASRUN": " ON "}))


if __name__ == "__main__":
	unittest.main()
