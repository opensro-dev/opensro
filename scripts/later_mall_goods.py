"""
===========================================================================

later_mall_goods.py - the v1.696 Item Mall goods whose items the tree holds

The v1.696 client's mall sells goods v1.150's never lists: the Immortal
and Astral stones of degrees 10 and 11 (packages 20113/20114 and their
Astral pair; v1.150 stops at degree 9 although its item table carries the
stones to 12), the Arabia avatars the Arabia layer imports, the gender
change bundles, the 11-day scroll packs and the Magic POP card. A later
native client is native (owner 2026-11-08, 2026-11-24: "we porting the
full game"), so this is no switch: the step lists every such good whose
items are in the built tree, and a good whose items are not is reported,
never half listed.

A good lists when:
	- the v1.696 goods row is in service, in a MALL_ tab the tree has;
	- the tree has no package of that code;
	- the package, its scraps and a price are authored in v1.696;
	- every item its scraps hold is in the tree, and a summon scroll's
	  creature (ITEM_COS_X_SCROLL summons COS_X) is in the tree too;
	- no scrap names a rental term (RENT_ITEM_*, v1.696's refrentitem):
	  the port has no rental clock, so a 1-day outfit at 14 silk would
	  deliver a permanent one. The rentals wait for that system.

Not listed: the PRE_MALL_ tabs, v1.696's second-currency shop of ITEM_PRE_
copies (no currency of that kind exists here); and the Devil's Spirit's
own tabs, which the server lays out itself (mallcategories.go
nasrunGoods). Every good sells at its v1.696 price, which continues
v1.150's ladder (degree 9 stones are 51 silk in both clients, a
permanent outfit 130); only the payments v1.150's mall knows (silk,
gift silk, the points row) are carried, since v1.696's 2048/4096 are its
second currency and mallcatalog.go refuses any other.

It runs after every item import, so it sees all of their rows, and its
marker strips its previous rows before a rebuild lists them again.

===========================================================================
"""
import collections
from contextlib import ExitStack
import json
import re
from pathlib import Path

import import_vsro_alexandria as tables
import mall_avatar_import as avatars
import mall_tables as mall
import sro_pk2
from prepare_client_resources import safe_target, write_atomically
from sro_paths import trees_overlap
import alexandria_npcs as text_tables

MARKER = ".opensro-later-mall.json"
FORMAT = "opensro-later-mall-v1"
TEXT_ROOT = mall.TEXT_ROOT
SHOP_TAB_TABLE = "refshoptab.txt"
SHOP_TAB_CODE = 3
GOODS_TAB, GOODS_PACKAGE, GOODS_SLOT = 2, 3, 4
PACKAGE_ID, PACKAGE_CODE = 2, 3
SCRAP_PACKAGE, SCRAP_ITEM = 2, 3
ITEM_CODE, CHARACTER_CODE = 2, 2
MALL_TAB = "MALL_"
SUMMON_SCROLL = re.compile(r"ITEM_(COS_[A-Z0-9_]+)_SCROLL")
SCRAP_RENT = 21
NO_RENT = "xxx"
MALL_CURRENCIES = ("2", "4", "16")


# ================
# tree_reader
#
# One textdata table of the built tree: the output's copy when a step
# before wrote one, else the native extraction's.
# ================
def tree_reader(native, output):
	base = native / TEXT_ROOT
	native_paths = {sro_pk2.fold_ascii(path.name): path for path in base.iterdir() if path.is_file()} if base.is_dir() else {}

	def read(name):
		written = output / TEXT_ROOT / name
		if written.is_file():
			return written.read_bytes()
		path = native_paths.get(sro_pk2.fold_ascii(name))
		return path.read_bytes() if path else None

	return read


# ================
# sharded_codes
#
# The codenames of one sharded table (itemdata, characterdata): its
# index names the shard files, each read from the tree.
# ================
def sharded_codes(read, index_name, column):
	raw = read(index_name)
	if raw is None:
		return set()
	codes = set()
	for row in tables.table_rows(raw):
		if not row or not row[0].strip():
			continue
		shard = read(sro_pk2.fold_ascii(row[0].strip()))
		if shard is not None:
			codes |= {cells[column] for cells in tables.table_rows(shard) if len(cells) > column}
	return codes


# ================
# TreeView
#
# What the selection reads of the built tree: its item and creature
# codenames, its mall tables and its shop tab codes.
# ================
class TreeView:
	def __init__(self, item_codes, creature_codes, mall_rows, shop_tabs):
		self.item_codes, self.creature_codes = item_codes, creature_codes
		self.mall_rows, self.shop_tabs = mall_rows, shop_tabs


# ================
# unlisted_reason
#
# Why one later good stays out, or None when it lists.
# ================
def unlisted_reason(tab, package, later_index, tree):
	package_row, scraps, prices = later_index
	if not tab.startswith(MALL_TAB):
		return "second-currency shop " + tab
	if tab not in tree.shop_tabs:
		return "no tab " + tab
	if package not in package_row or not prices.get(package):
		return "no package or price"
	rows = scraps.get(package, [])
	if not rows:
		return "no scrap"
	if any(len(row) > SCRAP_RENT and row[SCRAP_RENT] not in (NO_RENT, "") for row in rows):
		return "rental term"
	held = [row[SCRAP_ITEM] for row in rows]
	absent = [code for code in held if code not in tree.item_codes]
	if absent:
		return "item not in the tree: " + ", ".join(absent)
	for code in held:
		summon = SUMMON_SCROLL.fullmatch(code)
		if summon and summon.group(1) not in tree.creature_codes:
			return "creature not in the tree: " + summon.group(1)
	return None


