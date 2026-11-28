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
	# The VSRO rows trade, sell, store and drop (row 24415's cells).
	for column in (nasrun.CAN_TRADE, nasrun.CAN_SELL, nasrun.CAN_BORROW, nasrun.CAN_DROP):
		row[column] = "1"
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
			# The Spirits and the gear are staged bound; the jewel keeps its cells.
			cells = (nasrun.CAN_TRADE, nasrun.CAN_SELL, nasrun.CAN_BORROW, nasrun.CAN_DROP)
			for line in shard.splitlines():
				row = line.split("\t")
				bound = row[items.TYPE_FIRST:items.TYPE_FOURTH + 1] in nasrun.BOUND_TYPES
				self.assertEqual([row[c] for c in cells], ["0" if bound else "1"] * 4, row[items.CODENAME])
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


# ================
# skill_row
#
# One 118-column later skill row as the VSRO client authors a lent
# Nasrun skill: an icon and its names, no chain and no prerequisite.
# ================
def skill_row(code, identity):
	row = ["0"] * nasrun.SKILL_COLUMNS
	row[nasrun.SKILL_SERVICE], row[nasrun.SKILL_ID], row[nasrun.SKILL_CODENAME] = "1", str(identity), code
	# The colours and ranks share one name, as in the VSRO client.
	stem = code[:-len("_01")].replace("_YELLOW", "").replace("_BLUE", "")
	row[nasrun.SKILL_ICON] = "skill\\etc\\" + code.lower() + ".ddj"
	row[nasrun.SKILL_NAME_KEY], row[nasrun.SKILL_DESCRIPTION_KEY] = "SN_" + stem, "SN_" + stem + "_TT_DESC"
	# The VSRO cells: every rank at basic level 1, no cooldown group.
	row[nasrun.SKILL_BASIC_LEVEL], row[nasrun.SKILL_COOLDOWN_GROUP] = "1", "0"
	return row


