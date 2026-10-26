"""
===========================================================================

alexandria_shop_d11.py - ordinary degree 11 gear for Alexandria's shops

Port-only, not native. Called only by the Alexandria variant builder,
after the degree 10 pass (alexandria_shop_d10) and the supplies pass
(alexandria_shop_supplies). Issue #1748, second PR; the owner approved
this scope on 2026-10-26.

The degree 10 pass stocks Alexandria's own smith, armour and accessory
stores with grade A degree 10 goods. This pass gives each of those goods
its degree 11 twin, placed right after it, on the same stores:

	PACKAGE_ITEM_CH_BLADE_10_A  ->  PACKAGE_ITEM_CH_BLADE_11_A

Scope, as approved: Alexandria's stores only; plain grade A only - the
rare tiers and set gear stay drop-only, and a package that is not a plain
grade A good (a rare or set package, should one ever be stocked) never
gains a twin. A tab never grows past the largest tab the native tables
ship (45 slots); over that, the lowest degrees drop out, exactly as the
degree 10 pass trims.

Every good is added only when its item exists in the variant item table.
The native tables carry no degree 10 or 11 gear at all, so those items
arrive only through the cap-110 item import: without SRO_CAP110_ITEMS
this pass stocks nothing. A package is minted from the same kind's
degree 10 (or degree 9) package with the item's own price column, as the
degree 10 pass mints its packages. Missing item packages are reported,
never partly granted. Restocking is re-entry safe: a good already in the
tab is never stocked twice.

===========================================================================
"""
from collections import defaultdict

import alexandria_npcs as npcs
import alexandria_shop_d10 as d10
import import_vsro_alexandria as tables

SOURCE_DEGREE = "10"
NEW_DEGREE = "11"
STORES = ("STORE_SD_SMITH", "STORE_SD_ARMOR", "STORE_SD_ACCESSORY")


# ================
# plain_grade_a
#
# A plain grade A package: not a rare tier, not a set piece. Only these
# gain a degree 11 twin; rare and set gear stays drop-only.
# ================
def plain_grade_a(package):
	return package.endswith("_A") and "_SET_" not in package and "_RARE" not in package


# ================
# next_stock
#
# The ordered package codes of a tab after the degree 11 goods join it,
# and the new ones among them. Only a family whose newest stocked degree
# is 10 gains a good, only when the degree 11 item exists, and never a
# second copy of a good the tab already has.
# ================
def next_stock(packages, items):
	family_last = {}
	for at, package in enumerate(packages):
		parsed = d10.family_of(package)
		if parsed is not None and parsed[1] == SOURCE_DEGREE and plain_grade_a(package):
			family_last[parsed[0]] = at
	have = set(packages)
	stock, added = [], []
	for at, package in enumerate(packages):
		stock.append(package)
		parsed = d10.family_of(package)
		if parsed is None or family_last.get(parsed[0]) != at:
			continue
		new_package = d10.with_degree(package, NEW_DEGREE)
		if new_package not in have and new_package.replace("PACKAGE_ITEM_", "ITEM_", 1) in items:
			stock.append(new_package)
			added.append(new_package)
	# Over the largest native tab, the lowest degrees go first.
	while len(stock) > d10.MAX_TAB_SLOTS:
		lowest = min((p for p in stock if d10.family_of(p)), key=lambda p: d10.family_of(p)[1])
		stock.remove(lowest)
	return stock, added


