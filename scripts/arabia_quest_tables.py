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
# gameguidedata.txt: [enabled, id, Korean label, depth, parent or child
# count, content key, title key, quest code]. A depth-1 row is one quest's
# article; the client refuses one whose title or body has no text
# (quest-guide.ts), so the import does too.
GUIDE_DEPTH_COLUMN, GUIDE_CONTENT_COLUMN, GUIDE_TITLE_COLUMN, GUIDE_QUEST_COLUMN = 3, 5, 6, 7
# A composed guide body's section heads, as the shipped bodies write them
# (SN_PAYCON_QNO_AR_001): the Korean column and every other language.
GUIDE_HEAD = '<strong><font color="255,226,74,0">\u25c8 {}</font></strong><br>'
GUIDE_HEADS = {"korean": ("\uc784\ubb34", "\ud615\uc2dd"), "other": ("Task", "Format")}
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
	"SN_PAY_QNO_AR_071": ("소원의 램프 1", "Lamp of Wishes 1"),
	"SN_PAY_QNO_AR_072": ("소원의 램프 2", "Lamp of Wishes 2"),
	"SN_PAY_QNO_AR_073": ("소원의 램프 3", "Lamp of Wishes 3"),
	"SN_PAY_QNO_AR_074": ("소원의 램프 4", "Lamp of Wishes 4"),
	"SN_PAY_QNO_AR_075": ("램프의 가디언 1", "Guardian of the Lamp 1"),
	"SN_PAY_QNO_AR_076": ("램프의 가디언 2", "Guardian of the Lamp 2"),
	"SN_PAY_QNO_AR_077": ("램프의 가디언 3", "Guardian of the Lamp 3"),
	"SN_PAY_QNO_AR_078": ("악의 여신 1", "Goddess of Evil 1"),
	"SN_PAY_QNO_AR_079": ("악의 여신 2", "Goddess of Evil 2"),
	"SN_PAY_QNO_AR_080": ("악의 여신 3", "Goddess of Evil 3"),
	"SN_PAY_QNO_AR_081": ("페트라 조사단", "The Petra Expedition"),
	"SN_PAY_QNO_AR_082": ("기괴한 생명체", "Grotesque Creatures"),
	"SN_PAY_QNO_AR_083": ("불타는 정령", "Burning Spirits"),
	"SN_PAY_QNO_AR_084": ("모래의 악마", "Demons of the Sand"),
	"SN_PAY_QNO_AR_085": ("수상한 꼭두각시", "Suspicious Puppets"),
	"SN_PAY_QNO_AR_086": ("사념의 덩어리", "Masses of Malice"),
	"SN_PAY_QNO_AR_087": ("화염의 지배자", "Lord of Flames"),
	"SN_PAY_QNO_AR_088": ("에메랄드 바다", "The Emerald Sea"),
	"SN_PAY_QNO_AR_089": ("램프의 샤바훈", "Shabahun of the Lamp"),
	"SN_PAY_QNO_AR_090": ("가디언 하이리쑨", "Guardian Hairisun"),
	"SN_PAY_QNO_AR_091": ("벌레는 질색이야", "I Hate Bugs"),
	"SN_PAY_QNO_AR_092": ("선인장을 조심해!", "Watch Out for Cacti!"),
	"SN_PAY_QNO_AR_093": ("사막에서 바늘 찾기", "A Needle in the Desert"),
	"SN_PAY_QNO_AR_094": ("식인 거미", "Man-Eating Spiders"),
	"SN_PAY_QNO_AR_095": ("선인장 뽑기", "Pulling Up Cacti"),
	"SN_PAY_QNO_AR_096": ("불타는 나무", "Burning Trees"),
	"SN_PAY_QNO_AR_097": ("불타는 해안", "The Burning Coast"),
	"SN_PAY_QNO_AR_098": ("왕국 조사단 지원", "Supporting the Royal Expedition"),
	"SN_PAY_QNO_AR_099": ("보급로 확보", "Securing the Supply Route"),
	"SN_PAY_QNO_AR_100": ("고급 크랩 요리", "Fine Crab Cuisine"),
	"SN_PAY_QNO_AR_101": ("특식 배달", "Special Meal Delivery"),
	"SN_PAY_QNO_AR_102": ("진로 확보", "Clearing the Way"),
	"SN_PAY_QNO_AR_103": ("진로 확보2", "Clearing the Way 2"),
	"SN_PAY_QNO_AR_104": ("정원의 애완동물", "Pets of the Garden"),
	"SN_PAY_QNO_AR_105": ("정원의 안방마님", "Mistress of the Garden"),
	"SN_PAY_QNO_AR_106": ("섬뜩한 뜨거움", "An Eerie Heat"),
	"SN_PAY_QNO_AR_107": ("뱀 조련사", "The Snake Tamer"),
	"SN_PAY_QNO_AR_108": ("마녀의 흔적", "Traces of the Witch"),
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
# guide_rows
#
# The later client's guide rows without its strays: a row whose quest exists
# nowhere - no questdata row and no title text. v1.696 ships one: 370005
# names QNO_AR_109 with QNO_AR_108's label ("Trace of the Witch") under a
# parent that counts four children (370001..370004 are those four). The
# client refused it at package time (r204, "Missing quest article text"). A
# native data defect is not copied (owner, 2026-11-24). A row whose quest
# has text but no questdata row (071..090, 104..108 in v1.696) stays as
# before; any other missing text fails check_guide_text.
# ================
def guide_rows(rows, quests, texts):
	codes = {row[KEYS[QUEST_TABLE]] for row in quests if len(row) > KEYS[QUEST_TABLE]}
	return [row for row in rows if len(row) <= GUIDE_QUEST_COLUMN or not QUEST_CODE.search(row[GUIDE_QUEST_COLUMN])
		or row[GUIDE_QUEST_COLUMN] in codes or row[GUIDE_TITLE_COLUMN] in texts]


