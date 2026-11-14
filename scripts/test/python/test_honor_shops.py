"""
===========================================================================

test_honor_shops.py - the honor stores' layout contract (#3548)

Native (always): each weapon tab stocks, per kind, degree 8 grades A-C
then degree 9 grades A-C, and the stone tab the degree 8 then 9 stone,
as iSRO-R's tables do. Port-only (expansion on): degrees 10 and 11 from
the weapon tab's next page, a third tab of European weapons, the later
stones, and a minted package for every good that lacks one. A rerun
changes nothing, and switching off after on gives the off tables back.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import unittest

import alexandria_shop_d10 as d10
import cap110_items as items
import honor_items as honor
import honor_shops as shops
import import_vsro_alexandria as tables

GROUP = "STORE_KT_HONOR_GROUP1"
WEAPONS, STONES, EUROPE = "STORE_KT_HONOR_TAB1", "STORE_KT_HONOR_TAB2", "STORE_KT_HONOR_TAB3"
FILLER = ["-1", "xxx"] * 4


# ================
# utf16
# ================
def utf16(rows):
	return b"\xff\xfe" + "".join("\t".join(row) + "\r\n" for row in rows).encode("utf-16-le")


# ================
# item
# ================
def item(code, price=1000, durability=120, degree="0"):
	row = ["0"] * items.ITEM_COLUMNS
	row[items.CODENAME] = code
	row[d10.NAME_COLUMN], row[d10.DESCRIPTION_COLUMN] = "SN_" + code, "SN_" + code + "_TT_DESC"
	row[items.ICON] = "item\\" + code.lower() + ".ddj"
	row[honor.PRICE], row[honor.DURABILITY], row[items.CLASS] = str(price), str(durability), degree
	return row


# ================
# package / scrap / prices / good
# ================
def package(identity, code):
	return ["1", "11", str(identity), code, "0", "EXPAND_TERM_ALL", "SN_" + code, "SN_" + code + "_TT_DESC", "icon.ddj", *FILLER]


def scrap(identity, code, durability="0", param="0"):
	return ["1", "11", code, shops.item_of(code), "0", "0", durability, "1" if param != "0" else "0", param, *["0"] * 11, *FILLER, str(identity)]


def prices(code, gold, points):
	return [["1", "11", code, shops.PAYMENT_GOLD, str(gold), *FILLER], ["1", "11", code, shops.PAYMENT_HONOR, str(points), *FILLER]]


def good(tab, code, slot):
	return ["1", "11", tab, code, str(slot), *FILLER]


# ================
# native_tables
#
# One honor group as v1.150 ships it: degree 8 Chinese blades and bows
# stocked, the degree 9 packages authored but unstocked; the degree 8
# stone stocked, the degree 9 stone authored.
# ================
def native_tables():
	codes = {}
	for kind in ("BLADE", "BOW"):
		for degree in (8, 9):
			for grade in honor.GRADES:
				codes[honor.honor_code("CH", kind, degree, grade)] = (degree, grade)
	stones = [honor.stone_code(8), honor.stone_code(9)]
	packages, scraps, price_rows = [], [], []
	for at, code in enumerate([*codes, *stones]):
		name = shops.package_of(code)
		packages.append(package(100 + at, name))
		if code in codes:
			degree, grade = codes[code]
			param = (shops.REPAIR_VALUE << shops.MAGIC_VALUE_SHIFT) | (shops.REPAIR_OPTION_BASE + degree)
			scraps.append(scrap(500 + at, name, "120", str(param)))
			price_rows += prices(name, honor.honor_gold(degree, grade), honor.honor_points(degree, grade))
		else:
			scraps.append(scrap(500 + at, name))
			degree = int(code[-2:])
			price_rows += prices(name, honor.stone_gold(degree), honor.stone_points(degree))
	stocked = [shops.package_of(code) for code, (degree, _) in codes.items() if degree == 8]
	goods = [good(WEAPONS, code, slot) for slot, code in enumerate(stocked)] + [good(STONES, shops.package_of(stones[0]), 0)]
	goods.append(good("STORE_KT_POTION_TAB1", "PACKAGE_ITEM_ETC_HP_POTION_01", 0))
	raw = {name: utf16([]) for name in d10.NAMES}
	raw["refshoptab.txt"] = utf16([["1", "11", "10", WEAPONS, GROUP, shops.WEAPON_LABEL], ["1", "11", "11", STONES, GROUP, shops.STONE_LABEL]])
	raw["refshopgoods.txt"] = utf16(goods)
	raw["refpackageitem.txt"] = utf16(packages)
	raw["refscrapofpackageitem.txt"] = utf16(scraps)
	raw["refpricepolicyofitem.txt"] = utf16(price_rows)
	return raw, {code: item(code) for code in [*codes, *stones]}


# ================
# expanded_items
#
# The item tables with honor_items' rows: a degree 10 Chinese blade, a
# degree 8 Chinese shield, a degree 8 European axe, the degree 10 stone.
# ================
def expanded_items(native_items):
	found = dict(native_items)
	found["ITEM_CH_BLADE_10_A_RARE_HONOR"] = item("ITEM_CH_BLADE_10_A_RARE_HONOR", 8900000, 140)
	found["ITEM_EU_AXE_08_A_RARE_HONOR"] = item("ITEM_EU_AXE_08_A_RARE_HONOR", 2700000, 130)
	found[honor.stone_code(10)] = item(honor.stone_code(10), degree="10")
	found["ITEM_CH_SHIELD_08_A_RARE_HONOR"] = item("ITEM_CH_SHIELD_08_A_RARE_HONOR", 2700000, 110)
	return found


# ================
# apply
# ================
def apply(raw, found, expansion):
	merged, summary = shops.merge_honor_goods(raw, found, expansion)
	return {**raw, **merged}, summary


# ================
# layout
# ================
def layout(raw, tab):
	rows = [r for r in tables.table_rows(raw["refshopgoods.txt"]) if r[2] == tab]
	return [(int(r[shops.SLOT]), r[3]) for r in sorted(rows, key=lambda r: int(r[shops.SLOT]))]


class HonorShopTest(unittest.TestCase):
	def test_native_layout_stocks_degree_9(self):
		raw, found = native_tables()
		merged, summary = apply(raw, found, False)
		weapons = [code for _, code in layout(merged, WEAPONS)]
		self.assertEqual(len(weapons), 12)
		self.assertEqual(weapons[:6], [shops.package_of(honor.honor_code("CH", "BLADE", d, g)) for d in (8, 9) for g in honor.GRADES])
		self.assertEqual(layout(merged, STONES), [(0, shops.package_of(honor.stone_code(8))), (1, shops.package_of(honor.stone_code(9)))])
		self.assertEqual(layout(merged, "STORE_KT_POTION_TAB1"), [(0, "PACKAGE_ITEM_ETC_HP_POTION_01")])
		self.assertEqual((summary["packages"], summary["removedPackages"]), (0, 0))
		self.assertNotIn("refshoptab.txt", shops.merge_honor_goods(raw, found, False)[0])
		for name in ("refpackageitem.txt", "refscrapofpackageitem.txt", "refpricepolicyofitem.txt"):
			self.assertEqual(merged[name], raw[name])

	def test_expansion_adds_a_page_a_tab_and_packages(self):
		raw, found = native_tables()
		merged, summary = apply(raw, expanded_items(found), True)
		blade_10 = shops.package_of("ITEM_CH_BLADE_10_A_RARE_HONOR")
		shield = shops.package_of("ITEM_CH_SHIELD_08_A_RARE_HONOR")
		# The native page stays as off lays it; the expansion starts a page on,
		# the later degrees first, then the added kinds.
		native, _ = apply(raw, found, False)
		self.assertEqual(layout(merged, WEAPONS)[:12], layout(native, WEAPONS))
		self.assertEqual(layout(merged, WEAPONS)[12:], [(shops.PAGE_SLOTS, blade_10), (shops.PAGE_SLOTS + 1, shield)])
		self.assertEqual(layout(merged, EUROPE), [(0, "PACKAGE_ITEM_EU_AXE_08_A_RARE_HONOR")])
		self.assertEqual(layout(merged, STONES)[-1], (2, shops.package_of(honor.stone_code(10))))
		tabs = tables.table_rows(merged["refshoptab.txt"])
		self.assertIn([GROUP, shops.WEAPON_LABEL], [r[4:6] for r in tabs if r[3] == EUROPE])
		self.assertEqual(summary["packages"], 4)
		scraps = {r[2]: r for r in tables.table_rows(merged["refscrapofpackageitem.txt"])}
		self.assertEqual(scraps[blade_10][shops.SCRAP_DATA], "140")
		self.assertEqual(int(scraps[blade_10][shops.SCRAP_PARAM]), (6 << 32) | 236)
		price = {(r[2], r[3]): r[4] for r in tables.table_rows(merged["refpricepolicyofitem.txt"])}
		self.assertEqual(price[(blade_10, shops.PAYMENT_GOLD)], "8900000")
		self.assertEqual(price[(blade_10, shops.PAYMENT_HONOR)], str(honor.honor_points(10, "A")))
		stone_10 = shops.package_of(honor.stone_code(10))
		self.assertEqual(price[(stone_10, shops.PAYMENT_GOLD)], str(honor.stone_gold(10)))

	def test_rerun_changes_nothing(self):
		raw, found = native_tables()
		found = expanded_items(found)
		once, _ = apply(raw, found, True)
		twice, summary = apply(once, found, True)
		self.assertEqual(twice, once)
		self.assertEqual(summary["packages"], 0)

	def test_switching_off_gives_the_native_tables_back(self):
		raw, found = native_tables()
		fresh, _ = apply(raw, found, False)
		on, _ = apply(raw, expanded_items(found), True)
		off, summary = apply(on, found, False)
		self.assertEqual(summary["removedPackages"], 4)
		self.assertEqual(off, fresh)

	def test_goods_need_their_item(self):
		raw, found = native_tables()
		del found[honor.honor_code("CH", "BOW", 9, "C")]
		merged, _ = apply(raw, found, False)
		self.assertEqual(len(layout(merged, WEAPONS)), 11)


if __name__ == "__main__":
	unittest.main()
