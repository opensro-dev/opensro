"""
===========================================================================

cap110_quest_tables.py - the Blue Berserk chains' quest rows and texts

Port-only, not native. The four Blue Berserk chains (the Army / Military Tests
of levels 95 and 100 for both races, 28 quests, and the Alexandria steps of
levels 105 and 110, 24 quests) are absent from the v1.150 quest tables, and
most of their dialog has no English there. This tool imports, from the later
(VSRO) client:

	- the 52 quest rows (questdata.txt, ids 346 to 666: no id is used in v1.150),
	- their 52 contents rows (questcontentsdata.txt; the later table has four
	  more trailing columns than the v1.150 layout, which are dropped),
	- their 52 rows of the game guide (gameguidedata.txt),
	- the quest strings: names, pay lines, objectives, dialog and the NPC lines
	  (three later files, merged into the one textquest.txt the v1.150 client reads).

The v1.150 text holds many of these keys with a blank English column (the content
never shipped); a blank row is replaced by the later client's whole row, a row that
has English keeps it, absent keys are appended: native text never changes.

Not imported: refqusetreward.txt (a server-side table the v1.150 client does not
have; the quests' rewards are carried by the port's own quest specs) and
worldmapguidedata.txt (not read by the port).

These are the quests themselves, so they follow the quest switch:
SRO_CAP110_QUESTS with SRO_LEVEL_CAP=110. Off, the tool removes the rows and
restores the replaced text lines, so a reused variant cannot retain them. No
retail table is committed to Git.

===========================================================================
"""
import argparse
import json
import mmap
import re
from pathlib import Path

import alexandria_npcs as characters
import cap110_blue_berserk as blue
import cap110_monsters as monsters
import cap110_quest_monsters as quest_monsters
import import_vsro_alexandria as tables
from prepare_client_resources import safe_target, write_atomically

MARKER = ".opensro-cap110-quest-tables.json"
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA
FORMAT = "opensro-cap110-quest-tables-v1"
QUEST_CODE = re.compile(r"QNO_(CH|EU)_HWAN_|QNO_SD_HW_")
TEXT_FILE = "textquest.txt"
TEXT_SOURCES = ("textquest_queststring.txt", "textquest_speech&name.txt", "textquest_otherstring.txt")
# Symbols the quests use that their own key pattern does not cover: the Alexandria
# NPCs' conversation openers and the inventory-full line the 100 chain borrows.
EXTRA_SYMBOLS = ("SN_NPC_SD_M_AREA_SENMUTE_QS", "SN_NPC_SD_M_AREA_LENENUNE_QS", "SN_TALK_QNO_TQ_BAEKRYEONG_4_09")
CONTENTS_COLUMNS = 13
# Table, the column naming a row, and whether a row belongs to the quests by its key
# or by any of its fields (the game guide names the quest in a symbol column).
QUEST_TABLE, CONTENTS_TABLE, GUIDE_TABLE = "questdata.txt", "questcontentsdata.txt", "gameguidedata.txt"
KEYS = {QUEST_TABLE: 2, CONTENTS_TABLE: 0, GUIDE_TABLE: 1}
ID_COLUMN = {QUEST_TABLE: 1}
enabled = quest_monsters.enabled


# ================
# is_quest_row
# ================
def is_quest_row(name, row):
	if name == GUIDE_TABLE:
		return bool(QUEST_CODE.search("\t".join(row)))
	return len(row) > KEYS[name] and bool(QUEST_CODE.search(row[KEYS[name]]))


# ================
# quest_rows
#
# The rows of the three tables to append: the later client's quest rows whose
# key the variant lacks, in the variant's layout. A quest id the variant uses
# for another quest refuses (none does today).
# ================
def quest_rows(later, variant):
	out = {}
	for name, key in KEYS.items():
		have = {row[key] for row in variant[name] if len(row) > key}
		chosen = [row for row in later[name] if is_quest_row(name, row) and row[key] not in have]
		if name == CONTENTS_TABLE:
			chosen = [row[:CONTENTS_COLUMNS] for row in chosen]
		out[name] = chosen
	taken = {row[ID_COLUMN[QUEST_TABLE]] for row in variant[QUEST_TABLE] if len(row) > 1}
	clash = sorted(row[1] for row in out[QUEST_TABLE] if row[1] in taken)
	if clash:
		raise ValueError("a quest id is used by another quest in the variant: " + ", ".join(clash))
	if not out[QUEST_TABLE]:
		raise ValueError("the later client holds none of the Blue Berserk quests")
	return out


