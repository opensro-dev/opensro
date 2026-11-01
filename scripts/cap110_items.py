"""
===========================================================================

cap110_items.py - optional degree 10/11 equipment in the v1.150 layout

Port-only, not native. Preserves every native row and remaps imported item
identities by codename. Weapons, shields, armor and accessories share the
160-cell layout; set rows import with the rest, dormant: nothing on the
server or the client reads their Set cell yet, and their group catalogue
lands beside them for the later mechanics port. Does not invent prices,
statistics, shops, drops or set bonuses.

===========================================================================
"""
from collections import Counter
import re

import alexandria_npcs as text_tables
import import_vsro_alexandria as tables
from build_alexandria_variant import native_text_row

ITEM_COLUMNS = 160
SERVICE = 0
IDENTITY = 1
CODENAME = 2
PLACEHOLDER_NAME = 3
NAME_KEY = 5
DESCRIPTION_KEY = 6
TYPE_FIRST = 9
TYPE_SECOND = 10
TYPE_THIRD = 11
TYPE_FOURTH = 12
RACE = 14
REQUIRED_LEVEL = 33
MODEL = 52
DROP_MODEL = 53
ICON = 54
CLASS = 61
SET = 62
PARAM_FIRST = 118
PARAM_END = 158
PARAM_DESCRIPTION = 119
PARAM_STRIDE = 2
PARAM_LAST = 156
DEGREES = (10, 11)
CLASSES_PER_DEGREE = 3
RACES = ("0", "1")
ARMOR_TYPES = ("1", "2", "3", "9", "10", "11")
ACCESSORY_TYPES = ("5", "12")
EQUIPMENT_TYPES = ARMOR_TYPES + ACCESSORY_TYPES + ("4", "6")
LINKED_CODE = 4
OUTPUT_SHARD = "itemdata_110000.txt"
SET_GROUP_TABLE = "refsetitemgroup.txt"
MAGIC_GROUP_TABLE = "refmagicoptgroup.txt"
MAGIC_OPTION_TABLE = "magicoption.txt"
SET_GROUP_ID = 1
SET_GROUP_CODE = 2
SET_LINK_FIRST = 6
SET_LINK_LAST = 17
MAGIC_GROUP_ID = 1
MAGIC_GROUP_CODE = 3
MAGIC_GROUP_OPTION = 4
MAGIC_OPTION_ID = 1
SET_MAGIC_PREFIX = "MAGIC_11_SET_"
UNUSED = "xxx"
MAX_ID = 0xFFFFFFFF
PACKAGE_CODE = 2
PACKAGE_ITEM = 3
SHOP_PACKAGE = 3
SHOP_TAB = 2
PRICE_CURRENCY = 3
PRICE_AMOUNT = 4
LEGACY_SHOP_TAB = 1
LEGACY_SHOP_ITEM = 2


# ================
# index_items
# ================
def index_items(rows):
	by_code, by_id = {}, {}
	for row in rows:
		if len(row) != ITEM_COLUMNS:
			raise ValueError(f"item row must have {ITEM_COLUMNS} columns")
		code, identity = row[CODENAME], int(row[IDENTITY])
		if not code.startswith("ITEM_") or not 0 < identity <= MAX_ID:
			raise ValueError("invalid item identity")
		if code in by_code or identity in by_id:
			raise ValueError(f"duplicate item identity: {code}")
		by_code[code], by_id[identity] = row, code
	return by_code


# ================
# equipment_degree
#
# Existing enterworld.EquipmentDegree owns the same three-classes rule.
# Selection uses typed cells; the codename prefix only excludes event items.
# ================
def equipment_degree(row):
	if row[SERVICE] != "1" or row[TYPE_FIRST:TYPE_SECOND + 1] != ["3", "1"]:
		return 0
	if row[TYPE_THIRD] not in EQUIPMENT_TYPES:
		return 0
	if row[RACE] not in RACES or not row[CODENAME].startswith(("ITEM_CH_", "ITEM_EU_")):
		return 0
	item_class = int(row[CLASS])
	return (item_class - 1) // CLASSES_PER_DEGREE + 1 if item_class > 0 else 0


