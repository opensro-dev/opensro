"""
===========================================================================

skytemple_gates.py - the Sky Temple gate chain, projected for v1.150

PORT-ONLY, NOT NATIVE. Used by skytemple_import.py (SRO_SKYTEMPLE).

The source chain stands in a Jupiter field this port does not import,
and its C links name gates whose buildings the later client itself
never shipped (GATE_TO_LAST_FIELD_IN_BAGHDAD / _IN_HOTAN name buildings
that exist in no teleportbuilding table). Authored at import, and
recorded here: the source entry building (43802) is re-homed beside
Jangan's STORE_CH_GATE, the entry offers A and B on the source links
and C and D on authored links of the same shape, C's return is authored
to the entry, and D gets an authored return building cloned from C's.
Every row names world 1: the later per-field world ids (132-134) are
not worlds of this port, whose portal catalogue refuses unknown
worlds. The later link rows carry two trailing columns the v1.150
table has no place for; they sit outside every condition triple the
port evaluates, so rows project to the native 21 columns and the
merge's own condition rules (build_alexandria_variant.translate_link)
clamp or drop what it cannot evaluate.

Identities are the source's own unless one already stands in the
variant's tables, in which case all of them move above the used range
together, so a rebuild authors the same chain the marker recorded.

===========================================================================
"""
import cap110_monsters as monsters

# The fields are outdoor regions of the default world; the population
# (skytemple_import.build_population) names the same world in its rows.
FIELD_WORLD = 1

# The gate chain, by teleportdata code. The first four rows are the
# source's; GATE_OTHER_SKYTEMPLE_D is authored (the source authors no
# gate into D and no building standing in it).
ENTRY_GATE = "GATE_TO_OTHER_SKYTEMPLE"
FIELD_GATES = ("GATE_OTHER_SKYTEMPLE_A", "GATE_OTHER_SKYTEMPLE_B", "GATE_OTHER_SKYTEMPLE_C")
AUTHORED_GATE = "GATE_OTHER_SKYTEMPLE_D"
GATE_CODES = (ENTRY_GATE,) + FIELD_GATES + (AUTHORED_GATE,)
ENTRY_BUILDING_CODENAME = "STORE_TK_TO_OTHER_TEMLPE_GATE"
AUTHORED_BUILDING_CODENAME = "STORE_OTHER_SKYTEMPLE_D_TO_TOWN"
AUTHORED_BUILDING_NAME_KEY = "SN_STORE_OTHER_SKYTEMPLE_D_TO_TOWN"
AUTHORED_ZONE_KEY = "RN_OTHER_SKYTEMPLE_D_01"
# The authored entry stands this far east of the anchor gate building.
ENTRY_OFFSET_X = 10
AUTHORED_OFFSET_X = 6
# Where the authored D building stands: beside the authored arrival.
ANCHOR_CODENAME = "STORE_CH_GATE"
ANCHOR_ZONE_GATE = "GATE_CH"
TELEPORT_COLUMNS = 13
BUILDING_COLUMNS = 58
LINK_COLUMNS = 21


# format_coord
# ================
def format_coord(value):
	"""A teleport table coordinate: integers stay integers."""
	number = float(value)
	if number == int(number):
		return str(int(number))
	return repr(number)


