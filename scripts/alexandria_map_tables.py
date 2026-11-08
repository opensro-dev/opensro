"""
===========================================================================

alexandria_map_tables.py - source-authored Alexandria map table projection

Owns native map rows, overview tile coverage and the shared UTF-16 text
adapters used by the variant's gate and map imports. No files are written.

===========================================================================
"""
import import_vsro_alexandria as stage1

# World map and minimap (worldmap_mapinfo, worldmap_localinfo, regioncode).
MAP_ALEXANDRIA_ID = "12"
MAPINFO_COLUMNS = 19
LOCALINFO_COLUMNS = 20
# worldmap_localinfo ids: the landmark labels on the local map (23501-23510),
# the city's name and icon on the world map, and the two dimensional gates.
MAP_PIN_IDS = frozenset(
	[str(n) for n in range(26000, 26010)] + [str(n) for n in range(26300, 26304)]
	+ ["22009", "99000", "92706", "92713", "11057", "11062"]
)
# The overview's row (worldmap_mapinfo id 0, kind 0) and the columns of its
# region box: left, top, right, bottom.
OVERVIEW_ID, OVERVIEW_KIND = "0", "0"
# worldmap_localinfo page ids from here up name dungeon floors, not map pages.
DUNGEON_PAGE_MIN = 2000
OVERVIEW_BOX = (10, 11, 12, 13)
# Texture (one tile) and draw (whole page) size columns of the overview row.
OVERVIEW_TEXTURE = (4, 5)
OVERVIEW_DRAW = (8, 9)
OVERVIEW_TILE_REGIONS = 4
MAP_TEXTURES = (
	"interface/worldmap/map/map_alex.ddj",
	"interface/worldmap/map/city_alexandria.ddj",
	"interface/worldmap/map/xy_gate.ddj"
)

# ================
# table_text
# ================
def table_text(rows):
	"""A textdata file's text from tab rows (CRLF, no trailing blank row)."""
	return "\r\n".join("\t".join(row) for row in rows) + "\r\n"


# ================
# append_rows
# ================
def append_rows(native_raw, rows):
	"""The native UTF-16 table with rows appended; the native bytes stay first."""
	if not rows:
		return native_raw
	text = stage1.decode_table(native_raw)
	if not text.endswith("\n"):
		text += "\r\n"
	return b"\xff\xfe" + (text.lstrip("﻿") + table_text(rows)).encode("utf-16-le")


# ================
# native_text_row
# ================
def native_text_row(row):
	"""
	A VSRO textdata_object row in the v1.150 textdataname layout. VSRO: service,
	key, Korean, '', zh-TW, zh-CN, de, ja, en, vi; v1.150: service, key,
	Korean, '', description, zh-CN, zh-TW, ja, en, vi, ''.
	"""
	return [row[0], row[1], row[2], "", "", row[5], row[4], row[7], row[8], row[9], ""]


