"""
===========================================================================

cap110_blue_berserk.py - the Blue Berserk system's items, shop rows and texts

Port-only, not native. The Blue Berserk line (levels 95 to 110) adds nine items
the v1.150 tables lack: the Energy of Life scrolls (ITEM_ETC_SECOND_HWAN_SCROLL_01
to 04), the buff item of their 100% gauge charge (ITEM_ETC_SECOND_HWAN) and the
four time-extension parameter items (ITEM_ETC_INCREASE_HWAN_TIME_01 to 04). This
tool imports their rows from the later (VSRO) client into an item shard of their
own, with:

	- the four scroll packages, their scrap and price rows, and the shop goods that
	  put them in the potion stores of Samarkand/Constantinople/Jangan/Khotan/
	  Donwhang (STORE_{CA,CH,EU,KT,WC}_POTION_TAB1), in the next free slots;
	- the Blue Berserk level table (hwanleveldata.txt: the title symbol of each
	  level and race);
	- the texts: names, tooltips, the Blue titles, the level refusal and the time
	  parameter line.

The v1.150 text table holds some of these symbols with a blank English column
(the content never shipped); a blank English row is replaced by the later
client's row, a row that has English keeps it: native text never changes.

They are plain game data of the cap-110 expansion, so they follow the item
switch and the level cap, not the quest switch: SRO_CAP110_ITEMS with
SRO_LEVEL_CAP=110. Off, the tool removes what a previous on build left and
restores the replaced text lines, so a reused variant cannot retain it.

The animation records of these items, and the icons and models, are native
already. item_grouping.txt (tooltip grouping) is not read by the port and is not
imported. The numeric level parameters (speed and damage absorption) are not a
client table; they belong to the server step. No retail table is committed to Git.

===========================================================================
"""
import argparse
from contextlib import ExitStack
import json
import mmap
from pathlib import Path

import alexandria_map_tables as map_tables
import alexandria_npcs as characters
import cap110_item_import as item_import
import cap110_items as items
import cap110_monsters as monsters
import cap110_quest_items as quest_items
import cap110_rewards as rewards
import import_vsro_alexandria as tables
import sro_pk2
from prepare_client_resources import safe_target, write_atomically

MARKER = ".opensro-cap110-blue-berserk.json"
OUTPUT_SHARD = "itemdata_110003.txt"
LEVEL_TABLE = "hwanleveldata.txt"
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA
FORMAT = "opensro-cap110-blue-berserk-v1"
VARIANT_VERSION = "11"
ENGLISH_COLUMN = 8
KEY_COLUMN = 1
SCROLL_COUNT = 4
# The nine items and the kind each must be (type 3/3/x/y).
ITEM_KINDS = {"ITEM_ETC_SECOND_HWAN": ("3", "3", "1", "8")}
ITEM_KINDS.update({f"ITEM_ETC_INCREASE_HWAN_TIME_0{tier}": ("3", "3", "3", "10") for tier in range(1, SCROLL_COUNT + 1)})
ITEM_KINDS.update({f"ITEM_ETC_SECOND_HWAN_SCROLL_0{tier}": ("3", "3", "13", "14") for tier in range(1, SCROLL_COUNT + 1)})
PACKAGES = tuple(f"PACKAGE_ITEM_ETC_SECOND_HWAN_SCROLL_0{tier}" for tier in range(1, SCROLL_COUNT + 1))
POTION_TABS = tuple(f"STORE_{region}_POTION_TAB1" for region in ("CA", "CH", "EU", "KT", "WC"))
SHOP_TABLES = ("refshopgoods.txt", "refpackageitem.txt", "refscrapofpackageitem.txt", "refpricepolicyofitem.txt")
# Item name and tooltip symbols (tiers 5 and 6 have texts only), then the interface symbols.
ITEM_SYMBOLS = (("SN_ITEM_ETC_SECOND_HWAN", "SN_ITEM_ETC_SECOND_HWAN_TT_DESC")
	+ tuple(f"SN_ITEM_ETC_INCREASE_HWAN_TIME_0{tier}{tail}" for tier in range(1, SCROLL_COUNT + 1) for tail in ("", "_TT_DESC"))
	+ tuple(f"SN_ITEM_ETC_SECOND_HWAN_SCROLL_0{tier}{tail}" for tier in range(1, 7) for tail in ("", "_TT_DESC")))
