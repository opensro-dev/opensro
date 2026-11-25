"""
===========================================================================

reset_scroll_import.py - the Item Mall reset scrolls' v1.696 items

Port-only, not native (owner, 2026-11-21). Each reset scroll the server
sells behind its own switch (SRO_STAT_RESET_SCROLL, SRO_SKILL_RESET_SCROLL;
the server's commerce/resetscrolls.go) is the later client's own item:
v1.150 has neither. Every switch off leaves the tree byte-identical.

  ITEM_MALL_STATS_POINT_RECALL_ALL  3/3/13/13, Param1 2: the native stat
                                    recall family, at every level.
  ITEM_MALL_SKILL_POINT_RECALL_A    3/3/13/13, Param1 5: the skill reset.

A scroll's row is its pinned v1.696 row (SRO_V1696_ROOT,
v1696_archive_pins.json) byte for byte but its identity and, for the
skill scroll, its requirement cells: v1.696 holds it to levels 1-100,
which this import lifts to the stat scroll's level-free cells. Each
takes its identity from published_ids: above every object row of the
tree, and the published one once a release ships it
(data/published-identities.json), so the items players hold keep it; its icon is
copied from the later client (cap110 copy_asset_closure, as the Nasrun
import does) and its English name and description are authored here,
joining textdataname.txt in the v1.150 layout (English in column 8): the
later stat name carries a typo and the skill item has no description.

A switch off removes exactly what that scroll added: its row (the shard
and its manifest line go with the last one), its text rows, the icon
files copied from the later client and, with the last scroll, the marker.

===========================================================================
"""
import json
import os
from contextlib import ExitStack
from pathlib import Path

import alexandria_npcs as characters
import cap110_item_import as shared
import cap110_items as items
import cap110_monsters as cap110
import import_vsro_alexandria as tables
import jupiter_import as jupiter
import later_world_import
import mall_avatar_import as avatars
import nasrun_import as nasrun
import published_ids
import sro_pk2
from prepare_client_resources import safe_target, write_atomically
from sro_paths import trees_overlap

ROOT_ENV = "SRO_V1696_ROOT"
MARKER = ".opensro-reset-scrolls.json"
# The 300000 shard band: no other importer writes this file (one importer
# owns one shard: a layer's off path unlinks its shard).
ITEM_SHARD = "itemdata_300000.txt"
NAME_TABLE = "textdataname.txt"
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA
# The four typed requirement pairs, types at cells 32/34/36/38 and values
# at 33/35/37/39 (server enterworld/itemrefs.go), and Param1
# (itemParam1_29c). LEVEL_FREE is the stat scroll's own cells: no type.
REQUIREMENTS = slice(32, 40)
LEVEL_FREE = ["-1", "0"] * 4
PARAM1 = 118

# Each scroll: (switch, codename, name, description), in the server
# table's order (commerce.ResetScrolls).
SCROLLS = (
	("SRO_STAT_RESET_SCROLL", "ITEM_MALL_STATS_POINT_RECALL_ALL", "Stat Recall (All Levels)",
		"Returns every stat point you have spent: STR and INT go back to their values for your level."),
	("SRO_SKILL_RESET_SCROLL", "ITEM_MALL_SKILL_POINT_RECALL_A", "Scroll of Skill Restore",
		"Returns every skill and mastery point you have spent: your skills are forgotten and every "
		"mastery goes back to 0."),
)


# ================
# enabled_scrolls
#
# The scrolls whose switch names an on value; unset is native.
# ================
def enabled_scrolls(env=os.environ):
	return [scroll for scroll in SCROLLS if env.get(scroll[0], "").strip().lower() in tables.FLAG_ON]


# ================
# text_row
#
# One v1.150 name-table row: service, key, then the language cells with
# the English text in each (English is column 8).
# ================
def text_row(key, text):
	return ["1", key, text, "", "", text, text, text, text, text, ""]


# ================
# remove_assets
#
# The later icon files a marker records, under output. A native-sourced
# copy is left: another layer may have copied the same native file.
# ================
def remove_assets(output, records):
	for record in records:
		if record.get("source") == "later":
			safe_target(output / shared.FOLDERS[record["archive"]], record["path"]).unlink(missing_ok=True)


