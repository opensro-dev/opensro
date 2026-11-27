"""
===========================================================================

mall_tables.py - the four Item Mall tables, as the mall imports write them

refpackageitem, refscrapofpackageitem, refpricepolicyofitem and
refshopgoods name a package in one column each; a mall import appends
rows to all four and a rebuild strips them again by package code. The
avatar import (mall_avatar_import.py) and the later mall goods
(later_mall_goods.py) both list a later-client package through
copy_later_package, so the layout conversion lives in one place.

===========================================================================
"""
import cap110_item_import as shared
import cap110_items as items
import import_vsro_alexandria as tables
from prepare_client_resources import safe_target, write_atomically

TEXT_ROOT = shared.TEXT_ROOT
SERVICE_ON = "1"
# Package, scrap, price and goods tables with the column holding the package code.
MALL_TABLES = (
	("refpackageitem.txt", 3), ("refscrapofpackageitem.txt", 2),
	("refpricepolicyofitem.txt", 2), ("refshopgoods.txt", 3),
)
# The source cell native mall rows carry.
NATIVE_SOURCE = "11"
# refpricepolicyofitem's payment cell for silk.
SILK_CURRENCY = "2"
PACKAGE_FLAG_COLUMN = 4
# The later price table's amount column; v1.150's sits one to the left.
MALL_PRICE_AMOUNT = 5
# A goods row's slot is one byte on the wire (mallcatalog.go MallAddress.Slot).
MALL_SLOTS = 256


# ================
# in_packages
#
# Whether one mall table row belongs to these packages, by the table's
# package-code column (MALL_TABLES).
# ================
def in_packages(fields, column, packages):
	return len(fields) > column and fields[column] in packages


# ================
# strip_mall
#
# Drops the rows a previous run added to the four mall tables, by package
# code, leaving every other line (including comments) untouched.
# ================
def strip_mall(output, packages):
	if not packages:
		return
	for name, column in MALL_TABLES:
		path = safe_target(output / TEXT_ROOT, name)
		if not path.exists():
			continue
		lines = tables.decode_table(path.read_bytes()).splitlines()
		kept = [line for line in lines if not in_packages(line.split("\t"), column, packages)]
		if len(kept) != len(lines):
			write_atomically(path, b"\xff\xfe" + ("\r\n".join(kept) + "\r\n").encode("utf-16-le"))


# ================
# without_packages
#
# The mall tables without the rows of these packages, by each table's
# package-code column. A later source's rows from a previous build are
# read through this before an earlier source allocates past them.
# ================
def without_packages(current, packages):
	if not packages:
		return current
	return {
		name: [row for row in current[name] if not in_packages(row, column, packages)]
		for name, column in MALL_TABLES
	}


# ================
# LaterPackage / copy_later_package
#
# One later-client mall package carried into the native layout: its
# package row (source cell native, the later flag cleared), its scraps,
# its prices (the later price table carries one extra column before the
# amount, dropped; silk, when given, replaces the silk amount) and one
# goods row at the given tab and slot. The avatar import and the later
# mall goods (later_mall_goods.py) both list a package through here.
# ================
class LaterPackage:
	def __init__(self, package_row, prices, tab, slot, silk=None, package_id=None):
		self.package_row, self.prices, self.tab, self.slot = package_row, prices, tab, slot
		self.silk, self.package_id = silk, package_id


# ================
# goods_row
#
# A refshopgoods row listing package in tab at slot.
# ================
def goods_row(tab, package, slot):
	return ["1", NATIVE_SOURCE, tab, package, str(slot)] + ["-1", items.UNUSED] * 4


# ================
# converted_price
#
# A later refpricepolicyofitem row in v1.150's layout: the native source
# id, and the amount one column to the left. silk, when given, replaces
# a silk row's amount.
# ================
def converted_price(price, silk=None):
	converted = price[:1] + [NATIVE_SOURCE] + price[2:MALL_PRICE_AMOUNT - 1] + price[MALL_PRICE_AMOUNT:]
	if silk is not None and converted[3] == SILK_CURRENCY:
		converted[4] = silk
	return converted


def copy_later_package(rows, later, listing):
	package = listing.package_row[3]
	package_cells = list(listing.package_row)
	package_cells[1], package_cells[PACKAGE_FLAG_COLUMN] = NATIVE_SOURCE, "0"
	if listing.package_id is not None:
		package_cells[2] = str(listing.package_id)
	rows["refpackageitem.txt"].append(package_cells)
	for scrap in later["refscrapofpackageitem.txt"]:
		if scrap[0] == SERVICE_ON and len(scrap) >= 20 and scrap[2] == package:
			rows["refscrapofpackageitem.txt"].append(scrap[:1] + [NATIVE_SOURCE] + scrap[2:])
	for price in listing.prices:
		rows["refpricepolicyofitem.txt"].append(converted_price(price, listing.silk))
	rows["refshopgoods.txt"].append(goods_row(listing.tab, package, listing.slot))
