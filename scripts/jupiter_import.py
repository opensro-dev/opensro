"""
===========================================================================

jupiter_import.py - optional Jupiter field and Mirror Dimension population.

Port-only, not native. Off unless SRO_JUPITER is "1", "on" or "true";
SRO_JUPITER_TEMPLE (the two Mirror Dimension temple sets) additionally
needs SRO_JUPITER, as the cap-110 temple needs the field flag.

Two later sources, both the operator's own and never committed:

	the v1.696 client   SRO_V1696_ROOT, hash-pinned before anything is
	                    read (v1696_archive_pins.json beside this tool).
	                    It carries the MOB_JUPITER_* character and skill
	                    rows the population names.
	the population      SRO_JUPITER_DATABASE names a folder holding
	export              jupiter-population.json: the authored nests,
	                    tactics and hive facts of worlds 86-94, extracted
	                    from the later server's shard backup. The export
	                    records the backup's sha256; only the pinned
	                    backup's export is admitted.

The admitted kinds (levels 111-120, the authored grades) are converted
to the v1.150 layout in a character shard of their own, their monster
skills are merged the cap-110 way, and each family's nests are written
as a Version-2 population file (jupiter-field.txt, jupiter-temple.txt)
the server reads behind the same flags. add_jupiter (below) is the
variant-side merge: the regions' terrain, navmesh, names and Data
files, built on the Alexandria merge's catalog machinery. A switch off removes exactly
the rows and files the marker records. No authored drops: the monsters
keep the native loot posture until their drop tables are imported.

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
from prepare_client_resources import write_atomically

FLAG = "SRO_JUPITER"
TEMPLE_FLAG = "SRO_JUPITER_TEMPLE"
ROOT_ENV = "SRO_V1696_ROOT"
SOURCE_ENV = "SRO_JUPITER_DATABASE"
MARKER = ".opensro-jupiter.json"
# The 170000 identity band: 120000 is the cap-110 family's, 130000 the
# pet import's (140000/150000 held for its later sources), 160000 the
# Sky Temple's. One importer owns one shard file in a build.
CHARACTER_SHARD = "characterdata_170000.txt"
SKILL_SHARD = "skilldata_170000.txt"
FIELD_FILE = "jupiter-field.txt"
TEMPLE_FILE = "jupiter-temple.txt"
POPULATION_NAME = "jupiter-population.json"
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA

PIN_FILE = Path(__file__).with_name("v1696_archive_pins.json")
PIN_FORMAT = "opensro-vsro-archive-pins-v1"
# The later server's shard backup the population export was extracted
# from (research/investigations/later-sources-3247, extract_jupiter_population.py).
# An export made from another backup is refused before anything is staged.
POPULATION_SOURCE_SHA256 = "89c6835f8e51b1a75950242c89cf3ad639ea7d50a22191a90632cca333664a45"

FIELD_WORLD = 90
TEMPLE_WORLDS = (86, 87, 88, 89, 93, 94)
MIN_LEVEL = 111
MAX_LEVEL = 120
GRADES = (0, 1, 3, 6, 8)
PREFIX = "MOB_JUPITER_"

_verified_archives = set()


#============================================================================
# Flags and pins
#============================================================================


# ================
# flag_enabled
# ================
def flag_enabled(env=os.environ):
	"""True only when SRO_JUPITER names an on value; unset is native."""
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON


# ================
# temple_enabled
# ================
def temple_enabled(env=os.environ):
	"""True only when SRO_JUPITER_TEMPLE names an on value."""
	return env.get(TEMPLE_FLAG, "").strip().lower() in tables.FLAG_ON


# ================
# archive_pins
# ================
def archive_pins():
	"""The pinned v1.696 archive hashes (v1696_archive_pins.json beside this tool)."""
	try:
		pin = json.loads(PIN_FILE.read_text(encoding="utf-8"))
	except (OSError, ValueError) as error:
		raise SystemExit(f"{PIN_FILE} is unreadable: {error}")
	if pin.get("format") != PIN_FORMAT or not isinstance(pin.get("archives"), dict):
		raise SystemExit(f"{PIN_FILE} is not a {PIN_FORMAT} pin file")
	return pin["archives"]


# ================
# require_v1696_pin
#
# The v1.696 archives against their pins, before any of them is read:
# a missing or changed archive fails the build. A process verifies
# each archive once.
# ================
def require_v1696_pin(root, names):
	for name in names:
		path = Path(root) / f"{name}.pk2"
		key = str(path.resolve())
		if key in _verified_archives:
			continue
		expected = archive_pins().get(name)
		if expected is None:
			raise SystemExit(f"{PIN_FILE.name} pins no {name}.pk2; pin the archive before reading it")
		if not path.is_file():
			raise SystemExit(f"{path} not found; --v1696-root must be the pinned v1.696 client folder")
		hasher = hashlib.sha256()
		with open(path, "rb") as stream:
			for chunk in iter(lambda: stream.read(1 << 20), b""):
				hasher.update(chunk)
		actual = hasher.hexdigest()
		if actual != expected:
			raise SystemExit(
				f"{path} does not match the pinned v1.696 archive: sha256 is {actual}, pinned is {expected}. "
				"Jupiter is built from one pinned v1.696 client; its archives are never committed."
			)
		_verified_archives.add(key)


#============================================================================
# Population export
#============================================================================


# ================
# read_population
#
# The population export: its recorded backup hash must be the pinned
# one, and the three area sets must all be present.
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
	for world in (FIELD_WORLD, *TEMPLE_WORLDS):
		if str(world) not in worlds:
			raise ValueError(f"population export {path} has no world {world}")
	return export


# ================
# population_regions
#
# The region ids a build covers: the field world's, plus the temple
# worlds' while the temple switch is on.
# ================
def population_regions(export, temple):
	worlds = export["worlds"]
	ids = set(worlds[str(FIELD_WORLD)]["Regions"])
	if temple:
		for world in TEMPLE_WORLDS:
			ids |= set(worlds[str(world)]["Regions"])
	return sorted(ids)


# ================
# project_population
#
# The nest rows of the given worlds, in the cap-110 Version-2 row
# schema the server reads. Extraction facts the schema does not carry
# (the continent, the hive join) are validated, never kept: a nest
# flag or a keep-count hive would need a projection this family does
# not have, so it refuses by name instead of silently shrinking. A
# hive total cap is the one exception: the row schema has no
# cross-nest cap, so the capped kind (the field guest's eleven spots,
# one alive) projects onto its first nest with the cap as its count.
# ================
def project_population(rows, worlds):
	projected = []
	hive_kept = set()
	for row in sorted(rows, key=lambda item: item["NestID"]):
		if row["WorldID"] not in worlds:
			continue
		hive = row.get("Hive") or {}
		if row.get("NestFlag") or hive.get("KeepMonsterCountType"):
			raise ValueError("jupiter nest requires a projection this family does not have: " + row["Codename"])
		cap = hive.get("OverwriteMaxTotalCount") or 0
		if cap:
			if row["Codename"] in hive_kept:
				continue
			hive_kept.add(row["Codename"])
		out = {
			"Codename": row["Codename"], "NestID": row["NestID"], "WorldID": row["WorldID"],
			"RegionID": row["RegionID"], "X": row["X"], "Y": row["Y"], "Z": row["Z"],
			"Radius": row["Radius"], "GenerateRadius": row["GenerateRadius"],
			"MaxCount": min(row["MaxCount"], cap) if cap else row["MaxCount"], "RespawnDelayMinSec": row["RespawnDelayMinSec"],
			"RespawnDelayMaxSec": row["RespawnDelayMaxSec"], "Respawn": row["Respawn"],
			"InitialDir": row["InitialDir"], "ChampionGenPercentage": row["ChampionGenPercentage"],
			"Controls": row["Controls"], "Champion": row["Champion"],
		}
		if row.get("SpawnType"):
			out["SpawnType"] = row["SpawnType"]
		projected.append(out)
	projected.sort(key=lambda row: row["NestID"])
	return {"Version": 2, "Rows": projected}


# The one guest the source places in the field beside the family: an
# Arabia kind at the window's own top level and a family grade. Like
# the cap-110 field's Apis, it is admitted by name, on the same window.
GUEST_CODE = "MOB_ARABIA_KIDEMONAS"


# ================
# later_object_text
#
# The later client's name table as one text. In v1.696
# textdata_object.txt is a shard index (it lists TextData_Object_<n>
# parts) where the VSRO client's was one file: follow the index when
# the named file is one, and read a flat table as itself.
# ================
def later_object_text(read):
	rows = tables.table_rows(read("textdata_object.txt"))
	shards = [row[0] for row in rows if row]
	if shards and all(len(row) == 1 and row[0].lower().endswith(".txt") for row in rows if row):
		return "".join(tables.decode_table(read(name)) for name in shards)
	return tables.decode_table(read("textdata_object.txt"))


# ================
# later_text_row
#
# One later name row in the layout the name merge reads: v1.696 text
# rows carry 17 columns (a numeric text id after the service cell,
# one more language cell, "0" where a translation is absent) where
# the VSRO rows the merge was written for carry 15. Drop the id, and
# let an absent translation take the English cell, as VSRO authors
# them - the same mapping the mall avatar import applies.
# ================
def later_text_row(row):
	if len(row) != 17 or not row[1].strip().isdigit():
		return row
	cells = [row[0]] + row[2:16]
	english = cells[8]
	for column in range(3, 15):
		if cells[column] == "0":
			cells[column] = "" if column == 3 else english
	return cells


# ================
# family_admits
#
# One later character row against the family's admission window: a
# MOB_JUPITER_* codename (or the field's named guest), its level
# (row 57) inside 111-120 and its grade (row 15) one of the authored
# grades.
# ================
def family_admits(code, row):
	if not code.startswith(PREFIX) and code != GUEST_CODE:
		return False
	return MIN_LEVEL <= int(row[57]) <= MAX_LEVEL and int(row[15]) in GRADES


#============================================================================
# The monster import
#============================================================================


# ================
# add_monsters
# ================
def add_monsters(v1696_root, native, output, source_root=None):
	native, output = Path(native).resolve(), Path(output).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("jupiter overlay must be separate from native extraction")
	field, temple = flag_enabled(), temple_enabled()
	if temple and not field:
		raise ValueError(f"{TEMPLE_FLAG} requires {FLAG}")
	marker = output / MARKER
	prior = cap110.read_json(marker) if marker.exists() else {}
	text_root = output / TEXT_ROOT
	if not field:
		if prior:
			effect_path = text_root / "skilleffect.txt"
			effect_text = effects.remove_animation_bases(tables.decode_table(effect_path.read_bytes()), set(prior.get("animationBases", [])))
			write_atomically(effect_path, b"\xff\xfe" + cap110.drop_characterinfo_rows(effect_text, set(prior.get("characterInfo", []))).encode("utf-16-le"))
			text_path = text_root / "textdataname.txt"
			write_atomically(text_path, cap110.encode_rows(cap110.drop_symboled_rows(characters.translation_rows(text_path.read_bytes()), set(prior.get("nameSymbols", [])))))
		for name in (CHARACTER_SHARD, SKILL_SHARD, FIELD_FILE, TEMPLE_FILE):
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
	# The export's nests also place the area's NPCs (believers, commerce
	# and garrison rows, all level 0): roster content, not monsters.
	# They stay out of both populations for the NPC slice to place.
	npc_rows = sum(1 for row in export["rows"] if row["Codename"].startswith("NPC_"))
	rows = [row for row in export["rows"] if not row["Codename"].startswith("NPC_")]
	claimed = {
		"field": {row["Codename"] for row in rows if row["WorldID"] == FIELD_WORLD},
		"temple": {row["Codename"] for row in rows if row["WorldID"] in TEMPLE_WORLDS} if temple else set(),
	}
	with open(Path(v1696_root) / "Media.pk2", "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as media:
		index = tables.archive_index(media)

		# ================
		# read
		# ================
		def read(name):
			return tables.table_text(media, index, name.lower())

		later = {row[2]: row for shard in tables.table_rows(read("characterdata.txt")) for row in tables.table_rows(read(shard[0]))}
		claims = {}
		for family, codes in claimed.items():
			for code in codes:
				claims.setdefault(code, []).append(family)
		refs = {}
		for code in sorted(claims):
			if code not in later:
				raise ValueError("claimed monster has no v1.696 character row: " + code)
			if family_admits(code, later[code]):
				refs[code] = characters.convert_character(later[code])
		for family, codes in claimed.items():
			missing = sorted(codes - set(refs))
			if missing:
				raise ValueError(f"jupiter {family} nests name unadmitted monsters: {missing}")
		native_chars = [r for p in text_root.glob("characterdata_*.txt") if p.name != CHARACTER_SHARD for r in tables.table_rows(p.read_bytes())]
		occupied = {int(r[1]) for r in native_chars}
		allocated = cap110.allocate_identities(occupied, sorted(refs), prior.get("characters", {}))
		for code, row in refs.items():
			row[1] = str(allocated[code])
		native_skills = [r for p in text_root.glob("skilldata_*.txt") if p.name != SKILL_SHARD for r in tables.table_rows(p.read_bytes()) if len(r) == 118]
		needed = {int(s) for r in refs.values() for s in r[89:99] if int(s)}
		client_skills = [r for shard in tables.table_rows(read("skilldata.txt"))
			for r in tables.table_rows(read(shard[0])) if len(r) > 3 and r[1].isdigit()]
		added, skill_map = cap110.merge_skills(native_skills, client_skills, needed, prior.get("skills", {}))
		for row in refs.values():
			row[89:99] = [str(skill_map[int(s)]) if int(s) else "0" for s in row[89:99]]
		populations = {"field": project_population(rows, (FIELD_WORLD,))}
		if temple:
			populations["temple"] = project_population(rows, TEMPLE_WORLDS)
		# Sorted by identity: the codename set has no stable order, and a
		# rebuild should write the same bytes.
		write_atomically(text_root / CHARACTER_SHARD, cap110.encode_rows(sorted(refs.values(), key=lambda row: int(row[1]))))
		write_atomically(text_root / SKILL_SHARD, cap110.encode_rows(added))
		manifest = tables.table_rows((text_root / "skilldata.txt").read_bytes())
		manifest = [r for r in manifest if r[0].lower() != SKILL_SHARD] + [[SKILL_SHARD]]
		write_atomically(text_root / "skilldata.txt", cap110.encode_rows(manifest))
		write_atomically(text_root / FIELD_FILE, (json.dumps(populations["field"]) + "\n").encode())
		temple_path = text_root / TEMPLE_FILE
		if temple:
			write_atomically(temple_path, (json.dumps(populations["temple"]) + "\n").encode())
		else:
			temple_path.unlink(missing_ok=True)
		base_text = (text_root / "textdataname.txt").read_bytes()
		known = {r[1] for r in characters.translation_rows(base_text) if len(r) > 1}
		wanted = {r[5] for r in refs.values()} - known
		object_raw = b"\xff\xfe" + later_object_text(read).encode("utf-16-le")
		texts = [[r[0], r[1], r[2], "", "", r[5], r[4], r[7], r[8], r[9], ""] for r in
			(later_text_row(row) for row in characters.translation_rows(object_raw)) if len(r) >= 10 and r[1] in wanted]
		if wanted - {r[1] for r in texts}:
			raise ValueError("missing jupiter monster names")
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
			if len(r) == characters.SKILL_EFFECT_COLUMNS and r[0].startswith((PREFIX, GUEST_CODE)) and r[0] not in known_info]
		# Same rule for the characterinfo rows: a species no longer
		# referenced loses its row, one still referenced keeps it.
		owned_info = cap110.characterinfo_codenames(effect_text) & set(refs)
		stale_info = set(prior.get("characterInfo", [])) - owned_info
		if stale_info:
			effect_text = cap110.drop_characterinfo_rows(effect_text, stale_info)
		write_atomically(effect_path, characters.skill_effect_section(effect_text.encode("utf-16"), info))
		summary = {"enabled": True, "temple": temple, "npcRows": npc_rows,
			"characters": {c: int(r[1]) for c, r in refs.items()}, "skills": {r[3]: int(r[1]) for r in added},
			"families": {name: {"nests": len(population["Rows"]),
				"regions": sorted({r["RegionID"] for r in population["Rows"]})}
				for name, population in populations.items()},
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
# helpers are alexandria_map_tables'. The population is add_monsters.
#============================================================================


# ================
# merge_jupiter_region_tables
# ================
def merge_jupiter_region_tables(native, later, region_ids):
	"""
	The native regioncode.txt and textzonename.txt with the Jupiter
	regions' rows from the v1.696 tables: a region code replaces the
	native placeholder of its id, and each region's zone names join by
	id and by code prefix, as the Alexandria map merge does.
	"""
	id_strings = {str(region_id) for region_id in region_ids}
	native_regions = {row[1]: row for row in tables.table_rows(native["regioncode.txt"]) if len(row) >= 4}
	code_rows = [
		row for row in tables.table_rows(later["regioncode.txt"])
		if len(row) >= 4 and row[1] in id_strings and row[2] != "xxx"
		and native_regions.get(row[1], [0, 0, "xxx"])[2] == "xxx"
	]
	replaced = {row[1] for row in code_rows}
	kept = [
		row for row in tables.table_rows(native["regioncode.txt"]) if len(row) < 2 or row[1] not in replaced
	]
	regioncode = (
		append_rows(b"\xff\xfe" + table_text(kept).encode("utf-16-le"), code_rows)
		if replaced else append_rows(native["regioncode.txt"], code_rows)
	)
	prefixes = tuple(sorted({row[2] + "_" for row in code_rows}))
	native_zone_keys = {row[1] for row in tables.table_rows(native["textzonename.txt"]) if len(row) > 1}
	zone_rows = [
		native_text_row(row) for row in tables.table_rows(later["textzonename.txt"])
		if len(row) >= 10 and (row[1] in id_strings or row[1].startswith(prefixes))
		and row[1] not in native_zone_keys
	]
	return {
		"regioncode.txt": regioncode,
		"textzonename.txt": append_rows(native["textzonename.txt"], zone_rows)
	}, {"regionCodes": len(code_rows), "zoneNames": len(zone_rows)}


# ================
# add_jupiter
# ================
def add_jupiter(native, out, v1696_root=None, source_root=None):
	"""
	Merge the Jupiter field (and, with SRO_JUPITER_TEMPLE, the Mirror
	Dimension temple sets) from the pinned v1.696 client into the
	variant: the regions' terrain remapped onto the merged tile
	catalog, their navmesh and availability bits, every Data file the
	native tree lacks, the regions' minimap tiles and names, and the
	monster population. Port-only, not native; off, only the monster
	pass's owned outputs are reaped and no archive is read.
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
	regions = population_regions(export, temple_enabled())
	# The temple sets' named interiors are dungeon regions (bit 15):
	# no sector terrain exists for them - their geometry is the DOF
	# tree the Data pass below copies, published into the client's
	# dungeon catalogue by its own slice. Only outdoor regions take
	# sector files; the interiors take their authored nav data.
	local_regions = [region_id for region_id in regions if region_id & 0x8000]
	sectors = [tables.region_xy(region_id) for region_id in regions if not region_id & 0x8000]
	textdata = Path("Media_extracted") / tables.TEXTDATA

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
			raise SystemExit(f"Map.pk2 lacks terrain for Jupiter regions: {missing_terrain}")
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
				raise SystemExit(f"Data.pk2 has no {nav}; the Jupiter regions are incomplete")
			variant.replace_file(out / "Data_extracted" / nav, sro_pk2.payload(data_data, entry))
			nav_added += 1
		for region_id in local_regions:
			nav = f"navmesh/ainavdata_{region_id}.dat"
			entry = data_index.get(nav)
			if entry is None:
				raise SystemExit(f"Data.pk2 has no {nav}; the Jupiter regions are incomplete")
			variant.replace_file(out / "Data_extracted" / nav, sro_pk2.payload(data_data, entry))
			nav_added += 1
	finally:
		data_data.close()
		data_handle.close()
	mapinfo = out / "Data_extracted" / "navmesh" / "mapinfo.mfo"
	variant.replace_file(mapinfo, variant.enable_regions(mapinfo.read_bytes(), sectors))

	# Media: the regions' minimap tiles and their code and zone rows.
	media_path, media_handle, media_data = tables.open_archive(v1696_root, "Media")
	try:
		media_index = tables.archive_index(media_data)
		native_media = variant.lower_paths(native / "Media_extracted")
		minimaps = 0
		for sector_x, sector_y in sectors:
			name = f"minimap/{sector_x}x{sector_y}.ddj"
			entry = media_index.get(name)
			if entry is not None and name not in native_media:
				variant.replace_file(out / "Media_extracted" / name, sro_pk2.payload(media_data, entry))
				minimaps += 1

		def media_table(name):
			entry = media_index.get((tables.TEXTDATA + name).lower())
			if entry is None:
				raise SystemExit(f"Media.pk2 has no {tables.TEXTDATA}{name}")
			return sro_pk2.payload(media_data, entry)

		later_tables = {name: media_table(name) for name in ("regioncode.txt", "textzonename.txt")}
	finally:
		media_data.close()
		media_handle.close()
	current_tables = {name: (out / textdata / name).read_bytes() for name in later_tables}
	merged_tables, table_summary = merge_jupiter_region_tables(current_tables, later_tables, regions)
	for name, content in merged_tables.items():
		variant.replace_file(out / textdata / name, content)

	monsters = add_monsters(v1696_root, native, out, source_root)
	return {
		"enabled": True, "regions": len(regions), "localRegions": len(local_regions), "mapFilesAdded": len(terrains),
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
