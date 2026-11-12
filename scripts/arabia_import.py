"""
===========================================================================

arabia_import.py - optional Arabia continent (Baghdad and its fields).

Port-only, not native. Off unless SRO_ARABIA is "1", "on" or "true".

Two later sources, both the operator's own and never committed:

	the v1.696 client   SRO_V1696_ROOT, hash-pinned before anything is
	                    read (v1696_archive_pins.json beside this tool).
	                    It carries the continent's terrain, navmesh,
	                    names and the MOB_AR_* / MOB_ARABIA_* character
	                    and skill rows.
	the population      SRO_ARABIA_DATABASE names a folder holding
	export              arabia-population.json: 1,656 authored nests of
	                    the town, the three fields and the Baghdad
	                    dungeon, extracted from the later server's
	                    shard backup. The export records the backup's
	                    sha256; only the pinned backup's export is
	                    admitted.

The town and the three fields stand on the persistent world; their
1,347 monster nests are written as a Version-2 population file
(arabia-field.txt) and join the default world, as the cap-110 field
population does. The Baghdad dungeon is a world of its own in the
source (112, GROUP_ARABIA_DUNGEON, Type 0, twenty regions); its 237
nests (arabia-dungeon.txt) join that world, registered by the server
only while the switch is on. The boss-lair, Petra and Garden worlds
(113-115) author no nests in the backup: their regions' terrain still
merges with the continent (players can walk them), but no world is
registered for them and nothing spawns there - the admission slice
sources their content or leaves them empty deliberately.

Region reconciliation, recorded: the client's regioncode names 263
RN_ARABIA_* regions (FIELD 180, TOWN 26, PETRA 28, DUNGEON 20,
GARDEN 9) and the build merges exactly those; the backup's nests
cover 173 of them. The town's NPC nests (72 rows) are not monsters:
they are counted here and stay for the NPC pass, as do entry and
admission (travel to Baghdad, the dungeon's gate).

===========================================================================
"""
import hashlib
import json
import mmap
import os
from pathlib import Path

import alexandria_npcs as characters
from alexandria_map_tables import append_rows, native_text_row, table_text
import cap110_monsters as cap110
import cap110_skill_effects as effects
import import_vsro_alexandria as tables
import sro_pk2
from jupiter_import import later_object_text, later_text_row, project_population, require_v1696_pin
from muhan_import import merge_muhan_region_tables
from prepare_client_resources import write_atomically

FLAG = "SRO_ARABIA"
ROOT_ENV = "SRO_V1696_ROOT"
SOURCE_ENV = "SRO_ARABIA_DATABASE"
MARKER = ".opensro-arabia.json"
# The 210000 identity band: 120000 is the cap-110 family's, 130000 the
# pet import's (140000/150000 held for its later sources), 160000 the
# Sky Temple's, 170000 Jupiter's, 180000 Hunting Ground 2's, 190000
# the GOD-era temples', 200000 Muhan's. One importer owns one shard
# file in a build.
CHARACTER_SHARD = "characterdata_210000.txt"
SKILL_SHARD = "skilldata_210000.txt"
FIELD_FILE = "arabia-field.txt"
DUNGEON_FILE = "arabia-dungeon.txt"
POPULATION_NAME = "arabia-population.json"
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA

# The later server's shard backup the population export was extracted
# from (research/investigations/later-sources-3247, extract_arabia_population.py).
# An export made from another backup is refused before anything is staged.
POPULATION_SOURCE_SHA256 = "89c6835f8e51b1a75950242c89cf3ad639ea7d50a22191a90632cca333664a45"

DUNGEON_WORLD = 112
DUNGEON_CODE = "GROUP_ARABIA_DUNGEON"
FIELD_WORLD = 1
REGION_PREFIX = "RN_ARABIA_"
PREFIXES = ("MOB_AR_", "MOB_ARABIA_")
# The family's level window: the v1.696 client's Arabia rows run
# 120-130, and the backup's 51 nest kinds sit inside it at 121-130.
MIN_LEVEL = 120
MAX_LEVEL = 130


