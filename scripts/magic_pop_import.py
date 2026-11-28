"""
===========================================================================

magic_pop_import.py - the v1.696 Magic POP prize pool and its prize items

Later-native (v1.696), the port's default: on unless SRO_MAGIC_POP_V1696
is "0", "off", "false" or "no" (owner, 2026-11-27). With the switch off,
or with no v1.696 client named (SRO_V1696_ROOT), a previous import is
reaped and the server keeps the v1.150 gachaitemset.txt.

The v1.696 machine's normal card plays set 1 (the prizes a player picks)
and set 2 (the losing draw); gachanpcmap.txt names them for NPC 9251.
This layer writes three tables beside the native ones, which stay
byte-identical:

  gachaitemset_v1696.txt            the surviving set 1 and 2 rows, all
                                    twenty v1.696 columns, column 3 (the
                                    reward) rewritten to this tree's id
                                    by codename
  gachanpcmap_v1696.txt             the v1.696 machine row, verbatim
  refgachatreeforclientuidata.txt   the select window's category tree,
                                    verbatim

and stages every prize item the tree lacks in its own shard
(itemdata_340000.txt) under fresh, marker-kept identities. Every such item
is v1.696 event Seal gear (ITEM_EVENT_*_RARE, degrees 1 to 10): the native
rare row with one cell more, the "maximum sockets" parameter, which the
v1.150 layout has no reader for and which this import resets to its
unused value. The event rows reuse the native names, models and icons;
the asset closure copies whatever this tree does not have yet.

The pool rules (owner, 2026-11-27):
  - sets 1 and 2 only: the premium card's sets 3 and 4 are out of scope;
  - no degree-11 prize: a row whose minimum level cap (column 16) is 101
    or more is dropped (degree 10 rows, cap 91, stay; the server
    opens them at cap 90, gacha/catalog.go degree10OpensAt);
  - WITHHELD below: prizes the owner dropped, and win prizes whose wear
    effect a later change adds (each such change deletes its line);
  - the lose set ships every native prize as it is (owner, 2026-11-28):
    an expendable the tree lacks is staged with its v1.696 parameters
    verbatim, whether or not the server serves its use yet (one the
    server does not know is refused on use and nothing is spent).

A prize the tree lacks that is neither withheld, equipment, nor a lose-set
expendable refuses the build: the pool never pays an item with no lane.

===========================================================================
"""
import json
import os
import re
from contextlib import ExitStack
from pathlib import Path

import alexandria_npcs as text_tables
import cap110_item_import as shared
import cap110_items as items
import cap110_monsters as cap110
import import_vsro_alexandria as tables
import jupiter_import as jupiter
import later_mall_goods as goods
import mall_avatar_import as avatars
import nasrun_import as nasrun
import published_ids
import sro_pk2
from prepare_client_resources import safe_target, write_atomically
from sro_paths import trees_overlap

FLAG = "SRO_MAGIC_POP_V1696"
ROOT_ENV = "SRO_V1696_ROOT"
MARKER = ".opensro-magic-pop.json"
FORMAT = "opensro-magic-pop-v1"
# Every band from 100000 to 330000 is taken; 190002 is already used twice.
ITEM_SHARD = "itemdata_340000.txt"
NAME_TABLE = "textdataname.txt"
UI_TABLE = "textuisystem.txt"
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA
FLAG_OFF = ("0", "off", "false", "no")

# The v1.696 source tables (Media.pk2 server_dep/silkroad/textdata).
SOURCE_POOL = "gachaitemset.txt"
SOURCE_NPC_MAP = "gachanpcmap.txt"
SOURCE_TREE = "refgachatreeforclientuidata.txt"
# What this layer writes, beside the native tables.
POOL_TABLE = "gachaitemset_v1696.txt"
NPC_MAP_TABLE = "gachanpcmap_v1696.txt"
TREE_TABLE = "refgachatreeforclientuidata.txt"
GACHA_TABLES = (POOL_TABLE, NPC_MAP_TABLE, TREE_TABLE)