# ================
# text_symbols
#
# Every key of the three later quest string files that names one of the quests,
# plus the extra symbols, in file order.
# ================
def text_symbols(later_texts):
	found = []
	for rows in later_texts:
		found += [row[blue.KEY_COLUMN] for row in rows if len(row) > blue.ENGLISH_COLUMN + 1
			and (QUEST_CODE.search(row[blue.KEY_COLUMN]) or row[blue.KEY_COLUMN] in EXTRA_SYMBOLS)]
	return tuple(dict.fromkeys(found))


# ================
# drop_rows
#
# The raw table without the lines whose key column is in owned. The table's
# own final line ending comes back as it was (a table without one keeps none),
# so the native bytes return exactly.
# ================
def drop_rows(raw, key_column, owned, ended):
	lines = tables.decode_table(raw).split("\r\n")
	kept = [line for line in lines if not (len(line.split("\t")) > key_column and line.split("\t")[key_column] in owned)]
	while kept and kept[-1] == "":
		kept.pop()
	return b"\xff\xfe" + ("\r\n".join(kept) + ("\r\n" if ended else "")).encode("utf-16-le")


# ================
# remove_owned
#
# What a previous on build left: the appended rows of the three tables, and the
# text (appended lines leave, replaced lines come back).
# ================
def remove_owned(text_root, prior):
	for name, keys in prior.get("rows", {}).items():
		path = text_root / name
		if path.exists():
			write_atomically(path, drop_rows(path.read_bytes(), KEYS[name], set(keys), prior["ended"][name]))
	record = prior.get("text")
	path = text_root / TEXT_FILE
	if record and path.exists():
		write_atomically(path, blue.restore_text(path.read_bytes(), set(record["appended"]), record["replaced"]))


# ================
# add_quest_tables
# ================
def add_quest_tables(vsro_root, native, output):
	native, output = Path(native).resolve(), Path(output).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("quest table overlay must be separate from native extraction")
	text_root, marker = output / TEXT_ROOT, output / MARKER
	prior = json.loads(marker.read_text(encoding="utf-8")) if marker.exists() else {}
	if marker.exists():
		remove_owned(text_root, prior)
		marker.unlink()
	if not enabled():
		return {"enabled": False}
	with open(Path(vsro_root) / "Media.pk2", "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as media:
		index = tables.archive_index(media)

		# ================
		# read
		# ================
		def read(name):
			return tables.table_text(media, index, name.lower())
		later = {name: tables.table_rows(read(name)) for name in KEYS}
		variant = {name: tables.table_rows(safe_target(text_root, name).read_bytes()) for name in KEYS}
		new_rows = quest_rows(later, variant)
		later_texts = [tables.table_rows(read(name)) for name in TEXT_SOURCES]
		symbols = text_symbols(later_texts)
		by_key = {row[blue.KEY_COLUMN]: row for rows in later_texts for row in rows if len(row) > blue.ENGLISH_COLUMN + 1}
		text_path = safe_target(text_root, TEXT_FILE)
		raw, appended, replaced = blue.fill_text(text_path.read_bytes(), by_key, symbols)
		ended = {}
		for name, rows in new_rows.items():
			path = safe_target(text_root, name)
			ended[name] = tables.decode_table(path.read_bytes()).endswith("\n")
			write_atomically(path, characters.append_rows(path.read_bytes(), rows))
		write_atomically(text_path, raw)
		summary = {"enabled": True, "format": FORMAT, "portOnly": True,
			"quests": sorted(row[2] for row in new_rows[QUEST_TABLE]),
			"rows": {name: [row[KEYS[name]] for row in rows] for name, rows in new_rows.items()}, "ended": ended,
			"text": {"appended": appended, "replaced": replaced}, "textSymbols": len(symbols),
			"laterArchiveHash": tables.sha256_of(media)}
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
	# Off, add_quest_tables only removes its owned outputs and reads no archive.
	if enabled():
		tables.require_vsro_pin(args.vsro_root, ("Media",))
	summary = add_quest_tables(args.vsro_root, args.native_extracted, args.output)
	print(json.dumps({key: value for key, value in summary.items() if key not in ("rows", "text", "quests", "ended")}, indent=2))


if __name__ == "__main__":
	main()