# ================
# convert_equipment
#
# Later degree-11 rare rows also name degree-12 items in parameter labels.
# Scope inference, port-only: v1.150 has no such conversion feature, and
# degree 12 is outside this expansion. Remove only verified out-of-scope
# item links, recording them rather than importing unsupported content.
# Set rows convert with the rest; their Set cell survives untouched as
# dormant data, bound to its group catalogue by set_catalogue below.
# ================
def convert_equipment(row, identity, later):
	if equipment_degree(row) not in DEGREES:
		raise ValueError(f"unsupported equipment family: {row[CODENAME]}")
	if not 0 < identity <= MAX_ID:
		raise ValueError("imported item ID exceeds the wire width")
	converted = list(row)
	trimmed = []
	family = re.fullmatch(r"(ITEM_(?:CH|EU)_[A-Z_]+)_11_([A-Z_]*?)A_RARE", row[CODENAME])
	expected = {f"{family[1]}_12_{family[2]}{grade}_RARE" for grade in ("A", "B", "C")} if family else set()
	if int(row[SET]) != 0:
		# Set rows link to the plain degree-12 family, not a set one: the
		# SET_<grade> segment drops out of the expected names, so
		# ITEM_CH_BLADE_11_SET_A_RARE names ITEM_CH_BLADE_12_{A,B,C}_RARE
		# and ITEM_CH_M_CLOTHES_11_AA_SET_B_RARE names the AA family. The
		# plain family regex would carry SET_ into the names (and misses
		# SET_B rows entirely, since it requires an A_RARE tail).
		set_family = re.fullmatch(r"(ITEM_(?:CH|EU)_[A-Z_]+)_11_([A-Z_]*?)SET_[AB]_RARE", row[CODENAME])
		if set_family:
			expected = {f"{set_family[1]}_12_{set_family[2]}{grade}_RARE" for grade in ("A", "B", "C")}
	for column in range(PARAM_FIRST, PARAM_LAST, PARAM_STRIDE):
		if int(row[column]) == -1 and row[column + 1] == UNUSED:
			continue
		target = later.get(row[column + 1])
		if int(row[column]) != 1 or row[column + 1] not in expected:
			raise ValueError(f"equipment has unresolved parameter references: {row[CODENAME]}")
		if target is not None and (equipment_degree(target) != max(DEGREES) + 1 or
			target[TYPE_FIRST:TYPE_FOURTH + 1] != row[TYPE_FIRST:TYPE_FOURTH + 1] or target[RACE] != row[RACE]):
			raise ValueError("out-of-scope item link changes equipment family")
		trimmed.append({"code": row[CODENAME], "column": column, "target": row[column + 1], "targetPresent": target is not None})
		converted[column:column + PARAM_STRIDE] = ["-1", UNUSED]
	converted[IDENTITY], converted[PLACEHOLDER_NAME] = str(identity), UNUSED
	return converted, trimmed


# ================
# plain_worn_models
#
# Degree-11 set armour authors MODEL as a comma pair: the plain resource
# and its full-set variant. The v1.150 client reads MODEL as one path
# and has no full-set display, so the import keeps the first (plain)
# path (owner decision 2026-10-28, #1745). Returns how many rows it
# changed.
# ================
def plain_worn_models(equipment):
	changed = 0
	for row in equipment:
		if "," in row[MODEL]:
			row[MODEL] = row[MODEL].split(",")[0].strip()
			changed += 1
	return changed


