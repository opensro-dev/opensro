"""
===========================================================================

arabia_map_tiles.py - Arabia's M map and minimap art from the v1.696 client

arabia_import.py copies Arabia's terrain, navmesh, NPCs and tables; this
module owns the map art those regions need: one minimap tile per outdoor
region, the M map overview tiles that cover them, and the overview's
names and icons on them (worldmap_localinfo rows and their texts). A tile the variant
lacks is owned by the Arabia marker and reaped by the off path. A tile the
variant already holds is native or Alexandria art: its bytes are kept under
ORIGINALS so turning SRO_ARABIA off restores it instead of deleting it.

Port-only, not native: the art is v1.696 client data, under SRO_ARABIA.

===========================================================================
"""
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import build_alexandria_variant as variant  # noqa: E402
import sro_pk2  # noqa: E402
from alexandria_map_tables import LOCALINFO_COLUMNS, MAPINFO_COLUMNS, append_rows, overview_tiles  # noqa: E402
from import_vsro_alexandria import TEXTDATA, decode_table, table_rows  # noqa: E402
from prepare_client_resources import safe_target, write_atomically  # noqa: E402

# The native map art a tile import replaced, kept so the off path puts it
# back: an overview tile is native or Alexandria art, never Arabia's own.
ORIGINALS = Path(".opensro-arabia-originals")
# Regions at y >= 128 are dungeon ids, which have no overview or minimap art.
DUNGEON_REGION_Y = 128


# ================
# map_tile_paths
#
# The Media.pk2 map art Arabia's outdoor regions need: one minimap tile
# per region (minimap/<x>x<y>.ddj, region id = y << 8 | x) and the
# overview tiles of the M map's region box that cover a region.
# worldmap_mapinfo needs no row: the overview page draws every tile
# in the box, so without these files the M map showed the older art over
# Arabia, and the minimap was blank there (owner report, 2026-11-15).
# ================
def map_tile_paths(regions, box):
	outdoor = sorted((region & 0xff, region >> 8) for region in regions if (region >> 8) < DUNGEON_REGION_Y)
	paths = [f"minimap/{x}x{y}.ddj" for x, y in outdoor]
	if box is not None:
		paths += overview_tiles(box, outdoor)
	return paths


# ================
# import_map_tiles
#
# Copies the v1.696 tiles. A file the variant lacks is owned (in the
# marker's files, reaped by the off path). A file it already holds with
# other bytes is native or Alexandria art: its first bytes are kept under
# ORIGINALS and the path is recorded as replaced, so the off path restores
# it rather than deleting a native tile. Returns (owned, replaced).
# ================
def import_map_tiles(media, media_index, output, paths):
	owned, replaced = [], []
	for archive_path in paths:
		entry = media_index.get(archive_path)
		if entry is None:
			continue
		relative = "Media_extracted/" + archive_path
		target = safe_target(output, relative)
		stored = sro_pk2.payload(media, entry)
		if not target.exists():
			variant.replace_file(target, stored)
			owned.append(relative)
			continue
		if target.read_bytes() == stored:
			continue
		original = safe_target(output / ORIGINALS, relative)
		if not original.exists():
			original.parent.mkdir(parents=True, exist_ok=True)
			write_atomically(original, target.read_bytes())
		variant.replace_file(target, stored)
		replaced.append(relative)
	return owned, replaced


# ================
# restore_map_tiles
#
# The off half of import_map_tiles: every replaced tile gets its
# original bytes back and the kept copy leaves.
# ================
def restore_map_tiles(output, prior):
	for relative in prior.get("replacedTiles", []):
		original = safe_target(output / ORIGINALS, relative)
		if original.exists():
			variant.replace_file(safe_target(output, relative), original.read_bytes())
			original.unlink()


#============================================================================
# Labels
#============================================================================