# ================
# merge_map_tables
# ================
def merge_map_tables(native, vsro, regions):
	"""
	The native tables with Alexandria's names and map entries added: the
	region code of each city region (regioncode.txt), its zone names
	(textzonename.txt), the local map (worldmap_mapinfo.txt), the landmark,
	world-map and gate rows of worldmap_localinfo.txt and the SN_ texts they
	name (textdataname.txt), including the city's authored shop/NPC markers.
	Returns {name: bytes} and a summary.
	"""
	region_ids = {(y << 8) | x for x, y in regions}
	code_rows = [
		row for row in stage1.table_rows(vsro["regioncode.txt"])
		if len(row) >= 4 and row[1].isdigit() and int(row[1]) in region_ids and row[2] != "xxx"
	]
	native_regions = {row[1]: row for row in stage1.table_rows(native["regioncode.txt"]) if len(row) >= 4}
	code_rows = [row for row in code_rows if native_regions.get(row[1], [0, 0, "xxx"])[2] == "xxx"]
	replaced = {row[1] for row in code_rows}
	kept = [
		row for row in stage1.table_rows(native["regioncode.txt"]) if len(row) < 2 or row[1] not in replaced
	]

	# A region's zone names: its own id as a key, and its code's _01 name and
	# _02 description (RN_SD_EGYPT01_01, _02).
	zone_ids = {str(region_id) for region_id in region_ids}
	zone_prefixes = tuple(sorted({row[2] + "_" for row in code_rows}))
	native_zone_keys = {row[1] for row in stage1.table_rows(native["textzonename.txt"]) if len(row) > 1}
	zone_rows = [
		native_text_row(row) for row in stage1.table_rows(vsro["textzonename.txt"])
		if len(row) >= 10 and (row[1] in zone_ids or row[1].startswith(zone_prefixes))
		and row[1] not in native_zone_keys
	]

	native_maps = {row[0] for row in stage1.table_rows(native["worldmap_mapinfo.txt"])}
	mapinfo_raw, box = widen_overview(native["worldmap_mapinfo.txt"], vsro["worldmap_mapinfo.txt"])
	map_rows = [
		row[:MAPINFO_COLUMNS] for row in stage1.table_rows(vsro["worldmap_mapinfo.txt"])
		if len(row) >= MAPINFO_COLUMNS and row[0] == MAP_ALEXANDRIA_ID and row[0] not in native_maps
	]

	native_pins = {row[0] for row in stage1.table_rows(native["worldmap_localinfo.txt"])}
	pin_rows = []
	for row in stage1.table_rows(vsro["worldmap_localinfo.txt"]):
		# The VSRO row has a leading service column and one more at the end.
		if len(row) != LOCALINFO_COLUMNS + 2 or row[0] != "1" or row[1] in native_pins:
			continue
		# Later tables also carry NPC lookup-only rows without a glyph or
		# coordinates. Only complete static records enter the v1.150 map table.
		city_label = row[8] == MAP_ALEXANDRIA_ID and row[2] == "1" and row[3].startswith("SN_")
		city_icon = row[8] == MAP_ALEXANDRIA_ID and row[2] == "2" and row[3].lower().endswith(".ddj")
		city_icon = city_icon and all(value.isdigit() and int(value) > 0 for value in row[13:15])
		field_label = row[8] == OVERVIEW_ID and row[2] == "1" and row[3].startswith("SN_")
		field_label = field_label and all(value.isdigit() for value in row[9:11])
		field_label = field_label and ((int(row[10]) << 8) | int(row[9])) in region_ids
		# The overview's own icons (teleports, gates) inside the widened region
		# box. A town or fort page the table does not publish gets no icon;
		# dungeon pages (>= DUNGEON_PAGE_MIN) are never overview pages, so
		# their teleports are drawn and only the click has no target.
		overview_icon = row[8] == OVERVIEW_ID and row[2] == "2" and row[3].lower().endswith(".ddj")
		overview_icon = overview_icon and all(value.isdigit() and int(value) > 0 for value in row[13:15])
		overview_icon = overview_icon and all(value.isdigit() for value in row[7:8] + row[9:11])
		if overview_icon:
			west, north, east, south = box
			overview_icon = west <= int(row[9]) <= east and south <= int(row[10]) <= north
			destination = int(row[7])
			overview_icon = overview_icon and (
				destination == 0 or destination >= DUNGEON_PAGE_MIN
				or str(destination) in native_maps or str(destination) in {mapped[0] for mapped in map_rows}
			)
		if row[1] in MAP_PIN_IDS or city_label or city_icon or field_label or overview_icon:
			pin_rows.append(row[1:1 + LOCALINFO_COLUMNS])
	native_keys = {row[1] for row in stage1.table_rows(native["textdataname.txt"]) if len(row) > 1}
	wanted = {row[2] for row in pin_rows if row[2].startswith("SN_")}
	texts = [
		native_text_row(row) for row in stage1.table_rows(vsro["textdata_object.txt"])
		if len(row) >= 10 and row[1] in wanted and row[1] not in native_keys
	]
	# Some later overview records name tomb labels absent even from their
	# source text catalogue. They are incomplete content, not usable labels.
	resolved_keys = native_keys | {row[1] for row in texts}
	pin_rows = [row for row in pin_rows
		if row[1] != "1" or not row[2].startswith("SN_") or row[2] in resolved_keys]
	merged = {
		"regioncode.txt": append_rows(
			b"\xff\xfe" + table_text(kept).encode("utf-16-le"), code_rows
		) if replaced else append_rows(native["regioncode.txt"], code_rows),
		"textzonename.txt": append_rows(native["textzonename.txt"], zone_rows),
		"worldmap_mapinfo.txt": append_rows(mapinfo_raw, map_rows),
		"worldmap_localinfo.txt": append_rows(native["worldmap_localinfo.txt"], pin_rows),
		"textdataname.txt": append_rows(native["textdataname.txt"], texts)
	}
	return merged, {
		"regionCodes": len(code_rows), "zoneNames": len(zone_rows), "maps": len(map_rows),
		"pins": len(pin_rows), "texts": len(texts), "overview": box
	}


# ================
# overview_box
# ================
def overview_box(raw):
	"""The (left, top, right, bottom) region box of a worldmap_mapinfo overview row, or None."""
	for row in stage1.table_rows(raw):
		if len(row) >= MAPINFO_COLUMNS and row[0] == OVERVIEW_ID and row[1] == OVERVIEW_KIND:
			return tuple(int(row[at]) for at in OVERVIEW_BOX)
	return None


