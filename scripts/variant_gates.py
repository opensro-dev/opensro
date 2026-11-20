"""
===========================================================================

variant_gates.py - the Alexandria variant's teleport-gate tables

The gate rows the variant carries (the city's gates and ferry, the
authored crossings, the tomb and temple gates behind their flags), the
level cap their conditions are clamped to, and the pure merge of the
VSRO teleportdata, teleportbuilding and teleportlink rows into the native
tables. Pure functions over table bytes: build_alexandria_variant.add_gates
reads the tables and writes the merge, and the later importers (Arabia,
Roc, Sky Temple, the cap-110 quests) reuse the merge for their own gates.
Split out of build_alexandria_variant.py, which kept the file writing.

===========================================================================
"""
import os

import import_vsro_alexandria as stage1
import pharaoh_temple as temple
import published_ids
from alexandria_map_tables import append_rows, native_text_row

# Port-only, not native: the city's gates and ferry, plus the two authored
# outdoor crossings: Delta East is Abundance Ground; Delta West is Forbidden Plain.
# Ferry teleportdata names an NPC, not a teleportbuilding.
# The crossings' far ends come from stage 1, which imports their terrain.
GATE_CODES = (
	"GATE_SD1", "GATE_SD2", "GATE_NPC_SD_M_AREA_FERRY", "GATE_DELTA_GATE_E", "GATE_DELTA_GATE_W"
) + stage1.CROSSING_GATE_CODES
# Port-only, not native: the Pharaoh Tomb's gates, added with SRO_PHARAOH_TOMB.
# The one entrance in Kings Valley links to the three tomb exits. The source's
# GATE_TOMBOSIRIS_* rows have no position and are not carried.
TOMB_GATE_CODES = (
	"GATE_TOMB_GATE_IN", "GATE_TOMB_GATE_OUT_1", "GATE_TOMB_GATE_OUT_2", "GATE_TOMB_GATE_OUT_3"
)
# Port-only, not native: the Pharaoh Temple's four gates, added with
# SRO_PHARAOH_TEMPLE (pharaoh_temple.py): the two clicked desert gates and the
# two walk-in gates inside the Temple. Their world column is flattened (below).
TEMPLE_GATE_CODES = temple.GATE_CODES
# The last teleportdata column names the destination game world. The source's
# tomb exits name worlds 57-59, one per tomb. The v1.150 world table does not
# define them, but the port does while SRO_PHARAOH_TOMB is on (the three
# permanent per-tomb worlds, #1750), so the column stands as authored and the
# server resolves it through its widened world catalogue.
TELEPORT_WORLD_COLUMN = 12
# The v1.150 server's level cap (levels.json levelCap). The VSRO links need
# level 95 for a cap of 110; clamped here so the city stays reachable.
NATIVE_LEVEL_CAP = 90
# The server's level-cap flag (progression/levelcap.go LevelCapFromEnv): the
# gate conditions are clamped to the cap the shard runs, read with the same
# values the server accepts so build and server can never disagree.
LEVEL_CAP_FLAG = "SRO_LEVEL_CAP"
EXPANSION_LEVEL_CAP = 110
LEVEL_CAP_NATIVE_VALUES = ("", "0", "off", "false", "90")
LEVEL_CAP_EXPANSION_VALUES = ("110",)
LINK_COLUMNS = 21
LINK_KIND_LEVEL = 1
# The condition kinds the v1.150 server's portal catalogue evaluates
# (portalAdmission): 1 level, 2 transport, 4 and 5 an item held (5 spends
# one), 6 outside job mode (portal_conditions.go, #3399), 7 thief suit
# with a transport (#1749 PR-C(ii)). Any other kind is dropped and
# reported, below.
LINK_KINDS_EVALUATED = (1, 2, 4, 5, 6, 7)
# The item kinds: Data_1 is an item identity of the source client, which
# the variant's item tables number differently.
LINK_KINDS_ITEM = (4, 5)
TELEPORT_COLUMNS = 13
BUILDING_COLUMNS = 58


# ================
# by_column
# ================
def by_column(raw, column, width=0):
	"""{value of column: row} of a table, keeping rows at least width wide."""
	return {row[column]: row for row in stage1.table_rows(raw) if len(row) > column and len(row) >= width}


