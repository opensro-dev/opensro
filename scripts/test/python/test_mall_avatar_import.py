"""
===========================================================================

test_mall_avatar_import.py - the mall avatar import, on by default

Synthetic rows cover the default-on switch, selection, identity retention
and reassignment, skipped rows, the manifest shared with the cap-110 item
import, the on/off/on rebuild and the absent-client skip.

===========================================================================
"""
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch
import zipfile

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import avatar_archive  # noqa: E402
import cap110_items as items  # noqa: E402
import mall_avatar_import as importer  # noqa: E402
import sro_pk2  # noqa: E402
from test_cap110_items import archive_bytes, utf16  # noqa: E402

FIRST_ID = 24371


# ================
# avatar
# ================
def avatar(code, identity, service="1", kind="13"):
	row = ["0"] * items.ITEM_COLUMNS
	row[0:4] = [service, str(identity), code, "unused source name"]
	row[4] = "xxx"
	row[5], row[6] = "SN_" + code, "SN_" + code + "_TT_DESC"
	row[9:13] = ["3", "1", kind, "1"]
	row[52:55] = ["item/avatar.bsr", "item/drop.bsr", "item/avatar.ddj"]
	for column in range(items.PARAM_FIRST, items.PARAM_END, items.PARAM_STRIDE):
		row[column:column + items.PARAM_STRIDE] = ["-1", "xxx"]
	return row


# ================
# later_rows
# ================
def later_rows(first=FIRST_ID):
	codes = ("ITEM_MALL_AVATAR_M_TURKEY_HAT", "ITEM_MALL_AVATAR_W_TURKEY_HAT", "ITEM_MALL_AVATAR_M_TURKEY", "ITEM_MALL_AVATAR_W_TURKEY")
	return [avatar(code, first + index) for index, code in enumerate(codes)]


# ================
# native_row
# ================
def native_row(identity, code="ITEM_CH_SWORD_01"):
	row = avatar(code, identity)
	row[9:13] = ["3", "1", "6", "2"]
	return row


class TextLayoutTests(unittest.TestCase):
	# ================
	# test_v1696_row_takes_the_vsro_layout
	# ================
	def test_v1696_row_takes_the_vsro_layout(self):
		row = ["1", "5979", "SN_X", "ko"] + ["0"] * 5 + ["Devil"] + ["0"] * 3 + ["Devil"] * 4
		cells = importer.vsro_text_row(row)
		self.assertEqual(len(cells), importer.VSRO_TEXT_COLUMNS)
		self.assertEqual(cells[:4], ["1", "SN_X", "ko", ""])
		self.assertEqual(cells[importer.TEXT_ENGLISH], "Devil")
		self.assertNotIn(importer.V1696_ABSENT, cells[3:])

	# ================
	# test_vsro_row_passes_through
	# ================
	def test_vsro_row_passes_through(self):
		row = ["1", "SN_X", "ko", "", "tw", "cn", "en", "ja", "en", "vi", "en", "ru", "en", "en", "en"]
		self.assertIs(importer.vsro_text_row(row), row)


class SwitchTests(unittest.TestCase):
	# ================
	# test_unset_is_on
	# ================
	def test_unset_is_on(self):
		for value in ({}, {importer.FLAG: ""}, {importer.FLAG: "on"}, {importer.FLAG: "1"}):
			self.assertTrue(importer.enabled(value), value)

	# ================
	# test_explicit_off_is_off
	# ================
	def test_explicit_off_is_off(self):
		for value in ("off", "0", "false", "no", " OFF "):
			self.assertFalse(importer.enabled({importer.FLAG: value}), value)

	# ================
	# test_unreadable_value_is_refused
	# ================
	def test_unreadable_value_is_refused(self):
		with self.assertRaises(ValueError):
			importer.enabled({importer.FLAG: "maybe"})