# ================
# widen_overview
# ================
def widen_overview(native_raw, vsro_raw):
	"""
	The native worldmap_mapinfo.txt with the overview row's region box grown to
	the VSRO client's (west to region 46, south to region 70), and that box as
	(left, top, right, bottom). The page's draw size follows the box; the other columns stay. An
	overview the VSRO does not widen leaves the table as it was.
	"""
	native_box, vsro_box = overview_box(native_raw), overview_box(vsro_raw)
	if native_box is None or vsro_box is None:
		raise SystemExit("worldmap_mapinfo.txt has no overview row")
	left, top = min(native_box[0], vsro_box[0]), max(native_box[1], vsro_box[1])
	right, bottom = max(native_box[2], vsro_box[2]), min(native_box[3], vsro_box[3])
	box = (left, top, right, bottom)
	if box == native_box:
		return native_raw, box
	lines = stage1.decode_table(native_raw).lstrip("﻿").split("\r\n")
	for number, line in enumerate(lines):
		row = line.split("\t")
		if len(row) >= MAPINFO_COLUMNS and row[0] == OVERVIEW_ID and row[1] == OVERVIEW_KIND:
			for at, value in zip(OVERVIEW_BOX, box):
				row[at] = str(value)
			# The tile grid is the draw size over the tile (texture) size, in
			# the build as in the client: one 128-pixel tile per four regions.
			columns = (right - left + 1) // OVERVIEW_TILE_REGIONS
			tile_rows = (top - bottom + 1) // OVERVIEW_TILE_REGIONS
			row[OVERVIEW_DRAW[0]] = str(columns * int(row[OVERVIEW_TEXTURE[0]]))
			row[OVERVIEW_DRAW[1]] = str(tile_rows * int(row[OVERVIEW_TEXTURE[1]]))
			lines[number] = "\t".join(row)
			break
	return b"\xff\xfe" + "\r\n".join(lines).encode("utf-16-le"), box


# ================
# overview_tiles
# ================
def overview_tiles(box, regions=None):
	"""Overview tile files for a box, optionally only tiles covering the imported regions."""
	left, top, right, bottom = box
	return [
		f"interface/worldmap/map/map_world_{x}x{y}.ddj"
		for y in range(top, bottom - 1, -OVERVIEW_TILE_REGIONS)
		for x in range(left, right + 1, OVERVIEW_TILE_REGIONS)
		if regions is None or any(x <= rx < x + OVERVIEW_TILE_REGIONS and y - OVERVIEW_TILE_REGIONS < ry <= y
			for rx, ry in regions)
	]


# ================
# seam_tiles
#
# Port-only, not native. The v1.150 overview draws decorative sea and blank
# land around its world; the later client draws Egypt there. A tile that
# shows no v1.150 region carries no v1.150 information, so where it touches
# later art it takes the later art too, or the page shows a patchwork of
# the two styles around Alexandria. The walk spreads only through such
# tiles, so the v1.150 world and the decoration away from the import stay.
# ================
def seam_tiles(box, native_box, regions, native_land, differs):
	"""
	Overview tiles inside native_box that join the imported art to the page:
	reachable (edge to edge) from a later-art tile through tiles with no
	native_land(x, y) region whose later art differs(path) from the native.
	"""
	left, top, right, bottom = box
	native_tiles = set(overview_tiles(native_box))
	later = set(overview_tiles(box, regions)) | (set(overview_tiles(box)) - native_tiles)

	def tile(x, y):
		return f"interface/worldmap/map/map_world_{x}x{y}.ddj"

	def open_tile(x, y):
		return (tile(x, y) in native_tiles and tile(x, y) not in later
			and not any(native_land(rx, ry) for rx in range(x, x + OVERVIEW_TILE_REGIONS)
				for ry in range(y - OVERVIEW_TILE_REGIONS + 1, y + 1))
			and differs(tile(x, y)))

	step = OVERVIEW_TILE_REGIONS
	cells = [(x, y) for y in range(top, bottom - 1, -step) for x in range(left, right + 1, step)]
	frontier = [(x, y) for x, y in cells if tile(x, y) in later]
	seams, seen = [], set(frontier)
	while frontier:
		x, y = frontier.pop()
		for nx, ny in ((x - step, y), (x + step, y), (x, y - step), (x, y + step)):
			if (nx, ny) in seen or not (left <= nx <= right and bottom <= ny <= top):
				continue
			seen.add((nx, ny))
			if open_tile(nx, ny):
				seams.append(tile(nx, ny))
				frontier.append((nx, ny))
	return sorted(seams)


# ================
# map_media_paths
# ================
def map_media_paths(regions, box=None, native_box=None):
	"""
	The Media.pk2 files the map needs: the local-map art, the minimap tiles and
	the overview tiles the widened box adds or whose imported land changes.
	"""
	tiles = [f"minimap/{x}x{y}.ddj" for x, y in regions]
	overview = overview_tiles(box) if box else []
	known = set(overview_tiles(native_box)) if native_box else set()
	changed = set(overview_tiles(box, regions)) if box else set()
	return list(MAP_TEXTURES) + tiles + [path for path in overview if path not in known or path in changed]