# ================
# merge_equipment
# ================
def merge_equipment(native_rows, later_rows, reserved_ids=(), previous_ids=None):
	native, later = index_items(native_rows), index_items(later_rows)
	occupied = {int(row[IDENTITY]) for row in native_rows + later_rows} | set(reserved_ids)
	previous_ids = previous_ids or {}
	retained = {int(entry["target"]) for entry in previous_ids.values()}
	if len(retained) != len(previous_ids) or retained & occupied or any(not 0 < identity <= MAX_ID for identity in retained):
		raise ValueError("previous imported IDs collide or exceed the wire width")
	for code, entry in previous_ids.items():
		row = later.get(code)
		if row is None or int(entry["source"]) != int(row[IDENTITY]) or code in native:
			raise ValueError(f"previous imported identity changed: {code}")
	occupied |= retained
	next_id = max(occupied, default=0) + 1
	added, existing, trimmed = [], [], []
	# Weapons precede the new families, preserving v1 import IDs even in a
	# fresh overlay. A prior marker additionally preserves every published ID.
	for code, row in sorted(later.items(), key=lambda entry: (entry[1][TYPE_THIRD] != "6", entry[0])):
		if equipment_degree(row) not in DEGREES:
			continue
		if code in native:
			existing.append(code)
			continue
		identity = int(previous_ids[code]["target"]) if code in previous_ids else next_id
		converted, removed = convert_equipment(row, identity, later)
		added.append(converted)
		trimmed.extend(removed)
		if code not in previous_ids:
			next_id += 1
	known_codes = native.keys() | {row[CODENAME] for row in added}
	for row in added:
		if row[LINKED_CODE] not in ("", UNUSED) and row[LINKED_CODE] not in known_codes:
			raise ValueError(f"unresolved linked equipment: {row[CODENAME]} -> {row[LINKED_CODE]}")
	return added, {
		"added": len(added), "existingNative": existing,
		"sets": [{"code": row[CODENAME], "setId": int(row[SET])} for row in added if int(row[SET]) != 0],
		"families": dict(sorted(Counter(
			f"race={row[RACE]},degree={equipment_degree(row)},type={row[TYPE_THIRD]},kind={row[TYPE_FOURTH]}" for row in added
		).items())),
		"categories": dict(sorted(Counter(equipment_category(row) for row in added).items())),
		"idMap": {row[CODENAME]: {"source": int(later[row[CODENAME]][IDENTITY]), "target": int(row[IDENTITY])} for row in added},
		"preservedNativeRows": len(native_rows),
		"trimmedOutOfScopeItemLinks": trimmed,
	}


# ================
# equipment_category
# ================
def equipment_category(row):
	if row[TYPE_THIRD] in ARMOR_TYPES:
		return "armor"
	if row[TYPE_THIRD] in ACCESSORY_TYPES:
		return "accessories"
	return "shields" if row[TYPE_THIRD] == "4" else "weapons"


# ================
# set_catalogue
#
# The dormant set-group catalogue: every refsetitemgroup row, plus only the
# degree-11 set rows of refmagicoptgroup. The chain is validated whole, so
# a missing link fails the build instead of shipping dead groups: every
# imported set row names a group, every piece-count link in a group names
# an imported magic group, and every option in those rows is defined by
# the later magicoption table. Link cells pair a requirement figure with
# a magic group id (armour A: 21/2, 0/3, 0/4); a zero group cell is an
# empty slot, and weapon and shield groups carry no links at all. The
# figure's meaning belongs to the mechanics port. Nothing here applies a
# bonus; the rows wait for it.
# ================
def set_catalogue(set_group_rows, magic_group_rows, option_rows, equipment):
	groups = {}
	for row in set_group_rows:
		if len(row) <= SET_LINK_LAST:
			raise ValueError("short set group row")
		group = int(row[SET_GROUP_ID])
		if group in groups:
			raise ValueError(f"duplicate set group: {group}")
		groups[group] = row
	magic = {}
	for row in magic_group_rows:
		if len(row) <= MAGIC_GROUP_OPTION:
			raise ValueError("short magic option group row")
		if not row[MAGIC_GROUP_CODE].startswith(SET_MAGIC_PREFIX):
			continue
		group = int(row[MAGIC_GROUP_ID])
		if (group, int(row[MAGIC_GROUP_OPTION])) in magic:
			raise ValueError(f"duplicate set magic option: {row[MAGIC_GROUP_CODE]}")
		magic[group, int(row[MAGIC_GROUP_OPTION])] = row
	options = {int(row[MAGIC_OPTION_ID]) for row in option_rows if len(row) > MAGIC_OPTION_ID}
	for row in equipment:
		if int(row[SET]) != 0 and int(row[SET]) not in groups:
			raise ValueError(f"set item names an unknown set group: {row[CODENAME]} -> {row[SET]}")
	magic_groups = {group for group, _option in magic}
	for group, row in groups.items():
		for column in range(SET_LINK_FIRST + 2, SET_LINK_LAST + 1, 2):
			if int(row[column]) != 0 and int(row[column]) not in magic_groups:
				raise ValueError(f"set group names an unknown magic group: {row[SET_GROUP_CODE]} -> {row[column]}")
	for (group, option), row in magic.items():
		if option not in options:
			raise ValueError(f"set magic group names an unknown magic option: {row[MAGIC_GROUP_CODE]} -> {option}")
	return list(set_group_rows), [row for _key, row in sorted(magic.items())], {
		"groups": len(groups), "magicGroups": len(magic_groups),
		"optionIds": sorted({option for _group, option in magic}),
		"setItems": sum(1 for row in equipment if int(row[SET]) != 0),
	}


