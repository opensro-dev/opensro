"""
===========================================================================

test_alexandria_npcs.py - the VSRO city's NPCs in the v1.150 layout

Synthetic tables, no game data: the characterdata column conversion, the
NPC class gate, and a merge of one NPC with its position, talk row, shop,
size and texts.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import sys
import unittest
from pathlib import Path

import alexandria_npcs as npcs  # noqa: E402


# ================
# utf16
# ================
def utf16(rows):
	text = "\r\n".join("\t".join(row) for row in rows) + "\r\n"
	return b"\xff\xfe" + text.encode("utf-16-le")


# ================
# vsro_character
# ================
def vsro_character(ref_id, code, tid=("1", "2", "2"), *, name_key=None):
	"""A 104-column VSRO characterdata row; column n holds the text "v<n>"."""
	row = [f"v{column}" for column in range(npcs.VSRO_CHARACTER_COLUMNS)]
	row[0], row[npcs.ID_COLUMN], row[npcs.CODENAME_COLUMN] = "1", str(ref_id), code
	row[npcs.NAME_STRING_COLUMN] = "?? ??"
	row[npcs.NAME_KEY_COLUMN] = name_key or f"SN_{code}"
	row[npcs.CHAR_BIT_COLUMN] = "1"
	row[9], row[10], row[11] = tid
	return row


# ================
# ColumnTests
# ================
class ColumnTests(unittest.TestCase):
	# ================
	# test_the_resistances_and_skill_pairs_are_inserted_where_native_has_them
	# ================
	def test_the_resistances_and_skill_pairs_are_inserted_where_native_has_them(self):
		vsro = vsro_character(26792, "NPC_SD_M_AREA_POTION")
		native = npcs.convert_character(vsro)
		self.assertEqual(len(native), npcs.NATIVE_CHARACTER_COLUMNS)
		# Columns 0..60 keep their place, the six resistances are zero.
		self.assertEqual(native[:61][5], "SN_NPC_SD_M_AREA_POTION")
		self.assertEqual(native[61:67], ["0"] * 6)
		# VSRO 61 is native 67; the combat block (VSRO 71..) is native 77..
		self.assertEqual(native[67], "v61")
		self.assertEqual(native[77], "v71")
		self.assertEqual(native[98], "v92")
		# Five (skill, rate) pairs, then VSRO 93.. as native 109..
		self.assertEqual(native[99:109], ["0", "0.0"] * 5)
		self.assertEqual(native[109], "v93")
		self.assertEqual(native[119], "v103")

	# ================
	# test_the_korean_name_placeholder_becomes_the_native_unused_mark
	# ================
	def test_the_korean_name_placeholder_becomes_the_native_unused_mark(self):
		self.assertEqual(npcs.convert_character(vsro_character(1, "NPC_X"))[npcs.NAME_STRING_COLUMN], "xxx")

	# ================
	# test_a_row_of_another_width_is_refused
	# ================
	def test_a_row_of_another_width_is_refused(self):
		with self.assertRaises(ValueError):
			npcs.convert_character(vsro_character(1, "NPC_X")[:-1])


# ================
# ClassGateTests
# ================
class ClassGateTests(unittest.TestCase):
	# ================
	# test_npcs_pass_and_monsters_do_not
	# ================
	def test_npcs_pass_and_monsters_do_not(self):
		self.assertTrue(npcs.is_npc(vsro_character(1, "NPC_X", ("1", "2", "2"))))
		self.assertFalse(npcs.is_npc(vsro_character(2, "MOB_X", ("1", "2", "1"))))
		self.assertFalse(npcs.is_npc(vsro_character(3, "COS_X", ("1", "2", "3"))))

	# ================
	# test_a_disabled_row_is_not_an_npc
	# ================
	def test_a_disabled_row_is_not_an_npc(self):
		row = vsro_character(1, "NPC_X")
		row[0] = "0"
		self.assertFalse(npcs.is_npc(row))


CITY = (46, 90)
CITY_REGION = (90 << 8) | 46


# ================
# tables
# ================
def tables(extra_native_codes=()):
	"""Native and VSRO tables with one city NPC (a potion seller) and one far away."""
	native_rows = [vsro_character(i + 1, code) for i, code in enumerate(extra_native_codes)]
	native_rows = [npcs.convert_character(row) for row in native_rows]
	native = {
		"characters": [utf16(native_rows)],
		npcs.CHARACTER_TARGET: utf16(native_rows),
		"npcpos.txt": utf16([["1", "7", "1.0", "0.0", "1.0"]]),
		"npcchat.txt": utf16([["1", "NPC_CH_POTION", "SN_NPC_CH_POTION_BS", "SN_NPC_CH_POTION_PS"]]),
		"refshopgroup.txt": utf16([["1", "11", "778", "GROUP_STORE_CH_POTION", "NPC_CH_POTION", "-1", "xxx", "-1", "xxx", "-1", "xxx", "-1", "xxx"]]),
		"refmappingshopgroup.txt": utf16([["1", "11", "GROUP_STORE_CH_POTION", "STORE_KT_POTION"]]),
		"refshop.txt": utf16([["1", "11", "768", "STORE_KT_POTION"]]),
		"skilleffect.txt": utf16([
			["#section", "characterInfo"], ["NPC_CH_POTION", "PLAYER", "1.6", "none", "none", "none", "none", "none", "0,13,-2", "none", "none", "0", ""],
			["#section", "light"], ["lamp"]
		])
	}
	potion = vsro_character(26792, "NPC_SD_M_AREA_POTION")
	far = vsro_character(26793, "NPC_SD_M_AREA_ARMOR")
	vsro = {
		"characters": [utf16([potion, far])],
		"npcpos.txt": utf16([
			["26792", str(CITY_REGION), "5.0", "0.0", "6.0"],
			["26793", "9999", "5.0", "0.0", "6.0"]
		]),
		"npcchat.txt": utf16([["1", "NPC_SD_M_AREA_POTION", "SN_NPC_SD_M_AREA_POTION_BS", "SN_NPC_SD_M_AREA_POTION_PS"]]),
		"refshopgroup.txt": utf16([
			["1", "15", "2618", "GROUP_STORE_SD_M_POTION", "NPC_SD_M_AREA_POTION", "-1", "xxx", "-1", "xxx", "-1", "xxx", "-1", "xxx"],
			["1", "15", "2617", "GROUP_STORE_SD_M_ACCESSORY", "NPC_SD_M_AREA_ACCESSORY", "-1", "xxx", "-1", "xxx", "-1", "xxx", "-1", "xxx"]
		]),
		"refmappingshopgroup.txt": utf16([
			["1", "15", "GROUP_STORE_SD_M_POTION", "STORE_KT_POTION"],
			["1", "15", "GROUP_STORE_SD_M_ACCESSORY", "STORE_SD_ACCESSORY"]
		]),
		"skilleffect.txt": utf16([
			["NPC_SD_M_AREA_POTION", "PLAYER", "2.0", "none", "none", "none", "none", "none", "0,13,-2", "none", "none", "0", "0", "", "", ""]
		]),
		"textdata_object.txt": utf16([
			["1", "SN_NPC_SD_M_AREA_POTION", "kor", "", "tw", "cn", "de", "ja", "en", "vi", "", "", "", "", ""],
			["1", "SN_NPC_SD_M_AREA_POTION_BS", "kor", "", "tw", "cn", "de", "ja", "hello", "vi", "", "", "", "", ""],
			["1", "SN_NPC_SD_M_AREA_POTION_PS", "kor", "", "tw", "cn", "de", "ja", "bye", "vi", "", "", "", "", ""],
			["1", "SN_UNRELATED", "kor", "", "tw", "cn", "de", "ja", "no", "vi", "", "", "", "", ""]
		])
	}
	return native, vsro


# ================
# merge
# ================
def merge(native, vsro):
	text = utf16([["1", "SN_OLD", "k", "", "", "c", "t", "j", "old", "v", ""]])
	return npcs.merge_npc_tables(native, vsro, [CITY], text)


# ================
# rows_of
# ================
def rows_of(merged, name):
	from import_vsro_alexandria import table_rows
	return table_rows(merged[name])


# ================
# MergeTests
# ================
class MergeTests(unittest.TestCase):
	# ================
	# test_unquoted_translation_continuation_preserves_the_english_name
	# ================
	def test_unquoted_translation_continuation_preserves_the_english_name(self):
		raw = utf16([["1", "SN_NPC_SD_T_AREA_THIEF", "ko", "", "tw", "cn", "German"], [" Tausert", "ja", "Thief Union President Tausert", "vi", "pt", "ru", "tr", "es", "ar"]])
		rows = npcs.translation_rows(raw)
		self.assertEqual(len(rows), 1)
		self.assertEqual(rows[0][8], "Thief Union President Tausert")

	# ================
	# test_missing_armor_placement_uses_the_authored_region_only
	# ================
	def test_missing_armor_placement_uses_the_authored_region_only(self):
		characters = {"10": vsro_character(10, "NPC_SD_T_AREA_ARMOR"), "11": vsro_character(11, "NPC_SD_T_AREA_SMITH")}
		anchor = ["11", "23346", "648.39001", "1310.96", "1730.5"]
		placed = npcs.restore_missing_positions([anchor], characters, {23602})
		self.assertEqual(placed[0], ["10", "23602", "639.19", "1364.63", "87.459999"])
		self.assertEqual(npcs.restore_missing_positions([anchor] + placed, characters, {23602}), [])
		self.assertEqual(npcs.restore_missing_positions([anchor], characters, {23346}), [])

	# ================
	# test_restored_armor_uses_the_database_model_and_position
	# ================
	def test_restored_armor_uses_the_database_model_and_position(self):
		native, vsro = tables()
		armor = vsro_character(26822, "NPC_SD_T_AREA_ARMOR")
		vsro["characters"].append(utf16([armor]))
		merged, summary = npcs.merge_npc_tables(native, vsro, [(50, 92)], utf16([]))
		row = next(row for row in rows_of(merged, npcs.CHARACTER_TARGET) if row[2] == armor[2])
		self.assertEqual(row[52], "npc\\sd\\sd_guild_agent3.bsr")
		self.assertEqual(rows_of(merged, "npcpos.txt")[-1], ["26822", "23602", "639.19", "1364.63", "87.459999"])
		self.assertEqual(len(summary["restoredPositions"]), 1)

	# ================
	# test_sharon_restores_her_authored_position_and_model
	# ================
	def test_sharon_restores_her_authored_position_and_model(self):
		native, vsro = tables()
		merged, summary = npcs.merge_npc_tables(native, vsro, [(47, 90)], utf16([]))
		row = next(row for row in rows_of(merged, npcs.CHARACTER_TARGET) if row[2] == "NPC_SD_M_AREA_ARMOR")
		self.assertEqual(row[52], "npc\\sd\\sd_m_area_warehouse.bsr")
		self.assertEqual(rows_of(merged, "npcpos.txt")[-1], ["26793", "23087", "1731.92", "863.31", "875.04999"])
		self.assertEqual(len(summary["restoredPositions"]), 1)
		placed = summary["restoredPositions"]
		characters = {"26793": vsro_character(26793, "NPC_SD_M_AREA_ARMOR")}
		self.assertEqual(npcs.restore_missing_positions(placed, characters, {23087}), [])

	# ================
	# test_only_the_npc_placed_in_the_city_is_added_in_the_native_layout
	# ================
	def test_only_the_npc_placed_in_the_city_is_added_in_the_native_layout(self):
		merged, summary = merge(*tables())
		self.assertEqual(summary["npcs"], ["NPC_SD_M_AREA_POTION"])
		added = rows_of(merged, npcs.CHARACTER_TARGET)
		self.assertEqual([row[2] for row in added], ["NPC_SD_M_AREA_POTION"])
		self.assertEqual(len(added[0]), npcs.NATIVE_CHARACTER_COLUMNS)

	# ================
	# test_the_position_talk_row_and_size_follow_the_npc
	# ================
	def test_the_position_talk_row_and_size_follow_the_npc(self):
		merged, summary = merge(*tables())
		self.assertEqual(rows_of(merged, "npcpos.txt")[-1], ["26792", str(CITY_REGION), "5.0", "0.0", "6.0"])
		self.assertEqual(rows_of(merged, "npcchat.txt")[-1][1], "NPC_SD_M_AREA_POTION")
		effects = rows_of(merged, "skilleffect.txt")
		codes = [row[0] for row in effects]
		# In the characterInfo section, cut to the native 13 columns, before the next section.
		self.assertLess(codes.index("NPC_SD_M_AREA_POTION"), codes.index("#section", 1))
		self.assertEqual(len(effects[codes.index("NPC_SD_M_AREA_POTION")]), npcs.SKILL_EFFECT_COLUMNS)

	# ================
	# test_a_shop_on_a_native_store_is_added_and_one_on_a_new_store_is_left_out
	# ================
	def test_a_shop_on_a_native_store_is_added_and_one_on_a_new_store_is_left_out(self):
		merged, summary = merge(*tables())
		self.assertEqual(summary["shopGroups"], ["GROUP_STORE_SD_M_POTION"])
		group = rows_of(merged, "refshopgroup.txt")[-1]
		# The table kind is the native one (11), not the VSRO one (15).
		self.assertEqual(group[1], "11")
		self.assertEqual(rows_of(merged, "refmappingshopgroup.txt")[-1][2:], ["GROUP_STORE_SD_M_POTION", "STORE_KT_POTION"])
		self.assertNotIn("GROUP_STORE_SD_M_ACCESSORY", [row[3] for row in rows_of(merged, "refshopgroup.txt") if len(row) > 3])

	# ================
	# test_texts_are_added_in_the_native_layout_for_the_npc_only
	# ================
	def test_texts_are_added_in_the_native_layout_for_the_npc_only(self):
		merged, summary = merge(*tables())
		added = {row[1]: row for row in rows_of(merged, "textdataname.txt")}
		self.assertEqual(added["SN_NPC_SD_M_AREA_POTION_BS"][8], "hello")
		self.assertNotIn("SN_UNRELATED", added)
		self.assertIn("SN_OLD", added)

	# ================
	# test_an_npc_the_native_table_has_keeps_its_native_id_in_the_position_row
	# ================
	def test_an_npc_the_native_table_has_keeps_its_native_id_in_the_position_row(self):
		native, vsro = tables(("NPC_SD_M_AREA_POTION",))
		merged, summary = merge(native, vsro)
		self.assertEqual(summary["npcs"], [])
		self.assertEqual(rows_of(merged, "npcpos.txt")[-1][0], "1")

	# ================
	# test_an_id_the_native_table_uses_is_refused
	# ================
	def test_an_id_the_native_table_uses_is_refused(self):
		native, vsro = tables()
		native["characters"] = [utf16([npcs.convert_character(vsro_character(26792, "NPC_OTHER"))])]
		with self.assertRaises(SystemExit):
			merge(native, vsro)


if __name__ == "__main__":
	unittest.main()


class BomTests(unittest.TestCase):
	"""A leading U+FEFF is stripped before the UTF-16 prefix is re-added (never a second BOM)."""

	def test_append_rows_never_doubles_the_bom(self):
		raw = b"\xff\xfe" + "\ufeff1\tA\r\n".encode("utf-16-le")
		out = npcs.append_rows(raw, [["2", "B"]])
		self.assertTrue(out.startswith(b"\xff\xfe"))
		self.assertNotEqual(out[2:4], b"\xff\xfe")
		self.assertEqual(out[2:].decode("utf-16-le"), "1\tA\r\n2\tB\r\n")