# ================
# goods_rows
#
# The four tables' rows that list every later good the tree can sell,
# in each tab's later slot order after the tab's present slots, and the
# report of the goods left out. A package identity the tree already
# uses moves past every identity either client holds.
# ================
def goods_rows(later, tree):
	package_row = {row[PACKAGE_CODE]: row for row in later["refpackageitem.txt"]
		if row and row[0] == mall.SERVICE_ON and len(row) >= 9}
	scraps = collections.defaultdict(list)
	for row in later["refscrapofpackageitem.txt"]:
		if row and row[0] == mall.SERVICE_ON and len(row) >= 20:
			scraps[row[SCRAP_PACKAGE]].append(row)
	prices = collections.defaultdict(list)
	for row in later["refpricepolicyofitem.txt"]:
		if row and row[0] == mall.SERVICE_ON and len(row) > mall.MALL_PRICE_AMOUNT and row[3] in MALL_CURRENCIES:
			prices[row[2]].append(row)
	current = tree.mall_rows
	have = {row[PACKAGE_CODE] for row in current["refpackageitem.txt"] if len(row) > PACKAGE_CODE}
	have |= {row[GOODS_PACKAGE] for row in current["refshopgoods.txt"] if len(row) > GOODS_PACKAGE}
	used_ids = {int(row[PACKAGE_ID]) for row in current["refpackageitem.txt"] if len(row) > PACKAGE_ID and row[PACKAGE_ID].isdigit()}
	next_id = max(used_ids | {int(row[PACKAGE_ID]) for row in package_row.values() if row[PACKAGE_ID].isdigit()}, default=0) + 1
	next_slot = collections.Counter()
	for row in current["refshopgoods.txt"]:
		if len(row) > GOODS_SLOT and row[GOODS_SLOT].isdigit():
			next_slot[row[GOODS_TAB]] = max(next_slot[row[GOODS_TAB]], int(row[GOODS_SLOT]) + 1)
	sold = sorted((row for row in later["refshopgoods.txt"] if row and row[0] == mall.SERVICE_ON and len(row) > GOODS_SLOT
		and row[GOODS_TAB].startswith(("MALL_", "PRE_MALL_")) and row[GOODS_PACKAGE] not in have),
		key=lambda row: (row[GOODS_TAB], int(row[GOODS_SLOT]) if row[GOODS_SLOT].isdigit() else 0))
	rows = {name: [] for name, _ in mall.MALL_TABLES}
	listed, unlisted = [], collections.Counter()
	for row in sold:
		tab, package = row[GOODS_TAB], row[GOODS_PACKAGE]
		if package in listed:
			continue
		reason = unlisted_reason(tab, package, (package_row, scraps, prices), tree)
		if reason is not None:
			unlisted[reason.split(":")[0]] += 1
			continue
		package_id = None
		if int(package_row[package][PACKAGE_ID]) in used_ids:
			package_id, next_id = next_id, next_id + 1
		used_ids.add(int(package_id if package_id is not None else package_row[package][PACKAGE_ID]))
		mall.copy_later_package(rows, later, mall.LaterPackage(package_row[package], prices[package], tab,
			next_slot[tab], package_id=package_id))
		next_slot[tab] += 1
		listed.append(package)
	for tab, slots in next_slot.items():
		if slots > mall.MALL_SLOTS:
			raise ValueError(f"{tab} would hold {slots} goods; the mall's one-byte slot holds {mall.MALL_SLOTS}")
	return rows, {"packages": listed, "listed": len(listed), "unlisted": dict(sorted(unlisted.items()))}


# ================
# read_tree
#
# The TreeView of the built tree.
# ================
def read_tree(read):
	mall_rows = {name: tables.table_rows(read(name) or b"") for name, _ in mall.MALL_TABLES}
	tabs = {row[SHOP_TAB_CODE] for row in tables.table_rows(read(SHOP_TAB_TABLE) or b"") if len(row) > SHOP_TAB_CODE}
	return TreeView(sharded_codes(read, "itemdata.txt", ITEM_CODE), sharded_codes(read, "characterdata.txt", CHARACTER_CODE),
		mall_rows, tabs)


# ================
# add_goods
#
# The step: strips its previous rows, then lists the later goods. With no
# v1.696 client the tree keeps only what the steps before wrote.
# ================
def add_goods(later_root, native, output):
	native, output = Path(native).resolve(), Path(output).resolve()
	if trees_overlap(native, output):
		raise ValueError("later mall output must be separate from the native extraction")
	marker = output / MARKER
	if marker.exists():
		mall.strip_mall(output, set(json.loads(marker.read_text(encoding="utf-8")).get("packages", [])))
		marker.unlink()
	if not avatars.source_present(later_root, avatars.V1696_SOURCE):
		return {"enabled": False, "skipped": "no v1.696 client"}
	tables.require_vsro_pin(later_root, tuple(avatars.FOLDERS), avatars.source_pins(avatars.V1696_SOURCE))
	read = tree_reader(native, output)
	with ExitStack() as stack:
		archives, identities = avatars.open_archives(stack, later_root, avatars.V1696_SOURCE)
		media, index = archives["Media"]
		later = {name: avatars.later_table_rows(media, index, name) for name, _ in mall.MALL_TABLES}
	rows, summary = goods_rows(later, read_tree(read))
	for name, _ in mall.MALL_TABLES:
		raw = read(name)
		if raw is None:
			raise ValueError(f"the tree has no {name} to list the later goods in")
		write_atomically(safe_target(output / TEXT_ROOT, name), text_tables.append_rows(raw, rows[name]))
	summary.update({"enabled": True, "format": FORMAT, "laterArchiveHashes": identities})
	write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode("utf-8"))
	return summary
