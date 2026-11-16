"""
===========================================================================

honor_shops.py - every town's honor store, as the official tables lay it

Called by the Alexandria variant builder's NPC services step on the final
shop tables (#3548). Every honor store (the warehouse keepers' Training
Camp shops; Alexandria's two map onto Hotan's, as in every source) has a
weapon tab (SN_TAB_HONOR) and a stone tab (SN_TAB_HONOR2), and every
store sells the same goods.

Native (later official data, always on): the v1.150 tables stock the
degree 8 goods only, though they author the degree 9 packages and
prices. iSRO-R's live SHARD database (_RefShopGoods) stocks each weapon
tab with, per kind, degree 8 grades A-C then degree 9 grades A-C (30
slots), and each stone tab with the degree 8 then the degree 9 repair
stone. This pass lays the tabs so.

Port-only, not native (SRO_HONOR_GOODS, with honor_items.py): the rest
of the line the client authored. The weapon tab's first page stays
native; its second page (slot 30 on) carries degrees 10 and 11 per
kind, then the Chinese shields at every degree; a third tab carries the European honor weapons, laid
the same way; the stone tab gains the degree 10 and 11 stones. A good
without a package gets one minted from a shipped honor package: the
item's own name, icon, durability and gold price, its degree's honor
price (honor_items' tables) and its degree's MATTR_REPAIR seal, as the
shipped packages carry (option 234 at degree 8, 235 at degree 9, value
6). With the switch off the expansion's tab, packages and goods leave.

A good is stocked only when its item exists. Running the pass twice
changes nothing the second time.

===========================================================================
"""
from collections import defaultdict
import re

import alexandria_npcs as npcs
import alexandria_shop_d10 as d10
import honor_items as honor
import import_vsro_alexandria as tables

WEAPON_LABEL = "SN_TAB_HONOR"
STONE_LABEL = "SN_TAB_HONOR2"
EXPANSION_TAB = "_TAB3"
TAB_SUFFIX = re.compile(r"_TAB\d+$")
# The shop window shows a tab 30 slots a page (merchant.ts merchantPage).
PAGE_SLOTS = 30
NATIVE_DEGREES = (8, 9)
EXPANSION_DEGREES = (10, 11)
PACKAGE_PREFIX, ITEM_PREFIX = "PACKAGE_ITEM_", "ITEM_"
# refscrapofpackageitem: Data (durability), MagParamNum, MagParam1.
SCRAP_DATA, SCRAP_PARAM_COUNT, SCRAP_PARAM = 6, 7, 8
# The shipped honor packages' seal: MATTR_REPAIR option 234 + (degree - 8)
# with value 6, as (value << 32) | option.
REPAIR_OPTION_BASE = 226
REPAIR_VALUE = 6
MAGIC_VALUE_SHIFT = 32
PRICE_PAYMENT, PRICE_AMOUNT = 3, 4
PAYMENT_GOLD, PAYMENT_HONOR = "1", "32"
SLOT = 4


# ================
# package_of / item_of
# ================
def package_of(code):
	return code.replace(ITEM_PREFIX, PACKAGE_PREFIX, 1)


def item_of(package):
	return package.replace(PACKAGE_PREFIX, ITEM_PREFIX, 1)


# ================
# band
#
# One page band of a weapon tab: per kind, each degree's grades A-C, the
# goods whose item exists.
# ================
def band(race, kinds, degrees, items):
	goods = []
	for kind in kinds:
		for degree in degrees:
			for grade in honor.GRADES:
				code = honor.honor_code(race, kind, degree, grade)
				if code in items:
					goods.append(package_of(code))
	return goods


