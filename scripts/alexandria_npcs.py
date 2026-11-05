"""
===========================================================================

alexandria_npcs.py - the VSRO city's NPCs, in the v1.150 table layout

PORT-ONLY, NOT NATIVE. Used by build_alexandria_variant.py, which is off
unless SRO_ALEXANDRIA is "1", "on" or "true".

The VSRO client's characterdata has 104 columns and v1.150's has 120. The
server and the client both read the v1.150 layout, so an NPC row is
converted before it is added. The 16 extra v1.150 columns, found by lining
up 11,871 rows the two clients share by codename (the ids differ between
clients, so a codename is the only safe key):

	61..66    six resistances (frozen, frostbite, burn, shock, poison, zombie)
	99..108   five (skill id, rate) pairs; the rate is a float ("0.0")

Everything else keeps its place; the VSRO columns 61..92 become 67..98 and
93..103 become 109..119. An NPC has none of those values, so they are 0.

What an NPC needs besides its row:

	npcpos.txt            where it stands (the ids are VSRO ids; an NPC the
	                      native table already has keeps the native id)
	npcchat.txt           its talk row, and the SN_ texts that row names
	skilleffect.txt       its size, in the characterInfo section
	refshopgroup.txt and  its shop, but only when the shop's store is a
	refmappingshopgroup   native one: the city's own stores sell level-100+
	                      gear the v1.150 item tables do not have

Nothing native is changed: rows are appended, and a codename the native
tables already have is left alone.

===========================================================================
"""
from collections import Counter

import import_vsro_alexandria as stage1

SERVICE_COLUMN = 0
ID_COLUMN = 1
CODENAME_COLUMN = 2
NAME_STRING_COLUMN = 3
NAME_KEY_COLUMN = 5
CHAR_BIT_COLUMN = 8
TID_COLUMNS = (9, 10, 11)
UNUSED_NAME = "xxx"

VSRO_CHARACTER_COLUMNS = 104
NATIVE_CHARACTER_COLUMNS = 120
# VSRO columns before the six resistances, and before the five skill pairs.
RESISTANCE_AT = 61
RESISTANCE_COUNT = 6
SKILL_PAIRS_AT = 93
SKILL_PAIR_COUNT = 5
# The native table that new rows join (its ids end below the city's).
CHARACTER_TARGET = "characterdata_25000.txt"

# CICNPC's class gate (GIDObject_SelectRuntimeClassAndSpawn), as the server's npcworlddata.go tests it.
NPC_TID_MASK = 0x7FE
NPC_TID_WORD = 0x0146

SKILL_EFFECT_COLUMNS = 13
SKILL_EFFECT_KEPT = 12
SECTION_MARK = "#"

CHAT_COLUMNS = 4
STORE_GROUP_COLUMNS = 5
STORE_MAP_COLUMNS = 4

REGION_MASK = 0xFFFF
# Owner-supplied ISRO-R shard SHA256 89c6835f8e51b1a75950242c89cf3ad639ea7d50a22191a90632cca333664a45.
# Nest 37159 joins tactics 1288 to NPC_SD_T_AREA_ARMOR, independently of VSRO ids.
ARMOR_POSITION = ["23602", "639.19", "1364.63", "87.459999"]
ARMOR_MODEL = "npc\\sd\\sd_guild_agent3.bsr"
SHARON_POSITION = ["23087", "1731.92", "863.31", "875.04999"]
SHARON_MODEL = "npc\\sd\\sd_m_area_warehouse.bsr"
RESTORED_ARMOR_NPCS = {
	"NPC_SD_T_AREA_ARMOR": (ARMOR_POSITION, ARMOR_MODEL),
	"NPC_SD_M_AREA_ARMOR": (SHARON_POSITION, SHARON_MODEL),
}
MODEL_COLUMN = 52


# ================
# translation_rows
#
# The supplied object's German Tausert cell contains an unquoted newline.
# Join only a short translation record and its non-record continuation;
# preserve the following language cells rather than discarding the name.
# ================
def translation_rows(raw):
	rows = []
	for row in stage1.table_rows(raw):
		if rows and len(rows[-1]) < 10 and len(rows[-1]) > 1 and rows[-1][1].startswith("SN_") and not row[0].isdigit():
			rows[-1][-1] += " " + row[0].strip()
			rows[-1].extend(row[1:])
		else:
			rows.append(row)
	return rows


# ================
# restore_missing_positions
#
# Port-only, not native: ISRO-R nests 37159 (Viviana) and 37158 (Sharon)
# restore the merchants in imported regions. Explicit client positions win.
# ================
def restore_missing_positions(placed, characters, region_ids):
	by_code = {row[CODENAME_COLUMN]: row for row in characters.values()}
	known = {row[0] for row in placed}
	restored = []
	for code, (position, _) in RESTORED_ARMOR_NPCS.items():
		target = by_code.get(code)
		if target is None or int(position[0]) not in region_ids or target[ID_COLUMN] in known:
			continue
		restored.append([target[ID_COLUMN]] + position)
	return restored


#============================================================================
# Characters
#============================================================================