# ================
# by_ids
#
# The object ids an object table's rows hold.
# ================
def by_ids(raw):
	return {int(row[1]) for row in stage1.table_rows(raw) if len(row) > 1 and row[1].isdigit()}


# ================
# gate_buildings
#
# A gate building is an object: its id shares one space with every item
# and character (#3994). A VSRO building id another object already holds
# moves to a fresh id from the id owner (published_ids), in place in
# buildings; returns {old id: new id} for the gate rows that name it.
# Released items keep theirs: r203q shipped six Halloween avatars on the
# ids of the Alexandria and temple gate buildings (27751-27756), and the
# client resolved one object for both. The server and the hiding rules
# name a building by code, never by id, so the building is free to move.
# ================
def gate_buildings(buildings, taken):
	clashing = [row for row in buildings if int(row[1]) in taken]
	if not clashing:
		return {}
	occupied = set(taken) | {int(row[1]) for row in buildings}
	fresh = published_ids.allocate_identities(occupied, [row[2] for row in clashing], {})
	moved = {}
	for row in clashing:
		moved[row[1]] = str(fresh[row[2]])
		row[1] = moved[row[1]]
	return moved


# ================
# translate_link
# ================
def translate_link(row, ids, level_cap, dropped=None, items=None):
	"""
	One VSRO teleportlink row for the native table: its two ends renumbered by
	teleport code, the scheduling word set to the one v1.150 server accepts
	(1), a level condition clamped to the build's cap, an item condition's
	item renumbered through items ({source item id: variant item id}) and
	conditions of a kind the server does not evaluate dropped. Every drop is
	appended to dropped (when given) as {"from", "to", "kind", "operands"}
	with the renumbered ends, so the loss is reported, never silent (#1749
	PR-C(i); kind 3 still has no port evaluator).

	Returns None when an item condition names an item items does not map:
	dropping that condition would open a keyed gate to everyone, so the
	whole link stays out (merge_gate_tables reports it).
	"""
	columns = list(row[:LINK_COLUMNS])
	columns[1], columns[2] = ids[row[1]], ids[row[2]]
	columns[4], columns[5] = "1", "0"
	for at in range(6, LINK_COLUMNS, 3):
		if columns[at] == str(LINK_KIND_LEVEL) and int(columns[at + 1]) > level_cap:
			columns[at + 1] = str(level_cap)
		if int(columns[at]) in LINK_KINDS_ITEM:
			item = (items or {}).get(columns[at + 1])
			if item is None:
				return None
			columns[at + 1] = item
			continue
		# A condition kind the v1.150 server does not evaluate is dropped;
		# the server refuses the whole catalogue otherwise.
		if int(columns[at]) != 0 and int(columns[at]) not in LINK_KINDS_EVALUATED:
			if dropped is not None:
				dropped.append({
					"from": columns[1], "to": columns[2],
					"kind": int(columns[at]), "operands": columns[at + 1:at + 3]
				})
			columns[at:at + 3] = ["0", "0", "0"]
	return columns


