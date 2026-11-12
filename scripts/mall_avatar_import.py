"""
===========================================================================

mall_avatar_import.py - stage the later client's mall avatars (outfits, hats, attachments)

Port-only, not native. On unless SRO_MALL_AVATARS is 0/off/false/no: the
owner approved these cosmetics behind a flag that defaults on (2026-11-04).
Called by the Alexandria builder after the cap-110 item import. The v1.150
client lacks about 150 mall avatar rows the later (VSRO) client has, among
them the Ottoman ("Turkey") set.

Selection is by item type and codename: a service-enabled avatar row
(type 3/1/13) named ITEM_MALL_AVATAR_*, not in the native tables, not a
test row. Event copies (ITEM_EVENT_*) share the models but belong to events
this port does not have, so they stay out. A row whose model or icon is
absent from both the later archives and the native tree, or whose name text
is missing, is skipped and reported, never half imported (the Nasrun mounts
are the known case).

A third source (owner ask #3254) reads the v2.355 export's avatars from
one optional pinned archive named by SRO_AVATARS_ZIP, packed by
pack_avatars.py and never committed: the game builds byte-identically
without it. Its rows take any codename (that client brands its sets), are
listed on authored mall boards instead of that client's own tabs, and the
archive's sha256 is pinned in avatars_archive_pins.json.

Rows keep their later-client identity when it is free in the native tables
and the character data; the few that clash take a fresh one above every
known identity, remembered in the marker so a rebuild never moves an item.
Native files win in resource lookup; resources the native tree lacks are
copied from the later archives, pinned by hash by the caller. Each avatar
the later client's Item Mall sold is listed in the matching mall tab at its
authored silk price; the rest are carried by GM grant only. Nothing here
adds a drop or an event reward. No retail table is committed to Git.

===========================================================================
"""
import argparse
import collections
from contextlib import ExitStack
import json
import mmap
import os
from pathlib import Path
import re

import alexandria_npcs as text_tables
import avatar_archive
import cap110_item_import as shared
import cap110_items as items
import import_vsro_alexandria as tables
import sro_pk2
from prepare_client_resources import safe_target, write_atomically
from sro_paths import EXTRACTED_ROOT, REPO_ROOT

FLAG = "SRO_MALL_AVATARS"
FLAG_OFF = ("0", "off", "false", "no")
REPORT_NAME = ".opensro-mall-avatars.json"
OUTPUT_SHARD = "itemdata_100000.txt"

# The later clients the avatars come from, in import order. Each owns one
# shard and one marker, so a source's off path removes only its own rows.
# The v1.696 client (owner, 2026-11-11: "we need all of them") adds the
# 264 avatars the VSRO client lacks; its pass runs second and treats every
# row already imported as existing, so a codename is imported once, from
# the earliest client that has it.
VSRO_SOURCE = {"name": "vsro", "shard": OUTPUT_SHARD, "report": REPORT_NAME, "pins": None}
V1696_PIN_FILE = Path(__file__).with_name("v1696_archive_pins.json")
V1696_ENV = "SRO_V1696_ROOT"
V1696_SOURCE = {"name": "v1696", "shard": "itemdata_100001.txt", "report": ".opensro-mall-avatars-v1696.json",
	"pins": V1696_PIN_FILE}
# The v2.355 export's avatars (owner ask #3254): one pinned archive the
# packer wrote outside the repository, named by SRO_AVATARS_ZIP. Unset or
# absent removes this source's import and changes nothing else. Its rows
# are listed on authored boards (below), not on a later client's tabs.
AVATARS_PIN_FILE = Path(__file__).with_name("avatars_archive_pins.json")
AVATARS_ENV = "SRO_AVATARS_ZIP"
AVATARS_PIN_FORMAT = avatar_archive.PIN_FORMAT
AVATARS_SOURCE = {"name": "avatars", "shard": "itemdata_100002.txt", "report": ".opensro-mall-avatars-archive.json",
	"pins": AVATARS_PIN_FILE, "zip": True, "boards": True}