#============================================================================
# Flags
#============================================================================


# ================
# flag_enabled
# ================
def flag_enabled(env=os.environ):
	"""True only when SRO_ARABIA names an on value; unset is native."""
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON


#============================================================================
# Population export
#============================================================================


# ================
# read_population
#
# The population export: its recorded backup hash must be the pinned
# one, the dungeon world must be present as a Type-0 world binding
# regions, and every monster nest must stand on the persistent world
# or in the dungeon - a nest anywhere else has no projection here.
# ================
def read_population(source_root):
	path = Path(source_root) / POPULATION_NAME
	try:
		export = json.loads(path.read_text(encoding="utf-8"))
	except (OSError, ValueError) as error:
		raise SystemExit(f"{path} is unreadable: {error}")
	if str(export.get("sourceSha256", "")).lower() != POPULATION_SOURCE_SHA256:
		raise ValueError(
			f"population export {path} records backup {export.get('sourceSha256') or 'none'}, "
			f"not the pinned shard backup ({POPULATION_SOURCE_SHA256})"
		)
	worlds = export.get("worlds") or {}
	dungeon = worlds.get(str(DUNGEON_WORLD))
	if dungeon is None or dungeon.get("CodeName") != DUNGEON_CODE or dungeon.get("Type") != 0 or not dungeon.get("Regions"):
		raise ValueError(f"population export {path} does not bind the Baghdad dungeon world {DUNGEON_WORLD}")
	for row in export.get("rows") or []:
		if row["Codename"].startswith("MOB_") and row["WorldID"] not in (FIELD_WORLD, DUNGEON_WORLD):
			raise ValueError(
				f"arabia nest {row['NestID']} ({row['Codename']}) stands in world {row['WorldID']}, "
				"which this import does not project"
			)
	return export


# ================
# dungeon_regions
#
# The region ids the Baghdad dungeon world binds: the build covers
# exactly these, and the client must name every one of them.
# ================
def dungeon_regions(export):
	return sorted(export["worlds"][str(DUNGEON_WORLD)]["Regions"])


# ================
# nest_partition
#
# The export's rows split for the two population files: monster nests
# of the persistent world, monster nests of the dungeon, and the rest
# (the NPC population, counted for the NPC pass, never projected).
# ================
def nest_partition(export):
	field, dungeon, npcs = [], [], 0
	for row in export.get("rows") or []:
		if not row["Codename"].startswith("MOB_"):
			npcs += 1
		elif row["WorldID"] == DUNGEON_WORLD:
			dungeon.append(row)
		else:
			field.append(row)
	return field, dungeon, npcs


# ================
# client_regions
#
# The continent's regions as the v1.696 client's regioncode names
# them: every RN_ARABIA_* row's id, grouped by the group's suffix
# (FIELD, TOWN, PETRA, DUNGEON, GARDEN). The build merges exactly
# these; a dungeon region the client does not name refuses the build.
# ================
def client_regions(later_tables, export):
	groups = {}
	for row in tables.table_rows(later_tables["regioncode.txt"]):
		if len(row) >= 4 and row[2].startswith(REGION_PREFIX) and row[2] != "xxx":
			group = row[2][len(REGION_PREFIX):].split("_")[0]
			groups.setdefault(group, set()).add(int(row[1]))
	ids = set()
	for members in groups.values():
		ids |= members
	missing = [region for region in dungeon_regions(export) if region not in ids]
	if missing:
		raise ValueError(f"the client names no RN_ARABIA region for the dungeon's bound regions {missing}")
	return groups, sorted(ids)


# ================
# family_admits
#
# One later character row against the family's admission: an Arabia
# kind the nests name, at a level inside the family's window.
# ================
def family_admits(code, row, needed):
	if code not in needed:
		return False
	if not code.startswith(PREFIXES):
		return False
	return MIN_LEVEL <= int(row[57]) <= MAX_LEVEL


#============================================================================
# The monster import
#============================================================================


