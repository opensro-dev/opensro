"""
===========================================================================

later_world_import.py - the shared engine of the later-world imports.

jupiter_import and god_import are two skins of one import: admit a
pinned population export, convert the later kinds its nests name into
a character shard of their own, merge their skills, names and effects,
and merge their regions' terrain, navmesh, names and Data files into
the variant on the Alexandria catalog machinery. What differs is the
worlds' shape - admission windows, population files, the summary each
build reports - and that lives in each module's WorldImport spec and
its small shape functions. Everything mechanical lives here, once.

A switch off must leave the native tree behind. The variant root is
NOT re-extracted between builds: build_variant links the native tree
into the same extracted root and only fills gaps, so what a build
writes stays until another build rewrites it. The marker each import
writes is therefore the reap's enrolment: the monster pass's owned
rows and files, and the region merge's (files written, availability
sectors, region ids, zone-name prefixes). The tile and object
catalogs need no enrolment: the Alexandria stage rebuilds both from
the native bytes on every build before any later-world merge runs,
so a build with a switch off leaves native catalogs behind on its
own. Both imports are port-only, not native.

Four worlds ride this engine (#3326). jupiter and god are driven by
their population exports end to end (add_world). muhan (waves name
kinds by level, not by nest) and hunting2 (the client's own npcpos
placements; it postdates the shard backup) drive their own monster
passes and take the shared region merge and reap through
merge_world, reap_monsters and reap_world, so the off path above is
decided once for all four. skytemple_import and arabia_import are
the other family: they merge catalogues over the variant's current
ones inside build_alexandria_variant and share their machinery
through skytemple_import; they take no WorldImport spec.

===========================================================================
"""
import hashlib
import json
import mmap
import os
from dataclasses import dataclass
from pathlib import Path
from typing import Callable

import alexandria_npcs as characters
from alexandria_map_tables import append_rows, native_text_row, table_text
import cap110_monsters as cap110
import cap110_skill_effects as effects
import import_vsro_alexandria as tables
import sro_pk2
from prepare_client_resources import write_atomically

TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA
PIN_FORMAT = "opensro-vsro-archive-pins-v1"

_verified_archives = set()


# ================
# WorldImport
#
# One later world's import spec: the names it reads and writes, the
# export worlds it requires, and the shape functions that are truly
# its own (which groups its nests claim, what admits a kind, which
# population files it writes, what its summary reports, which regions
# it covers). Frozen: a spec is a fact about the world, not state.
#
# A world whose population the nest export drives fills every field
# (jupiter, god). A world whose kinds no nest export names drives
# its own monster pass on the same merge and reap (muhan's waves,
# hunting2's client placements): its export-driving fields stay
# empty, and add_monsters / add_world refuse such a spec by name -
# merge_world, reap_monsters and reap_world are its entry points.
# ================
@dataclass(frozen=True)
class WorldImport:
	label: str
	flag: str
	root_env: str
	character_shard: str
	skill_shard: str
	marker: str
	pin_file: Path
	pin_sentence: str
	region_label: str
	population_files: tuple
	requires: str | None = None
	source_env: str = ""
	population_name: str = ""
	source_sha256: str = ""
	worlds_required: tuple = ()
	info_prefixes: tuple = ()
	claim_groups: Callable = None
	admit: Callable = None
	build_populations: Callable = None
	summarize_populations: Callable = None
	regions_of: Callable = None


#============================================================================
# Flags and pins
#============================================================================


# ================
# archive_pins
# ================
def archive_pins(spec):
	"""The pinned v1.696 archive hashes (v1696_archive_pins.json beside this tool)."""
	try:
		pin = json.loads(spec.pin_file.read_text(encoding="utf-8"))
	except (OSError, ValueError) as error:
		raise SystemExit(f"{spec.pin_file} is unreadable: {error}")
	if pin.get("format") != PIN_FORMAT or not isinstance(pin.get("archives"), dict):
		raise SystemExit(f"{spec.pin_file} is not a {PIN_FORMAT} pin file")
	return pin["archives"]


