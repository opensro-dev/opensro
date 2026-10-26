"""
===========================================================================

alexandria_shop_supplies.py - band consumables and alchemy for the city

Port-only, not native. Called only by the Alexandria variant builder, after
the degree 10 pass (alexandria_shop_d10). Issue #1748, first PR.

Two supplies the native tables give no shop for:

Consumables. The city's herbalist maps to STORE_KT_POTION, the store
Khotan's herbalist shares, and the native tab tops out below the band: it
stocks the top HP/MP potion and cure-all grades but only the lower
random-cure and detect grades. The city gets a potion store of its own, copied from
Khotan's exactly as the degree 10 pass copies the smith and armor stores,
and the band grades join the copied tab. The top grades are the native
answer at level 100-110: the strongest HP potion restores 820 HP against
pools of roughly 8,500-11,000 (MaxHP = trunc(1.02^(level-1) * STR * 10)),
so no stronger grade is authored.

Alchemy. The native gold shops sell no degree 10 or 11 stones at all (the
mall only), so the city's accessory store gains them on its alchemy tab
(the tab the extra-goods pass names SN_TAB_ALCHEMY): the full degree 10
band there, and the degree 11 band on a second alchemy tab in the same tab
group, so no tab passes the 45-slot native maximum. Tablets and elements
in their drop forms stay out of shops; degree 11 gear is a separate PR.

Every good is added only when its item exists in the variant item table,
which is what SRO_CAP110_ITEMS gates: without those items this pass copies
the potion store and stocks no alchemy. A package is minted from the same
kind's degree 9 (or lower-grade) package with the item's own price column,
as the degree 10 pass mints its packages. Missing item packages are
reported, never partly granted.

===========================================================================
"""
import re
from collections import defaultdict

import alexandria_npcs as npcs
import alexandria_shop_d10 as d10
import import_vsro_alexandria as tables

POTION_STORE, POTION_SOURCE = "STORE_SD_POTION", "STORE_KT_POTION"
POTION_GROUPS = ("GROUP_STORE_SD_M_POTION", "GROUP_STORE_SD_T_POTION")
ACCESSORY_STORE = "STORE_SD_ACCESSORY"
ALCHEMY_TAB_NAME = "SN_TAB_ALCHEMY"
TAB_NUMBER = re.compile(r"(\d+)$")

# The band grades the copied potion tab gains, in stock order. The top HP
# and MP potion grades are already in the native tab and arrive with the
# copy. Vigor potions are not here: no native package of any grade exists
# for them, and minting one from another kind's template would invent a
# package shape the tables never ship - vigor stock stays with the
# server-side extra-goods list, which names its stores explicitly.
CONSUMABLE_PACKAGES = (
	"PACKAGE_ITEM_ETC_CURE_ALL_04",
	"PACKAGE_ITEM_ETC_CURE_RANDOM_04",
	"PACKAGE_ITEM_ETC_DETECT_04"
)

MAGICSTONE_KINDS = (
	"ASTRAL", "ATHANASIA", "BURN", "DUR", "ER", "ESHOCK", "EVADE_BLOCK", "EVADE_CRITICAL", "FROSTBITE",
	"HP", "HR", "INT", "LUCK", "MP", "POISON", "SOLID", "STR", "ZOMBIE"
)
ATTRSTONE_KINDS = (
	"BR", "CRITICAL", "ER", "HR", "MAINT", "MAR", "MA", "MDINT", "MD", "PAR", "PASTR", "PA", "PDSTR", "PD"
)
ELEMENT_KINDS = ("EARTH", "FIRE", "WATER", "WIND")
ALCHEMY_DEGREES = ("10", "11")


# ================
# alchemy_items
#
# The item codes of one degree's shop alchemy band: the magic and attribute
# stones, the four elements and the lucky-powder elixir. 37 codes.
# ================
def alchemy_items(degree):
	codes = [f"ITEM_ETC_ARCHEMY_MAGICSTONE_{kind}_{degree}" for kind in MAGICSTONE_KINDS]
	codes += [f"ITEM_ETC_ARCHEMY_ATTRSTONE_{kind}_{degree}" for kind in ATTRSTONE_KINDS]
	codes += [f"ITEM_ETC_ELEMENT_{kind}_{degree}" for kind in ELEMENT_KINDS]
	codes.append(f"ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_{degree}")
	return codes