SOURCES = (VSRO_SOURCE, V1696_SOURCE, AVATARS_SOURCE)
PIN_FORMAT = "opensro-vsro-archive-pins-v1"
FOLDERS = shared.FOLDERS
TEXT_ROOT = shared.TEXT_ROOT
CODE_PREFIX = "ITEM_MALL_AVATAR_"
EXCLUDED_WORDS = ("_TEST",)
AVATAR_TYPES = ["3", "1", "13"]
SERVICE_ON = "1"
FRESH_ID_BASE = 50000
HAT_SUFFIX = "_HAT"
# Rows whose model the v1.150 item-model builder refuses: their material has an
# animated or material-scoped environment map (characterMaterialState.mjs
# "needs runtime ownership"), and one such model aborts the whole asset
# build. Found by screening every imported avatar model's environment
# modifiers; revisit if that builder learns the modifier.
RENDER_UNSUPPORTED = {
	"ITEM_MALL_AVATAR_M_EARTHSPIRIT_HAT": "environment-mapped material",
	"ITEM_MALL_AVATAR_W_EARTHSPIRIT_HAT": "environment-mapped material",
}
# Package, scrap, price and goods tables with the column holding the package code.
MALL_TABLES = (
	("refpackageitem.txt", 3), ("refscrapofpackageitem.txt", 2),
	("refpricepolicyofitem.txt", 2), ("refshopgoods.txt", 3),
)
# Attachments take the later clients' own attachment tab (owner 2026-11-12,
# replacing the 2026-10-13 hat-tab rule once the hats and attachments
# together passed a tab's 256 slots). v1.150 ships the tab's label
# (UIIT_STT_SILKMALL_ATTACH) but not its refshoptab row, so the import
# carries the row over from the later client.
ATTACH_TAB = "MALL_AVATAR_ATTACH"
SHOP_TAB_TABLE = "refshoptab.txt"
SHOP_TAB_CODE = 3
MALL_TAB_FOR = {"MALL_AVATAR_DRESS": "MALL_AVATAR_DRESS", "MALL_AVATAR_HAT": "MALL_AVATAR_HAT", ATTACH_TAB: ATTACH_TAB}
NATIVE_SOURCE = "11"
# The silk price of every listed avatar is the v1.150 mall's own tier, not the
# later client's authored amount (owner, 2026-11-04: "same as the others"):
# the European wedding dress sells at 130 and its hat at 30, and the native
# Halloween attachment is priced 15 (mallextra.go carries the same figures).
SILK_CURRENCY = "2"
SILK_PRICE = {"dress": "130", "hat": "30", "attach": "15"}
HAT_CODE = re.compile(r"_HAT(_|$)")
ATTACH_CODE = re.compile(r"_ATTACH(_|$)")
PACKAGE_FLAG_COLUMN = 4
MALL_PRICE_AMOUNT = 5
# Port-only, not native (owner 2026-11-12: "we need it all"): an imported
# avatar the later client never packaged or sold is listed anyway, with
# the rows the server's authored mall goods carry (mallextra.go
# authorMallPackage): a package drawn as the item, one plain scrap, the
# native silk tier. Off lists only what the later mall sold.
LIST_ALL_FLAG = "SRO_MALL_AVATARS_LIST_ALL"
KIND_TAB = {"dress": "MALL_AVATAR_DRESS", "hat": "MALL_AVATAR_HAT", "attach": ATTACH_TAB}
PACKAGE_PREFIX = "PACKAGE_"
SCRAP_ZERO_COLUMNS = 16
SCRAP_SERIAL = 28
# A goods row's slot is one byte on the wire (mallcatalog.go MallAddress.Slot).
MALL_SLOTS = 256
# Port-only, not native (design comment on #3254): the archive source's
# avatars list on two authored boards in the avatar shop's group - custom
# sets, and event sets under their own board - one generated refshoptab
# row per page of MALL_SLOTS goods, page two and onward suffixed _2, _3.
# The client groups the pages by board code; without the archive neither
# board exists and the mall is exactly the native one.
BOARD_GROUP = "MALL_AVATAR_GROUP1"
BOARD_TABS = {"cu": "MALL_AVATAR_BOARD_CU", "ev": "MALL_AVATAR_BOARD_EV"}
BOARD_LABELS = {"cu": "UIIT_STT_SILKMALL_AVATAR_BOARD_CU", "ev": "UIIT_STT_SILKMALL_AVATAR_BOARD_EV"}
BOARD_LABEL_TEXT = {"cu": "Custom avatars", "ev": "Event avatars"}
EVENT_WORD = "EVENT"


# ================
# enabled
#
# Unlike the cap-110 switches this one defaults on: an unset or empty value
# is on, and only an explicit off value turns it off. An unreadable value is
# refused rather than guessed.
# ================
def enabled(env=os.environ, flag=FLAG):
	value = env.get(flag, "").strip().lower()
	if value in ("", "1", "on", "true", "yes"):
		return True
	if value in FLAG_OFF:
		return False
	raise ValueError(f"{flag} must be on or off, not {value!r}")


# ================
# candidate
#
# A later client's avatar row. The board source takes any codename: the
# v2.355 export brands its sets instead of using ITEM_MALL_AVATAR_*.
# ================
def candidate(row, any_prefix=False):
	code = row[items.CODENAME]
	return (
		row[items.SERVICE] == SERVICE_ON and row[items.TYPE_FIRST:items.TYPE_THIRD + 1] == AVATAR_TYPES
		and (any_prefix or code.startswith(CODE_PREFIX)) and not any(word in code for word in EXCLUDED_WORDS)
	)


# ================
# previous_targets
#
# Reads a marker's identity map. The first (Turkey-only) marker stored a
# bare number per code, which was the later identity; both forms are read.
# ================
def previous_targets(prior):
	result = {}
	for code, entry in prior.get("idMap", {}).items():
		result[code] = int(entry["target"]) if isinstance(entry, dict) else int(entry)
	return result


