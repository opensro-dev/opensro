"""
===========================================================================

alexandria_shop_d10.py - ordinary degree 10 stock for Alexandria's own shops

Port-only, not native. Called only by the Alexandria variant builder, and
only does anything when the degree 10 items exist (SRO_CAP110_ITEMS).

Alexandria's smiths and armor traders map to STORE_KT_SMITH and
STORE_KT_ARMOR, the stores Khotan's traders share. Those stay exactly as
they are. The city gets stores of its own, copied from them, and its
accessory store (already its own) is restocked in place. In each tab, a
degree 9 ordinary (grade A) good gains the degree 10 good of the same
family, placed right after it:

	PACKAGE_ITEM_CH_BLADE_09_A  ->  PACKAGE_ITEM_CH_BLADE_10_A

A tab never grows past the largest tab the native tables ship (45 slots);
over that, the lowest degrees drop out. Degree 11, grades B and C and rare
or seal items are never added. A package's price is its item's price column
(the 9D package price is the same column of the 9D item: 4042000 for
ITEM_CH_BLADE_09_A). INFERENCE: v1.150 has no degree 10 shop, so the item
table is the only price source.

===========================================================================
"""
import re
from collections import defaultdict

import alexandria_npcs as npcs
import import_vsro_alexandria as tables

SOURCE_DEGREE = "09"
NEW_DEGREE = "10"
# The largest tab in the shipped tables (slots 0..44).
MAX_TAB_SLOTS = 45
DEGREE_TOKEN = re.compile(r"_(\d\d)_")
ICON_SUFFIX = ".ddj"
PRICE_COLUMN = 26
NAME_COLUMN = 5
DESCRIPTION_COLUMN = 6
ITEM_ID_COLUMN = 1

# new store -> (the store it is copied from, the shop groups that move to it)
CLONED_STORES = {
	"STORE_SD_SMITH": ("STORE_KT_SMITH", ("GROUP_STORE_SD_M_SMITH", "GROUP_STORE_SD_T_SMITH")),
	"STORE_SD_ARMOR": ("STORE_KT_ARMOR", ("GROUP_STORE_SD_M_ARMOR", "GROUP_STORE_SD_T_ARMOR"))
}
# Stores that are already Alexandria's own and are restocked in place.
OWN_STORES = ("STORE_SD_ACCESSORY",)
STORE_PREFIX_FROM, STORE_PREFIX_TO = "STORE_KT_", "STORE_SD_"

NAMES = (
	"refshop.txt", "refmappingshopgroup.txt", "refmappingshopwithtab.txt", "refshoptabgroup.txt", "refshoptab.txt",
	"refshopgoods.txt", "refpackageitem.txt", "refscrapofpackageitem.txt", "refpricepolicyofitem.txt"
)
IDENTITY = {"refshop.txt": 2, "refshoptabgroup.txt": 2, "refshoptab.txt": 2, "refpackageitem.txt": 2}


# ================
# family_of
#
# The package code with its degree removed, and the degree; None for a code
# without a "_NN_" token (ammunition, scrolls, alchemy).
# ================
def family_of(package):
	match = DEGREE_TOKEN.search(package)
	if match is None:
		return None
	return package[:match.start()] + "_#_" + package[match.end():], match.group(1)


# ================
# with_degree
# ================
def with_degree(code, degree):
	return DEGREE_TOKEN.sub(f"_{degree}_", code, count=1)


# ================
# next_stock
#
# The ordered package codes of a tab after the degree 10 goods join it, and
# the new ones among them. Only a family whose newest degree is 9 gains a
# good, only when the degree 10 item exists, and never a second copy of a
# good the tab already has: restocking a restocked tab (the in-place
# accessory store, when the services step runs twice on one output tree)
# adds nothing.
# ================
def next_stock(packages, items):
	family_last = {}
	for at, package in enumerate(packages):
		parsed = family_of(package)
		if parsed is not None and parsed[1] == SOURCE_DEGREE:
			family_last[parsed[0]] = at
	have = set(packages)
	stock, added = [], []
	for at, package in enumerate(packages):
		stock.append(package)
		parsed = family_of(package)
		if parsed is None or family_last.get(parsed[0]) != at:
			continue
		new_package = with_degree(package, NEW_DEGREE)
		if new_package not in have and new_package.replace("PACKAGE_ITEM_", "ITEM_", 1) in items:
			stock.append(new_package)
			added.append(new_package)
	# Over the largest native tab, the lowest degrees go first.
	while len(stock) > MAX_TAB_SLOTS:
		lowest = min((p for p in stock if family_of(p)), key=lambda p: family_of(p)[1])
		stock.remove(lowest)
	return stock, added