LOCALINFO = "worldmap_localinfo.txt"
MAPINFO = "worldmap_mapinfo.txt"
# v1.696 keeps the map pages in wlocalmap.txt: the v1.150 worldmap_mapinfo
# row's 19 columns, then a title key and three later fields.
LATER_MAPINFO = "wlocalmap.txt"
# A town page row: id, kind 1, name, the page art.
PAGE_KIND, PAGE_ART = "1", 3
TEXT_NAMES = "textdataname.txt"
# v1.696 worldmap_localinfo: service, then the v1.150 row's 20 columns,
# then three later ones (NPC keys, two flags) the v1.150 layout lacks.
LATER_LOCALINFO_COLUMNS = LOCALINFO_COLUMNS + 1
# Columns of the v1.696 row: kind (1 text label, 2 image), the text key or
# image path, the page a click opens, the page the row stands on, its sector.
KIND, KEY, DESTINATION, PAGE, SECTOR_X, SECTOR_Y = 2, 3, 7, 8, 9, 10
LABEL_KIND, IMAGE_KIND = "1", "2"
# The overview's page id (map_manager.txt's field page).
OVERVIEW_PAGE = "0"
# The destination column inside the 20-column v1.150 row.
NATIVE_DESTINATION = DESTINATION - 1
# v1.696 textdata_object_*.txt rows: service, id, key, Korean, five
# unused, English.
LATER_TEXT_KEY, LATER_TEXT_KOREAN, LATER_TEXT_ENGLISH = 2, 3, 9
LATER_TEXT_PREFIX = TEXTDATA + "textdata_object"
# A v1.150 textdataname row: service, key, Korean, '', description,
# zh-CN, zh-TW, ja, en, vi, ''.
NATIVE_TEXT_COLUMNS, NATIVE_TEXT_ENGLISH = 11, 8
# A table row ends in CR LF; a bare LF is a line break inside a cell.
ROW_END = "\r\n"
# An image row's art comes with its hover and pressed faces.
IMAGE_FACES = ("", "_focus", "_press")


# ================
# later_rows
#
# The v1.696 text tables are UTF-16 without a byte-order mark, which
# decode_table would read as CP949; a NUL second byte tells them apart.
# ================
def later_rows(raw):
	if raw[:2] not in (b"\xff\xfe", b"\xfe\xff") and raw[1:2] == b"\x00":
		raw = b"\xff\xfe" + raw
	return table_rows(raw)


# ================
# map_label_rows
#
# The v1.696 overview rows standing on an Arabia region, as v1.150 rows:
# the area names (Qarq, the Desert of Dreams, the Arabian Coast, the two
# outposts), Baghdad's name and city badge, and the NPC and gate icons.
# The owner found the M map showed Arabia's art with no names on it
# (2026-11-15). Rows standing on a page this import adds (Baghdad's town
# page 18: its NPC, market and gate pins) come too. A click destination
# the variant has no page for becomes 0: the badge shows and its click
# opens nothing, as the Alexandria merge drops icons to unpublished pages
# rather than point them nowhere (inference: no published page, no click).
# ================
def map_label_rows(rows, regions, native_ids, pages, added_pages=frozenset()):
	out = []
	for row in rows:
		if len(row) < LATER_LOCALINFO_COLUMNS or row[0] != "1" or row[1] in native_ids:
			continue
		if row[PAGE] not in added_pages and not overview_row_in(row, regions):
			continue
		label = row[KIND] == LABEL_KIND and row[KEY].startswith("SN_")
		image = row[KIND] == IMAGE_KIND and row[KEY].lower().endswith(".ddj")
		if not (label or image):
			continue
		native = row[1:1 + LOCALINFO_COLUMNS]
		if native[NATIVE_DESTINATION] not in pages:
			native[NATIVE_DESTINATION] = OVERVIEW_PAGE
		out.append(native)
	return out


# ================
# overview_row_in
# ================
def overview_row_in(row, regions):
	if row[PAGE] != OVERVIEW_PAGE or not (row[SECTOR_X].isdigit() and row[SECTOR_Y].isdigit()):
		return False
	return ((int(row[SECTOR_Y]) << 8) | int(row[SECTOR_X])) in regions


# ================
# map_page_rows
#
# The town pages Arabia's overview badges open (Baghdad, page 18) that
# the variant lacks, as v1.150 worldmap_mapinfo rows: the first 19
# columns of the v1.696 wlocalmap row, the shape the Alexandria merge
# publishes for Alexandria's page 12. The owner clicked Baghdad on the
# M map and it opened nothing (2026-11-15).
# ================
def map_page_rows(later_pages, later_pins, regions, pages):
	wanted = {row[DESTINATION] for row in later_pins
		if len(row) >= LATER_LOCALINFO_COLUMNS and row[0] == "1" and overview_row_in(row, regions)}
	return [row[:MAPINFO_COLUMNS] for row in later_pages
		if len(row) >= MAPINFO_COLUMNS and row[0] in wanted and row[0] not in pages and row[1] == PAGE_KIND
		and row[PAGE_ART].lower().endswith(".ddj")]