# ================
# NasrunSkillImportTest
#
# The lent skills' pass: eleven rows in their own skill shard under
# marker-kept ids above every skill the tree holds, the manifest line
# last, their names; off gives back the tables byte for byte.
# ================
class NasrunSkillImportTest(unittest.TestCase):
	NATIVE_SKILL = 31200
	LATER_SKILL = 31142

	# ================
	# fixture
	# ================
	def fixture(self, base, drop=None, mutate=None):
		vsro, native, output = base / "vsro", base / "native", base / "output"
		text_root = output / nasrun.TEXT_ROOT
		text_root.mkdir(parents=True)
		(native / nasrun.TEXT_ROOT).mkdir(parents=True)
		vsro.mkdir()
		rows, texts = [], set()
		for offset, code in enumerate(nasrun.SKILL_CODES):
			if code == drop:
				continue
			row = skill_row(code, self.LATER_SKILL + offset)
			if mutate:
				mutate(row)
			rows.append(row)
			texts |= {row[nasrun.SKILL_NAME_KEY], row[nasrun.SKILL_DESCRIPTION_KEY]}
		decoy = skill_row("SKILL_EVENT_NASRUN_ECCENTRICDEMON_01", 31141)
		self.media = archive_bytes({
			"skilldata.txt": utf16([["skilldata_35000.txt"]]),
			"skilldata_35000.txt": utf16([decoy] + rows),
			"textdata_equip&skill.txt": utf16([text_row(key) for key in sorted(texts)]),
		})
		native_skill = ["0"] * nasrun.SKILL_COLUMNS
		native_skill[0], native_skill[1], native_skill[3] = "1", str(self.NATIVE_SKILL), "SKILL_NATIVE_THING"
		(native / nasrun.TEXT_ROOT / "skilldata_5000.txt").write_bytes(utf16([native_skill]))
		(text_root / "skilldata.txt").write_bytes(utf16([["skilldata_5000.txt"], ["skilldata_110000.txt"]]))
		(text_root / nasrun.NAME_TABLE).write_bytes(utf16([["1", "SN_NATIVE_A", "", "", "", "", "", "", "Native A"]]))
		return vsro, native, output, text_root

	# ================
	# run_import
	# ================
	def run_import(self, vsro, native, output, flag="on"):
		data = self.media
		index = {shared.normalized_path(entry.path): entry for entry in sro_pk2.read_directory(data)}
		self.icons = set()

		def open_archives(stack, root, source):
			return {"Media": (data, index)}, {"Media": "fixture"}

		def copy_resource_closure(pending, native_root, output_root, archives):
			self.icons = set(pending)
			return []

		with mock.patch.dict(os.environ, {"SRO_NASRUN": flag}), 				mock.patch.object(avatars, "source_present", return_value=True), 				mock.patch.object(avatars, "open_archives", open_archives), 				mock.patch.object(shared, "copy_resource_closure", copy_resource_closure):
			return nasrun.add_nasrun_skills(vsro, native, output)

	# ================
	# test_import_stages_the_eleven_skills
	# ================
	def test_import_stages_the_eleven_skills(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root = self.fixture(Path(root))
			summary = self.run_import(vsro, native, output)
			self.assertEqual(sorted(summary["skills"]), sorted(nasrun.SKILL_CODES))
			ids = sorted(summary["skills"].values())
			# Above the native table, never the later client's own ids.
			self.assertGreater(ids[0], self.NATIVE_SKILL)
			self.assertEqual(len(set(ids)), len(nasrun.SKILL_CODES))
			shard = (text_root / nasrun.SKILL_SHARD).read_bytes().decode("utf-16")
			self.assertNotIn("SKILL_EVENT_NASRUN", shard)
			# The Ignore ranks carry the later official level and shared
			# cooldown group, whatever the VSRO cells hold (#3797 review).
			staged = {row[nasrun.SKILL_CODENAME]: row for row in (line.split("\t") for line in shard.splitlines())}
			for code, level in (("SKILL_MALL_NASRUN_MOBDEFENCEIGNORE_01", "1"), ("SKILL_MALL_NASRUN_MOBDEFENCEIGNORE_02", "2")):
				self.assertEqual(staged[code][nasrun.SKILL_BASIC_LEVEL], level, code)
				self.assertEqual(staged[code][nasrun.SKILL_COOLDOWN_GROUP], "-872415232", code)
			self.assertEqual(staged["SKILL_MALL_NASRUN_ECCENTRICDEMON_01"][nasrun.SKILL_BASIC_LEVEL], "1")
			self.assertNotIn("	" + str(self.LATER_SKILL) + "	", shard)
			manifest = [line for line in (text_root / "skilldata.txt").read_bytes().decode("utf-16").splitlines() if line]
			self.assertEqual(manifest, ["skilldata_5000.txt", "skilldata_110000.txt", nasrun.SKILL_SHARD])
			names = (text_root / nasrun.NAME_TABLE).read_bytes().decode("utf-16")
			self.assertIn("SN_SKILL_MALL_NASRUN_MOBDEFENCEIGNORE	", names)
			# The skills' icons, and the worn Spirit's active edge (#4384).
			edge = ("Media", nasrun.SPIRIT_EDGE_SHEET)
			self.assertIn(edge, self.icons)
			skill_icons = self.icons - {edge}
			self.assertEqual(len(skill_icons), len(nasrun.SKILL_CODES))
			self.assertTrue(all(archive == "Media" and path.startswith("icon/skill/etc/") for archive, path in skill_icons))
			again = self.run_import(vsro, native, output)
			self.assertEqual(again["skills"], summary["skills"])
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
			self.assertFalse((output / nasrun.SKILL_MARKER).exists())
			self.assertEqual({path.name: path.read_bytes() for path in text_root.iterdir()}, before)

	# ================
	# test_missing_or_chained_skill_refuses
	# ================
	def test_missing_or_chained_skill_refuses(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, _ = self.fixture(Path(root), drop="SKILL_MALL_NASRUN_MOBDEFENCEIGNORE_02")
			with self.assertRaisesRegex(ValueError, "no Nasrun skill"):
				self.run_import(vsro, native, output)

		def chain(row):
			row[nasrun.SKILL_CHAIN_NEXT] = "31143"

		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, _ = self.fixture(Path(root), mutate=chain)
			with self.assertRaisesRegex(ValueError, "chains to or requires"):
				self.run_import(vsro, native, output)


if __name__ == "__main__":
	unittest.main()


# ================
# SkillEffectMergeTest
#
# The lent skills' effect rows join the tree's skilleffect.txt in their own
# sections, the transformation keeps its own animation group,
# and a rerun swaps the previous pass's rows for the new ones (#4407).
# ================
class SkillEffectMergeTest(unittest.TestCase):
	# ================
	# stage
	#
	# A skilleffectset row in the native layout: its stage object in the
	# directory (17) and name (18) cells.
	# ================
	@staticmethod
	def stage(first, key, step, directory, name):
		cells = [first, key, step] + ["xxx"] * 25
		cells[17], cells[18] = directory, name
		return "\t".join(cells) + "\r\n"

	TREE = (
		"#section\tskillaniset2\r\n"
		"1\tnative\tSKILL_NATIVE\t0\tFALSE\t0\tDEFAULT\tnone\r\n"
		"#section\tskilleffectset\r\n"
		+ stage.__func__("-", "SKILL_NATIVE", "SHOT", "system\\", "native.efp")
	)
	LATER = (
		"#section\tskillaniset2\r\n"
		"1\tlent\tSKILL_LENT\t0\tFALSE\t0\tAVATAR_NASRUN1\tANI_READY01\r\n"
		"1\tother\tSKILL_OTHER\t0\tFALSE\t0\tDEFAULT\tnone\r\n"
		"#section\tskilleffectset\r\n"
		+ stage.__func__("lent", "SKILL_LENT", "READY", "system\\", "avatar_nasrun01_skill01_ready01.efp")
		+ stage.__func__("-", "AVATAR_LENT", "ACT_L", "system\\", "avatar_nasrun01_skill01_body01.efp")
		+ stage.__func__("-", "SKILL_OTHER", "SHOT", "system\\", "other.efp")
	)

	# ================
	# test_rows_join_their_sections
	# ================
	def test_rows_join_their_sections(self):
		keys = {"SKILL_LENT", "AVATAR_LENT"}
		merged, efps = nasrun.merge_skill_effects(self.TREE, self.LATER, keys, set())
		lines = merged.split("\r\n")
		aniset, effectset = lines.index("#section\tskillaniset2"), lines.index("#section\tskilleffectset")
		self.assertIn("1\tlent\tSKILL_LENT\t0\tFALSE\t0\tAVATAR_NASRUN1\tANI_READY01", lines[aniset:effectset])
		self.assertIn(self.stage("-", "AVATAR_LENT", "ACT_L", "system\\", "avatar_nasrun01_skill01_body01.efp").rstrip("\r\n"),
			lines[effectset:])
		self.assertNotIn("SKILL_OTHER", merged)
		# The files are the stage cells' directory + name, wherever the row keeps them.
		self.assertEqual(efps, [("Particles", "system/avatar_nasrun01_skill01_body01.efp"),
			("Particles", "system/avatar_nasrun01_skill01_ready01.efp")])
		# A rerun takes the previous rows out before adding them again.
		again, _ = nasrun.merge_skill_effects(merged, self.LATER, keys, keys)
		self.assertEqual(again, merged)
		# Off: the previous rows leave, the native ones stay.
		off, _ = nasrun.merge_skill_effects(merged, "", set(), keys)
		self.assertEqual(off, self.TREE)


class ItemEffectTest(unittest.TestCase):
	"""The Spirits' presentation rows of the later itemeffect.txt."""

	CODES = {"ITEM_MALL_AVATAR_M_NASRUN"}
	TABLE = "\n".join([
		"//item presentation\t\t\t",
		"#section\titemcodename\t\t",
		"//SkillEffect Codename\tSkillEffect Codename\tstate\t",
		"ITEM_MALL_AVATAR_M_NASRUN\tITEMSKILL_MALL_AVATAR_NASRUN_AWAKE\t2\t",
		"ITEM_EVENT_AVATAR_M_NASRUN\tITEMSKILL_EVENT_AVATAR_NASRUN_EQUIP\t\t",
		"ITEM_MALL_AVATAR_M_NASRUN\tITEMSKILL_MALL_AVATAR_NASRUN_EQUIP\t\t",
		"ITEM_MALL_AVATAR_M_NASRUN\tITEMSKILL_MALL_AVATAR_NASRUN_TIMEOUT\t3\t",
		"ITEM_MALL_AVATAR_M_NASRUN\tITEMSKILL_MALL_AVATAR_NASRUN_UNEQUIP\t1\t",
		"#section\titemtypeid\t\t\t\t\t",
		"//  TID 1\tTID 2\tTID 3\tTID 4\tSkillEffect Codename\tstate",
	])

	def test_each_spirit_names_its_four_transitions(self):
		self.assertEqual(nasrun.item_effects(self.TABLE, self.CODES), {
			"ITEM_MALL_AVATAR_M_NASRUN": {
				"equip": "ITEMSKILL_MALL_AVATAR_NASRUN_EQUIP",
				"unequip": "ITEMSKILL_MALL_AVATAR_NASRUN_UNEQUIP",
				"awake": "ITEMSKILL_MALL_AVATAR_NASRUN_AWAKE",
				"expire": "ITEMSKILL_MALL_AVATAR_NASRUN_TIMEOUT",
			},
		})

	def test_a_partial_repeated_or_unknown_row_refuses(self):
		partial = self.TABLE.replace("ITEM_MALL_AVATAR_M_NASRUN\tITEMSKILL_MALL_AVATAR_NASRUN_TIMEOUT\t3\t\n", "")
		with self.assertRaises(ValueError):
			nasrun.item_effects(partial, self.CODES)
		with self.assertRaises(ValueError):
			nasrun.item_effects(self.TABLE.replace("NASRUN_AWAKE\t2", "NASRUN_AWAKE\t9"), self.CODES)
		with self.assertRaises(ValueError):
			nasrun.item_effects(self.TABLE.replace("NASRUN_AWAKE\t2", "NASRUN_AWAKE\t1"), self.CODES)