# ================
# select_avatars
#
# missing(row) returns the names of a row's absent resources. Identities:
# a retained one first, else the later one when free, else a fresh one.
# ================
def select_avatars(native_rows, later_rows, reserved_ids=(), previous=None, missing=lambda row: [], texts=None,
		any_prefix=False):
	native, later = items.index_items(native_rows), items.index_items(later_rows)
	taken = {int(row[items.IDENTITY]) for row in native_rows} | set(reserved_ids)
	previous = previous or {}
	chosen, skipped, existing = [], [], []
	for code, row in sorted(later.items()):
		if not candidate(row, any_prefix):
			continue
		if code in native:
			existing.append(code)
			continue
		absent = missing(row)
		if code in RENDER_UNSUPPORTED:
			absent = absent + [RENDER_UNSUPPORTED[code]]
		if texts is not None and row[items.NAME_KEY] not in texts:
			absent = absent + ["name text"]
		if absent:
			skipped.append({"code": code, "missing": absent})
			continue
		chosen.append(row)
	if not chosen:
		raise ValueError("no mall avatar rows to import")
	targets = {}
	for row in chosen:
		code = row[items.CODENAME]
		retained = previous.get(code)
		if retained is not None and retained not in taken:
			targets[code] = retained
			taken.add(retained)
	for row in chosen:
		code, source = row[items.CODENAME], int(row[items.IDENTITY])
		if code not in targets and source not in taken:
			targets[code] = source
			taken.add(source)
	fresh = max(FRESH_ID_BASE, max(taken) + 1)
	for row in chosen:
		code = row[items.CODENAME]
		if code not in targets:
			while fresh in taken:
				fresh += 1
			targets[code] = fresh
			taken.add(fresh)
	added = []
	for row in chosen:
		converted = list(row)
		converted[items.IDENTITY] = str(targets[row[items.CODENAME]])
		converted[items.PLACEHOLDER_NAME] = items.UNUSED
		added.append(converted)
	if len({int(row[items.IDENTITY]) for row in added}) != len(added) or any(not 0 < int(row[items.IDENTITY]) <= items.MAX_ID for row in added):
		raise ValueError("avatar identities collide or exceed the wire width")
	return added, {
		"added": len(added), "existingNative": existing, "skipped": skipped,
		"hats": sum(row[items.CODENAME].endswith(HAT_SUFFIX) for row in added),
		"idMap": {
			row[items.CODENAME]: {"source": int(later[row[items.CODENAME]][items.IDENTITY]), "target": int(row[items.IDENTITY])}
			for row in added
		},
		"preservedNativeRows": len(native_rows),
	}


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
		kept = [line for line in lines if not (len(line.split("\t")) > column and line.split("\t")[column] in packages)]
		if len(kept) != len(lines):
			write_atomically(path, b"\xff\xfe" + ("\r\n".join(kept) + "\r\n").encode("utf-16-le"))


# ================
# avatar_kind
# ================
def avatar_kind(code):
	if ATTACH_CODE.search(code):
		return "attach"
	return "hat" if HAT_CODE.search(code) else "dress"


# ================
# mall_rows
#
# The rows that list the imported avatars in the Item Mall, for the avatars
# the later client's mall sold. The later price table carries one extra
# column before the amount, dropped to the native layout. The v1.150 mall
# has no attachment tab, so attachments take the hat tab (the owner's
# 2026-10-13 mall-extra rule). Silk amounts follow the native tier. Slots follow the later order, after the
# tab's present slots. Avatars with no package, no sale or no price stay
# unlisted and are reported.
# ================
def mall_rows(later, avatars, current, native_package_ids, list_all=False, ignore_packages=frozenset()):
	scrap_of = {row[3]: row[2] for row in later["refscrapofpackageitem.txt"] if row[0] == SERVICE_ON and len(row) >= 20}
	package_row = {row[3]: row for row in later["refpackageitem.txt"] if row[0] == SERVICE_ON and len(row) >= 9}
	sale = {row[3]: (row[2], int(row[4])) for row in later["refshopgoods.txt"] if row[0] == SERVICE_ON and len(row) >= 5}
	prices = collections.defaultdict(list)
	for row in later["refpricepolicyofitem.txt"]:
		if row[0] == SERVICE_ON and len(row) > MALL_PRICE_AMOUNT:
			prices[row[2]].append(row)
	# Slots follow the present goods, but a later source's stale goods
	# do not count: that source strips and re-lists them after this
	# pass, so appending past them walks the tab upward a rebuild at a
	# time until the one-byte slot overflows.
	next_slot = collections.Counter()
	for row in current["refshopgoods.txt"]:
		if len(row) >= 5 and row[2] in MALL_TAB_FOR.values() and row[3] not in ignore_packages:
			next_slot[row[2]] = max(next_slot[row[2]], int(row[4]) + 1)
	rows = {name: [] for name, _ in MALL_TABLES}
	packages, unlisted, authored = [], [], []
	ids = AuthoredIds(later, current, native_package_ids)
	order = sorted(avatars, key=lambda row: (
		unlisted_reason(row[items.CODENAME], scrap_of, package_row, sale, prices) is not None,
		sale.get(scrap_of.get(row[items.CODENAME]), ("", 0))[1], row[items.CODENAME],
	))
	for row in order:
		code = row[items.CODENAME]
		reason = unlisted_reason(code, scrap_of, package_row, sale, prices)
		if reason is not None and list_all:
			package = author_package(rows, row, ids)
			tab = KIND_TAB[avatar_kind(code)]
			rows["refshopgoods.txt"].append(["1", NATIVE_SOURCE, tab, package, str(next_slot[tab])] + ["-1", items.UNUSED] * 4)
			next_slot[tab] += 1
			packages.append(package)
			authored.append(code)
			continue
		if reason is not None:
			unlisted.append({"code": code, "reason": reason})
			continue
		package = scrap_of[code]
		tab = MALL_TAB_FOR[sale[package][0]]
		if int(package_row[package][2]) in native_package_ids:
			raise ValueError(f"package identity clash: {package}")
		package_cells = list(package_row[package])
		package_cells[1], package_cells[PACKAGE_FLAG_COLUMN] = NATIVE_SOURCE, "0"
		rows["refpackageitem.txt"].append(package_cells)
		for scrap in later["refscrapofpackageitem.txt"]:
			if scrap[0] == SERVICE_ON and len(scrap) >= 20 and scrap[2] == package:
				rows["refscrapofpackageitem.txt"].append(scrap[:1] + [NATIVE_SOURCE] + scrap[2:])
		for price in prices[package]:
			converted = price[:1] + [NATIVE_SOURCE] + price[2:MALL_PRICE_AMOUNT - 1] + price[MALL_PRICE_AMOUNT:]
			if converted[3] == SILK_CURRENCY:
				converted[4] = SILK_PRICE[avatar_kind(code)]
			rows["refpricepolicyofitem.txt"].append(converted)
		rows["refshopgoods.txt"].append(["1", NATIVE_SOURCE, tab, package, str(next_slot[tab])] + ["-1", items.UNUSED] * 4)
		next_slot[tab] += 1
		packages.append(package)
	for tab, slots in next_slot.items():
		if slots > MALL_SLOTS:
			raise ValueError(f"{tab} would hold {slots} goods; the mall's one-byte slot holds {MALL_SLOTS}")
	return rows, {"packages": packages, "listed": len(packages), "authored": authored, "unlisted": unlisted}