class SelectionTests(unittest.TestCase):
	# ================
	# test_selects_mall_avatars_only
	# ================
	def test_selects_mall_avatars_only(self):
		extra = [
			avatar("ITEM_EVENT_AVATAR_M_TURKEY", 24945),
			avatar("ITEM_MALL_AVATAR_M_TEST", 24946),
			avatar("ITEM_MALL_AVATAR_M_OFF", 24947, service="0"),
			avatar("ITEM_MALL_AVATAR_M_MOUNT", 24948, kind="14"),
		]
		added, summary = importer.select_avatars([native_row(10)], later_rows() + extra)
		self.assertEqual(summary["added"], 4)
		self.assertEqual({row[items.CODENAME] for row in added}, {row[items.CODENAME] for row in later_rows()})
		self.assertTrue(all(row[items.PLACEHOLDER_NAME] == items.UNUSED for row in added))

	# ================
	# test_free_identity_is_kept
	# ================
	def test_free_identity_is_kept(self):
		_, summary = importer.select_avatars([native_row(10)], later_rows())
		self.assertEqual(summary["idMap"]["ITEM_MALL_AVATAR_M_TURKEY_HAT"], {"source": FIRST_ID, "target": FIRST_ID})

	# ================
	# test_taken_identity_gets_a_fresh_one
	# ================
	def test_taken_identity_gets_a_fresh_one(self):
		added, summary = importer.select_avatars([native_row(FIRST_ID)], later_rows(), reserved_ids={FIRST_ID + 1})
		targets = [row[items.IDENTITY] for row in added]
		self.assertEqual(len(set(targets)), 4)
		moved = {code: entry for code, entry in summary["idMap"].items() if entry["source"] != entry["target"]}
		self.assertEqual(len(moved), 2)
		self.assertTrue(all(entry["target"] >= importer.FRESH_ID_BASE for entry in moved.values()))

	# ================
	# test_previous_identity_is_retained
	# ================
	def test_previous_identity_is_retained(self):
		first, summary = importer.select_avatars([native_row(FIRST_ID)], later_rows())
		previous = importer.previous_targets({"idMap": summary["idMap"]})
		again, second = importer.select_avatars([native_row(FIRST_ID)], later_rows(), previous=previous)
		self.assertEqual(summary["idMap"], second["idMap"])

	# ================
	# test_old_marker_format_is_read
	# ================
	def test_old_marker_format_is_read(self):
		self.assertEqual(importer.previous_targets({"idMap": {"ITEM_MALL_AVATAR_M_TURKEY": 24377}}), {"ITEM_MALL_AVATAR_M_TURKEY": 24377})

	# ================
	# test_rows_with_absent_resources_are_skipped
	# ================
	def test_rows_with_absent_resources_are_skipped(self):
		def missing(row):
			return ["model"] if row[items.CODENAME].endswith("_HAT") else []
		added, summary = importer.select_avatars([native_row(10)], later_rows(), missing=missing)
		self.assertEqual(summary["added"], 2)
		self.assertEqual(len(summary["skipped"]), 2)
		self.assertEqual(summary["skipped"][0]["missing"], ["model"])

	# ================
	# test_unrenderable_rows_are_skipped
	# ================
	def test_unrenderable_rows_are_skipped(self):
		rows = later_rows() + [avatar("ITEM_MALL_AVATAR_M_EARTHSPIRIT_HAT", FIRST_ID + 9)]
		added, summary = importer.select_avatars([native_row(10)], rows)
		self.assertEqual(summary["added"], 4)
		self.assertEqual(summary["skipped"][0]["code"], "ITEM_MALL_AVATAR_M_EARTHSPIRIT_HAT")
		self.assertEqual(summary["skipped"][0]["missing"], ["environment-mapped material"])

	# ================
	# test_rows_without_name_text_are_skipped
	# ================
	def test_rows_without_name_text_are_skipped(self):
		texts = {"SN_ITEM_MALL_AVATAR_M_TURKEY"}
		added, summary = importer.select_avatars([native_row(10)], later_rows(), texts=texts)
		self.assertEqual(summary["added"], 1)
		self.assertEqual(summary["skipped"][0]["missing"], ["name text"])

	# ================
	# test_nothing_to_import_is_refused
	# ================
	def test_nothing_to_import_is_refused(self):
		with self.assertRaises(ValueError):
			importer.select_avatars([native_row(10)], [native_row(11, "ITEM_CH_SWORD_02")])

	# ================
	# test_native_codename_wins
	# ================
	def test_native_codename_wins(self):
		native = [native_row(10), avatar("ITEM_MALL_AVATAR_M_TURKEY", 99)]
		added, summary = importer.select_avatars(native, later_rows())
		self.assertEqual(len(added), 3)
		self.assertEqual(summary["existingNative"], ["ITEM_MALL_AVATAR_M_TURKEY"])