# ================
# require_pin
#
# The v1.696 archives against their pins, before any of them is read:
# a missing or changed archive fails the build. A process verifies
# each archive once. The pins come through the caller's own
# archive_pins so a test can stand pins in for an import module.
# ================
def require_pin(spec, root, names, pins_for):
	pins = pins_for()
	for name in names:
		path = Path(root) / f"{name}.pk2"
		key = str(path.resolve())
		if key in _verified_archives:
			continue
		expected = pins.get(name)
		if expected is None:
			raise SystemExit(f"{spec.pin_file.name} pins no {name}.pk2; pin the archive before reading it")
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
				+ spec.pin_sentence
			)
		_verified_archives.add(key)


# ================
# require_pairing
#
# A world whose kinds join another import's template refuses its own
# switch without that import's, before anything is read - the same
# refusal the server makes at load.
# ================
def require_pairing(spec, env=os.environ):
	if spec.requires and env.get(spec.requires, "").strip().lower() not in tables.FLAG_ON:
		raise ValueError(f"{spec.flag} requires {spec.requires} (its population and drops)")


#============================================================================
# Population export
#============================================================================


# ================
# read_population
#
# The population export: its recorded backup hash must be the pinned
# one, and the spec's worlds must all be present.
# ================
def read_population(spec, source_root):
	path = Path(source_root) / spec.population_name
	try:
		export = json.loads(path.read_text(encoding="utf-8"))
	except (OSError, ValueError) as error:
		raise SystemExit(f"{path} is unreadable: {error}")
	if str(export.get("sourceSha256", "")).lower() != spec.source_sha256:
		raise ValueError(
			f"population export {path} records backup {export.get('sourceSha256') or 'none'}, "
			f"not the pinned shard backup ({spec.source_sha256})"
		)
	worlds = export.get("worlds") or {}
	for world in spec.worlds_required:
		if str(world) not in worlds:
			raise ValueError(f"population export {path} has no world {world}")
	return export


# ================
# project_population
#
# The nest rows of the given worlds, in the cap-110 Version-2 row
# schema the server reads. Extraction facts the schema does not carry
# (the continent, the hive join) are validated, never kept: a nest
# flag or a keep-count hive would need a projection this family does
# not have, so it refuses by name instead of silently shrinking. A
# hive total cap is the one exception: the row schema has no
# cross-nest cap, so a capped kind projects onto its first nest with
# the cap as its count. The spawn kind rides the row as SpawnType,
# as the temple loader preserves it.
# ================
def project_population(spec, rows, worlds):
	projected = []
	hive_kept = set()
	for row in sorted(rows, key=lambda item: item["NestID"]):
		if row["WorldID"] not in worlds:
			continue
		hive = row.get("Hive") or {}
		if row.get("NestFlag") or hive.get("KeepMonsterCountType"):
			raise ValueError(f"{spec.label} nest requires a projection this family does not have: " + row["Codename"])
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


#============================================================================
# The monster import
#============================================================================


# ================
# reap_monsters
#
# The monster half of a switch off: the previous build's enrolled
# rows leave the shared tables, and every file the import owns is
# unlinked. The marker goes last - it is the enrolment both reaps
# read.
# ================
def reap_monsters(spec, output, prior):
	text_root = output / TEXT_ROOT
	if prior:
		effect_path = text_root / "skilleffect.txt"
		effect_text = effects.remove_animation_bases(tables.decode_table(effect_path.read_bytes()), set(prior.get("animationBases", [])))
		write_atomically(effect_path, b"\xff\xfe" + cap110.drop_characterinfo_rows(effect_text, set(prior.get("characterInfo", []))).encode("utf-16-le"))
		text_path = text_root / "textdataname.txt"
		write_atomically(text_path, cap110.encode_rows(cap110.drop_symboled_rows(characters.translation_rows(text_path.read_bytes()), set(prior.get("nameSymbols", [])))))
	for name in (spec.character_shard, spec.skill_shard) + tuple(filename for _, filename in spec.population_files):
		(text_root / name).unlink(missing_ok=True)
	manifest = text_root / "skilldata.txt"
	if manifest.exists():
		write_atomically(manifest, cap110.encode_rows([r for r in tables.table_rows(manifest.read_bytes()) if r[0].lower() != spec.skill_shard]))
	(output / spec.marker).unlink(missing_ok=True)


