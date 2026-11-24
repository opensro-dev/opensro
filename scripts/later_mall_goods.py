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
	- a scrap's rental term (RENT_ITEM_*, v1.696's refrentitem) resolves
	  to a period in that table and names the very item the scrap sells:
	  the server owns the rental clock and the item's expiry (issue
	  #4240), and the step writes the term's row with the good so the
	  built tree carries it. A term with no row, or a fixed window
	  instead of a period, stays unlisted; a term naming another item
	  is the native load's fatal, so the build refuses it here.

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
RENT_TABLE = "refrentitem.txt"
# A refrentitem row: its code, the item it rents and its period in
# minutes (0 with a fixed begin/end window instead, which no mall good
# names; the server port takes periods only).
RENT_CODE, RENT_ITEM_ID, RENT_MINUTES = 1, 2, 9
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
# rent_verdict
#
# What one scrap's rent term resolves to: None with its refrentitem
# row for a sellable period, or the reason the good carrying it stays
# out. The native load fatals when a rent term names another item than
# the one being granted (_RefRentItem lookup, sub_6a61b0); that is a
# build error, not a listing choice, so it raises.
# ================
def rent_verdict(term, scrap, rent_rows, item_by_id):
	rent_row = rent_rows.get(term)
	if rent_row is None:
		return "rent term without a row", None
	if len(rent_row) <= RENT_MINUTES or not rent_row[RENT_MINUTES].isdigit() or int(rent_row[RENT_MINUTES]) == 0:
		return "rental window", None
	if item_by_id.get(rent_row[RENT_ITEM_ID]) != scrap[SCRAP_ITEM]:
		raise ValueError(
			f"rent term {term} names item {rent_row[RENT_ITEM_ID]}, "
			f"not the {scrap[SCRAP_ITEM]} its scrap sells")
	return None, rent_row


