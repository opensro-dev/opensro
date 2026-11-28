"""
===========================================================================

switch_scroll_import.py - the Item Mall switch scrolls as items of their own

Port-only, not native (owner, 2026-11-21). Each switch scroll the server
sells behind its own flag (SRO_WEAPON_SWITCH_SCROLL,
SRO_ARMOR_SWITCH_SCROLL, SRO_ACCESSORY_SWITCH_SCROLL; the server's
commerce/equipswitch.go) is an item of its own: v1.150 has none, and the
owner keeps the shipped reinforce scrolls the switches first borrowed
untouched. Every flag off leaves the tree byte-identical to today.

A scroll's row is its donor's v1.150 row (ITEM_MALL_REINFORCE_WEAPON_INC_2_
SCROLL for the weapon scroll, ..._ARMOR_INC_1_SCROLL for the armor one,
..._ACCESSARY_INC_1_SCROLL for the accessory one), byte for byte but its
identity, its codename and its two name symbols: the donor's icon, type
(3/3/13/11), stack and price come along, so the client draws and stacks
it like the scroll whose art it wears. Each takes its identity from
published_ids: above every object row of the tree, and the published one
once a release ships it (data/published-identities.json), so the items
players hold keep it; its English name and description join
textdataname.txt in the v1.150 layout (English in column 8).

A flag off removes exactly what that scroll added: its row (the shard
and its manifest line go with the last one), its text rows and, with
the last scroll, the marker.

===========================================================================
"""
import json
import os
from pathlib import Path

import alexandria_npcs as characters
import cap110_monsters as cap110
import import_vsro_alexandria as tables
import later_tables
import nasrun_import as nasrun
import published_ids
from prepare_client_resources import safe_target, write_atomically
from sro_paths import trees_overlap

MARKER = ".opensro-switch-scrolls.json"
# The 290000 shard band: no other importer writes this file (one importer
# owns one shard: a layer's off path unlinks its shard).
ITEM_SHARD = "itemdata_290000.txt"
NAME_TABLE = "textdataname.txt"
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA
ITEM_COLUMNS = 160

# Each scroll: (flag, codename, donor codename, name, description), in the
# server table's order (commerce.EquipSwitchScrolls).
SCROLLS = (
	("SRO_WEAPON_SWITCH_SCROLL", "ITEM_MALL_WEAPON_SWITCH_SCROLL", "ITEM_MALL_REINFORCE_WEAPON_INC_2_SCROLL",
		"Weapon Switch Scroll",
		"Turns a weapon in your bag into another weapon type of either race, same degree, grade and seal. "
		"Plus and blue options are kept."),
	("SRO_ARMOR_SWITCH_SCROLL", "ITEM_MALL_ARMOR_SWITCH_SCROLL", "ITEM_MALL_REINFORCE_ARMOR_INC_1_SCROLL",
		"Armor Switch Scroll",
		"Turns a piece of armor or a shield in your bag into the same piece of the other race. "
		"Plus and blue options are kept."),
	("SRO_ACCESSORY_SWITCH_SCROLL", "ITEM_MALL_ACCESSORY_SWITCH_SCROLL", "ITEM_MALL_REINFORCE_ACCESSARY_INC_1_SCROLL",
		"Accessory Switch Scroll",
		"Turns an earring, necklace or ring in your bag into the same accessory of the other race. "
		"Plus and blue options are kept."),
)

# The name and description symbol columns of an itemdata row.
NAME_COLUMN = 5
DESCRIPTION_COLUMN = 6


# ================
# enabled_scrolls
#
# The scrolls whose flag names an on value; unset is native.
# ================
def enabled_scrolls(env=os.environ):
	return [scroll for scroll in SCROLLS if env.get(scroll[0], "").strip().lower() in tables.FLAG_ON]


# ================
# name_symbols
# ================
def name_symbols(codename):
	return "SN_" + codename, "SN_" + codename + "_TT_DESC"


# ================
# text_row
#
# One v1.150 name-table row: service, key, then the language cells with
# the English text in each (English is column 8).
# ================
def text_row(key, text):
	return ["1", key, text, "", "", text, text, text, text, text, ""]