# gachaitemset.txt, 0-based: enabled, set, reward, weight, quantity,
# entry, ..., minimum level cap (the client shows a prize only when the
# level cap reaches it, v1.696 0x7D47A0).
POOL_COLUMNS = 20
POOL_ENABLED, POOL_SET, POOL_REWARD, POOL_ENTRY, POOL_MIN_LEVEL = 0, 1, 2, 5, 15
NORMAL_SETS = ("1", "2")
# Degree 11 opens at cap 101 (degree 10 at 91).
DROPPED_MIN_LEVEL = 101
# refgachatreeforclientuidata.txt: the group and subgroup label columns.
TREE_COLUMNS = 10
TREE_LABELS = (6, 8)
# gachanpcmap.txt: enabled, machine NPC, then the sets.
NPC_MAP_NPC = 1
MACHINE_ID = 9251
MACHINE_CODENAME = "NPC_CH_GACHA_MACHINE"
CHARACTER_ID, CHARACTER_CODE = 1, 2

# ================
# WITHHELD
#
# Prize codenames left out of the pool, with why. Owner-dropped prizes
# stay out; a pending-effect line goes when the change that makes that
# prize work lands (it imports the item itself). A codename matches the
# first pattern that fits.
# ================
WITHHELD = (
	# Owner, 2026-11-27: the v1.696 fellow pets have no server lane.
	(re.compile(r"^ITEM_(EVENT|PRE)_PET2_SCROLL_"), "dropped: PET2 fellow pet scroll"),
	# Pending: event Spirits (3.1.14.x) grant no ability yet.
	(re.compile(r"^ITEM_EVENT_AVATAR_[MW]_(NASRUN|AMALRUN)_UNIQUE$"), "pending: event Spirit"),
	# Pending: event avatars (3.1.13.x) need their own import.
	(re.compile(r"^ITEM_EVENT_AVATAR_"), "pending: event avatar"),
	# Pending: the attach-slot flags (3.1.13.4); the Roc has no worn model.
	(re.compile(r"^ITEM_ETC_E090429_(GACHA_)?ROC_[MW]$"), "pending: Roc flag"),
	(re.compile(r"^ITEM_ETC_E060529_GACHA_GOLDDRAGONFLAG_"), "pending: gold dragon flag"),
	# Owner, 2026-11-28: the lose set's prizes (the skill point scroll, the
	# alchemy catalyst, the summon flowers, the Hwan look items) are no
	# longer withheld; they ship as native and their effects follow.
)
# The lose set (gachanpcmap column 4 of the normal card).
LOSE_SET = "2"
EXPENDABLE = ("3", "3")

# The v1.696 event gear's twentieth parameter (0-based cells 156, 157):
# "maximum sockets the item may hold", 0 or 3. The v1.150 rare row keeps
# that pair unused.
SOCKET_PARAM = 156
SOCKET_LABEL = "소유 가능한 최대 소켓 수"
EVENT_PREFIX = "ITEM_EVENT_"
MAX_DEGREE = 10


# ================
# flag_enabled
#
# The port's default: unset is on.
# ================
def flag_enabled(env=os.environ):
	return env.get(FLAG, "").strip().lower() not in FLAG_OFF


# ================
# withheld_reason
# ================
def withheld_reason(code):
	for pattern, reason in WITHHELD:
		if pattern.search(code):
			return reason
	return None


# ================
# gear_degree
#
# The degree of an equipment row by its class cell (three classes per
# degree, enterworld.EquipmentDegree's rule); 0 for anything else. Unlike
# cap110_items.equipment_degree, an ITEM_EVENT_ code is admitted.
# ================
def gear_degree(row):
	if row[items.SERVICE] != "1" or row[items.TYPE_FIRST:items.TYPE_SECOND + 1] != ["3", "1"]:
		return 0
	if row[items.TYPE_THIRD] not in items.EQUIPMENT_TYPES or row[items.RACE] not in items.RACES:
		return 0
	item_class = int(row[items.CLASS])
	return (item_class - 1) // items.CLASSES_PER_DEGREE + 1 if item_class > 0 else 0


