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
	  built tree carries it, its rented item renumbered to the tree's
	  own identity of that item: the later client numbers its items
	  its own way, and a row naming the later id rents another item
	  or none in the tree. A term with no row, or a fixed window
	  instead of a period, stays unlisted; a term naming another item
	  is the native load's fatal, so the build refuses it here.

One later tab is remapped, not carried: v1.696 sells its miscellaneous
alchemy goods in MALL_ARCHEMY_ETC, a tab v1.150's mall does not have.
Per the owner's rule for the later goods (2026-11-25: every new v1.696
good goes to the normal Silk Mall, no new tab is carried for one tab's
goods), those goods are placed in MALL_ARCHEMY_ATHANASIA, the last tab
of the same alchemy group (MALL_ARCHEMY_GROUP1: Astral, Athanasia, then
ETC in the later client). A remapped good still lists only when every
item its scraps name stands in the tree; today none of the tab's four
families is served - the degree-3 Destruction Rondo (3/3/11/9, the
dissolve lane consumes the 3/3/11/6 rondo), the 12th-grade Magic Proof
Stones (3/3/10/8, no native family), the 11th-grade Special Awaken
Stones (3/3/11/15, no awakening system here) and the trade seals and
statement (3/3/9/0 and 3/3/11/12, no job-craft exchange here) - so the
tab's goods stay unlisted on the items, each family tracked by its own
issue (#4573 proof stones, #4574 awaken stones, #4575 trade seals,
#4576 the degree-3 rondo), and list here the day its item import lands.

Not listed: the PRE_MALL_ tabs, v1.696's second-currency shop of ITEM_PRE_
copies (no currency of that kind exists here). The Devil's Spirit's two
tabs the tree lacks are carried over from the later client when one of
their goods lists (their tab rows and tab labels below), so the family
sells in the ordinary mall at its later prices (#4240 part 4); the
layout's Premium > Gears face keeps standing on the same packages
(mallcategories.go nasrunGoods). Every good sells at its v1.696 price, which continues
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
TAB_LABEL = 5
TEXT_TABLE = "textuisystem.txt"
# The Devil's Spirit's tabs (#4240 part 4): the tree has neither, so a
# good in one lists only when the step carries the tab over from the
# later client, as mall_avatar_import carries the attachment tab.
NASRUN_TABS = ("MALL_PREMIUM_NASRUN", "MALL_PREMIUM_NASRUN_ETC")
GOODS_TAB, GOODS_PACKAGE, GOODS_SLOT = 2, 3, 4
PACKAGE_ID, PACKAGE_CODE = 2, 3
SCRAP_PACKAGE, SCRAP_ITEM = 2, 3
ITEM_ID, ITEM_CODE, CHARACTER_CODE = 1, 2, 2
MALL_TAB = "MALL_"
# Later tabs the tree lacks that are placed in an existing tab rather
# than carried over (the banner above): the miscellaneous alchemy tab
# lands in the last tab of its own alchemy group. A PRE_MALL_ tab is
# never remapped - it stays the second-currency shop.
TAB_REMAP = {"MALL_ARCHEMY_ETC": "MALL_ARCHEMY_ATHANASIA"}
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
# sharded_rows
#
# The rows of one sharded table (itemdata, characterdata): its index
# names the shard files, each read from the tree.
# ================
def sharded_rows(read, index_name):
	raw = read(index_name)
	if raw is None:
		return
	for row in tables.table_rows(raw):
		if not row or not row[0].strip():
			continue
		shard = read(sro_pk2.fold_ascii(row[0].strip()))
		if shard is not None:
			yield from tables.table_rows(shard)


# ================
# sharded_codes
#
# The codenames of one sharded table.
# ================
def sharded_codes(read, index_name, column):
	return {cells[column] for cells in sharded_rows(read, index_name) if len(cells) > column}


# ================
# sharded_ids
#
# One sharded table's identities by codename: the id the tree knows
# each row by, which the server and the wire key on.
# ================
def sharded_ids(read, index_name, code_column, id_column):
	return {cells[code_column]: cells[id_column] for cells in sharded_rows(read, index_name)
		if len(cells) > max(code_column, id_column)}


# ================
# TreeView
#
# What the selection reads of the built tree: its item identities by
# codename, its creature codenames, its mall tables and its shop tab
# codes.
# ================
class TreeView:
	def __init__(self, item_ids, creature_codes, mall_rows, shop_tabs, carry_tabs=frozenset()):
		self.item_ids, self.creature_codes = item_ids, creature_codes
		self.mall_rows, self.shop_tabs = mall_rows, shop_tabs
		# Tabs the tree lacks that the step may carry over from the
		# later client (add_goods names the Devil's Spirit's): a good
		# in one lists, and the step writes the tab it landed in.
		self.carry_tabs = carry_tabs


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
# built_rent_row
#
# The term's later row as the tree ships it: its rented-item cell is the
# tree's identity of the scrap's item. The later row names the later
# client's id (28268 for the Winter attachment the tree knows as 29411),
# and the server marks the reference that cell names as a rental
# (itemrefs_rent.go): left as it was, 150 of r204l's 252 rows marked an
# absent or a different item, and a rented good's rows desynced every
# packet that carried their deadline.
# ================
def built_rent_row(rent_row, item_code, tree):
	item_id = tree.item_ids.get(item_code)
	if item_id is None:
		raise ValueError(f"rent term {rent_row[RENT_CODE]} rents {item_code}, which the tree does not hold")
	row = list(rent_row)
	row[RENT_ITEM_ID] = item_id
	return row


# ================
# unlisted_reason
#
# Why one later good stays out, or None when it lists.
# ================
def unlisted_reason(tab, package, later_index, tree):
	package_row, scraps, prices, rent_rows, item_by_id = later_index
	if not tab.startswith(MALL_TAB):
		return "second-currency shop " + tab
	if tab not in tree.shop_tabs and tab not in tree.carry_tabs:
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
	absent = [code for code in held if code not in tree.item_ids]
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
	packaged = {row[PACKAGE_CODE] for row in current["refpackageitem.txt"] if len(row) > PACKAGE_CODE}
	have = packaged | {row[GOODS_PACKAGE] for row in current["refshopgoods.txt"] if len(row) > GOODS_PACKAGE}
	# A package a gold shop of the tree sells, but no mall tab (the
	# degree 10-11 Immortal and Astral stones, Alexandria's accessory
	# goods), joins the mall on its own package: only the goods row and
	# the mall prices it lacks are added (#4658). A package no shop
	# lists (the Kangaroo and Pegasus scrolls) is the server's mall
	# category layout's to sell (commerce/mallcategories.go), never here.
	in_mall = {row[GOODS_PACKAGE] for row in current["refshopgoods.txt"]
		if len(row) > GOODS_PACKAGE and row[GOODS_TAB].startswith(MALL_TAB)}
	in_shop = {row[GOODS_PACKAGE] for row in current["refshopgoods.txt"] if len(row) > GOODS_PACKAGE}
	joinable = (packaged & in_shop) - in_mall
	tree_currencies = collections.defaultdict(set)
	for row in current["refpricepolicyofitem.txt"]:
		if len(row) > 3:
			tree_currencies[row[2]].add(row[3])
	used_ids = {int(row[PACKAGE_ID]) for row in current["refpackageitem.txt"] if len(row) > PACKAGE_ID and row[PACKAGE_ID].isdigit()}
	next_id = max(used_ids | {int(row[PACKAGE_ID]) for row in package_row.values() if row[PACKAGE_ID].isdigit()}, default=0) + 1
	next_slot = collections.Counter()
	for row in current["refshopgoods.txt"]:
		if len(row) > GOODS_SLOT and row[GOODS_SLOT].isdigit():
			next_slot[row[GOODS_TAB]] = max(next_slot[row[GOODS_TAB]], int(row[GOODS_SLOT]) + 1)
	sold = sorted((row for row in later["refshopgoods.txt"] if row and row[0] == mall.SERVICE_ON and len(row) > GOODS_SLOT
		and row[GOODS_TAB].startswith(("MALL_", "PRE_MALL_")) and (row[GOODS_PACKAGE] not in have or row[GOODS_PACKAGE] in joinable)),
		key=lambda row: (row[GOODS_TAB], int(row[GOODS_SLOT]) if row[GOODS_SLOT].isdigit() else 0))
	rows = {name: [] for name, _ in mall.MALL_TABLES}
	rows[RENT_TABLE] = []
	listed, unlisted = [], collections.Counter()
	rent_terms, rentals = set(), 0
	remapped, joined = {}, {}
	for row in sold:
		package = row[GOODS_PACKAGE]
		tab = TAB_REMAP.get(row[GOODS_TAB], row[GOODS_TAB])
		if tab != row[GOODS_TAB]:
			remapped[package] = tab
		if package in listed:
			continue
		reason = unlisted_reason(tab, package, (package_row, scraps, prices, rent_rows, item_by_id), tree)
		if reason is not None:
			unlisted[reason.split(":")[0]] += 1
			continue
		if package in joinable:
			added = [price for price in prices[package] if price[3] not in tree_currencies[package]]
			if not added:
				unlisted["no mall price the package lacks"] += 1
				continue
			rows["refpricepolicyofitem.txt"].extend(mall.converted_price(price) for price in added)
			rows["refshopgoods.txt"].append(mall.goods_row(tab, package, next_slot[tab]))
			joined[package] = sorted(price[3] for price in added)
			next_slot[tab] += 1
			listed.append(package)
			continue
		package_id = None
		if int(package_row[package][PACKAGE_ID]) in used_ids:
			package_id, next_id = next_id, next_id + 1
		used_ids.add(int(package_id if package_id is not None else package_row[package][PACKAGE_ID]))
		mall.copy_later_package(rows, later, mall.LaterPackage(package_row[package], prices[package], tab,
			next_slot[tab], package_id=package_id))
		next_slot[tab] += 1
		listed.append(package)
		rented = [scrap for scrap in scraps[package]
			if len(scrap) > SCRAP_RENT and scrap[SCRAP_RENT] not in (NO_RENT, "")]
		if rented:
			rentals += 1
		for scrap in rented:
			term = scrap[SCRAP_RENT]
			if term not in rent_terms:
				rent_terms.add(term)
				rows[RENT_TABLE].append(built_rent_row(rent_rows[term], scrap[SCRAP_ITEM], tree))
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
				rows[RENT_TABLE].append(built_rent_row(rent_row, scrap[SCRAP_ITEM], tree))
		else:
			staged_rentals += 1
	for tab, slots in next_slot.items():
		if slots > mall.MALL_SLOTS:
			raise ValueError(f"{tab} would hold {slots} goods; the mall's one-byte slot holds {mall.MALL_SLOTS}")
	# A carried tab the step must write: one a listed good landed in
	# that the tree itself does not have.
	carried = sorted({row[GOODS_TAB] for row in rows["refshopgoods.txt"]} & set(tree.carry_tabs))
	return rows, {"packages": [package for package in listed if package not in joined], "joined": joined, "listed": len(listed), "rentals": rentals + staged_rentals,
		"rentTerms": sorted(rent_terms), "unstage": sorted(unstage), "tabs": carried,
		"remapped": {package: remapped[package] for package in listed if package in remapped},
		"unlisted": dict(sorted(unlisted.items()))}


# ================
# strip_joined
#
# Undoes a previous run's joined listings: the mall goods rows and the
# mall price rows (by currency) added to packages the tree already held.
# The package and its other rows, such as a gold shop's, stay.
# ================
def strip_joined(output, joined):
	if not joined:
		return
	for name in ("refshopgoods.txt", "refpricepolicyofitem.txt"):
		path = safe_target(output / TEXT_ROOT, name)
		if not path.exists():
			continue
		lines = tables.decode_table(path.read_bytes()).splitlines()
		kept = [line for line in lines if not joined_row(name, line.split("\t"), joined)]
		if len(kept) != len(lines):
			write_atomically(path, b"\xff\xfe" + ("\r\n".join(kept) + "\r\n").encode("utf-16-le"))


# ================
# joined_row
#
# Whether a goods or price row is one a joined listing added.
# ================
def joined_row(name, cells, joined):
	if name == "refshopgoods.txt":
		return len(cells) > GOODS_PACKAGE and cells[GOODS_PACKAGE] in joined and cells[GOODS_TAB].startswith(MALL_TAB)
	return len(cells) > 3 and cells[3] in joined.get(cells[2], ())


# ================
# carried_tab_row
#
# The later client's refshoptab row for tab, as the step carries it
# over (mall_avatar_import.attach_tab_row does the same for the
# attachment tab): its group, label and identity, the identity moved
# past the tree's when it is taken. Appended last, it shifts no other
# tab's index (mallcatalog.go numbers tabs by row order within a
# shop). Refused when the later client has no such tab either.
# ================
def carried_tab_row(current, later, tab):
	authored = [row for row in later if len(row) > SHOP_TAB_CODE and row[0] == mall.SERVICE_ON and row[SHOP_TAB_CODE] == tab]
	if not authored:
		raise ValueError(f"the later client has no {tab} tab to carry over")
	row = list(authored[0])
	row[1] = mall.NATIVE_SOURCE
	taken = {int(cells[2]) for cells in current if len(cells) > 2 and cells[2].isdigit()}
	identity = int(row[2])
	while identity in taken:
		identity += 1
	row[2] = str(identity)
	return row


# ================
# later_label_texts
#
# The later client's own English for the given textuisystem symbols,
# read raw from its shards (the converted row stream drops a row of
# the wrong width, #4328): key at column 2, English at column 9.
# ================
def later_label_texts(media, index, symbols):
	found = {}
	if not symbols:
		return found
	shards = [row[0].strip() for row in tables.table_rows(tables.table_text(media, index, TEXT_TABLE))
		if row and row[0].strip()]
	for shard in shards:
		for row in tables.table_rows(tables.table_text(media, index, shard)):
			if len(row) > 9 and row[2] in symbols and row[2] not in found:
				found[row[2]] = row[9]
		if len(found) == len(symbols):
			break
	return found


# ================
# tab_label_row
#
# One carried tab's textuisystem row in the built layout, the later
# client's English in every text cell (mall_avatar_import's board
# labels take the same shape).
# ================
def tab_label_row(symbol, text):
	return ["1", symbol, text, "", "", text, text, text, text, text]


# ================
# read_tree
#
# The TreeView of the built tree.
# ================
def read_tree(read):
	mall_rows = {name: tables.table_rows(read(name) or b"") for name, _ in mall.MALL_TABLES}
	tabs = {row[SHOP_TAB_CODE] for row in tables.table_rows(read(SHOP_TAB_TABLE) or b"") if len(row) > SHOP_TAB_CODE}
	return TreeView(sharded_ids(read, "itemdata.txt", ITEM_CODE, ITEM_ID), sharded_codes(read, "characterdata.txt", CHARACTER_CODE),
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
		strip_joined(output, previous.get("joined", {}))
		strip_rent(output, set(previous.get("rentTerms", [])))
		avatars.strip_tabs(output, set(previous.get("tabs", [])))
		avatars.strip_text_rows(output, set(previous.get("tabLabels", [])))
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
		later_tabs = avatars.later_table_rows(media, index, SHOP_TAB_TABLE)
		label_texts = later_label_texts(media, index,
			{row[TAB_LABEL] for row in later_tabs if len(row) > TAB_LABEL and row[SHOP_TAB_CODE] in NASRUN_TABS})
	tree = read_tree(read)
	# The Spirit's tabs may be carried when the later client has them
	# and the tree does not; a tab is written only if a good lands in
	# it (goods_rows reports those in the summary's tabs).
	tree.carry_tabs = {code for code in NASRUN_TABS if code not in tree.shop_tabs
		and any(len(row) > SHOP_TAB_CODE and row[0] == mall.SERVICE_ON and row[SHOP_TAB_CODE] == code for row in later_tabs)}
	rows, summary = goods_rows(later, tree, rent_rows, item_by_id)
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
	summary["tabLabels"] = []
	if summary.get("tabs"):
		current = tables.table_rows(read(SHOP_TAB_TABLE) or b"")
		carried = []
		for code in summary["tabs"]:
			row = carried_tab_row(current, later_tabs, code)
			carried.append(row)
			current.append(row)
		write_atomically(safe_target(output / TEXT_ROOT, SHOP_TAB_TABLE),
			text_tables.append_rows(read(SHOP_TAB_TABLE), carried))
		labels = [tab_label_row(row[TAB_LABEL], label_texts[row[TAB_LABEL]]) for row in carried
			if len(row) > TAB_LABEL and row[TAB_LABEL] in label_texts]
		if labels:
			write_atomically(safe_target(output / TEXT_ROOT, TEXT_TABLE),
				text_tables.append_rows(read(TEXT_TABLE), labels))
			summary["tabLabels"] = [row[1] for row in labels]
	summary.update({"enabled": True, "format": FORMAT, "laterArchiveHashes": identities})
	write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode("utf-8"))
	return summary
