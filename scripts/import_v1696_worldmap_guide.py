"""
===========================================================================

import_v1696_worldmap_guide.py - the M map guide's tables and art (#3387)

The renewal M map's guide (Monster and Field tabs, the tinted hunting
grounds, the reverse return buttons and the guide's own scrollbar) is
native to the later client only. This importer reads the operator's
pinned v1.696 Media.pk2 (SRO_V1696_ROOT, pins in v1696_archive_pins.json)
and publishes one catalogue, /assets/data/worldmap-guide.json, plus the
art it names. No retail data or art is committed.

The tables and every column's meaning are traced in the v1.696 client
(research/notes/worldmap-renewal-v1696.md): map_manager.txt (the map
pages), area.txt (the Field tree), monster.txt (the Monster tree),
worldmapguidedata_region.txt (each hunting ground's tiles) and
worldmap_localinfo.txt (where a Field row points). Two columns each in
area.txt and monster.txt have no reader in the client: the content
tier (the level cap the content opened at, plus one) and the continent
(0 China, 1 Europe, 3 the shared West). The owner chose to use them
(2026-11-13): the guide groups by continent and gates rows above the
server's cap. Every column is carried, including the unread ones.

The hunting-ground tiles are colour-keyed masks shared by a page: the
client keeps a pixel when its ARGB value is in the monster's colour set
and makes every other pixel transparent (TextureColorGenerate,
sub_f46270). This importer applies that test once, here, and publishes
one white mask per monster tile, so the client only tints it.

Native quirk, not copied: monster records are keyed by codename, so a
codename listed on two pages (four Donwhang cave monsters) keeps only
its first row and both region lines land on it. The region file lists
exactly one line per monster row in row order (checked for every
codename), so this importer pairs the n-th line with the n-th row.

===========================================================================
"""
import argparse
import hashlib
import json
import mmap
import os
import struct
from pathlib import Path

from PIL import Image

import sro_pk2
from convert_images import save_image_payload
from import_vsro_alexandria import archive_index, table_rows
from import_vsro_tomb_minimap import ddj_payload
from sro_paths import PUBLIC_ROOT

V1696_ENV = "SRO_V1696_ROOT"
PIN_FILE = Path(__file__).with_name("v1696_archive_pins.json")
PIN_FORMAT = "opensro-vsro-archive-pins-v1"
TEXTDATA = "server_dep/silkroad/textdata/"
CATALOG = PUBLIC_ROOT / "assets" / "data" / "worldmap-guide.json"
IMAGES = PUBLIC_ROOT / "assets" / "images" / "Media_extracted" / "interface"
MASK_DIR = "worldmap/guide-mask/"
CATALOG_FORMAT = "opensro-worldmap-guide-v1"
HASH_CHUNK = 1 << 20

# The DDS header the tiles carry: 128 bytes after the magic's offset.
DDS_HEADER_BYTES = 128
# A tile pixel the client never tints (TextureColorGenerate leaves 0 and
# 0xffffff alone; every other pixel is either in the set or cleared).
UNKEYED_PIXELS = (0x00000000, 0x00ffffff)
# The characterdata columns of a character's codename and level (the
# same indices cap110_monsters reads).
LEVEL_COLUMN = 57
CODENAME_COLUMN = 2

# The art the guide draws besides the tiles, by its path under interface/:
# the hunting ground's icon and the guide scrollbar's arrows, thumb and
# track in their faces. The reverse return button's three faces
# (worldmap/map/xy_returnicon*) are not here: the v1.188 buttons import
# owns them, and a test keeps the two lists disjoint.
ART = (
	"worldmap/wmap_monster_icon",
	"guide/gd_scroll_up",
	"guide/gd_scroll_up_focus",
	"guide/gd_scroll_up_press",
	"guide/gd_scroll_down",
	"guide/gd_scroll_down_focus",
	"guide/gd_scroll_down_press",
	"guide/gd_scroll_button",
	"guide/gd_scroll_button_focus",
	"guide/gd_scroll_button_press",
	"guide/gd_scroll_line",
	"guide/gd_contents_lamp",
	"guide/gd_index_button_open",
	"guide/gd_index_button_close",
)