class MallTests(unittest.TestCase):
	# ================
	# tables
	# ================
	def tables(self):
		codes = ("ITEM_MALL_AVATAR_M_TURKEY", "ITEM_MALL_AVATAR_M_TURKEY_HAT", "ITEM_MALL_AVATAR_M_TURKEY_ATTACH", "ITEM_MALL_AVATAR_M_NOSALE", "ITEM_MALL_AVATAR_M_NOPACKAGE")
		later = {name: [] for name, _ in importer.MALL_TABLES}
		for index, code in enumerate(codes[:4]):
			package = "PACKAGE_" + code[5:]
			later["refpackageitem.txt"].append(["1", "15", str(70000 + index), package, "32", "EXPAND_TERM_ALL", "SN_" + code, "SN_" + code + "_TT_DESC", "icon.ddj"] + ["-1", "xxx"] * 4)
			later["refscrapofpackageitem.txt"].append(["1", "15", package, code] + ["0"] * 16 + ["-1", "xxx"] * 4 + ["80000"])
			for currency, amount in (("2", "398"), ("4", "0"), ("16", "0")):
				later["refpricepolicyofitem.txt"].append(["1", "15", package, currency, "0", amount] + ["-1", "xxx"] * 4)
		tabs = {codes[0]: ("MALL_AVATAR_DRESS", 6), codes[1]: ("MALL_AVATAR_HAT", 5), codes[2]: ("MALL_AVATAR_ATTACH", 9)}
		for code, (tab, slot) in tabs.items():
			later["refshopgoods.txt"].append(["1", "15", tab, "PACKAGE_" + code[5:], str(slot)] + ["-1", "xxx"] * 4)
		current = {name: [] for name, _ in importer.MALL_TABLES}
		current["refshopgoods.txt"] = [["1", "11", "MALL_AVATAR_HAT", "PACKAGE_X", "1"] + ["-1", "xxx"] * 4]
		avatars = [avatar(code, FIRST_ID + index) for index, code in enumerate(codes)]
		return later, current, avatars

	# ================
	# test_listing_converts_to_the_native_layout
	# ================
	def test_listing_converts_to_the_native_layout(self):
		later, current, avatars = self.tables()
		rows, summary = importer.mall_rows(later, avatars, current, {1})
		self.assertEqual(summary["listed"], 3)
		price = [row for row in rows["refpricepolicyofitem.txt"] if row[3] == "2" and row[2] == "PACKAGE_MALL_AVATAR_M_TURKEY"][0]
		self.assertEqual(price[:5], ["1", "11", "PACKAGE_MALL_AVATAR_M_TURKEY", "2", "130"])
		self.assertEqual(len(price), 13)
		package = rows["refpackageitem.txt"][0]
		self.assertEqual((package[1], package[4]), ("11", "0"))

	# ================
	# test_silk_prices_follow_the_native_tier
	# ================
	def test_silk_prices_follow_the_native_tier(self):
		later, current, avatars = self.tables()
		rows, _ = importer.mall_rows(later, avatars, current, {1})
		silk = {row[2]: row[4] for row in rows["refpricepolicyofitem.txt"] if row[3] == "2"}
		self.assertEqual(silk["PACKAGE_MALL_AVATAR_M_TURKEY"], "130")
		self.assertEqual(silk["PACKAGE_MALL_AVATAR_M_TURKEY_HAT"], "30")
		self.assertEqual(silk["PACKAGE_MALL_AVATAR_M_TURKEY_ATTACH"], "15")
		other = [row for row in rows["refpricepolicyofitem.txt"] if row[3] != "2"]
		self.assertTrue(all(row[4] == "0" for row in other))

	# ================
	# test_kind_follows_the_codename
	# ================
	def test_kind_follows_the_codename(self):
		self.assertEqual(importer.avatar_kind("ITEM_MALL_AVATAR_M_CLOWN_HAT_4"), "hat")
		self.assertEqual(importer.avatar_kind("ITEM_MALL_AVATAR_W_ADMIRAL_ATTACH_2011"), "attach")
		self.assertEqual(importer.avatar_kind("ITEM_MALL_AVATAR_M_ADMIRAL_2011"), "dress")

	# ================
	# test_attachments_take_their_own_tab_after_present_slots
	# ================
	def test_attachments_take_their_own_tab_after_present_slots(self):
		later, current, avatars = self.tables()
		rows, _ = importer.mall_rows(later, avatars, current, {1})
		slots = {row[3]: (row[2], row[4]) for row in rows["refshopgoods.txt"]}
		self.assertEqual(slots["PACKAGE_MALL_AVATAR_M_TURKEY_HAT"], ("MALL_AVATAR_HAT", "2"))
		self.assertEqual(slots["PACKAGE_MALL_AVATAR_M_TURKEY_ATTACH"], (importer.ATTACH_TAB, "0"))
		self.assertEqual(slots["PACKAGE_MALL_AVATAR_M_TURKEY"], ("MALL_AVATAR_DRESS", "0"))

	# ================
	# test_attach_tab_row_is_carried_over_once
	# ================
	def test_attach_tab_row_is_carried_over_once(self):
		later = [["1", "15", "3401", importer.ATTACH_TAB, "MALL_AVATAR_GROUP1", "UIIT_STT_SILKMALL_ATTACH", "xxx"]]
		current = [["1", "11", "1982", "MALL_AVATAR_HAT", "MALL_AVATAR_GROUP1", "UIIT_STT_SILKMALL_HAT"],
			["1", "11", "3401", "OTHER_TAB", "OTHER_GROUP", "X"]]
		row = importer.attach_tab_row(current, later)
		self.assertEqual(row, ["1", "11", "3402", importer.ATTACH_TAB, "MALL_AVATAR_GROUP1", "UIIT_STT_SILKMALL_ATTACH", "xxx"])
		self.assertIsNone(importer.attach_tab_row(current + [row], later))
		with self.assertRaises(ValueError):
			importer.attach_tab_row(current, [])

	# ================
	# test_strip_tabs_removes_only_the_added_tab
	# ================
	def test_strip_tabs_removes_only_the_added_tab(self):
		with tempfile.TemporaryDirectory() as folder:
			output = Path(folder)
			path = output / importer.TEXT_ROOT / importer.SHOP_TAB_TABLE
			path.parent.mkdir(parents=True)
			path.write_bytes(utf16([["1", "11", "1982", "MALL_AVATAR_HAT"], ["1", "11", "2379", importer.ATTACH_TAB]]))
			importer.strip_tabs(output, {importer.ATTACH_TAB})
			text = path.read_bytes().decode("utf-16")
			self.assertIn("MALL_AVATAR_HAT", text)
			self.assertNotIn(importer.ATTACH_TAB, text)

	# ================
	# test_unsold_and_packageless_avatars_stay_unlisted
	# ================
	def test_unsold_and_packageless_avatars_stay_unlisted(self):
		later, current, avatars = self.tables()
		_, summary = importer.mall_rows(later, avatars, current, {1})
		reasons = {entry["code"]: entry["reason"] for entry in summary["unlisted"]}
		self.assertEqual(reasons["ITEM_MALL_AVATAR_M_NOSALE"], "not sold in the later mall")
		self.assertEqual(reasons["ITEM_MALL_AVATAR_M_NOPACKAGE"], "no package")

	# ================
	# test_list_all_authors_the_unsold_and_packageless
	#
	# Port-only listing: both avatars the later mall could not list get an
	# authored package, plain scrap and native silk price, and take the
	# dress tab after the avatars the later mall sold.
	# ================
	def test_list_all_authors_the_unsold_and_packageless(self):
		later, current, avatars = self.tables()
		rows, summary = importer.mall_rows(later, avatars, current, {1}, list_all=True)
		self.assertEqual(summary["listed"], 5)
		self.assertEqual(summary["unlisted"], [])
		self.assertEqual(sorted(summary["authored"]), ["ITEM_MALL_AVATAR_M_NOPACKAGE", "ITEM_MALL_AVATAR_M_NOSALE"])
		package = [row for row in rows["refpackageitem.txt"] if row[3] == "PACKAGE_ITEM_MALL_AVATAR_M_NOPACKAGE"][0]
		self.assertEqual(package[4:9], ["0", "EXPAND_TERM_ALL", "SN_ITEM_MALL_AVATAR_M_NOPACKAGE",
			"SN_ITEM_MALL_AVATAR_M_NOPACKAGE_TT_DESC", "item/avatar.ddj"])
		self.assertEqual(len(package), 17)
		scrap = [row for row in rows["refscrapofpackageitem.txt"] if row[2] == package[3]][0]
		self.assertEqual((len(scrap), scrap[3]), (29, "ITEM_MALL_AVATAR_M_NOPACKAGE"))
		prices = [row for row in rows["refpricepolicyofitem.txt"] if row[2] == package[3]]
		self.assertEqual([row[3:5] for row in prices], [["2", "130"]])
		slots = {row[3]: (row[2], row[4]) for row in rows["refshopgoods.txt"]}
		self.assertEqual(slots["PACKAGE_MALL_AVATAR_M_TURKEY"], ("MALL_AVATAR_DRESS", "0"))
		self.assertEqual(slots["PACKAGE_ITEM_MALL_AVATAR_M_NOPACKAGE"], ("MALL_AVATAR_DRESS", "1"))
		self.assertEqual(slots["PACKAGE_ITEM_MALL_AVATAR_M_NOSALE"], ("MALL_AVATAR_DRESS", "2"))

	# ================
	# test_authored_identities_pass_every_shipped_one
	# ================
	def test_authored_identities_pass_every_shipped_one(self):
		later, current, avatars = self.tables()
		rows, summary = importer.mall_rows(later, avatars, current, {90000}, list_all=True)
		authored = {"PACKAGE_" + code for code in summary["authored"]}
		ids = [int(row[2]) for row in rows["refpackageitem.txt"] if row[3] in authored]
		serials = [int(row[28]) for row in rows["refscrapofpackageitem.txt"] if row[2] in authored]
		self.assertTrue(all(identity > 90000 for identity in ids))
		self.assertTrue(all(serial > 80000 for serial in serials))
		self.assertEqual(len(set(ids)), 2)
		self.assertEqual(len(set(serials)), 2)

	# ================
	# test_a_full_tab_is_refused
	# ================
	def test_a_full_tab_is_refused(self):
		later, current, avatars = self.tables()
		current["refshopgoods.txt"].append(["1", "11", "MALL_AVATAR_DRESS", "PACKAGE_Y", "255"] + ["-1", "xxx"] * 4)
		with self.assertRaises(ValueError):
			importer.mall_rows(later, avatars, current, {1}, list_all=True)

	# ================
	# test_list_all_switch_is_read
	# ================
	def test_list_all_switch_is_read(self):
		self.assertTrue(importer.enabled({}, importer.LIST_ALL_FLAG))
		self.assertFalse(importer.enabled({importer.LIST_ALL_FLAG: "off"}, importer.LIST_ALL_FLAG))

	# ================
	# test_package_identity_clash_is_refused
	# ================
	def test_package_identity_clash_is_refused(self):
		later, current, avatars = self.tables()
		with self.assertRaises(ValueError):
			importer.mall_rows(later, avatars, current, {70000})

	# ================
	# test_strip_removes_only_the_listed_packages
	# ================
	def test_strip_removes_only_the_listed_packages(self):
		with tempfile.TemporaryDirectory() as folder:
			output = Path(folder)
			path = output / importer.TEXT_ROOT / "refshopgoods.txt"
			path.parent.mkdir(parents=True)
			path.write_bytes(utf16([["1", "11", "MALL_AVATAR_HAT", "PACKAGE_KEEP", "0"], ["1", "11", "MALL_AVATAR_HAT", "PACKAGE_DROP", "1"]]))
			importer.strip_mall(output, {"PACKAGE_DROP"})
			text = path.read_bytes().decode("utf-16")
			self.assertIn("PACKAGE_KEEP", text)
			self.assertNotIn("PACKAGE_DROP", text)


