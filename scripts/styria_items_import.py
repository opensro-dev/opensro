"""
===========================================================================

styria_items_import.py - the Styria Clash reward items (port-only).

Port-only, not native. Off unless SRO_STYRIA_CLASH is "1", "on" or
"true", the switch the arena map (styria_import.py) and the server's
match rules (#3648) already read.

The match rewards name four items the v1.150 client never had (the
win's two rune seals, and the blue and black stones every result
pays), so with the switch on the server's grant lookup found no row
and skipped the item half (#4375). The v1.696 client authors all
four (itemdata_35050/35150), and this import stages them the way the
reset scrolls stage theirs: each row is its pinned v1.696 row byte
for byte but its identity, which published_ids allocates above every
object row of the tree and the marker keeps, and its placeholder
name cell, which reads xxx as the Nasrun rows leave theirs. The
English names and descriptions are the later client's own, merged
into the name table by cap110_items.merge_texts; the icons and the
drop model ride the cap110 asset closure. The later client's
ITEM_PRE_* and ITEM_EVENT_* copies of the seals name the same
texts but are other codenames nothing grants, so they stay out.

A switch off removes exactly what the marker records: the shard,
its manifest line, the merged texts, the later icon files and the
marker. The shard is this import's own (itemdata_230001.txt, the
Styria band the arena map's character and skill shards open at
230000): one importer owns one shard.

===========================================================================
"""
import json
import os
from contextlib import ExitStack
from pathlib import Path

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
import later_tables
from prepare_client_resources import safe_target, write_atomically
from sro_paths import trees_overlap

FLAG = "SRO_STYRIA_CLASH"
ROOT_ENV = "SRO_V1696_ROOT"
MARKER = ".opensro-styria-items.json"
TEXT_ROOT = shared.TEXT_ROOT
NAME_TABLE = "textdataname.txt"
# The Styria band's item shard: styria_import owns the 230000
# character and skill shards, and no other importer writes this file.
ITEM_SHARD = "itemdata_230001.txt"
# The later text tables that carry item names (nasrun merges the same
# three).
LATER_TEXT_TABLES = ("textdata_equip&skill.txt", "textdata_object.txt", "textdata_new.txt")

SEAL_TYPE = ["3", "3", "9", "0"]
STONE_TYPE = ["3", "3", "11", "12"]

# The reward items and the type each must carry, in the reward
# table's order (world/styria): the win's seals first, then the two
# stones every result pays. The v1.696 client's ids are 35135/35141
# (seals) and 35025/35026 (stones).
ITEMS = (
	("ITEM_MALL_TRADE_MAGICRUNE_SEAL", SEAL_TYPE),
	("ITEM_MALL_TRADE_STRENTHRUNE_SEAL", SEAL_TYPE),
	("ITEM_TRADE_BLUESTONE", STONE_TYPE),
	("ITEM_TRADE_BLACKSTONE", STONE_TYPE),
)


# ================
# flag_enabled
# ================
def flag_enabled(env=os.environ):
	"""True only when SRO_STYRIA_CLASH names an on value; unset is native."""
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON


