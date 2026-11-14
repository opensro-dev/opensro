"""
===========================================================================

alexandria_shop_honor.py - Alexandria's own honor store (degree 9 goods)

Port-only, not native. Called only by the Alexandria variant builder,
after the gear passes, and only behind the SRO_ALEXANDRIA_HONOR_STORE
build flag (build_alexandria_variant.honor_store_enabled). Issue #3548.

What the data started and never finished. The later tables give both of
Alexandria's warehouse keepers an honor shop group (GROUP_STORE_SD_HONOR,
GROUP_STORE_SD_HONOR2), but map both onto Hotan's STORE_KT_HONOR, so the
city sells the degree 8 honor weapons (level 62-70) that every town
sells. No STORE_SD_HONOR exists in any source. The native v1.150 tables
meanwhile author a whole degree 9 honor line that no shop stocks:

	ITEM_CH_*_09_{A,B,C}_RARE_HONOR    15 items, packages and prices
	                                   (5.8M-9.5M gold + 4000-8000 honor)
	ITEM_EU_*_09_{A,B,C}_RARE_HONOR    27 items, no packages
	PACKAGE_ITEM_ETC_ARCHEMY_MAGICSTONE_REPAIR_09
	                                   50,000 gold + 15 honor, unsold

Degree 9 (level 74-82) is Alexandria's band, the city is the one town
both races share (its smith and accessory stores carry both lines), and
the honor store is the only shop that sells _RARE_HONOR goods: the
inference is that STORE_SD_HONOR was to sell this line. This pass builds
it:

- the store is copied from STORE_KT_HONOR (tabs, tab names, layout)
  through the degree 10 pass's store copy, and both SD honor groups move
  onto it;
- each degree 8 honor good becomes its degree 9 twin, and the degree 8
  repair stone its degree 9 stone, when the native package exists;
- every degree 9 honor item that has no package (the European line)
  gains one, after the Chinese goods on the weapon tab, minted from the
  Chinese degree 9 package of its grade: the same honor price row, its
  own gold price (column 26, which matches the Chinese packages), its
  own name, icon and durability, and the grade's magic parameter.

A tab never grows past the largest native tab; a good that would is
reported, never stocked. Missing packages are reported, never partly
granted. Running the pass twice changes nothing the second time.

===========================================================================
"""
import re
from collections import defaultdict

import alexandria_npcs as npcs
import alexandria_shop_d10 as d10
import import_vsro_alexandria as tables

HONOR_STORE = "STORE_SD_HONOR"
SOURCE_STORE = "STORE_KT_HONOR"
HONOR_GROUPS = ("GROUP_STORE_SD_HONOR", "GROUP_STORE_SD_HONOR2")
SOURCE_DEGREE = "08"
HONOR_DEGREE = "09"
HONOR_SUFFIX = "_RARE_HONOR"
# The repair stone's code ends in its degree, which d10.DEGREE_TOKEN
# (a "_NN_" token) does not match.
REPAIR_STONE = "PACKAGE_ITEM_ETC_ARCHEMY_MAGICSTONE_REPAIR_"
# A degree 9 honor item: its race, weapon kind and grade.
HONOR_ITEM = re.compile(r"^ITEM_([A-Z]+)_([A-Z]+)_" + HONOR_DEGREE + r"_([ABC])" + HONOR_SUFFIX + "$")
PACKAGE_PREFIX, ITEM_PREFIX = "PACKAGE_ITEM_", "ITEM_"
# refscrapofpackageitem Data: an equipment package's durability, the
# item's lower durability bound (every native honor package agrees).
SCRAP_DATA_COLUMN = 6
DURABILITY_COLUMN = 63
PRICE_PAYMENT_COLUMN, PRICE_AMOUNT_COLUMN = 3, 4
PAYMENT_GOLD = "1"
SLOT_COLUMN = 4


# ================
# honor_twin
#
# The degree 9 package a stocked honor good becomes: an 08 honor weapon
# its 09 twin, the 08 repair stone the 09 stone. Any other code (or a
# good already at degree 9) is itself.
# ================
def honor_twin(package):
	if package == REPAIR_STONE + SOURCE_DEGREE:
		return REPAIR_STONE + HONOR_DEGREE
	parsed = d10.family_of(package)
	if package.endswith(HONOR_SUFFIX) and parsed is not None and parsed[1] == SOURCE_DEGREE:
		return d10.with_degree(package, HONOR_DEGREE)
	return package


# ================
# grade_of
#
# A degree 9 honor package's grade letter, or None.
# ================
def grade_of(package):
	match = HONOR_ITEM.match(package.replace(PACKAGE_PREFIX, ITEM_PREFIX, 1))
	return match.group(3) if match else None


# ================
# next_stock
#
# The ordered package codes of one honor tab, and the codes that need a
# package minted. known is the set of package codes the tables hold.
# Each good becomes its degree 9 twin when that package exists (a twin
# without one keeps the good, reported in missing); a weapon tab (one
# that stocks honor weapons) then gains every degree 9 honor item that
# no package carries, in item code order, until the tab is full.
# ================
def next_stock(packages, items, known):
	stock, missing, mint, full = [], [], [], []
	for package in packages:
		twin = honor_twin(package)
		if twin != package and twin not in known:
			missing.append(twin)
			twin = package
		if twin not in stock:
			stock.append(twin)
	if not any(grade_of(p) for p in stock):
		return stock, mint, missing, full
	for code in sorted(items):
		if not HONOR_ITEM.match(code):
			continue
		package = code.replace(ITEM_PREFIX, PACKAGE_PREFIX, 1)
		if package in stock or package in known:
			continue
		if len(stock) >= d10.MAX_TAB_SLOTS:
			full.append(package)
			continue
		stock.append(package)
		mint.append(package)
	return stock, mint, missing, full