# ================
# add_monsters
#
# enabled and sub_enabled are the import module's own flag reads (a
# sub-flag pairing refusal, where the world has one, is the module's
# before it delegates). Off, only the enrolled outputs are reaped and
# no archive is read.
# ================
def add_monsters(spec, v1696_root, native, output, source_root, enabled, sub_enabled, pins_for):
	native, output = Path(native).resolve(), Path(output).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError(f"{spec.label} overlay must be separate from native extraction")
	marker = output / spec.marker
	prior = cap110.read_json(marker) if marker.exists() else {}
	text_root = output / TEXT_ROOT
	if not enabled:
		reap_monsters(spec, output, prior)
		return {"enabled": False}
	if spec.claim_groups is None:
		raise ValueError(f"{spec.label} import drives its own monster pass; add_monsters does not serve it")
	require_pairing(spec)
	if not v1696_root:
		raise SystemExit(f"Pass the v1.696 client folder or set {spec.root_env}.")
	require_pin(spec, v1696_root, ("Media",), pins_for)
	if not source_root and not os.environ.get(spec.source_env):
		raise SystemExit(f"{spec.source_env} names no population export folder")
	source_root = Path(source_root or os.environ[spec.source_env])
	export = read_population(spec, source_root)
	# The export's nests also place the worlds' NPCs (believers, commerce
	# and garrison rows, all level 0): roster content, not monsters.
	# They stay out of every population for the NPC slice to place.
	npc_rows = sum(1 for row in export["rows"] if row["Codename"].startswith("NPC_"))
	rows = [row for row in export["rows"] if not row["Codename"].startswith("NPC_")]
	claimed = spec.claim_groups(rows, sub_enabled)
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
			if spec.admit(code, later[code], claims[code]):
				refs[code] = characters.convert_character(later[code])
		for family, codes in claimed.items():
			missing = sorted(codes - set(refs))
			if missing:
				raise ValueError(f"{spec.label} {family} nests name unadmitted monsters: {missing}")
		native_chars = [r for p in text_root.glob("characterdata_*.txt") if p.name != spec.character_shard for r in tables.table_rows(p.read_bytes())]
		occupied = {int(r[1]) for r in native_chars}
		allocated = cap110.allocate_identities(occupied, sorted(refs), prior.get("characters", {}))
		for code, row in refs.items():
			row[1] = str(allocated[code])
		native_skills = [r for p in text_root.glob("skilldata_*.txt") if p.name != spec.skill_shard for r in tables.table_rows(p.read_bytes()) if len(r) == 118]
		needed = {int(s) for r in refs.values() for s in r[89:99] if int(s)}
		client_skills = [r for shard in tables.table_rows(read("skilldata.txt"))
			for r in tables.table_rows(read(shard[0])) if len(r) > 3 and r[1].isdigit()]
		added, skill_map = cap110.merge_skills(native_skills, client_skills, needed, prior.get("skills", {}))
		for row in refs.values():
			row[89:99] = [str(skill_map[int(s)]) if int(s) else "0" for s in row[89:99]]
		populations = spec.build_populations(rows, sub_enabled)
		# Sorted by identity: the codename set has no stable order, and a
		# rebuild should write the same bytes.
		write_atomically(text_root / spec.character_shard, cap110.encode_rows(sorted(refs.values(), key=lambda row: int(row[1]))))
		write_atomically(text_root / spec.skill_shard, cap110.encode_rows(added))
		manifest = tables.table_rows((text_root / "skilldata.txt").read_bytes())
		manifest = [r for r in manifest if r[0].lower() != spec.skill_shard] + [[spec.skill_shard]]
		write_atomically(text_root / "skilldata.txt", cap110.encode_rows(manifest))
		for group, filename in spec.population_files:
			population_path = text_root / filename
			if group in populations:
				write_atomically(population_path, (json.dumps(populations[group]) + "\n").encode())
			else:
				population_path.unlink(missing_ok=True)
		base_text = (text_root / "textdataname.txt").read_bytes()
		known = {r[1] for r in characters.translation_rows(base_text) if len(r) > 1}
		wanted = {r[5] for r in refs.values()} - known
		object_raw = b"\xff\xfe" + later_object_text(read).encode("utf-16-le")
		texts = [[r[0], r[1], r[2], "", "", r[5], r[4], r[7], r[8], r[9], ""] for r in
			(later_text_row(row) for row in characters.translation_rows(object_raw)) if len(r) >= 10 and r[1] in wanted]
		if wanted - {r[1] for r in texts}:
			raise ValueError(f"missing {spec.label} monster names")
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
			if len(r) == characters.SKILL_EFFECT_COLUMNS and r[0].startswith(spec.info_prefixes) and r[0] not in known_info]
		# Same rule for the characterinfo rows: a species no longer
		# referenced loses its row, one still referenced keeps it.
		owned_info = cap110.characterinfo_codenames(effect_text) & set(refs)
		stale_info = set(prior.get("characterInfo", [])) - owned_info
		if stale_info:
			effect_text = cap110.drop_characterinfo_rows(effect_text, stale_info)
		write_atomically(effect_path, characters.skill_effect_section(effect_text.encode("utf-16"), info))
		summary = {"enabled": True, "npcRows": npc_rows,
			"characters": {c: int(r[1]) for c, r in refs.items()}, "skills": {r[3]: int(r[1]) for r in added},
			**spec.summarize_populations(populations, rows, claimed, refs, sub_enabled),
			"animationBases": sorted(set(prior.get("animationBases", [])) | {base for rows in selected.values() for base, _ in rows}),
			"characterInfo": sorted(owned_info | {r[0] for r in info}),
			"nameSymbols": sorted(({r[1] for r in characters.translation_rows(base_text) if len(r) > 1}
				| {r[1] for r in texts}) & {r[5] for r in refs.values()}),
			"sourcePopulationSha256": spec.source_sha256,
			"populationFileSha256": sro_pk2.digest((source_root / spec.population_name).read_bytes())}
		write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode())
		return summary


