"""
===========================================================================

alexandria_npc_services.py - optional city shop and map-label dependencies

Port-only, not native. Called only by the Alexandria variant builder.
Native rows win. Missing item packages are reported, never partly granted.
===========================================================================
"""
from collections import defaultdict

import alexandria_npcs as npcs
import alexandria_shop_d10 as d10
import alexandria_shop_d11 as d11
import alexandria_shop_supplies as supplies
import import_vsro_alexandria as tables

SHOP_TABLES = (
	"refshopgroup.txt", "refmappingshopgroup.txt", "refshop.txt",
	"refmappingshopwithtab.txt", "refshoptabgroup.txt", "refshoptab.txt",
	"refshopgoods.txt", "refpackageitem.txt", "refscrapofpackageitem.txt",
	"refpricepolicyofitem.txt", "refconditiontosellpackageitem.txt", "refrewardpolicytosellpackageitem.txt"
)
IDENTITY_TABLES = {"refshopgroup.txt", "refshop.txt", "refshoptabgroup.txt", "refshoptab.txt", "refpackageitem.txt"}
REGION_UNITS = 192
POSITION_SCALE = 10
LOCAL_MAP_COLUMNS = 20
CITY_MAP_ID = "12"
RESTORED_STORE_NPCS = {"NPC_SD_M_AREA_ACCESSORY", "NPC_SD_T_AREA_ACCESSORY"}
GOODS_TAB = 2
GOODS_SLOT = 4
# The largest tab the native tables ship (slots 0..44).
MAX_TAB_SLOTS = 45


# ================
# unique_slots
#
# The later tables place two packages in one slot of a tab (for example
# STORE_SD_ACCESSORY_EU_TAB3 slot 11). The v1.150 shop window shows one
# package per slot, so a second package would silently hide the first.
# The first row keeps its slot; a later one moves to the tab's lowest
# free slot, or is dropped and reported when the tab is full. #1745.
# ================
def unique_slots(goods):
	used = defaultdict(set)
	for row in goods:
		used[row[GOODS_TAB]].add(row[GOODS_SLOT])
	kept, seen, moved, dropped = [], defaultdict(set), [], []
	for row in goods:
		tab, slot = row[GOODS_TAB], row[GOODS_SLOT]
		if slot in seen[tab]:
			free = next((str(s) for s in range(MAX_TAB_SLOTS) if str(s) not in used[tab]), None)
			if free is None:
				dropped.append(f"{tab}:{slot}:{row[3]}")
				continue
			moved.append(f"{tab}:{slot}->{free}:{row[3]}")
			row = [*row[:GOODS_SLOT], free, *row[GOODS_SLOT + 1:]]
			used[tab].add(free)
			slot = free
		seen[tab].add(slot)
		kept.append(row)
	return kept, {"moved": moved, "dropped": dropped}


# ================
# merge_shops
# ================
def merge_shops(native, later, item_codes, npc_codes):
	base = {name: tables.table_rows(raw) for name, raw in native.items()}
	source = {name: tables.table_rows(raw) for name, raw in later.items()}
	selected = {name: [] for name in SHOP_TABLES}
	groups = {r[3] for r in source["refshopgroup.txt"] if len(r) >= 5 and r[0] == "1" and r[4] in npc_codes}
	selected["refshopgroup.txt"] = [r for r in source["refshopgroup.txt"] if len(r) >= 5 and r[3] in groups]
	selected["refmappingshopgroup.txt"] = [r for r in source["refmappingshopgroup.txt"] if len(r) >= 4 and r[2] in groups]
	stores = {r[3] for r in selected["refmappingshopgroup.txt"]}
	selected["refshop.txt"] = [r for r in source["refshop.txt"] if len(r) >= 4 and r[3] in stores]
	selected["refmappingshopwithtab.txt"] = [r for r in source["refmappingshopwithtab.txt"] if len(r) >= 4 and r[2] in stores]
	tab_groups = {r[3] for r in selected["refmappingshopwithtab.txt"]}
	selected["refshoptabgroup.txt"] = [r for r in source["refshoptabgroup.txt"] if len(r) >= 5 and r[3] in tab_groups]
	selected["refshoptab.txt"] = [r for r in source["refshoptab.txt"] if len(r) >= 6 and r[4] in tab_groups]
	# Existing stores keep their complete native merchandise and layout.
	native_tabs = {r[3] for r in base["refshoptab.txt"] if len(r) >= 6}
	tabs = {r[3] for r in selected["refshoptab.txt"]} - native_tabs
	goods = [r for r in source["refshopgoods.txt"] if len(r) >= 5 and r[0] == "1" and r[2] in tabs]
	scraps = defaultdict(list)
	for row in source["refscrapofpackageitem.txt"]:
		if len(row) >= 20 and row[0] == "1":
			scraps[row[2]].append(row)
	packages = {r[3] for r in goods}
	missing = {code: sorted({r[3] for r in scraps[code]} - item_codes) for code in packages}
	allowed = {code for code in packages if scraps[code] and not missing[code]}
	native_packages = {r[3] for r in base["refpackageitem.txt"] if len(r) > 3}
	selected["refshopgoods.txt"], slots = unique_slots([r for r in goods if r[3] in allowed])
	for name in ("refpackageitem.txt", "refscrapofpackageitem.txt", "refpricepolicyofitem.txt", "refconditiontosellpackageitem.txt", "refrewardpolicytosellpackageitem.txt"):
		selected[name] = [r for r in source[name] if len(r) > 3 and r[0] == "1" and (r[3] if name == "refpackageitem.txt" else r[2]) in allowed - native_packages]
	merged, counts = {}, {}
	for name in SHOP_TABLES:
		rows = base[name]
		kind = npcs.common_value(rows, 1) if rows else "11"
		if name in IDENTITY_TABLES:
			known = {r[3] for r in rows if len(r) > 3}
			occupied = {int(r[2]) for r in rows if len(r) > 3}
			next_id = max(occupied | {int(r[2]) for r in selected[name]}, default=0) + 1
		else:
			known = {tuple(r[2:]) for r in rows}
		added = []
		for row in selected[name]:
			key = row[3] if name in IDENTITY_TABLES else tuple(row[2:])
			if key in known:
				continue
			converted = npcs.with_kind(row, kind)
			if name in IDENTITY_TABLES:
				if int(converted[2]) in occupied:
					converted[2] = str(next_id)
					next_id += 1
				occupied.add(int(converted[2]))
			known.add(key)
			added.append(converted)
		merged[name] = npcs.append_rows(native[name], added)
		counts[name] = len(added)
	return merged, {"added": counts, "deferredPackages": {k: v for k, v in sorted(missing.items()) if v}, "packages": len(allowed), "goodsSlots": slots}