#============================================================================
# Archive
#============================================================================


# ================
# verify_media
#
# The v1.696 Media.pk2 against its pin, before a byte of it is read.
# ================
def verify_media(root):
	try:
		pin = json.loads(PIN_FILE.read_text(encoding="utf-8"))
	except (OSError, ValueError) as error:
		raise SystemExit(f"{PIN_FILE} is unreadable: {error}")
	if pin.get("format") != PIN_FORMAT or "Media" not in pin.get("archives", {}):
		raise SystemExit(f"{PIN_FILE} pins no Media.pk2")
	path = Path(root) / "Media.pk2"
	if not path.is_file():
		raise SystemExit(f"{path} not found; {V1696_ENV} must be the pinned v1.696 client folder")
	hasher = hashlib.sha256()
	with open(path, "rb") as stream:
		for chunk in iter(lambda: stream.read(HASH_CHUNK), b""):
			hasher.update(chunk)
	if hasher.hexdigest() != pin["archives"]["Media"]:
		raise SystemExit(f"{path} does not match the pinned v1.696 Media.pk2 (sha256 {hasher.hexdigest()})")
	return path


# ================
# Archive
#
# One open Media.pk2: its bytes and its lower-case path index.
# ================
class Archive:
	def __init__(self, data):
		self.data = data
		self.index = archive_index(data)

	def has(self, path):
		return path.lower() in self.index

	def read(self, path):
		entry = self.index.get(path.lower())
		if entry is None:
			raise SystemExit(f"Media.pk2 has no {path}")
		return sro_pk2.payload(self.data, entry)

	def table(self, name):
		return table_rows(self.read(TEXTDATA + name))

	def sharded(self, name):
		"""A table that is an index of shards (v1.696 text and character data)."""
		rows = self.table(name)
		if rows and all(len(row) == 1 and row[0].strip().lower().endswith(".txt") for row in rows):
			return [row for shard in rows for row in self.table(shard[0].strip())]
		return rows


#============================================================================
# Text and levels
#============================================================================


# ================
# english_text
#
# key -> English of a v1.696 text table. Its rows carry a numeric id
# after the service cell, the key in column 2 and English in column 9.
# ================
def english_text(rows):
	out = {}
	for row in rows:
		if len(row) > 9 and row[0].strip() == "1" and row[1].strip().isdigit():
			english = row[9].strip()
			if english and english != "0":
				out[row[2].strip()] = english
	return out


# ================
# monster_levels
# ================
def monster_levels(rows):
	out = {}
	for row in rows:
		if len(row) > LEVEL_COLUMN and row[0].strip() == "1":
			try:
				out[row[CODENAME_COLUMN].strip()] = int(row[LEVEL_COLUMN])
			except ValueError:
				pass
	return out


#============================================================================
# Tables
#============================================================================


def pair(text):
	"""'46x113' -> [46, 113]."""
	left, right = text.strip().lower().split("x")
	return [int(left), int(right)]


def number(text):
	return int(text.strip())


# ================
# read_pages
#
# map_manager.txt: service, name, origin sector, end region (tiles
# across and down), tile size in pixels, sectors per tile, page id.
# ================
def read_pages(rows):
	pages = {}
	for row in rows:
		if len(row) < 7 or row[0].strip() != "1":
			continue
		pages[row[1].strip()] = {
			"origin": pair(row[2]),
			"extent": pair(row[3]),
			"tile": pair(row[4]),
			"perTile": pair(row[5]),
			"id": number(row[6]),
		}
	return pages


def order_of(row, first):
	return [number(row[first]), number(row[first + 1]), number(row[first + 2])]


def optional(row, column, default=None):
	return number(row[column]) if len(row) > column and row[column].strip().lstrip("-").isdigit() else default


