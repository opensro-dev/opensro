"""
===========================================================================

nasrun_import.py - the Devil's Spirit (Nasrun) item rows, inert (#3348).

Port-only, not native. Off unless SRO_NASRUN is "1", "on" or "true";
an unset or off build is byte-identical to one without this import.

The later clients' Devil's Spirit is a wearable of type 3/1/14 (v1.150
knows avatars of type 3/1/13 only), with an awakening clock, an
extension gear that lengthens it and an alchemy line that upgrades it
(#3348). This is the first step of that strand: it stages the item rows
and their resources from the VSRO client, so the mechanics can follow
without another data pass. Nothing here gives the rows behaviour:

- the wearables (3/1/14/1) fit no equipment or avatar socket the port
  admits today, so they can be carried but not worn;
- the extension gear (3/3/13/16) and the four Nasrun stones and recipe
  (3/3/11/5, 3/3/11/3) match no use or alchemy handler;
- Sabakun's jewel (3/3/10/1) has an elixir's type, but its parameters
  admit only item class 14, the Spirit itself, so reinforcing any v1.150
  item with it is refused.

The rows arrive by GM grant only: no drop, no event reward, no mall
listing (the listing waits for the mechanics, #3348 step 5).

Left out on purpose: the event copies (ITEM_EVENT_*), which belong to
events this port does not have, and the Umdu magic stone, whose option
(MATTR_NASRUN_UMDU) has no row in the v1.150 magicoption table.

The Spirit has no worn model: every 3/1/14 row of the VSRO client
carries only a drop model and an icon, so there is no body geometry to
import. The mall avatar import's "absent resources" note on these rows
was that missing worn column, not a missing source.

A switch off removes exactly what the marker records: the shard, its
manifest line, the name texts it added and the marker itself. Copied
resources stay, as the other imports leave theirs: no row names them.

===========================================================================
"""
import json
from contextlib import ExitStack
import os
from pathlib import Path

import alexandria_npcs as text_tables
import cap110_item_import as shared
import cap110_items as items
import cap110_monsters as cap110
import import_vsro_alexandria as tables
import mall_avatar_import as avatars
import sro_pk2
from prepare_client_resources import safe_target, write_atomically

FLAG = "SRO_NASRUN"
MARKER = ".opensro-nasrun.json"
ITEM_SHARD = "itemdata_190001.txt"
TEXT_ROOT = shared.TEXT_ROOT
NAME_TABLE = "textdataname.txt"
# The VSRO text tables that carry item names (mall_avatar_import reads
# the same three).
LATER_TEXT_TABLES = ("textdata_equip&skill.txt", "textdata_object.txt", "textdata_new.txt")

WEARABLE_TYPE = ["3", "1", "14", "1"]
EXTENSION_TYPE = ["3", "3", "13", "16"]
JEWEL_TYPE = ["3", "3", "10", "1"]
RECIPE_TYPE = ["3", "3", "11", "3"]
STONE_TYPE = ["3", "3", "11", "5"]

# The admitted rows and the type each must carry, in import order. The
# VSRO client's ids are 24405-24412 and 24415-24422.
ROWS = (
	("ITEM_MALL_AVATAR_M_NASRUN", WEARABLE_TYPE),
	("ITEM_MALL_AVATAR_W_NASRUN", WEARABLE_TYPE),
	("ITEM_MALL_AVATAR_M_NASRUN_YELLOW", WEARABLE_TYPE),
	("ITEM_MALL_AVATAR_W_NASRUN_YELLOW", WEARABLE_TYPE),
	("ITEM_MALL_AVATAR_M_NASRUN_BLUE", WEARABLE_TYPE),
	("ITEM_MALL_AVATAR_W_NASRUN_BLUE", WEARABLE_TYPE),
	("ITEM_MALL_AVATAR_M_NASRUN_UNIQUE", WEARABLE_TYPE),
	("ITEM_MALL_AVATAR_W_NASRUN_UNIQUE", WEARABLE_TYPE),
	("ITEM_MALL_NASRUN_EXTENSION", EXTENSION_TYPE),
	("ITEM_ETC_NASRUN_ARCHEMY_UPGRADE_STONE", JEWEL_TYPE),
	("ITEM_ETC_NASRUN_ARCHEMY_RECIPE", RECIPE_TYPE),
	("ITEM_ETC_NASRUN_ARCHEMY_STONE_01", STONE_TYPE),
	("ITEM_ETC_NASRUN_ARCHEMY_STONE_02", STONE_TYPE),
	("ITEM_ETC_NASRUN_ARCHEMY_STONE_03", STONE_TYPE),
	("ITEM_ETC_NASRUN_ARCHEMY_STONE_04", STONE_TYPE),
)

