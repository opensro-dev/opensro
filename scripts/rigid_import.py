"""
===========================================================================

rigid_import.py - the Rigid L2 unique summon scrolls' kinds and items (#3785)

Later-native content, not a port-only option: the later clients
widened the unique ladder with down-scaled "Rigid" copies of the
field uniques (MOB_<X>_L2), and a summon scroll per copy - use one
and the copy lands beside you. The use path is in the v1.188 server
(the expendable handler reads the monster codename from the item's
first parameter description and spawns it at the user), so the
scrolls ship ON with the later content: no SRO_ switch, in the build
or on the server (owner 2026-11-24: "if its native in later
versions, we should have it"). The layer follows the later client
root like every later layer: without SRO_V1696_ROOT it stages
nothing and a previous import is reaped, byte-native.

The eight scroll items stand in no staged client - the v1.150, VSRO
and v1.696 Media tables were searched whole, and the ISRO-R refdata
file export has none either; the rows live only in the ISRO-R shard
database, which the owner ruled a legitimate gap-fill source
(2026-11-24). scripts/build/import_rigid_scroll_rows.py extracts
them there once; this import stages the committed extraction
(scripts/data/rigid-scroll-rows.json), never the backup itself.
What the import authors is only the name and description texts: the
ISRO-R text tables carry the SN_ITEM_RIGID_* symbols in no staged
source, so both texts are authored English here (the items' Korean
ObjName comes from the extracted rows).

  - The four kinds no staged client row reaches the variant with
    (MOB_AM_IVY_L2, MOB_EU_KERBEROS_L2, MOB_TQ_BLACKSNAKE_L2 and its
    L3), converted from the pinned v1.696 client by the later-world
    merge (later_world_import.stage_kinds), with their skills, names
    and characterinfo rows. The other four scrolls name kinds the
    v1.150 tables already hold. No placement: the scrolls land them.
  - The eight scroll items verbatim from the extraction (identity
    reallocated under this import's marker): family type 3,3,3,23,
    required level 90, parameter 1's description the monster each
    summons, parameter 4 = COOLTIME:0x000000F1 carrying 600000 ms -
    the family cooltime the server enforces (rigidsummonscroll.go).

Unstocked by design: in ISRO-R these sell from the Battle Arena and
Survival Arena stores, which the v1.150 feature set does not have;
no later official table stocks or drops them, so they ship
obtainable only through GM/event distribution until the owner
places them.

The off path (no later root) removes exactly the rows and files the
marker records.

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
import winter_event_import as winter
import sro_pk2
from prepare_client_resources import safe_target, write_atomically

ROOT_ENV = "SRO_V1696_ROOT"
MARKER = ".opensro-rigid-scrolls.json"
# The 320000 identity band: every earlier band is a named layer's (test_later_shard_bands.py).
CHARACTER_SHARD = "characterdata_320000.txt"
SKILL_SHARD = "skilldata_320000.txt"
ITEM_SHARD = "itemdata_320000.txt"
PIN_FILE = Path(__file__).with_name("v1696_archive_pins.json")
# Data too, not Media alone: the scrolls' drop model resolves from
# it through the item asset closure, and the tuple feeds the pin
# (the pin file carries Data). Dropping it kills the build's rigid
# layer at the closure (review T5145).
ARCHIVES = ("Data", "Media")
ROWS_FILE = Path(__file__).with_name("data") / "rigid-scroll-rows.json"
# The kinds this import stages: the L2 copies (and the Black Snake's
# L3) the v1.150 character tables lack.
KINDS = ("MOB_AM_IVY_L2", "MOB_EU_KERBEROS_L2", "MOB_TQ_BLACKSNAKE_L2", "MOB_TQ_BLACKSNAKE_L3")
# itemdata columns (cap110_items.py's layout): identity, codename,
# name and description symbols, required level.
ITEM_ID, ITEM_CODE, ITEM_NAME, ITEM_DESC, ITEM_REQ_LEVEL = 1, 2, 5, 6, 33
ITEM_COLUMNS = 160
# The twenty parameter slots: value at 118 + 2k, description at 119 + 2k.
# A slot whose description is "xxx" is unused; "-1" is the "no value"
# every other unused slot in the shipped tables reads.
PARAM_FIRST, PARAM_SLOTS, PARAM_UNUSED, PARAM_NONE = 118, 20, "xxx", "-1"
# Scroll codename -> (monster codename, display name for the authored
# texts). The monster link is the native row's first parameter
# description; the port's handler carries the same table
# (apps/server internal/game/action/rigidsummonscroll.go).
SCROLLS = {
	"ITEM_RIGID_TIGERWOMAN_L2_SUMMON_SCROLL": ("MOB_CH_TIGERWOMAN_L2", "Tiger Girl"),
	"ITEM_RIGID_KERBERUS_L2_SUMMON_SCROLL": ("MOB_EU_KERBEROS_L2", "Cerberus"),
	"ITEM_RIGID_URUCHI_L2_SUMMON_SCROLL": ("MOB_OA_URUCHI_L2", "Uruchi"),
	"ITEM_RIGID_IVY_L2_SUMMON_SCROLL": ("MOB_AM_IVY_L2", "Captain Ivy"),
	"ITEM_RIGID_ISYUTARU_L2_SUMMON_SCROLL": ("MOB_KK_ISYUTARU_L2", "Isyutaru"),
	"ITEM_RIGID_BONELORD_L2_SUMMON_SCROLL": ("MOB_TK_BONELORD_L2", "Lord Yarkan"),
	"ITEM_RIGID_TAHOMET_L2_SUMMON_SCROLL": ("MOB_RM_TAHOMET_L2", "Demon Shaitan"),
	"ITEM_RIGID_BLACKSNAKE_L2_SUMMON_SCROLL": ("MOB_TQ_BLACKSNAKE_L2", "Selket"),
}
ITEM_NAME_TEXT = "Unique Summon Scroll (L2)"
# The English for the three staged kinds whose names v1.696 ships in
# Korean only, translated from that Korean (#4287). They ride the rows
# this layer stages (stage_kinds english), not the retail completion
# catalog: a catalog key the tree lacks fails every build without the
# v1.696 client (#4466). The other five scrolls' kinds are v1.150 rows.
KIND_NAMES = {
	"SN_MOB_TQ_BLACKSNAKE_L2": "Cheonha-class Black Snake Soso",
	"SN_MOB_EU_KERBEROS_L2": "Taebaek-class Cerberus",
	"SN_MOB_AM_IVY_L2": "Geumgang-class Captain Ivy",
}

SPEC = later_world_import.WorldImport(
	label="rigid scrolls",
	flag="",
	root_env=ROOT_ENV,
	character_shard=CHARACTER_SHARD,
	skill_shard=SKILL_SHARD,
	marker=MARKER,
	pin_file=PIN_FILE,
	pin_sentence="The rigid scroll kinds are built from one pinned v1.696 client; its archives are never committed.",
	region_label="rigid scrolls",
	population_files=(),
)


# ================
# scroll_rows
#
# The eight extracted item rows from the committed extraction
# (scripts/build/import_rigid_scroll_rows.py refills it from the
# ISRO-R shard backup), verbatim but for unused_parameters below. The
# extraction is pinned here: a row that drifts from its monster link,
# family type, required level or cooltime refuses the build rather
# than staging silently.
# ================
def scroll_rows():
	document = json.loads(ROWS_FILE.read_text(encoding="utf-8"))
	if document.get("format") != "opensro-rigid-scroll-rows-v1":
		raise ValueError(f"{ROWS_FILE.name} is not a rigid scroll extraction")
	rows = []
	for entry in document["rows"]:
		code, row = entry["codename"], list(entry["row"])
		monster, _name = SCROLLS[code]
		if len(row) != ITEM_COLUMNS or row[ITEM_CODE] != code:
			raise ValueError(f"{code}: the extraction's row is not a 160-column itemdata row")
		if row[9:13] != ["3", "3", "3", "23"] or row[ITEM_REQ_LEVEL] != "90":
			raise ValueError(f"{code}: unexpected family type or required level in the extraction")
		if row[118] != "-1" or row[119] != monster or row[124] != "600000" or row[125] != "COOLTIME:0x000000F1":
			raise ValueError(f"{code}: unexpected monster link or cooltime in the extraction")
		rows.append(unused_parameters(row))
	if {row[ITEM_CODE] for row in rows} != set(SCROLLS):
		raise ValueError("the extraction does not carry every rigid scroll")
	return rows


# ================
# unused_parameters
#
# The backup carries two damaged unused slots: URUCHI parameter 5 is
# -257 (0xFFFFFEFF) and IVY parameter 18 is -65537 (0xFFFEFFFF), each
# the "no value" -1 with one bit cleared. Across every staged itemdata
# row (about 200,000 unused slots) no other value below -1 occurs, and
# the item loaders refuse one (alchemy.LoadCatalog: "parameter %d"),
# which stopped the r204k catalogue. The value is unread, since its
# description is "xxx", so the repair is the value the slot means: an
# unused slot holding a negative number other than -1 reads -1 again.
# A positive value in an unused slot is a real one (retail rows hold
# 0, 1, 50 and 100 there) and is kept. A defect in the source is
# fixed, not copied (owner, 2026-11-24).
# ================
def unused_parameters(row):
	fixed = list(row)
	for k in range(PARAM_SLOTS):
		value, description = PARAM_FIRST + 2 * k, PARAM_FIRST + 2 * k + 1
		if fixed[description] != PARAM_UNUSED or fixed[value] == PARAM_NONE:
			continue
		if fixed[value].startswith("-") and fixed[value][1:].isdigit():
			fixed[value] = PARAM_NONE
	return fixed


# ================
# authored_texts
#
# The name and description rows the scrolls name, in the textdataname
# layout (service, symbol, then the text in the two text columns the
# later-name merge fills). Authored: no staged source carries the
# SN_ITEM_RIGID_* symbols (the extraction's rows name them; the
# URUCHI row's own SUMNON spelling is kept, as the row names it).
# ================
def authored_texts(rows):
	out = []
	for row in rows:
		monster, display = SCROLLS[row[ITEM_CODE]]
		name_symbol, desc_symbol = row[ITEM_NAME], row[ITEM_DESC]
		description = (f"Use to summon {display} beside you. The unique stays until it is defeated. "
			"Cannot be used in a town. Summon scrolls share a 10-minute cooldown.")
		out.append(["1", name_symbol, "", "", "", ITEM_NAME_TEXT, "", "", ITEM_NAME_TEXT, "", ""])
		out.append(["1", desc_symbol, "", "", "", description, "", "", description, "", ""])
	return out


# ================
# add_rigid
#
# The variant layer's entry point (variant_later_layers.py). Always
# on with the later content: without the v1.696 client a previous
# import is reaped and no archive is read.
# ================
def add_rigid(native, out, env=os.environ):
	native, output = Path(native).resolve(), Path(out).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("rigid scroll overlay must be separate from native extraction")
	marker = output / MARKER
	prior = cap110.read_json(marker) if marker.exists() else {}
	text_root = output / later_world_import.TEXT_ROOT
	root = env.get(ROOT_ENV, "").strip()
	if not root:
		reap_items(output, prior)
		later_world_import.reap_monsters(SPEC, output, prior)
		return {"enabled": False, "reason": f"{ROOT_ENV} is not set"}
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

		later = tables.later_characters(read)
		refs = {}
		for code in KINDS:
			row = later.get(code)
			if row is None:
				raise ValueError(f"the v1.696 client carries no character row {code}")
			refs[code] = characters.convert_character(row)
		kinds = later_world_import.stage_kinds(SPEC.label, read, has, refs, text_root, prior, CHARACTER_SHARD, SKILL_SHARD,
			lambda code: code in refs, english=KIND_NAMES)
		staged = add_items(native, output, archives, prior)
	summary = {"enabled": True, **kinds, **staged}
	write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode())
	return summary


# ================
# add_items
#
# The extracted scroll rows under marker-kept identities, the icon
# and model closure they name, and the authored name rows, in the
# item shard of this import's band.
# ================
def add_items(native, output, archives, prior):
	text_root = output / later_world_import.TEXT_ROOT
	native_text = Path(native) / later_world_import.TEXT_ROOT
	rows = scroll_rows()
	occupied = published_ids.occupied_objects(text_root, (ITEM_SHARD,))
	allocated = published_ids.allocate(published_ids.OBJECTS, occupied, [row[ITEM_CODE] for row in rows], prior.get("items", {}))
	for row in rows:
		row[ITEM_ID] = str(allocated[row[ITEM_CODE]])
	write_atomically(safe_target(text_root, ITEM_SHARD), cap110.encode_rows(rows))
	manifest = text_root / "itemdata.txt"
	current = manifest.read_bytes() if manifest.exists() else (native_text / "itemdata.txt").read_bytes()
	names = [r[0] for r in tables.table_rows(current) if r and r[0].lower() != ITEM_SHARD]
	write_atomically(manifest, cap110.encode_rows([[name] for name in names + [ITEM_SHARD]]))
	resources = item_import.copy_asset_closure(rows, Path(native), output, archives, worn=False)
	texts = authored_texts(rows)
	symbols = [row[1] for row in texts]
	name_path = text_root / "textdataname.txt"
	write_atomically(name_path, winter.replace_text_rows(name_path.read_bytes(), symbols, texts))
	return {
		"items": {row[ITEM_CODE]: int(row[ITEM_ID]) for row in rows},
		"itemSymbols": symbols,
		"itemResources": [record for record in resources if record["source"] == "later"],
	}


# ================
# reap_items
#
# The item half of an off build: the shard, its manifest line, the
# authored name rows and the resources a previous build copied from
# the later archives leave; everything else stays.
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
	name_path = text_root / "textdataname.txt"
	symbols = set(prior.get("itemSymbols", []))
	if symbols and name_path.exists():
		write_atomically(name_path, winter.drop_text_rows(name_path.read_bytes(), symbols))
	for record in prior.get("itemResources", []):
		folder = item_import.FOLDERS.get(record.get("archive"))
		if folder:
			safe_target(output / folder, record["path"]).unlink(missing_ok=True)