# ================
# add_monsters
# ================
def add_monsters(v1696_root, native, output, source_root=None):
	native, output = Path(native).resolve(), Path(output).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("arabia overlay must be separate from native extraction")
	marker = output / MARKER
	prior = cap110.read_json(marker) if marker.exists() else {}
	text_root = output / TEXT_ROOT
	if not flag_enabled():
		if prior:
			effect_path = text_root / "skilleffect.txt"
			effect_text = effects.remove_animation_bases(tables.decode_table(effect_path.read_bytes()), set(prior.get("animationBases", [])))
			write_atomically(effect_path, b"\xff\xfe" + cap110.drop_characterinfo_rows(effect_text, set(prior.get("characterInfo", []))).encode("utf-16-le"))
			text_path = text_root / "textdataname.txt"
			write_atomically(text_path, cap110.encode_rows(cap110.drop_symboled_rows(characters.translation_rows(text_path.read_bytes()), set(prior.get("nameSymbols", [])))))
		for name in (CHARACTER_SHARD, SKILL_SHARD, FIELD_FILE, DUNGEON_FILE):
			(text_root / name).unlink(missing_ok=True)
		manifest = text_root / "skilldata.txt"
		if manifest.exists():
			write_atomically(manifest, cap110.encode_rows([r for r in tables.table_rows(manifest.read_bytes()) if r[0].lower() != SKILL_SHARD]))
		marker.unlink(missing_ok=True)
		return {"enabled": False}
	if not v1696_root:
		raise SystemExit(f"Pass the v1.696 client folder or set {ROOT_ENV}.")
	require_v1696_pin(v1696_root, ("Media",))
	source_root = Path(source_root or os.environ[SOURCE_ENV])
	export = read_population(source_root)
	field_rows, dungeon_rows, npc_rows = nest_partition(export)
	field = project_population(field_rows, {FIELD_WORLD})
	dungeon = project_population(dungeon_rows, {DUNGEON_WORLD})
	needed = {row["Codename"] for row in field["Rows"]} | {row["Codename"] for row in dungeon["Rows"]}
	with open(Path(v1696_root) / "Media.pk2", "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as media:
		index = tables.archive_index(media)

		# ================
		# read
		# ================
		def read(name):
			return tables.table_text(media, index, name.lower())

		later = {row[2]: row for shard in tables.table_rows(read("characterdata.txt")) for row in tables.table_rows(read(shard[0]))}
		refs = {}
		for code in sorted(later):
			if family_admits(code, later[code], needed):
				refs[code] = characters.convert_character(later[code])
		absent = needed - set(refs)
		if absent:
			raise ValueError(f"the nests name kinds the client does not carry in the window: {sorted(absent)}")
		native_chars = [r for p in text_root.glob("characterdata_*.txt") if p.name != CHARACTER_SHARD for r in tables.table_rows(p.read_bytes())]
		occupied = {int(r[1]) for r in native_chars}
		allocated = cap110.allocate_identities(occupied, sorted(refs), prior.get("characters", {}))
		for code, row in refs.items():
			row[1] = str(allocated[code])
		native_skills = [r for p in text_root.glob("skilldata_*.txt") if p.name != SKILL_SHARD for r in tables.table_rows(p.read_bytes()) if len(r) == 118]
		needed_skills = {int(s) for r in refs.values() for s in r[89:99] if int(s)}
		client_skills = [r for shard in tables.table_rows(read("skilldata.txt"))
			for r in tables.table_rows(read(shard[0])) if len(r) > 3 and r[1].isdigit()]
		added, skill_map = cap110.merge_skills(native_skills, client_skills, needed_skills, prior.get("skills", {}))
		for row in refs.values():
			row[89:99] = [str(skill_map[int(s)]) if int(s) else "0" for s in row[89:99]]
		# Sorted by identity: the codename set has no stable order, and a
		# rebuild should write the same bytes.
		write_atomically(text_root / CHARACTER_SHARD, cap110.encode_rows(sorted(refs.values(), key=lambda row: int(row[1]))))
		write_atomically(text_root / SKILL_SHARD, cap110.encode_rows(added))
		manifest = tables.table_rows((text_root / "skilldata.txt").read_bytes())
		manifest = [r for r in manifest if r[0].lower() != SKILL_SHARD] + [[SKILL_SHARD]]
		write_atomically(text_root / "skilldata.txt", cap110.encode_rows(manifest))
		write_atomically(text_root / FIELD_FILE, (json.dumps(field) + "\n").encode())
		write_atomically(text_root / DUNGEON_FILE, (json.dumps(dungeon) + "\n").encode())
		base_text = (text_root / "textdataname.txt").read_bytes()
		known = {r[1] for r in characters.translation_rows(base_text) if len(r) > 1}
		wanted = {r[5] for r in refs.values()} - known
		object_raw = b"\xff\xfe" + later_object_text(read).encode("utf-16-le")
		texts = [[r[0], r[1], r[2], "", "", r[5], r[4], r[7], r[8], r[9], ""] for r in
			(later_text_row(row) for row in characters.translation_rows(object_raw)) if len(r) >= 10 and r[1] in wanted]
		if wanted - {r[1] for r in texts}:
			raise ValueError("missing arabia monster names")
		# Name rows a previous build owned leave with the species that
		# named them: the marker's list is the previous build's owned set.
		stale_names = set(prior.get("nameSymbols", [])) - {r[5] for r in refs.values()}
		if stale_names:
			base_text = cap110.encode_rows(cap110.drop_symboled_rows(characters.translation_rows(base_text), stale_names))
		write_atomically(text_root / "textdataname.txt", characters.append_rows(base_text, texts))
		effect_path = text_root / "skilleffect.txt"
		effect_text = tables.decode_table(effect_path.read_bytes())
		later_effects = tables.decode_table(read("skilleffect.txt"))
		selected = effects.select_later_rows(later_effects, {r[3] for r in added} - effects.native_bases(effect_text))
		unknown = effects.missing_animation_states([line for rows in selected.values() for _, line in rows], effect_text)
		if unknown:
			raise ValueError("monster animation states absent natively: " + str(sorted(unknown)))
		effect_text, _ = effects.merge_skill_effect_text(effect_text, selected)
		known_info = {r[0] for r in tables.table_rows(effect_text.encode("utf-16")) if r}
		info = [r[:characters.SKILL_EFFECT_KEPT] for r in tables.table_rows(read("skilleffect.txt"))
			if len(r) == characters.SKILL_EFFECT_COLUMNS and r[0] in refs and r[0] not in known_info]
		# Same rule for the characterinfo rows: a species no longer
		# referenced loses its row, one still referenced keeps it.
		owned_info = cap110.characterinfo_codenames(effect_text) & set(refs)
		stale_info = set(prior.get("characterInfo", [])) - owned_info
		if stale_info:
			effect_text = cap110.drop_characterinfo_rows(effect_text, stale_info)
		write_atomically(effect_path, characters.skill_effect_section(effect_text.encode("utf-16"), info))
		summary = {"enabled": True,
			"characters": {c: int(r[1]) for c, r in refs.items()}, "skills": {r[3]: int(r[1]) for r in added},
			"kinds": len(refs), "fieldNests": len(field["Rows"]), "dungeonNests": len(dungeon["Rows"]),
			"npcRows": npc_rows,
			"animationBases": sorted(set(prior.get("animationBases", [])) | {base for rows in selected.values() for base, _ in rows}),
			"characterInfo": sorted(owned_info | {r[0] for r in info}),
			"nameSymbols": sorted(({r[1] for r in characters.translation_rows(base_text) if len(r) > 1}
				| {r[1] for r in texts}) & {r[5] for r in refs.values()}),
			"sourcePopulationSha256": POPULATION_SOURCE_SHA256,
			"populationFileSha256": sro_pk2.digest((source_root / POPULATION_NAME).read_bytes())}
		write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode())
		return summary