UI_SYMBOLS = (tuple(f"UIO_SECOND_HWAN_{race}_NAME_LEVEL_{level}" for race in ("CH", "EU") for level in range(1, 7))
	+ ("UIIT_MSG_HWANITEM_CANNOT_USE", "PARAM_HWAN_TIME_INCREASE"))
TEXT_FILES = (("textdataname.txt", "textdata_object.txt", ITEM_SYMBOLS), ("textuisystem.txt", "textuisystem.txt", UI_SYMBOLS))


# ================
# enabled
#
# The item switch and the expansion level cap; the quest switch is not asked.
# ================
enabled = quest_items.enabled


# ================
# select_items
#
# The later client's rows of the nine items, in codename order. A native row
# wins (an item native already is skipped); an item the later client lacks, a
# row of another kind, or a link to an item the native table lacks refuses.
# ================
def select_items(later, native):
	selected = []
	for code in sorted(ITEM_KINDS):
		if code in native:
			continue
		row = later.get(code)
		if row is None or row[items.SERVICE] != "1":
			raise ValueError("Blue Berserk item is not in the later client: " + code)
		if tuple(row[items.TYPE_FIRST:items.TYPE_FOURTH + 1]) != ITEM_KINDS[code]:
			raise ValueError("Blue Berserk item has an unexpected kind: " + code)
		for column in range(items.PARAM_FIRST, items.PARAM_LAST, items.PARAM_STRIDE):
			link = row[column + 1]
			if link.startswith("ITEM_") and link not in native:
				raise ValueError(f"Blue Berserk item links an item the native table lacks: {code} -> {link}")
		selected.append(row)
	return selected


# ================
# fill_text
#
# The text table with the later client's rows for the given symbols: a symbol
# the table lacks is appended, a symbol whose English column is blank is
# replaced in place, and a symbol with English keeps its line. Returns the new
# bytes, the appended keys and {key: original line} of the replaced ones, so
# the off path can restore the table exactly.
# ================
def fill_text(raw, later_by_key, symbols):
	lines = tables.decode_table(raw).split("\r\n")
	present = {}
	for at, line in enumerate(lines):
		fields = line.split("\t")
		if len(fields) > KEY_COLUMN and fields[KEY_COLUMN] in symbols:
			present[fields[KEY_COLUMN]] = at
	appended, replaced, additions = [], {}, []
	for key in symbols:
		if key not in later_by_key:
			continue
		line = "\t".join(map_tables.native_text_row(later_by_key[key]))
		if key not in present:
			appended.append(key)
			additions.append(line)
			continue
		fields = lines[present[key]].split("\t")
		if len(fields) > ENGLISH_COLUMN and fields[ENGLISH_COLUMN].strip() == "":
			replaced[key] = lines[present[key]]
			lines[present[key]] = line
	if additions:
		while lines and lines[-1] == "":
			lines.pop()
		lines += additions + [""]
	return b"\xff\xfe" + "\r\n".join(lines).encode("utf-16-le"), appended, replaced


# ================
# restore_text
#
# The inverse of fill_text for the recorded keys.
# ================
def restore_text(raw, appended, replaced):
	lines = tables.decode_table(raw).split("\r\n")
	kept = []
	for line in lines:
		fields = line.split("\t")
		key = fields[KEY_COLUMN] if len(fields) > KEY_COLUMN else None
		if key in appended:
			continue
		kept.append(replaced.get(key, line))
	return b"\xff\xfe" + "\r\n".join(kept).encode("utf-16-le")


