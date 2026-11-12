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

	entry    a hub gate cloned from the town arrival stands beside
	         Jangan's STORE_CH_GATE (its building authored from the
	         dungeon start building's shape) and links to the town
	         arrival, which keeps its source row and gains its
	         authored building in Baghdad;
	dungeon  the town portal gains its authored building beside its
	         source arrival in Baghdad; the source's three links
	         (town -> start, start -> town, end -> town) stand as
	         authored, their level condition (121) clamping through
	         the merge like every imported gate's.

Every row names the world it names in the source: the dungeon pair
names world 112 (GROUP_ARABIA_DUNGEON, registered port-only while
SRO_ARABIA is on), the rest world 1. The later link rows carry two
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

# The town and fields are outdoor regions of the default world; the
# population (arabia_import.build_population) names the same worlds.
FIELD_WORLD = 1
DUNGEON_WORLD = 112

# The gate chain, by teleportdata code. The first four rows are the
# source's; GATE_TO_ARABIA is the authored hub (see the header).
HUB_GATE = "GATE_TO_ARABIA"
TOWN_GATE = "GATE_TO_LAST_FIELD_IN_BAGHDAD"
DUNGEON_TOWN_GATE = "GATE_BAGH_TO_ARABIA_DUNGEON"
DUNGEON_START_GATE = "GATE_ARABIA_DUNGEON_START"
DUNGEON_END_GATE = "GATE_ARABIA_DUNGEON_END"
SOURCE_GATES = (DUNGEON_START_GATE, DUNGEON_END_GATE, DUNGEON_TOWN_GATE, TOWN_GATE)
GATE_CODES = (HUB_GATE,) + SOURCE_GATES
# The buildings the source authors for the two dungeon gates; the
# other three buildings are authored below from the start building's
# shape (the source ships none of them).
HUB_BUILDING_CODENAME = "STORE_ARABIA_GATE"
HUB_BUILDING_NAME_KEY = "SN_STORE_ARABIA_GATE"
TOWN_BUILDING_CODENAME = "STORE_BAGHDAD_GATE"
TOWN_BUILDING_NAME_KEY = "SN_STORE_BAGHDAD_GATE"
DUNGEON_TOWN_BUILDING_CODENAME = "STORE_ARABIA_DUNGEON_TOWN_GATE"
DUNGEON_TOWN_BUILDING_NAME_KEY = "SN_STORE_ARABIA_DUNGEON_TOWN_GATE"
# The authored town buildings stand this far east of their arrivals;
# the hub building stands beside the anchor gate, as the Sky Temple
# entry does.
ENTRY_OFFSET_X = 10
AUTHORED_OFFSET_X = 6
ANCHOR_CODENAME = "STORE_CH_GATE"
ANCHOR_ZONE_GATE = "GATE_CH"
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
# lists and its textdata_object raw bytes; native_buildings and
# native_data are the v1.150 rows the authored hub anchors on;
# current_ids and current_building_ids are the identities the variant's
# tables already use (the source identities stand unless one collides).
# Returns (tables, codes) with tables mapping the three table names and
# "text" to encoded bytes, ready for variant.merge_gate_tables.
# ================
def prepare_gates(later, native_buildings, native_data, current_ids, current_building_ids):
	data_by_code = {row[2]: row for row in later["teleportdata.txt"] if len(row) >= TELEPORT_COLUMNS}
	missing = [code for code in SOURCE_GATES if code not in data_by_code]
	if missing:
		raise ValueError(f"v1.696 teleportdata lacks the Arabia gates: {missing}")
	buildings_by_id = {row[1]: row for row in later["teleportbuilding.txt"] if len(row) >= BUILDING_COLUMNS}

	anchor = next((row for row in native_buildings if row[2] == ANCHOR_CODENAME), None)
	if anchor is None:
		anchor = next((row for row in native_buildings if len(row) > 45 and row[41] == "25000"), None)
	if anchor is None:
		raise ValueError(f"native teleportbuilding has no {ANCHOR_CODENAME} to anchor the entry on")
	hub_pos = (str(int(anchor[43]) + ENTRY_OFFSET_X), anchor[44], anchor[45])
	zone_key = next((row[4] for row in native_data if len(row) > 4 and row[2] == ANCHOR_ZONE_GATE), None)

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
		hub_teleport_id = str(base + len(source_data))
	else:
		teleport_ids = {row[1]: row[1] for row in source_data}
		hub_teleport_id = str(max([int(i) for i in current_ids if i.isdigit()] + [int(r[1]) for r in source_data]) + 1)
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
	for codename in (DUNGEON_TOWN_BUILDING_CODENAME, TOWN_BUILDING_CODENAME, HUB_BUILDING_CODENAME):
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

	# The authored hub: the town arrival's row cloned, arriving beside
	# the anchor gate where its own building stands.
	town_row = next(row for row in data_rows if row[2] == TOWN_GATE)
	hub_row = list(town_row)
	hub_row[1], hub_row[2] = hub_teleport_id, HUB_GATE
	hub_row[3] = authored_building_ids[HUB_BUILDING_CODENAME]
	if zone_key is not None:
		hub_row[4] = zone_key
	hub_row[5], hub_row[6], hub_row[7], hub_row[8] = anchor[41], *hub_pos
	hub_row[12] = str(FIELD_WORLD)
	data_rows.append(hub_row)
	building_rows.append(authored_building(shape, hub_row[3], HUB_BUILDING_CODENAME,
		HUB_BUILDING_NAME_KEY, anchor[41], *hub_pos))

	# Links: the source's own among the imported gates, plus the
	# authored hub pair on the same shape (the source's town-arrival
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
	for pair in ((hub_teleport_id, town_id), (town_id, hub_teleport_id)):
		if pair in pairs:
			continue
		row = list(template)
		row[1], row[2] = pair
		link_rows.append(row)
		pairs.add(pair)

	encoded = {
		"teleportdata.txt": monsters.encode_rows(data_rows),
		"teleportbuilding.txt": monsters.encode_rows(building_rows),
		"teleportlink.txt": monsters.encode_rows(link_rows),
		"text": later["text"],
	}
	return encoded, GATE_CODES