# ================
# merge_texts
#
# Later equip/skill translations use the same language order as the city's
# object table. Native symbols win; every imported item must have a name.
# ================
def merge_texts(native_raw, later_rows, equipment):
	known = {row[1] for row in tables.table_rows(native_raw) if len(row) > 1}
	needed = {row[column] for row in equipment for column in (NAME_KEY, DESCRIPTION_KEY) if row[column] != UNUSED}
	translations = {}
	for row in later_rows:
		if len(row) > 1 and row[1] in needed:
			if len(row) < 10:
				raise ValueError(f"short translation row: {row[1]}")
			converted = native_text_row(row)
			if row[1] in translations and translations[row[1]] != converted:
				raise ValueError(f"conflicting translation: {row[1]}")
			translations[row[1]] = converted
	missing = needed - known - translations.keys()
	missing_names = missing & {row[NAME_KEY] for row in equipment}
	if missing_names:
		raise ValueError(f"missing equipment names: {sorted(missing_names)}")
	# Ordinary equipment description symbols are often absent in both clients.
	# Retain that authored absence; never synthesize tooltip prose.
	return text_tables.append_rows(native_raw, [translations[key] for key in sorted(needed - known - missing)]), sorted(missing)


# ================
# direct_assets
# ================
def direct_assets(rows):
	assets = set()
	for row in rows:
		# Worn models need the shared type-driven dual/female resolver.
		for column in (DROP_MODEL, ICON):
			value = row[column]
			if value in ("", UNUSED):
				raise ValueError(f"equipment has no required resource at {column}: {row[CODENAME]}")
			prefix = "icon/" if column == ICON else "res/"
			assets.add(("Media" if column == ICON else "Data", prefix + value.replace("\\", "/")))
	return assets


# ================
# shop_dependencies
#
# Report authored package/tab/price dependencies without adding merchandise.
# Shop import waits for its complete item and condition closure.
# ================
def shop_dependencies(source, equipment, native_codes, source_ids):
	codes = {row[CODENAME] for row in equipment}
	scraps = [row for row in source["refscrapofpackageitem.txt"] if len(row) >= 20 and row[SERVICE] == "1"]
	packages = {row[PACKAGE_CODE] for row in scraps if row[PACKAGE_ITEM] in codes}
	return {
		"legacyStock": [{"tabId": int(row[LEGACY_SHOP_TAB]), "sourceItemId": int(row[LEGACY_SHOP_ITEM]),
			"code": source_ids[int(row[LEGACY_SHOP_ITEM])]}
			for row in source.get("shopitemdata.txt", []) if len(row) > LEGACY_SHOP_ITEM and row[SERVICE] == "1"
			and int(row[LEGACY_SHOP_ITEM]) in source_ids],
		"packages": sorted(packages),
		"tabs": sorted({row[SHOP_TAB] for row in source["refshopgoods.txt"]
			if len(row) > SHOP_PACKAGE and row[SERVICE] == "1" and row[SHOP_PACKAGE] in packages}),
		"prices": [{"package": row[PACKAGE_CODE], "currency": int(row[PRICE_CURRENCY]), "amount": int(row[PRICE_AMOUNT])}
			for row in source["refpricepolicyofitem.txt"]
			if len(row) > PRICE_AMOUNT and row[SERVICE] == "1" and row[PACKAGE_CODE] in packages],
		"otherMissingItems": sorted({row[PACKAGE_ITEM] for row in scraps
			if row[PACKAGE_CODE] in packages and row[PACKAGE_ITEM] not in codes and row[PACKAGE_ITEM] not in native_codes})
	}