# ================
# convert_gear
#
# One v1.696 prize gear row in the v1.150 layout under identity. Only the
# socket parameter is reset; any other authored parameter refuses, so no
# later mechanic rides in unread.
# ================
def convert_gear(row, identity):
	code = row[items.CODENAME]
	if len(row) != items.ITEM_COLUMNS:
		raise ValueError(f"Magic POP prize {code} is not a {items.ITEM_COLUMNS}-column item row")
	degree = gear_degree(row)
	if not 1 <= degree <= MAX_DEGREE:
		raise ValueError(f"Magic POP prize {code} has no import lane (not degree 1-{MAX_DEGREE} equipment)")
	if not 0 < identity <= items.MAX_ID:
		raise ValueError("imported item ID exceeds the wire width")
	for column in range(items.PARAM_FIRST, SOCKET_PARAM, items.PARAM_STRIDE):
		if not (row[column] == "-1" and row[column + 1] == items.UNUSED):
			raise ValueError(f"Magic POP prize {code} authors parameter {row[column + 1]!r} at {column}")
	converted = list(row)
	socket = row[SOCKET_PARAM:SOCKET_PARAM + items.PARAM_STRIDE]
	if socket != ["-1", items.UNUSED]:
		if socket[1] != SOCKET_LABEL:
			raise ValueError(f"Magic POP prize {code} authors parameter {socket[1]!r} at {SOCKET_PARAM}")
		converted[SOCKET_PARAM:SOCKET_PARAM + items.PARAM_STRIDE] = ["-1", items.UNUSED]
	converted[items.IDENTITY], converted[items.PLACEHOLDER_NAME] = str(identity), items.UNUSED
	return converted


# ================
# select_pool
#
# The surviving pool rows and why the others went. later_codes maps the
# v1.696 item ids to codenames; a pool row naming an id the client has no
# item for refuses.
# ================
def select_pool(pool_rows, later_codes):
	kept, dropped = [], {}
	for row in pool_rows:
		if not row or not row[0].strip():
			continue
		if len(row) != POOL_COLUMNS:
			raise ValueError(f"gachaitemset row has {len(row)} columns, want {POOL_COLUMNS}")
		if row[POOL_ENABLED] != "1" or row[POOL_SET] not in NORMAL_SETS:
			continue
		code = later_codes.get(row[POOL_REWARD])
		if code is None:
			raise ValueError(f"gachaitemset entry {row[POOL_ENTRY]} rewards unknown item {row[POOL_REWARD]}")
		reason = "dropped: degree 11" if int(row[POOL_MIN_LEVEL]) >= DROPPED_MIN_LEVEL else withheld_reason(code)
		if reason:
			dropped[reason] = dropped.get(reason, 0) + 1
			continue
		kept.append((row, code))
	entries = [row[POOL_ENTRY] for row, _ in kept]
	if len(entries) != len(set(entries)):
		raise ValueError("gachaitemset repeats an entry id")
	for set_id in NORMAL_SETS:
		if not any(row[POOL_SET] == set_id for row, _ in kept):
			raise ValueError(f"the Magic POP pool keeps no set {set_id} row")
	return kept, dropped


# ================
# tree_item_ids
#
# This tree's item ids by codename, every listed shard but this layer's.
# ================
def tree_item_ids(read):
	ids = {}
	raw = read("itemdata.txt")
	for row in tables.table_rows(raw or b""):
		if not row or not row[0].strip() or sro_pk2.fold_ascii(row[0].strip()) == ITEM_SHARD:
			continue
		shard = read(sro_pk2.fold_ascii(row[0].strip()))
		for cells in tables.table_rows(shard or b""):
			if len(cells) > items.CODENAME and cells[items.SERVICE] == "1":
				ids.setdefault(cells[items.CODENAME], int(cells[items.IDENTITY]))
	return ids


# ================
# check_machine
#
# The machine the v1.696 map names is this tree's NPC_CH_GACHA_MACHINE.
# ================
def check_machine(read, npc_rows):
	machines = {row[NPC_MAP_NPC] for row in npc_rows if row and row[0].strip() == "1"}
	if machines != {str(MACHINE_ID)}:
		raise ValueError(f"gachanpcmap names machines {sorted(machines)}, want {MACHINE_ID}")
	characters = goods.sharded_ids(read, "characterdata.txt", CHARACTER_CODE, CHARACTER_ID)
	if characters.get(MACHINE_CODENAME) != str(MACHINE_ID):
		raise ValueError(f"{MACHINE_CODENAME} is not object {MACHINE_ID} in this tree")


# ================
# remove_assets
#
# The later files a marker records. A native-sourced copy stays: another
# layer may have copied the same native file.
# ================
def remove_assets(output, records):
	for record in records:
		if record.get("source") == "later":
			safe_target(output / shared.FOLDERS[record["archive"]], record["path"]).unlink(missing_ok=True)