# ================
# attach_tab_row
#
# The attachment tab's refshoptab row as the later client authors it (its
# group, label and identity), or None when the tree already has the tab.
# The later identity is kept unless the tree uses it, then the next free
# one. Appended last, it is the avatar shop's fourth tab, as in the later
# clients, and shifts no other tab's index (mallcatalog.go numbers tabs
# by row order within a shop).
# ================
def attach_tab_row(current, later):
	if any(len(row) > SHOP_TAB_CODE and row[SHOP_TAB_CODE] == ATTACH_TAB for row in current):
		return None
	authored = [row for row in later if len(row) > SHOP_TAB_CODE and row[0] == SERVICE_ON and row[SHOP_TAB_CODE] == ATTACH_TAB]
	if not authored:
		raise ValueError(f"the later client has no {ATTACH_TAB} tab to carry over")
	row = list(authored[0])
	row[1] = NATIVE_SOURCE
	taken = {int(cells[2]) for cells in current if len(cells) > 2 and cells[2].isdigit()}
	identity = int(row[2])
	while identity in taken:
		identity += 1
	row[2] = str(identity)
	return row


# ================
# strip_tabs
#
# Drops the refshoptab rows a previous run added, by tab code.
# ================
def strip_tabs(output, codes):
	if not codes:
		return
	path = safe_target(output / TEXT_ROOT, SHOP_TAB_TABLE)
	if not path.exists():
		return
	lines = tables.decode_table(path.read_bytes()).splitlines()
	kept = [line for line in lines if not (len(line.split("\t")) > SHOP_TAB_CODE and line.split("\t")[SHOP_TAB_CODE] in codes)]
	if len(kept) != len(lines):
		write_atomically(path, b"\xff\xfe" + ("\r\n".join(kept) + "\r\n").encode("utf-16-le"))


# ================
# unlisted_reason
#
# Why the later client's own mall rows cannot list an avatar, or None when
# they can: no package, a package it never sold, a tab the v1.150 mall
# lacks, or no price.
# ================
def unlisted_reason(code, scrap_of, package_row, sale, prices):
	package = scrap_of.get(code)
	if package is None or package not in package_row:
		return "no package"
	if package not in sale:
		return "not sold in the later mall"
	if MALL_TAB_FOR.get(sale[package][0]) is None:
		return "tab " + sale[package][0]
	if not prices[package]:
		return "no price"
	return None


# ================
# AuthoredIds
#
# The package identities and scrap serials an authored listing takes: past
# every one the native tree and the later client hold, so no authored row
# can collide with a shipped one or with a later package imported beside it.
# ================
class AuthoredIds:
	def __init__(self, later, current, native_package_ids):
		package_ids = set(native_package_ids)
		serials = set()
		for tables_ in (later, current):
			for row in tables_["refpackageitem.txt"]:
				if len(row) > 3 and row[2].isdigit():
					package_ids.add(int(row[2]))
			for row in tables_["refscrapofpackageitem.txt"]:
				if len(row) > SCRAP_SERIAL and row[SCRAP_SERIAL].isdigit():
					serials.add(int(row[SCRAP_SERIAL]))
		# Only the built tree's names can clash: a later package this import
		# does not carry over (one it never sold) never reaches the output.
		names = {row[3] for row in current["refpackageitem.txt"] if len(row) > 3}
		self.package_names = names
		self.next_package = max(package_ids, default=0) + 1
		self.next_serial = max(serials, default=0) + 1


# ================
# author_package
#
# The package, scrap and silk price v1.150 would carry for a one-avatar
# package it never shipped, in the later client's column layout: named,
# described and drawn as the item itself (mallextra.go authorMallPackage
# writes the same rows for an operator-listed item).
# ================
def author_package(rows, row, ids):
	code = row[items.CODENAME]
	package = PACKAGE_PREFIX + code
	if package in ids.package_names:
		raise ValueError(f"authored package {package} already exists")
	ids.package_names.add(package)
	none = ["-1", items.UNUSED] * 4
	rows["refpackageitem.txt"].append(
		["1", NATIVE_SOURCE, str(ids.next_package), package, "0", "EXPAND_TERM_ALL",
		row[items.NAME_KEY], row[items.DESCRIPTION_KEY], row[items.ICON]] + none
	)
	rows["refscrapofpackageitem.txt"].append(
		["1", NATIVE_SOURCE, package, code] + ["0"] * SCRAP_ZERO_COLUMNS + none + [str(ids.next_serial)]
	)
	rows["refpricepolicyofitem.txt"].append(["1", NATIVE_SOURCE, package, SILK_CURRENCY, SILK_PRICE[avatar_kind(code)]] + none)
	ids.next_package += 1
	ids.next_serial += 1
	return package


