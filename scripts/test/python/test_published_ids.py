"""
===========================================================================

test_published_ids.py - released ids stay with their codes (#3815)

A full variant build numbers its imports afresh, so a flag that adds rows
to an early importer moved every later importer's ids. These pin the rule
published_ids owns and the refresh tool that writes its pin.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import json
import tempfile
import unittest
from pathlib import Path

import cap110_items as items
import pin_published_identities as pin_tool
import published_ids
from test_cap110_items import weapon


class PublishedIdsTest(unittest.TestCase):
	# ================
	# test_a_flag_that_adds_an_early_code_moves_no_released_id
	#
	# #3815: SRO_ROC's armour took the ids an import after it had shipped.
	# Under the pin the released codes keep theirs and the newcomer goes
	# above every released id.
	# ================
	def test_a_flag_that_adds_an_early_code_moves_no_released_id(self):
		pinned = {"ITEM_HONOR_A": 501, "ITEM_HONOR_B": 502, "ITEM_STONE": 503}
		fresh = published_ids.allocate(published_ids.OBJECTS, {500}, ["ITEM_ROC", "ITEM_HONOR_A", "ITEM_HONOR_B"], {},
			pinned)
		self.assertEqual(fresh, {"ITEM_ROC": 504, "ITEM_HONOR_A": 501, "ITEM_HONOR_B": 502})
		# Without the pin a fresh build hands 501 to the newcomer.
		self.assertEqual(published_ids.allocate(published_ids.OBJECTS, {500}, ["ITEM_ROC", "ITEM_HONOR_A"], {}, {}),
			{"ITEM_ROC": 501, "ITEM_HONOR_A": 502})

	# ================
	# test_overlay_drops_a_previous_id_the_pin_gives_another_code
	# ================
	def test_overlay_drops_a_previous_id_the_pin_gives_another_code(self):
		pinned = {"A": 10, "OTHER": 11}
		held, merged = published_ids.overlay(pinned, ["A", "B"], {11}, {"A": 20, "B": 10, "C": 30})
		# 11 is already in the tree, so it is not held twice.
		self.assertEqual(held, set())
		self.assertEqual(merged, {"A": 10, "C": 30})
		held, _ = published_ids.overlay(pinned, ["A"], set(), {})
		self.assertEqual(held, {11})

	# ================
	# test_a_pinned_live_collision_stays_with_the_item
	#
	# r202 shipped five ids under a monster and a pet scroll at once, and
	# the client keeps one object per id (#3994). The refresh settles it in
	# the pin: the scroll a player can hold keeps the id, the monster moves
	# into "former" and is numbered afresh, and an unsettled pin refuses.
	# ================
	def test_a_pinned_live_collision_stays_with_the_item(self):
		pin = {"format": published_ids.PIN_FORMAT, "characters": {"MOB_ARABIA_MUSTAFA": 50331, "MOB_OTHER": 50330},
			"items": {"ITEM_PRE_COS_P_TOGUIDOL_SCROLL": 50331}}
		settled = published_ids.settle_shared(pin)
		self.assertEqual(settled["characters"], {"MOB_OTHER": 50330})
		self.assertEqual(settled["former"], {"characters": {"MOB_ARABIA_MUSTAFA": [50331]}})
		self.assertEqual(published_ids.settle_shared(settled), settled)
		with tempfile.TemporaryDirectory() as folder:
			path = Path(folder) / "pin.json"
			saved = published_ids.os.environ[published_ids.PIN_VARIABLE]
			published_ids.os.environ[published_ids.PIN_VARIABLE] = str(path)
			try:
				path.write_text(json.dumps(pin), encoding="utf-8")
				with self.assertRaises(ValueError):
					published_ids.published(published_ids.OBJECTS)
				path.write_text(json.dumps(settled), encoding="utf-8")
				pinned = published_ids.published(published_ids.OBJECTS)
				self.assertEqual(pinned, {"MOB_OTHER": 50330, "ITEM_PRE_COS_P_TOGUIDOL_SCROLL": 50331})
				self.assertEqual(published_ids.former_ids("characters"), {"MOB_ARABIA_MUSTAFA": [50331]})
				# Whichever importer runs first, the monster never takes 50331.
				monsters = published_ids.allocate(published_ids.OBJECTS, set(), ["MOB_ARABIA_MUSTAFA"], {}, pinned)
				self.assertEqual(monsters, {"MOB_ARABIA_MUSTAFA": 50332})
				scrolls = published_ids.allocate(published_ids.OBJECTS, {50332}, ["ITEM_PRE_COS_P_TOGUIDOL_SCROLL"], {},
					pinned)
				self.assertEqual(scrolls, {"ITEM_PRE_COS_P_TOGUIDOL_SCROLL": 50331})
			finally:
				published_ids.os.environ[published_ids.PIN_VARIABLE] = saved
		# Two items under one id has no holder to prefer, so it refuses.
		with self.assertRaises(ValueError):
			published_ids.settle_shared({**pin, "items": {"ITEM_PRE_COS_P_TOGUIDOL_SCROLL": 50331, "ITEM_TWIN": 50331}})
		# A kept id another row of the tree holds still refuses.
		with self.assertRaises(ValueError):
			published_ids.allocate(published_ids.OBJECTS, {7}, ["A"], {}, {"A": 7})

	# ================
	# test_occupied_objects_reads_every_object_table
	#
	# #3994: the gears stones read the item tables alone and took the fox
	# pets' character ids. Every object table counts; the asker's own
	# shard and the manifests do not.
	# ================
	def test_occupied_objects_reads_every_object_table(self):
		with tempfile.TemporaryDirectory() as folder:
			root = Path(folder)
			(root / "itemdata.txt").write_text("itemdata_5000.txt\n", encoding="utf-8")
			(root / "itemdata_5000.txt").write_text("1\t10\tITEM_A\n", encoding="utf-8")
			(root / "characterdata_130000.txt").write_text("1\t50853\tCOS_P_FOX_001\n", encoding="utf-8")
			(root / "teleportbuilding.txt").write_text("1\t27751\tSTORE_GATE\n", encoding="utf-8")
			(root / "itemdata_190000.txt").write_text("1\t50854\tITEM_STONE\n", encoding="utf-8")
			(root / "skilldata_5000.txt").write_text("1\t99\tSKILL_X\n", encoding="utf-8")
			self.assertEqual(published_ids.occupied_objects(root, ("ITEMDATA_190000.txt",)), {10, 50853, 27751})
			stones = published_ids.allocate(published_ids.OBJECTS, published_ids.occupied_objects(root, ("itemdata_190000.txt",)),
				["ITEM_STONE"], {}, {})
			self.assertEqual(stones, {"ITEM_STONE": 50854})
			self.assertEqual(published_ids.shared_objects(root), {})
			(root / "itemdata_190000.txt").write_text("1\t50853\tITEM_STONE\n", encoding="utf-8")
			self.assertEqual(published_ids.shared_objects(root), {50853: ["COS_P_FOX_001", "ITEM_STONE"]})

	# ================
	# test_equipment_keeps_released_ids_across_the_roc_flag
	# ================
	def test_equipment_keeps_released_ids_across_the_roc_flag(self):
		# The Roc's piece sorts after the sword, as ITEM_ROC_ sorts last.
		roc = weapon("ITEM_ROC_CH_M_CLOTHES_AA_SET", 60, item_class=30)
		roc[items.TYPE_THIRD], roc[items.TYPE_FOURTH] = "1", "5"
		piece = weapon("ITEM_CH_M_HEAVY_10_BA_A", 50)
		piece[items.TYPE_THIRD] = "3"
		_, off = items.merge_equipment([], [roc, piece], pinned={"ITEM_CH_M_HEAVY_10_BA_A": 900, "ITEM_EU_GONE": 901})
		self.assertEqual(off["idMap"]["ITEM_CH_M_HEAVY_10_BA_A"]["target"], 900)
		_, on = items.merge_equipment([], [roc, piece], roc=True,
			pinned={"ITEM_CH_M_HEAVY_10_BA_A": 900, "ITEM_EU_GONE": 901})
		self.assertEqual(on["idMap"]["ITEM_CH_M_HEAVY_10_BA_A"]["target"], 900)
		self.assertGreater(on["idMap"][roc[items.CODENAME]]["target"], 901)

	# ================
	# test_the_pin_reads_the_named_file_or_none
	# ================
	def test_the_pin_reads_the_named_file_or_none(self):
		self.assertEqual(published_ids.published(published_ids.OBJECTS), {})
		with tempfile.TemporaryDirectory() as folder:
			path = Path(folder) / "pin.json"
			path.write_text(json.dumps({"format": published_ids.PIN_FORMAT, "items": {"A": 5}, "skills": {"S": 5}}),
				encoding="utf-8")
			saved = published_ids.os.environ[published_ids.PIN_VARIABLE]
			published_ids.os.environ[published_ids.PIN_VARIABLE] = str(path)
			try:
				self.assertEqual(published_ids.published(published_ids.OBJECTS), {"A": 5})
				self.assertEqual(published_ids.published(published_ids.SKILLS), {"S": 5})
				path.write_text(json.dumps({"items": {"A": 5}}), encoding="utf-8")
				with self.assertRaises(ValueError):
					published_ids.published(published_ids.OBJECTS)
			finally:
				published_ids.os.environ[published_ids.PIN_VARIABLE] = saved


class PinToolTest(unittest.TestCase):
	# ================
	# test_a_refresh_keeps_a_dropped_code_and_refuses_a_move
	# ================
	def test_a_refresh_keeps_a_dropped_code_and_refuses_a_move(self):
		self.assertEqual(pin_tool.merge_pin({"OFF": 7, "A": 1}, {"A": 1, "NEW": 2}), {"A": 1, "NEW": 2, "OFF": 7})
		with self.assertRaises(ValueError):
			pin_tool.merge_pin({"A": 1}, {"A": 3})
		with self.assertRaises(ValueError):
			pin_tool.pin_rows([{"codename": "A", "id": 1}, {"codename": "A", "id": 2}])

	# ================
	# test_build_pin_reads_the_three_catalogue_files
	# ================
	def test_build_pin_reads_the_three_catalogue_files(self):
		with tempfile.TemporaryDirectory() as folder:
			for file, key, rows in (("monsters.json", "monsters", [{"codename": "MOB", "id": 3}]),
					("items.json", "items", [{"codename": "ITEM", "id": 4}]),
					("skills.json", "skills", [{"codename": "SKILL", "id": 3}])):
				(Path(folder) / file).write_text(json.dumps({"release": "abc", key: rows}), encoding="utf-8")
			document = pin_tool.build_pin(folder, {"items": {"OLD": 9}})
		self.assertEqual(document, {"format": published_ids.PIN_FORMAT, "release": "abc",
			"characters": {"MOB": 3}, "items": {"ITEM": 4, "OLD": 9}, "skills": {"SKILL": 3}})

	# ================
	# test_a_refresh_settles_a_shared_id_and_keeps_the_former_map
	#
	# A refresh from a catalogue that still lists a monster on a scroll's
	# id settles it, and the former map of the previous pin carries over.
	# ================
	def test_a_refresh_settles_a_shared_id_and_keeps_the_former_map(self):
		with tempfile.TemporaryDirectory() as folder:
			for file, key, rows in (("monsters.json", "monsters", [{"codename": "MOB", "id": 4}]),
					("items.json", "items", [{"codename": "ITEM", "id": 4}]),
					("skills.json", "skills", [])):
				(Path(folder) / file).write_text(json.dumps({"release": "abc", key: rows}), encoding="utf-8")
			document = pin_tool.build_pin(folder, {"former": {"characters": {"OLD_MOB": [2]}}})
		self.assertEqual(document["characters"], {})
		self.assertEqual(document["former"], {"characters": {"MOB": [4], "OLD_MOB": [2]}})


if __name__ == "__main__":
	unittest.main()
