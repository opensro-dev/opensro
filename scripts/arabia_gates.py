"""
===========================================================================

arabia_gates.py - the Arabia gate chain, projected for v1.150

PORT-ONLY, NOT NATIVE. Used by arabia_import.py (SRO_ARABIA).

The source authors exactly four Arabia gates: the Baghdad dungeon's
start and end, the town-side dungeon portal whose building the later
client itself never shipped (40489, in no teleportbuilding table),
and GATE_TO_LAST_FIELD_IN_BAGHDAD, the town's own arrival whose
departure stands in a later area this port does not import (its
building is likewise absent everywhere). No source gate leads into
Baghdad from the shipped world, as none led into the Sky Temple
fields. Authored at import, and recorded here:

	entry    Jangan's own gate (GATE_CH) lists Baghdad among its
	         destinations, and the town arrival, which keeps its
	         source row and gains its authored building in Baghdad,
	         leads back to it. No second gate stands in Jangan: a
	         building beside STORE_CH_GATE overlapped it, so a click
	         on the gate opened a Baghdad-only menu and hid every
	         native destination;
	dungeon  the town portal gains its authored building beside its
	         source arrival in Baghdad; the source's three links
	         (town -> start, start -> town, end -> town) stand as
	         authored, their level condition (121) clamping through
	         the merge like every imported gate's.

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

# The gate chain, by teleportdata code: the source's four rows.
TOWN_GATE = "GATE_TO_LAST_FIELD_IN_BAGHDAD"
DUNGEON_TOWN_GATE = "GATE_BAGH_TO_ARABIA_DUNGEON"
DUNGEON_START_GATE = "GATE_ARABIA_DUNGEON_START"
DUNGEON_END_GATE = "GATE_ARABIA_DUNGEON_END"
SOURCE_GATES = (DUNGEON_START_GATE, DUNGEON_END_GATE, DUNGEON_TOWN_GATE, TOWN_GATE)
GATE_CODES = SOURCE_GATES
# The buildings the source authors for the two dungeon gates; the
# other two buildings are authored below from the start building's
# shape (the source ships none of them).
TOWN_BUILDING_CODENAME = "STORE_BAGHDAD_GATE"
TOWN_BUILDING_NAME_KEY = "SN_STORE_BAGHDAD_GATE"
DUNGEON_TOWN_BUILDING_CODENAME = "STORE_ARABIA_DUNGEON_TOWN_GATE"
DUNGEON_TOWN_BUILDING_NAME_KEY = "SN_STORE_ARABIA_DUNGEON_TOWN_GATE"
# The authored town buildings stand this far east of their arrivals.
AUTHORED_OFFSET_X = 6
# The native gate that lists Baghdad (Jangan's).
ANCHOR_GATE = "GATE_CH"
TELEPORT_COLUMNS = 13
BUILDING_COLUMNS = 58
LINK_COLUMNS = 21


# ================
# format_coord
# ================
def format_coord(value):
	"""A teleport table coordinate: integers stay integers."""
	number = float(value)
	if number == int(number):
		return str(int(number))
	return repr(number)


# ================
# authored_building
#
# One authored building row from the shape's: the shape's own row with
# its identity, codename, name key and standing place replaced.
# ================
def authored_building(shape, identity, codename, name_key, region, x, y, z):
	row = list(shape)
	row[1], row[2] = identity, codename
	if len(row) > 5:
		row[5] = name_key
	row[41] = str(region)
	row[43], row[44], row[45] = format_coord(x), format_coord(y), format_coord(z)
	return row


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
	source_building_ids = [row[3] for row in source_data]
	shipped = [bid for bid in source_building_ids if bid in buildings_by_id]
	if len(shipped) != 2:
		raise ValueError(f"v1.696 teleportbuilding ships {len(shipped)} Arabia gate buildings, expected the two dungeon gates")
	shape = buildings_by_id[shipped[0]]

	renumber = any(row[1] in current_ids for row in source_data)
	if renumber:
		base = max([int(i) for i in current_ids if i.isdigit()] + [0]) + 1
		teleport_ids = {row[1]: str(base + at) for at, row in enumerate(source_data)}
	else:
		teleport_ids = {row[1]: row[1] for row in source_data}
	building_collision = any(bid in current_building_ids for bid in source_building_ids)
	if building_collision:
		base = max([int(i) for i in current_building_ids if i.isdigit()] + [0]) + 1
		building_ids = {bid: str(base + at) for at, bid in enumerate(source_building_ids)}
		next_building = base + len(source_building_ids)
	else:
		building_ids = {bid: bid for bid in source_building_ids}
		next_building = max([int(i) for i in current_building_ids if i.isdigit()]
			+ [int(b) for b in source_building_ids]) + 1
	authored_building_ids = {}
	for codename in (DUNGEON_TOWN_BUILDING_CODENAME, TOWN_BUILDING_CODENAME):
		authored_building_ids[codename] = str(next_building)
		next_building += 1

	data_rows, building_rows = [], []
	for row in source_data:
		row = list(row)
		source_building = row[3]
		row[1] = teleport_ids[row[1]]
		if row[2] in (DUNGEON_START_GATE, DUNGEON_END_GATE):
			row[3] = building_ids[source_building]
			building = list(buildings_by_id[source_building])
			building[1] = building_ids[source_building]
			building_rows.append(building)
		elif row[2] == DUNGEON_TOWN_GATE:
			row[3] = authored_building_ids[DUNGEON_TOWN_BUILDING_CODENAME]
			building_rows.append(authored_building(shape, row[3], DUNGEON_TOWN_BUILDING_CODENAME,
				DUNGEON_TOWN_BUILDING_NAME_KEY, row[5],
				float(row[6]) + AUTHORED_OFFSET_X, row[7], row[8]))
		elif row[2] == TOWN_GATE:
			row[3] = authored_building_ids[TOWN_BUILDING_CODENAME]
			building_rows.append(authored_building(shape, row[3], TOWN_BUILDING_CODENAME,
				TOWN_BUILDING_NAME_KEY, row[5],
				float(row[6]) + AUTHORED_OFFSET_X, row[7], row[8]))
		data_rows.append(row)

	# Links: the source's own among the imported gates, plus the
	# Jangan gate pair on the same shape (the source's town-arrival
	# links name a gate of an area this port does not import, so the
	# merge would drop them anyway; they are left out here instead).
	kept_ids = set(teleport_ids)
	template = next((list(row[:LINK_COLUMNS]) for row in later["teleportlink.txt"]
		if len(row) >= LINK_COLUMNS and row[1] in kept_ids and row[2] in kept_ids), None)
	link_rows = []
	for row in later["teleportlink.txt"]:
		if len(row) < LINK_COLUMNS or row[1] not in kept_ids or row[2] not in kept_ids:
			continue
		row = list(row[:LINK_COLUMNS])
		row[1], row[2] = teleport_ids[row[1]], teleport_ids[row[2]]
		link_rows.append(row)
	if not link_rows:
		raise ValueError("v1.696 teleportlink has no links among the Arabia gates")
	if template is None:
		template = link_rows[0]
	town_id = teleport_ids[data_by_code[TOWN_GATE][1]]
	pairs = {(row[1], row[2]) for row in link_rows}
	for pair in ((anchor[1], town_id), (town_id, anchor[1])):
		if pair in pairs:
			continue
		row = list(template)
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