# ================
# remove_import
#
# The off path: the shard, its manifest line, the three gacha tables, the
# later files and the marker. The texts went before this runs.
# ================
def remove_import(output, prior, marker, text_root):
	safe_target(text_root, ITEM_SHARD).unlink(missing_ok=True)
	manifest = safe_target(text_root, "itemdata.txt")
	if manifest.exists():
		names = [row[0] for row in tables.table_rows(manifest.read_bytes()) if row]
		kept = [name for name in names if sro_pk2.fold_ascii(name) != ITEM_SHARD]
		if len(kept) != len(names):
			write_atomically(manifest, cap110.encode_rows([[name] for name in kept]))
	for name in GACHA_TABLES:
		safe_target(text_root, name).unlink(missing_ok=True)
	remove_assets(output, prior.get("assets", []))
	marker.unlink(missing_ok=True)


# ================
# ui_labels
#
# The tree's label rows the UI table lacks, from the later client's own
# English. A label the later client has no text for is reported, never
# synthesized.
# ================
def ui_labels(ui_raw, tree_rows, media, index):
	known = {row[1] for row in tables.table_rows(ui_raw or b"") if len(row) > 1}
	wanted = sorted({row[column] for row in tree_rows for column in TREE_LABELS} - known)
	found = goods.later_label_texts(media, index, set(wanted))
	return [goods.tab_label_row(symbol, found[symbol]) for symbol in wanted if symbol in found], \
		[symbol for symbol in wanted if symbol not in found]


# ================
# add_magic_pop
#
# The variant step (variant_later_layers): stage the pool, its tables and
# its missing prize items, or reap a previous import.
# ================
# convert_carried
#
# One v1.696 lose-set expendable (TID 3.3) under identity, every parameter
# kept as the later client authors it: the owner ships the lose set as
# native, and each prize's effect change reads those parameters.
# ================
def convert_carried(row, identity):
	code = row[items.CODENAME]
	if len(row) != items.ITEM_COLUMNS:
		raise ValueError(f"Magic POP prize {code} is not a {items.ITEM_COLUMNS}-column item row")
	if tuple(row[items.TYPE_FIRST:items.TYPE_SECOND + 1]) != EXPENDABLE:
		raise ValueError(f"Magic POP prize {code} has no import lane (not an expendable)")
	if not 0 < identity <= items.MAX_ID:
		raise ValueError("imported item ID exceeds the wire width")
	converted = list(row)
	converted[items.IDENTITY], converted[items.PLACEHOLDER_NAME] = str(identity), items.UNUSED
	return converted


