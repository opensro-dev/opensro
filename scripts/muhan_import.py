"""
===========================================================================

muhan_import.py - optional Muhan, the Battlefield of Infinity.

Port-only, not native. Off unless SRO_MUHAN is "1", "on" or "true".

Two later sources, both the operator's own and never committed:

	the v1.696 client   SRO_V1696_ROOT, hash-pinned before anything is
	                    read (v1696_archive_pins.json beside this tool).
	                    It carries the MOB_MUHAN_* character and skill
	                    rows the waves name by level.
	the population      SRO_MUHAN_DATABASE names a folder holding
	export              muhan-population.json: the eight wave worlds,
	                    their 160 authored wave rows and the two wave
	                    regions, extracted from the later server's
	                    shard backup. The export records the backup's
	                    sha256; only the pinned backup's export is
	                    admitted.

Muhan is not a nest import: the source authors no monster nests for
these worlds at all. Its wave table (_RefMuhanMonsterWave) names a
world, a sequence, a point, a delay, a count, a level offset, a
radius and a monster type - never a kind. The kinds are the v1.696
client's level-stamped MOB_MUHAN_* clones (one combat kind per level,
grade 0); a wave spawns the kind at its world's band level plus the
wave's offset (the band rides the world codename: GROUP_MUHAN_*_75
spawns levels 72-80). That selection rule is inference - no binary
this port may cite implements the system - recorded here and in
docs/features/MUHAN.md, as the scope rule requires. The admitted
kinds are converted to the v1.150 layout in a character shard of
their own, their skills merged the cap-110 way, and the wave rows
written as a Version-2 file (muhan-waves.txt) the server's wave
director reads behind the same flag. add_muhan (below) is the
variant-side merge of the two wave regions (terrain, navmesh, names
and Data files) on the Alexandria merge's catalog machinery. A
switch off removes exactly the rows and files the marker records.
The entry slice (#3314) admits the manager and change NPCs (24
export nests) into the same character shard and writes them as
muhan-npcs.txt, so the arenas have a gatekeeper to talk to. The
transform masks' monster forms (MOB_MUHAN_CHANGE_*) stay out of the
shard: the wave director must never resolve one as a wave kind, and
admitting them waits for the change interaction itself.

The region merge and the reap are later_world_import's
(merge_world / reap_world, #3326); this module is the Muhan spec
and the wave shapes that are truly its own: the banded worlds, the
level admission, the wave and NPC projections.

===========================================================================
"""
import json
import mmap
import os
from pathlib import Path

import alexandria_npcs as characters
import cap110_monsters as cap110
import cap110_skill_effects as effects
import import_vsro_alexandria as tables
import jupiter_import as jupiter
import later_world_import
import sro_pk2
from later_world_import import later_object_text, later_text_row
from prepare_client_resources import write_atomically

FLAG = "SRO_MUHAN"
ROOT_ENV = "SRO_V1696_ROOT"
SOURCE_ENV = "SRO_MUHAN_DATABASE"
MARKER = ".opensro-muhan.json"
# The 200000 identity band: 120000 is the cap-110 family's, 130000 the
# pet import's (140000/150000 held for its later sources), 160000 the
# Sky Temple's, 170000 Jupiter's, 180000 Hunting Ground 2's, 190000
# the GOD-era temples'. One importer owns one shard file in a build.
CHARACTER_SHARD = "characterdata_200000.txt"
SKILL_SHARD = "skilldata_200000.txt"
WAVES_FILE = "muhan-waves.txt"
NPCS_FILE = "muhan-npcs.txt"
NPC_PREFIX = "NPC_MUHAN_"
POPULATION_NAME = "muhan-population.json"
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA
PIN_FILE = Path(__file__).with_name("v1696_archive_pins.json")

# The later server's shard backup the population export was extracted
# from (research/investigations/later-sources-3247, extract_muhan_population.py).
# An export made from another backup is refused before anything is staged.
POPULATION_SOURCE_SHA256 = "89c6835f8e51b1a75950242c89cf3ad639ea7d50a22191a90632cca333664a45"

# The eight banded wave worlds; 99-102 are unbound shells with no
# wave rows and stay unregistered (docs/features/MUHAN.md).
WAVE_WORLDS = (103, 104, 105, 106, 108, 109, 110, 111)
WAVES_PER_WORLD = 20
PREFIX = "MOB_MUHAN_"
CHANGE_PREFIX = "MOB_MUHAN_CHANGE_"
GRADE = 0


#============================================================================
# Flags
#============================================================================


# ================
# flag_enabled
# ================
def flag_enabled(env=os.environ):
	"""True only when SRO_MUHAN names an on value; unset is native."""
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON


#============================================================================
# Population export
#============================================================================


# ================
# world_band
#
# The band level a wave world's codename carries (GROUP_MUHAN_*_75).
# ================
def world_band(code):
	for band in (75, 85, 95, 105):
		if code.endswith("_" + str(band)):
			return band
	raise ValueError("muhan wave world names no band level: " + code)


# ================
# read_population
#
# The population export: the shared read (its recorded backup hash
# must be the pinned one, the eight wave worlds all present), then
# Muhan's own contract - every wave world banded and region-bound,
# and carrying its full sequence of waves.
# ================
def read_population(source_root):
	export = later_world_import.read_population(SPEC, source_root)
	path = Path(source_root) / POPULATION_NAME
	worlds = export.get("worlds") or {}
	for world in WAVE_WORLDS:
		row = worlds.get(str(world))
		if row is None:
			raise ValueError(f"population export {path} has no wave world {world}")
		world_band(row["CodeName"])
		if not row.get("Regions"):
			raise ValueError(f"population export {path} binds no region to wave world {world}")
	waves = export.get("waves") or []
	sequences = {}
	for wave in waves:
		sequences.setdefault(wave["WorldID"], []).append(wave["Seq"])
	for world in WAVE_WORLDS:
		if sorted(sequences.get(world, [])) != list(range(1, WAVES_PER_WORLD + 1)):
			raise ValueError(f"population export {path} does not carry waves 1-{WAVES_PER_WORLD} for world {world}")
	return export


# ================
# population_regions
#
# The region ids the wave worlds bind: the build covers exactly these.
# ================
def population_regions(export):
	ids = set()
	for world in WAVE_WORLDS:
		ids |= set(export["worlds"][str(world)]["Regions"])
	return sorted(ids)


# ================
# needed_levels
#
# Every level a wave can name: each world's band plus each of its
# waves' offsets. The import admits exactly the kinds at these levels.
# ================
def needed_levels(export):
	worlds = export["worlds"]
	levels = set()
	for wave in export["waves"]:
		if wave["WorldID"] in WAVE_WORLDS:
			levels.add(world_band(worlds[str(wave["WorldID"])]["CodeName"]) + wave["LevelOffset"])
	return levels


# ================
# project_waves
#
# The wave rows in the Version-2 schema the server reads, sorted by
# world then sequence. A wave outside its world's bound regions, or
# with no monsters, is a data defect and refuses the build.
# ================
def project_waves(export):
	worlds = export["worlds"]
	projected = []
	for wave in export["waves"]:
		if wave["WorldID"] not in WAVE_WORLDS:
			continue
		if wave["RegionID"] not in worlds[str(wave["WorldID"])]["Regions"]:
			raise ValueError(
				f"muhan wave {wave['WorldID']}/{wave['Seq']} stands in region {wave['RegionID']}, "
				"which its world does not bind"
			)
		if wave["Count"] < 1:
			raise ValueError(f"muhan wave {wave['WorldID']}/{wave['Seq']} spawns nothing")
		projected.append({
			"WorldID": wave["WorldID"], "Seq": wave["Seq"], "RegionID": wave["RegionID"],
			"X": wave["X"], "Y": wave["Y"], "Z": wave["Z"], "DelayMs": wave["DelayMs"],
			"Count": wave["Count"], "LevelOffset": wave["LevelOffset"],
			"Radius": wave["Radius"], "MonsterType": wave["MonsterType"],
		})
	projected.sort(key=lambda row: (row["WorldID"], row["Seq"]))
	return {"Version": 2, "Rows": projected}


# ================
# project_npcs
#
# The arena NPCs (managers and change NPCs) in the Version-2 nest
# schema the server's population join reads (cap110Nest). Every row
# must stand in one of its world's bound regions; a nest naming
# another world or region is a data defect and refuses the build.
# ================
def project_npcs(export):
	worlds = export["worlds"]
	projected = []
	for nest in export.get("npcNests") or []:
		world = nest["WorldID"]
		if world not in WAVE_WORLDS:
			raise ValueError(f"muhan npc nest {nest['NestID']} stands in non-wave world {world}")
		if nest["RegionID"] not in worlds[str(world)]["Regions"]:
			raise ValueError(
				f"muhan npc nest {nest['NestID']} stands in region {nest['RegionID']}, "
				"which its world does not bind"
			)
		code = nest["Codename"]
		if not code.startswith(NPC_PREFIX):
			raise ValueError(f"muhan npc nest {nest['NestID']} names non-muhan npc {code}")
		projected.append({
			"Codename": code, "NestID": nest["NestID"], "WorldID": world,
			"RegionID": nest["RegionID"], "X": nest["X"], "Y": nest["Y"], "Z": nest["Z"],
			"Radius": nest["Radius"], "GenerateRadius": nest["GenerateRadius"],
			"MaxCount": nest["MaxCount"], "RespawnDelayMinSec": nest["RespawnDelayMinSec"],
			"RespawnDelayMaxSec": nest["RespawnDelayMaxSec"], "Respawn": bool(nest["Respawn"]),
			"InitialDir": nest["InitialDir"], "SpawnType": nest["SpawnType"],
			"ChampionGenPercentage": nest["ChampionGenPercentage"],
			"Controls": nest["Controls"], "Champion": nest["Champion"],
		})
	projected.sort(key=lambda row: row["NestID"])
	return {"Version": 2, "Rows": projected}