# ================
# tid_word
# ================
def tid_word(row):
	"""The packed TypeID word of a characterdata row, as the server packs it."""
	word = (int(row[TID_COLUMNS[0]]) & 7) << 2 | (int(row[TID_COLUMNS[1]]) & 3) << 5 | (int(row[TID_COLUMNS[2]]) & 15) << 7
	if int(row[CHAR_BIT_COLUMN]) != 0:
		word |= 2
	return word


# ================
# is_npc
# ================
def is_npc(row):
	"""True for an enabled characterdata row in the NPC class."""
	if len(row) <= TID_COLUMNS[-1] or row[SERVICE_COLUMN] != "1":
		return False
	try:
		return tid_word(row) & NPC_TID_MASK == NPC_TID_WORD
	except ValueError:
		return False


# ================
# convert_character
# ================
def convert_character(row):
	"""One VSRO characterdata row in the 120-column v1.150 layout."""
	if len(row) != VSRO_CHARACTER_COLUMNS:
		raise ValueError(f"characterdata row {row[ID_COLUMN]} has {len(row)} columns")
	converted = (
		row[:RESISTANCE_AT] + ["0"] * RESISTANCE_COUNT + row[RESISTANCE_AT:SKILL_PAIRS_AT]
		+ ["0", "0.0"] * SKILL_PAIR_COUNT + row[SKILL_PAIRS_AT:]
	)
	# The VSRO name column holds a Korean placeholder; v1.150 writes "xxx".
	converted[NAME_STRING_COLUMN] = UNUSED_NAME
	return converted


# ================
# character_rows
# ================
def character_rows(raws):
	"""Every characterdata row in the raw tables, in file order."""
	return [row for raw in raws for row in stage1.table_rows(raw) if len(row) > CODENAME_COLUMN]


# ================
# position_rows
# ================
def position_rows(raw, region_ids):
	"""The npcpos rows (id, region, x, y, z) whose region is in region_ids."""
	rows = []
	for row in stage1.table_rows(raw):
		if len(row) >= 5 and row[1].lstrip("-").isdigit() and int(row[1]) & REGION_MASK in region_ids:
			rows.append(row[:5])
	return rows


#============================================================================
# Merge
#============================================================================


# ================
# table_text
# ================
def table_text(rows):
	return "\r\n".join("\t".join(row) for row in rows) + "\r\n"


# ================
# append_rows
# ================
def append_rows(raw, rows):
	"""The UTF-16 table with rows appended; the original bytes stay first."""
	if not rows:
		return raw
	text = stage1.decode_table(raw)
	if not text.endswith("\n"):
		text += "\r\n"
	return b"\xff\xfe" + (text.lstrip("\ufeff") + table_text(rows)).encode("utf-16-le")


# ================
# common_value
# ================
def common_value(rows, column):
	"""The most common value of a column; the table kind the rows share."""
	return Counter(row[column] for row in rows if len(row) > column).most_common(1)[0][0]


# ================
# with_kind
# ================
def with_kind(row, kind):
	"""The row with its second column (the table kind: 11 native, 15 VSRO) set."""
	return [row[0], kind] + row[2:]


# ================
# shop_rows
# ================
def shop_rows(native, vsro, codes):
	"""
	The refshopgroup and refmappingshopgroup rows that give the NPCs in codes
	a shop. A group is added when the native tables lack it and the store it
	maps to is one the native refshop has.
	"""
	native_groups = stage1.table_rows(native["refshopgroup.txt"])
	native_maps = stage1.table_rows(native["refmappingshopgroup.txt"])
	stores = {row[3] for row in stage1.table_rows(native["refshop.txt"]) if len(row) > 3}
	known = {row[3] for row in native_groups if len(row) >= STORE_GROUP_COLUMNS}
	group_kind, map_kind = common_value(native_groups, 1), common_value(native_maps, 1)
	vsro_maps = [row for row in stage1.table_rows(vsro["refmappingshopgroup.txt"]) if len(row) >= STORE_MAP_COLUMNS]

	groups, maps, skipped = [], [], []
	for row in stage1.table_rows(vsro["refshopgroup.txt"]):
		if len(row) < STORE_GROUP_COLUMNS or row[0] != "1" or row[4] not in codes or row[3] in known:
			continue
		mapped = [m for m in vsro_maps if m[2] == row[3]]
		if not mapped or any(m[3] not in stores for m in mapped):
			skipped.append(row[3])
			continue
		groups.append(with_kind(row, group_kind))
		maps += [with_kind(m, map_kind) for m in mapped]
	taken = {row[2] for row in native_groups if len(row) > 2}
	if taken & {row[2] for row in groups}:
		raise SystemExit("a VSRO shop group id is already a native shop group id")
	return groups, maps, skipped


# ================
# text_keys
# ================
def text_keys(characters, chat_rows):
	"""The SN_ keys the NPC rows and their talk rows name."""
	keys = {row[NAME_KEY_COLUMN] for row in characters}
	for row in chat_rows:
		keys |= {key for key in row[2:CHAT_COLUMNS] if key.startswith("SN_")}
	return keys