# ================
# read_areas
#
# area.txt: service, Korean name, codename, text key, page, main/sub,
# order China / Europe / third, link kind, link target, instance worlds,
# content tier, continent.
# ================
def read_areas(rows, text):
	areas = []
	for row in rows:
		if len(row) < 11 or row[0].strip() != "1":
			continue
		instances = [name.strip() for name in (row[11] if len(row) > 11 else "").split(",")]
		areas.append({
			"codename": row[2].strip(),
			"textKey": row[3].strip(),
			"name": text.get(row[3].strip(), row[2].strip()),
			"page": row[4].strip(),
			"main": row[5].strip().lower() == "main",
			"order": order_of(row, 6),
			"linkKind": number(row[9]),
			"link": number(row[10]),
			"instances": [name for name in instances if name and name.lower() != "xxx"],
			"tier": optional(row, 12, 1),
			"continent": optional(row, 13, 3),
		})
	return areas


# ================
# read_monsters
#
# monster.txt: service, Korean name, codename, page, main/sub, order
# China / Europe / third, world x / y, sector x / y (-1 = a page
# point), point x / y, mask colours, content tier, continent.
# ================
def read_monsters(rows, text, levels, areas):
	area_names = {area["codename"]: area["name"] for area in areas}
	monsters = []
	for row in rows:
		if len(row) < 15 or row[0].strip() != "1":
			continue
		codename = row[2].strip()
		main = row[4].strip().lower() == "main"
		colours = [int(value.strip(), 16) for value in row[14].split(",") if value.strip().lower().startswith("0x")]
		monsters.append({
			"codename": codename,
			# A main row is a continent group named by its area row; a
			# sub row is a monster named by its object text.
			"name": area_names.get(codename, codename) if main else text.get("SN_" + codename, codename),
			"level": None if main else levels.get(codename),
			"page": row[3].strip(),
			"main": main,
			"order": order_of(row, 5),
			"world": [number(row[8]), number(row[9])],
			"sector": [number(row[10]), number(row[11])],
			"point": [number(row[12]), number(row[13])],
			"colours": colours,
			"tier": optional(row, 15, 1),
			"continent": optional(row, 16, 3),
			"tiles": [],
		})
	return monsters


# ================
# attach_regions
#
# The region file's n-th line for a codename belongs to the n-th
# monster row of that codename (see the banner); a line for a codename
# with no row is dropped, as natively.
# ================
def attach_regions(monsters, rows):
	by_code = {}
	for monster in monsters:
		if not monster["main"]:
			by_code.setdefault(monster["codename"], []).append(monster)
	seen = {}
	for row in rows:
		if len(row) < 2:
			continue
		code = row[0].strip()
		at = seen.get(code, 0)
		seen[code] = at + 1
		owners = by_code.get(code, [])
		if at >= len(owners):
			continue
		owners[at]["tiles"] = [pair(cell) for cell in row[1].split(",") if "x" in cell.lower()]


# ================
# read_localinfo
#
# worldmap_localinfo.txt rows a Field row links to: id (col 1), page id
# (col 8), sector x / y (cols 9-10, -1 on a town page), point (11-12).
# ================
def read_localinfo(rows, wanted):
	out = {}
	for row in rows:
		if len(row) < 13 or row[0].strip() != "1":
			continue
		ident = number(row[1])
		if ident in wanted:
			out[str(ident)] = {"page": number(row[8]), "sector": [number(row[9]), number(row[10])],
				"point": [number(row[11]), number(row[12])]}
	return out


#============================================================================
# Art
#============================================================================


# ================
# tile_pixels
#
# One uncompressed A8R8G8B8 tile as (width, height, pixels).
# ================
def tile_pixels(raw):
	dds = ddj_payload(raw)
	height, width = struct.unpack_from("<II", dds, 12)
	bits = struct.unpack_from("<I", dds, 88)[0]
	if bits != 32:
		raise SystemExit("a hunting-ground tile is not 32-bit ARGB; its colour keys would not survive")
	count = width * height
	return width, height, struct.unpack_from(f"<{count}I", dds, DDS_HEADER_BYTES)