# ================
# remove_import
#
# The off path: the shard, its manifest line, the later icon files the
# marker records and the marker.
# ================
def remove_import(output, prior, marker, shard, manifest):
	if shard.exists():
		shard.unlink()
	if manifest.exists():
		names = [row[0] for row in tables.table_rows(manifest.read_bytes()) if row]
		kept = [name for name in names if sro_pk2.fold_ascii(name) != ITEM_SHARD]
		if len(kept) != len(names):
			write_atomically(manifest, cap110.encode_rows([[name] for name in kept]))
	remove_assets(output, prior.get("assets", []))
	marker.unlink(missing_ok=True)


# ================
# add_reset_scrolls
#
# Stage the enabled scrolls in their shard under marker-kept identities,
# with their icons and texts, or (every switch off) reap a previous
# import. The variant-side entry the Alexandria builder calls through
# variant_later_layers.
# ================
def add_reset_scrolls(native, output, v1696_root=None):
	native, output = Path(native).resolve(), Path(output).resolve()
	if trees_overlap(native, output):
		raise ValueError("reset scroll overlay must be separate from native extraction")
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
		remove_import(output, prior, marker, shard, manifest)
		return {"enabled": False}
	root = later_world_import.client_root(v1696_root, ROOT_ENV)
	jupiter.require_v1696_pin(root, tuple(shared.FOLDERS))
	codes = [code for _, code, _, _ in scrolls]
	for path in sorted(text_root.glob("itemdata*.txt")):
		if sro_pk2.fold_ascii(path.name) in (ITEM_SHARD, "itemdata.txt"):
			continue
		for row in tables.table_rows(path.read_bytes()):
			if len(row) > items.CODENAME and row[items.SERVICE] == "1" and row[items.CODENAME] in codes:
				raise ValueError(f"reset scroll {row[items.CODENAME]} already stands in the item tables")
	with ExitStack() as stack:
		archives, identities = avatars.open_archives(stack, root, avatars.V1696_SOURCE)
		media, index = archives["Media"]

		# ================
		# read_later
		# ================
		def read_later(name):
			return tables.table_text(media, index, name)

		later = {row[items.CODENAME]: row for row in shared.read_item_tables(read_later)
			if len(row) == items.ITEM_COLUMNS and row[items.SERVICE] == "1"}
		occupied = published_ids.occupied_objects(text_root, (ITEM_SHARD,))
		allocated = published_ids.allocate(published_ids.OBJECTS, occupied, codes, prior.get("items", {}))
		rows = []
		for code in codes:
			if code not in later:
				raise ValueError(f"the v1.696 client has no row {code}")
			row = list(later[code])
			row[items.IDENTITY] = str(allocated[code])
			row[REQUIREMENTS] = LEVEL_FREE
			rows.append(row)
		assets = shared.copy_asset_closure(rows, native, output, archives, worn=False)
	# A switch turned off since the last build leaves its later icons.
	kept_paths = {(record["archive"], record["path"]) for record in assets}
	remove_assets(output, [record for record in prior.get("assets", [])
		if (record["archive"], record["path"]) not in kept_paths])
	rows.sort(key=lambda row: int(row[items.IDENTITY]))
	texts = []
	for _, code, name, description in scrolls:
		texts += [text_row("SN_" + code, name), text_row("SN_" + code + "_TT_DESC", description)]
	base_names = name_path.read_bytes() if name_path.exists() else (native / TEXT_ROOT / NAME_TABLE).read_bytes()
	write_atomically(shard, cap110.encode_rows(rows))
	write_atomically(name_path, characters.append_rows(base_names, texts))
	current = manifest.read_bytes() if manifest.exists() else (native / TEXT_ROOT / "itemdata.txt").read_bytes()
	names = [row[0] for row in tables.table_rows(current) if row]
	names = [name for name in names if sro_pk2.fold_ascii(name) != ITEM_SHARD] + [ITEM_SHARD]
	write_atomically(manifest, cap110.encode_rows([[name] for name in names]))
	summary = {"enabled": True, "format": "opensro-reset-scrolls-v1", "portOnly": True, "source": "v1696",
		"laterArchiveHashes": identities,
		"items": {row[items.CODENAME]: int(row[items.IDENTITY]) for row in rows},
		"texts": [row[1] for row in texts], "assets": assets,
		"limits": [
			"Sold in the Item Mall only while the scroll's own switch is on (commerce authors the listing).",
			"No drop or event reward is added; the rows arrive by mall purchase or GM grant alone.",
		]}
	write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode())
	return summary