# The jewel's elixir parameters (Params[0] and Params[4]) list the item
# classes it reinforces, one per byte (alchemy.elixirClasses). Class 14
# is the Spirit's; a jewel naming any other class could reinforce a
# v1.150 item and would no longer be inert.
SPIRIT_CLASS = 14
ELIXIR_CLASS_PARAMS = (items.PARAM_FIRST, items.PARAM_FIRST + 4 * items.PARAM_STRIDE)


# ================
# flag_enabled
# ================
def flag_enabled(env=os.environ):
	"""True only when SRO_NASRUN names an on value; unset is native."""
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON


# ================
# jewel_classes
#
# The item classes an elixir row admits: the non-zero bytes of its two
# class parameters, read as the server reads them.
# ================
def jewel_classes(row):
	classes = set()
	for column in ELIXIR_CLASS_PARAMS:
		value = int(row[column]) & 0xFFFFFFFF
		for shift in range(0, 32, 8):
			if value >> shift & 0xFF:
				classes.add(value >> shift & 0xFF)
	return classes


# ================
# select_rows
#
# The admitted rows of the later item tables, in ROWS order. A missing
# or miscast row refuses: half the family would stage items the later
# steps cannot pair. Every row must name a drop model and an icon; a
# worn model would mean the source is not the client this was read
# from.
# ================
def select_rows(later_rows):
	by_code = {}
	for row in later_rows:
		if len(row) > items.CODENAME and row[items.SERVICE] == "1":
			by_code.setdefault(row[items.CODENAME], row)
	selected = []
	for code, kind in ROWS:
		row = by_code.get(code)
		if row is None:
			raise ValueError(f"the later client has no Nasrun row {code}")
		if len(row) != items.ITEM_COLUMNS or row[items.TYPE_FIRST:items.TYPE_FOURTH + 1] != kind:
			raise ValueError(f"Nasrun row {code} is not of type {'/'.join(kind)}")
		if row[items.MODEL] not in ("", "0", items.UNUSED):
			raise ValueError(f"Nasrun row {code} names a worn model ({row[items.MODEL]!r})")
		for column in (items.DROP_MODEL, items.ICON):
			if row[column] in ("", "0", items.UNUSED):
				raise ValueError(f"Nasrun row {code} has no resource at column {column}")
		if kind is JEWEL_TYPE and jewel_classes(row) != {SPIRIT_CLASS}:
			raise ValueError(f"Nasrun jewel {code} reinforces classes {sorted(jewel_classes(row))}, want only {SPIRIT_CLASS}")
		selected.append(row)
	return selected


# ================
# strip_texts
#
# Drops the name-table rows a previous on build added, by key. Every
# other line keeps its own ending: the shipped table mixes CRLF and LF
# rows, and the off build must give back the table's exact bytes.
# ================
def strip_texts(path, keys):
	if not keys or not path.exists():
		return
	lines = tables.decode_table(path.read_bytes()).lstrip("﻿").splitlines(keepends=True)
	kept = [line for line in lines if not (len(line.split("\t")) > 1 and line.split("\t")[1] in keys)]
	if len(kept) != len(lines):
		write_atomically(path, b"\xff\xfe" + "".join(kept).encode("utf-16-le"))


# ================
# remove_import
#
# The off path: the shard, its manifest line, the texts the marker
# records and the marker.
# ================
def remove_import(output, prior, marker, shard, manifest):
	strip_texts(safe_target(output / TEXT_ROOT, NAME_TABLE), set(prior.get("texts", [])))
	if shard.exists():
		shard.unlink()
	if manifest.exists():
		names = [row[0] for row in tables.table_rows(manifest.read_bytes()) if row]
		kept = [name for name in names if sro_pk2.fold_ascii(name) != ITEM_SHARD]
		if len(kept) != len(names):
			write_atomically(manifest, cap110.encode_rows([[name] for name in kept]))
	marker.unlink(missing_ok=True)