# ================
# board_of
#
# The board an archive avatar lists on: event sets keep a board of their
# own, every other set is a custom set (design comment on #3254).
# ================
def board_of(code):
	parts = code.split("_")
	return "ev" if len(parts) > 1 and parts[1] == EVENT_WORD else "cu"


# ================
# board_rows
#
# The mall rows listing the archive avatars on their boards: every avatar
# gets an authored package at the native silk tier for its kind, and the
# goods fill one board page of MALL_SLOTS at a time in codename order, so
# no tab ever passes the wire's one-byte slot. Returns the rows, the
# marker summary and the (board, tab code) pages in listing order.
# ================
def board_rows(avatars, current, native_package_ids):
	rows = {name: [] for name, _ in MALL_TABLES}
	ids = AuthoredIds({name: [] for name, _ in MALL_TABLES}, current, native_package_ids)
	per_board = collections.defaultdict(list)
	for row in sorted(avatars, key=lambda row: row[items.CODENAME]):
		per_board[board_of(row[items.CODENAME])].append(row)
	packages, pages = [], []
	for board in sorted(per_board):
		members = per_board[board]
		for page, start in enumerate(range(0, len(members), MALL_SLOTS)):
			tab = BOARD_TABS[board] if page == 0 else f"{BOARD_TABS[board]}_{page + 1}"
			pages.append((board, tab))
			for slot, row in enumerate(members[start:start + MALL_SLOTS]):
				package = author_package(rows, row, ids)
				rows["refshopgoods.txt"].append(["1", NATIVE_SOURCE, tab, package, str(slot)] + ["-1", items.UNUSED] * 4)
				packages.append(package)
	summary = {
		"packages": packages, "listed": len(packages),
		"authored": [row[items.CODENAME] for row in avatars], "unlisted": [],
		"tabs": [tab for _, tab in pages],
	}
	return rows, summary, pages


# ================
# board_tab_rows
#
# One refshoptab row per board page, in the avatar shop's group (the
# dress tab's own group), identities past every taken one. Appended
# last, the pages shift no other tab's index.
# ================
def board_tab_rows(current, pages):
	group = next((row[4] for row in current if len(row) > 4 and row[SHOP_TAB_CODE] == "MALL_AVATAR_DRESS"), BOARD_GROUP)
	taken = {int(cells[2]) for cells in current if len(cells) > 2 and cells[2].isdigit()}
	identity = max(taken, default=0) + 1
	rows = []
	for board, code in pages:
		rows.append(["1", NATIVE_SOURCE, str(identity), code, group, BOARD_LABELS[board]])
		identity += 1
	return rows


# ================
# board_label_rows
#
# The textuisystem rows naming the board tabs, one per board (its pages
# share the label). Cells without a translation take the English text,
# as the VSRO client authors its own rows.
# ================
def board_label_rows(boards):
	return [
		["1", BOARD_LABELS[board], BOARD_LABEL_TEXT[board], "", "", BOARD_LABEL_TEXT[board], BOARD_LABEL_TEXT[board],
			BOARD_LABEL_TEXT[board], BOARD_LABEL_TEXT[board], BOARD_LABEL_TEXT[board]]
		for board in boards
	]


# ================
# strip_text_rows
#
# Drops the textuisystem rows a previous run added, by key.
# ================
def strip_text_rows(output, keys):
	if not keys:
		return
	path = safe_target(output / TEXT_ROOT, "textuisystem.txt")
	if not path.exists():
		return
	lines = tables.decode_table(path.read_bytes()).splitlines()
	kept = [line for line in lines if not (len(line.split("\t")) > 1 and line.split("\t")[1] in keys)]
	if len(kept) != len(lines):
		write_atomically(path, b"\xff\xfe" + ("\r\n".join(kept) + "\r\n").encode("utf-16-le"))


# ================
# remove_import
#
# Off (or a rebuild) drops the shard, its manifest line and its mall rows,
# so a reused variant cannot keep a previous on build.
# ================
def remove_import(output, marker, shard, manifest):
	if marker.exists():
		mall = json.loads(marker.read_text(encoding="utf-8")).get("mall", {})
		strip_mall(output, set(mall.get("packages", [])))
		strip_tabs(output, set(mall.get("tabs", [])))
		strip_text_rows(output, set(mall.get("tabLabels", [])))
	if shard.exists():
		shard.unlink()
	if manifest.exists():
		names = [row[0] for row in tables.table_rows(manifest.read_bytes()) if row]
		kept = [name for name in names if sro_pk2.fold_ascii(name) != shard.name]
		if len(kept) != len(names):
			write_atomically(manifest, b"\xff\xfe" + ("\r\n".join(kept) + "\r\n").encode("utf-16-le"))
	if marker.exists():
		marker.unlink()


# ================
# resource_check
#
# The row's worn model, drop model and icon must exist in the later archives
# or the native tree. Deeper dependencies (meshes, textures) are verified by
# copy_asset_closure afterwards, which fails the build if one is absent.
# ================
def resource_check(archives, native_paths):
	def has(archive, path):
		return path in archives[archive][1] or path in native_paths[archive]
	def missing(row):
		absent = []
		model = row[items.MODEL].split(",")[0].strip()
		if model in ("", items.UNUSED) or not has("Data", "res/" + shared.normalized_path(model)):
			absent.append("model")
		if row[items.DROP_MODEL] in ("", items.UNUSED) or not has("Data", "res/" + shared.normalized_path(row[items.DROP_MODEL])):
			absent.append("drop model")
		if row[items.ICON] in ("", items.UNUSED) or not has("Media", "icon/" + shared.normalized_path(row[items.ICON])):
			absent.append("icon")
		return absent
	return missing