# ================
# family_admits
#
# One later character row against the family's admission: a combat
# MOB_MUHAN_* kind (the MOB_MUHAN_CHANGE_* transform forms stay out),
# grade 0 (row 15), at a level (row 57) some wave can name; or one of
# the arena NPCs (NPC_MUHAN_*), which carry no wave level and are
# admitted on codename alone (#3314).
# ================
def family_admits(code, row, levels):
	if code.startswith(NPC_PREFIX):
		return True
	if not code.startswith(PREFIX) or code.startswith(CHANGE_PREFIX):
		return False
	return int(row[15]) == GRADE and int(row[57]) in levels


SPEC = later_world_import.WorldImport(
	label="muhan",
	flag=FLAG,
	root_env=ROOT_ENV,
	character_shard=CHARACTER_SHARD,
	skill_shard=SKILL_SHARD,
	marker=MARKER,
	pin_file=PIN_FILE,
	pin_sentence="Muhan is built from one pinned v1.696 client; its archives are never committed.",
	region_label="Muhan",
	population_files=(("waves", WAVES_FILE), ("npcs", NPCS_FILE)),
	source_env=SOURCE_ENV,
	population_name=POPULATION_NAME,
	source_sha256=POPULATION_SOURCE_SHA256,
	worlds_required=WAVE_WORLDS,
)


#============================================================================
# The engine, under this import's names
#============================================================================


# ================
# archive_pins
#
# The family's one pin reader is jupiter_import's (one pin file, one
# process-wide verified set); this name exists so the pin check
# below reads through it.
# ================
def archive_pins():
	"""The pinned v1.696 archive hashes (v1696_archive_pins.json beside this tool)."""
	return jupiter.archive_pins()


# ================
# require_v1696_pin
# ================
def require_v1696_pin(root, names):
	"""The v1.696 archives against their pins, before any of them is read."""
	return later_world_import.require_pin(SPEC, root, names, archive_pins)


#============================================================================
# The monster import (Muhan's own: waves name kinds by level)
#============================================================================