# ================
# write_mask
#
# The tile's pixels in the monster's colour set, white and opaque;
# every other pixel transparent (sub_f46270's test).
# ================
def write_mask(raw, colours, target):
	width, height, pixels = tile_pixels(raw)
	keep = set(colours)
	alpha = bytes(255 if pixel in keep and pixel not in UNKEYED_PIXELS else 0 for pixel in pixels)
	# RGBA, not grey + alpha: Safari misreads grey PNGs through
	# copyExternalImageToTexture (WebKit bug 296208).
	image = Image.new("RGBA", (width, height), (255, 255, 255, 255))
	image.putalpha(Image.frombytes("L", (width, height), alpha))
	target.parent.mkdir(parents=True, exist_ok=True)
	image.save(target)


# ================
# publish_masks
#
# One mask per monster tile, named by the monster's place in the
# catalogue; each monster's tiles list becomes [sx, sy, mask path]. A
# tile the archive lacks is dropped, as the client draws nothing for it.
# ================
def publish_masks(archive, monsters):
	written = missing = 0
	for index, monster in enumerate(monsters):
		kept = []
		for sx, sy in monster["tiles"]:
			source = f"interface/worldmap/monster/{monster['page']}_{sx}x{sy}.ddj"
			if not archive.has(source):
				missing += 1
				continue
			name = f"{MASK_DIR}m{index}_{sx}x{sy}"
			write_mask(archive.read(source), monster["colours"], IMAGES / f"{name}.png")
			kept.append([sx, sy, name])
			written += 1
		monster["tiles"] = kept
	return written, missing


def publish_art(archive):
	for name in ART:
		target = IMAGES / f"{name}.png"
		target.parent.mkdir(parents=True, exist_ok=True)
		save_image_payload(ddj_payload(archive.read(f"interface/{name}.ddj")), target, target)


#============================================================================
# Main
#============================================================================


# ================
# build_catalog
#
# The catalogue from one archive; the art is written beside it.
# ================
def build_catalog(archive):
	ui = english_text(archive.sharded("textuisystem.txt"))
	objects = english_text(archive.sharded("textdata_object.txt"))
	levels = monster_levels(archive.sharded("characterdata.txt"))
	pages = read_pages(archive.table("map_manager.txt"))
	# An area's key is a UI key (UIIT_STT_*) on a group row and a zone
	# name (SN_ZONE_*) in the object text on a town or field row.
	areas = read_areas(archive.table("area.txt"), {**objects, **ui})
	monsters = read_monsters(archive.table("monster.txt"), objects, levels, areas)
	attach_regions(monsters, archive.table("worldmapguidedata_region.txt"))
	localinfo = read_localinfo(archive.table("worldmap_localinfo.txt"),
		{area["link"] for area in areas if area["linkKind"] == 1})
	written, missing = publish_masks(archive, monsters)
	publish_art(archive)
	catalog = {"format": CATALOG_FORMAT, "imageRoot": "/assets/images/Media_extracted/interface/",
		"pages": pages, "areas": areas, "monsters": monsters, "localinfo": localinfo,
		"art": list(ART)}
	return catalog, written, missing


def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--v1696-root", default=os.environ.get(V1696_ENV),
		help="the pinned v1.696 client folder (else SRO_V1696_ROOT)")
	args = parser.parse_args()
	if not args.v1696_root:
		raise SystemExit(f"Pass --v1696-root or set {V1696_ENV} to the pinned v1.696 client folder.")
	path = verify_media(args.v1696_root)
	with open(path, "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as data:
		catalog, written, missing = build_catalog(Archive(data))
	CATALOG.parent.mkdir(parents=True, exist_ok=True)
	CATALOG.write_text(json.dumps(catalog, ensure_ascii=False, separators=(",", ":")), encoding="utf-8")
	print(f"World map guide: {len(catalog['monsters'])} monster row(s), {len(catalog['areas'])} area row(s), "
		f"{written} mask(s), {missing} missing tile(s)")


if __name__ == "__main__":
	main()