# ================
# later_text_rows
#
# The rows of one later text table. The VSRO client keeps each table in
# one file; the v1.696 client keeps a short index of shard names
# (textdata_equip&skill_150.txt, ...) under the table's own name, as
# itemdata.txt indexes its shards. A table the client does not ship
# contributes no rows (v1.696 has no textdata_new.txt).
# ================
def later_text_rows(media, index, name):
	return [vsro_text_row(row) for row in later_table_rows(media, index, name)]


# ================
# later_table_rows
#
# The rows of one later-client table, [] when the client lacks it. v1.696
# shards its big tables: the named file then lists only `<stem>_<n>.txt`
# names, and the rows are those files' rows in the listed order.
# ================
def later_table_rows(media, index, name):
	if tables.TEXTDATA + name not in index:
		return []
	rows = tables.table_rows(tables.table_text(media, index, name))
	stem = sro_pk2.fold_ascii(name)[:-len(".txt")]
	shard = re.compile(re.escape(stem) + r"_\d+\.txt", re.IGNORECASE)
	listed = [row[0].strip() for row in rows if row and row[0].strip()]
	if not listed or not all(len(row) == 1 and shard.fullmatch(row[0].strip()) for row in rows if row and row[0].strip()):
		return rows
	return [row for part in listed for row in tables.table_rows(tables.table_text(media, index, sro_pk2.fold_ascii(part)))]


# The VSRO text layout: service, key, then 13 language cells; 8 is the
# English one the port reads, and the VSRO client repeats English in 6,
# 10, 12, 13 and 14 where it has no translation.
VSRO_TEXT_COLUMNS = 15
TEXT_ENGLISH = 8
V1696_TEXT_COLUMNS = 17
V1696_ABSENT = "0"


# ================
# vsro_text_row
#
# One text row in the VSRO layout. A v1.696 row carries a numeric text id
# after the service cell, one more language cell at the end, and "0" in
# every cell it has no translation for (read 2026-11-11 off
# SN_ITEM_MALL_AVATAR_M_TURKEY in both clients: v1.696 cell i+1 is VSRO
# cell i). The id goes, the cells shift back, and an absent translation
# takes the English name, as the VSRO client authors it. VSRO rows pass
# through unchanged.
# ================
def vsro_text_row(row):
	if len(row) != V1696_TEXT_COLUMNS or not row[1].strip().isdigit():
		return row
	cells = [row[0]] + row[2:2 + VSRO_TEXT_COLUMNS - 1]
	english = cells[TEXT_ENGLISH]
	for column in range(3, VSRO_TEXT_COLUMNS):
		if cells[column] == V1696_ABSENT:
			cells[column] = "" if column == 3 else english
	return cells


