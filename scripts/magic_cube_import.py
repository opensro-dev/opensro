"""
===========================================================================

magic_cube_import.py - the mall Magic Cube rows (port-only, issue #3906).

Port-only, not native. Off unless SRO_MAGIC_CUBE is "1", "on" or "true";
an unset or off build is byte-identical to today.

v1.150 has no Magic Cube. The later client's sixteen mall rows
(ITEM_PRE_MALL_MAGIC_CUBE_{WEAPON,ARMOR,ACCESSARY,SHIELD} and the _10 /
_20 / _30 sizes — the pinned v1.696 Media.pk2 names; OriDB's
ITEM_MALL_MAGIC_CUBE_* spelling is a different dump) are TID 3/2/3/1 with
Param9 capacity (10/20/30/50) and Param10 category filter (1 weapon /
2 armor / 4 accessory / 8 shield). This import stages exactly those rows
from the pinned v1.696 client (SRO_V1696_ROOT, v1696_archive_pins.json),
byte for byte except their identities: the later client's numeric ids are
its own, so each row takes a fresh identity above every published row,
remembered in the marker so a rebuild never moves an item. Icons come
with the rows (cap110 copy_asset_closure, as the Nasrun and reset-scroll
imports do). Names and descriptions come from the later client's text
tables (cap110_items.merge_texts).

A switch off removes exactly what the marker records: the shard, its
manifest line, the name rows it added, the later icon files and the
marker itself.

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

FLAG = "SRO_MAGIC_CUBE"
ROOT_ENV = "SRO_V1696_ROOT"
MARKER = ".opensro-magic-cubes.json"
# The 190002 shard sits beside gears (190000) and Nasrun (190001); one
# importer owns one shard file (a layer's off path unlinks its shard).
ITEM_SHARD = "itemdata_190002.txt"
NAME_TABLE = "textdataname.txt"
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA
CUBE_TYPE = ["3", "2", "3", "1"]
# Param9 (capacity) and Param10 (category filter) in the itemdata layout.
PARAM_CAPACITY = 134
PARAM_FILTER = 136
CAPACITIES = (10, 20, 30, 50)
# Filter bits the later client authors: one category per cube.
FILTER_WEAPON = 1
FILTER_ARMOR = 2
FILTER_ACCESSARY = 4
FILTER_SHIELD = 8
FILTERS = (FILTER_WEAPON, FILTER_ARMOR, FILTER_ACCESSARY, FILTER_SHIELD)

# (codename, capacity, filter), in the later client's display order:
# the four 50-slot cubes, then 10 / 20 / 30.
CUBES = (
	("ITEM_PRE_MALL_MAGIC_CUBE_WEAPON", 50, FILTER_WEAPON),
	("ITEM_PRE_MALL_MAGIC_CUBE_ARMOR", 50, FILTER_ARMOR),
	("ITEM_PRE_MALL_MAGIC_CUBE_ACCESSARY", 50, FILTER_ACCESSARY),
	("ITEM_PRE_MALL_MAGIC_CUBE_SHIELD", 50, FILTER_SHIELD),
	("ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_10", 10, FILTER_WEAPON),
	("ITEM_PRE_MALL_MAGIC_CUBE_ARMOR_10", 10, FILTER_ARMOR),
	("ITEM_PRE_MALL_MAGIC_CUBE_ACCESSARY_10", 10, FILTER_ACCESSARY),
	("ITEM_PRE_MALL_MAGIC_CUBE_SHIELD_10", 10, FILTER_SHIELD),
	("ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_20", 20, FILTER_WEAPON),
	("ITEM_PRE_MALL_MAGIC_CUBE_ARMOR_20", 20, FILTER_ARMOR),
	("ITEM_PRE_MALL_MAGIC_CUBE_ACCESSARY_20", 20, FILTER_ACCESSARY),
	("ITEM_PRE_MALL_MAGIC_CUBE_SHIELD_20", 20, FILTER_SHIELD),
	("ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_30", 30, FILTER_WEAPON),
	("ITEM_PRE_MALL_MAGIC_CUBE_ARMOR_30", 30, FILTER_ARMOR),
	("ITEM_PRE_MALL_MAGIC_CUBE_ACCESSARY_30", 30, FILTER_ACCESSARY),
	("ITEM_PRE_MALL_MAGIC_CUBE_SHIELD_30", 30, FILTER_SHIELD),
)


# ================
# flag_enabled
#
# True only when SRO_MAGIC_CUBE names an on value; unset is native.
# ================
def flag_enabled(env=os.environ):
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON


# ================
# resolve_cube_icon
#
# The itemdata icon cell. When the authored Premium\etc\..._50.ddj is
# absent from Media (the pinned archive only ships that glyph under
# icon/item/etc/), rewrite the cell to item\etc\... so copy_asset_closure
# finds it and the client draws the same path.
# ================
def resolve_cube_icon(icon, media_index):
	authored = icon.replace("/", "\\")
	lookup = shared.normalized_path("icon/" + authored.replace("\\", "/"))
	if lookup in media_index:
		return authored
	fallback = "item\\etc\\" + Path(authored.replace("\\", "/")).name
	fallback_lookup = shared.normalized_path("icon/" + fallback.replace("\\", "/"))
	if fallback_lookup not in media_index:
		raise ValueError(f"Magic Cube icon missing: {authored} (also tried {fallback})")
	return fallback


# ================
# cube_codenames
# ================
def cube_codenames():
	return [code for code, _, _ in CUBES]


# ================
# select_cubes
#
# The sixteen admitted rows of the later item tables. A missing, miscast
# or mislabelled row refuses: a partial import would list cubes the game
# cannot use.
# ================
def select_cubes(later_rows):
	by_code = {}
	for row in later_rows:
		if len(row) > items.CODENAME and row[items.SERVICE] == "1":
			by_code.setdefault(row[items.CODENAME], row)
	selected = []
	for code, capacity, category in CUBES:
		row = by_code.get(code)
		if row is None:
			raise ValueError(f"the later client has no Magic Cube {code}")
		if len(row) != items.ITEM_COLUMNS or row[items.TYPE_FIRST:items.TYPE_FOURTH + 1] != CUBE_TYPE:
			raise ValueError(f"Magic Cube {code} is not a 3/2/3/1 row")
		if int(row[PARAM_CAPACITY]) != capacity:
			raise ValueError(f"Magic Cube {code} names capacity {row[PARAM_CAPACITY]}, want {capacity}")
		if int(row[PARAM_FILTER]) != category:
			raise ValueError(f"Magic Cube {code} names filter {row[PARAM_FILTER]}, want {category}")
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
# add_magic_cubes
#
# Stage the sixteen rows in their shard under marker-kept identities,
# with their icons and texts, or (switch off, or no later client) reap a
# previous import. The variant-side entry the Alexandria builder calls
# through variant_later_layers.
# ================
def add_magic_cubes(native, output, v1696_root=None):
	native, output = Path(native).resolve(), Path(output).resolve()
	if trees_overlap(native, output):
		raise ValueError("Magic Cube overlay must be separate from native extraction")
	marker = output / MARKER
	prior = cap110.read_json(marker) if marker.exists() else {}
	text_root = output / TEXT_ROOT
	shard = safe_target(text_root, ITEM_SHARD)
	manifest = safe_target(text_root, "itemdata.txt")
	name_path = safe_target(text_root, NAME_TABLE)
	# A rebuild, and the off path, start from the table without this
	# import's own name rows.
	nasrun.strip_texts(name_path, set(prior.get("texts", [])))
	if not flag_enabled():
		remove_import(output, prior, marker, shard, manifest)
		return {"enabled": False}
	root = later_world_import.client_root(v1696_root, ROOT_ENV)
	jupiter.require_v1696_pin(root, tuple(shared.FOLDERS))
	codes = cube_codenames()
	shared.refuse_standing_items(text_root, ITEM_SHARD, codes, "Magic Cube")
	with ExitStack() as stack:
		archives, identities = avatars.open_archives(stack, root, avatars.V1696_SOURCE)
		media, index = archives["Media"]

		# ================
		# read_later
		# ================
		read_later = later_tables.Archive.media(media, index)

		later_rows = [row for row in shared.read_item_tables(read_later)
			if len(row) == items.ITEM_COLUMNS and row[items.SERVICE] == "1"]
		cubes = select_cubes(later_rows)
		occupied = published_ids.occupied_objects(text_root, (ITEM_SHARD,))
		allocated = published_ids.allocate(published_ids.OBJECTS, occupied, codes, prior.get("items", {}))
		rows = []
		for cube in cubes:
			row = list(cube)
			row[items.IDENTITY] = str(allocated[cube[items.CODENAME]])
			# The 50-slot rows author Premium\etc\..._50.ddj, but the pinned
			# Media only ships that glyph under icon/item/etc/ (premium/etc
			# has 10/20/30 and the bare name, not _50).
			row[items.ICON] = resolve_cube_icon(row[items.ICON], index)
			rows.append(row)
		later_texts = [row for name in nasrun.LATER_TEXT_TABLES for row in avatars.later_text_rows(read_later, name)]
		base_names = name_path.read_bytes() if name_path.exists() else (native / TEXT_ROOT / NAME_TABLE).read_bytes()
		merged_names, absent_descriptions = items.merge_texts(base_names, later_texts, rows)
		known = {row[1] for row in tables.table_rows(base_names) if len(row) > 1}
		added_texts = sorted({row[1] for row in tables.table_rows(merged_names) if len(row) > 1} - known)
		assets = shared.copy_asset_closure(rows, native, output, archives, worn=False)
	# A switch turned off since the last build leaves its later icons.
	kept_paths = {(record["archive"], record["path"]) for record in assets}
	remove_assets(output, [record for record in prior.get("assets", [])
		if (record["archive"], record["path"]) not in kept_paths])
	rows.sort(key=lambda row: int(row[items.IDENTITY]))
	write_atomically(shard, cap110.encode_rows(rows))
	write_atomically(name_path, merged_names)
	current = manifest.read_bytes() if manifest.exists() else (native / TEXT_ROOT / "itemdata.txt").read_bytes()
	names = [row[0] for row in tables.table_rows(current) if row]
	names = [name for name in names if sro_pk2.fold_ascii(name) != ITEM_SHARD] + [ITEM_SHARD]
	write_atomically(manifest, cap110.encode_rows([[name] for name in names]))
	summary = {
		"enabled": True, "format": "opensro-magic-cubes-v1", "portOnly": True, "source": "v1696",
		"laterArchiveHashes": identities,
		"items": {row[items.CODENAME]: int(row[items.IDENTITY]) for row in rows},
		"texts": added_texts, "absentDescriptions": absent_descriptions, "assets": assets,
		"limits": [
			"Deposit, withdraw, mall listing, sell and consignment refusals need SRO_MAGIC_CUBE on the server too.",
			"No drop or event reward is added; the rows arrive by mall purchase or GM grant alone.",
		],
	}
	write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode())
	return summary