# ================
# merge_honor_store
#
# raw maps d10.NAMES to raw bytes; items maps an item code to its
# itemdata row. Returns the changed tables, and a summary.
# ================
def merge_honor_store(raw, items):
	copied, copy_summary = d10.merge_d10_stock(raw, items, clones={HONOR_STORE: (SOURCE_STORE, HONOR_GROUPS)}, own_stores=())
	raw = {**raw, **copied}
	rows = {name: tables.table_rows(raw[name]) for name in d10.NAMES}
	summary = {"stores": copy_summary["stores"], "goods": 0, "packages": 0, "missing": [], "full": []}

	goods_by_tab = defaultdict(list)
	for row in rows["refshopgoods.txt"]:
		if len(row) > SLOT_COLUMN and row[0] == "1":
			goods_by_tab[row[2]].append(row)
	packages = {r[3]: r for r in rows["refpackageitem.txt"] if len(r) > 3}
	scraps = {r[2]: r for r in rows["refscrapofpackageitem.txt"] if len(r) > 3}
	prices = defaultdict(list)
	for row in rows["refpricepolicyofitem.txt"]:
		if len(row) > PRICE_AMOUNT_COLUMN:
			prices[row[2]].append(row)
	tab_groups = {r[3] for r in rows["refmappingshopwithtab.txt"] if len(r) >= 4 and r[2] == HONOR_STORE}
	honor_tabs = [r[3] for r in rows["refshoptab.txt"] if len(r) >= 6 and r[4] in tab_groups]
	next_package = max((int(r[2]) for r in rows["refpackageitem.txt"] if len(r) > 3 and r[2].isdigit()), default=0) + 1
	next_scrap = max((int(r[-1]) for r in rows["refscrapofpackageitem.txt"] if r and r[-1].isdigit()), default=0) + 1
	new_packages, new_scraps, new_prices = [], [], []

	# ================
	# mint
	#
	# The package for a degree 9 honor item, from the stocked package of
	# its grade (templates): package, scrap and every price row copied,
	# then the item's own name, icon, durability and gold price.
	# Returns False when the item or a template is missing.
	# ================
	def mint(code, templates):
		nonlocal next_package, next_scrap
		item = items.get(code.replace(PACKAGE_PREFIX, ITEM_PREFIX, 1))
		source = templates.get(grade_of(code))
		if item is None or source is None or source not in scraps or not prices[source]:
			return False
		icon = next((v for v in item if v.lower().endswith(d10.ICON_SUFFIX)), packages[source][8])
		package = list(packages[source])
		package[2], package[3] = str(next_package), code
		package[6], package[7], package[8] = item[d10.NAME_COLUMN], item[d10.DESCRIPTION_COLUMN], icon
		next_package += 1
		scrap = list(scraps[source])
		scrap[2], scrap[3], scrap[-1] = code, item[2], str(next_scrap)
		scrap[SCRAP_DATA_COLUMN] = str(int(float(item[DURABILITY_COLUMN])))
		next_scrap += 1
		for price in prices[source]:
			price = list(price)
			price[2] = code
			if price[PRICE_PAYMENT_COLUMN] == PAYMENT_GOLD:
				price[PRICE_AMOUNT_COLUMN] = item[d10.PRICE_COLUMN]
			new_prices.append(price)
		packages[code] = package
		new_packages.append(package)
		new_scraps.append(scrap)
		return True

	replaced = {}
	for tab in honor_tabs:
		old = sorted(goods_by_tab[tab], key=lambda r: int(r[SLOT_COLUMN]))
		if not old:
			continue
		stock, to_mint, missing, full = next_stock([r[3] for r in old], items, set(packages))
		templates = {}
		for package in stock:
			grade = grade_of(package)
			if grade and package in packages:
				templates.setdefault(grade, package)
		minted = {code for code in to_mint if mint(code, templates)}
		summary["missing"] += missing + sorted(set(to_mint) - minted)
		summary["full"] += full
		stock = [p for p in stock if p not in to_mint or p in minted]
		template = old[0]
		stocked = [[*template[:3], package, str(slot), *template[SLOT_COLUMN + 1:]] for slot, package in enumerate(stock)]
		if [r[3] for r in stocked] != [r[3] for r in old]:
			replaced[tab] = stocked
			summary["goods"] += len(stock)

	goods = [r for r in rows["refshopgoods.txt"] if len(r) < 3 or r[2] not in replaced]
	for stocked in replaced.values():
		goods += stocked
	merged = {
		**copied,
		"refshopgoods.txt": b"\xff\xfe" + npcs.table_text(goods).encode("utf-16-le"),
		"refpackageitem.txt": npcs.append_rows(raw["refpackageitem.txt"], new_packages),
		"refscrapofpackageitem.txt": npcs.append_rows(raw["refscrapofpackageitem.txt"], new_scraps),
		"refpricepolicyofitem.txt": npcs.append_rows(raw["refpricepolicyofitem.txt"], new_prices)
	}
	summary["packages"] = len(new_packages)
	return merged, summary
