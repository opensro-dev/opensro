"""
===========================================================================

import_vsro_tomb_minimap.py - the later client's Ch'in Tomb minimap art

The v1.150 archives ship floor art for Donwhang Stone Cave alone; the pinned
VSRO client also has minimap_d/jinsi, on the same grid. This tool converts
those tiles and writes their own catalogue beside the retail one, so the
retail catalogue and its coverage gate never change. The client merges it
into the tomb's empty floors.
The asset build runs this step when SRO_VSRO_ROOT names the pinned archive
(laterClientImports.mjs); run it by hand only to refresh a built tree, and
follow it with `pnpm assets repack`: the client loads published assets only.
No retail art is committed to Git.

===========================================================================
"""
import argparse
import json
import re
import shutil
import tempfile
from pathlib import Path

import sro_pk2
from convert_images import save_image_payload
from import_vsro_alexandria import verify_vsro_archive
from sro_paths import PUBLIC_ROOT

CATALOG_FORMAT = "sro-mission-dungeon-minimap-manifest"
CATALOG_VERSION = 1
CATALOG_NAME = "later-dungeon-minimap.json"
TILE_DIRECTORY = "jinsi"
TILE_PUBLIC_ROOT = "/assets/images/Media_extracted/minimap_d/"
DDJ_HEADER_BYTES = 20
DDJ_MAGIC = b"JMXVDDJ "
DDS_MAGIC = b"DDS "
TILE_NAME = re.compile(r"^minimap_d/jinsi/(qt_a01_floor0([1-6]))_(-?\d+x-?\d+)\.ddj$")
# dungeonInfo.mjs: jinsi_floor01..06 are regions 7..2. Each DOF holds one floor.
FLOOR_REGIONS = {1: 0x8007, 2: 0x8006, 3: 0x8005, 4: 0x8004, 5: 0x8003, 6: 0x8002}
TOMB_FLOOR_INDEX = 0


def tomb_tiles(names):
	"""
	The archive names that are tomb tiles, as (name, prefix, floor, key).
	Everything else in the archive is ignored.
	"""
	tiles = []
	for name in names:
		match = TILE_NAME.match(name.lower().replace("\\", "/"))
		if match:
			tiles.append((name, match[1], int(match[2]), match[3]))
	return sorted(tiles, key=lambda tile: (tile[2], tile[3]))


def tomb_catalog(tiles):
	"""The catalogue the client's minimapDungeons and minimapArt read."""
	dungeons = []
	for floor, region in FLOOR_REGIONS.items():
		rows = [tile for tile in tiles if tile[2] == floor]
		if not rows:
			raise SystemExit(f"the later client has no tomb floor {floor}")
		dungeons.append({
			"sectorId": region,
			"floors": [{
				"floorIndex": TOMB_FLOOR_INDEX,
				"floorLabel": rows[0][1].upper(),
				"directory": TILE_DIRECTORY,
				"prefix": rows[0][1],
				"tiles": [row[3] for row in rows]
			}]
		})
	return {
		"format": CATALOG_FORMAT,
		"version": CATALOG_VERSION,
		"dungeons": dungeons,
		"tilePaths": [f"{TILE_PUBLIC_ROOT}{TILE_DIRECTORY}/{tile[1]}_{tile[3]}.png" for tile in tiles]
	}


def ddj_payload(data):
	"""The DDS inside a DDJ file (convert_images.extract_ddj_payload, in memory)."""
	if len(data) < DDJ_HEADER_BYTES or not data.startswith(DDJ_MAGIC):
		raise ValueError("not a JMXVDDJ texture")
	size = int.from_bytes(data[12:16], "little")
	payload = data[DDJ_HEADER_BYTES:DDJ_HEADER_BYTES + size]
	if not payload.startswith(DDS_MAGIC):
		raise ValueError("DDJ payload is not DDS")
	return payload


def entry_name(entry):
	"""An archive entry's path as text; names are CP949."""
	name = getattr(entry, "path", None) or entry.name
	return name if isinstance(name, str) else name.decode("cp949")


def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--vsro-root", required=True, help="the pinned VSRO client folder")
	args = parser.parse_args()
	verify_vsro_archive(args.vsro_root, "Media")
	data = (Path(args.vsro_root) / "Media.pk2").read_bytes()
	directory = sro_pk2.read_directory(data)
	entries = {entry_name(entry): entry for entry in (directory.values() if isinstance(directory, dict) else directory)}
	tiles = tomb_tiles(entries)

	images = PUBLIC_ROOT / "assets" / "images" / "Media_extracted" / "minimap_d" / TILE_DIRECTORY
	images.mkdir(parents=True, exist_ok=True)
	with tempfile.TemporaryDirectory(prefix="sro-tomb-minimap-") as scratch:
		for name, prefix, _floor, key in tiles:
			target = Path(scratch) / f"{prefix}_{key}.png"
			save_image_payload(ddj_payload(sro_pk2.payload(data, entries[name])), target, target)
			shutil.move(str(target), images / target.name)
	catalog = PUBLIC_ROOT / "assets" / "data" / CATALOG_NAME
	catalog.write_text(json.dumps(tomb_catalog(tiles), separators=(",", ":")), encoding="utf-8", newline="\n")
	print(f"{len(tiles)} tomb minimap tile(s) written; run pnpm assets repack")


if __name__ == "__main__":
	main()