#============================================================================
# The variant-side merge (port-only, not native). The tile and object
# catalog machinery it remaps onto lives in build_alexandria_variant
# (the Alexandria merge owns it) and is imported lazily; the name-table
# helpers are alexandria_map_tables'. The population is add_monsters.
#============================================================================


# ================
# merge_region_tables_record
#
# The native regioncode.txt and textzonename.txt with one world's
# regions' rows from the v1.696 tables: a region code replaces the
# native placeholder of its id, and each region's zone names join by
# id and by code prefix, as the Alexandria map merge does. The code
# prefixes ride along for the reap, which must find the zone rows
# again in a tree it did not merge.
# ================
def merge_region_tables_record(native, later, region_ids):
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
	}, {"regionCodes": len(code_rows), "zoneNames": len(zone_rows)}, prefixes


# ================
# merge_region_tables
# ================
def merge_region_tables(native, later, region_ids):
	"""merge_region_tables_record without the reap's prefixes (hunting2's shape)."""
	merged, summary, _prefixes = merge_region_tables_record(native, later, region_ids)
	return merged, summary


# ================
# add_world
#
# An export-driven world's whole import: the pairing and pin checks,
# the pinned population export, the regions it covers, and the merge
# itself (merge_world). A spec with no export-driving shape refuses
# here: that world drives its own monster pass (see WorldImport).
# ================
def add_world(spec, native, out, v1696_root, source_root, sub_enabled, pins_for):
	if spec.regions_of is None:
		raise ValueError(f"{spec.label} import drives its own monster pass; add_world does not serve it")
	native, out = Path(native), Path(out)
	require_pairing(spec)
	v1696_root = Path(v1696_root or os.environ.get(spec.root_env, ""))
	if not str(v1696_root):
		raise SystemExit(f"Pass the v1.696 client folder or set {spec.root_env}.")
	source_root = source_root or os.environ.get(spec.source_env, "")
	if not source_root:
		raise SystemExit(f"{spec.source_env} names no population export folder")
	require_pin(spec, v1696_root, ("Map", "Media", "Data"), pins_for)
	export = read_population(spec, source_root)
	regions = spec.regions_of(export, sub_enabled)
	return merge_world(spec, native, out, v1696_root, regions,
		lambda: add_monsters(spec, v1696_root, native, out, source_root, True, sub_enabled, pins_for))


