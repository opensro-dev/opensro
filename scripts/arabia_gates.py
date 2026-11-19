"""
===========================================================================

arabia_gates.py - the Arabia gate chain, projected for v1.150

PORT-ONLY, NOT NATIVE. Used by arabia_import.py (SRO_ARABIA).

The source authors Baghdad's own portal network and four Arabia gates
around it:

	city     the four Portals (GATE_AR_S/W/M/N, buildings
	         STORE_AR_GATE_S/W/M/N), each linked to the other three.
	         The map places their model (bagh_city_portal.bsr) at the
	         S, W and N buildings; M's building row names it itself;
	dungeon  the dungeon's start and end gates, both with their
	         buildings, and GATE_BAGH_TO_ARABIA_DUNGEON, the town-side
	         arrival the two lead back to (its building, 40489, is in
	         no teleportbuilding table: the later client never shipped
	         it), with the source's links town -> start, start -> town
	         and end -> town;
	town     GATE_TO_LAST_FIELD_IN_BAGHDAD, the town's own arrival,
	         whose departure stands in a later area this port does not
	         import (its building, 43990, is likewise absent
	         everywhere).

No source gate leads into Baghdad from the shipped world, as none led
into the Sky Temple fields. Authored at import, and recorded here:

	entry    Jangan's own gate (GATE_CH) lists Baghdad, the town
	         arrival, among its destinations, and every city Portal
	         lists Jangan back. No second gate stands in Jangan: a
	         building beside STORE_CH_GATE overlapped it, so a click on
	         the gate opened a Baghdad-only menu and hid every native
	         destination;
	arrivals the two town arrivals keep their source rows with no
	         building (0, as the native arrival-only rows such as
	         GATE_JINSI_IN): the buildings the source names are
	         absent, and a building authored in their place had no
	         model, so it stood as an invisible click target;
	dungeon  every city Portal lists the dungeon's start gate on the
	         source's town -> start link, which the town arrival can no
	         longer offer; its level condition (121) and the Jangan
	         pair's (the same link's) clamp through the merge like
	         every imported gate's.

Every row names the world it names in the source: the dungeon pair
names world 112 (GROUP_ARABIA_DUNGEON, registered port-only while
SRO_ARABIA is on), the rest world 1. The Jangan gate's row rides in the
prepared tables only so the merge renumbers its links by code; it is
not one of GATE_CODES and the merge never appends it. The later link rows carry two
trailing columns the v1.150 table has no place for; they sit outside
every condition triple the port evaluates, so rows project to the
native 21 columns and the merge's own condition rules
(build_alexandria_variant.translate_link) clamp or drop what it
cannot evaluate.

Identities are the source's own unless one already stands in the
variant's tables, in which case all of them move above the used range
together, so a rebuild authors the same chain the marker recorded.

===========================================================================
"""
import cap110_monsters as monsters

# The gate chain, by teleportdata code: the source's rows.
TOWN_GATE = "GATE_TO_LAST_FIELD_IN_BAGHDAD"
DUNGEON_TOWN_GATE = "GATE_BAGH_TO_ARABIA_DUNGEON"
DUNGEON_START_GATE = "GATE_ARABIA_DUNGEON_START"
DUNGEON_END_GATE = "GATE_ARABIA_DUNGEON_END"
CITY_GATES = ("GATE_AR_S", "GATE_AR_W", "GATE_AR_M", "GATE_AR_N")
# The arrivals whose source buildings no client ships.
ARRIVAL_GATES = (DUNGEON_TOWN_GATE, TOWN_GATE)
SOURCE_GATES = (DUNGEON_START_GATE, DUNGEON_END_GATE, DUNGEON_TOWN_GATE, TOWN_GATE) + CITY_GATES
GATE_CODES = SOURCE_GATES
# The building column of an arrival-only gate (native GATE_JINSI_IN).
NO_BUILDING = "0"
# The native gate that lists Baghdad (Jangan's).
ANCHOR_GATE = "GATE_CH"
TELEPORT_COLUMNS = 13
BUILDING_COLUMNS = 58
LINK_COLUMNS = 21


# ================
# renumbered
#
# {source id: variant id}: the source's own identities, or all of them
# moved above the used range together when one already stands.
# ================
def renumbered(source_ids, current_ids):
	if not set(source_ids) & set(current_ids):
		return {identity: identity for identity in source_ids}
	base = max([int(i) for i in current_ids if i.isdigit()] + [0]) + 1
	return {identity: str(base + at) for at, identity in enumerate(source_ids)}