# ================
# merge_d10_stock
#
# tables maps the NAMES to raw bytes; items maps an item code to its
# itemdata row. Returns the changed tables, and a summary.
# ================
def merge_d10_stock(raw, items):
	rows = {name: tables.table_rows(raw[name]) for name in NAMES}
	next_id = {name: max((int(r[col]) for r in rows[name] if len(r) > col and r[col].isdigit()), default=0) + 1 for name, col in IDENTITY.items()}

	# ================
	# new_id
	# ================
	def new_id(name):
		value = next_id[name]
		next_id[name] += 1
		return str(value)

	goods_by_tab = defaultdict(list)
	for row in rows["refshopgoods.txt"]:
		if len(row) >= 5 and row[0] == "1":
			goods_by_tab[row[2]].append(row)
	packages = {r[3]: r for r in rows["refpackageitem.txt"] if len(r) > 3}
	scraps = {r[2]: r for r in rows["refscrapofpackageitem.txt"] if len(r) > 3}
	prices = {r[2]: r for r in rows["refpricepolicyofitem.txt"] if len(r) > 2}
	tabs_of_group = defaultdict(list)
	for row in rows["refshoptab.txt"]:
		if len(row) >= 6:
			tabs_of_group[row[4]].append(row)
	groups_of_store = defaultdict(list)
	for row in rows["refmappingshopwithtab.txt"]:
		if len(row) >= 4:
			groups_of_store[row[2]].append(row[3])
	tab_groups = {r[3]: r for r in rows["refshoptabgroup.txt"] if len(r) > 3}
	scrap_ids = [int(r[-1]) for r in rows["refscrapofpackageitem.txt"] if r[-1].isdigit()]
	next_scrap = max(scrap_ids, default=0) + 1

	new_packages, new_scraps, new_prices = [], [], []
	missing = set()

	# ================
	# make_package
	# ================
	def make_package(code):
		"""Create the degree 10 package for code (a 9D package) once; returns False if it cannot be made."""
		nonlocal next_scrap
		if code in packages:
			return True
		source = with_degree(code, SOURCE_DEGREE)
		item_code = code.replace("PACKAGE_ITEM_", "ITEM_", 1)
		item = items.get(item_code)
		if source not in packages or source not in scraps or source not in prices or item is None:
			missing.add(code)
			return False
		icon = next((v for v in item if v.lower().endswith(ICON_SUFFIX)), packages[source][8])
		package = list(packages[source])
		package[2], package[3] = new_id("refpackageitem.txt"), code
		package[6], package[7], package[8] = item[NAME_COLUMN], item[DESCRIPTION_COLUMN], icon
		scrap = list(scraps[source])
		scrap[2], scrap[3], scrap[-1] = code, item_code, str(next_scrap)
		next_scrap += 1
		price = list(prices[source])
		price[2], price[4] = code, item[PRICE_COLUMN]
		packages[code] = package
		new_packages.append(package)
		new_scraps.append(scrap)
		new_prices.append(price)
		return True

	# ================
	# restock
	# ================
	def restock(tab_code):
		"""The tab's goods after the degree 10 ones are added: rows in slot order."""
		old = sorted(goods_by_tab[tab_code], key=lambda r: int(r[4]))
		stock, added = next_stock([r[3] for r in old], items)
		template = old[0]
		made = {code for code in added if make_package(code)}
		rows_out = []
		for package in (p for p in stock if p not in added or p in made):
			source = next((r for r in old if r[3] == package), template)
			rows_out.append([*source[:3], package, str(len(rows_out)), *source[5:]])
		return rows_out, len(made)

	summary = {"stores": [], "goods": 0, "packages": 0}
	appended = {name: [] for name in NAMES}
	replaced_tabs = {}
	moved_groups = {}

	for store, (source, groups) in CLONED_STORES.items():
		stores = {r[3] for r in rows["refshop.txt"] if len(r) > 3}
		# The NPC merge rebuilds mappings before this pass. Reconnect an owned
		# store even when its goods were already staged by a previous build.
		if store in stores:
			moved_groups.update({group: store for group in groups})
			continue
		if source not in stores:
			continue
		shop = next(r for r in rows["refshop.txt"] if len(r) > 3 and r[3] == source)
		appended["refshop.txt"].append([*shop[:2], new_id("refshop.txt"), store, *shop[4:]])
		for group in groups_of_store[source]:
			new_group = group.replace(STORE_PREFIX_FROM, STORE_PREFIX_TO, 1)
			group_row = list(tab_groups[group])
			group_row[2], group_row[3] = new_id("refshoptabgroup.txt"), new_group
			appended["refshoptabgroup.txt"].append(group_row)
			appended["refmappingshopwithtab.txt"].append([*rows["refmappingshopwithtab.txt"][0][:2], store, new_group])
			for tab_row in tabs_of_group[group]:
				new_tab = tab_row[3].replace(STORE_PREFIX_FROM, STORE_PREFIX_TO, 1)
				cloned = list(tab_row)
				cloned[2], cloned[3], cloned[4] = new_id("refshoptab.txt"), new_tab, new_group
				appended["refshoptab.txt"].append(cloned)
				goods_by_tab[new_tab] = [[*g[:2], new_tab, *g[3:]] for g in goods_by_tab[tab_row[3]]]
				stocked, count = restock(new_tab)
				appended["refshopgoods.txt"] += stocked
				summary["goods"] += count
		moved_groups.update({group: store for group in groups})
		summary["stores"].append(store)

	for store in OWN_STORES:
		for group in groups_of_store[store]:
			for tab_row in tabs_of_group[group]:
				stocked, count = restock(tab_row[3])
				if count:
					replaced_tabs[tab_row[3]] = stocked
					summary["goods"] += count

	mapping = []
	for row in rows["refmappingshopgroup.txt"]:
		if len(row) >= 4 and row[2] in moved_groups:
			row = [*row[:3], moved_groups[row[2]]]
		mapping.append(row)

	goods = [r for r in rows["refshopgoods.txt"] if len(r) < 3 or r[2] not in replaced_tabs]
	for stocked in replaced_tabs.values():
		goods += stocked
	merged = {
		"refshop.txt": npcs.append_rows(raw["refshop.txt"], appended["refshop.txt"]),
		"refmappingshopgroup.txt": b"\xff\xfe" + npcs.table_text(mapping).encode("utf-16-le"),
		"refmappingshopwithtab.txt": npcs.append_rows(raw["refmappingshopwithtab.txt"], appended["refmappingshopwithtab.txt"]),
		"refshoptabgroup.txt": npcs.append_rows(raw["refshoptabgroup.txt"], appended["refshoptabgroup.txt"]),
		"refshoptab.txt": npcs.append_rows(raw["refshoptab.txt"], appended["refshoptab.txt"]),
		"refshopgoods.txt": b"\xff\xfe" + npcs.table_text(goods + appended["refshopgoods.txt"]).encode("utf-16-le"),
		"refpackageitem.txt": npcs.append_rows(raw["refpackageitem.txt"], new_packages),
		"refscrapofpackageitem.txt": npcs.append_rows(raw["refscrapofpackageitem.txt"], new_scraps),
		"refpricepolicyofitem.txt": npcs.append_rows(raw["refpricepolicyofitem.txt"], new_prices)
	}
	summary["packages"] = len(new_packages)
	summary["missing"] = sorted(missing)
	return merged, summary
