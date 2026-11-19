"""
===========================================================================

winter_event_import.py - the winter event's monsters for the Christmas event

Port-only, not native. Off unless SRO_CHRISTMAS_EVENT is "1", "on" or
"true" when the variant builds; the server spawns these kinds behind the
same switch (action/christmas_waves.go).

The v1.150 client has a Christmas event (the stolen socks, event group
1) but no winter monsters. The later clients' winter event brings
three kinds, all in the pinned v1.696 client (SRO_V1696_ROOT,
hash-pinned before anything is read):

	MOB_EV_WINTER_SNOWMAN        "Snow Slave", level 1: the hunted
	                             crowd of the "Snow Slave Hunting Event"
	MOB_EV_WINTER_WHITEKNIGHT_N  "White Knight", one champion row per
	                             level band N (20 ... 130)
	MOB_EV_WINTER_PRINCESS_N     "Princess Miyene", the same bands

Their models, materials and animations are already in the variant: the
Alexandria stage copies every VSRO Data file the native tree lacks, and
the VSRO client ships the same three .bsr closures. What this import
adds is their character rows (converted to the v1.150 layout in a shard
of their own), the skills they name (the native basic attack), their
names and their characterinfo rows. Rows above the server's level cap
(cap110.MAX_LEVEL) stay out. No placement: the client authors no nest
for any of them: the server lands the Snow Slaves while it snows and
the White Knight and Princess on their timetable.

The event's token rides the same switch (add_winter_items): the Snow
flake (ITEM_ETC_E091216_SNOWFLAKE, a plain 3/3/9 row the v1.150 table
takes byte for byte but its identity) with its icon and drop model, its
name and description, and So-ok's winter talk
(SN_TALK_QEV_SERVER_EVENT_QUEST_A..E, textevent).

The kind merge is later_world_import's (stage_kinds); the switch off
removes exactly the rows and files the marker records (reap_monsters,
reap_items).

===========================================================================
"""
from contextlib import ExitStack
import json
import mmap
import os
from pathlib import Path

import alexandria_npcs as characters
import cap110_item_import as item_import
import cap110_monsters as cap110
import published_ids
import import_vsro_alexandria as tables
import jupiter_import as jupiter
import later_world_import
import sro_pk2
from prepare_client_resources import safe_target, write_atomically

FLAG = "SRO_CHRISTMAS_EVENT"
ROOT_ENV = "SRO_V1696_ROOT"
MARKER = ".opensro-winter-event.json"
# The 240000 identity band: 210000 is the Constantinople fortress's,
# 220000 Muhan's and 230000 Styria's. One importer owns one shard file in
# a build: a layer's off reap unlinks its shard, so a shared band would
# delete the other layer's rows (test_later_shard_bands.py).
CHARACTER_SHARD = "characterdata_240000.txt"
SKILL_SHARD = "skilldata_240000.txt"
PREFIX = "MOB_EV_WINTER_"
# The winter event's token and its So-ok talk (the server's
# action/christmas.go), staged beside the monsters: the snow flake's item
# row in a shard of its own, its icon and drop model, its name and
# description, and So-ok's five talk rows.
ITEM_SHARD = "itemdata_240000.txt"
ITEMS = ("ITEM_ETC_E091216_SNOWFLAKE",)
ITEM_COLUMNS = 160
TALK_SYMBOLS = tuple(f"SN_TALK_QEV_SERVER_EVENT_QUEST_{part}" for part in "ABCDE")
ARCHIVES = ("Data", "Media", "Particles")
# The winter kinds' sound profiles: their effectsound objects (the base
# codenames their characterinfo rows name), cp949 like the native table.
SOUND_OBJECTS = ("MOB_EV_SNOWMAN", "MOB_EV_WHITEKNIGHT", "MOB_EV_PRINCESS")
SOUND_TABLE = "effectsound.txt"
SOUND_ENCODING = "cp949"
PIN_FILE = Path(__file__).with_name("v1696_archive_pins.json")
# Character row columns (cap110_monsters.py's layout).
CHAR_CODE = 2
CHAR_LEVEL = 57

SPEC = later_world_import.WorldImport(
	label="winter event",
	flag=FLAG,
	root_env=ROOT_ENV,
	character_shard=CHARACTER_SHARD,
	skill_shard=SKILL_SHARD,
	marker=MARKER,
	pin_file=PIN_FILE,
	pin_sentence="The winter monsters are built from one pinned v1.696 client; its archives are never committed.",
	region_label="winter event",
	population_files=(),
)


# ================
# flag_enabled
# ================
def flag_enabled(env=os.environ):
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON


# ================
# admits
#
# One later character row against the import: a winter event monster
# at or below the server's level cap.
# ================
def admits(row):
	return row[CHAR_CODE].startswith(PREFIX) and int(row[CHAR_LEVEL]) <= cap110.MAX_LEVEL


# ================
# add_winter_monsters
#
# The variant layer's entry point (variant_later_layers.py). Off, or
# without the v1.696 client, a previous import is reaped and no archive
# is read.
# ================
def add_winter_monsters(native, out, env=os.environ):
	native, output = Path(native).resolve(), Path(out).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("winter event overlay must be separate from native extraction")
	marker = output / MARKER
	prior = cap110.read_json(marker) if marker.exists() else {}
	text_root = output / later_world_import.TEXT_ROOT
	root = env.get(ROOT_ENV, "").strip()
	if not flag_enabled(env) or not root:
		reap_items(output, prior)
		reap_sounds(output, prior)
		later_world_import.reap_monsters(SPEC, output, prior)
		return {"enabled": False, "reason": "off" if not flag_enabled(env) else f"{ROOT_ENV} is not set"}
	later_world_import.require_pin(SPEC, root, ARCHIVES, jupiter.archive_pins)
	with ExitStack() as stack:
		archives = {}
		for name in ARCHIVES:
			handle = stack.enter_context(open(Path(root) / f"{name}.pk2", "rb"))
			data = stack.enter_context(mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ))
			archives[name] = (data, {item_import.normalized_path(entry.path): entry for entry in sro_pk2.read_directory(data)})
		media = archives["Media"][0]
		index = tables.archive_index(media)

		# ================
		# read
		# ================
		def read(name):
			return tables.table_text(media, index, name.lower())

		# ================
		# has
		# ================
		def has(name):
			return tables.TEXTDATA + name.lower() in index

		later = {row[CHAR_CODE]: row for shard in tables.table_rows(read("characterdata.txt"))
			for row in tables.table_rows(read(shard[0]))}
		refs = {code: characters.convert_character(row) for code, row in sorted(later.items()) if admits(row)}
		if not refs:
			raise ValueError(f"the v1.696 client carries no {PREFIX}* character rows")
		kinds = later_world_import.stage_kinds(SPEC.label, read, has, refs, text_root, prior, CHARACTER_SHARD, SKILL_SHARD,
			lambda code: code in refs)
		staged = add_winter_items(native, output, read, archives, prior)
		presentation = add_winter_presentation(native, output, read, set(refs), prior)
	kinds["characterInfo"] = sorted(set(kinds["characterInfo"]) | set(presentation.pop("characterInfo")))
	summary = {"enabled": True, **kinds, **staged, **presentation, "levels": {code: int(later[code][CHAR_LEVEL]) for code in refs}}
	write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode())
	return summary


# ================
# later_text_table
#
# One later text table's rows, through its shard manifest when the
# later client splits it (textdata_object.txt, textevent.txt).
# ================
def later_text_table(read, name):
	rows = tables.table_rows(read(name))
	shards = [row[0] for row in rows if row]
	if shards and all(len(row) == 1 and row[0].lower().endswith(".txt") for row in rows if row):
		raw = b"\xff\xfe" + "".join(tables.decode_table(read(shard)) for shard in shards).encode("utf-16-le")
	else:
		raw = read(name)
	return [later_world_import.later_text_row(row) for row in joined_rows(raw)]


# ================
# joined_rows
#
# The later tables' rows with multi-line cells joined: a physical line
# that does not open with a service cell continues the previous row's
# last cell (So-ok's QUEST_A and QUEST_C), joined with the literal "\n"
# the v1.150 talk text uses.
# ================
def joined_rows(raw):
	rows = []
	for row in tables.table_rows(raw):
		if rows and not (len(row) > 2 and row[0] in ("0", "1")):
			rows[-1][-1] += "\\n" + row[0]
			rows[-1].extend(row[1:])
		else:
			rows.append(list(row))
	return rows


# ================
# native_text_layout
#
# A later text row in the v1.150 name and event tables' layout, English
# in column 8 (stage_kinds' mapping).
# ================
def native_text_layout(row):
	return [row[0], row[1], row[2], "", "", row[5], row[4], row[7], row[8], row[9], ""]


# ================
# drop_text_rows
#
# The table's bytes without the symbols' rows, every other line exactly
# as it was: a row's continuation lines (a multi-line cell) leave with it,
# and no other row is parsed or re-encoded, so their text stays byte for
# byte (the English completion catalog pins the Korean it translated).
# ================
def drop_text_rows(raw, symbols):
	text = tables.decode_table(raw)
	kept, dropping = [], False
	for line in text.splitlines(keepends=True):
		cells = line.split("\t")
		if len(cells) > 2 and cells[0].lstrip("﻿") in ("0", "1"):
			dropping = cells[1] in symbols
		if not dropping:
			kept.append(line)
	return b"\xff\xfe" + "".join(kept).lstrip("﻿").encode("utf-16-le")