# ================
# prepare_gates
#
# The gate tables this import feeds merge_gate_tables, and its codes.
# later holds the v1.696 teleportdata/teleportbuilding/teleportlink row
# lists and its textdata_object raw bytes; native_data is the v1.150
# teleportdata the Jangan gate is read from; current_ids and current_building_ids are the identities the variant's
# tables already use (the source identities stand unless one collides).
# Returns (tables, codes) with tables mapping the three table names and
# "text" to encoded bytes, ready for variant.merge_gate_tables.
# ================
def prepare_gates(later, native_data, current_ids, current_building_ids):
	data_by_code = {row[2]: row for row in later["teleportdata.txt"] if len(row) >= TELEPORT_COLUMNS}
	missing = [code for code in SOURCE_GATES if code not in data_by_code]
	if missing:
		raise ValueError(f"v1.696 teleportdata lacks the Arabia gates: {missing}")
	buildings_by_id = {row[1]: row for row in later["teleportbuilding.txt"] if len(row) >= BUILDING_COLUMNS}

	anchor = next((list(row) for row in native_data if len(row) >= TELEPORT_COLUMNS and row[2] == ANCHOR_GATE), None)
	if anchor is None:
		raise ValueError(f"native teleportdata has no {ANCHOR_GATE} to list Baghdad on")

	source_data = [data_by_code[code] for code in SOURCE_GATES]
	built = [row for row in source_data if row[2] not in ARRIVAL_GATES]
	unshipped = [row[2] for row in built if row[3] not in buildings_by_id]
	if unshipped:
		raise ValueError(f"v1.696 teleportbuilding lacks the buildings of the Arabia gates: {unshipped}")

	teleport_ids = renumbered([row[1] for row in source_data], current_ids)
	building_ids = renumbered([row[3] for row in built], current_building_ids)

	data_rows, building_rows = [], []
	for row in source_data:
		row = list(row)
		source_building = row[3]
		row[1] = teleport_ids[row[1]]
		if row[2] in ARRIVAL_GATES:
			row[3] = NO_BUILDING
		else:
			row[3] = building_ids[source_building]
			building = list(buildings_by_id[source_building])
			building[1] = row[3]
			building_rows.append(building)
		data_rows.append(row)

	# Links: the source's own among the imported gates, but none out of
	# an arrival (no building stands there to click), then the authored
	# ones on the source's town -> start link: Jangan <-> the town
	# arrival, and every city Portal to Jangan and the dungeon's start.
	# The source's town-arrival links name a gate of an area this port
	# does not import, so the merge would drop them anyway; they are
	# left out here instead.
	kept_ids = set(teleport_ids)
	arrival_ids = {data_by_code[code][1] for code in ARRIVAL_GATES}
	link_rows = []
	for row in later["teleportlink.txt"]:
		if len(row) < LINK_COLUMNS or row[1] not in kept_ids or row[2] not in kept_ids or row[1] in arrival_ids:
			continue
		row = list(row[:LINK_COLUMNS])
		row[1], row[2] = teleport_ids[row[1]], teleport_ids[row[2]]
		link_rows.append(row)
	entry = next((list(row[:LINK_COLUMNS]) for row in later["teleportlink.txt"]
		if len(row) >= LINK_COLUMNS and row[1] == data_by_code[DUNGEON_TOWN_GATE][1]
		and row[2] == data_by_code[DUNGEON_START_GATE][1]), None)
	if entry is None:
		raise ValueError("v1.696 teleportlink has no link from Baghdad into the Arabia dungeon")

	def gate_id(code):
		return teleport_ids[data_by_code[code][1]]

	authored = [(anchor[1], gate_id(TOWN_GATE))]
	for code in CITY_GATES:
		authored += [(gate_id(code), anchor[1]), (gate_id(code), gate_id(DUNGEON_START_GATE))]
	pairs = {(row[1], row[2]) for row in link_rows}
	for pair in authored:
		if pair in pairs:
			continue
		row = list(entry)
		row[1], row[2] = pair
		link_rows.append(row)
		pairs.add(pair)

	encoded = {
		# The anchor row maps the pair's native end by code (merge_gate_tables).
		"teleportdata.txt": monsters.encode_rows(data_rows + [anchor]),
		"teleportbuilding.txt": monsters.encode_rows(building_rows),
		"teleportlink.txt": monsters.encode_rows(link_rows),
		"text": later["text"],
	}
	return encoded, GATE_CODES
