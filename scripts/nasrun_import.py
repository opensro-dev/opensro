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

A second pass (add_nasrun_skills, #3456) stages the eleven skills a
worn Spirit lends, in their own skill shard under fresh ids; it runs
after the skill tier, which rewrites the skill manifest.

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
import avatar_effects as avatar_fx
import cap110_item_import as shared
import cap110_items as items
import cap110_monsters as cap110
import published_ids
import import_vsro_alexandria as tables
import mall_avatar_import as avatars
import skilldata_enc as skill_tables
import sro_pk2
from prepare_client_resources import safe_target, write_atomically
from sro_paths import trees_overlap

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

# Bound rows (port-only, the owner's pick on #3456: the Spirit is earned
# and bound, and the extension gear is sold bound). v1.150's own bound
# goods, the honor shop's rows, clear CanTrade, CanBorrow and CanDrop
# (honor_items.py); these also clear CanSell, so a token bought row never
# turns into gold. The server reads each cell (exchange, stall, market,
# warehouse, drop, merchant sale).
BOUND_TYPES = (WEARABLE_TYPE, EXTENSION_TYPE)
CAN_TRADE, CAN_SELL, CAN_BORROW, CAN_DROP = 16, 17, 19, 20
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
	lines = tables.decode_table(path.read_bytes()).lstrip("\ufeff").splitlines(keepends=True)
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
	if trees_overlap(native, output):
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
		for path in sorted(text_root.glob("itemdata*.txt")):
			if sro_pk2.fold_ascii(path.name) in (ITEM_SHARD, "itemdata.txt"):
				continue
			for row in tables.table_rows(path.read_bytes()):
				if len(row) > items.CODENAME and row[items.SERVICE] == "1" and row[items.CODENAME] in codes:
					raise ValueError(f"Nasrun row {row[items.CODENAME]} already stands in the item tables")
		# Identities stand above every object row of the tree, items and
		# characters alike (#3994).
		occupied = published_ids.occupied_objects(text_root, (ITEM_SHARD,))
		allocated = published_ids.allocate(published_ids.OBJECTS, occupied, codes, prior.get("items", {}))
		staged = []
		for row in rows:
			converted = list(row)
			converted[items.IDENTITY] = str(allocated[row[items.CODENAME]])
			converted[items.PLACEHOLDER_NAME] = items.UNUSED
			if converted[items.TYPE_FIRST:items.TYPE_FOURTH + 1] in BOUND_TYPES:
				for column in (CAN_TRADE, CAN_SELL, CAN_BORROW, CAN_DROP):
					converted[column] = "0"
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


#============================================================================
# The skills a worn Spirit lends (#3456)
#============================================================================

SKILL_MARKER = ".opensro-nasrun-skills.json"
SKILL_SHARD = "skilldata_130000.txt"
SKILL_MANIFEST = "skilldata.txt"
SKILL_COLUMNS = 118
SKILL_SERVICE, SKILL_ID, SKILL_CODENAME = 0, 1, 3
SKILL_CHAIN_NEXT = 9
SKILL_PREREQ_IDS = (40, 41, 42)
SKILL_ICON, SKILL_NAME_KEY, SKILL_DESCRIPTION_KEY = 61, 62, 64
SKILL_UNUSED = ("", "0", "xxx")
SKILL_BASIC_LEVEL, SKILL_COOLDOWN_GROUP = 7, 18

# The two Monster Defence Ignore ranks, as the later official tables
# author them: the iSRO-R server's skilldata_35000 and the v1.696 client
# agree that rank 2 is basic level 2 and that both ranks share cooldown
# group 0xCC000000 (stored signed). The VSRO client stages both ranks at
# level 1 with no group, so rank 2 would read as rank 1 by group and
# level, and the ranks would not share a cooldown (#3797 review).
SKILL_OVERRIDES = {
	"SKILL_MALL_NASRUN_MOBDEFENCEIGNORE_01": {SKILL_BASIC_LEVEL: "1", SKILL_COOLDOWN_GROUP: "-872415232"},
	"SKILL_MALL_NASRUN_MOBDEFENCEIGNORE_02": {SKILL_BASIC_LEVEL: "2", SKILL_COOLDOWN_GROUP: "-872415232"},
}

# What nasrun.AbilitySkills names (apps/server/internal/game/nasrun/
# ability.go): the three Devil skills of each colour, the red line first,
# and the two Monster Defence Ignore ranks. The VSRO client's ids are
# 31142-31152; the event pair's 31141 belongs to the event copies, which
# are not imported.
SKILL_CODES = tuple(
	f"SKILL_MALL_NASRUN_{name}{colour}_01"
	for colour in ("", "_YELLOW", "_BLUE")
	for name in ("ECCENTRICDEMON", "THEDEVILCONFUSION", "DESTROYTHEDEVIL")
) + ("SKILL_MALL_NASRUN_MOBDEFENCEIGNORE_01", "SKILL_MALL_NASRUN_MOBDEFENCEIGNORE_02")


# ================
# select_skill_rows
#
# The lent skills of the later skill tables, in SKILL_CODES order. A
# missing, short or chained row refuses: a chain or a prerequisite would
# name a skill id this import does not stage.
# ================
def select_skill_rows(later_rows):
	by_code = {}
	for row in later_rows:
		if len(row) > SKILL_CODENAME and row[SKILL_SERVICE] == "1":
			by_code.setdefault(row[SKILL_CODENAME], row)
	selected = []
	for code in SKILL_CODES:
		row = by_code.get(code)
		if row is None:
			raise ValueError(f"the later client has no Nasrun skill {code}")
		if len(row) != SKILL_COLUMNS:
			raise ValueError(f"Nasrun skill {code} has {len(row)} columns, want {SKILL_COLUMNS}")
		if any(row[column] != "0" for column in (SKILL_CHAIN_NEXT,) + SKILL_PREREQ_IDS):
			raise ValueError(f"Nasrun skill {code} chains to or requires another skill")
		for column in (SKILL_ICON, SKILL_NAME_KEY):
			if row[column] in SKILL_UNUSED:
				raise ValueError(f"Nasrun skill {code} has nothing at column {column}")
		selected.append(row)
	return selected


# ================
# remove_skills
#
# The skill pass's off path: its shard, its manifest line, the texts its
# marker records and the marker.
# ================
def remove_skills(output, prior, marker, shard, manifest):
	strip_texts(safe_target(output / TEXT_ROOT, NAME_TABLE), set(prior.get("texts", [])))
	if shard.exists():
		shard.unlink()
	safe_target(output / TEXT_ROOT, VISUAL_CHANGE_TABLE).unlink(missing_ok=True)
	effect_path = safe_target(output / TEXT_ROOT, SKILL_EFFECT_TABLE)
	if effect_path.exists() and prior.get("effectKeys"):
		text, _ = merge_skill_effects(tables.decode_table(effect_path.read_bytes()), "", set(), set(prior["effectKeys"]))
		write_atomically(effect_path, b"\xff\xfe" + text.lstrip("\ufeff").encode("utf-16-le"))
	if manifest.exists():
		names = [row[0] for row in tables.table_rows(manifest.read_bytes()) if row]
		kept = [name for name in names if sro_pk2.fold_ascii(name) != SKILL_SHARD]
		if len(kept) != len(names):
			write_atomically(manifest, cap110.encode_rows([[name] for name in kept]))
	marker.unlink(missing_ok=True)


# ================
# effect_sections
#
# The table's lines grouped as [header, [(section, lines)]] with each
# line's own ending kept.
# ================
def effect_sections(text):
	header, sections = [], []
	for line in text.splitlines(keepends=True):
		if line.startswith("#section"):
			sections.append((line.split("\t")[1].strip(), [line]))
		elif sections:
			sections[-1][1].append(line)
		else:
			header.append(line)
	return header, sections


# ================
# effect_key
#
# The key a row of section names, or None for a comment or blank line.
# ================
def effect_key(section, line):
	column = EFFECT_SECTIONS.get(section)
	cells = line.rstrip("\r\n").split("\t")
	if column is None or line.startswith("//") or len(cells) <= column:
		return None
	return cells[column].strip()


# ================
# merge_skill_effects
#
# The tree's skilleffect.txt with the previous pass's rows (by key) taken
# out and later's rows of keys appended at the end of their sections.
# The rows' animation group moves off the later transformation set.
# ================
def merge_skill_effects(tree_text, later_text, keys, previous):
	header, sections = effect_sections(tree_text)
	ending = "\r\n" if "\r\n" in tree_text else "\n"
	_, later_sections = effect_sections(later_text)
	added, efps = {name: [] for name in EFFECT_SECTIONS}, set()
	for name, lines in later_sections:
		if name not in EFFECT_SECTIONS:
			continue
		for line in lines[1:]:
			if effect_key(name, line) not in keys:
				continue
			cells = line.rstrip("\r\n").split("\t")
			if name == "skillaniset2" and len(cells) > ANI_GROUP_COLUMN and cells[ANI_GROUP_COLUMN] == LATER_ANI_GROUP:
				cells[ANI_GROUP_COLUMN] = "DEFAULT"
			if name == "skilleffectset":
				efps.update(cell for cell in cells if cell.lower().endswith(".efp"))
			added[name].append("\t".join(cells) + ending)
	out = list(header)
	for name, lines in sections:
		kept = [line for line in lines if effect_key(name, line) not in previous]
		if added.get(name):
			if kept and not kept[-1].endswith(("\n", "\r")):
				kept[-1] += ending
			kept += added[name]
		out += kept
	missing = [name for name in EFFECT_SECTIONS if added[name] and name not in {n for n, _ in sections}]
	if missing:
		raise ValueError(f"the tree's skilleffect.txt has no {missing} section")
	return "".join(out), sorted(efps)


# ================
# costume_effects
#
# The costume effect sets' steady loops (ACT_L / AT_LOOP) as worn-avatar
# auras, by effect set key (avatar_effects.loop_effect), for the build to
# hang on each costume's body piece. A row's character scale token
# (SCT_CHAR_SCALE, the transformation's growth) is not a particle scale:
# the particle keeps its own size. Returns ({key: [effect]}, refused).
# ================
def costume_effects(later_text, keys, has_particle):
	_, sections = effect_sections(later_text)
	effects, refused = {}, []
	for name, lines in sections:
		if name != "skilleffectset":
			continue
		for line in lines[1:]:
			key = effect_key(name, line)
			if key not in keys:
				continue
			# The VSRO row in the v1.150 layout, shifted to the later one
			# avatar_effects reads (service and id columns first).
			row = [avatar_fx.SERVICE_ON, "0"] + line.rstrip("\r\n").split("\t")
			row += [avatar_fx.NO_VALUE] * max(0, avatar_fx.EFFECT_COLUMNS - len(row))
			if row[avatar_fx.EFFECT_SCALE].startswith(CHAR_SCALE_TOKEN):
				row[avatar_fx.EFFECT_SCALE] = avatar_fx.NO_VALUE
			if row[avatar_fx.EFFECT_PHASE] != avatar_fx.LOOP_PHASE:
				continue
			avatar_fx.loop_effect_row(row, key, has_particle, effects, refused)
	return effects, refused


# ================
# add_nasrun_skills
#
# The skill pass the Alexandria builder runs after the skill tier
# (cap110_skill_import.add_skills, which rewrites the skill manifest from
# the native one): stage the eleven lent skills in their shard under
# marker-kept identities above every skill the tree holds, with their
# name texts and icons, or reap a previous import. The rows are the
# VSRO client's as authored; only the id changes.
# ================
# The later clients' active-Spirit icon edge: a 512x32 sheet of sixteen
# 32x32 cells (item-slot-effects.ts spiritActiveOverlay).
SPIRIT_EDGE_SHEET = "icon/item/etc/icon_edge_nasrun.ddj"

# The later clients' character visual change table (#4407): a lent skill's
# buff (ConditionID 3, the skill codename in StrPram1) dresses its caster in
# the Spirit's costume, one row per piece: its slot mask, priority,
# animation set and male/female BSR. The v1.150 tree has no such table;
# the import stages the lent skills' rows alone, as the client ships them.
VISUAL_CHANGE_TABLE = "charactervisualchange.txt"
VC_SERVICE, VC_SKILL, VC_AVATAR_EFFECT, VC_BSR_M, VC_BSR_F = 0, 9, 10, 19, 20

# The lent skills' cast effects and their costume's looping auras (#4407):
# the VSRO skilleffect.txt rows keyed by a lent skill (its skillaniset2 row
# and its READY/SHOT effects) or by the costume's effect set
# (AVATAR_MALL_NASRUN_*: ACT_S, ACT_L loops, DEACT), in the v1.150 layout.
# They join the tree's table in their own sections.
SKILL_EFFECT_TABLE = "skilleffect.txt"
EFFECT_SECTIONS = {"skillaniset2": 2, "skilleffectset": 1}
# The skill rows' animation group: AVATAR_NASRUN1 is the later clients'
# transformation set, which the v1.150 animation-set table lacks
# (animationSetNames.ts); until the transformation animations land the
# casts take the default set, whose ANI_READY01 the native skills share.
ANI_GROUP_COLUMN = 6
# A skilleffectset scale cell that grows the character, not the particle.
CHAR_SCALE_TOKEN = "SCT_CHAR_SCALE"
LATER_ANI_GROUP = "AVATAR_NASRUN1"


def add_nasrun_skills(vsro_root, native, output):
	native, output = Path(native).resolve(), Path(output).resolve()
	if trees_overlap(native, output):
		raise ValueError("Nasrun output must be separate from the native extraction")
	marker = output / SKILL_MARKER
	prior = cap110.read_json(marker) if marker.exists() else {}
	text_root = output / TEXT_ROOT
	shard = safe_target(text_root, SKILL_SHARD)
	manifest = safe_target(text_root, SKILL_MANIFEST)
	if not flag_enabled():
		remove_skills(output, prior, marker, shard, manifest)
		return {"enabled": False}
	if not avatars.source_present(vsro_root, avatars.VSRO_SOURCE):
		remove_skills(output, prior, marker, shard, manifest)
		return {"enabled": False, "skipped": "no VSRO client found"}
	strip_texts(safe_target(text_root, NAME_TABLE), set(prior.get("texts", [])))
	with ExitStack() as stack:
		archives, identities = avatars.open_archives(stack, vsro_root, avatars.VSRO_SOURCE)
		media, index = archives["Media"]

		# ================
		# read_later
		# ================
		def read_later(name):
			return tables.table_text(media, index, name)

		rows = select_skill_rows(skill_tables.client_skill_rows(read_later, lambda name: tables.TEXTDATA + name in index))
		# Ids stand above every skill of the native tables and of each
		# shard an earlier pass wrote into the output.
		occupied = set()
		for path in sorted({*text_root.glob("skilldata_*.txt"), *(native / TEXT_ROOT).glob("skilldata_*.txt")}):
			if sro_pk2.fold_ascii(path.name) == SKILL_SHARD:
				continue
			for row in tables.table_rows(path.read_bytes()):
				if len(row) > SKILL_CODENAME and row[SKILL_ID].strip().isdigit():
					occupied.add(int(row[SKILL_ID]))
					if row[SKILL_CODENAME] in SKILL_CODES:
						raise ValueError(f"Nasrun skill {row[SKILL_CODENAME]} already stands in the skill tables")
		allocated = published_ids.allocate(published_ids.SKILLS, occupied, list(SKILL_CODES), prior.get("skills", {}))
		staged = []
		for row in rows:
			converted = list(row)
			converted[SKILL_ID] = str(allocated[row[SKILL_CODENAME]])
			for column, value in SKILL_OVERRIDES.get(row[SKILL_CODENAME], {}).items():
				converted[column] = value
			staged.append(converted)
		symbols = {row[column] for row in staged for column in (SKILL_NAME_KEY, SKILL_DESCRIPTION_KEY)} - set(SKILL_UNUSED)
		later_texts = [row for name in LATER_TEXT_TABLES for row in avatars.later_text_rows(media, index, name)]
		name_path = safe_target(text_root, NAME_TABLE)
		base_names = name_path.read_bytes() if name_path.exists() else (native / TEXT_ROOT / NAME_TABLE).read_bytes()
		merged_names, absent = items.merge_texts(base_names, later_texts, [], extra=sorted(symbols))
		known = {row[1] for row in tables.table_rows(base_names) if len(row) > 1}
		after = {row[1] for row in tables.table_rows(merged_names) if len(row) > 1}
		missing_names = {row[SKILL_NAME_KEY] for row in staged} - after
		if missing_names:
			raise ValueError(f"missing Nasrun skill names: {sorted(missing_names)}")
		icons = {("Media", "icon/" + shared.normalized_path(row[SKILL_ICON])) for row in staged}
		# The worn, awake Spirit's icon edge (#4384): the later clients
		# animate it over the Spirit's avatar socket (item-slot-effects.ts).
		icons.add(("Media", SPIRIT_EDGE_SHEET))
		# The lent skills' costume rows and the BSRs they name (#4407).
		visual_text = read_later(VISUAL_CHANGE_TABLE) if tables.TEXTDATA + VISUAL_CHANGE_TABLE in index else b""
		visual_rows = [row for row in tables.table_rows(visual_text)
			if len(row) > VC_BSR_F and row[VC_SERVICE].strip() == "1" and row[VC_SKILL].strip() in SKILL_CODES]
		for row in visual_rows:
			for column in (VC_BSR_M, VC_BSR_F):
				icons.add(("Data", shared.normalized_path(row[column])))
		# Their cast effects and costume auras, and the effect files they play.
		effect_keys = set(SKILL_CODES) | {row[VC_AVATAR_EFFECT].strip() for row in visual_rows} - {"xxx", ""}
		# A tree or a later client without the table (the unit fixtures)
		# stages no effect rows.
		effect_path = safe_target(text_root, SKILL_EFFECT_TABLE)
		native_effects = native / TEXT_ROOT / SKILL_EFFECT_TABLE
		tree_effects = effect_path.read_bytes() if effect_path.exists() else (
			native_effects.read_bytes() if native_effects.exists() else None)
		merged_effects, efps = None, []
		if tree_effects is not None and tables.TEXTDATA + SKILL_EFFECT_TABLE in index:
			merged_effects, efps = merge_skill_effects(tables.decode_table(tree_effects),
				tables.decode_table(read_later(SKILL_EFFECT_TABLE)), effect_keys, set(prior.get("effectKeys", [])))
		for efp in efps:
			icons.add(("Particles", "system/" + shared.normalized_path(efp)))
		assets = shared.copy_resource_closure(icons, native, output, archives)
		# The costume's steady auras, its effect files now in the tree.
		particles = (output / "Particles_extracted", native / "Particles_extracted")
		worn, worn_refused = costume_effects(tables.decode_table(read_later(SKILL_EFFECT_TABLE)),
			{row[VC_AVATAR_EFFECT].strip() for row in visual_rows},
			lambda path: any((root / path).is_file() for root in particles)) if merged_effects is not None else ({}, [])
	write_atomically(shard, cap110.encode_rows(staged))
	if merged_effects is not None:
		write_atomically(effect_path, b"\xff\xfe" + merged_effects.lstrip("\ufeff").encode("utf-16-le"))
	visual_path = safe_target(text_root, VISUAL_CHANGE_TABLE)
	if visual_rows:
		write_atomically(visual_path, cap110.encode_rows(visual_rows))
	else:
		visual_path.unlink(missing_ok=True)
	write_atomically(name_path, merged_names)
	current = manifest.read_bytes() if manifest.exists() else (native / TEXT_ROOT / SKILL_MANIFEST).read_bytes()
	names = [row[0] for row in tables.table_rows(current) if row]
	names = [name for name in names if sro_pk2.fold_ascii(name) != SKILL_SHARD] + [SKILL_SHARD]
	write_atomically(manifest, cap110.encode_rows([[name] for name in names]))
	summary = {
		"enabled": True, "format": "opensro-nasrun-skills-v1", "portOnly": True, "source": "vsro",
		"laterArchiveHashes": identities,
		"skills": {row[SKILL_CODENAME]: int(row[SKILL_ID]) for row in staged},
		"texts": sorted(after - known), "absentDescriptions": absent, "assets": assets,
		"visualChanges": len(visual_rows),
		"effectKeys": sorted(effect_keys),
		"costumeEffects": worn, "costumeEffectsRefused": worn_refused,
		"limits": [
			"Lent by a worn, awake Devil's Spirit alone (nasrun/ability.go); no learn row or mastery names them.",
			"The event Spirit's skill (31141 in the VSRO client) is not imported, as the event copies are not.",
		],
	}
	write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode())
	return summary