# ================
# replace_text_rows
#
# The table with the symbols' previous rows dropped and the new ones
# appended, so a rebuild never doubles a row.
# ================
def replace_text_rows(raw, symbols, rows):
	return characters.append_rows(drop_text_rows(raw, set(symbols)), rows)


# ================
# add_winter_items
#
# The snow flake's row under a marker-kept identity no published item or
# character holds, its resources (native files first, then the later archives), its
# name rows, and So-ok's talk rows. A missing row or text refuses: a
# half import would hand out an item the client cannot name.
# ================
def add_winter_items(native, output, read, archives, prior):
	text_root = output / later_world_import.TEXT_ROOT
	native_text = Path(native) / later_world_import.TEXT_ROOT
	later_items = {row[2]: row for name in tables.table_rows(read("itemdata.txt"))
		for row in tables.table_rows(read(name[0])) if len(row) > 2 and row[0] == "1"}
	rows = []
	for code in ITEMS:
		row = later_items.get(code)
		if row is None or len(row) != ITEM_COLUMNS:
			raise ValueError(f"the v1.696 client has no item row {code}")
		rows.append(list(row))
	# Items and characters share one identity space (_RefObjCommon): the
	# flake takes one no row of either table holds, this import's own
	# monsters included.
	occupied = {int(row[1]) for pattern in ("itemdata*.txt", "characterdata*.txt") for path in sorted(text_root.glob(pattern))
		if path.name.lower() != ITEM_SHARD
		for row in tables.table_rows(path.read_bytes()) if len(row) > 2 and row[0] == "1" and row[1].isdigit()}
	allocated = published_ids.allocate(published_ids.OBJECTS, occupied, [row[2] for row in rows], prior.get("items", {}))
	for row in rows:
		row[1] = str(allocated[row[2]])
	write_atomically(safe_target(text_root, ITEM_SHARD), cap110.encode_rows(rows))
	manifest = text_root / "itemdata.txt"
	current = manifest.read_bytes() if manifest.exists() else (native_text / "itemdata.txt").read_bytes()
	names = [r[0] for r in tables.table_rows(current) if r and r[0].lower() != ITEM_SHARD]
	write_atomically(manifest, cap110.encode_rows([[name] for name in names + [ITEM_SHARD]]))
	resources = item_import.copy_asset_closure(rows, Path(native), output, archives, worn=False)
	name_symbols = [symbol for row in rows for symbol in (row[5], row[6])]
	later_names = {row[1]: row for row in later_text_table(read, "textdata_object.txt") if len(row) >= 10}
	missing = [symbol for symbol in name_symbols if symbol not in later_names]
	if missing:
		raise ValueError(f"missing winter item texts: {missing}")
	name_path = text_root / "textdataname.txt"
	write_atomically(name_path, replace_text_rows(name_path.read_bytes(), name_symbols,
		[native_text_layout(later_names[symbol]) for symbol in name_symbols]))
	later_talk = {row[1]: row for row in later_text_table(read, "textevent.txt") if len(row) >= 10}
	missing = [symbol for symbol in TALK_SYMBOLS if symbol not in later_talk]
	if missing:
		raise ValueError(f"missing winter talk texts: {missing}")
	event_path = text_root / "textevent.txt"
	event_raw = event_path.read_bytes() if event_path.exists() else (native_text / "textevent.txt").read_bytes()
	write_atomically(event_path, replace_text_rows(event_raw, TALK_SYMBOLS,
		[native_text_layout(later_talk[symbol]) for symbol in TALK_SYMBOLS]))
	return {
		"items": {row[2]: int(row[1]) for row in rows},
		"itemSymbols": name_symbols, "talkSymbols": list(TALK_SYMBOLS),
		"itemResources": [record for record in resources if record["source"] == "later"],
	}