# ================
# merge_world
#
# One world's variant-side merge over a resolved region set: the
# regions' terrain remapped onto the merged tile catalog, their
# navmesh and availability bits, every Data file the native tree
# lacks, the regions' minimap tiles and names, and the monster
# population (build_monsters, the caller's own pass, run last).
#
# The variant root is reused across builds (see the header): the
# merge enrols what it writes in the marker's regionMerge record -
# every file it writes (unioned with the previous build's record, so
# a file an earlier build wrote and this one merely kept is still
# enrolled), the availability sectors, the region ids and the zone
# prefixes. The two catalogs are not enrolled: the Alexandria stage
# rebuilds tile2d.ifo and object.ifo from the native bytes on every
# build before this merge runs. The availability bits and the two
# name tables are undone from the record itself (reap_regions).
# ================
def merge_world(spec, native, out, v1696_root, regions, build_monsters):
	import build_alexandria_variant as variant
	native, out = Path(native), Path(out)
	v1696_root = Path(v1696_root)
	# The temple sets' named interiors are dungeon regions (bit 15):
	# no sector terrain exists for them - their geometry is the DOF
	# tree the Data pass below copies, published into the client's
	# dungeon catalogue by its own slice. Only outdoor regions take
	# sector files; the interiors take their authored nav data.
	local_regions = [region_id for region_id in regions if region_id & 0x8000]
	sectors = [tables.region_xy(region_id) for region_id in regions if not region_id & 0x8000]
	textdata = Path("Media_extracted") / tables.TEXTDATA
	marker = out / spec.marker
	prior = cap110.read_json(marker) if marker.exists() else {}
	prior_record = prior.get("regionMerge") or {}
	written = set(prior_record.get("files", []))

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
			raise SystemExit(f"Map.pk2 lacks terrain for {spec.region_label} regions: {missing_terrain}")
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
			target = out / "Map_extracted" / archive_path
			variant.replace_file(target, stored)
			written.add(target.relative_to(out).as_posix().lower())
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
				written.add(target.relative_to(out).as_posix().lower())
				textures += 1
	finally:
		map_data.close()
		map_handle.close()

	# Data: every file the native tree lacks (the regions' objects and
	# the monsters' models), the regions' navmesh, their availability.
	data_root = out / "Data_extracted"
	present = variant.lower_paths(data_root)
	data_added = variant.add_missing_files(
		v1696_root, "Data", data_root, present, ("navmesh/",)
	)
	written |= {"data_extracted/" + path for path in variant.lower_paths(data_root) - present}
	data_path, data_handle, data_data = tables.open_archive(v1696_root, "Data")
	try:
		data_index = tables.archive_index(data_data)
		nav_added = 0
		for sector_x, sector_y in sectors:
			nav = tables.navmesh_path(sector_x, sector_y)
			entry = data_index.get(nav)
			if entry is None:
				raise SystemExit(f"Data.pk2 has no {nav}; the {spec.region_label} regions are incomplete")
			target = out / "Data_extracted" / nav
			variant.replace_file(target, sro_pk2.payload(data_data, entry))
			written.add(target.relative_to(out).as_posix().lower())
			nav_added += 1
		for region_id in local_regions:
			nav = f"navmesh/ainavdata_{region_id}.dat"
			entry = data_index.get(nav)
			if entry is None:
				raise SystemExit(f"Data.pk2 has no {nav}; the {spec.region_label} regions are incomplete")
			target = out / "Data_extracted" / nav
			variant.replace_file(target, sro_pk2.payload(data_data, entry))
			written.add(target.relative_to(out).as_posix().lower())
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
				target = out / "Media_extracted" / name
				variant.replace_file(target, sro_pk2.payload(media_data, entry))
				written.add(target.relative_to(out).as_posix().lower())
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
	merged_tables, table_summary, prefixes = merge_region_tables_record(current_tables, later_tables, regions)
	for name, content in merged_tables.items():
		variant.replace_file(out / textdata / name, content)

	monsters = build_monsters()
	record = {
		"files": sorted(written),
		"sectors": sorted({tuple(sector) for sector in prior_record.get("sectors", [])} | set(sectors)),
		"regions": sorted({int(region) for region in prior_record.get("regions", [])} | set(regions)),
		"prefixes": sorted(set(prior_record.get("prefixes", [])) | set(prefixes)),
	}
	enrolled = cap110.read_json(marker)
	enrolled["regionMerge"] = record
	write_atomically(marker, (json.dumps(enrolled, indent=2) + "\n").encode())
	return {
		"enabled": True, "regions": len(regions), "localRegions": len(local_regions), "mapFilesAdded": len(terrains),
		"tileTexturesAdded": textures, "mapFilesMissing": sorted(map_missing),
		"dataFilesAdded": data_added, "navmeshAdded": nav_added, "minimapTiles": minimaps,
		**table_summary, "monsters": monsters
	}