# ================
# lower_grade_of
#
# The same consumable one grade down (..._04 -> ..._03), the package a
# minted band package is copied from; None when the code ends in no grade.
# ================
def lower_grade_of(code):
	match = TAB_NUMBER.search(code)
	if match is None or int(match.group(1)) <= 0:
		return None
	return code[:match.start()] + str(int(match.group(1)) - 1).zfill(len(match.group(1)))


# ================
# degree9_source
#
# The degree 9 twin an alchemy package is minted from. Alchemy codenames
# end in their degree (..._LUCK_10), where the degree 10 pass's mid-code
# token does not reach; both shapes are tried, and a code that cannot
# change answers None so the template walk always ends.
# ================
def degree9_source(code):
	changed = re.sub(r"_(?:10|11)$", "_09", code)
	if changed == code:
		changed = d10.with_degree(code, "09")
	return changed if changed != code else None


# ================
# merge_supplies
#
# raw maps the shop table names to raw bytes; items maps an item code to
# its itemdata row. Returns the changed tables, and a summary.
# ================
def merge_supplies(raw, items):
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
	tab_groups = {r[3]: r for r in rows["refshoptabgroup.txt"] if len(r) > 3}
	scrap_ids = [int(r[-1]) for r in rows["refscrapofpackageitem.txt"] if r[-1].isdigit()]
	next_scrap = max(scrap_ids, default=0) + 1

	new_packages, new_scraps, new_prices = [], [], []
	missing = set()

	# ================
	# make_package
	#
	# Mint the package for an item code once, copied from the template
	# package of the same kind (its degree 9 or lower-grade twin, walked
	# to by source_of), priced from the item's own price column. Returns
	# False - and reports the package - when the item or a template is
	# missing, so a good is never partly granted.
	# ================
	def make_package(item_code, source_of):
		nonlocal next_scrap
		code = "PACKAGE_" + item_code
		if code in packages:
			return True
		source = source_of(code)
		while source is not None and source not in packages:
			source = source_of(source)
		item = items.get(item_code)
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
	# stock_tab
	#
	# A tab's goods after the wanted item codes join it: ones already
	# there are skipped, minted packages use template_row for the new
	# goods rows, and the tab never passes the native maximum. Returns
	# the rows and how many goods were added.
	# ================
	def stock_tab(tab_code, wanted, source_of, template_row):
		old = sorted(goods_by_tab[tab_code], key=lambda r: int(r[4]))
		have = {r[3] for r in old}
		rows_out = list(old)
		for item_code in wanted:
			code = "PACKAGE_" + item_code
			if code in have or len(rows_out) >= d10.MAX_TAB_SLOTS:
				continue
			if not make_package(item_code, source_of):
				continue
			rows_out.append([*template_row[:2], tab_code, code, str(len(rows_out)), *template_row[5:]])
			have.add(code)
		goods_by_tab[tab_code] = rows_out
		return rows_out, len(rows_out) - len(old)

	summary = {"store": None, "goods": 0, "packages": 0, "alchemyTab": None}
	appended = {name: [] for name in d10.NAMES}
	appended_goods = []
	replaced_tabs = {}
	moved_groups = {}

	# The potion store of the city's own, Khotan's copied and re-pointed.
	stores = {r[3] for r in rows["refshop.txt"] if len(r) > 3}
	if POTION_STORE in stores:
		# The NPC merge rebuilds mappings before this pass; reconnect an
		# owned store even when its goods were staged by a previous build.
		moved_groups.update({group: POTION_STORE for group in POTION_GROUPS})
	elif POTION_SOURCE in stores:
		shop = next(r for r in rows["refshop.txt"] if len(r) > 3 and r[3] == POTION_SOURCE)
		appended["refshop.txt"].append([*shop[:2], new_id("refshop.txt"), POTION_STORE, *shop[4:]])
		for group in groups_of_store[POTION_SOURCE]:
			new_group = group.replace(d10.STORE_PREFIX_FROM, d10.STORE_PREFIX_TO, 1)
			group_row = list(tab_groups[group])
			group_row[2], group_row[3] = new_id("refshoptabgroup.txt"), new_group
			appended["refshoptabgroup.txt"].append(group_row)
			appended["refmappingshopwithtab.txt"].append([*rows["refmappingshopwithtab.txt"][0][:2], POTION_STORE, new_group])
			for tab_row in tabs_of_group[group]:
				new_tab = tab_row[3].replace(d10.STORE_PREFIX_FROM, d10.STORE_PREFIX_TO, 1)
				cloned = list(tab_row)
				cloned[2], cloned[3], cloned[4] = new_id("refshoptab.txt"), new_tab, new_group
				appended["refshoptab.txt"].append(cloned)
				goods_by_tab[new_tab] = [[*g[:2], new_tab, *g[3:]] for g in goods_by_tab[tab_row[3]]]
				if goods_by_tab[new_tab]:
					stocked, count = stock_tab(new_tab, [c.replace("PACKAGE_ITEM_", "ITEM_", 1) for c in CONSUMABLE_PACKAGES], lower_grade_of, goods_by_tab[new_tab][0])
					appended_goods += stocked
					summary["goods"] += count
		moved_groups.update({group: POTION_STORE for group in POTION_GROUPS})
		summary["store"] = POTION_STORE

	# Alchemy on the accessory store's own alchemy tab, degree 11 on a
	# second tab of the same group.
	alchemy_tab = next(
		(t for group in groups_of_store[ACCESSORY_STORE] for t in tabs_of_group[group] if len(t) >= 6 and t[5] == ALCHEMY_TAB_NAME),
		None
	)
	# An alchemy tab with no goods has no row to copy new goods from; it
	# counts as no tab at all.
	if alchemy_tab is not None and not goods_by_tab[alchemy_tab[3]]:
		alchemy_tab = None
	if alchemy_tab is not None:
		summary["alchemyTab"] = alchemy_tab[3]
		goods_row = goods_by_tab[alchemy_tab[3]][0]
		stocked, count = stock_tab(alchemy_tab[3], alchemy_items(ALCHEMY_DEGREES[0]), degree9_source, goods_row)
		if count:
			replaced_tabs[alchemy_tab[3]] = stocked
			summary["goods"] += count
		degree11 = [code for code in alchemy_items(ALCHEMY_DEGREES[1]) if code in items]
		if degree11:
			group_tabs = tabs_of_group[alchemy_tab[4]]
			numbers = [int(m.group(1)) for t in group_tabs if (m := TAB_NUMBER.search(t[3]))]
			stem = TAB_NUMBER.sub("", alchemy_tab[3])
			new_tab = stem + str(max(numbers, default=0) + 1)
			tab_row = list(alchemy_tab)
			tab_row[2], tab_row[3] = new_id("refshoptab.txt"), new_tab
			goods_by_tab[new_tab] = []
			stocked, count = stock_tab(new_tab, degree11, degree9_source, goods_row)
			# The second tab is authored only when something stocks on it.
			if count:
				appended["refshoptab.txt"].append(tab_row)
				appended_goods += stocked
				summary["goods"] += count

	mapping = []
	for row in rows["refmappingshopgroup.txt"]:
		if len(row) >= 4 and row[2] in moved_groups:
			row = [*row[:3], moved_groups[row[2]]]
		mapping.append(row)

	goods = [r for r in rows["refshopgoods.txt"] if len(r) < 3 or r[2] not in replaced_tabs]
	for stocked in replaced_tabs.values():
		goods += stocked
	goods += appended_goods
	merged = {
		"refshop.txt": npcs.append_rows(raw["refshop.txt"], appended["refshop.txt"]),
		"refmappingshopgroup.txt": b"\xff\xfe" + npcs.table_text(mapping).encode("utf-16-le"),
		"refmappingshopwithtab.txt": npcs.append_rows(raw["refmappingshopwithtab.txt"], appended["refmappingshopwithtab.txt"]),
		"refshoptabgroup.txt": npcs.append_rows(raw["refshoptabgroup.txt"], appended["refshoptabgroup.txt"]),
		"refshoptab.txt": npcs.append_rows(raw["refshoptab.txt"], appended["refshoptab.txt"]),
		"refshopgoods.txt": b"\xff\xfe" + npcs.table_text(goods).encode("utf-16-le"),
		"refpackageitem.txt": npcs.append_rows(raw["refpackageitem.txt"], new_packages),
		"refscrapofpackageitem.txt": npcs.append_rows(raw["refscrapofpackageitem.txt"], new_scraps),
		"refpricepolicyofitem.txt": npcs.append_rows(raw["refpricepolicyofitem.txt"], new_prices)
	}
	summary["packages"] = len(new_packages)
	summary["missing"] = sorted(missing)
	return merged, summary