class BuildTests(unittest.TestCase):
	# ================
	# stage
	# ================
	def stage(self, root, mall=False):
		native, output, later = root / "native", root / "variant", root / "later"
		base = native / importer.TEXT_ROOT
		base.mkdir(parents=True)
		(output / importer.TEXT_ROOT).mkdir(parents=True)
		later.mkdir()
		manifest = utf16([["itemdata_5000.txt"]])
		(base / "itemdata.txt").write_bytes(manifest)
		(base / "itemdata_5000.txt").write_bytes(utf16([native_row(10)]))
		(base / "textdataname.txt").write_bytes(utf16([]))
		texts = [["1", row[5], "ko", "", "tw", "cn", "de", "ja", "Turkey", "vi"] for row in later_rows()]
		files = {
			"itemdata.txt": manifest, "itemdata_5000.txt": utf16(later_rows()),
			"textdata_equip&skill.txt": utf16(texts),
			"textdata_object.txt": utf16([]), "textdata_new.txt": utf16([]),
		}
		if mall:
			for name, _ in importer.MALL_TABLES:
				(base / name).write_bytes(utf16([]))
				files[name] = utf16([])
		(later / "Media.pk2").write_bytes(archive_bytes(files))
		(later / "Data.pk2").write_bytes(archive_bytes({}))
		(later / "Particles.pk2").write_bytes(archive_bytes({}))
		return native, output, later

	# ================
	# manifest_names
	# ================
	def manifest_names(self, output):
		raw = (output / importer.TEXT_ROOT / "itemdata.txt").read_bytes().decode("utf-16")
		return [line for line in raw.splitlines() if line.strip()]

	# ================
	# patched
	# ================
	def patched(self):
		return (
			patch.object(importer.shared, "copy_asset_closure", return_value=[]),
			patch.object(importer, "resource_check", return_value=lambda row: []),
		)

	# ================
	# test_default_on_off_on_rebuild
	# ================
	def test_default_on_off_on_rebuild(self):
		with tempfile.TemporaryDirectory() as folder:
			native, output, later = self.stage(Path(folder))
			environment = {key: value for key, value in os.environ.items() if key != importer.FLAG}
			closure, check = self.patched()
			with patch.dict(os.environ, environment, clear=True), closure, check:
				first = importer.add_avatars(later, native, output)
				self.assertTrue(first["enabled"])
				self.assertEqual(first["added"], 4)
				self.assertEqual(self.manifest_names(output), ["itemdata_5000.txt", importer.OUTPUT_SHARD])
				second = importer.add_avatars(later, native, output)
				self.assertEqual(first["idMap"], second["idMap"])
				self.assertEqual(self.manifest_names(output), ["itemdata_5000.txt", importer.OUTPUT_SHARD])
				with patch.dict(os.environ, {importer.FLAG: "off"}):
					self.assertFalse(importer.add_avatars(later, native, output)["enabled"])
				self.assertEqual(self.manifest_names(output), ["itemdata_5000.txt"])
				self.assertFalse((output / importer.TEXT_ROOT / importer.OUTPUT_SHARD).exists())
				self.assertFalse((output / importer.REPORT_NAME).exists())
				self.assertTrue(importer.add_avatars(later, native, output)["enabled"])

	# ================
	# stage_v1696
	#
	# A v1.696-shaped client: the text table is a shard index naming one
	# part, whose rows carry the numeric text id and "0" cells.
	# ================
	def stage_v1696(self, root, mall=False):
		later = root / "v1696"
		later.mkdir()
		codes = ("ITEM_MALL_AVATAR_M_DEVIL_HAT", "ITEM_MALL_AVATAR_M_DEVIL")
		rows = [avatar(code, FIRST_ID + index) for index, code in enumerate(codes)]
		texts = [
			["1", str(5000 + index), row[5], "ko"] + ["0"] * 5 + ["Devil"] + ["0"] * 3 + ["Devil"] * 4
			for index, row in enumerate(rows)
		]
		manifest = utf16([["itemdata_100001.txt"]])
		files = {
			"itemdata.txt": manifest, "itemdata_100001.txt": utf16(rows),
			"textdata_equip&skill.txt": utf16([["textdata_equip&skill_1.txt"]]),
			"textdata_equip&skill_1.txt": utf16(texts),
			"textdata_object.txt": utf16([]),
		}
		if mall:
			for name, _ in importer.MALL_TABLES:
				files[name] = utf16([])
		(later / "Media.pk2").write_bytes(archive_bytes(files))
		(later / "Data.pk2").write_bytes(archive_bytes({}))
		(later / "Particles.pk2").write_bytes(archive_bytes({}))
		return later

	# ================
	# test_second_source_adds_its_own_shard
	#
	# The v1.696 pass runs after the VSRO pass: its rows land in their own
	# shard, its sharded 17-column text is read, and it takes no identity
	# the VSRO pass holds.
	# ================
	def test_second_source_adds_its_own_shard(self):
		with tempfile.TemporaryDirectory() as folder:
			native, output, later = self.stage(Path(folder))
			v1696 = self.stage_v1696(Path(folder))
			closure, check = self.patched()
			with closure, check:
				first = importer.add_avatars(later, native, output)
				second = importer.add_avatars(v1696, native, output, importer.V1696_SOURCE)
			self.assertEqual(second["added"], 2)
			self.assertEqual(second["source"], "v1696")
			targets = [{entry["target"] for entry in summary["idMap"].values()} for summary in (first, second)]
			self.assertFalse(targets[0] & targets[1])
			self.assertEqual(
				self.manifest_names(output),
				["itemdata_5000.txt", importer.OUTPUT_SHARD, importer.V1696_SOURCE["shard"]],
			)
			names = (output / importer.TEXT_ROOT / "textdataname.txt").read_bytes().decode("utf-16")
			self.assertIn("Devil", names)

	# ================
	# test_two_sources_relist_identically_on_a_reused_tree
	#
	# A rebuild strips each source's goods and lists them again beside
	# the other source's: the goods must land on the same slots, not
	# walk each tab upward until the one-byte slot overflows, and the
	# re-authored package identities stay unique in the table.
	# ================
	def test_two_sources_relist_identically_on_a_reused_tree(self):
		with tempfile.TemporaryDirectory() as folder:
			native, output, later = self.stage(Path(folder), mall=True)
			v1696 = self.stage_v1696(Path(folder), mall=True)
			closure, check = self.patched()
			with closure, check:
				first = importer.add_avatars(later, native, output)
				second = importer.add_avatars(v1696, native, output, importer.V1696_SOURCE)
				goods = (output / importer.TEXT_ROOT / "refshopgoods.txt").read_bytes()
				again_first = importer.add_avatars(later, native, output)
				again_second = importer.add_avatars(v1696, native, output, importer.V1696_SOURCE)
			self.assertEqual(first["idMap"], again_first["idMap"])
			self.assertEqual(second["idMap"], again_second["idMap"])
			self.assertEqual(goods, (output / importer.TEXT_ROOT / "refshopgoods.txt").read_bytes())
			packages = importer.tables.table_rows((output / importer.TEXT_ROOT / "refpackageitem.txt").read_bytes())
			identities = [row[2] for row in packages]
			self.assertEqual(len(identities), len(set(identities)))

	# ================
	# test_absent_client_skips_without_failing
	# ================
	def test_absent_client_skips_without_failing(self):
		with tempfile.TemporaryDirectory() as folder:
			native, output, later = self.stage(Path(folder))
			summary = importer.add_avatars(Path(folder) / "nowhere", native, output)
			self.assertFalse(summary["enabled"])
			self.assertIn("skipped", summary)
			self.assertFalse((output / importer.TEXT_ROOT / importer.OUTPUT_SHARD).exists())

	# ================
	# test_locally_changed_shard_is_not_replaced
	# ================
	def test_locally_changed_shard_is_not_replaced(self):
		with tempfile.TemporaryDirectory() as folder:
			native, output, later = self.stage(Path(folder))
			closure, check = self.patched()
			with closure, check:
				importer.add_avatars(later, native, output)
				shard = output / importer.TEXT_ROOT / importer.OUTPUT_SHARD
				shard.write_bytes(shard.read_bytes() + b"x\x00")
				with self.assertRaises(ValueError):
					importer.add_avatars(later, native, output)


