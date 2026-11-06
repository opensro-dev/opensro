"""
===========================================================================

test_mall_avatar_import.py - the mall avatar import, on by default

Synthetic rows cover the default-on switch, selection, identity retention
and reassignment, skipped rows, the manifest shared with the cap-110 item
import, the on/off/on rebuild and the absent-client skip.

===========================================================================
"""
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import cap110_items as items  # noqa: E402
import mall_avatar_import as importer  # noqa: E402
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
	# test_attachments_take_the_hat_tab_after_present_slots
	# ================
	def test_attachments_take_the_hat_tab_after_present_slots(self):
		later, current, avatars = self.tables()
		rows, _ = importer.mall_rows(later, avatars, current, {1})
		slots = {row[3]: (row[2], row[4]) for row in rows["refshopgoods.txt"]}
		self.assertEqual(slots["PACKAGE_MALL_AVATAR_M_TURKEY_HAT"], ("MALL_AVATAR_HAT", "2"))
		self.assertEqual(slots["PACKAGE_MALL_AVATAR_M_TURKEY_ATTACH"], ("MALL_AVATAR_HAT", "3"))
		self.assertEqual(slots["PACKAGE_MALL_AVATAR_M_TURKEY"], ("MALL_AVATAR_DRESS", "0"))

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
	def stage(self, root):
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


if __name__ == "__main__":
	unittest.main()