# ================
# reap_items
#
# The item half of a switch off: the shard, its manifest line, the name
# and talk rows and the resources a previous build copied from the later
# archives leave; everything else stays.
# ================
def reap_items(output, prior):
	text_root = output / later_world_import.TEXT_ROOT
	(text_root / ITEM_SHARD).unlink(missing_ok=True)
	manifest = text_root / "itemdata.txt"
	if manifest.exists():
		names = [r[0] for r in tables.table_rows(manifest.read_bytes()) if r]
		kept = [name for name in names if name.lower() != ITEM_SHARD]
		if len(kept) != len(names):
			write_atomically(manifest, cap110.encode_rows([[name] for name in kept]))
	for table, key in (("textdataname.txt", "itemSymbols"), ("textevent.txt", "talkSymbols")):
		path = text_root / table
		symbols = set(prior.get(key, []))
		if symbols and path.exists():
			write_atomically(path, drop_text_rows(path.read_bytes(), symbols))
	for record in prior.get("itemResources", []):
		folder = item_import.FOLDERS.get(record.get("archive"))
		if folder:
			safe_target(output / folder, record["path"]).unlink(missing_ok=True)


# ================
# add_winter_presentation
#
# The kinds' characterinfo rows and sound rules, without which the build
# gives them the first characterinfo row's profile (a Chinese man: his
# death scream on a Snow Slave). stage_kinds reads characterinfo from the
# later skilleffect.txt, which in v1.696 holds none: the rows moved to
# the characterinfo.txt shards, two columns (service, id) wider. Each kind
# keeps its own row and the base row it names (MOB_EV_SNOWMAN), cut to the
# native record; the bases' effectsound rules join the native table in
# its own layout (an empty first cell, cp949).
# ================
def add_winter_presentation(native, output, read, codes, prior):
	text_root = output / later_world_import.TEXT_ROOT
	later = {row[2]: row[2:] for row in later_text_table_rows(read, "characterinfo.txt")
		if len(row) >= characters.SKILL_EFFECT_COLUMNS + 2 and row[0] in ("0", "1")}
	# The Snow Slave has no row of its own: its characterdata base
	# (MOB_EV_SNOWMAN) is its profile, as are the sound objects.
	wanted = set(codes) | {later[code][1] for code in codes if code in later} | set(SOUND_OBJECTS)
	effect_path = text_root / "skilleffect.txt"
	effect_text = tables.decode_table(effect_path.read_bytes())
	known = cap110.characterinfo_codenames(effect_text)
	info = [later[code][:characters.SKILL_EFFECT_KEPT] for code in sorted(wanted) if code in later and code not in known]
	write_atomically(effect_path, characters.skill_effect_section(effect_path.read_bytes(), info))
	sound_path = text_root / SOUND_TABLE
	native_sound = Path(native) / later_world_import.TEXT_ROOT / SOUND_TABLE
	current = (sound_path if sound_path.exists() else native_sound).read_bytes().decode(SOUND_ENCODING)
	later_sound = read(SOUND_TABLE).decode(SOUND_ENCODING, errors="strict")
	rules = ["\t" + "\t".join(line.split("\t")[1:]) for line in later_sound.split("\r\n")
		if len(line.split("\t")) > 2 and line.split("\t")[1] in SOUND_OBJECTS]
	if not rules:
		raise ValueError("the v1.696 client carries no winter sound rules")
	kept = drop_sound_rules(current, SOUND_OBJECTS)
	if not kept.endswith("\r\n"):
		kept += "\r\n"
	write_atomically(sound_path, (kept + "\r\n".join(rules) + "\r\n").encode(SOUND_ENCODING))
	return {"characterInfo": sorted(code for code in wanted if code in later), "soundObjects": list(SOUND_OBJECTS)}


# ================
# later_text_table_rows
#
# A later UTF-16 table's rows through its shard manifest, as they stand
# (no text-row mapping): characterinfo.txt lists its Characterinfo_NNN
# shards.
# ================
def later_text_table_rows(read, name):
	rows = tables.table_rows(read(name))
	shards = [row[0] for row in rows if row]
	if shards and all(len(row) == 1 and row[0].lower().endswith(".txt") for row in rows if row):
		return [row for shard in shards for row in tables.table_rows(read(shard))]
	return rows


# ================
# drop_sound_rules
#
# The effectsound text without the objects' rules; every other line as it
# was.
# ================
def drop_sound_rules(text, objects):
	kept = []
	for line in text.split("\r\n"):
		cells = line.split("\t")
		object_name = cells[1] if len(cells) > 1 and not cells[0] else cells[0]
		if object_name not in objects:
			kept.append(line)
	return "\r\n".join(kept)


# ================
# reap_sounds
#
# The sound half of a switch off: the rules a previous build added leave.
# The characterinfo rows leave with the monsters' own reap (the marker's
# characterInfo list).
# ================
def reap_sounds(output, prior):
	path = output / later_world_import.TEXT_ROOT / SOUND_TABLE
	objects = set(prior.get("soundObjects", []))
	if objects and path.exists():
		write_atomically(path, drop_sound_rules(path.read_bytes().decode(SOUND_ENCODING), objects).encode(SOUND_ENCODING))