# ================
# shop_rows
#
# The rows to append to the four shop tables, in the variant's own layout
# (version 11, a thirteen column price row): goods that put each scroll
# package in the next free slot of every potion store tab, and the package,
# scrap and price rows from the later client.
# ================
def shop_rows(later, variant):
	goods, packages, scraps, prices = [], [], [], []
	for row in later["refpackageitem.txt"]:
		if row[3] in PACKAGES:
			packages.append([row[0], VARIANT_VERSION] + row[2:])
	for row in later["refscrapofpackageitem.txt"]:
		if row[2] in PACKAGES:
			scraps.append([row[0], VARIANT_VERSION] + row[2:])
	for row in later["refpricepolicyofitem.txt"]:
		if row[2] in PACKAGES:
			prices.append([row[0], VARIANT_VERSION, row[2], row[3], row[5]] + row[6:])
	found = {kind: sorted(row[2] if kind != "package" else row[3] for row in rows)
		for kind, rows in (("package", packages), ("scrap", scraps), ("price", prices))}
	if any(names != sorted(PACKAGES) for names in found.values()):
		raise ValueError("the later client lacks a scroll package, scrap or price row")
	slots = {}
	for row in variant["refshopgoods.txt"]:
		if row[2] in POTION_TABS:
			slots[row[2]] = max(slots.get(row[2], -1), int(row[4]))
	if sorted(slots) != sorted(POTION_TABS):
		raise ValueError("the variant lacks a potion store tab: " + ", ".join(sorted(set(POTION_TABS) - set(slots))))
	for tab in POTION_TABS:
		for step, package in enumerate(PACKAGES, 1):
			goods.append(["1", VARIANT_VERSION, tab, package, str(slots[tab] + step), "-1", "xxx", "-1", "xxx", "-1", "xxx", "-1", "xxx"])
	return {"refshopgoods.txt": goods, "refpackageitem.txt": packages, "refscrapofpackageitem.txt": scraps,
		"refpricepolicyofitem.txt": prices}


# ================
# shop_key
#
# Which column names the package in each shop table.
# ================
SHOP_KEY = {"refshopgoods.txt": 3, "refpackageitem.txt": 3, "refscrapofpackageitem.txt": 2, "refpricepolicyofitem.txt": 2}


# ================
# remove_owned
#
# What a previous on build left: the shard and its manifest line, the shop
# rows of the four packages, the level table, and the text (appended lines
# leave, replaced lines come back).
# ================
def remove_owned(text_root, prior):
	(text_root / OUTPUT_SHARD).unlink(missing_ok=True)
	(text_root / LEVEL_TABLE).unlink(missing_ok=True)
	manifest = text_root / "itemdata.txt"
	if manifest.exists():
		lines = [row[0] for row in tables.table_rows(manifest.read_bytes()) if row and row[0].lower() != OUTPUT_SHARD]
		write_atomically(manifest, b"\xff\xfe" + ("\r\n".join(lines) + "\r\n").encode("utf-16-le"))
	for name in SHOP_TABLES:
		path = text_root / name
		if path.exists():
			kept = [row for row in tables.table_rows(path.read_bytes()) if len(row) <= SHOP_KEY[name] or row[SHOP_KEY[name]] not in PACKAGES]
			write_atomically(path, monsters.encode_rows(kept))
	for name, record in prior.get("texts", {}).items():
		path = text_root / name
		if path.exists():
			write_atomically(path, restore_text(path.read_bytes(), set(record["appended"]), record["replaced"]))