# ================
# add_avatars
#
# A successful marker owns exactly one generated shard. The manifest is read
# from the output when the cap-110 import already wrote it, else from the
# native tree, and gains one line.
# ================
def add_avatars(vsro_root, native, output, source=VSRO_SOURCE):
	native, output = native.resolve(), output.resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("avatar output must be separate from the native extraction")
	marker = output / source["report"]
	prior = json.loads(marker.read_text(encoding="utf-8")) if marker.exists() else {}
	shard = safe_target(output / TEXT_ROOT, source["shard"])
	manifest = safe_target(output / TEXT_ROOT, "itemdata.txt")
	if shard.exists():
		if not marker.exists():
			raise ValueError("output already owns an unmarked avatar import shard")
		if sro_pk2.digest(shard.read_bytes()) != prior["shardSha256"]:
			raise ValueError("refusing to replace a locally changed imported shard")
	if not enabled():
		remove_import(output, marker, shard, manifest)
		return {"enabled": False, "added": 0}
	if not source_present(vsro_root, source):
		remove_import(output, marker, shard, manifest)
		return {"enabled": False, "added": 0, "skipped": "no later client found"}
	with ExitStack() as stack:
		archives, identities = open_archives(stack, vsro_root, source)
		media, index = archives["Media"]
		# ================
		# read_later
		# ================
		def read_later(name):
			return tables.table_text(media, index, name)
		base = native / TEXT_ROOT
		base_paths = {sro_pk2.fold_ascii(path.name): path for path in base.iterdir() if path.is_file()}
		# ================
		# read_native
		# ================
		def read_native(name):
			return base_paths[name].read_bytes()
		native_rows = shared.read_item_tables(read_native)
		later_rows = shared.read_item_tables(read_later)
		reserved = {
			int(row[1]) for path in (output / TEXT_ROOT).glob("characterdata_*.txt")
			for row in tables.table_rows(path.read_bytes()) if len(row) > 1
		}
		# The cap-110 shard's identities sit above every later row and below
		# FRESH_ID_BASE; they are reserved anyway so a clash cannot happen.
		other_shard = output / TEXT_ROOT / items.OUTPUT_SHARD
		if other_shard.exists():
			reserved |= {int(row[items.IDENTITY]) for row in tables.table_rows(other_shard.read_bytes())}
		# An earlier source's avatars are already in the game: this source
		# skips their codenames as it skips native ones, and keeps clear of
		# their identities.
		for earlier in SOURCES[:source_index(source)]:
			earlier_shard = output / TEXT_ROOT / earlier["shard"]
			if earlier_shard.exists():
				earlier_rows = tables.table_rows(earlier_shard.read_bytes())
				native_rows = native_rows + earlier_rows
				reserved |= {int(row[items.IDENTITY]) for row in earlier_rows}
		native_paths = {
			name: {
				shared.normalized_path(path.relative_to(native / folder).as_posix())
				for path in (native / folder).rglob("*") if path.is_file()
			} for name, folder in FOLDERS.items()
		}
		later_texts = [
			row for name in ("textdata_equip&skill.txt", "textdata_object.txt", "textdata_new.txt")
			for row in later_text_rows(media, index, name)
		]
		text_path = safe_target(output / TEXT_ROOT, "textdataname.txt")
		base_text = text_path.read_bytes() if text_path.exists() else read_native("textdataname.txt")
		known_text = {row[1] for row in tables.table_rows(base_text) if len(row) > 1} | {row[1] for row in later_texts if len(row) > 1}
		avatars, summary = select_avatars(
			native_rows, later_rows, reserved, previous_targets(prior),
			resource_check(archives, native_paths), known_text,
			any_prefix=bool(source.get("boards")),
		)
		merged_text, missing_descriptions = items.merge_texts(base_text, later_texts, avatars)
		assets = shared.copy_asset_closure(avatars, native, output, archives)
		strip_mall(output, set(prior.get("mall", {}).get("packages", [])))
		strip_tabs(output, set(prior.get("mall", {}).get("tabs", [])))
		strip_text_rows(output, set(prior.get("mall", {}).get("tabLabels", [])))
		mall_files = {name: safe_target(output / TEXT_ROOT, name) for name, _ in MALL_TABLES}
		mall_summary = {"packages": [], "listed": 0, "unlisted": [], "note": "no mall tables in the later client or native tree"}
		if source.get("boards"):
			current = {
				name: tables.table_rows(mall_files[name].read_bytes() if mall_files[name].exists() else read_native(name))
				for name, _ in MALL_TABLES
			}
			mall, mall_summary, pages = board_rows(
				avatars, current, {int(row[2]) for row in current["refpackageitem.txt"] if len(row) > 2},
			)
			for name, _ in MALL_TABLES:
				raw = mall_files[name].read_bytes() if mall_files[name].exists() else read_native(name)
				write_atomically(mall_files[name], text_tables.append_rows(raw, mall[name]))
			tab_path = safe_target(output / TEXT_ROOT, SHOP_TAB_TABLE)
			raw_tabs = tab_path.read_bytes() if tab_path.exists() else read_native(SHOP_TAB_TABLE)
			write_atomically(tab_path, text_tables.append_rows(raw_tabs, board_tab_rows(tables.table_rows(raw_tabs), pages)))
			boards = sorted({board for board, _ in pages})
			mall_summary["tabLabels"] = [BOARD_LABELS[board] for board in boards]
			ui_path = safe_target(output / TEXT_ROOT, "textuisystem.txt")
			raw_ui = ui_path.read_bytes() if ui_path.exists() else read_native("textuisystem.txt")
			write_atomically(ui_path, text_tables.append_rows(raw_ui, board_label_rows(boards)))
		elif all(sro_pk2.fold_ascii(name) in base_paths and tables.TEXTDATA + name in index for name, _ in MALL_TABLES):
			current = {
				name: tables.table_rows(mall_files[name].read_bytes() if mall_files[name].exists() else read_native(name))
				for name, _ in MALL_TABLES
			}
			later_mall = {name: later_table_rows(media, index, name) for name, _ in MALL_TABLES}
			# Goods a later source listed in a previous build are stale:
			# that source strips and re-lists them after this pass.
			stale_packages = set()
			for later_source in SOURCES[source_index(source) + 1:]:
				later_marker = output / later_source["report"]
				if later_marker.exists():
					stale_packages |= set(json.loads(later_marker.read_text(encoding="utf-8")).get("mall", {}).get("packages", []))
			mall, mall_summary = mall_rows(
				later_mall, avatars, current, {int(row[2]) for row in current["refpackageitem.txt"] if len(row) > 2},
				enabled(flag=LIST_ALL_FLAG), stale_packages,
			)
			for name, _ in MALL_TABLES:
				raw = mall_files[name].read_bytes() if mall_files[name].exists() else read_native(name)
				write_atomically(mall_files[name], text_tables.append_rows(raw, mall[name]))
			if any(row[2] == ATTACH_TAB for row in mall["refshopgoods.txt"]):
				tab_path = safe_target(output / TEXT_ROOT, SHOP_TAB_TABLE)
				raw = tab_path.read_bytes() if tab_path.exists() else read_native(SHOP_TAB_TABLE)
				row = attach_tab_row(tables.table_rows(raw), later_table_rows(media, index, SHOP_TAB_TABLE))
				if row is not None:
					write_atomically(tab_path, text_tables.append_rows(raw, [row]))
					mall_summary["tabs"] = [ATTACH_TAB]
		raw_shard = b"\xff\xfe" + text_tables.table_text(avatars).encode("utf-16-le")
		current = manifest.read_bytes() if manifest.exists() else read_native("itemdata.txt")
		names = [row[0] for row in tables.table_rows(current) if row]
		names = [name for name in names if sro_pk2.fold_ascii(name) != shard.name]
		write_atomically(shard, raw_shard)
		write_atomically(text_path, merged_text)
		write_atomically(manifest, b"\xff\xfe" + ("\r\n".join(names + [shard.name]) + "\r\n").encode("utf-16-le"))
		summary.update({
			"enabled": True, "format": "opensro-mall-avatars-v1", "portOnly": True, "source": source["name"],
			"laterArchiveHashes": identities, "shardSha256": sro_pk2.digest(raw_shard),
			"absentAuthoredDescriptions": missing_descriptions,
			"assets": assets, "mall": mall_summary,
			"limits": [
				"Listed in the Item Mall only where the later client's mall sold them; the rest are granted by GM. No drop or event reward is added.",
				"Event copies and rows with absent resources (the Nasrun mounts) are not imported.",
				"Rendered appearance in each slot requires playtesting."
			]
		})
		write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode("utf-8"))
		return summary