# ================
# donor_rows
#
# The tree's item rows by codename, this import's own shard excepted,
# in the order the tree's itemdata index lists them (the first row of a
# codename wins): the output's tables over the native tree's.
# ================
def donor_rows(native, text_root):
	rows = {}
	for row in later_tables.Archive.overlay(native / TEXT_ROOT, text_root).rows("itemdata.txt", excluding=(ITEM_SHARD,)):
		if len(row) > 2 and row[0] == "1":
			rows.setdefault(row[2], row)
	return rows


# ================
# write_manifest
#
# The item manifest with this import's shard listed once, or not at all.
# ================
def write_manifest(manifest, native, listed):
	current = manifest.read_bytes() if manifest.exists() else (native / TEXT_ROOT / "itemdata.txt").read_bytes()
	names = [row[0] for row in tables.table_rows(current) if row]
	kept = [name for name in names if name.lower() != ITEM_SHARD] + ([ITEM_SHARD] if listed else [])
	if kept != names:
		write_atomically(manifest, cap110.encode_rows([[name] for name in kept]))


# ================
# add_switch_scrolls
#
# Stage the enabled scrolls in their shard under marker-kept identities,
# or (every flag off) reap a previous import. The variant-side entry the
# Alexandria builder calls through variant_later_layers.
# ================
def add_switch_scrolls(native, output):
	native, output = Path(native).resolve(), Path(output).resolve()
	if trees_overlap(native, output):
		raise ValueError("switch scroll overlay must be separate from native extraction")
	marker = output / MARKER
	prior = cap110.read_json(marker) if marker.exists() else {}
	text_root = output / TEXT_ROOT
	shard = safe_target(text_root, ITEM_SHARD)
	manifest = safe_target(text_root, "itemdata.txt")
	name_path = safe_target(text_root, NAME_TABLE)
	# A rebuild, and the off path, start from the table without this
	# import's own text rows.
	nasrun.strip_texts(name_path, set(prior.get("texts", [])))
	scrolls = enabled_scrolls()
	if not scrolls:
		shard.unlink(missing_ok=True)
		if manifest.exists():
			write_manifest(manifest, native, False)
		marker.unlink(missing_ok=True)
		return {"enabled": False}
	donors = donor_rows(native, text_root)
	rows = []
	for _, codename, donor, _, _ in scrolls:
		row = donors.get(donor)
		if row is None or len(row) != ITEM_COLUMNS:
			raise ValueError(f"the switch scroll {codename} has no donor row {donor}")
		if codename in donors:
			raise ValueError(f"the switch scroll {codename} already stands in the item tables")
		row = list(row)
		row[2] = codename
		row[NAME_COLUMN], row[DESCRIPTION_COLUMN] = name_symbols(codename)
		rows.append(row)
	occupied = published_ids.occupied_objects(text_root, (ITEM_SHARD,))
	allocated = published_ids.allocate(published_ids.OBJECTS, occupied, [row[2] for row in rows], prior.get("items", {}))
	for row in rows:
		row[1] = str(allocated[row[2]])
	rows.sort(key=lambda row: int(row[1]))
	texts = []
	for _, codename, _, name, description in scrolls:
		name_key, description_key = name_symbols(codename)
		texts += [text_row(name_key, name), text_row(description_key, description)]
	base_names = name_path.read_bytes() if name_path.exists() else (native / TEXT_ROOT / NAME_TABLE).read_bytes()
	write_atomically(shard, cap110.encode_rows(rows))
	write_atomically(name_path, characters.append_rows(base_names, texts))
	write_manifest(manifest, native, True)
	summary = {"enabled": True, "format": "opensro-switch-scrolls-v1", "portOnly": True,
		"items": {row[2]: int(row[1]) for row in rows},
		"donors": {codename: donor for _, codename, donor, _, _ in scrolls},
		"texts": [row[1] for row in texts],
		"limits": [
			"Sold in the Item Mall only while the scroll's own flag is on (commerce authors the listing).",
			"No drop or event reward is added; the rows arrive by mall purchase or GM grant alone.",
		]}
	write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode())
	return summary
