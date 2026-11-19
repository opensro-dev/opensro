"""
===========================================================================

cap110_quest_items.py - the Blue Berserk chains' quest items

Port-only, not native. The Alexandria chains of the Blue Berserk line (quests
QNO_SD_HW_002 to QNO_SD_HW_024, levels 105 and 110) hand out and collect 18
quest items (ITEM_QNO_SD_HW_*: fourteen ostracon items and four inscriptions)
that the native v1.150 item table lacks. This tool imports their rows from the
later (VSRO) client into an item shard of their own and merges their names.

The items are plain game data of the cap-110 expansion, so they follow the
item switch and the level cap, not the quest switch: they are built when
SRO_CAP110_ITEMS is on and SRO_LEVEL_CAP is 110, whether or not the quests
themselves are (SRO_CAP110_QUESTS owns only the availability of the quests).
Off, the tool removes what a previous on build left, so a reused variant
cannot retain it.

A quest item is a type 3/3/9/0 row. Its model (drop_ch_quest.bsr) and icons
(E060411_EGG.ddj, scroll_return_01.ddj) are in the v1.150 tree, so no resource
is copied; a row whose resources are missing would be copied from the later
archives by the shared asset closure. Rows keep no later-client identity: each
takes a fresh one above every known identity, remembered in the marker so a
rebuild never moves an item. Nothing here adds a drop, a shop or a quest.

===========================================================================
"""
import argparse
from contextlib import ExitStack
import json
import mmap
import os
from pathlib import Path

import alexandria_npcs as characters
import build_alexandria_variant as variant
import cap110_item_import as item_import
import cap110_items as items
import cap110_monsters as monsters
import cap110_rewards as rewards
import import_vsro_alexandria as tables
import sro_pk2
from prepare_client_resources import safe_target, write_atomically

MARKER = ".opensro-cap110-quest-items.json"
OUTPUT_SHARD = "itemdata_110002.txt"
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA
FORMAT = "opensro-cap110-quest-items-v1"
# The codename prefixes this tool imports, and the one item kind they share.
ITEM_PREFIXES = ("ITEM_QNO_SD_HW_",)
QUEST_ITEM_KIND = ("3", "3", "9", "0")


# ================
# enabled
#
# The item switch and the expansion level cap; the quest switch is not asked.
# ================
def enabled(env=os.environ):
	cap = env.get(variant.LEVEL_CAP_FLAG, "").strip().lower()
	return item_import.enabled(env) and cap in variant.LEVEL_CAP_EXPANSION_VALUES


# ================
# select_quest_items
#
# The later client's rows of the quest items the native table lacks, in
# codename order. A native row wins. A row of another kind, or a link to an item
# the native table lacks, refuses rather than shipping a different item. An
# empty selection refuses too: the later client is expected to hold them.
# ================
def select_quest_items(later, native):
	selected = []
	for code in sorted(later):
		if not code.startswith(ITEM_PREFIXES) or code in native:
			continue
		row = later[code]
		if row[items.SERVICE] != "1":
			continue
		if tuple(row[items.TYPE_FIRST:items.TYPE_FOURTH + 1]) != QUEST_ITEM_KIND:
			raise ValueError("quest item is not a quest item kind: " + code)
		for column in range(items.PARAM_FIRST, items.PARAM_LAST, items.PARAM_STRIDE):
			link = row[column + 1]
			if link.startswith("ITEM_") and link not in native:
				raise ValueError(f"quest item links an item the native table lacks: {code} -> {link}")
		selected.append(row)
	if not selected:
		raise ValueError("the later client holds none of the Blue Berserk quest items")
	return selected


# ================
# remove_owned
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
# add_quest_items
# ================
def add_quest_items(vsro_root, native, output):
	native, output = Path(native).resolve(), Path(output).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("quest item overlay must be separate from native extraction")
	text_root, marker = output / TEXT_ROOT, output / MARKER
	prior = json.loads(marker.read_text(encoding="utf-8")) if marker.exists() else {}
	shard = safe_target(text_root, OUTPUT_SHARD)
	if shard.exists():
		if not marker.exists():
			raise ValueError("output already owns an unmarked quest item shard")
		if sro_pk2.digest(shard.read_bytes()) != prior["shardSha256"]:
			raise ValueError("refusing to replace a locally changed quest item shard")
	if not enabled():
		if marker.exists():
			remove_owned(text_root, prior)
			marker.unlink()
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
		selected = select_quest_items(later, native_items)
		converted = rewards.convert_rows(selected, rewards.occupied_identities(text_root, later_rows, OUTPUT_SHARD),
			{code: entry["target"] for code, entry in prior.get("idMap", {}).items()})
		text_path = safe_target(text_root, "textdataname.txt")
		later_texts = [row for name in ("textdata_equip&skill.txt", "textdata_object.txt", "textdata_new.txt")
			for row in tables.table_rows(read_later(name))]
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
		write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode("utf-8"))
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
	# Off, add_quest_items only removes its owned outputs and reads no archive.
	if enabled():
		tables.require_vsro_pin(args.vsro_root, tuple(item_import.FOLDERS))
	summary = add_quest_items(args.vsro_root, args.native_extracted, args.output)
	print(json.dumps({key: value for key, value in summary.items() if key not in ("assets", "idMap")}, indent=2))


if __name__ == "__main__":
	main()