# ================
# source_pins
#
# The pinned archive hashes of one source: None keeps the VSRO pin file
# import_vsro_alexandria reads; another source names its own file, in the
# same format, so its archives are checked before any of them is read.
# ================
def source_pins(source):
	if source["pins"] is None:
		return None
	try:
		pin = json.loads(Path(source["pins"]).read_text(encoding="utf-8"))
	except (OSError, ValueError) as error:
		raise SystemExit(f"{source['pins']} is unreadable: {error}")
	if pin.get("format") != PIN_FORMAT or not isinstance(pin.get("archives"), dict):
		raise SystemExit(f"{source['pins']} is not a {PIN_FORMAT} pin file")
	return pin["archives"]


# ================
# source_index
#
# A source's place in import order, found by its shard: each source owns
# exactly one shard, so a caller may pass an adjusted copy of a source.
# ================
def source_index(source):
	for index, known in enumerate(SOURCES):
		if known["shard"] == source["shard"]:
			return index
	raise ValueError(f"unknown avatar source shard: {source['shard']}")


# ================
# source_present
#
# A pk2 source is present when its client folder holds every archive;
# the pinned-archive source when the archive file itself is there.
# ================
def source_present(root, source):
	if root is None:
		return False
	if source.get("zip"):
		return Path(root).is_file()
	return all((Path(root) / f"{name}.pk2").is_file() for name in FOLDERS)


# ================
# open_archives
#
# One source's archives as (data, index) pairs plus the identities the
# marker records: pk2 sources mmap their client archives, the pinned
# archive comes from avatar_archive.
# ================
def open_archives(stack, root, source):
	if source.get("zip"):
		return avatar_archive.open_archive(root, source["pins"])
	archives, identities = {}, {}
	for name in FOLDERS:
		handle = stack.enter_context(open(Path(root) / f"{name}.pk2", "rb"))
		data = stack.enter_context(mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ))
		archives[name] = (data, {shared.normalized_path(entry.path): entry for entry in sro_pk2.read_directory(data)})
		identities[name] = tables.sha256_of(data)
	return archives, identities


# ================
# add_source
#
# One source's pass: its archives are verified against its pins when the
# import is on and the client is present, then imported; an absent
# client removes the source's previous import. The pinned archive is
# verified as it is opened (avatar_archive), before any entry is read.
# ================
def add_source(root, native, output, source):
	if root is not None and enabled() and source_present(root, source) and not source.get("zip"):
		tables.require_vsro_pin(root, tuple(FOLDERS), source_pins(source))
	return add_avatars(root, native, output, source)


# ================
# v1696_root
#
# The v1.696 client folder from SRO_V1696_ROOT, or None when unset.
# ================
def v1696_root(env=os.environ):
	value = env.get(V1696_ENV, "").strip()
	return Path(value) if value else None


# ================
# avatars_zip
#
# The pinned avatar archive from SRO_AVATARS_ZIP, or None when unset.
# The game builds without it; the archive is never committed.
# ================
def avatars_zip(env=os.environ):
	value = env.get(AVATARS_ENV, "").strip()
	return Path(value) if value else None


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--vsro-root", type=Path, required=True)
	parser.add_argument("--v1696-root", type=Path, default=v1696_root(), help=f"the v1.696 client (else {V1696_ENV})")
	parser.add_argument("--avatars-zip", type=Path, default=avatars_zip(), help=f"the pinned avatar archive (else {AVATARS_ENV})")
	parser.add_argument("--native-extracted", type=Path, default=EXTRACTED_ROOT)
	parser.add_argument("--output-extracted", type=Path, default=REPO_ROOT / ".state" / "mall-avatars" / "extracted")
	args = parser.parse_args()
	for source, root in ((VSRO_SOURCE, args.vsro_root), (V1696_SOURCE, args.v1696_root), (AVATARS_SOURCE, args.avatars_zip)):
		summary = add_source(root, args.native_extracted, args.output_extracted, source)
		print(f"[{source['name']}] Avatars: {summary['added']}; enabled: {summary['enabled']}")
		if summary["enabled"]:
			print(f"  Hats: {summary['hats']}; skipped: {len(summary['skipped'])}; verified resources: {len(summary['assets'])}")
			print(f"  Item Mall: {summary['mall']['listed']} listed; {len(summary['mall']['unlisted'])} unlisted")
			for entry in summary["skipped"]:
				print(f"  skipped {entry['code']}: {', '.join(entry['missing'])}")
			print(f"  Report: {args.output_extracted / source['report']}")
		elif "skipped" in summary:
			print(f"  {summary['skipped']}")


if __name__ == "__main__":
	main()
