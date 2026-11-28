"""
===========================================================================

arabia_quest_items.py - the Arabia quests' items (#4134)

Port-only, not native. The Arabia collect and delivery quests hand out
and collect 53 quest items (ITEM_QNO_AR_*) that the v1.150 item table
lacks. This tool imports their rows from the v1.696 client into an item
shard of their own and merges their names, on the Blue Berserk quest
items' pattern (cap110_quest_items.py): a quest item is a type 3/3/9/0
row, keeps no later-client identity (each takes a fresh one above every
known identity, remembered in the marker so a rebuild never moves an
item), and a row of another kind, or a link to an item the build does
not hold, refuses rather than shipping a different item.

These are the continent's items, so they follow its switch: SRO_ARABIA,
as one step of arabia_import's layer (add_quest_items takes that layer's
reader, so the archives open once). Off, the tool removes the shard,
its manifest line and its names, so a reused variant cannot retain it.
Nothing here adds a drop or a quest: the collect and delivery specs
follow in the quest package. No retail table is committed to Git.

===========================================================================
"""
import argparse
from contextlib import ExitStack
import json
import mmap
import os
from pathlib import Path

import alexandria_map_tables as map_tables
import alexandria_npcs as characters
import cap110_item_import as item_import
import cap110_items as items
import cap110_monsters as monsters
import cap110_rewards as rewards
import import_vsro_alexandria as tables
import sro_pk2
import later_tables
from prepare_client_resources import safe_target, write_atomically
from sro_paths import MAIN_CHECKOUT_ROOT

MARKER = ".opensro-arabia-quest-items.json"
OUTPUT_SHARD = "itemdata_200001.txt"
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA
FORMAT = "opensro-arabia-quest-items-v1"
FLAG = "SRO_ARABIA"
SOURCE_ENV = "SRO_V1696_ROOT"
ITEM_PREFIX = "ITEM_QNO_AR"
QUEST_ITEM_KIND = ("3", "3", "9", "0")


# ================
# enabled
#
# The continent's switch, as arabia_import.enabled reads it.
# ================
def enabled(env=os.environ):
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON


# ================
# source_root
#
# The staged v1.696 client (SRO_V1696_ROOT, defaulting to inputs/sro_1696
# beside the game root), resolved as skytemple_import.source_root does.
# ================
def source_root(env=os.environ):
	configured = env.get(SOURCE_ENV, "").strip()
	if configured:
		return Path(configured)
	return MAIN_CHECKOUT_ROOT.parent / "inputs" / "sro_1696"


# ================
# select_quest_items
#
# The later client's Arabia quest items the build does not already hold,
# in codename order. A held row wins. A row of another kind, or a link to
# an item the build lacks, refuses rather than shipping a different item.
# An empty selection refuses too: the later client is expected to hold them.
# ================
def select_quest_items(later, held):
	selected = []
	for code in sorted(later):
		if not code.startswith(ITEM_PREFIX) or code in held:
			continue
		row = later[code]
		if row[items.SERVICE] != "1":
			continue
		if tuple(row[items.TYPE_FIRST:items.TYPE_FOURTH + 1]) != QUEST_ITEM_KIND:
			raise ValueError("quest item is not a quest item kind: " + code)
		for column in range(items.PARAM_FIRST, items.PARAM_LAST, items.PARAM_STRIDE):
			link = row[column + 1]
			if link.startswith("ITEM_") and link not in held:
				raise ValueError(f"quest item links an item the build lacks: {code} -> {link}")
		selected.append(row)
	if not selected:
		raise ValueError("the later client holds none of the Arabia quest items")
	return selected


# ================
# remove_owned
#
# What a previous on build left: the shard, its manifest line and the
# merged names.
# ================
def remove_owned(text_root, prior):
	(text_root / OUTPUT_SHARD).unlink(missing_ok=True)
	manifest = text_root / "itemdata.txt"
	if manifest.exists():
		lines = [row[0] for row in tables.table_rows(manifest.read_bytes()) if row and row[0].lower() != OUTPUT_SHARD]
		write_atomically(manifest, b"\xff\xfe" + ("\r\n".join(lines) + "\r\n").encode("utf-16-le"))
	text_path = text_root / "textdataname.txt"
	owned = set(prior.get("nameSymbols", []))
	if owned and text_path.exists():
		kept = [row for row in characters.translation_rows(text_path.read_bytes()) if len(row) < 2 or row[1] not in owned]
		write_atomically(text_path, monsters.encode_rows(kept))


# ================
# remove_quest_items
#
# The layer's off path: the marker's record leaves, and so does it.
# ================
def remove_quest_items(output):
	output = Path(output)
	marker = output / MARKER
	if marker.exists():
		remove_owned(output / TEXT_ROOT, json.loads(marker.read_text(encoding="utf-8")))
		marker.unlink()