#============================================================================
# The variant-side merge (port-only, not native). The tile and object
# catalog machinery it remaps onto lives in build_alexandria_variant
# (the Alexandria merge owns it) and is imported lazily; the name-table
# merge is muhan_import.merge_muhan_region_tables; the population is
# add_monsters.
#============================================================================


# ================
# add_arabia
# ================
def add_arabia(native, out, v1696_root=None, source_root=None):
	"""
	Merge the Arabia continent from the pinned v1.696 client into the
	variant: every RN_ARABIA_* region's terrain remapped onto the
	merged tile catalog, their navmesh and availability bits, every
	Data file the native tree lacks, the regions' minimap tiles and
	names, and the monster population. Port-only, not native; off,
	only the monster pass's owned outputs are reaped and no archive
	is read.
	"""
	import build_alexandria_variant as variant
	if not flag_enabled():
		add_monsters(None, native, out)
		return {"enabled": False}
	v1696_root = Path(v1696_root or os.environ.get(ROOT_ENV, ""))
	if not str(v1696_root):
		raise SystemExit(f"Pass the v1.696 client folder or set {ROOT_ENV}.")
	source_root = source_root or os.environ.get(SOURCE_ENV, "")
	if not source_root:
		raise SystemExit(f"{SOURCE_ENV} names no population export folder")
	require_v1696_pin(v1696_root, ("Map", "Media", "Data"))
	export = read_population(source_root)
	textdata = Path("Media_extracted") / tables.TEXTDATA

	# Media first: the continent's region set is the client's, and the
	# reconciliation (263 named regions, the nests' 173) is recorded
	# in the summary rather than assumed on either side.
	media_path, media_handle, media_data = tables.open_archive(v1696_root, "Media")
	try:
		media_index = tables.archive_index(media_data)

		def media_table(name):
			entry = media_index.get((tables.TEXTDATA + name).lower())
			if entry is None:
				raise SystemExit(f"Media.pk2 has no {tables.TEXTDATA}{name}")
			return sro_pk2.payload(media_data, entry)

		later_tables = {name: media_table(name) for name in ("regioncode.txt", "textzonename.txt")}
		groups, regions = client_regions(later_tables, export)
		local_regions = [region_id for region_id in regions if region_id & 0x8000]
		sectors = [tables.region_xy(region_id) for region_id in regions if not region_id & 0x8000]
		native_media = variant.lower_paths(native / "Media_extracted")
		minimaps = 0
		for sector_x, sector_y in sectors:
			name = f"minimap/{sector_x}x{sector_y}.ddj"
			entry = media_index.get(name)
			if entry is not None and name not in native_media:
				variant.replace_file(out / "Media_extracted" / name, sro_pk2.payload(media_data, entry))
				minimaps += 1
	finally:
		media_data.close()
		media_handle.close()

	# Map: the regions' files, their terrain remapped onto the catalog
	# the Alexandria merge left, and the ground textures they paint.
	map_path, map_handle, map_data = tables.open_archive(v1696_root, "Map")
	try:
		map_index = tables.archive_index(map_data)
		terrains, tile_ids, map_missing = {}, set(), []
		for sector_x, sector_y in sectors:
			for archive_path in tables.region_files(sector_x, sector_y):
				entry = map_index.get(archive_path)
				if entry is None:
					map_missing.append(archive_path)
					continue
				stored = sro_pk2.payload(map_data, entry)
				if archive_path.endswith(".m"):
					tile_ids |= tables.terrain_tile_ids(stored)
				terrains[archive_path] = stored
		missing_terrain = [path for path in map_missing if path.endswith(".m")]
		if missing_terrain:
			raise SystemExit(f"Map.pk2 lacks terrain for Arabia regions: {missing_terrain}")
		tiles, remap = variant.merge_tile_catalog(
			(out / "Map_extracted" / "tile2d.ifo").read_bytes(),
			sro_pk2.payload(map_data, map_index["tile2d.ifo"]), tile_ids
		)
		objects = variant.merge_object_catalog(
			(out / "Map_extracted" / "object.ifo").read_bytes(),
			sro_pk2.payload(map_data, map_index["object.ifo"])
		)
		variant.replace_file(out / "Map_extracted" / "tile2d.ifo", tiles)
		variant.replace_file(out / "Map_extracted" / "object.ifo", objects)
		for archive_path, stored in sorted(terrains.items()):
			if archive_path.endswith(".m"):
				stored = variant.remap_terrain(stored, remap)
			variant.replace_file(out / "Map_extracted" / archive_path, stored)
		later_tiles = tables.tile_catalog(sro_pk2.payload(map_data, map_index["tile2d.ifo"]))
		textures = 0
		for tile_id in sorted(tile_ids):
			name = later_tiles[tile_id][1].replace("\\", "/")
			entry = map_index.get("tile2d/" + name.lower())
			target = out / "Map_extracted" / "tile2d" / name
			if entry is None:
				map_missing.append("tile2d/" + name)
			elif not target.exists():
				variant.replace_file(target, sro_pk2.payload(map_data, entry))
				textures += 1
	finally:
		map_data.close()
		map_handle.close()

	# Data: every file the native tree lacks (the regions' objects and
	# the monsters' models), the regions' navmesh, their availability.
	data_added = variant.add_missing_files(
		v1696_root, "Data", out / "Data_extracted", variant.lower_paths(out / "Data_extracted"), ("navmesh/",)
	)
	data_path, data_handle, data_data = tables.open_archive(v1696_root, "Data")
	try:
		data_index = tables.archive_index(data_data)
		nav_added = 0
		for sector_x, sector_y in sectors:
			nav = tables.navmesh_path(sector_x, sector_y)
			entry = data_index.get(nav)
			if entry is None:
				raise SystemExit(f"Data.pk2 has no {nav}; the Arabia regions are incomplete")
			variant.replace_file(out / "Data_extracted" / nav, sro_pk2.payload(data_data, entry))
			nav_added += 1
		for region_id in local_regions:
			nav = f"navmesh/ainavdata_{region_id}.dat"
			entry = data_index.get(nav)
			if entry is None:
				raise SystemExit(f"Data.pk2 has no {nav}; the Arabia regions are incomplete")
			variant.replace_file(out / "Data_extracted" / nav, sro_pk2.payload(data_data, entry))
			nav_added += 1
	finally:
		data_data.close()
		data_handle.close()
	mapinfo = out / "Data_extracted" / "navmesh" / "mapinfo.mfo"
	variant.replace_file(mapinfo, variant.enable_regions(mapinfo.read_bytes(), sectors))

	current_tables = {name: (out / textdata / name).read_bytes() for name in later_tables}
	merged_tables, table_summary = merge_muhan_region_tables(current_tables, later_tables, regions)
	for name, content in merged_tables.items():
		variant.replace_file(out / textdata / name, content)

	monsters = add_monsters(v1696_root, native, out, source_root)
	nest_regions = len({row["RegionID"] for row in export.get("rows") or []})
	return {
		"enabled": True, "regions": len(regions), "localRegions": len(local_regions),
		"regionGroups": {name: len(members) for name, members in sorted(groups.items())},
		"nestRegions": nest_regions, "mapFilesAdded": len(terrains),
		"tileTexturesAdded": textures, "mapFilesMissing": sorted(map_missing),
		"dataFilesAdded": data_added, "navmeshAdded": nav_added, "minimapTiles": minimaps,
		**table_summary, "monsters": monsters
	}


# ================
# main
# ================
def main():
	import argparse
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--v1696-root", default=os.environ.get(ROOT_ENV))
	parser.add_argument("--native-extracted", required=True)
	parser.add_argument("--output", required=True)
	parser.add_argument("--source-root", default=os.environ.get(SOURCE_ENV))
	args = parser.parse_args()
	# Off, add_monsters only removes its owned outputs and reads no archive.
	print(json.dumps(add_monsters(args.v1696_root, args.native_extracted, args.output, args.source_root), indent=2))


if __name__ == "__main__":
	main()
