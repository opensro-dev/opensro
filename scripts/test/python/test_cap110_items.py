"""
===========================================================================

test_cap110_items.py - equipment conversion, identity and optional build boundaries

Synthetic rows cover both races, native precedence, complete statistics,
degree-12 link trimming, translation ownership and off/on/off rebuilds.

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
import cap110_item_import as importer  # noqa: E402
from test_client_preparation import ArchiveBuilder  # noqa: E402


# ================
# weapon
# ================
def weapon(code="ITEM_CH_SWORD_10_A", identity=10, race="0", item_class=28):
	row = ["0"] * items.ITEM_COLUMNS
	row[0:4] = ["1", str(identity), code, "unused source name"]
	row[4] = "xxx"
	row[5], row[6] = "SN_" + code, "SN_" + code + "_TT_DESC"
	row[9:13], row[14], row[61] = ["3", "1", "6", "2"], race, str(item_class)
	row[33], row[59], row[60] = "90", "123", "456"
	row[52:55] = ["item/weapon.bsr", "item/drop.bsr", "item/weapon.ddj"]
	for column in range(items.PARAM_FIRST, items.PARAM_END, items.PARAM_STRIDE):
		row[column:column + items.PARAM_STRIDE] = ["-1", "xxx"]
	row[156:160] = ["3", "metadata", "9", "0"]
	return row


# ================
# utf16
# ================
def utf16(rows):
	return b"\xff\xfe" + ("\r\n".join("\t".join(row) for row in rows) + "\r\n").encode("utf-16-le")


# ================
# archive_bytes
# ================
def archive_bytes(files):
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
# set_group
#
# Link cells pair a requirement figure with a magic group id, as in the
# later table: armour A reads 0, 21/2, 0/3, 0/4 across the link range.
# ================
def set_group(group=13, code="ITEM_11_SET_A_CH_M_HEAVY", links=(0, 21, 2, 0, 3, 0, 4)):
	row = ["1", str(group), code, "later name", "SN_" + code, "xxx"] + [str(value) for value in links]
	return row + ["0"] * (19 - len(row))


# ================
# magic_group
# ================
def magic_group(group=2, code="MAGIC_11_SET_A_DEFENSE_2", option=389):
	return ["1", str(group), "2", code, str(option), "1", "1", "0", "xxx", "0", "xxx", ""]


# ================
# magic_option
# ================
def magic_option(option=389):
	return ["1", str(option), "MATTR_TEST", "+", "1", "1", "0", "0"]


# ================
# WeaponTests
# ================
class WeaponTests(unittest.TestCase):
	# ================
	# test_all_equipment_families_preserve_stats_and_requirements
	# ================
	def test_all_equipment_families_preserve_stats_and_requirements(self):
		rows = []
		for kind in items.EQUIPMENT_TYPES:
			row = weapon(f"ITEM_CH_TEST_{kind}_10_A", len(rows) + 1)
			row[items.TYPE_THIRD] = kind
			rows.append(row)
		added, report = items.merge_equipment([], rows)
		self.assertEqual(len(added), len(items.EQUIPMENT_TYPES))
		self.assertEqual(report["categories"], {"accessories": 2, "armor": 6, "shields": 1, "weapons": 1})
		original = {row[2]: row for row in rows}
		for row in added:
			self.assertEqual(row[4:], original[row[2]][4:])

	# ================
	# test_published_weapon_ids_survive_expanding_the_import
	# ================
	def test_published_weapon_ids_survive_expanding_the_import(self):
		sword = weapon(identity=100)
		_, previous = items.merge_equipment([], [sword])
		armor = weapon("ITEM_CH_M_HEAVY_10_BA_A", 50)
		armor[11] = "3"
		_, expanded = items.merge_equipment([], [armor, sword], previous_ids=previous["idMap"])
		self.assertEqual(expanded["idMap"][sword[2]], previous["idMap"][sword[2]])
		self.assertGreater(expanded["idMap"][armor[2]]["target"], previous["idMap"][sword[2]]["target"])
		_, fresh = items.merge_equipment([], [armor, sword])
		self.assertEqual(fresh["idMap"], expanded["idMap"])
		with self.assertRaises(ValueError):
			items.merge_equipment([], [armor, sword], [101], previous["idMap"])

	# ================
	# test_armor_links_require_complete_opposite_sex_rows
	# ================
	def test_armor_links_require_complete_opposite_sex_rows(self):
		male = weapon("ITEM_CH_M_HEAVY_11_BA_A_RARE", 1, item_class=31)
		female = weapon("ITEM_CH_W_HEAVY_11_BA_A_RARE", 2, item_class=31)
		male[11], female[11] = "3", "3"
		male[4], female[4] = female[2], male[2]
		male[118:120] = ["1", "ITEM_CH_M_HEAVY_12_BA_C_RARE"]
		added, report = items.merge_equipment([], [male, female])
		self.assertEqual(len(added), 2)
		self.assertEqual(report["trimmedOutOfScopeItemLinks"][0]["target"], male[119])
		with self.assertRaisesRegex(ValueError, "unresolved linked equipment"):
			items.merge_equipment([], [male])

	# ================
	# test_set_rows_import_with_their_families_but_non_equipment_does_not
	# ================
	def test_set_rows_import_with_their_families_but_non_equipment_does_not(self):
		rows = [weapon(f"ITEM_CH_TEST_{kind}_10_A", index + 1) for index, kind in enumerate(("4", "5", "3", "7"))]
		for row, kind in zip(rows, ("4", "5", "3", "7")):
			row[11], row[62] = kind, "39"
		added, report = items.merge_equipment([], rows)
		self.assertEqual(len(added), 3)
		self.assertEqual([entry["setId"] for entry in report["sets"]], [39, 39, 39])
		self.assertTrue(all(row[62] == "39" for row in added))
	# ================
	# test_remapping_preserves_every_stat_and_native_input
	# ================
	def test_remapping_preserves_every_stat_and_native_input(self):
		native = weapon(identity=100)
		later_existing = weapon(identity=1)
		later_existing[59] = "999"
		new = weapon("ITEM_EU_SWORD_11_A", 100, "1", 31)
		before = list(native)
		added, report = items.merge_equipment([native], [later_existing, new], [500])
		self.assertEqual(native, before)
		self.assertEqual(len(added), 1)
		self.assertEqual(added[0][1], "501")
		self.assertEqual(added[0][4:], new[4:])
		self.assertEqual(added[0][3], "xxx")
		self.assertEqual(report["existingNative"], [native[2]])

	# ================
	# test_set_weapons_import_but_event_items_do_not
	# ================
	def test_set_weapons_import_but_event_items_do_not(self):
		set_weapon = weapon("ITEM_CH_SWORD_11_SET_A", 1, item_class=31)
		set_weapon[62] = "39"
		event = weapon("ITEM_EVENT_CH_SWORD_10_A", 2)
		added, report = items.merge_equipment([], [set_weapon, event])
		self.assertEqual([row[2] for row in added], ["ITEM_CH_SWORD_11_SET_A"])
		self.assertEqual(report["sets"], [{"code": "ITEM_CH_SWORD_11_SET_A", "setId": 39}])

	# ================
	# test_set_catalogue_validates_the_whole_chain
	# ================
	def test_set_catalogue_validates_the_whole_chain(self):
		piece = weapon("ITEM_CH_M_HEAVY_11_SET_A", 1, item_class=31)
		piece[11], piece[62] = "3", "13"
		equipment, _report = items.merge_equipment([], [piece])
		groups = [set_group(13)]
		magic = [
			magic_group(2, "MAGIC_11_SET_A_DEFENSE_2", 389),
			magic_group(3, "MAGIC_11_SET_A_DEFENSE_4", 390),
			magic_group(4, "MAGIC_11_SET_A_DEFENSE_6", 389),
			magic_group(99, "MAGIC_ENCHANT_2", 389),
		]
		written_groups, written_magic, report = items.set_catalogue(
			groups, magic, [magic_option(389), magic_option(390)], equipment)
		self.assertEqual(written_groups, groups)
		self.assertEqual([row[3] for row in written_magic], [
			"MAGIC_11_SET_A_DEFENSE_2", "MAGIC_11_SET_A_DEFENSE_4", "MAGIC_11_SET_A_DEFENSE_6"])
		self.assertEqual(report, {"groups": 1, "magicGroups": 3, "optionIds": [389, 390], "setItems": 1})

	# ================
	# test_set_catalogue_fails_on_any_missing_link
	# ================
	def test_set_catalogue_fails_on_any_missing_link(self):
		piece = weapon("ITEM_CH_M_HEAVY_11_SET_A", 1, item_class=31)
		piece[11], piece[62] = "3", "13"
		equipment, _report = items.merge_equipment([], [piece])
		groups = [set_group(13)]
		magic = [magic_group(2), magic_group(3), magic_group(4)]
		options = [magic_option(389)]
		items.set_catalogue(groups, magic, options, equipment)
		with self.assertRaisesRegex(ValueError, "unknown set group"):
			items.set_catalogue([], magic, options, equipment)
		with self.assertRaisesRegex(ValueError, "unknown magic group"):
			items.set_catalogue(groups, magic[:2], options, equipment)
		with self.assertRaisesRegex(ValueError, "unknown magic option"):
			items.set_catalogue(groups, magic, [], equipment)

	# ================
	# test_set_catalogue_accepts_linkless_weapon_and_shield_groups
	# ================
	def test_set_catalogue_accepts_linkless_weapon_and_shield_groups(self):
		groups = [set_group(37, "ITEM_11_SET_B_WEAPON", ()), set_group(42, "ITEM_11_SET_B_SHIELD", ())]
		_written_groups, written_magic, report = items.set_catalogue(groups, [], [], [])
		self.assertEqual(written_magic, [])
		self.assertEqual(report["magicGroups"], 0)

	# ================
	# test_only_named_degree12_links_are_trimmed
	# ================
	def test_only_named_degree12_links_are_trimmed(self):
		row = weapon("ITEM_CH_SWORD_11_A_RARE", item_class=31)
		row[118:120] = ["1", "ITEM_CH_SWORD_12_A_RARE"]
		added, report = items.merge_equipment([], [row])
		self.assertEqual(added[0][118:120], ["-1", "xxx"])
		self.assertFalse(report["trimmedOutOfScopeItemLinks"][0]["targetPresent"])
		self.assertEqual(added[0][4:118], row[4:118])
		row[119] = "ITEM_EU_SWORD_12_A_RARE"
		with self.assertRaises(ValueError):
			items.merge_equipment([], [row])

	# ================
	# test_set_rare_links_trim_against_the_plain_degree12_family
	#
	# Real set rows link every param slot to the plain degree-12 family
	# (ITEM_CH_BLADE_11_SET_A_RARE -> ITEM_CH_BLADE_12_{A,B,C}_RARE);
	# no degree-12 set family exists for the link to name.
	# ================
	def test_set_rare_links_trim_against_the_plain_degree12_family(self):
		for grade in ("A", "B"):
			row = weapon(f"ITEM_CH_BLADE_11_SET_{grade}_RARE", item_class=31)
			row[items.SET] = "39"
			for slot, target in enumerate(("ITEM_CH_BLADE_12_A_RARE", "ITEM_CH_BLADE_12_B_RARE", "ITEM_CH_BLADE_12_C_RARE")):
				row[118 + slot * 2:120 + slot * 2] = ["1", target]
			added, report = items.merge_equipment([], [row])
			self.assertEqual(added[0][118:124], ["-1", "xxx"] * 3)
			self.assertEqual(
				[entry["target"] for entry in report["trimmedOutOfScopeItemLinks"]],
				["ITEM_CH_BLADE_12_A_RARE", "ITEM_CH_BLADE_12_B_RARE", "ITEM_CH_BLADE_12_C_RARE"],
			)
			row[119] = "ITEM_CH_BLADE_12_SET_A_RARE"
			with self.assertRaises(ValueError):
				items.merge_equipment([], [row])
		# Armour set rows keep their kind segment: only SET_<grade> drops.
		armor = weapon("ITEM_CH_M_CLOTHES_11_AA_SET_B_RARE", item_class=31)
		armor[items.TYPE_THIRD], armor[items.SET] = "1", "21"
		for slot, target in enumerate(("ITEM_CH_M_CLOTHES_12_AA_A_RARE", "ITEM_CH_M_CLOTHES_12_AA_B_RARE", "ITEM_CH_M_CLOTHES_12_AA_C_RARE")):
			armor[118 + slot * 2:120 + slot * 2] = ["1", target]
		added, report = items.merge_equipment([], [armor])
		self.assertEqual(added[0][118:124], ["-1", "xxx"] * 3)
		self.assertEqual(len(report["trimmedOutOfScopeItemLinks"]), 3)

	# ================
	# test_duplicate_ids_and_short_rows_fail
	# ================
	def test_duplicate_ids_and_short_rows_fail(self):
		for rows in ([weapon(), weapon("ITEM_CH_BLADE_10_A")], [weapon()[:-1]]):
			with self.assertRaises(ValueError):
				items.merge_equipment([], rows)

	# ================
	# test_shop_inventory_reports_legacy_and_package_references
	# ================
	def test_shop_inventory_reports_legacy_and_package_references(self):
		row = weapon()
		scrap = ["1", "15", "PACKAGE", row[2]] + ["0"] * 16
		other = ["1", "15", "PACKAGE", "ITEM_OTHER"] + ["0"] * 16
		report = items.shop_dependencies({
			"refscrapofpackageitem.txt": [scrap, other],
			"refshopgoods.txt": [["1", "15", "TAB", "PACKAGE", "0"]],
			"refpricepolicyofitem.txt": [["1", "15", "PACKAGE", "1", "1000"]],
			"shopitemdata.txt": [["1", "123", "10"]]
		}, [row], set(), {10: row[2]})
		self.assertEqual(report["otherMissingItems"], ["ITEM_OTHER"])
		self.assertEqual(report["tabs"], ["TAB"])
		self.assertEqual(report["legacyStock"][0]["code"], row[2])

	# ================
	# test_names_required_descriptions_optional_native_text_wins
	# ================
	def test_names_required_descriptions_optional_native_text_wins(self):
		row = weapon()
		translation = ["1", row[5], "ko", "", "tw", "cn", "de", "ja", "English sword", "vi"]
		merged, absent = items.merge_texts(utf16([]), [translation], [row])
		self.assertEqual(importer.tables.table_rows(merged)[0][8], "English sword")
		self.assertEqual(absent, [row[6]])
		with self.assertRaises(ValueError):
			items.merge_texts(utf16([]), [], [row])
		original = utf16([["1", row[5], "", "", "", "", "", "", "Native name", "", ""]])
		merged, _ = items.merge_texts(original, [translation], [row])
		self.assertEqual(merged, original)


# ================
# ImportTests
# ================
class ImportTests(unittest.TestCase):
	# ================
	# test_ambient_particle_dependencies_cross_archive_boundaries
	# ================
	def test_ambient_particle_dependencies_cross_archive_boundaries(self):
		row = weapon("ITEM_CH_M_CLOTHES_11_SA_A", item_class=31)
		row[11], row[12] = "1", "2"
		with tempfile.TemporaryDirectory() as folder:
			root = Path(folder)
			files = {
				"Data_extracted/res/item/weapon.bsr": b"model",
				"Data_extracted/res/item/drop.bsr": b"drop",
				"Media_extracted/icon/item/weapon.ddj": b"icon",
				"Particles_extracted/system/shoulder.efp": b"effect",
				"Particles_extracted/geometry/orb.bms": b"mesh",
				"Particles_extracted/textures/orb.ddj": b"texture",
			}
			for name, raw in files.items():
				path = root / "native" / name
				path.parent.mkdir(parents=True, exist_ok=True)
				path.write_bytes(raw)
			# ================
			# dependencies
			# ================
			def dependencies(requests):
				result = []
				for request in requests:
					if "weapon" in request:
						result.append(["res/item/weapon.bsr"])
					elif request["path"] == "res/item/weapon.bsr":
						result.append([{"archive": "Particles", "path": "system/shoulder.efp"}])
					elif request["path"].endswith(".efp"):
						result.append([{"archive": "Particles", "path": name} for name in ("orb.bms", "textures/orb.ddj")])
					else:
						result.append([])
				return result
			archives = {name: (None, {}) for name in importer.FOLDERS}
			with patch.object(importer, "resource_dependencies", side_effect=dependencies):
				assets = importer.copy_asset_closure([row], root / "native", root / "out", archives)
			self.assertEqual(len(assets), len(files))
			for name, raw in files.items():
				self.assertEqual((root / "out" / name).read_bytes(), raw)
	# ================
	# test_set_armour_comma_models_keep_the_plain_path
	#
	# Degree-11 set armour authors MODEL as a comma pair (plain and
	# full-set variant). The import keeps the plain path in the cell and
	# copies only its closure; the full-set variant is never requested.
	# ================
	def test_set_armour_comma_models_keep_the_plain_path(self):
		row = weapon("ITEM_CH_M_CLOTHES_11_HA_SET_A", item_class=31)
		row[11], row[62] = "1", "15"
		row[52] = "item/set_a.bsr,item/set_a_fullset.bsr"
		self.assertEqual(items.plain_worn_models([row]), 1)
		self.assertEqual(row[52], "item/set_a.bsr")
		self.assertEqual(items.plain_worn_models([row]), 0)
		with tempfile.TemporaryDirectory() as folder:
			root = Path(folder)
			native_only = {"Data_extracted/res/item/set_a_fullset.bsr": b"fullset"}
			files = {
				"Data_extracted/res/item/set_a.bsr": b"normal",
				"Data_extracted/res/item/drop.bsr": b"drop",
				"Media_extracted/icon/item/weapon.ddj": b"icon",
			}
			for name, raw in native_only.items():
				path = root / "native" / name
				path.parent.mkdir(parents=True, exist_ok=True)
				path.write_bytes(raw)
			for name, raw in files.items():
				path = root / "native" / name
				path.parent.mkdir(parents=True, exist_ok=True)
				path.write_bytes(raw)
			seen = []

			def dependencies(requests):
				seen.extend(requests)
				return [[request["weapon"]["resolvedModel"]] if "weapon" in request else [] for request in requests]

			archives = {name: (None, {}) for name in importer.FOLDERS}
			with patch.object(importer, "resource_dependencies", side_effect=dependencies):
				assets = importer.copy_asset_closure([row], root / "native", root / "out", archives)
			self.assertEqual(
				{request["weapon"]["resolvedModel"] for request in seen if "weapon" in request},
				{"res/item/set_a.bsr"},
			)
			self.assertEqual(len(assets), len(files))
			for name, raw in files.items():
				self.assertEqual((root / "out" / name).read_bytes(), raw)
			self.assertFalse((root / "out" / "Data_extracted/res/item/set_a_fullset.bsr").exists())

	# ================
	# test_accessories_have_drop_and_icon_assets_without_a_worn_model
	# ================
	def test_accessories_have_drop_and_icon_assets_without_a_worn_model(self):
		row = weapon("ITEM_CH_RING_10_A")
		row[11], row[52] = "5", "xxx"
		with tempfile.TemporaryDirectory() as folder:
			root = Path(folder)
			for name, resource in (("Data_extracted", row[53]), ("Media_extracted", row[54])):
				prefix = "res/" if name == "Data_extracted" else "icon/"
				path = root / "native" / name / (prefix + resource)
				path.parent.mkdir(parents=True, exist_ok=True)
				path.write_bytes(b"native fixture")
			with patch.object(importer, "resource_dependencies", side_effect=lambda requests: [[] for _ in requests]):
				assets = importer.copy_asset_closure([row], root / "native", root / "out", {"Data": (None, {}), "Media": (None, {})})
			self.assertEqual(len(assets), 2)
	# ================
	# test_split_weapons_and_both_harp_models_use_the_shared_resolver
	# ================
	def test_split_weapons_and_both_harp_models_use_the_shared_resolver(self):
		requests = [{"weapon": {"resolvedModel": "res/weapon.bsr", "tid": [3, 1, 6, kind]}} for kind in (9, 13, 14, 2)]
		paths = importer.resource_dependencies(requests)
		self.assertEqual(set(paths[0]), {"res/weapon_r.bsr", "res/weapon_l.bsr"})
		self.assertEqual(set(paths[1]), set(paths[0]))
		self.assertEqual(set(paths[2]), {"res/weapon.bsr", "res/weapon_f.bsr"})
		self.assertEqual(paths[3], ["res/weapon.bsr"])

	# ================
	# test_off_on_off_keeps_native_hardlinks_unchanged
	# ================
	def test_off_on_off_keeps_native_hardlinks_unchanged(self):
		with tempfile.TemporaryDirectory() as folder:
			root = Path(folder)
			native, output, later = root / "native", root / "variant", root / "later"
			base, target = native / importer.TEXT_ROOT, output / importer.TEXT_ROOT
			base.mkdir(parents=True)
			target.mkdir(parents=True)
			later.mkdir()
			original_manifest = utf16([["itemdata_5000.txt"]])
			(base / "itemdata.txt").write_bytes(original_manifest)
			(base / "itemdata_5000.txt").write_bytes(utf16([weapon()]))
			(base / "textdataname.txt").write_bytes(utf16([]))
			for name in ("itemdata.txt", "textdataname.txt"):
				os.link(base / name, target / name)
			with patch.dict(os.environ, {importer.FLAG: "off"}):
				self.assertFalse(importer.add_items(later, native, output)["enabled"])
			new = weapon("ITEM_EU_SWORD_11_A", 11, "1", 31)
			files = {
				"itemdata.txt": original_manifest, "itemdata_5000.txt": utf16([new]),
				"textdata_equip&skill.txt": utf16([["1", new[5], "ko", "", "tw", "cn", "de", "ja", "Sword", "vi"]]),
				"textdata_object.txt": utf16([]), "textdata_new.txt": utf16([]),
				"refscrapofpackageitem.txt": utf16([]), "refshopgoods.txt": utf16([]),
				"refpricepolicyofitem.txt": utf16([]),
				"shopitemdata.txt": utf16([]),
				"refsetitemgroup.txt": utf16([set_group(13)]),
				"refmagicoptgroup.txt": utf16([magic_group(2), magic_group(3), magic_group(4)]),
				"magicoption.txt": utf16([magic_option(389)]),
			}
			(later / "Media.pk2").write_bytes(archive_bytes(files))
			(later / "Data.pk2").write_bytes(archive_bytes({}))
			(later / "Particles.pk2").write_bytes(archive_bytes({}))
			with patch.dict(os.environ, {importer.FLAG: "on"}), patch.object(importer, "copy_asset_closure", return_value=[]):
				first = importer.add_items(later, native, output)
				second = importer.add_items(later, native, output)
			self.assertEqual(first["idMap"], second["idMap"])
			self.assertEqual(first["setCatalogue"], {"groups": 1, "magicGroups": 3, "optionIds": [389], "setItems": 0})
			self.assertEqual(importer.tables.table_rows((target / items.SET_GROUP_TABLE).read_bytes()), [set_group(13)])
			self.assertEqual((base / "itemdata.txt").read_bytes(), original_manifest)
			self.assertEqual((base / "textdataname.txt").read_bytes(), utf16([]))
			with patch.dict(os.environ, {importer.FLAG: "off"}):
				self.assertFalse(importer.add_items(later, native, output)["enabled"])
			self.assertFalse((target / items.OUTPUT_SHARD).exists())
			self.assertFalse((target / items.SET_GROUP_TABLE).exists())
			self.assertFalse((target / items.MAGIC_GROUP_TABLE).exists())
			self.assertEqual((target / "itemdata.txt").read_bytes(), original_manifest)

	# ================
	# test_set_item_without_its_group_fails_the_import
	# ================
	def test_set_item_without_its_group_fails_the_import(self):
		with tempfile.TemporaryDirectory() as folder:
			root = Path(folder)
			native, output, later = root / "native", root / "variant", root / "later"
			base, target = native / importer.TEXT_ROOT, output / importer.TEXT_ROOT
			base.mkdir(parents=True)
			target.mkdir(parents=True)
			later.mkdir()
			manifest = utf16([["itemdata_5000.txt"]])
			(base / "itemdata.txt").write_bytes(manifest)
			(base / "itemdata_5000.txt").write_bytes(utf16([weapon()]))
			(base / "textdataname.txt").write_bytes(utf16([]))
			piece = weapon("ITEM_CH_M_HEAVY_11_SET_A", 11, item_class=31)
			piece[11], piece[62] = "3", "13"
			files = {
				"itemdata.txt": manifest, "itemdata_5000.txt": utf16([piece]),
				"textdata_equip&skill.txt": utf16([["1", piece[5], "ko", "", "tw", "cn", "de", "ja", "Set armor", "vi"]]),
				"textdata_object.txt": utf16([]), "textdata_new.txt": utf16([]),
				"refscrapofpackageitem.txt": utf16([]), "refshopgoods.txt": utf16([]),
				"refpricepolicyofitem.txt": utf16([]),
				"shopitemdata.txt": utf16([]),
				"refsetitemgroup.txt": utf16([]),
				"refmagicoptgroup.txt": utf16([]),
				"magicoption.txt": utf16([]),
			}
			(later / "Media.pk2").write_bytes(archive_bytes(files))
			(later / "Data.pk2").write_bytes(archive_bytes({}))
			(later / "Particles.pk2").write_bytes(archive_bytes({}))
			with patch.dict(os.environ, {importer.FLAG: "on"}), patch.object(importer, "copy_asset_closure", return_value=[]):
				with self.assertRaisesRegex(ValueError, "unknown set group"):
					importer.add_items(later, native, output)

	# ================
	# test_paths_cannot_escape_and_native_cannot_be_output
	# ================
	def test_paths_cannot_escape_and_native_cannot_be_output(self):
		for name in ("../x", "C:/x", "item//x", "/x"):
			with self.assertRaises(ValueError):
				importer.normalized_path(name)
		with tempfile.TemporaryDirectory() as folder:
			root = Path(folder)
			with self.assertRaises(ValueError):
				importer.add_items(root, root, root)

	# ================
	# test_unknown_asset_refuses_certificate
	# ================
	def test_unknown_asset_refuses_certificate(self):
		with tempfile.TemporaryDirectory() as folder:
			root = Path(folder)
			with self.assertRaisesRegex(ValueError, "missing .* resource"):
				importer.copy_asset_closure([weapon()], root / "native", root / "output", {"Data": (None, {}), "Media": (None, {})})


if __name__ == "__main__":
	unittest.main()
