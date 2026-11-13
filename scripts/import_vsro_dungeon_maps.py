"""
===========================================================================

import_vsro_dungeon_maps.py - the later client's dungeon pages for the M map

The v1.150 archives have no dungeon world map. The pinned VSRO client has the
whole system: worldmap_mapinfo.txt's Dungeonmap section (one row per floor),
interface/worldmap/dungeon's tiles, worldmap_localinfo.txt's room names
and gate signs with their destinations, textzonename.txt's page titles
(the floor's region: "Underground Level 1 of Tomb of Qin-Shi") and
textuisystem.txt's monster level note the page foot shows. The v1.150 text
has neither: its zone rows name the tomb floors "Ch'in Tomb B1", and it has
no world map level filter. This tool stitches each floor's tiles
into one page image, so the M window shows a floor at once rather than tile by
tile, and writes one catalogue, later-dungeon-map.json. The asset build runs
this step when SRO_VSRO_ROOT names the pinned archive (#2956), so a build on
a machine with the archive publishes the pages on its own; run this by hand
only to refresh a built tree without one, and follow it with
`pnpm assets repack`: the client loads published assets only. No
retail art or table is committed to Git.

===========================================================================
"""
import argparse
import json
import os
import shutil
import tempfile
from pathlib import Path

from PIL import Image

import sro_pk2
from convert_images import save_image_payload
from import_vsro_alexandria import verify_vsro_archive
from import_vsro_tomb_minimap import ddj_payload, entry_name
from sro_paths import PUBLIC_ROOT

CATALOG_FORMAT = "sro-later-dungeon-map"
CATALOG_VERSION = 2
CATALOG_NAME = "later-dungeon-map.json"
ARCHIVE_ROOT = "interface/worldmap/dungeon/"
PUBLIC_IMAGE_ROOT = "/assets/images/Media_extracted/interface/worldmap/dungeon/"
TEXT_ROOT = "server_dep/silkroad/textdata/"
MAP_TABLE = TEXT_ROOT + "worldmap_mapinfo.txt"
SIGN_TABLE = TEXT_ROOT + "worldmap_localinfo.txt"
ZONE_TEXT = TEXT_ROOT + "textzonename.txt"
OBJECT_TEXT = TEXT_ROOT + "textdata_object.txt"
UI_TEXT = TEXT_ROOT + "textuisystem.txt"
# The page foot's note: the later client's world map level filter.
MONSTER_LEVEL_NOTE = "UIIT_STT_WORLDMAP_LEVEL_MONSTER_LIST"
DUNGEON_SECTION = "#section\tDungeonmap"
# Donwhang Stone Cave and the six Ch'in Tomb floors: the rows this port has.
FIRST_MAP_ID = 2001
LAST_MAP_ID = 2010
ENGLISH_COLUMN = 8
KOREAN_COLUMN = 2
LABEL_ROW = "1"
SIGN_ROW = "2"
SIGN_ICON = "map_world_iconwf"


def table_text(raw):
	"""A text table's lines; the client ships UTF-16 and CP949 ones."""
	if raw[:2] == b"\xff\xfe":
		return raw.decode("utf-16").splitlines()
	try:
		return raw.decode("utf-8").splitlines()
	except UnicodeDecodeError:
		return raw.decode("cp949", "replace").splitlines()


def map_rows(lines):
	"""The Dungeonmap rows for the floors this port has."""
	start = next(index for index, line in enumerate(lines) if line.startswith(DUNGEON_SECTION))
	rows = []
	for line in lines[start + 1:]:
		cells = line.split("\t")
		if line.startswith("//") or not cells[0].isdigit():
			continue
		if not FIRST_MAP_ID <= int(cells[0]) <= LAST_MAP_ID:
			continue
		rows.append({
			"id": int(cells[0]),
			"regionId": int(cells[5]),
			"floorIndex": 0,
			"layer": int(cells[3]),
			"columns": int(cells[6]),
			"rows": int(cells[7]),
			"width": int(cells[8]),
			"height": int(cells[9]),
			"left": int(cells[10]),
			"top": int(cells[11]),
			"right": int(cells[12]),
			"bottom": int(cells[13]),
			"prefix": cells[15].strip().replace("\\", "/").rsplit("/", 1)[-1],
			"floorLabel": cells[17].strip()
		})
	# A dungeon with several floors in one region numbers them from its first.
	for row in rows:
		if sum(1 for other in rows if other["regionId"] == row["regionId"]) > 1:
			row["floorIndex"] = row["layer"] - 1
	return rows


def english_text(lines):
	"""Code name to English, falling back to Korean, from a text table."""
	names = {}
	for line in lines:
		cells = line.split("\t")
		if len(cells) > KOREAN_COLUMN and not line.startswith("//"):
			english = cells[ENGLISH_COLUMN].strip() if len(cells) > ENGLISH_COLUMN else ""
			names[cells[1].strip()] = english or cells[KOREAN_COLUMN].strip()
	return names


def row_text(code, comment, names):
	"""A label's display text: its code's name, else the table's own comment."""
	return names.get(code) or names.get("SN_" + code) or comment.strip()