# ================
# add_monsters
# ================
def add_monsters(v1696_root, native, output, source_root=None):
	native, output = Path(native).resolve(), Path(output).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("muhan overlay must be separate from native extraction")
	marker = output / MARKER
	prior = cap110.read_json(marker) if marker.exists() else {}
	text_root = output / TEXT_ROOT
	if not flag_enabled():
		later_world_import.reap_monsters(SPEC, output, prior)
		return {"enabled": False}
	if not v1696_root:
		raise SystemExit(f"Pass the v1.696 client folder or set {ROOT_ENV}.")
	require_v1696_pin(v1696_root, ("Media",))
	source_root = Path(source_root or os.environ[SOURCE_ENV])
	export = read_population(source_root)
	levels = needed_levels(export)
	waves = project_waves(export)
	with open(Path(v1696_root) / "Media.pk2", "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as media:
		index = tables.archive_index(media)

		# ================
		# read
		# ================
		def read(name):
			return tables.table_text(media, index, name.lower())

		later = {row[2]: row for shard in tables.table_rows(read("characterdata.txt")) for row in tables.table_rows(read(shard[0]))}
		refs = {}
		by_level = {}
		for code in sorted(later):
			if family_admits(code, later[code], levels):
				refs[code] = characters.convert_character(later[code])
				if not code.startswith(NPC_PREFIX):
					by_level.setdefault(int(later[code][57]), []).append(code)
		# The wave selection rule names a kind by level alone, so every
		# level a wave can name must resolve to exactly one kind.
		for level in sorted(levels):
			codes = by_level.get(level, [])
			if len(codes) != 1:
				raise ValueError(f"muhan level {level} resolves to {len(codes)} kinds {codes}, want exactly one")
		# Every arena NPC's nest must name an admitted character row:
		# a nest without a kind can never materialize, so a mismatch
		# between the export and the client refuses the build here.
		npcs = project_npcs(export)
		for row in npcs["Rows"]:
			if row["Codename"] not in refs:
				raise ValueError(f"muhan npc nest {row['NestID']} names unadmitted character {row['Codename']}")
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
		# Sorted by identity: the codename set has no stable order, and a
		# rebuild should write the same bytes.
		write_atomically(text_root / CHARACTER_SHARD, cap110.encode_rows(sorted(refs.values(), key=lambda row: int(row[1]))))
		write_atomically(text_root / SKILL_SHARD, cap110.encode_rows(added))
		manifest = tables.table_rows((text_root / "skilldata.txt").read_bytes())
		manifest = [r for r in manifest if r[0].lower() != SKILL_SHARD] + [[SKILL_SHARD]]
		write_atomically(text_root / "skilldata.txt", cap110.encode_rows(manifest))
		write_atomically(text_root / WAVES_FILE, (json.dumps(waves) + "\n").encode())
		write_atomically(text_root / NPCS_FILE, (json.dumps(npcs) + "\n").encode())
		base_text = (text_root / "textdataname.txt").read_bytes()
		known = {r[1] for r in characters.translation_rows(base_text) if len(r) > 1}
		wanted = {r[5] for r in refs.values()} - known
		object_raw = b"\xff\xfe" + later_object_text(read).encode("utf-16-le")
		texts = [[r[0], r[1], r[2], "", "", r[5], r[4], r[7], r[8], r[9], ""] for r in
			(later_text_row(row) for row in characters.translation_rows(object_raw)) if len(r) >= 10 and r[1] in wanted]
		if wanted - {r[1] for r in texts}:
			raise ValueError("missing muhan monster names")
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
			"levels": sorted(levels),
			"waves": {str(world): sum(1 for row in waves["Rows"] if row["WorldID"] == world) for world in WAVE_WORLDS},
			"npcNests": len(npcs["Rows"]),
			"npcRows": len(export.get("npcNests") or []),
			"transMaskRows": len(export.get("transMask") or []),
			"animationBases": sorted(set(prior.get("animationBases", [])) | {base for rows in selected.values() for base, _ in rows}),
			"characterInfo": sorted(owned_info | {r[0] for r in info}),
			"nameSymbols": sorted(({r[1] for r in characters.translation_rows(base_text) if len(r) > 1}
				| {r[1] for r in texts}) & {r[5] for r in refs.values()}),
			"sourcePopulationSha256": POPULATION_SOURCE_SHA256,
			"populationFileSha256": sro_pk2.digest((source_root / POPULATION_NAME).read_bytes())}
		write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode())
		return summary

#============================================================================
# The variant-side merge is later_world_import's (merge_world, on the
# Alexandria merge's catalog machinery); the population is add_monsters.
#============================================================================


# ================
# merge_muhan_region_tables
# ================
def merge_muhan_region_tables(native, later, region_ids):
	"""
	The native regioncode.txt and textzonename.txt with the Muhan
	regions' rows from the v1.696 tables: a region code replaces the
	native placeholder of its id, and each region's zone names join by
	id and by code prefix, as the Alexandria map merge does.
	"""
	merged, summary, _prefixes = later_world_import.merge_region_tables_record(native, later, region_ids)
	return merged, summary


# ================
# add_muhan
# ================
def add_muhan(native, out, v1696_root=None, source_root=None):
	"""
	Merge Muhan, the Battlefield of Infinity, from the pinned v1.696
	client into the variant: the two wave regions' terrain remapped
	onto the merged tile catalog, their navmesh and availability
	bits, every Data file the native tree lacks, the regions' minimap
	tiles and names, and the monster population (later_world_import's
	merge_world; the wave regions are outdoor halls, so its dungeon-
	interior split is a guard here). Port-only, not native; off,
	exactly what the marker records is reaped - monster and region
	merge alike - and no archive is read.
	"""
	if not flag_enabled():
		later_world_import.reap_world(SPEC, native, out)
		return {"enabled": False}
	v1696_root = Path(v1696_root or os.environ.get(ROOT_ENV, ""))
	if not str(v1696_root):
		raise SystemExit(f"Pass the v1.696 client folder or set {ROOT_ENV}.")
	source_root = source_root or os.environ.get(SOURCE_ENV, "")
	if not source_root:
		raise SystemExit(f"{SOURCE_ENV} names no population export folder")
	require_v1696_pin(v1696_root, ("Map", "Media", "Data"))
	export = read_population(source_root)
	regions = population_regions(export)
	return later_world_import.merge_world(SPEC, native, out, v1696_root, regions,
		lambda: add_monsters(v1696_root, native, out, source_root))


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