# ================
# add_blue_berserk
# ================
def add_blue_berserk(vsro_root, native, output):
	native, output = Path(native).resolve(), Path(output).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("Blue Berserk overlay must be separate from native extraction")
	text_root, marker = output / TEXT_ROOT, output / MARKER
	prior = json.loads(marker.read_text(encoding="utf-8")) if marker.exists() else {}
	shard = safe_target(text_root, OUTPUT_SHARD)
	if shard.exists():
		if not marker.exists():
			raise ValueError("output already owns an unmarked Blue Berserk item shard")
		if sro_pk2.digest(shard.read_bytes()) != prior["shardSha256"]:
			raise ValueError("refusing to replace a locally changed Blue Berserk item shard")
	if marker.exists():
		remove_owned(text_root, prior)
		marker.unlink()
	if not enabled():
		return {"enabled": False}
	with ExitStack() as stack:
		archives, identities = {}, {}
		for name in item_import.FOLDERS:
			handle = stack.enter_context(open(Path(vsro_root) / f"{name}.pk2", "rb"))
			data = stack.enter_context(mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ))
			archives[name] = (data, {item_import.normalized_path(entry.path): entry for entry in sro_pk2.read_directory(data)})
			identities[name] = tables.sha256_of(data)
		media, index = archives["Media"]

		# ================
		# read_later
		# ================
		def read_later(name):
			return tables.table_text(media, index, name)
		later_rows = item_import.read_item_tables(read_later)
		later = items.index_items(later_rows)
		base = native / TEXT_ROOT
		native_items = items.index_items(item_import.read_item_tables(lambda name: (base / name).read_bytes()))
		selected = select_items(later, native_items)
		converted = rewards.convert_rows(selected, rewards.occupied_identities(text_root, later_rows, OUTPUT_SHARD),
			{code: entry["target"] for code, entry in prior.get("idMap", {}).items()})
		assets = item_import.copy_asset_closure(converted, native, output, archives, worn=False)
		raw_shard = b"\xff\xfe" + characters.table_text(converted).encode("utf-16-le")
		manifest_path = safe_target(text_root, "itemdata.txt")
		manifest_names = [row[0].strip() for row in tables.table_rows(manifest_path.read_bytes())]
		if OUTPUT_SHARD not in map(sro_pk2.fold_ascii, manifest_names):
			manifest_names.append(OUTPUT_SHARD)
		# Texts: item symbols from textdata_object, interface symbols from textuisystem.
		texts = {}
		for name, source, symbols in TEXT_FILES:
			path = safe_target(text_root, name)
			by_key = {row[KEY_COLUMN]: row for row in tables.table_rows(read_later(source)) if len(row) > ENGLISH_COLUMN + 1}
			raw, appended, replaced = fill_text(path.read_bytes(), by_key, symbols)
			texts[name] = {"appended": appended, "replaced": replaced, "raw": raw}
		later_shop = {name: tables.table_rows(read_later(name)) for name in SHOP_TABLES if name != "refshopgoods.txt"}
		later_shop["refshopgoods.txt"] = []
		variant_shop = {name: tables.table_rows(safe_target(text_root, name).read_bytes()) for name in SHOP_TABLES}
		new_shop = shop_rows(later_shop, variant_shop)
		level_table = read_later(LEVEL_TABLE)
		write_atomically(shard, raw_shard)
		write_atomically(manifest_path, b"\xff\xfe" + ("\r\n".join(manifest_names) + "\r\n").encode("utf-16-le"))
		for name, record in texts.items():
			write_atomically(safe_target(text_root, name), record.pop("raw"))
		for name, rows in new_shop.items():
			path = safe_target(text_root, name)
			write_atomically(path, characters.append_rows(path.read_bytes(), rows))
		write_atomically(safe_target(text_root, LEVEL_TABLE), level_table)
		summary = {"enabled": True, "format": FORMAT, "portOnly": True,
			"imported": len(converted), "codenames": [row[items.CODENAME] for row in converted],
			"idMap": {row[items.CODENAME]: {"source": int(later[row[items.CODENAME]][items.IDENTITY]),
				"target": int(row[items.IDENTITY])} for row in converted},
			"shopRows": {name: len(rows) for name, rows in new_shop.items()}, "texts": texts,
			"levelTableSha256": sro_pk2.digest(level_table), "assets": assets,
			"laterArchiveHashes": identities, "shardSha256": sro_pk2.digest(raw_shard)}
		write_atomically(marker, (json.dumps(summary, indent=2, ensure_ascii=False) + "\n").encode("utf-8"))
		return summary


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--vsro-root", required=True)
	parser.add_argument("--native-extracted", required=True)
	parser.add_argument("--output", required=True)
	args = parser.parse_args()
	# Off, add_blue_berserk only removes its owned outputs and reads no archive.
	if enabled():
		tables.require_vsro_pin(args.vsro_root, tuple(item_import.FOLDERS))
	summary = add_blue_berserk(args.vsro_root, args.native_extracted, args.output)
	print(json.dumps({key: value for key, value in summary.items() if key not in ("assets", "idMap", "texts")}, indent=2))


if __name__ == "__main__":
	main()
