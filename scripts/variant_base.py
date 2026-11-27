"""
===========================================================================

variant_base.py - the variant's first step: the native tree linked in, the city laid over it

PORT-ONLY, NOT NATIVE. The base step links the native folders into the
variant, merges the map catalogs, lays the stage 1 import's map and data
over them, and adds the VSRO files the native tree lacks. Its summary
carries the counts and the tile remap the variant manifest is written
from. A step module of its own (#4251), so its key is its own code.

===========================================================================
"""
from variant_files import (
	add_missing_files, enable_regions, link_tree, log, lower_paths, merge_object_catalog, merge_tile_catalog,
	remap_terrain, replace_file
)
import variant_reuse
import import_vsro_alexandria as stage1


LINKED_FOLDERS = ("Media_extracted", "Particles_extracted", "Music_mp3")


# ================
# build_base
#
# The fold's first step: the native folders linked in, the map catalogs
# merged, the imported map and data laid over them. Its summary carries
# the counts and the tile remap the manifest is written from.
# ================
def build_base(vsro_root, native, imported, out, regions, stage_manifest):
	linked = {}
	for folder in LINKED_FOLDERS + ("Map_extracted", "Data_extracted"):
		if not (native / folder).is_dir():
			raise SystemExit(f"{native / folder} is missing; run pnpm assets prepare for the v1.150 client first")
		reuse = variant_reuse.reuse_root(out.parent) / out.name / folder
		linked[folder] = link_tree(native / folder, out / folder, reuse)
		log(f"{folder}: {linked[folder]} files linked")

	# Map: catalogs, remapped regions, tile textures.
	vsro_catalogs = {name: (imported / "Map" / name).read_bytes() for name in stage1.CATALOGS}
	tiles, remap = merge_tile_catalog(
		(native / "Map_extracted" / "tile2d.ifo").read_bytes(), vsro_catalogs["tile2d.ifo"], set(stage_manifest["tileIds"])
	)
	objects = merge_object_catalog((native / "Map_extracted" / "object.ifo").read_bytes(), vsro_catalogs["object.ifo"])
	replace_file(out / "Map_extracted" / "tile2d.ifo", tiles)
	replace_file(out / "Map_extracted" / "object.ifo", objects)

	added, clashes = [], []
	for source in sorted((imported / "Map").rglob("*")):
		if not source.is_file() or source.name in stage1.CATALOGS:
			continue
		relative = source.relative_to(imported / "Map").as_posix()
		target = out / "Map_extracted" / relative
		stored = source.read_bytes()
		if relative.endswith(".m"):
			stored = remap_terrain(stored, remap)
		if target.exists() and relative.startswith("tile2d/"):
			if target.read_bytes() != stored:
				clashes.append(relative)
			continue
		replace_file(target, stored)
		added.append("Map_extracted/" + relative)

	# Data: every VSRO file the native tree lacks, the navmesh, the bitmap.
	present = lower_paths(native / "Data_extracted")
	data_added = add_missing_files(vsro_root, "Data", out / "Data_extracted", present, ("navmesh/",))
	# Particles: the city's effect programs (alex_*.efp) and what they use.
	particles_added = add_missing_files(
		vsro_root, "Particles", out / "Particles_extracted", lower_paths(native / "Particles_extracted")
	)
	for source in sorted((imported / "Data").rglob("*.nvm")):
		replace_file(out / "Data_extracted" / source.relative_to(imported / "Data").as_posix(), source.read_bytes())
	mapinfo = out / "Data_extracted" / "navmesh" / "mapinfo.mfo"
	replace_file(mapinfo, enable_regions((native / "Data_extracted" / "navmesh" / "mapinfo.mfo").read_bytes(), regions))
	return {
		"linked": linked,
		"tileRemap": {str(k): v for k, v in sorted(remap.items())},
		"tileTextureClashes": clashes,
		"mapFilesAdded": len(added),
		"dataFilesAdded": data_added,
		"particleFilesAdded": particles_added,
	}