# ================
# label_texts
#
# The v1.150 textdataname rows for the label keys the variant lacks,
# from the v1.696 textdata_object shards. A key no shard names stays
# out, and import_map_labels drops its label.
# ================
def label_texts(media, media_index, keys):
	texts = {}
	for name in sorted(n for n in media_index if n.startswith(LATER_TEXT_PREFIX) and n.endswith(".txt")):
		for row in later_rows(sro_pk2.payload(media, media_index[name])):
			if len(row) > LATER_TEXT_ENGLISH and row[LATER_TEXT_KEY] in keys:
				native = ["1", row[LATER_TEXT_KEY], row[LATER_TEXT_KOREAN]] + [""] * (NATIVE_TEXT_COLUMNS - 3)
				native[NATIVE_TEXT_ENGLISH] = row[LATER_TEXT_ENGLISH]
				texts.setdefault(row[LATER_TEXT_KEY], native)
	return [texts[key] for key in sorted(texts)]


# ================
# import_map_labels
#
# Appends the town pages Arabia's badges open to worldmap_mapinfo.txt,
# Arabia's overview and page rows to worldmap_localinfo.txt and their
# names to textdataname.txt, and returns (pin ids, text keys, image
# paths, page ids) for the marker and the tile copy. Rows a prior run added are
# already there and are not added twice. An extraction without the map
# tables (or a source without its localinfo) has no overview to label.
#
# The tables are appended to, never re-encoded: table_rows splits a row
# whose text holds a line break, so writing its rows back rewrote 48
# textdataname descriptions and the asset build refused them as stale
# (2026-11-15). append_rows keeps the native bytes first, as the
# Alexandria merge does.
# ================
def import_map_labels(media, media_index, text_root, regions):
	local_path, names_path = text_root / LOCALINFO, text_root / TEXT_NAMES
	tables = (local_path, names_path, text_root / MAPINFO)
	if not all(path.exists() for path in tables) or TEXTDATA + LOCALINFO not in media_index:
		return [], [], [], []
	native = table_rows(local_path.read_bytes())
	map_path = text_root / MAPINFO
	pages = {row[0] for row in table_rows(map_path.read_bytes()) if row}
	later = later_rows(sro_pk2.payload(media, media_index[TEXTDATA + LOCALINFO]))
	later_pages = []
	if TEXTDATA + LATER_MAPINFO in media_index:
		later_pages = later_rows(sro_pk2.payload(media, media_index[TEXTDATA + LATER_MAPINFO]))
	page_rows = map_page_rows(later_pages, later, regions, pages)
	if page_rows:
		write_atomically(map_path, append_rows(map_path.read_bytes(), page_rows))
	added = {row[0] for row in page_rows}
	pins = map_label_rows(later, regions, {row[0] for row in native if row}, pages | added, added)
	name_rows = table_rows(names_path.read_bytes())
	known = {row[1] for row in name_rows if len(row) > 1}
	wanted = {row[2] for row in pins if row[1] == LABEL_KIND} - known
	texts = label_texts(media, media_index, wanted)
	resolved = known | {row[1] for row in texts}
	pins = [row for row in pins if row[1] != LABEL_KIND or row[2] in resolved]
	if pins:
		write_atomically(local_path, append_rows(local_path.read_bytes(), pins))
	if texts:
		write_atomically(names_path, append_rows(names_path.read_bytes(), texts))
	images = {row[2].replace("\\", "/").lower()[:-len(".ddj")] + face + ".ddj"
		for row in pins if row[1] == IMAGE_KIND for face in IMAGE_FACES}
	images |= {row[PAGE_ART].replace("\\", "/").lower() for row in page_rows}
	return [row[0] for row in pins], [row[1] for row in texts], sorted(images), sorted(added)


# ================
# remove_map_labels
#
# The off half of import_map_labels: the pins and texts the marker
# records leave their tables. Only the physical lines of those rows go
# (each is one line, as import_map_labels wrote it); every other byte,
# a line break inside a description included, stays as it was.
# ================
def remove_map_labels(text_root, prior):
	pins, texts = set(prior.get("mapPins", [])), set(prior.get("mapTexts", []))
	added = set(prior.get("mapPages", []))
	for name, column, owned in ((LOCALINFO, 0, pins), (TEXT_NAMES, 1, texts), (MAPINFO, 0, added)):
		path = text_root / name
		if not owned or not path.exists():
			continue
		lines = decode_table(path.read_bytes()).lstrip("\ufeff").split(ROW_END)
		kept = [line for line in lines if not owned_line(line, column, owned)]
		write_atomically(path, b"\xff\xfe" + ROW_END.join(kept).encode("utf-16-le"))


# ================
# owned_line
# ================
def owned_line(line, column, owned):
	cells = line.split("\t")
	return len(cells) > column and cells[column] in owned
