"""
===========================================================================

arabia_quest_tables.py - the Arabia quests' rows and texts (#4134)

Port-only, not native. Baghdad's quests (82 QNO_AR_* rows, ids 1448 to
1764, levels 121 to 130) exist only in the later client; the continent
import (#3268) brought the town, its NPCs and their talk, but no quest
rows, so the NPCs offer nothing. This tool imports, from the v1.696
client:

	- the 82 quest rows (questdata.txt, whose index names its shards
	  there; no id is used in v1.150),
	- their contents rows (questcontentsdata.txt; the later table's
	  trailing columns the v1.150 layout lacks are dropped),
	- their game guide rows (gameguidedata.txt),
	- the quest strings (the later client's three textquest families,
	  sharded there and id-prefixed; merged into the one textquest.txt
	  the v1.150 client reads, English carried where the later client
	  ships it, authored here under the QUEST_ENGLISH contract where
	  it does not).

Not imported: refqusetreward.txt (a server table the v1.150 client
lacks; the quests' rewards ride the server's specs), and the 25 Arabia
quests that carry reward rows but no v1.696 questdata row (QNO_AR_071
to 090, QNO_AR_104 to 108): recorded on #4134, they join when their
identity rows source.

These are the continent's quests, so they follow its switch:
SRO_ARABIA, as one step of arabia_import's layer (add_quest_tables
takes that layer's reader, so the archives open once). Off, the tool
removes the rows and restores the replaced text lines, so a reused
variant cannot retain them. The server admits a quest spec only for a
quest whose row the build loaded, so off stays native there too. No
retail table is committed to Git.

===========================================================================
"""
import argparse
import json
import mmap
import os
import re
from pathlib import Path

import alexandria_map_tables as map_tables
import alexandria_npcs as characters
import cap110_blue_berserk as blue
import import_vsro_alexandria as tables
from prepare_client_resources import safe_target, write_atomically
from sro_paths import MAIN_CHECKOUT_ROOT

MARKER = ".opensro-arabia-quest-tables.json"
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA
FORMAT = "opensro-arabia-quest-tables-v1"
FLAG = "SRO_ARABIA"
SOURCE_ENV = "SRO_V1696_ROOT"
QUEST_CODE = re.compile(r"QNO_AR_")
TEXT_FILE = "textquest.txt"
TEXT_FAMILIES = ("textquest_queststring", "textquest_otherstring", "textquest_speech&name")
CONTENTS_COLUMNS = 13
# Table, the column naming a row, and whether a row belongs to the quests by
# its key or by any of its fields (the game guide names the quest in a symbol
# column).
QUEST_TABLE, CONTENTS_TABLE, GUIDE_TABLE = "questdata.txt", "questcontentsdata.txt", "gameguidedata.txt"
KEYS = {QUEST_TABLE: 2, CONTENTS_TABLE: 0, GUIDE_TABLE: 1}
ID_COLUMN = {QUEST_TABLE: 1}
SHARD_NAME = re.compile(r".*\.txt", re.IGNORECASE)


# ================
# QUEST_ENGLISH
#
# Authored English for the quest lines the v1.696 client ships without
# one (no English cell in any of its language columns). The asset build
# refuses an active textquest row without English
# (englishCompletions.mjs, assertEnglishCompletionCoverage), so a key
# the source leaves blank and this table does not carry fails the
# build rather than shipping a Korean-only line. Under the talk
# catalog's contract (SPEECH_ENGLISH in arabia_import.py): each entry
# records the exact Korean it translates, applies only while the
# source has no English, and a changed Korean refuses the build as
# stale rather than showing an outdated line.
# (key: (Korean source, English)). Translated from the v1.696 Korean.
# ================
QUEST_ENGLISH = {
}


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
# is_quest_row
# ================
def is_quest_row(name, row):
	if name == GUIDE_TABLE:
		return bool(QUEST_CODE.search("\t".join(row)))
	return len(row) > KEYS[name] and bool(QUEST_CODE.search(row[KEYS[name]]))


# ================
# sharded_rows
#
# One later table's rows: the v1.696 client keeps several of its textdata
# tables as an index naming shard files (questdata.txt, the textquest
# families); where the index is the table, its rows come back as they are.
# ================
def sharded_rows(read, name):
	rows = tables.table_rows(read(name))
	listed = [row[0].strip() for row in rows if row and row[0].strip()]
	if listed and all(len(row) == 1 and SHARD_NAME.fullmatch(row[0].strip()) for row in rows if row and row[0].strip()):
		return [row for shard in listed for row in tables.table_rows(read(shard.lower()))]
	return rows


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
		raise ValueError("the later client holds none of the Arabia quests")
	return out


