"""
===========================================================================

test_pet_import.py - the later clients' pet import

Synthetic rows cover the default-on switch, kind classification,
selection (existing, fellow kinds on their lane, deferred unknown
prefixes, skipped kinds without a scroll or a model), identity
retention and reassignment, and the pinned-archive source's import,
refusal and removal, all without a real client tree.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import later_tables  # noqa: E402
import hashlib
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
import unittest.mock

import alexandria_npcs as characters  # noqa: E402
import avatar_archive  # noqa: E402
import cap110_items as items  # noqa: E402
import pet_import as importer  # noqa: E402
import pet_rows  # noqa: E402

# A later client's characterdata width (VSRO and v1.696 alike).
CHAR_COLUMNS = characters.VSRO_CHARACTER_COLUMNS
# The v1.150 column the NPC model builder reads the material kind from
# (buildNpcModelAssets.mjs): later column 93, shifted by the skill pairs.
MATERIAL_KIND_COLUMN = 109


# ================
# char_row / scroll
# ================
def char_row(code, identity, level="1", model="cos\\newpet.bsr"):
	row = ["0"] * CHAR_COLUMNS
	row[characters.ID_COLUMN] = str(identity)
	row[characters.CODENAME_COLUMN] = code
	row[characters.NAME_KEY_COLUMN] = "SN_" + code
	row[characters.MODEL_COLUMN] = model
	row[characters.LEVEL_COLUMN] = level
	return row


def scroll(code, identity, assoc):
	row = ["0"] * items.ITEM_COLUMNS
	row[items.SERVICE] = "1"
	row[items.IDENTITY] = str(identity)
	row[items.CODENAME] = code
	row[items.NAME_KEY] = "SN_" + code
	row[items.SUMMONED_CHARACTER] = assoc
	return row


def native_wolf():
	return ([char_row("COS_P_WOLF_001", 6106)], [scroll("ITEM_PET_WOLF", 3000, "COS_P_WOLF")])


class SwitchTests(unittest.TestCase):
	def test_unset_is_on(self):
		with unittest.mock.patch.dict(os.environ, {}, clear=True):
			self.assertTrue(importer.enabled())

	def test_explicit_off_is_off(self):
		for value in importer.FLAG_OFF:
			with unittest.mock.patch.dict(os.environ, {importer.FLAG: value}):
				self.assertFalse(importer.enabled())

	def test_unreadable_value_is_refused(self):
		with unittest.mock.patch.dict(os.environ, {importer.FLAG: "sometimes"}):
			with self.assertRaises(ValueError):
				importer.enabled()


class KindTests(unittest.TestCase):
	def test_kinds_follow_prefix_and_shape(self):
		self.assertEqual(importer.kind_of("COS_P_FOX", [char_row("COS_P_FOX_001", 1), char_row("COS_P_FOX_040", 2, "40")]), "attack")
		self.assertEqual(importer.kind_of("COS_P_DOG", [char_row("COS_P_DOG", 1)]), "pickup")
		self.assertEqual(importer.kind_of("COS_T_MULE", [char_row("COS_T_MULE", 1)]), "transport")
		self.assertEqual(importer.kind_of("COS_C_CAMEL9", [char_row("COS_C_CAMEL9", 1)]), "vehicle")
		self.assertEqual(importer.kind_of("COS_F_FAIRY", [char_row("COS_F_FAIRY", 1)]), "fellow")
		self.assertEqual(importer.kind_of("COS_X_WISP", [char_row("COS_X_WISP", 1)]), "unknown")
		self.assertEqual(importer.kind_of("COS_GUARD_CH", [char_row("COS_GUARD_CH", 1)]), "excluded")


	# ================
	# test_test_rows_are_no_kind
	# ================
	def test_test_rows_are_no_kind(self):
		self.assertEqual(importer.kind_of("COS_T_BEHEMOTH_TEST", [char_row("COS_T_BEHEMOTH_TEST", 1)]), "excluded")


class SelectionTests(unittest.TestCase):
	def select(self, later_chars, later_items, native=None, earlier=(), previous=None, missing=lambda family, rows: []):
		native_chars, native_items = native or native_wolf()
		return importer.select_pets(native_chars, native_items, set(earlier), later_chars, later_items,
			previous=previous, missing=missing)

	def test_new_pickup_kind_is_chosen_with_its_scroll(self):
		chars, item_rows, report = self.select(
			[char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")])
		self.assertEqual([row[characters.CODENAME_COLUMN] for row in chars], ["COS_P_DOG"])
		self.assertEqual([row[items.CODENAME] for row in item_rows], ["ITEM_PET_DOG"])
		kind = report["added"][0]
		self.assertEqual((kind["kind"], kind["levels"], kind["scrolls"]), ("pickup", [1], ["ITEM_PET_DOG"]))
		self.assertEqual(kind["needs"], ["petdesign entry"])

	def test_attack_kind_reports_its_level_span_and_skill_need(self):
		chars, _, report = self.select(
			[char_row("COS_P_FOX_001", 9001), char_row("COS_P_FOX_040", 9002, "40")],
			[scroll("ITEM_PET_FOX", 9003, "COS_P_FOX")])
		self.assertEqual(len(chars), 2)
		kind = report["added"][0]
		self.assertEqual((kind["kind"], kind["levels"]), ("attack", [1, 40]))
		self.assertIn("pet skill rows", kind["needs"])

	# ================
	# test_rows_join_in_the_v1150_layout
	#
	# A later client's 104-column row is written in v1.150's 120: the
	# material kind (later column 93) lands at 109, where the NPC model
	# builder reads it. Copied as-is, column 109 did not exist and the
	# manifest wrote null, which the client refuses.
	# ================
	def test_rows_join_in_the_v1150_layout(self):
		later = char_row("COS_P_FOX_001", 9001)
		later[characters.SKILL_PAIRS_AT] = "3"
		chars, _, _ = self.select([later], [scroll("ITEM_PET_FOX", 9003, "COS_P_FOX")])
		self.assertEqual(len(chars[0]), characters.NATIVE_CHARACTER_COLUMNS)
		self.assertEqual(chars[0][MATERIAL_KIND_COLUMN], "3")
		self.assertEqual((chars[0][characters.CODENAME_COLUMN], chars[0][characters.MODEL_COLUMN]), ("COS_P_FOX_001", "cos\\newpet.bsr"))

	def test_fellow_kind_rides_the_attack_lane(self):
		# Fellows are admitted like attack kinds (#3328): rows and scroll
		# import, they fight with their native rows, and the report owes
		# no design entry (the expansion's design is attack-pet only).
		chars, item_rows, report = self.select(
			[char_row("COS_F_FAIRY", 9001)], [scroll("ITEM_PET_FAIRY", 9002, "COS_F_FAIRY")])
		self.assertEqual((len(chars), len(item_rows)), (1, 1))
		kind = report["added"][0]
		self.assertEqual((kind["family"], kind["kind"]), ("COS_F_FAIRY", "fellow"))
		self.assertEqual(kind["needs"], [])

	def test_unknown_prefix_is_deferred_not_imported(self):
		# A COS_ prefix kind_of does not name waits for its lane: with
		# fellow admissible, the fallthrough must not summon it as one.
		chars, item_rows, report = self.select(
			[char_row("COS_X_WISP", 9001)], [scroll("ITEM_PET_WISP", 9002, "COS_X_WISP")])
		self.assertEqual((chars, item_rows), ([], []))
		self.assertEqual(report["deferred"][0]["family"], "COS_X_WISP")

	def test_kind_without_a_scroll_is_skipped(self):
		chars, _, report = self.select([char_row("COS_P_DOG", 9001)], [])
		self.assertEqual(chars, [])
		self.assertEqual(report["skipped"][0]["missing"], ["summon scroll"])

	def test_kind_without_a_model_is_skipped(self):
		chars, _, report = self.select(
			[char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")],
			missing=lambda family, rows: ["model"])
		self.assertEqual(chars, [])
		self.assertEqual(report["skipped"][0]["missing"], ["model"])

	def test_native_family_is_existing(self):
		chars, _, report = self.select([char_row("COS_P_WOLF_001", 6106)], [])
		self.assertEqual(chars, [])
		self.assertEqual(report["existing"], ["COS_P_WOLF"])

	def test_partial_family_imports_only_its_fresh_rows(self):
		chars, _, report = self.select(
			[char_row("COS_P_WOLF_001", 6106), char_row("COS_P_WOLF_040", 9001, "40")],
			[scroll("ITEM_PET_WOLF2", 9002, "COS_P_WOLF")])
		self.assertEqual([row[characters.CODENAME_COLUMN] for row in chars], ["COS_P_WOLF_040"])
		self.assertEqual(report["added"][0]["levels"], [40])

	def test_earlier_source_wins_the_kind(self):
		chars, _, report = self.select(
			[char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")],
			earlier={"COS_P_DOG"})
		self.assertEqual(chars, [])
		self.assertEqual(report["existing"], ["COS_P_DOG"])

	def test_reserved_identity_is_not_handed_out(self):
		# Identities already placed in the staging tree by an earlier
		# source or another importer are taken even though the native
		# tables never held them.
		chars, item_rows, _ = self.select(
			[char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")])
		plain = (chars[0][characters.ID_COLUMN], item_rows[0][items.IDENTITY])
		chars, item_rows, _ = importer.select_pets(
			*native_wolf(), set(), [char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")],
			reserved_ids={9001, 9002})
		self.assertNotEqual((chars[0][characters.ID_COLUMN], item_rows[0][items.IDENTITY]), plain)
		self.assertNotIn(int(chars[0][characters.ID_COLUMN]), (9001, 9002))
		self.assertNotIn(int(item_rows[0][items.IDENTITY]), (9001, 9002))

	def test_taken_identity_gets_a_fresh_one_and_previous_is_retained(self):
		native = ([char_row("COS_P_WOLF_001", 9001)], [])
		chars, _, _ = self.select([char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")], native=native)
		self.assertGreater(int(chars[0][characters.ID_COLUMN]), 9001)
		chars, _, _ = self.select([char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")],
			native=native, previous={"COS_P_DOG": 4242})
		self.assertEqual(chars[0][characters.ID_COLUMN], "4242")


class ArchiveTests(unittest.TestCase):
	def _tree(self, root, chars, item_rows):
		text_root = root / importer.TEXT_ROOT
		text_root.mkdir(parents=True, exist_ok=True)
		(text_root / "characterdata_5000.txt").write_bytes(importer._encode(chars))
		(text_root / "characterdata.txt").write_bytes(importer._encode([["characterdata_5000.txt"]]))
		(text_root / "itemdata_5000.txt").write_bytes(importer._encode(item_rows))
		(text_root / "itemdata.txt").write_bytes(importer._encode([["itemdata_5000.txt"]]))

	# The pets ride the archive the avatar pass reads: entries at their
	# pk2 paths under media/ and data/, the whole zip pinned by sha256.
	# Returns (zip path, pin file); the pin names the zip's real hash.
	def _archive(self, folder, entries):
		zip_path = Path(folder) / "opensro_avatars.zip"
		avatar_archive.write_archive(zip_path, entries)
		pin_path = Path(folder) / "avatars_archive_pins.json"
		pin_path.write_text(json.dumps({
			"format": avatar_archive.PIN_FORMAT,
			"sha256": hashlib.sha256(zip_path.read_bytes()).hexdigest()}), encoding="utf-8")
		return zip_path, pin_path

	def _entries(self, chars, item_rows, texts=(), mixed_case=False):
		text = "media/" + importer.tables.TEXTDATA
		shard = "CharacterData_5000.txt" if mixed_case else "characterdata_5000.txt"
		entries = {
			f"{text}characterdata.txt": importer._encode([[shard]]),
			f"{text}{shard}": importer._encode(chars),
			f"{text}itemdata.txt": importer._encode([["itemdata_5000.txt"]]),
			f"{text}itemdata_5000.txt": importer._encode(item_rows),
			"data/res/cos/newpet.bsr": b"bsr",
		}
		if texts:
			entries[f"{text}textdata_object.txt"] = importer._encode(texts)
		return entries

	def _run(self, zip_path, pin_path, native, output, env=None):
		if env is None:
			env = {importer.AVATARS_ENV: str(zip_path)}
		with unittest.mock.patch.object(importer, "AVATARS_PIN_FILE", pin_path), \
				unittest.mock.patch.dict(os.environ, env, clear=True):
			return importer.add_archive_pets(native, output)

	def test_archive_source_imports_and_unset_removes(self):
		with tempfile.TemporaryDirectory() as folder:
			base = Path(folder)
			native, output = base / "native", base / "out"
			self._tree(native, *native_wolf())
			output.mkdir()
			# Shard names in a manifest are mixed-case in the real
			# clients (CharacterData_5000.txt); selection must fold.
			zip_path, pin_path = self._archive(base, self._entries(
				[char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")], mixed_case=True))
			report = self._run(zip_path, pin_path, native, output)
			self.assertTrue(report["enabled"])
			self.assertEqual([kind["family"] for kind in report["added"]], ["COS_P_DOG"])
			text_root = output / importer.TEXT_ROOT
			self.assertTrue((text_root / importer.ARCHIVE_SOURCE["char_shard"]).exists())
			self.assertTrue((output / "Data_extracted" / "res" / "cos" / "newpet.bsr").exists())
			marker = json.loads((output / importer.ARCHIVE_SOURCE["report"]).read_text(encoding="utf-8"))
			self.assertEqual(marker["idMap"]["COS_P_DOG"]["target"], 9001)
			removed = self._run(zip_path, pin_path, native, output, env={})
			self.assertFalse(removed["enabled"])
			self.assertFalse((text_root / importer.ARCHIVE_SOURCE["char_shard"]).exists())

	# ================
	# _mall
	#
	# A native mall whose pickup tab sells two goods at 88 silk and whose
	# growth tab one at 130: an imported pet lists at its tab's price.
	# ================
	def _mall(self, root):
		text_root = root / importer.TEXT_ROOT
		none = ["-1", items.UNUSED] * 4
		goods = [["1", "11", "MALL_PET_B_SILK", "PACKAGE_A", "4"] + none,
			["1", "11", "MALL_PET_B_SILK", "PACKAGE_B", "7"] + none,
			["1", "11", "MALL_PET_A_GROWTH", "PACKAGE_C", "0"] + none]
		prices = [["1", "11", "PACKAGE_A", "2", "88"] + none, ["1", "11", "PACKAGE_B", "2", "88"] + none,
			["1", "11", "PACKAGE_C", "2", "130"] + none]
		packages = [["1", "11", str(500 + n), name, "0"] + none for n, name in enumerate(("PACKAGE_A", "PACKAGE_B", "PACKAGE_C"))]
		scraps = [["1", "11", name, "ITEM_" + name] + none for name in ("PACKAGE_A", "PACKAGE_B", "PACKAGE_C")]
		for name, rows in (("refshopgoods.txt", goods), ("refpricepolicyofitem.txt", prices),
				("refpackageitem.txt", packages), ("refscrapofpackageitem.txt", scraps)):
			(text_root / name).write_bytes(importer._encode(rows))

	# ================
	# test_an_imported_pet_sells_on_its_tab_and_leaves_with_the_import
	#
	# Owner 2026-11-27: every new pet sells. The dog's scroll lists on
	# the pickup tab past its last slot at the tab's price; the event copy
	# of the same pet does not; unsetting the archive takes the listing
	# out again.
	# ================
	def test_an_imported_pet_sells_on_its_tab_and_leaves_with_the_import(self):
		with tempfile.TemporaryDirectory() as folder:
			base = Path(folder)
			native, output = base / "native", base / "out"
			self._tree(native, *native_wolf())
			self._mall(native)
			output.mkdir()
			zip_path, pin_path = self._archive(base, self._entries(
				[char_row("COS_P_DOG", 9001)],
				[scroll("ITEM_PET_DOG", 9002, "COS_P_DOG"), scroll("ITEM_EVENT_PET_DOG", 9003, "COS_P_DOG")]))
			report = self._run(zip_path, pin_path, native, output)
			self.assertEqual(report["added"][0]["kind"], "pickup")
			self.assertEqual(report["mall"]["packages"], ["PACKAGE_ITEM_PET_DOG"])
			self.assertEqual(report["mall"]["unlisted"], [{"code": "ITEM_EVENT_PET_DOG", "reason": "event copy"}])
			text_root = output / importer.TEXT_ROOT

			def rows(name):
				return importer.tables.table_rows((text_root / name).read_bytes())

			listed = [row for row in rows("refshopgoods.txt") if row[3] == "PACKAGE_ITEM_PET_DOG"]
			self.assertEqual([row[2] for row in listed], ["MALL_PET_B_SILK"])
			self.assertEqual(listed[0][4], "8")
			price = [row for row in rows("refpricepolicyofitem.txt") if row[2] == "PACKAGE_ITEM_PET_DOG"]
			self.assertEqual([(row[3], row[4]) for row in price], [("2", "88")])
			scrap = [row for row in rows("refscrapofpackageitem.txt") if row[2] == "PACKAGE_ITEM_PET_DOG"]
			self.assertEqual([row[3] for row in scrap], ["ITEM_PET_DOG"])
			# A rebuild lists it once, not twice.
			self._run(zip_path, pin_path, native, output)
			self.assertEqual(len([row for row in rows("refshopgoods.txt") if row[3] == "PACKAGE_ITEM_PET_DOG"]), 1)
			self._run(zip_path, pin_path, native, output, env={})
			self.assertFalse([row for row in rows("refshopgoods.txt") if row[3] == "PACKAGE_ITEM_PET_DOG"])
			self.assertEqual(len(rows("refshopgoods.txt")), 3)

	# ================
	# test_a_native_scroll_nobody_sells_lists_on_its_tab
	#
	# Owner 2026-11-28: every pet is in the game. A native pickup kind
	# whose scroll no package sells lists on the pickup tab at its price;
	# one some package already sells, and an event copy, do not; a
	# rebuild lists it once.
	# ================
	def test_a_native_scroll_nobody_sells_lists_on_its_tab(self):
		with tempfile.TemporaryDirectory() as folder:
			base = Path(folder)
			native, output = base / "native", base / "out"
			chars = [char_row("COS_P_GGLIDER", 23229), char_row("COS_P_SOLD", 23230)]
			# ITEM_PACKAGE_A sells (the mall fixture's PACKAGE_A), so its
			# kind's second scroll needs no listing of its own.
			scrolls = [scroll("ITEM_COS_P_GGLIDER_SCROLL", 23300, "COS_P_GGLIDER"),
				scroll("ITEM_EVENT_COS_P_GGLIDER_SCROLL", 23301, "COS_P_GGLIDER"),
				scroll("ITEM_PACKAGE_A", 23302, "COS_P_SOLD"), scroll("ITEM_COS_P_SOLD_SILK", 23303, "COS_P_SOLD")]
			self._tree(native, chars, scrolls)
			self._mall(native)
			output.mkdir()
			goods = importer.native_scrolls(chars, scrolls)
			self.assertEqual([(row[items.CODENAME], kind, family) for row, kind, family in goods], [
				("ITEM_COS_P_GGLIDER_SCROLL", "pickup", "COS_P_GGLIDER"), ("ITEM_EVENT_COS_P_GGLIDER_SCROLL", "pickup", "COS_P_GGLIDER"),
				("ITEM_PACKAGE_A", "pickup", "COS_P_SOLD"), ("ITEM_COS_P_SOLD_SILK", "pickup", "COS_P_SOLD")])
			for _ in range(2):
				report = {"_prior": {}}
				prior = importer.read_marker(output, importer.VSRO_SOURCE)
				importer._list_in_mall(output, native, [], report, prior, importer.VSRO_SOURCE, goods)
				importer._write_pass(output, importer.VSRO_SOURCE, [], [], report)
			self.assertEqual(report["mall"]["packages"], ["PACKAGE_ITEM_COS_P_GGLIDER_SCROLL"])
			self.assertEqual(sorted((entry["code"], entry["reason"]) for entry in report["mall"]["unlisted"]), [
				("ITEM_COS_P_SOLD_SILK", "kind already sold"), ("ITEM_EVENT_COS_P_GGLIDER_SCROLL", "event copy"),
				("ITEM_PACKAGE_A", "already sold")])
			listed = [row for row in importer.tables.table_rows((output / importer.TEXT_ROOT / "refshopgoods.txt").read_bytes())
				if row[3] == "PACKAGE_ITEM_COS_P_GGLIDER_SCROLL"]
			self.assertEqual([row[2] for row in listed], ["MALL_PET_B_SILK"])

	# ================
	# test_two_sources_rebuilt_on_a_kept_tree_keep_their_slots
	#
	# Each source strips only its own earlier listings; a later source's
	# previous listings must not count toward this pass's slots or ids,
	# or every rebuild moves both sources up a slot (pc3's #4672 probe).
	# ================
	def test_two_sources_rebuilt_on_a_kept_tree_keep_their_slots(self):
		with tempfile.TemporaryDirectory() as folder:
			base = Path(folder)
			native, output = base / "native", base / "out"
			self._tree(native, *native_wolf())
			self._mall(native)
			output.mkdir()
			(output / importer.TEXT_ROOT).mkdir(parents=True)
			passes = ((importer.VSRO_SOURCE, "ITEM_PET_CAT", 9101), (importer.ARCHIVE_SOURCE, "ITEM_PET_DOG", 9102))

			def build():
				listed = []
				for source, code, identity in passes:
					prior = importer.read_marker(output, source)
					report = {"added": [{"kind": "pickup", "scrolls": [code]}]}
					importer._list_in_mall(output, native, [scroll(code, identity, "COS_P_X")], report, prior, source)
					(output / source["report"]).write_text(json.dumps({"mall": report["mall"]}), encoding="utf-8")
				text_root = output / importer.TEXT_ROOT
				goods = importer.tables.table_rows((text_root / "refshopgoods.txt").read_bytes())
				packages = importer.tables.table_rows((text_root / "refpackageitem.txt").read_bytes())
				for _, code, _ in passes:
					package = "PACKAGE_" + code
					listed.append(([row[4] for row in goods if row[3] == package],
						[row[2] for row in packages if row[3] == package]))
				return listed

			first = build()
			self.assertEqual([slots for slots, _ in first], [["8"], ["9"]])
			self.assertEqual(build(), first)
			self.assertEqual(build(), first)

	def test_an_archive_with_the_wrong_sha256_is_refused(self):
		with tempfile.TemporaryDirectory() as folder:
			base = Path(folder)
			native, output = base / "native", base / "out"
			self._tree(native, *native_wolf())
			output.mkdir()
			zip_path, pin_path = self._archive(base, self._entries(
				[char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")]))
			pin_path.write_text(json.dumps({
				"format": avatar_archive.PIN_FORMAT, "sha256": "0" * 64}), encoding="utf-8")
			with unittest.mock.patch.object(importer, "AVATARS_PIN_FILE", pin_path), \
					unittest.mock.patch.dict(os.environ, {importer.AVATARS_ENV: str(zip_path)}, clear=True):
				with self.assertRaises(SystemExit):
					importer.add_archive_pets(native, output)
			# The refusal lands before a byte is read: nothing imported.
			text_root = output / importer.TEXT_ROOT
			self.assertFalse((text_root / importer.ARCHIVE_SOURCE["char_shard"]).exists())
			self.assertFalse((output / importer.ARCHIVE_SOURCE["report"]).exists())
			self.assertFalse((output / "Data_extracted").exists())

	def test_an_archive_without_pet_tables_imports_nothing(self):
		with tempfile.TemporaryDirectory() as folder:
			base = Path(folder)
			native, output = base / "native", base / "out"
			self._tree(native, *native_wolf())
			output.mkdir()
			zip_path, pin_path = self._archive(base, {"data/res/cos/newpet.bsr": b"bsr"})
			report = self._run(zip_path, pin_path, native, output)
			self.assertFalse(report["enabled"])
			self.assertFalse((output / importer.TEXT_ROOT / importer.ARCHIVE_SOURCE["char_shard"]).exists())

	def test_shard_identity_used_by_an_earlier_import_is_reserved(self):
		with tempfile.TemporaryDirectory() as folder:
			base = Path(folder)
			native, output = base / "native", base / "out"
			self._tree(native, *native_wolf())
			text_root = output / importer.TEXT_ROOT
			text_root.mkdir(parents=True)
			# An earlier import already placed identity 9001 in the tree.
			(text_root / "characterdata_7777.txt").write_bytes(importer._encode([char_row("COS_P_CAT", 9001)]))
			zip_path, pin_path = self._archive(base, self._entries(
				[char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")]))
			self._run(zip_path, pin_path, native, output)
			marker = json.loads((output / importer.ARCHIVE_SOURCE["report"]).read_text(encoding="utf-8"))
			self.assertNotEqual(marker["idMap"]["COS_P_DOG"]["target"], 9001)

	def test_published_ids_are_reserved_before_their_import_runs(self):
		# r201: the pet import ran before the Arabia import wrote its shard,
		# and a fresh pet took a published Arabia id. The pin keeps it, and
		# keeps a pet's own released ids though the source numbers it anew.
		def pick(pinned):
			chars, scrolls, _ = importer.select_pets([], [], set(), [char_row("COS_P_DOG", 50147)],
				[scroll("ITEM_PET_DOG", 50148, "COS_P_DOG")], pinned=pinned)
			return int(chars[0][characters.ID_COLUMN]), int(scrolls[0][items.IDENTITY])
		self.assertEqual(pick({}), (50147, 50148))
		dog, _ = pick({"MOB_SD_ACHNISH": 50147})
		self.assertNotEqual(dog, 50147)
		self.assertEqual(pick({"COS_P_DOG": 7001, "ITEM_PET_DOG": 7002}), (7001, 7002))

	def test_scroll_name_text_is_merged_with_the_kind_name(self):
		with tempfile.TemporaryDirectory() as folder:
			base = Path(folder)
			native, output = base / "native", base / "out"
			self._tree(native, *native_wolf())
			texts = [["1", "SN_COS_P_DOG", "k", "", "", "Dog", "Dog", "a", "b", "c"],
				["1", "SN_ITEM_PET_DOG", "k", "", "", "Dog scroll", "Dog scroll", "a", "b", "c"]]
			zip_path, pin_path = self._archive(base, self._entries(
				[char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")], texts))
			report = self._run(zip_path, pin_path, native, output)
			self.assertEqual(report["missingNameText"], [])
			name_rows = importer.characters.translation_rows((output / importer.TEXT_ROOT / "textdataname.txt").read_bytes())
			self.assertTrue({"SN_COS_P_DOG", "SN_ITEM_PET_DOG"} <= {row[1] for row in name_rows if len(row) > 1})

	def test_a_doubled_underscore_name_key_takes_its_single_underscore_title(self):
		# #4857: v1.696 keys its Dark Griffin event scroll ..._ASS__GRYPHON_A
		# and ships text only under ..._ASS_GRYPHON_A; untitled, the item
		# stops the build's title check.
		with tempfile.TemporaryDirectory() as folder:
			base = Path(folder)
			native, output = base / "native", base / "out"
			self._tree(native, *native_wolf())
			texts = [["1", "SN_COS_P_DOG", "k", "", "", "Dog", "Dog", "a", "b", "c"],
				["1", "SN_ITEM_PET_DOG", "k", "", "", "Dog scroll", "Dog scroll", "a", "b", "c"]]
			zip_path, pin_path = self._archive(base, self._entries(
				[char_row("COS_P_DOG", 9001)],
				[scroll("ITEM_PET_DOG", 9002, "COS_P_DOG"), scroll("ITEM_PET__DOG", 9003, "COS_P_DOG")], texts))
			report = self._run(zip_path, pin_path, native, output)
			self.assertEqual(report["missingNameText"], [])
			self.assertEqual(report["nameTextTypos"], {"SN_ITEM_PET__DOG": "SN_ITEM_PET_DOG"})
			name_rows = importer.characters.translation_rows((output / importer.TEXT_ROOT / "textdataname.txt").read_bytes())
			by_key = {row[1]: row[2:] for row in name_rows if len(row) > 1}
			self.assertEqual(by_key.get("SN_ITEM_PET__DOG"), by_key["SN_ITEM_PET_DOG"])

	def test_v1696_name_text_behind_a_shard_index_is_found(self):
		# v1.696 ships textdata_object.txt as an index of TextData_Object_<n>
		# parts, and each part's rows carry 17 columns: a numeric text id
		# after the service cell, "0" where a translation is absent.
		with tempfile.TemporaryDirectory() as folder:
			base = Path(folder)
			native, output = base / "native", base / "out"
			self._tree(native, *native_wolf())
			def v1696(key, english):
				return ["1", "1400", key, "k", "0", "0", "0", "0", "0", english, "0", "0", "0", english, english, english, english]
			texts = [v1696("SN_COS_P_DOG", "Dog"), v1696("SN_ITEM_PET_DOG", "Dog scroll")]
			entries = self._entries([char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")])
			text = "media/" + importer.tables.TEXTDATA
			entries[f"{text}textdata_object.txt"] = importer._encode([["TextData_Object_1000.txt"]])
			entries[f"{text}textdata_object_1000.txt"] = importer._encode(texts)
			zip_path, pin_path = self._archive(base, entries)
			report = self._run(zip_path, pin_path, native, output)
			self.assertEqual(report["missingNameText"], [])
			name_rows = importer.characters.translation_rows((output / importer.TEXT_ROOT / "textdataname.txt").read_bytes())
			english = {row[1]: row[8] for row in name_rows if len(row) > 8}
			self.assertEqual((english.get("SN_COS_P_DOG"), english.get("SN_ITEM_PET_DOG")), ("Dog", "Dog scroll"))

	def test_a_pass_that_selects_nothing_drops_its_manifest_lines(self):
		with tempfile.TemporaryDirectory() as folder:
			base = Path(folder)
			native, output = base / "native", base / "out"
			self._tree(native, *native_wolf())
			zip_path, pin_path = self._archive(base, self._entries(
				[char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")]))
			self._run(zip_path, pin_path, native, output)
			self._archive(base, self._entries([], []))
			self._run(zip_path, pin_path, native, output)
			text_root = output / importer.TEXT_ROOT
			for manifest_name, shard in (("characterdata.txt", importer.ARCHIVE_SOURCE["char_shard"]),
					("itemdata.txt", importer.ARCHIVE_SOURCE["item_shard"])):
				names = [row[0] for row in importer.tables.table_rows((text_root / manifest_name).read_bytes()) if row]
				self.assertNotIn(shard, names)
				self.assertFalse((text_root / shard).exists())


class SkillStagingTests(unittest.TestCase):
	"""DefaultSkillIDs on staged pets pull their skilldata rows with them."""

	@staticmethod
	def _skill_row(identity, code, next_id="0"):
		row = ["1"] + ["0"] * 117
		row[1] = str(identity)
		row[3] = code
		row[6] = str(next_id)
		return row

	def test_stage_pet_skills_writes_the_shard_and_remaps_ids(self):
		with tempfile.TemporaryDirectory() as folder:
			text_root = Path(folder) / importer.TEXT_ROOT
			text_root.mkdir(parents=True)
			(text_root / "skilldata.txt").write_bytes(importer._encode([["skilldata_5000.txt"]]))
			(text_root / "skilldata_5000.txt").write_bytes(importer._encode([
				self._skill_row(100, "PSKILL_NATIVE")]))
			# Native layout: DefaultSkillIDs start at column 89.
			char = ["0"] * characters.NATIVE_CHARACTER_COLUMNS
			char[characters.ID_COLUMN] = "1"
			char[characters.CODENAME_COLUMN] = "COS_P_FOX_001"
			char[characters.NATIVE_DEFAULT_SKILL_AT] = "5001"
			client = {5001: self._skill_row(5001, "PSKILL_P_FOX_01_ATTACK01")}

			chars, skills = importer.stage_pet_skills(
				[client[5001]], text_root, "skilldata_140000.txt", [char])
			self.assertEqual(set(skills), {"PSKILL_P_FOX_01_ATTACK01"})
			assigned = skills["PSKILL_P_FOX_01_ATTACK01"]
			self.assertEqual(chars[0][characters.NATIVE_DEFAULT_SKILL_AT], str(assigned))
			shard = importer.tables.table_rows((text_root / "skilldata_140000.txt").read_bytes())
			self.assertEqual([(row[1], row[3]) for row in shard], [(str(assigned), "PSKILL_P_FOX_01_ATTACK01")])
			manifest = [row[0] for row in importer.tables.table_rows((text_root / "skilldata.txt").read_bytes()) if row]
			self.assertIn("skilldata_140000.txt", manifest)

	def test_stage_pet_skills_drops_the_shard_when_nothing_is_needed(self):
		with tempfile.TemporaryDirectory() as folder:
			text_root = Path(folder) / importer.TEXT_ROOT
			text_root.mkdir(parents=True)
			(text_root / "skilldata.txt").write_bytes(importer._encode([
				["skilldata_5000.txt"], ["skilldata_140000.txt"]]))
			(text_root / "skilldata_140000.txt").write_bytes(importer._encode([
				self._skill_row(5001, "PSKILL_STALE")]))
			char = ["0"] * characters.NATIVE_CHARACTER_COLUMNS
			chars, skills = importer.stage_pet_skills(
				[], text_root, "skilldata_140000.txt", [char])
			self.assertEqual((skills, chars[0][characters.NATIVE_DEFAULT_SKILL_AT]), ({}, "0"))
			self.assertFalse((text_root / "skilldata_140000.txt").exists())
			manifest = [row[0] for row in importer.tables.table_rows((text_root / "skilldata.txt").read_bytes()) if row]
			self.assertNotIn("skilldata_140000.txt", manifest)


class PetIconTest(unittest.TestCase):
	"""An added kind's icons come from the later source when the native tree lacks them."""

	@staticmethod
	def _row(icon):
		row = [""] * (items.ICON + 1)
		row[items.ICON] = icon
		return row

	def test_missing_icons_are_copied_native_ones_kept_and_absent_ones_reported(self):
		with tempfile.TemporaryDirectory() as folder:
			native, output = Path(folder) / "native", Path(folder) / "out"
			(native / importer.ICON_ROOT / "cos").mkdir(parents=True)
			(native / importer.ICON_ROOT / "cos" / "native_pet.ddj").write_bytes(b"native")
			later = {"cos/item_cos_p_event_winter_snowman.ddj": b"snowman"}
			report = {}
			rows = [
				self._row("cos\\item_cos_p_event_winter_snowman.ddj"),
				self._row("cos\\NATIVE_PET.ddj"),
				self._row("cos\\nowhere.ddj"),
				self._row(items.UNUSED),
			]
			importer.copy_icon_files(native, output, rows, report, later.get)
			self.assertEqual(report["icons"], ["cos/item_cos_p_event_winter_snowman.ddj"])
			self.assertEqual(report["missingIcons"], ["cos/nowhere.ddj"])
			copied = output / importer.ICON_ROOT / "cos" / "item_cos_p_event_winter_snowman.ddj"
			self.assertEqual(copied.read_bytes(), b"snowman")
			self.assertFalse((output / importer.ICON_ROOT / "cos" / "native_pet.ddj").exists())

	def test_a_rebuild_removes_what_the_last_pass_copied(self):
		with tempfile.TemporaryDirectory() as folder:
			native, output = Path(folder) / "native", Path(folder) / "out"
			first = {}
			importer.copy_icon_files(native, output, [self._row("cos\\gone.ddj")], first, {"cos/gone.ddj": b"x"}.get)
			self.assertTrue((output / importer.ICON_ROOT / "cos" / "gone.ddj").exists())
			second = {"_prior": first}
			importer.copy_icon_files(native, output, [], second, {}.get)
			self.assertFalse((output / importer.ICON_ROOT / "cos" / "gone.ddj").exists())
			self.assertEqual(second["icons"], [])
			self.assertNotIn("_prior", second)


if __name__ == "__main__":
	unittest.main()