ZIP_CODES = ("ITEM_CREDDY_AVATAR_M_SUIT", "ITEM_CREDDY_AVATAR_M_SUIT_HAT", "ITEM_EVENT_AVATAR_W_BALL")


# ================
# zip_bytes
#
# A pinned-archive fixture: the tables the importer reads, under the
# media/ prefix the packer writes.
# ================
def zip_bytes(codes=ZIP_CODES):
	rows = [avatar(code, FIRST_ID + index) for index, code in enumerate(codes)]
	texts = [["1", row[5], "ko", "", "tw", "cn", "de", "ja", "Suit", "vi"] for row in rows]
	prefix = "media/" + importer.tables.TEXTDATA
	entries = {
		prefix + "itemdata.txt": utf16([[importer.AVATARS_SOURCE["shard"]]]),
		prefix + importer.AVATARS_SOURCE["shard"]: utf16(rows),
		prefix + "textdata_equip&skill.txt": utf16(texts),
		prefix + "textdata_object.txt": utf16([]),
		prefix + "textdata_new.txt": utf16([]),
		"data/res/item/avatar.bsr": b"model",
	}
	with tempfile.NamedTemporaryFile(suffix=".zip", delete=False) as handle:
		path = Path(handle.name)
	avatar_archive.write_archive(path, entries)
	raw = path.read_bytes()
	path.unlink()
	return raw