# ================
def add_magic_pop(native, output, v1696_root=None, env=os.environ):
	native, output = Path(native).resolve(), Path(output).resolve()
	if trees_overlap(native, output):
		raise ValueError("Magic POP overlay must be separate from native extraction")
	marker = output / MARKER
	prior = cap110.read_json(marker) if marker.exists() else {}
	text_root = output / TEXT_ROOT
	name_path = safe_target(text_root, NAME_TABLE)
	# A rebuild, and the off path, start from the tables without this
	# import's own text rows.
	nasrun.strip_texts(name_path, set(prior.get("texts", [])))
	avatars.strip_text_rows(output, set(prior.get("uiTexts", [])))
	named = str(v1696_root or "").strip() or env.get(ROOT_ENV, "").strip()
	if not flag_enabled(env) or not named:
		remove_import(output, prior, marker, text_root)
		reason = f"{FLAG} is off" if not flag_enabled(env) else f"{ROOT_ENV} is not set"
		return {"enabled": False, "reason": reason}
	root = Path(named)
	jupiter.require_v1696_pin(root, tuple(shared.FOLDERS))
	read = goods.tree_reader(native, output)
	with ExitStack() as stack:
		archives, identities = avatars.open_archives(stack, root, avatars.V1696_SOURCE)
		media, index = archives["Media"]

		# ================
		# read_later
		# ================
		def read_later(name):
			return tables.table_text(media, index, name)

		later_rows = [row for row in shared.read_item_tables(read_later)
			if len(row) == items.ITEM_COLUMNS and row[items.SERVICE] == "1"]
		later_by_code = {}
		for row in later_rows:
			later_by_code.setdefault(row[items.CODENAME], row)
		later_codes = {row[items.IDENTITY]: row[items.CODENAME] for row in later_rows}
		pool, dropped = select_pool(tables.table_rows(read_later(SOURCE_POOL)), later_codes)
		npc_rows = [row for row in tables.table_rows(read_later(SOURCE_NPC_MAP)) if row and row[0].strip()]
		tree_rows = [row for row in tables.table_rows(read_later(SOURCE_TREE)) if row and row[0].strip()]
		if any(len(row) != TREE_COLUMNS for row in tree_rows):
			raise ValueError(f"refgachatreeforclientuidata rows must have {TREE_COLUMNS} columns")
		check_machine(read, npc_rows)

		present = tree_item_ids(read)
		missing = sorted({code for _, code in pool if code not in present})
		occupied = published_ids.occupied_objects(text_root, (ITEM_SHARD,))
		allocated = published_ids.allocate(published_ids.OBJECTS, occupied, missing, prior.get("items", {}))
		lose_codes = {code for row, code in pool if row[POOL_SET] == LOSE_SET}
		carried_codes = {code for code in missing if code in lose_codes and gear_degree(later_by_code[code]) == 0}
		gear = [convert_gear(later_by_code[code], allocated[code]) for code in missing if code not in carried_codes]
		carried = [convert_carried(later_by_code[code], allocated[code]) for code in missing if code in carried_codes]
		items.plain_worn_models(gear)
		staged = gear + carried

		later_texts = [row for name in nasrun.LATER_TEXT_TABLES for row in avatars.later_text_rows(media, index, name)]
		base_names = name_path.read_bytes() if name_path.exists() else (native / TEXT_ROOT / NAME_TABLE).read_bytes()
		merged_names, absent_descriptions = items.merge_texts(base_names, later_texts, staged)
		known = {row[1] for row in tables.table_rows(base_names) if len(row) > 1}
		added_texts = sorted({row[1] for row in tables.table_rows(merged_names) if len(row) > 1} - known)
		ui_path = safe_target(text_root, UI_TABLE)
		ui_raw = read(UI_TABLE)
		labels, absent_labels = ui_labels(ui_raw, tree_rows, media, index)
		# Carried prizes have an icon and a drop model but no worn mesh.
		assets = shared.copy_asset_closure(gear, native, output, archives, worn=True) if gear else []
		if carried:
			assets += shared.copy_asset_closure(carried, native, output, archives, worn=False)
	kept_paths = {(record["archive"], record["path"]) for record in assets}
	remove_assets(output, [record for record in prior.get("assets", [])
		if (record["archive"], record["path"]) not in kept_paths])

	ids = {**present, **{code: allocated[code] for code in missing}}
	pool_out = []
	for row, code in pool:
		cells = list(row)
		cells[POOL_REWARD] = str(ids[code])
		pool_out.append(cells)
	staged.sort(key=lambda row: int(row[items.IDENTITY]))
	manifest = safe_target(text_root, "itemdata.txt")
	current = read("itemdata.txt")
	names = [row[0] for row in tables.table_rows(current or b"") if row]
	names = [name for name in names if sro_pk2.fold_ascii(name) != ITEM_SHARD]
	if staged:
		write_atomically(safe_target(text_root, ITEM_SHARD), cap110.encode_rows(staged))
		names.append(ITEM_SHARD)
	else:
		safe_target(text_root, ITEM_SHARD).unlink(missing_ok=True)
	write_atomically(manifest, cap110.encode_rows([[name] for name in names]))
	write_atomically(name_path, merged_names)
	if labels:
		write_atomically(ui_path, text_tables.append_rows(ui_raw, labels))
	write_atomically(safe_target(text_root, POOL_TABLE), cap110.encode_rows(pool_out))
	write_atomically(safe_target(text_root, NPC_MAP_TABLE), cap110.encode_rows(npc_rows))
	write_atomically(safe_target(text_root, TREE_TABLE), cap110.encode_rows(tree_rows))
	per_set = {set_id: sum(1 for row in pool_out if row[POOL_SET] == set_id) for set_id in NORMAL_SETS}
	summary = {
		"enabled": True, "format": FORMAT, "source": "v1696",
		"laterArchiveHashes": identities,
		"pool": {"rows": len(pool_out), "perSet": per_set, "dropped": dict(sorted(dropped.items()))},
		"items": {row[items.CODENAME]: int(row[items.IDENTITY]) for row in staged},
		"texts": added_texts, "absentDescriptions": absent_descriptions,
		"uiTexts": sorted(row[1] for row in labels), "absentUiTexts": absent_labels,
		"assets": assets,
		"limits": [
			"The server reads gachaitemset_v1696.txt only with SRO_MAGIC_POP_V1696 on.",
			"v1.696 authors no glow or seal row for ITEM_EVENT_ gear; none is added.",
			"Premium card sets 3 and 4 are not staged.",
		],
	}
	write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode())
	return summary