# ================
# add_nasrun
#
# The item pass the Alexandria builder runs after the mall avatars:
# stage the rows in their shard under marker-kept identities, their
# name texts and their resources, or (switch off, or no VSRO client)
# reap a previous import.
# ================
def add_nasrun(vsro_root, native, output):
	native, output = Path(native).resolve(), Path(output).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("Nasrun output must be separate from the native extraction")
	marker = output / MARKER
	prior = cap110.read_json(marker) if marker.exists() else {}
	text_root = output / TEXT_ROOT
	shard = safe_target(text_root, ITEM_SHARD)
	manifest = safe_target(text_root, "itemdata.txt")
	if not flag_enabled():
		remove_import(output, prior, marker, shard, manifest)
		return {"enabled": False}
	if not avatars.source_present(vsro_root, avatars.VSRO_SOURCE):
		remove_import(output, prior, marker, shard, manifest)
		return {"enabled": False, "skipped": "no VSRO client found"}
	# A rebuild starts from the tables without this import's own rows.
	strip_texts(safe_target(text_root, NAME_TABLE), set(prior.get("texts", [])))
	with ExitStack() as stack:
		archives, identities = avatars.open_archives(stack, vsro_root, avatars.VSRO_SOURCE)
		media, index = archives["Media"]

		# ================
		# read_later
		# ================
		def read_later(name):
			return tables.table_text(media, index, name)

		rows = select_rows(shared.read_item_tables(read_later))
		codes = [row[items.CODENAME] for row in rows]
		# Identities stand above every published row of the merged item
		# tables (the variant's accumulated shards), as the gears stones'.
		occupied = set()
		for path in sorted(text_root.glob("itemdata*.txt")):
			if sro_pk2.fold_ascii(path.name) in (ITEM_SHARD, "itemdata.txt"):
				continue
			for row in tables.table_rows(path.read_bytes()):
				if len(row) > items.CODENAME and row[items.SERVICE] == "1":
					occupied.add(int(row[items.IDENTITY]))
					if row[items.CODENAME] in codes:
						raise ValueError(f"Nasrun row {row[items.CODENAME]} already stands in the item tables")
		allocated = cap110.allocate_identities(occupied, codes, prior.get("items", {}))
		staged = []
		for row in rows:
			converted = list(row)
			converted[items.IDENTITY] = str(allocated[row[items.CODENAME]])
			converted[items.PLACEHOLDER_NAME] = items.UNUSED
			staged.append(converted)
		later_texts = [row for name in LATER_TEXT_TABLES for row in avatars.later_text_rows(media, index, name)]
		name_path = safe_target(text_root, NAME_TABLE)
		base_names = name_path.read_bytes() if name_path.exists() else (native / TEXT_ROOT / NAME_TABLE).read_bytes()
		merged_names, absent_descriptions = items.merge_texts(base_names, later_texts, staged)
		known = {row[1] for row in tables.table_rows(base_names) if len(row) > 1}
		added_texts = sorted({row[1] for row in tables.table_rows(merged_names) if len(row) > 1} - known)
		assets = shared.copy_asset_closure(staged, native, output, archives, worn=False)
	staged.sort(key=lambda row: int(row[items.IDENTITY]))
	write_atomically(shard, cap110.encode_rows(staged))
	write_atomically(name_path, merged_names)
	current = manifest.read_bytes() if manifest.exists() else (native / TEXT_ROOT / "itemdata.txt").read_bytes()
	names = [row[0] for row in tables.table_rows(current) if row]
	names = [name for name in names if sro_pk2.fold_ascii(name) != ITEM_SHARD] + [ITEM_SHARD]
	write_atomically(manifest, cap110.encode_rows([[name] for name in names]))
	summary = {
		"enabled": True, "format": "opensro-nasrun-v1", "portOnly": True, "source": "vsro",
		"laterArchiveHashes": identities,
		"items": {row[items.CODENAME]: int(row[items.IDENTITY]) for row in staged},
		"texts": added_texts, "absentDescriptions": absent_descriptions, "assets": assets,
		"limits": [
			"Inert: no socket, use, alchemy or mall listing admits these rows yet (#3348).",
			"No drop or event reward is added; the rows arrive by GM grant alone.",
			"Event copies and the Umdu magic stone (MATTR_NASRUN_UMDU has no v1.150 option row) are not imported.",
		],
	}
	write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode())
	return summary