# ================
# later_text_rows
#
# The three textquest families' rows, shards fanned out, each projected
# from the later 17-column layout into the layout fill_text reads.
# ================
def later_text_rows(read):
	return [[map_tables.later_text_row(row) for row in sharded_rows(read, family + ".txt")]
		for family in TEXT_FAMILIES]


# ================
# text_symbols
#
# Every key of the later quest strings that names one of the quests, in
# file order.
# ================
def text_symbols(later_texts):
	found = []
	for rows in later_texts:
		found += [row[blue.KEY_COLUMN] for row in rows if len(row) > blue.ENGLISH_COLUMN + 1
			and QUEST_CODE.search(row[blue.KEY_COLUMN])]
	return tuple(dict.fromkeys(found))


# ================
# complete_english
#
# The later rows with QUEST_ENGLISH applied: a row whose English is blank
# takes its authored line (its recorded Korean must still match), a row
# with English keeps it, and a blank quest row the table does not carry
# is collected and raised, so the build refuses instead of shipping a
# Korean-only line.
# ================
def complete_english(rows):
	out, missing = [], []
	for row in rows:
		if len(row) <= blue.ENGLISH_COLUMN or row[blue.ENGLISH_COLUMN].strip():
			out.append(row)
			continue
		key = row[blue.KEY_COLUMN]
		authored = QUEST_ENGLISH.get(key)
		if authored is None:
			if QUEST_CODE.search(key):
				missing.append(key)
			out.append(row)
			continue
		source, line = authored
		if source.strip() != row[blue.KOREAN_COLUMN].strip():
			raise ValueError(f"stale Arabia quest English for {key}: the v1.696 Korean changed")
		row = list(row)
		row[blue.ENGLISH_COLUMN] = line
		out.append(row)
	if missing:
		raise ValueError("the v1.696 client ships no English for: " + ", ".join(sorted(set(missing)))
			+ " (QUEST_ENGLISH does not carry them)")
	return out


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
# What a previous on build left: the appended rows of the three tables, and
# the text (appended lines leave, replaced lines come back).
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
# remove_quest_tables
#
# The layer's off path: the marker's record leaves, and so does it.
# ================
def remove_quest_tables(output):
	output = Path(output)
	marker = output / MARKER
	if marker.exists():
		remove_owned(output / TEXT_ROOT, json.loads(marker.read_text(encoding="utf-8")))
		marker.unlink()


# ================
# add_quest_tables
#
# read(name) returns one later textdata table of the v1.696 client (the
# Arabia layer's reader, over its already-open Media archive, whose
# sha256 archive_sha is).
# ================
def add_quest_tables(read, archive_sha, output):
	output = Path(output)
	text_root, marker = output / TEXT_ROOT, output / MARKER
	prior = json.loads(marker.read_text(encoding="utf-8")) if marker.exists() else {}
	if marker.exists():
		remove_owned(text_root, prior)
		marker.unlink()
	later = {QUEST_TABLE: sharded_rows(read, QUEST_TABLE), CONTENTS_TABLE: tables.table_rows(read(CONTENTS_TABLE)),
		GUIDE_TABLE: sharded_rows(read, GUIDE_TABLE)}
	variant = {name: tables.table_rows(safe_target(text_root, name).read_bytes()) for name in KEYS}
	new_rows = quest_rows(later, variant)
	later_texts = [complete_english(rows) for rows in later_text_rows(read)]
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
		"laterArchiveHash": archive_sha}
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
		raise ValueError("quest table overlay must be separate from native extraction")
	if not enabled():
		remove_quest_tables(output)
		print(json.dumps({"enabled": False}, indent=2))
		return
	root = Path(args.v1696_root) if args.v1696_root else source_root()
	with open(root / "Media.pk2", "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as media:
		index = tables.archive_index(media)

		def read(name):
			return tables.table_text(media, index, name.lower())

		summary = add_quest_tables(read, tables.sha256_of(media), output)
	print(json.dumps({key: value for key, value in summary.items() if key not in ("rows", "text", "quests", "ended")}, indent=2))


if __name__ == "__main__":
	main()