# ================
# merge_gate_tables
# ================
def merge_gate_tables(
	native, vsro, codes=GATE_CODES, level_cap=NATIVE_LEVEL_CAP, flatten=(), items=None, taken=frozenset()
):
	"""
	The native tables with the gates in codes added. native and vsro map
	"teleportdata.txt", "teleportbuilding.txt", "teleportlink.txt" and
	"text" (textdataname / textdata_object) to raw bytes. Teleport ids differ
	between the clients, so links are renumbered by code; the new gates take
	their VSRO ids, which the native table does not use. A gate named in
	flatten has its destination world set to the persistent world (the
	temple's source world 60 does not exist in the port). items maps the
	source client's item ids to the variant's for item conditions
	(translate_link); a link whose item does not map is left out and
	listed under omittedLinks. taken is the object ids other rows hold
	(gate_buildings). Returns {name: bytes} for the changed files, and the
	list of gates added.
	"""
	native_gates = by_column(native["teleportdata.txt"], 2, TELEPORT_COLUMNS)
	vsro_gates = by_column(vsro["teleportdata.txt"], 2, TELEPORT_COLUMNS)
	native_ids = {code: row[1] for code, row in native_gates.items()}
	vsro_codes = {row[1]: code for code, row in vsro_gates.items()}
	new_ids = {row[1] for code, row in vsro_gates.items() if code in codes}
	if len(new_ids) != len(codes):
		raise SystemExit(f"VSRO teleportdata lacks some of {codes}")
	if new_ids & {row[1] for row in native_gates.values()}:
		raise SystemExit("a VSRO gate id is already a native teleport id")
	ids = {vsro_id: native_ids.get(code, vsro_id) for vsro_id, code in vsro_codes.items()}

	data_rows = []
	for code in codes:
		row = list(vsro_gates[code])
		if code in flatten:
			row[TELEPORT_WORLD_COLUMN] = temple.SOURCE_WORLD_COLUMN_VALUE
		data_rows.append(row)
	building_ids = {row[3] for row in data_rows}
	buildings = [
		list(row) for row in stage1.table_rows(vsro["teleportbuilding.txt"])
		if len(row) >= BUILDING_COLUMNS and row[1] in building_ids
	]
	moved = gate_buildings(buildings, taken | by_ids(native["teleportbuilding.txt"]))
	for row in data_rows:
		row[3] = moved.get(row[3], row[3])
	links, dropped_conditions, omitted_links = [], [], []
	for row in stage1.table_rows(vsro["teleportlink.txt"]):
		if len(row) != LINK_COLUMNS or row[0] != "1" or not ({row[1], row[2]} & new_ids):
			continue
		other = row[2] if row[1] in new_ids else row[1]
		if other not in new_ids and (other not in vsro_codes or vsro_codes[other] not in native_ids):
			continue
		translated = translate_link(row, ids, level_cap, dropped_conditions, items)
		if translated is None:
			omitted_links.append({
				"from": ids[row[1]], "to": ids[row[2]],
				"items": sorted({row[at + 1] for at in range(6, LINK_COLUMNS, 3) if int(row[at]) in LINK_KINDS_ITEM})
			})
			continue
		links.append(translated)

	text_keys = {row[4] for row in data_rows} | {row[5] for row in buildings}
	native_keys = {row[1] for row in stage1.table_rows(native["text"]) if len(row) > 1}
	texts = {
		row[1]: row for row in stage1.table_rows(vsro["text"]) if len(row) >= 10 and row[1] in text_keys
	}
	text_rows = [native_text_row(texts[key]) for key in sorted(texts) if key not in native_keys]
	return {
		"teleportdata.txt": append_rows(native["teleportdata.txt"], data_rows),
		"teleportbuilding.txt": append_rows(native["teleportbuilding.txt"], buildings),
		"teleportlink.txt": append_rows(native["teleportlink.txt"], links),
		"text": append_rows(native["text"], text_rows)
	}, {
		"gates": list(codes), "links": len(links), "buildings": len(buildings), "texts": len(text_rows),
		# The server hides these structures while SRO_ALEXANDRIA is off (alexandria.go).
		"buildingCodes": sorted(row[2] for row in buildings),
		# Link conditions the native catalogue cannot evaluate yet, reported
		# so the loss is visible in the build output and manifest (#1749).
		**({"droppedConditions": dropped_conditions} if dropped_conditions else {}),
		# Keyed links whose item the variant does not carry (translate_link).
		**({"omittedLinks": omitted_links} if omitted_links else {}),
		**({"worldsFlattened": [code for code in codes if code in flatten]} if set(codes) & set(flatten) else {})
	}


#============================================================================
# Region names, world map and minimap
#============================================================================


#============================================================================
# Trees
#============================================================================


# ================
# gate_level_cap
# ================
def gate_level_cap(environ=None):
	"""
	The level cap the gate conditions are clamped to, from SRO_LEVEL_CAP with
	the server's own values: 90 unless "110"; anything else is refused, as the
	server refuses to start on it.
	"""
	value = (os.environ if environ is None else environ).get(LEVEL_CAP_FLAG, "").strip().lower()
	if value in LEVEL_CAP_NATIVE_VALUES:
		return NATIVE_LEVEL_CAP
	if value in LEVEL_CAP_EXPANSION_VALUES:
		return EXPANSION_LEVEL_CAP
	raise ValueError(f"{LEVEL_CAP_FLAG} must be off, 90 or 110, not {value!r}")