# ================
# select_rows
#
# The admitted rows of the later item tables, in ITEMS order. A
# missing or miscast row refuses: half the reward would stage items
# the grant then pays unevenly. Every row must name a drop model
# and an icon; a worn model would mean the source is not the client
# this was read from.
# ================
def select_rows(later_rows):
	by_code = {}
	for row in later_rows:
		if len(row) > items.CODENAME and row[items.SERVICE] == "1":
			by_code.setdefault(row[items.CODENAME], row)
	selected = []
	for code, kind in ITEMS:
		row = by_code.get(code)
		if row is None:
			raise ValueError(f"the later client has no Styria reward row {code}")
		if len(row) != items.ITEM_COLUMNS or row[items.TYPE_FIRST:items.TYPE_FOURTH + 1] != kind:
			raise ValueError(f"Styria reward row {code} is not of type {'/'.join(kind)}")
		if row[items.MODEL] not in ("", "0", items.UNUSED):
			raise ValueError(f"Styria reward row {code} names a worn model ({row[items.MODEL]!r})")
		for column in (items.DROP_MODEL, items.ICON):
			if row[column] in ("", "0", items.UNUSED):
				raise ValueError(f"Styria reward row {code} has no resource at column {column}")
		selected.append(row)
	return selected


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
# The off path: the shard, its manifest line, the merged texts, the
# later icon files the marker records and the marker.
# ================
def remove_import(output, prior, marker, shard, manifest):
	nasrun.strip_texts(safe_target(output / TEXT_ROOT, NAME_TABLE), set(prior.get("texts", [])))
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
# add_styria_items
#
# The item pass the Alexandria builder runs after the arena map:
# stage the four reward rows in their shard under marker-kept
# identities, their name texts and their resources, or (switch off)
# reap a previous import. Port-only, not native.
# ================
def add_styria_items(native, output, v1696_root=None):
	native, output = Path(native).resolve(), Path(output).resolve()
	if trees_overlap(native, output):
		raise ValueError("Styria items output must be separate from native extraction")
	marker = output / MARKER
	prior = cap110.read_json(marker) if marker.exists() else {}
	text_root = output / TEXT_ROOT
	shard = safe_target(text_root, ITEM_SHARD)
	manifest = safe_target(text_root, "itemdata.txt")
	if not flag_enabled():
		remove_import(output, prior, marker, shard, manifest)
		return {"enabled": False}
	root = later_world_import.client_root(v1696_root, ROOT_ENV)
	jupiter.require_v1696_pin(root, tuple(shared.FOLDERS))
	# A rebuild starts from the tables without this import's own rows.
	nasrun.strip_texts(safe_target(text_root, NAME_TABLE), set(prior.get("texts", [])))
	with ExitStack() as stack:
		archives, identities = avatars.open_archives(stack, root, avatars.V1696_SOURCE)
		media, index = archives["Media"]

		# ================
		# read_later
		# ================
		read_later = later_tables.Archive.media(media, index)

		rows = select_rows(shared.read_item_tables(read_later))
		codes = [row[items.CODENAME] for row in rows]
		for path in sorted(text_root.glob("itemdata*.txt")):
			if sro_pk2.fold_ascii(path.name) in (ITEM_SHARD, "itemdata.txt"):
				continue
			for row in tables.table_rows(path.read_bytes()):
				if len(row) > items.CODENAME and row[items.SERVICE] == "1" and row[items.CODENAME] in codes:
					raise ValueError(f"Styria reward row {row[items.CODENAME]} already stands in the item tables")
		# Identities stand above every object row of the tree, items and
		# characters alike (#3994).
		occupied = published_ids.occupied_objects(text_root, (ITEM_SHARD,))
		allocated = published_ids.allocate(published_ids.OBJECTS, occupied, codes, prior.get("items", {}))
		staged = []
		for row in rows:
			converted = list(row)
			converted[items.IDENTITY] = str(allocated[row[items.CODENAME]])
			converted[items.PLACEHOLDER_NAME] = items.UNUSED
			staged.append(converted)
		later_texts = [row for name in LATER_TEXT_TABLES for row in avatars.later_text_rows(read_later, name)]
		name_path = safe_target(text_root, NAME_TABLE)
		base_names = name_path.read_bytes() if name_path.exists() else (native / TEXT_ROOT / NAME_TABLE).read_bytes()
		merged_names, absent_descriptions = items.merge_texts(base_names, later_texts, staged)
		known = {row[1] for row in tables.table_rows(base_names) if len(row) > 1}
		added_texts = sorted({row[1] for row in tables.table_rows(merged_names) if len(row) > 1} - known)
		assets = shared.copy_asset_closure(staged, native, output, archives, worn=False)
	# A previous build's later icons this pass did not copy again go.
	kept_paths = {(record["archive"], record["path"]) for record in assets}
	remove_assets(output, [record for record in prior.get("assets", [])
		if (record["archive"], record["path"]) not in kept_paths])
	staged.sort(key=lambda row: int(row[items.IDENTITY]))
	write_atomically(shard, cap110.encode_rows(staged))
	write_atomically(name_path, merged_names)
	current = manifest.read_bytes() if manifest.exists() else (native / TEXT_ROOT / "itemdata.txt").read_bytes()
	names = [row[0] for row in tables.table_rows(current) if row]
	names = [name for name in names if sro_pk2.fold_ascii(name) != ITEM_SHARD] + [ITEM_SHARD]
	write_atomically(manifest, cap110.encode_rows([[name] for name in names]))
	summary = {
		"enabled": True, "format": "opensro-styria-items-v1", "portOnly": True, "source": "v1696",
		"laterArchiveHashes": identities,
		"items": {row[items.CODENAME]: int(row[items.IDENTITY]) for row in staged},
		"texts": added_texts, "absentDescriptions": absent_descriptions, "assets": assets,
		"limits": [
			"The Styria Clash reward items (#3648): granted by the match reward while "
			"SRO_STYRIA_CLASH is on; no shop lists them and nothing drops them.",
		],
	}
	write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode())
	return summary