# ================
# weapon_slots
#
# A weapon tab's (slot, package) layout: the kinds the tab stocks natively
# at the native degrees from slot 0, untouched by the expansion; from the
# next page, the expansion's degrees of those kinds, then every degree of
# the kinds it adds (the Chinese shields). A tab with no native kind (the
# European one) lays its kinds out natively, the later degrees a page on.
# ================
def weapon_slots(race, kinds, native_kinds, items, expansion):
	first_kinds = native_kinds or kinds
	first = band(race, first_kinds, NATIVE_DEGREES, items)
	layout = list(enumerate(first))
	if expansion:
		later = band(race, first_kinds, EXPANSION_DEGREES, items)
		later += band(race, [kind for kind in kinds if kind not in first_kinds], honor.DEGREES, items)
		start = -(-len(first) // PAGE_SLOTS) * PAGE_SLOTS
		layout += [(start + at, package) for at, package in enumerate(later)]
	return layout


# ================
# stone_slots
# ================
def stone_slots(items, expansion):
	degrees = NATIVE_DEGREES + (EXPANSION_DEGREES if expansion else ())
	return list(enumerate(package_of(honor.stone_code(d)) for d in degrees if honor.stone_code(d) in items))


# ================
# tab_kinds
#
# The weapon kinds a race's tab sells: native's Chinese tab keeps its
# stocked order; the expansion adds every kind the item tables carry an
# honor item of.
# ================
def tab_kinds(race, stocked, items, expansion):
	kinds = []
	for package in stocked:
		match = honor.HONOR_CODE.match(item_of(package))
		if match and match[1] == race and match[2] not in kinds:
			kinds.append(match[2])
	if expansion:
		found = honor.authored_kinds(code for code in items if code.startswith(f"ITEM_{race}_"))
		kinds += [kind for kind in found.get(race, []) if kind not in kinds]
	return kinds


# ================
# merge_honor_goods
#
# raw maps d10.NAMES to raw bytes; items maps an item code to its
# itemdata row. Returns the changed tables, and a summary.
# ================
def merge_honor_goods(raw, items, expansion):
	rows = {name: tables.table_rows(raw[name]) for name in d10.NAMES}
	tabs = [r for r in rows["refshoptab.txt"] if len(r) >= 6]
	groups = defaultdict(dict)
	for tab in tabs:
		# The expansion's tab carries the weapon label too; the group's own
		# weapon tab is the other one.
		if tab[5] in (WEAPON_LABEL, STONE_LABEL) and not tab[3].endswith(EXPANSION_TAB):
			groups[tab[4]].setdefault(tab[5], tab)
	groups = {group: found for group, found in groups.items() if WEAPON_LABEL in found and STONE_LABEL in found}
	goods_by_tab = defaultdict(list)
	for row in rows["refshopgoods.txt"]:
		if len(row) > SLOT and row[0] == "1":
			goods_by_tab[row[2]].append(row)

	# The expansion's tab: added per honor group when on, removed when off.
	next_tab = max((int(r[2]) for r in tabs if r[2].isdigit()), default=0) + 1
	kept_tabs, removed_tabs, layouts = [], set(), {}
	for tab in tabs:
		if tab[4] in groups and tab[3].endswith(EXPANSION_TAB) and tab[5] == WEAPON_LABEL:
			removed_tabs.add(tab[3])
			continue
		kept_tabs.append(tab)
	for group, found in sorted(groups.items()):
		weapon, stone = found[WEAPON_LABEL], found[STONE_LABEL]
		stocked = [r[3] for r in sorted(goods_by_tab[weapon[3]], key=lambda r: int(r[SLOT]))]
		# The native kinds are the first page's: a rerun finds the
		# expansion's own kinds stocked from the second page on.
		first_page = [r[3] for r in goods_by_tab[weapon[3]] if int(r[SLOT]) < PAGE_SLOTS]
		native_kinds = tab_kinds("CH", [p for p in stocked if p in first_page], items, False)
		layouts[weapon[3]] = weapon_slots("CH", tab_kinds("CH", stocked, items, expansion), native_kinds, items, expansion)
		layouts[stone[3]] = stone_slots(items, expansion)
		if expansion:
			code = TAB_SUFFIX.sub(EXPANSION_TAB, weapon[3])
			existing = next((r for r in tabs if r[3] == code), None)
			kept_tabs.append(existing or [*weapon[:2], str(next_tab), code, *weapon[4:]])
			next_tab += 0 if existing else 1
			removed_tabs.discard(code)
			layouts[code] = weapon_slots("EU", tab_kinds("EU", [], items, expansion), [], items, expansion)

	# Packages: mint what a stocked good lacks.
	packages = {r[3]: r for r in rows["refpackageitem.txt"] if len(r) > 3}
	scraps = {r[2]: r for r in rows["refscrapofpackageitem.txt"] if len(r) > 3}
	prices = defaultdict(list)
	for row in rows["refpricepolicyofitem.txt"]:
		if len(row) > PRICE_AMOUNT:
			prices[row[2]].append(row)
	weapon_template = next((code for code in sorted(packages) if honor.HONOR_CODE.match(item_of(code)) and code in scraps), None)
	stone_template = next((code for code in sorted(packages) if code.startswith(PACKAGE_PREFIX + honor.STONE_PREFIX[5:]) and code in scraps), None)
	next_package = max((int(r[2]) for r in packages.values() if r[2].isdigit()), default=0) + 1
	next_scrap = max((int(r[-1]) for r in scraps.values() if r[-1].isdigit()), default=0) + 1
	minted, missing = [], []
	new_packages, new_scraps, new_prices = [], [], []
	for package in sorted({p for layout in layouts.values() for _, p in layout} - set(packages)):
		item = items[item_of(package)]
		match = honor.HONOR_CODE.match(item_of(package))
		template = weapon_template if match else stone_template
		if template is None:
			missing.append(package)
			continue
		row = list(packages[template])
		row[2], row[3] = str(next_package), package
		row[6], row[7] = item[d10.NAME_COLUMN], item[d10.DESCRIPTION_COLUMN]
		row[8] = next((v for v in item if v.lower().endswith(d10.ICON_SUFFIX)), row[8])
		scrap = list(scraps[template])
		scrap[2], scrap[3], scrap[-1] = package, item_of(package), str(next_scrap)
		if match:
			degree, grade = int(match[3]), match[4]
			scrap[SCRAP_DATA] = str(int(float(item[honor.DURABILITY])))
			scrap[SCRAP_PARAM_COUNT] = "1"
			scrap[SCRAP_PARAM] = str((REPAIR_VALUE << MAGIC_VALUE_SHIFT) | (REPAIR_OPTION_BASE + degree))
			gold, points = item[honor.PRICE], honor.honor_points(degree, grade)
		else:
			degree = int(item[honor.items.CLASS])
			gold, points = honor.stone_gold(degree), honor.stone_points(degree)
		for template_price in prices[template]:
			price = list(template_price)
			price[2] = package
			price[PRICE_AMOUNT] = str(gold if price[PRICE_PAYMENT] == PAYMENT_GOLD else points)
			new_prices.append(price)
		new_packages.append(row)
		new_scraps.append(scrap)
		packages[package] = row
		minted.append(package)
		next_package += 1
		next_scrap += 1
	missing_set = set(missing)

	# Goods: every honor tab rewritten to its layout, the removed tab's
	# goods dropped.
	goods = [r for r in rows["refshopgoods.txt"] if len(r) < 3 or (r[2] not in layouts and r[2] not in removed_tabs)]
	template_goods = next((r for r in rows["refshopgoods.txt"] if len(r) > SLOT and r[2] in layouts), None)
	for tab in sorted(layouts):
		for slot, package in layouts[tab]:
			if package not in missing_set:
				goods.append([*template_goods[:2], tab, package, str(slot), *template_goods[SLOT + 1:]])

	# Orphans: honor packages no shop stocks any more (the expansion's,
	# with the switch off). Native stocks every honor package it ships.
	honor_package = re.compile(r"^PACKAGE_(ITEM_(CH|EU)_[A-Z]+_\d\d_[ABC]_RARE_HONOR|" + honor.STONE_PREFIX + r"\d\d)$")
	stocked = {r[3] for r in goods if len(r) > 3}
	orphans = {code for code in packages if honor_package.match(code) and code not in stocked}

	# ================
	# rewritten
	#
	# A table the pass only adds to keeps its exact bytes first
	# (npcs.append_rows); one it removes rows from is rewritten.
	# ================
	def rewritten(name, column, extra):
		kept = [r for r in rows[name] if not (len(r) > column and r[column] in orphans)]
		if len(kept) == len(rows[name]):
			return npcs.append_rows(raw[name], extra)
		return b"\xff\xfe" + npcs.table_text(kept + extra).encode("utf-16-le")

	merged = {
		"refshopgoods.txt": b"\xff\xfe" + npcs.table_text(goods).encode("utf-16-le"),
		"refpackageitem.txt": rewritten("refpackageitem.txt", 3, new_packages),
		"refscrapofpackageitem.txt": rewritten("refscrapofpackageitem.txt", 2, new_scraps),
		"refpricepolicyofitem.txt": rewritten("refpricepolicyofitem.txt", 2, new_prices)
	}
	if kept_tabs != tabs:
		merged["refshoptab.txt"] = b"\xff\xfe" + npcs.table_text(kept_tabs).encode("utf-16-le")
	summary = {
		"expansion": expansion, "groups": sorted(groups), "packages": len(minted), "removedPackages": len(orphans),
		"missing": missing, "goods": {tab: len(layout) for tab, layout in sorted(layouts.items())}
	}
	return merged, summary