# ================
# composed_guide_bodies
#
# A text row for each quest article whose body v1.696 never wrote: 300002
# (QNO_AR_002) names SN_PAYCON_QNO_AR_002, which no textquest family holds,
# while the quest's own task lines (SN_CON_<code>_<n>) and format line
# (SN_NC_<code>) are there. The body is those two, in the shipped bodies'
# layout; reward, NPC and scenario stay in the quest window, which reads
# them from the quest itself. Port-only (owner, 2026-11-24: native data
# defects are fixed, not copied).
# ================
def composed_guide_bodies(guide, later_texts):
	by_key = {row[blue.KEY_COLUMN]: row for rows in later_texts for row in rows if len(row) > blue.KEY_COLUMN}
	out = []
	for row in guide:
		if len(row) <= GUIDE_QUEST_COLUMN or row[GUIDE_DEPTH_COLUMN] != "1" or row[GUIDE_CONTENT_COLUMN] in by_key:
			continue
		code = row[GUIDE_QUEST_COLUMN]
		prefix = "SN_CON_" + code + "_"
		steps = sorted(int(k[len(prefix):]) for k in by_key if k.startswith(prefix) and k[len(prefix):].isdigit())
		tasks = [by_key[f"{prefix}{step}"] for step in steps]
		form = by_key.get("SN_NC_" + code)
		if not tasks or form is None:
			continue
		body = list(form)
		body[blue.KEY_COLUMN] = row[GUIDE_CONTENT_COLUMN]
		for column in range(blue.KOREAN_COLUMN, len(body)):
			if not form[column]:
				continue
			task, shape = GUIDE_HEADS["korean" if column == blue.KOREAN_COLUMN else "other"]
			lines = "<br>".join(t[column] for t in tasks if len(t) > column)
			body[column] = ("<sml2>" + GUIDE_HEAD.format(task) + lines + "<br><br>" + GUIDE_HEAD.format(shape)
				+ form[column] + "</sml2>")
		out.append(body)
	return out


# ================
# check_guide_text
#
# Every quest article the import adds has a title, and a depth-1 article a
# body, with text: the client's own rule (quest-guide.ts), read with the
# importers' no-text rule (alexandria_npcs.is_placeholder_text), so a
# missing key fails here at import, not at the release's package step.
# ================
def check_guide_text(guide, texts, variant_text):
	missing = []
	for row in guide:
		if len(row) <= GUIDE_QUEST_COLUMN or not QUEST_CODE.search(row[GUIDE_QUEST_COLUMN]):
			continue
		keys = [row[GUIDE_TITLE_COLUMN]] + ([row[GUIDE_CONTENT_COLUMN]] if row[GUIDE_DEPTH_COLUMN] == "1" else [])
		for key in keys:
			english = texts.get(key)
			absent = key not in variant_text if english is None else characters.is_placeholder_text(english)
			if absent:
				missing.append(f"{row[KEYS[GUIDE_TABLE]]}:{key}")
	if missing:
		raise ValueError("Arabia guide rows name text the import does not carry: " + ", ".join(missing))


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
# The later rows with QUEST_ENGLISH applied: a row whose English is a
# placeholder (characters.is_placeholder_text, the asset build's rule)
# takes its authored line (its recorded Korean must still match), a row
# with English keeps it, and a blank quest row the table does not carry
# is collected and raised, so the build refuses instead of shipping a
# Korean-only line.
# ================
def complete_english(rows):
	out, missing = [], []
	for row in rows:
		if len(row) <= blue.ENGLISH_COLUMN or not characters.is_placeholder_text(row[blue.ENGLISH_COLUMN]):
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
	quests = sharded_rows(read, QUEST_TABLE)
	later_texts = [complete_english(rows) for rows in later_text_rows(read)]
	known = {row[blue.KEY_COLUMN] for rows in later_texts for row in rows if len(row) > blue.KEY_COLUMN}
	later = {QUEST_TABLE: quests, CONTENTS_TABLE: tables.table_rows(read(CONTENTS_TABLE)),
		GUIDE_TABLE: guide_rows(sharded_rows(read, GUIDE_TABLE), quests, known)}
	variant = {name: tables.table_rows(safe_target(text_root, name).read_bytes()) for name in KEYS}
	new_rows = quest_rows(later, variant)
	later_texts.append(composed_guide_bodies(new_rows[GUIDE_TABLE], later_texts))
	symbols = text_symbols(later_texts)
	by_key = {row[blue.KEY_COLUMN]: row for rows in later_texts for row in rows if len(row) > blue.ENGLISH_COLUMN + 1}
	text_path = safe_target(text_root, TEXT_FILE)
	variant_text = {row[blue.KEY_COLUMN] for row in tables.table_rows(text_path.read_bytes()) if len(row) > blue.KEY_COLUMN}
	check_guide_text(new_rows[GUIDE_TABLE], {k: row[blue.ENGLISH_COLUMN] for k, row in by_key.items()}, variant_text)
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