def add_signs(rows, lines, names):
	"""Puts each floor's room names and gate signs on its row."""
	by_id = {row["id"]: row for row in rows}
	for row in rows:
		row["labels"] = []
		row["signs"] = []
	for line in lines:
		cells = line.split("\t")
		if line.startswith("//") or len(cells) < 20 or not cells[8].isdigit():
			continue
		row = by_id.get(int(cells[8]))
		if not row:
			continue
		x, y = int(cells[11]), int(cells[12])
		if cells[2] == LABEL_ROW:
			row["labels"].append({
				"text": row_text(cells[3].strip(), cells[6], names),
				"x": x,
				"y": y,
				"font": int(cells[13] or 0),
				"color": [int(cells[15]), int(cells[16]), int(cells[17])]
			})
		elif cells[2] == SIGN_ROW and SIGN_ICON in cells[3]:
			code = cells[-1].strip() if len(cells) > 21 else ""
			row["signs"].append({
				"text": cells[6].strip(),
				"name": row_text(code, "", names),
				"x": x,
				"y": y,
				"width": int(cells[13]),
				"height": int(cells[14])
			})


def add_captions(rows, zones):
	"""
	Puts each floor's page title on its row: the later client's zone name
	of the floor's region, which its M window heads the page with. A floor
	whose region has no zone name stops the import, as a page without its
	title would show another client's caption.
	"""
	for row in rows:
		caption = zones.get(str(row["regionId"]), "")
		if not caption:
			raise SystemExit(f"{ZONE_TEXT} names no region {row['regionId']} (Dungeonmap row {row['id']})")
		row["caption"] = caption


def required_text(names, code, table):
	"""One string the catalogue carries; the import stops when it is absent."""
	text = names.get(code, "")
	if not text:
		raise SystemExit(f"{table} has no {code}")
	return text


def tile_cells(row):
	"""Each tile of a floor's region box: its file name and page column and row."""
	return [
		(f"{row['prefix']}{x}x{z}", x - row["left"], row["top"] - z) for x in range(row["left"], row["right"] + 1)
		for z in range(row["bottom"], row["top"] + 1)
	]


def stitch_page(row, read_tile, target):
	"""
	Draws a floor's tiles into one image at its authored page size. read_tile
	gives a tile's opened image, or None where the client ships none.
	"""
	unit_x, unit_y = row["width"] // row["columns"], row["height"] // row["rows"]
	page = Image.new("RGBA", (row["width"], row["height"]), (0, 0, 0, 255))
	for name, column, line in tile_cells(row):
		tile = read_tile(name)
		if tile is not None:
			page.paste(tile.convert("RGBA").resize((unit_x, unit_y), Image.LANCZOS), (column * unit_x, line * unit_y))
	page.convert("RGB").save(target, "PNG", optimize=True)


def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument(
		"--vsro-root", default=os.environ.get("SRO_VSRO_ROOT"),
		help="the pinned VSRO client folder (else SRO_VSRO_ROOT)"
	)
	args = parser.parse_args()
	if not args.vsro_root:
		raise SystemExit("Pass --vsro-root or set SRO_VSRO_ROOT to the pinned VSRO client folder.")
	verify_vsro_archive(args.vsro_root, "Media")
	data = sro_pk2.map_archive(Path(args.vsro_root) / "Media.pk2")
	directory = sro_pk2.read_directory(data)
	entries = {
		entry_name(entry).lower().replace("\\", "/"): entry
		for entry in (directory.values() if isinstance(directory, dict) else directory)
	}

	def read(path):
		return sro_pk2.payload(data, entries[path])

	rows = map_rows(table_text(read(MAP_TABLE)))
	zones = english_text(table_text(read(ZONE_TEXT)))
	add_captions(rows, zones)
	note = required_text(english_text(table_text(read(UI_TEXT))), MONSTER_LEVEL_NOTE, UI_TEXT)
	names = dict(zones)
	names.update(english_text(table_text(read(OBJECT_TEXT))))
	add_signs(rows, table_text(read(SIGN_TABLE)), names)

	images = PUBLIC_ROOT / "assets" / "images" / "Media_extracted" / "interface" / "worldmap" / "dungeon"
	images.mkdir(parents=True, exist_ok=True)
	for stale in images.glob("map_world_*.png"):
		stale.unlink()
	with tempfile.TemporaryDirectory(prefix="sro-dungeon-map-") as scratch:

		def converted(name):
			entry = ARCHIVE_ROOT + name + ".ddj"
			if entry not in entries:
				return None
			target = Path(scratch) / f"{name}.png"
			save_image_payload(ddj_payload(read(entry)), target, target)
			return target

		shutil.move(str(converted(SIGN_ICON)), images / f"{SIGN_ICON}.png")

		def read_tile(name):
			path = converted(name)
			if path is None:
				return None
			with Image.open(path) as tile:
				return tile.copy()

		for row in rows:
			row["image"] = row["prefix"].rstrip("_")
			stitch_page(row, read_tile, images / f"{row['image']}.png")
	catalog = {
		"format": CATALOG_FORMAT,
		"version": CATALOG_VERSION,
		"imageRoot": PUBLIC_IMAGE_ROOT,
		"signIcon": SIGN_ICON,
		"monsterLevelNote": note,
		"maps": rows
	}
	path = PUBLIC_ROOT / "assets" / "data" / CATALOG_NAME
	path.parent.mkdir(parents=True, exist_ok=True)
	path.write_text(json.dumps(catalog, ensure_ascii=False, separators=(",", ":")), encoding="utf-8", newline="\n")
	print(f"{len(rows)} floor page(s) imported")


if __name__ == "__main__":
	main()