# ================
# availability
#
# An availability file's bytes, checked for its signature; b"" when
# the file is absent (a native tree without one enables nothing).
# ================
def availability(variant, path):
	if not path.exists():
		return b""
	raw = path.read_bytes()
	if raw[:len(variant.MFO_SIGNATURE)] != variant.MFO_SIGNATURE:
		raise ValueError(f"{path} is not a JMXVMFO availability file")
	return raw


# ================
# reap_regions
#
# The region half of a switch off, from the marker's regionMerge
# record: the enrolled files are unlinked (the next build's link
# stage re-links anything the native tree owns at those paths), the
# availability bits the merge set are cleared, and the two name
# tables are rebuilt without the import's rows - the region ids'
# native placeholder rows restored from the native tree's tables,
# the zone rows found by id and by the recorded code prefixes. A
# table whose native counterpart is missing is left alone: without
# it the import's rows cannot be told from native ones.
# ================
def reap_regions(spec, native, out, record):
	for relative in record.get("files", []):
		target = (out / relative).resolve()
		# The marker is the build's own file, but a path it names is
		# still checked: a reap never deletes outside the overlay.
		if out not in target.parents:
			raise ValueError(f"{spec.label} enrolled file {relative!r} is outside the overlay")
		target.unlink(missing_ok=True)
	sectors = [tuple(sector) for sector in record.get("sectors", [])]
	if sectors:
		import build_alexandria_variant as variant
		mapinfo = out / "Data_extracted" / "navmesh" / "mapinfo.mfo"
		if mapinfo.exists():
			native_bits = availability(variant, native / "Data_extracted" / "navmesh" / "mapinfo.mfo")
			data = bytearray(availability(variant, mapinfo))
			for sector_x, sector_y in sectors:
				region_id = (sector_y << 8) | sector_x
				at, bit = variant.MFO_HEADER_BYTES + (region_id >> 3), 0x80 >> (region_id & 7)
				# A sector the native tree already enables stays enabled:
				# the merge set it again, it did not create it.
				if native_bits and at < len(native_bits) and native_bits[at] & bit:
					continue
				data[at] &= ~bit
			write_atomically(mapinfo, bytes(data))
	ids = {str(region_id) for region_id in record.get("regions", [])}
	prefixes = tuple(record.get("prefixes", []))
	out_tables = out / TEXT_ROOT
	native_tables = native / TEXT_ROOT
	regioncode = out_tables / "regioncode.txt"
	native_regioncode = native_tables / "regioncode.txt"
	if ids and regioncode.exists() and native_regioncode.exists():
		kept = [row for row in tables.table_rows(regioncode.read_bytes()) if len(row) < 2 or row[1] not in ids]
		kept_ids = {row[1] for row in kept if len(row) > 1}
		restored = [row for row in tables.table_rows(native_regioncode.read_bytes())
			if len(row) > 1 and row[1] in ids and row[1] not in kept_ids]
		write_atomically(regioncode, b"\xff\xfe" + table_text(kept + restored).encode("utf-16-le"))
	zonename = out_tables / "textzonename.txt"
	native_zonename = native_tables / "textzonename.txt"
	if zonename.exists() and native_zonename.exists():
		native_keys = {row[1] for row in tables.table_rows(native_zonename.read_bytes()) if len(row) > 1}
		kept = [row for row in tables.table_rows(zonename.read_bytes())
			if not (len(row) > 1 and (row[1] in ids or row[1].startswith(prefixes)) and row[1] not in native_keys)]
		write_atomically(zonename, b"\xff\xfe" + table_text(kept).encode("utf-16-le"))


# ================
# reap_world
#
# A switch off for the whole import: the region merge's enrolment is
# undone first (it reads the marker), then the monster pass's, which
# unlinks the marker itself. No archive is read.
# ================
def reap_world(spec, native, out):
	native, out = Path(native).resolve(), Path(out).resolve()
	if native == out or native in out.parents or out in native.parents:
		raise ValueError(f"{spec.label} overlay must be separate from native extraction")
	marker = out / spec.marker
	prior = cap110.read_json(marker) if marker.exists() else {}
	record = prior.get("regionMerge")
	if record:
		reap_regions(spec, native, out, record)
	reap_monsters(spec, out, prior)