# ================
# skill_effect_section
# ================
def skill_effect_section(raw, rows):
	"""
	The skilleffect.txt text with rows added at the end of its first section
	(characterInfo), the one that holds every character's size. Rows are cut
	to the native width.
	"""
	lines = stage1.decode_table(raw).split("\r\n")
	section_end = next((at for at, line in enumerate(lines) if at > 0 and line.startswith(SECTION_MARK)), len(lines))
	added = ["\t".join(row[:SKILL_EFFECT_KEPT] + [""] * (SKILL_EFFECT_COLUMNS - SKILL_EFFECT_KEPT)) for row in rows]
	merged = lines[:section_end] + added + lines[section_end:]
	return b"\xff\xfe" + "\r\n".join(merged).lstrip("\ufeff").encode("utf-16-le")


# ================
# merge_npc_tables
# ================
def merge_npc_tables(native, vsro, regions, native_text):
	"""
	The native tables with the NPCs placed in regions added. native and vsro
	map table names to raw bytes ("characters" is a list of every
	characterdata file). native_text and the vsro "textdata_object.txt" give
	the SN_ texts. Returns {name: bytes} for the changed files and a summary.
	"""
	region_ids = {(y << 8) | x for x, y in regions}
	native_rows = character_rows(native["characters"])
	native_by_code = {row[CODENAME_COLUMN]: row for row in native_rows}
	taken_ids = {row[ID_COLUMN] for row in native_rows}
	vsro_by_id = {row[ID_COLUMN]: row for row in character_rows(vsro["characters"])}

	placed = position_rows(vsro["npcpos.txt"], region_ids)
	restored_positions = restore_missing_positions(placed, vsro_by_id, region_ids)
	placed += restored_positions
	npcs = [vsro_by_id[row[0]] for row in placed if row[0] in vsro_by_id and is_npc(vsro_by_id[row[0]])]
	placed = [row for row in placed if row[0] in vsro_by_id and is_npc(vsro_by_id[row[0]])]

	characters, seen = [], set()
	for row in npcs:
		code = row[CODENAME_COLUMN]
		if code in native_by_code or code in seen:
			continue
		if row[ID_COLUMN] in taken_ids:
			raise SystemExit(f"VSRO NPC id {row[ID_COLUMN]} ({code}) is already a native id")
		seen.add(code)
		converted = convert_character(row)
		if code in RESTORED_ARMOR_NPCS:
			# ISRO-R references 26765/26736 supply the models for the restored merchants.
			converted[MODEL_COLUMN] = RESTORED_ARMOR_NPCS[code][1]
		characters.append(converted)
	codes = {row[CODENAME_COLUMN] for row in npcs}

	positions, known_positions = [], {tuple(row[:5]) for row in stage1.table_rows(native["npcpos.txt"]) if len(row) >= 5}
	for row in placed:
		native_row = native_by_code.get(vsro_by_id[row[0]][CODENAME_COLUMN])
		moved = [native_row[ID_COLUMN] if native_row else row[0]] + row[1:]
		if tuple(moved) not in known_positions:
			positions.append(moved)

	native_chat = {row[1] for row in stage1.table_rows(native["npcchat.txt"]) if len(row) > 1}
	chat = [
		row[:CHAT_COLUMNS] for row in stage1.table_rows(vsro["npcchat.txt"])
		if len(row) >= CHAT_COLUMNS and row[0] == "1" and row[1] in codes and row[1] not in native_chat
	]
	groups, maps, skipped = shop_rows(native, vsro, codes)

	native_keys = {row[1] for row in stage1.table_rows(native_text) if len(row) > 1}
	wanted = text_keys(characters, chat)
	texts = [
		[row[0], row[1], row[2], "", "", row[5], row[4], row[7], row[8], row[9], ""]
		for row in translation_rows(vsro["textdata_object.txt"])
		if len(row) >= 10 and row[1] in wanted and row[1] not in native_keys
	]

	new_codes = {row[CODENAME_COLUMN] for row in characters}
	native_effects = {row[0] for row in stage1.table_rows(native["skilleffect.txt"]) if len(row) > 1}
	effects = [
		row for row in stage1.table_rows(vsro["skilleffect.txt"])
		if row[0] in new_codes and len(row) > SKILL_EFFECT_KEPT and row[0] not in native_effects
	]

	merged = {
		CHARACTER_TARGET: append_rows(native[CHARACTER_TARGET], characters),
		"npcpos.txt": append_rows(native["npcpos.txt"], positions),
		"npcchat.txt": append_rows(native["npcchat.txt"], chat),
		"refshopgroup.txt": append_rows(native["refshopgroup.txt"], groups),
		"refmappingshopgroup.txt": append_rows(native["refmappingshopgroup.txt"], maps),
		"skilleffect.txt": skill_effect_section(native["skilleffect.txt"], effects) if effects else native["skilleffect.txt"],
		"textdataname.txt": append_rows(native_text, texts)
	}
	return merged, {
		"npcs": sorted(new_codes), "positions": len(positions), "chat": len(chat),
		"shopGroups": [row[3] for row in groups], "shopsSkipped": sorted(set(skipped)),
		"effects": len(effects), "texts": len(texts), "restoredPositions": restored_positions
	}