# ================
# unlisted_reason
#
# Why one later good stays out, or None when it lists.
# ================
def unlisted_reason(tab, package, later_index, tree):
	package_row, scraps, prices, rent_rows, item_by_id = later_index
	if not tab.startswith(MALL_TAB):
		return "second-currency shop " + tab
	if tab not in tree.shop_tabs:
		return "no tab " + tab
	if package not in package_row or not prices.get(package):
		return "no package or price"
	rows = scraps.get(package, [])
	if not rows:
		return "no scrap"
	for row in rows:
		if len(row) <= SCRAP_RENT or row[SCRAP_RENT] in (NO_RENT, ""):
			continue
		reason, _ = rent_verdict(row[SCRAP_RENT], row, rent_rows, item_by_id)
		if reason is not None:
			return reason
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
# in each tab's later slot order after the tab's present slots, the
# refrentitem rows of the listed goods' rental terms, and the report
# of the goods left out. A package identity the tree already uses
# moves past every identity either client holds. rent_rows and
# item_by_id read the later refrentitem and itemdata tables; without
# them every rental stays out, as before the rental clock.
# ================
def goods_rows(later, tree, rent_rows=None, item_by_id=None):
	rent_rows = rent_rows or {}
	item_by_id = item_by_id or {}
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
	rows[RENT_TABLE] = []
	listed, unlisted = [], collections.Counter()
	rent_terms, rentals = set(), 0
	for row in sold:
		tab, package = row[GOODS_TAB], row[GOODS_PACKAGE]
		if package in listed:
			continue
		reason = unlisted_reason(tab, package, (package_row, scraps, prices, rent_rows, item_by_id), tree)
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
		terms = [scrap[SCRAP_RENT] for scrap in scraps[package]
			if len(scrap) > SCRAP_RENT and scrap[SCRAP_RENT] not in (NO_RENT, "")]
		if terms:
			rentals += 1
		for term in terms:
			if term not in rent_terms:
				rent_terms.add(term)
				rows[RENT_TABLE].append(rent_rows[term])
	# Goods an earlier step already staged (the avatar import lists the
	# avatars it carries before this step runs) sell through the same
	# goods table, so their rent rows must ship too: a staged rental
	# without its row stops the mall load, the runtime shape of the
	# native refusal. A term the shipped table cannot sell unstages
	# the good instead (the summary names it for add_goods to strip).
	staged_rentals, unstage = 0, set()
	for package in sorted(have):
		package_scraps = scraps.get(package, [])
		terms = sorted({row[SCRAP_RENT] for row in package_scraps
			if len(row) > SCRAP_RENT and row[SCRAP_RENT] not in (NO_RENT, "")})
		if not terms:
			continue
		for term in terms:
			scrap = next(row for row in package_scraps if len(row) > SCRAP_RENT and row[SCRAP_RENT] == term)
			reason, rent_row = rent_verdict(term, scrap, rent_rows, item_by_id)
			if reason is not None:
				unlisted[reason.split(":")[0]] += 1
				unstage.add(package)
				break
			if term not in rent_terms:
				rent_terms.add(term)
				rows[RENT_TABLE].append(rent_row)
		else:
			staged_rentals += 1
	for tab, slots in next_slot.items():
		if slots > mall.MALL_SLOTS:
			raise ValueError(f"{tab} would hold {slots} goods; the mall's one-byte slot holds {mall.MALL_SLOTS}")
	return rows, {"packages": listed, "listed": len(listed), "rentals": rentals + staged_rentals,
		"rentTerms": sorted(rent_terms), "unstage": sorted(unstage),
		"unlisted": dict(sorted(unlisted.items()))}


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
# strip_rent
#
# Drops the refrentitem rows a previous run wrote, by rent code, the
# strip_mall posture for the step's own table: the native tree has no
# refrentitem.txt, so a file the terms leave empty is removed again.
# ================
def strip_rent(output, terms):
	if not terms:
		return
	path = safe_target(output / TEXT_ROOT, RENT_TABLE)
	if not path.exists():
		return
	lines = tables.decode_table(path.read_bytes()).splitlines()
	kept = [line for line in lines if not (len(line.split("\t")) > RENT_CODE and line.split("\t")[RENT_CODE] in terms)]
	if len(kept) == len(lines):
		return
	if not any(line.strip() and not line.startswith("//") for line in kept):
		path.unlink()
		return
	write_atomically(path, b"\xff\xfe" + ("\r\n".join(kept) + "\r\n").encode("utf-16-le"))


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
		previous = json.loads(marker.read_text(encoding="utf-8"))
		mall.strip_mall(output, set(previous.get("packages", [])))
		strip_rent(output, set(previous.get("rentTerms", [])))
		marker.unlink()
	if not avatars.source_present(later_root, avatars.V1696_SOURCE):
		return {"enabled": False, "skipped": "no v1.696 client"}
	tables.require_vsro_pin(later_root, tuple(avatars.FOLDERS), avatars.source_pins(avatars.V1696_SOURCE))
	read = tree_reader(native, output)
	with ExitStack() as stack:
		archives, identities = avatars.open_archives(stack, later_root, avatars.V1696_SOURCE)
		media, index = archives["Media"]
		later = {name: avatars.later_table_rows(media, index, name) for name, _ in mall.MALL_TABLES}
		rent_rows = {row[RENT_CODE]: row for row in avatars.later_table_rows(media, index, RENT_TABLE)
			if row and row[0] == mall.SERVICE_ON and len(row) > RENT_CODE}
		item_by_id = {row[1]: row[2] for row in avatars.later_table_rows(media, index, "itemdata.txt") if len(row) > 2}
	rows, summary = goods_rows(later, read_tree(read), rent_rows, item_by_id)
	if summary.get("unstage"):
		mall.strip_mall(output, set(summary["unstage"]))
	for name, _ in mall.MALL_TABLES:
		raw = read(name)
		if raw is None:
			raise ValueError(f"the tree has no {name} to list the later goods in")
		write_atomically(safe_target(output / TEXT_ROOT, name), text_tables.append_rows(raw, rows[name]))
	if rows[RENT_TABLE]:
		write_atomically(safe_target(output / TEXT_ROOT, RENT_TABLE),
			text_tables.append_rows(read(RENT_TABLE) or b"", rows[RENT_TABLE]))
	summary.update({"enabled": True, "format": FORMAT, "laterArchiveHashes": identities})
	write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode("utf-8"))
	return summary