# ================
# add_quest_items
#
# read(name) returns one later textdata table of the v1.696 client (the
# Arabia layer's reader, over its already-open Media archive); root is
# that client's folder, for the asset closure's archives; native is the
# native extraction the closure checks resources against.
# ================
def add_quest_items(read, root, native, output):
	output, native = Path(output), Path(native)
	text_root, marker = output / TEXT_ROOT, output / MARKER
	prior = json.loads(marker.read_text(encoding="utf-8")) if marker.exists() else {}
	shard = safe_target(text_root, OUTPUT_SHARD)
	if shard.exists():
		if not marker.exists():
			raise ValueError("output already owns an unmarked Arabia quest item shard")
		if sro_pk2.digest(shard.read_bytes()) != prior["shardSha256"]:
			raise ValueError("refusing to replace a locally changed Arabia quest item shard")
	with ExitStack() as stack:
		archives, identities = {}, {}
		for name in item_import.FOLDERS:
			handle = stack.enter_context(open(Path(root) / f"{name}.pk2", "rb"))
			data = stack.enter_context(mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ))
			archives[name] = (data, {item_import.normalized_path(entry.path): entry for entry in sro_pk2.read_directory(data)})
			identities[name] = tables.sha256_of(data)

		# What the build already holds, our own shard aside (its rows are
		# the ones being replaced, so holding them would empty the
		# selection on a rebuild).
		manifest_names = [row[0].strip() for row in tables.table_rows((text_root / "itemdata.txt").read_bytes())]
		held_rows = [row for name in manifest_names if name.lower() != OUTPUT_SHARD
			for row in tables.table_rows((text_root / name).read_bytes())]
		later_rows = item_import.read_item_tables(read)
		later = items.index_items(later_rows)
		held = items.index_items(held_rows)
		selected = select_quest_items(later, held)
		converted = rewards.convert_rows(selected, rewards.occupied_identities(text_root, later_rows, OUTPUT_SHARD),
			{code: entry["target"] for code, entry in prior.get("idMap", {}).items()})
		text_path = safe_target(text_root, "textdataname.txt")
		# The later name files are sharded like questdata: the base
		# file is a manifest of shard names. textdata_new.txt does not
		# exist in the later client at all, and an optional family must
		# not kill the import, so an absent table reads as no rows (a
		# listed shard the archive lacks still refuses, and a missing
		# NAME refuses in merge_texts below).
		later_name_rows = [
			row for name in ("textdata_equip&skill.txt", "textdata_object.txt", "textdata_new.txt")
			for row in read.rows(name, optional=True)
		]
		if not later_name_rows:
			raise SystemExit("the later client has no item name text to import the quest items under")
		later_texts = [map_tables.later_text_row(row) for row in later_name_rows]
		base_text = text_path.read_bytes()
		merged_text, missing_descriptions = items.merge_texts(base_text, later_texts, converted)
		known = {row[1] for row in characters.translation_rows(base_text) if len(row) > 1}
		added_names = sorted({row[1] for row in characters.translation_rows(merged_text) if len(row) > 1} - known)
		assets = item_import.copy_asset_closure(converted, native, output, archives, worn=False)
		raw_shard = b"\xff\xfe" + characters.table_text(converted).encode("utf-16-le")
		manifest_path = safe_target(text_root, "itemdata.txt")
		manifest_names = [row[0].strip() for row in tables.table_rows(manifest_path.read_bytes())]
		if OUTPUT_SHARD not in map(sro_pk2.fold_ascii, manifest_names):
			manifest_names.append(OUTPUT_SHARD)
		write_atomically(shard, raw_shard)
		write_atomically(text_path, merged_text)
		write_atomically(manifest_path, b"\xff\xfe" + ("\r\n".join(manifest_names) + "\r\n").encode("utf-16-le"))
		summary = {"enabled": True, "format": FORMAT, "portOnly": True,
			"imported": len(converted), "codenames": [row[items.CODENAME] for row in converted],
			"idMap": {row[items.CODENAME]: {"source": int(later[row[items.CODENAME]][items.IDENTITY]),
				"target": int(row[items.IDENTITY])} for row in converted},
			"nameSymbols": sorted(set(prior.get("nameSymbols", [])) | set(added_names)),
			"absentAuthoredDescriptions": missing_descriptions, "assets": assets,
			"laterArchiveHashes": identities, "shardSha256": sro_pk2.digest(raw_shard)}
		write_atomically(marker, (json.dumps(summary, indent=2, ensure_ascii=False) + "\n").encode("utf-8"))
		return summary


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--v1696-root", default=None)
	parser.add_argument("--native-extracted", required=True)
	parser.add_argument("--output", required=True)
	args = parser.parse_args()
	native, output = Path(args.native_extracted).resolve(), Path(args.output).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("quest item overlay must be separate from native extraction")
	if not enabled():
		remove_quest_items(output)
		print(json.dumps({"enabled": False}, indent=2))
		return
	root = Path(args.v1696_root) if args.v1696_root else source_root()
	with open(root / "Media.pk2", "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as media:
		index = tables.archive_index(media)

		read = later_tables.Archive.media(media, index)

		summary = add_quest_items(read, root, native, output)
	print(json.dumps({key: value for key, value in summary.items() if key not in ("assets", "idMap")}, indent=2))


if __name__ == "__main__":
	main()
