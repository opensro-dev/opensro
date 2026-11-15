"""
===========================================================================

arabia_map_tiles.py - Arabia's M map and minimap art from the v1.696 client

arabia_import.py copies Arabia's terrain, navmesh, NPCs and tables; this
module owns the map art those regions need: one minimap tile per outdoor
region and the M map overview tiles that cover them. A tile the variant
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
from alexandria_map_tables import overview_tiles  # noqa: E402
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
