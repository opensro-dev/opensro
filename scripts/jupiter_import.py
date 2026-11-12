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
the rows and files the marker records, monster and region-merge alike. No authored drops: the monsters
keep the native loot posture until their drop tables are imported.

The machinery is later_world_import's; this module is the Jupiter
spec (its flags, shards and worlds) and the shape functions that are
truly Jupiter's: the field/temple claim split, the admission window,
the two population files and the family summary.

===========================================================================
"""
import json
import os
from pathlib import Path

import import_vsro_alexandria as tables
import later_world_import

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
TEXT_ROOT = later_world_import.TEXT_ROOT

PIN_FILE = Path(__file__).with_name("v1696_archive_pins.json")
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

# The engine's process-wide set of verified archives, under this
# module's name: muhan and hunting2 read their pins through this
# module's archive_pins too.
_verified_archives = later_world_import._verified_archives

# The later name-table readers, re-exported from the engine: this
# module's suite pins them under this import's name.
later_object_text = later_world_import.later_object_text
later_text_row = later_world_import.later_text_row


#============================================================================
# Flags
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


#============================================================================
# The Jupiter shape: admission, claims, populations, summary, regions
#============================================================================


# The one guest the source places in the field beside the family: an
# Arabia kind at the window's own top level and a family grade. Like
# the cap-110 field's Apis, it is admitted by name, on the same window.
GUEST_CODE = "MOB_ARABIA_KIDEMONAS"


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
# SPEC shape functions
#
# claim_groups splits the export's kinds into the field family and,
# while the temple switch is on, the temple family; admit is the one
# admission window for both; build_populations writes one Version-2
# file per family; summarize_populations reports per family.
# ================
def _claim_groups(rows, sub_enabled):
	return {
		"field": {row["Codename"] for row in rows if row["WorldID"] == FIELD_WORLD},
		"temple": {row["Codename"] for row in rows if row["WorldID"] in TEMPLE_WORLDS} if sub_enabled else set(),
	}


def _admit(code, row, families):
	return family_admits(code, row)


def _build_populations(rows, sub_enabled):
	populations = {"field": project_population(rows, (FIELD_WORLD,))}
	if sub_enabled:
		populations["temple"] = project_population(rows, TEMPLE_WORLDS)
	return populations


def _summarize_populations(populations, rows, claimed, refs, sub_enabled):
	return {"temple": sub_enabled,
		"families": {name: {"nests": len(population["Rows"]),
			"regions": sorted({row["RegionID"] for row in population["Rows"]})}
			for name, population in populations.items()}}


def _regions_of(export, sub_enabled):
	return population_regions(export, sub_enabled)


SPEC = later_world_import.WorldImport(
	label="jupiter",
	flag=FLAG,
	requires=None,
	root_env=ROOT_ENV,
	source_env=SOURCE_ENV,
	population_name=POPULATION_NAME,
	source_sha256=POPULATION_SOURCE_SHA256,
	character_shard=CHARACTER_SHARD,
	skill_shard=SKILL_SHARD,
	marker=MARKER,
	pin_file=PIN_FILE,
	pin_sentence="Jupiter is built from one pinned v1.696 client; its archives are never committed.",
	region_label="Jupiter",
	worlds_required=(FIELD_WORLD, *TEMPLE_WORLDS),
	population_files=(("field", FIELD_FILE), ("temple", TEMPLE_FILE)),
	info_prefixes=(PREFIX, GUEST_CODE),
	claim_groups=_claim_groups,
	admit=_admit,
	build_populations=_build_populations,
	summarize_populations=_summarize_populations,
	regions_of=_regions_of,
)


#============================================================================
# The engine, under this import's names
#============================================================================


# ================
# archive_pins
# ================
def archive_pins():
	"""The pinned v1.696 archive hashes (v1696_archive_pins.json beside this tool)."""
	return later_world_import.archive_pins(SPEC)


# ================
# require_v1696_pin
# ================
def require_v1696_pin(root, names):
	"""The v1.696 archives against their pins, before any of them is read."""
	return later_world_import.require_pin(SPEC, root, names, archive_pins)


# ================
# read_population
# ================
def read_population(source_root):
	"""The pinned population export, all seven worlds present."""
	return later_world_import.read_population(SPEC, source_root)


# ================
# project_population
# ================
def project_population(rows, worlds):
	"""The nest rows of the given worlds in the Version-2 row schema."""
	return later_world_import.project_population(SPEC, rows, worlds)


# ================
# add_monsters
#
# The temple sub-flag without the field flag refuses before anything
# is reaped or read, as it always has.
# ================
def add_monsters(v1696_root, native, output, source_root=None):
	field, temple = flag_enabled(), temple_enabled()
	if temple and not field:
		raise ValueError(f"{TEMPLE_FLAG} requires {FLAG}")
	return later_world_import.add_monsters(SPEC, v1696_root, native, output, source_root, field, temple, archive_pins)


# ================
# merge_jupiter_region_tables
# ================
def merge_jupiter_region_tables(native, later, region_ids):
	"""The native region tables with the Jupiter regions' later rows merged."""
	return later_world_import.merge_region_tables(native, later, region_ids)


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
	monster population. Port-only, not native; off, the monster and
	region-merge outputs a previous build enrolled are reaped and no
	archive is read.
	"""
	if not flag_enabled():
		if temple_enabled():
			raise ValueError(f"{TEMPLE_FLAG} requires {FLAG}")
		later_world_import.reap_world(SPEC, native, out)
		return {"enabled": False}
	return later_world_import.add_world(SPEC, native, out, v1696_root, source_root, temple_enabled(), archive_pins)


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