# ================
# prepare_gates
#
# The gate tables this import feeds merge_gate_tables, and its codes.
# later holds the v1.696 teleportdata/teleportbuilding/teleportlink row
# lists and its textdata_object raw bytes; native_buildings and
# native_data are the v1.150 rows the authored entry anchors on;
# current_ids and current_building_ids are the identities the variant's
# tables already use (the source identities stand unless one collides);
# d_arrival is the authored D arrival, (region, x, y, z) as strings.
# Returns (tables, codes) with tables mapping the three table names and
# "text" to encoded bytes, ready for variant.merge_gate_tables.
# ================
def prepare_gates(later, native_buildings, native_data, current_ids, current_building_ids, d_arrival):
	data_by_code = {row[2]: row for row in later["teleportdata.txt"] if len(row) >= TELEPORT_COLUMNS}
	missing = [code for code in (ENTRY_GATE,) + FIELD_GATES if code not in data_by_code]
	if missing:
		raise ValueError(f"v1.696 teleportdata lacks the sky temple gates: {missing}")
	buildings_by_id = {row[1]: row for row in later["teleportbuilding.txt"] if len(row) >= BUILDING_COLUMNS}

	anchor = next((row for row in native_buildings if row[2] == ANCHOR_CODENAME), None)
	if anchor is None:
		anchor = next((row for row in native_buildings if len(row) > 45 and row[41] == "25000"), None)
	if anchor is None:
		raise ValueError(f"native teleportbuilding has no {ANCHOR_CODENAME} to anchor the entry on")
	entry_pos = (str(int(anchor[43]) + ENTRY_OFFSET_X), anchor[44], anchor[45])
	zone_key = next((row[4] for row in native_data if len(row) > 4 and row[2] == ANCHOR_ZONE_GATE), None)

	source_data = [data_by_code[code] for code in (ENTRY_GATE,) + FIELD_GATES]
	source_building_ids = [row[3] for row in source_data]
	absent = [bid for bid in source_building_ids if bid not in buildings_by_id]
	if absent:
		raise ValueError(f"v1.696 teleportbuilding lacks the gate buildings: {absent}")

	renumber = any(row[1] in current_ids for row in source_data)
	if renumber:
		base = max([int(i) for i in current_ids if i.isdigit()] + [0]) + 1
		teleport_ids = {row[1]: str(base + at) for at, row in enumerate(source_data)}
		d_teleport_id = str(base + len(source_data))
	else:
		teleport_ids = {row[1]: row[1] for row in source_data}
		d_teleport_id = str(max([int(i) for i in current_ids if i.isdigit()] + [int(r[1]) for r in source_data]) + 1)
	if any(bid in current_building_ids for bid in source_building_ids):
		base = max([int(i) for i in current_building_ids if i.isdigit()] + [0]) + 1
		building_ids = {bid: str(base + at) for at, bid in enumerate(source_building_ids)}
		d_building_id = str(base + len(source_building_ids))
	else:
		building_ids = {bid: bid for bid in source_building_ids}
		d_building_id = str(max([int(i) for i in current_building_ids if i.isdigit()] + [int(b) for b in source_building_ids]) + 1)

	data_rows, building_rows = [], []
	for row in source_data:
		row = list(row)
		source_building = row[3]
		row[1] = teleport_ids[row[1]]
		row[3] = building_ids[source_building]
		# The destination world column names world 1, like every outdoor
		# gate of this port (see the module header).
		row[12] = str(FIELD_WORLD)
		if row[2] == ENTRY_GATE:
			row[5], row[6], row[7], row[8] = anchor[41], *entry_pos
			if zone_key is not None:
				row[4] = zone_key
		data_rows.append(row)
		building = list(buildings_by_id[source_building])
		building[1] = building_ids[source_building]
		if building[2] == ENTRY_BUILDING_CODENAME:
			building[41], building[43], building[44], building[45] = anchor[41], *entry_pos
		building_rows.append(building)

	# Authored D: the arrival row cloned from C's (its shape is the
	# source's own), the return building cloned from C's building.
	d_row = list(data_by_code[FIELD_GATES[-1]])
	d_row[1], d_row[2], d_row[3] = d_teleport_id, AUTHORED_GATE, d_building_id
	d_row[4] = AUTHORED_ZONE_KEY
	d_row[5], d_row[6], d_row[7], d_row[8] = d_arrival
	d_row[12] = str(FIELD_WORLD)
	data_rows.append(d_row)
	d_building = list(building_rows[-1])
	d_building[1], d_building[2] = d_building_id, AUTHORED_BUILDING_CODENAME
	if len(d_building) > 5:
		d_building[5] = AUTHORED_BUILDING_NAME_KEY
	d_building[41] = d_arrival[0]
	d_building[43] = format_coord(float(d_arrival[1]) + AUTHORED_OFFSET_X)
	d_building[44], d_building[45] = d_arrival[2], d_arrival[3]
	building_rows.append(d_building)

	# Links: the source's own pairs among the imported gates, plus the
	# authored C and D pairs on the same shape. The source's C links name
	# gates whose buildings were never shipped, so the merge would drop
	# them anyway; they are left out here instead.
	entry_id = teleport_ids[data_by_code[ENTRY_GATE][1]]
	source_ids = set(teleport_ids)
	# The later client's link rows carry two trailing columns the v1.150
	# table has no place for; they sit outside every condition triple the
	# port evaluates, so the rows project to the native 21 columns.
	template = next((list(row[:LINK_COLUMNS]) for row in later["teleportlink.txt"]
		if len(row) >= LINK_COLUMNS and row[1] in source_ids and row[2] in source_ids), None)
	link_rows = []
	for row in later["teleportlink.txt"]:
		if len(row) < LINK_COLUMNS or row[1] not in source_ids or row[2] not in source_ids:
			continue
		row = list(row[:LINK_COLUMNS])
		row[1], row[2] = teleport_ids[row[1]], teleport_ids[row[2]]
		link_rows.append(row)
	pairs = {(row[1], row[2]) for row in link_rows}
	for code in (FIELD_GATES[-1], AUTHORED_GATE):
		target = next(row[1] for row in data_rows if row[2] == code)
		for pair in ((entry_id, target), (target, entry_id)):
			if pair in pairs:
				continue
			if template is None:
				raise ValueError("v1.696 teleportlink has no sky temple link to clone")
			row = list(template)
			row[1], row[2] = pair
			link_rows.append(row)
			pairs.add(pair)
	if not link_rows:
		raise ValueError("v1.696 teleportlink has no links among the sky temple gates")

	encoded = {
		"teleportdata.txt": monsters.encode_rows(data_rows),
		"teleportbuilding.txt": monsters.encode_rows(building_rows),
		"teleportlink.txt": monsters.encode_rows(link_rows),
		"text": later["text"],
	}
	return encoded, GATE_CODES


# ================
# d_arrival
#
# The authored D arrival: the field's placement region holding the most
# placements (the lowest region id wins a tie), at its first placement
# in sorted order. Deterministic, so a rebuild authors the same gate.
# ================
def d_arrival(found, fields):
	d_placements = sorted(p for p in found if fields[p[1]] == "D")
	if not d_placements:
		raise ValueError("no placements in field D to anchor its gate on")
	counts = {}
	for _code, region, _x, _y, _z in d_placements:
		counts[region] = counts.get(region, 0) + 1
	region = min(counts, key=lambda r: (-counts[r], r))
	_code, _region, x, y, z = next(p for p in d_placements if p[1] == region)
	return (str(region), format_coord(x), format_coord(y), format_coord(z))