class ArchiveTests(unittest.TestCase):
	# ================
	# stage
	#
	# A native tree with the mall tables, shop tabs and interface texts
	# the board listing appends to.
	# ================
	def stage(self, root):
		native, output = root / "native", root / "variant"
		base = native / importer.TEXT_ROOT
		base.mkdir(parents=True)
		(output / importer.TEXT_ROOT).mkdir(parents=True)
		(base / "itemdata.txt").write_bytes(utf16([["itemdata_5000.txt"]]))
		(base / "itemdata_5000.txt").write_bytes(utf16([native_row(10)]))
		(base / "textdataname.txt").write_bytes(utf16([]))
		for name, _ in importer.MALL_TABLES:
			(base / name).write_bytes(utf16([]))
		tabs = [
			["1", "11", "1982", "MALL_AVATAR_HAT", "MALL_AVATAR_GROUP1", "UIIT_STT_SILKMALL_HAT"],
			["1", "11", "2378", "MALL_AVATAR_DRESS", "MALL_AVATAR_GROUP1", "UIIT_STT_SILKMALL_DRESS"],
		]
		(base / importer.SHOP_TAB_TABLE).write_bytes(utf16(tabs))
		(base / "textuisystem.txt").write_bytes(utf16([["1", "UIIT_STT_SILKMALL", "ko", "", "", "cn", "tw", "ja", "Item Mall", "vi"]]))
		return native, output

	# ================
	# archive
	#
	# The fixture archive and a source pinned to its hash.
	# ================
	def archive(self, root, codes=ZIP_CODES, sha256=None):
		path = root / "opensro_avatars.zip"
		raw = zip_bytes(codes)
		path.write_bytes(raw)
		pin = root / "pins.json"
		pin.write_text(json.dumps({
			"format": importer.AVATARS_PIN_FORMAT,
			"sha256": sha256 if sha256 is not None else sro_pk2.digest(raw),
		}), encoding="utf-8")
		return path, dict(importer.AVATARS_SOURCE, pins=pin)

	# ================
	# patched
	# ================
	def patched(self):
		return (
			patch.object(importer.shared, "copy_asset_closure", return_value=[]),
			patch.object(importer, "resource_check", return_value=lambda row: []),
		)

	# ================
	# table
	# ================
	def table(self, output, name):
		return importer.tables.table_rows((output / importer.TEXT_ROOT / name).read_bytes())

	# ================
	# test_candidate_takes_any_prefix_only_for_boards
	# ================
	def test_candidate_takes_any_prefix_only_for_boards(self):
		row = avatar("ITEM_EVENT_AVATAR_M_X", FIRST_ID)
		self.assertFalse(importer.candidate(row))
		self.assertTrue(importer.candidate(row, any_prefix=True))
		self.assertFalse(importer.candidate(avatar("ITEM_CREDDY_AVATAR_M_TEST", FIRST_ID), any_prefix=True))
		self.assertEqual(importer.board_of("ITEM_EVENT_AVATAR_W_BALL"), "ev")
		self.assertEqual(importer.board_of("ITEM_CREDDY_AVATAR_M_SUIT"), "cu")

	# ================
	# test_archive_lists_avatars_on_boards
	# ================
	def test_archive_lists_avatars_on_boards(self):
		with tempfile.TemporaryDirectory() as folder:
			native, output = self.stage(Path(folder))
			path, source = self.archive(Path(folder))
			closure, check = self.patched()
			with closure, check:
				summary = importer.add_source(path, native, output, source)
			self.assertEqual(summary["added"], 3)
			self.assertEqual(summary["source"], "avatars")
			self.assertEqual(summary["laterArchiveHashes"], {"archive": sro_pk2.digest(path.read_bytes())})
			goods = self.table(output, "refshopgoods.txt")
			by_tab = {}
			for row in goods:
				by_tab.setdefault(row[2], []).append(row)
			self.assertEqual(sorted(by_tab), [importer.BOARD_TABS["cu"], importer.BOARD_TABS["ev"]])
			self.assertEqual([row[4] for row in by_tab[importer.BOARD_TABS["cu"]]], ["0", "1"])
			self.assertEqual([row[4] for row in by_tab[importer.BOARD_TABS["ev"]]], ["0"])
			prices = {row[2]: row[4] for row in self.table(output, "refpricepolicyofitem.txt")}
			self.assertEqual(prices["PACKAGE_ITEM_CREDDY_AVATAR_M_SUIT"], "130")
			self.assertEqual(prices["PACKAGE_ITEM_CREDDY_AVATAR_M_SUIT_HAT"], "30")
			tabs = {row[3]: row for row in self.table(output, importer.SHOP_TAB_TABLE)}
			for board in ("cu", "ev"):
				row = tabs[importer.BOARD_TABS[board]]
				self.assertEqual((row[1], row[4], row[5]), ("11", "MALL_AVATAR_GROUP1", importer.BOARD_LABELS[board]))
			self.assertEqual({int(tabs[code][2]) for code in (importer.BOARD_TABS["cu"], importer.BOARD_TABS["ev"])}, {2379, 2380})
			labels = {row[1]: row[8] for row in self.table(output, "textuisystem.txt") if len(row) > 8}
			self.assertEqual(labels[importer.BOARD_LABELS["cu"]], "Custom avatars")
			self.assertEqual(labels[importer.BOARD_LABELS["ev"]], "Event avatars")
			marker = json.loads((output / source["report"]).read_text(encoding="utf-8"))
			self.assertEqual(marker["mall"]["tabs"], [importer.BOARD_TABS["cu"], importer.BOARD_TABS["ev"]])

	# ================
	# test_board_pages_hold_one_byte_of_goods
	# ================
	def test_board_pages_hold_one_byte_of_goods(self):
		codes = tuple(f"ITEM_CREDDY_AVATAR_M_SET_{index:03d}" for index in range(importer.MALL_SLOTS + 1))
		with tempfile.TemporaryDirectory() as folder:
			native, output = self.stage(Path(folder))
			path, source = self.archive(Path(folder), codes)
			closure, check = self.patched()
			with closure, check:
				summary = importer.add_source(path, native, output, source)
			self.assertEqual(summary["mall"]["listed"], importer.MALL_SLOTS + 1)
			goods = self.table(output, "refshopgoods.txt")
			pages = {}
			for row in goods:
				pages.setdefault(row[2], []).append(int(row[4]))
			self.assertEqual(sorted(pages), [importer.BOARD_TABS["cu"], importer.BOARD_TABS["cu"] + "_2"])
			self.assertEqual(pages[importer.BOARD_TABS["cu"]], list(range(importer.MALL_SLOTS)))
			self.assertEqual(pages[importer.BOARD_TABS["cu"] + "_2"], [0])

	# ================
	# test_rebuild_is_stable_and_removal_strips_the_boards
	# ================
	def test_rebuild_is_stable_and_removal_strips_the_boards(self):
		with tempfile.TemporaryDirectory() as folder:
			native, output = self.stage(Path(folder))
			path, source = self.archive(Path(folder))
			closure, check = self.patched()
			with closure, check:
				first = importer.add_source(path, native, output, source)
				second = importer.add_source(path, native, output, source)
				self.assertEqual(first["idMap"], second["idMap"])
				tabs = self.table(output, importer.SHOP_TAB_TABLE)
				self.assertEqual(sum(row[3].startswith("MALL_AVATAR_BOARD") for row in tabs), 2)
				labels = [row for row in self.table(output, "textuisystem.txt") if len(row) > 1 and row[1].startswith("UIIT_STT_SILKMALL_AVATAR_BOARD")]
				self.assertEqual(len(labels), 2)
				self.assertFalse(importer.add_source(None, native, output, source)["enabled"])
			self.assertEqual(self.table(output, "refshopgoods.txt"), [])
			self.assertFalse(any(row[3].startswith("MALL_AVATAR_BOARD") for row in self.table(output, importer.SHOP_TAB_TABLE)))
			self.assertFalse(any(len(row) > 1 and row[1].startswith("UIIT_STT_SILKMALL_AVATAR_BOARD") for row in self.table(output, "textuisystem.txt")))
			self.assertFalse((output / importer.TEXT_ROOT / source["shard"]).exists())
			self.assertFalse((output / source["report"]).exists())

	# ================
	# test_absent_archive_changes_nothing
	# ================
	def test_absent_archive_changes_nothing(self):
		with tempfile.TemporaryDirectory() as folder:
			native, output = self.stage(Path(folder))
			_, source = self.archive(Path(folder))
			before = {path.name: path.read_bytes() for path in (output / importer.TEXT_ROOT).iterdir()}
			for root in (None, Path(folder) / "nowhere.zip"):
				summary = importer.add_source(root, native, output, source)
				self.assertFalse(summary["enabled"])
			after = {path.name: path.read_bytes() for path in (output / importer.TEXT_ROOT).iterdir()}
			self.assertEqual(before, after)
			self.assertFalse((output / source["report"]).exists())

	# ================
	# test_wrong_pin_is_refused
	# ================
	def test_wrong_pin_is_refused(self):
		with tempfile.TemporaryDirectory() as folder:
			native, output = self.stage(Path(folder))
			path, source = self.archive(Path(folder), sha256="0" * 64)
			with self.assertRaises(SystemExit):
				importer.add_source(path, native, output, source)


if __name__ == "__main__":
	unittest.main()