# ================
# merge_d11_stock
#
# raw maps the shop table names to raw bytes; items maps an item code to
# its itemdata row. Returns the changed tables, and a summary.
# ================
def merge_d11_stock(raw, items):
	rows = {name: tables.table_rows(raw[name]) for name in d10.NAMES}
	next_id = {name: max((int(r[col]) for r in rows[name] if len(r) > col and r[col].isdigit()), default=0) + 1 for name, col in d10.IDENTITY.items()}

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
	scrap_ids = [int(r[-1]) for r in rows["refscrapofpackageitem.txt"] if r[-1].isdigit()]
	next_scrap = max(scrap_ids, default=0) + 1

	new_packages, new_scraps, new_prices = [], [], []
	missing = set()

	# ================
	# make_package
	#
	# Mint the degree 11 package for code (a 10A package) once, copied
	# from the same kind's degree 10 or degree 9 package, priced from
	# the item's own price column. Returns False - and reports the
	# package - when the item or a template is missing, so a good is
	# never partly granted.
	# ================
	def make_package(code):
		nonlocal next_scrap
		if code in packages:
			return True
		item_code = code.replace("PACKAGE_ITEM_", "ITEM_", 1)
		item = items.get(item_code)
		source = next((d10.with_degree(code, degree) for degree in (SOURCE_DEGREE, "09")
		               if d10.with_degree(code, degree) in packages), None)
		if source is None or source not in scraps or source not in prices or item is None:
			missing.add(code)
			return False
		icon = next((v for v in item if v.lower().endswith(d10.ICON_SUFFIX)), packages[source][8])
		package = list(packages[source])
		package[2], package[3] = new_id("refpackageitem.txt"), code
		package[6], package[7], package[8] = item[d10.NAME_COLUMN], item[d10.DESCRIPTION_COLUMN], icon
		scrap = list(scraps[source])
		scrap[2], scrap[3], scrap[-1] = code, item_code, str(next_scrap)
		next_scrap += 1
		price = list(prices[source])
		price[2], price[4] = code, item[d10.PRICE_COLUMN]
		# All three rows register, so the dicts keep describing the tables
		# as they will be, minted packages included.
		packages[code] = package
		scraps[code] = scrap
		prices[code] = price
		new_packages.append(package)
		new_scraps.append(scrap)
		new_prices.append(price)
		return True

	# ================
	# restock
	# ================
	def restock(tab_code):
		"""The tab's goods after the degree 11 ones are added: rows in slot order."""
		old = sorted(goods_by_tab[tab_code], key=lambda r: int(r[4]))
		if not old:
			return [], 0
		stock, added = next_stock([r[3] for r in old], items)
		template = old[0]
		made = {code for code in added if make_package(code)}
		rows_out = []
		for package in (p for p in stock if p not in added or p in made):
			source = next((r for r in old if r[3] == package), template)
			rows_out.append([*source[:3], package, str(len(rows_out)), *source[5:]])
		return rows_out, len(made)

	summary = {"goods": 0, "packages": 0}
	replaced_tabs = {}
	for store in STORES:
		for group in groups_of_store[store]:
			for tab_row in tabs_of_group[group]:
				stocked, count = restock(tab_row[3])
				if count:
					replaced_tabs[tab_row[3]] = stocked
					summary["goods"] += count

	goods = [r for r in rows["refshopgoods.txt"] if len(r) < 3 or r[2] not in replaced_tabs]
	for stocked in replaced_tabs.values():
		goods += stocked
	merged = {
		"refshop.txt": npcs.append_rows(raw["refshop.txt"], []),
		"refmappingshopgroup.txt": npcs.append_rows(raw["refmappingshopgroup.txt"], []),
		"refmappingshopwithtab.txt": npcs.append_rows(raw["refmappingshopwithtab.txt"], []),
		"refshoptabgroup.txt": npcs.append_rows(raw["refshoptabgroup.txt"], []),
		"refshoptab.txt": npcs.append_rows(raw["refshoptab.txt"], []),
		"refshopgoods.txt": b"\xff\xfe" + npcs.table_text(goods).encode("utf-16-le"),
		"refpackageitem.txt": npcs.append_rows(raw["refpackageitem.txt"], new_packages),
		"refscrapofpackageitem.txt": npcs.append_rows(raw["refscrapofpackageitem.txt"], new_scraps),
		"refpricepolicyofitem.txt": npcs.append_rows(raw["refpricepolicyofitem.txt"], new_prices)
	}
	summary["packages"] = len(new_packages)
	summary["missing"] = sorted(missing)
	return merged, summary