# ================
# npc_map_labels
#
# Port-only: later NPC map records name an NPC but omit pixel coordinates.
# Project the restored npcpos through the same page bounds as world-map.ts,
# emitting native kind-1 labels so the existing M window draws the names.
# ================
def npc_map_labels(mapinfo, positions, characters, original):
	page = next((r for r in tables.table_rows(mapinfo) if r[0] == CITY_MAP_ID), None)
	if page is None:
		return original, 0
	left, top, right, bottom = [float(page[i]) * REGION_UNITS + float(page[i + 4]) for i in range(10, 14)]
	width, height = float(page[8]), float(page[9])
	by_id = {r[1]: r for r in characters if len(r) > 5 and r[2].startswith("NPC_SD_")}
	known = {r[2] for r in tables.table_rows(original) if len(r) == LOCAL_MAP_COLUMNS and r[7] == CITY_MAP_ID}
	occupied = {int(r[0]) for r in tables.table_rows(original) if r and r[0].isdigit()}
	next_id = max(occupied, default=0) + 1
	added = []
	for pose in tables.table_rows(positions):
		row = by_id.get(pose[0])
		if row is None or row[5] in known:
			continue
		region = int(pose[1]) & 0xffff
		x = ((region & 255) * REGION_UNITS + float(pose[2]) / POSITION_SCALE - left) / (right - left) * width
		y = (top - ((region >> 8) * REGION_UNITS + float(pose[4]) / POSITION_SCALE)) / (top - bottom) * height
		if not 0 <= x < width or not 0 <= y < height:
			continue
		added.append([str(next_id), "1", row[5], "", "", "", "0", CITY_MAP_ID, "-1", "-1", str(round(x)), str(round(y)), "0", "0", "255", "255", "255", "0", "0", "1"])
		known.add(row[5])
		next_id += 1
	return npcs.append_rows(original, added), len(added)


# ================
# add_services
# ================
def add_services(vsro_root, native, output):
	from build_alexandria_variant import TEXTDATA, read_vsro_file, replace_file
	root = output / "Media_extracted" / TEXTDATA
	native_root = native / "Media_extracted" / TEXTDATA
	characters = [r for p in root.glob("characterdata_*.txt") for r in tables.table_rows(p.read_bytes())]
	codes = {r[2] for r in characters if len(r) > 5 and r[2] in RESTORED_STORE_NPCS}
	items = {r[2] for p in root.glob("itemdata_*.txt") for r in tables.table_rows(p.read_bytes()) if len(r) > 2 and r[0] == "1"}
	base = {name: (root / name).read_bytes() if (root / name).exists() else (native_root / name).read_bytes() for name in SHOP_TABLES}
	later = {name: read_vsro_file(vsro_root, "Media", TEXTDATA + name) for name in SHOP_TABLES}
	merged, summary = merge_shops(base, later, items, codes)
	for name, raw in merged.items():
		replace_file(root / name, raw)
	# Ordinary degree 10 goods, on stores of the city's own (a no-op without the degree 10 items).
	item_rows = {r[2]: r for p in root.glob("itemdata_*.txt") for r in tables.table_rows(p.read_bytes()) if len(r) > d10.PRICE_COLUMN and r[0] == "1"}
	stock, stock_summary = d10.merge_d10_stock({name: (root / name).read_bytes() for name in d10.NAMES}, item_rows)
	for name, raw in stock.items():
		replace_file(root / name, raw)
	summary["degree10"] = stock_summary
	# Band consumables and degree 10/11 alchemy (#1748), on the post-degree-10 tables.
	supplied, supplies_summary = supplies.merge_supplies({name: (root / name).read_bytes() for name in d10.NAMES}, item_rows)
	for name, raw in supplied.items():
		replace_file(root / name, raw)
	summary["supplies"] = supplies_summary
	# Degree 11 grade A gear (#1748 PR 2, owner-approved), on the same stores.
	geared, gear_summary = d11.merge_d11_stock({name: (root / name).read_bytes() for name in d10.NAMES}, item_rows)
	for name, raw in geared.items():
		replace_file(root / name, raw)
	summary["degree11"] = gear_summary
	labels, count = npc_map_labels((root / "worldmap_mapinfo.txt").read_bytes(), (root / "npcpos.txt").read_bytes(), characters, (root / "worldmap_localinfo.txt").read_bytes())
	replace_file(root / "worldmap_localinfo.txt", labels)
	summary["mapLabels"] = count
	return summary
