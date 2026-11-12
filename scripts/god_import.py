"""
===========================================================================

god_import.py - the GOD-era temple worlds: Togui, Flame Mountain, Shipwreck.

Port-only, not native. The 22 worlds (ids 63-84) are the port's temple
bands (apps/server/internal/game/world/temple): instanced, layered
worlds the server registers behind SRO_CAP110_TEMPLE, a switch that
requires SRO_CAP110_MONSTERS at build and at load alike. This import
builds what the switch publishes, from two later sources, both the
operator's own and never committed:

	the kinds   the MOB_GOD_* character and skill rows of the v1.696
	            client (SRO_V1696_ROOT, hash-pinned before anything is
	            read), converted to the v1.150 layout in a character
	            shard of their own. Admission is per world at its
	            authored level window - Togui 36-68, Flame Mountain
	            71-89, the Shipwreck 91-109 (the pinned bands of
	            research/investigations/later-sources-3247) - and at the
	            authored grades.
	the nests   cap110-temple.txt, the Version-2 population file the
	            server already reads behind the same switch, projected
	            from the pinned population export: SRO_GOD_DATABASE
	            names the folder holding god-population.json, the
	            authored nests, tactics and hive facts of worlds 63-84
	            extracted from the later server's shard backup. The
	            export records the backup's sha256; only the pinned
	            backup's export is admitted.
	the regions the worlds' 497 regions, merged from the same v1.696
	            client by add_god below: terrain, navmesh, names and
	            Data files, on the Alexandria merge's catalog machinery.

The export's NPC rows (the worlds' instance and field attendants) stay
out of every population: roster content for the NPC slice, not
monsters. No authored drops: the monsters keep the native loot posture
until their drop tables are imported. The switch off removes exactly
the rows and files the marker records, monster and region-merge
alike. Entry and the bands' driving rules are the admission slice's
(#1800 step 3 slice 4), scoped in docs/features/GOD.md.

The machinery is later_world_import's; this module is the GOD spec
(its flags, shards and worlds) and the shape functions that are truly
the GOD worlds': the three areas' claim split and admission windows,
the one population file and the per-area summary.

===========================================================================
"""
import json
import os
from pathlib import Path

import cap110_monsters as cap110
import import_vsro_alexandria as tables
import later_world_import

ROOT_ENV = "SRO_V1696_ROOT"
SOURCE_ENV = "SRO_GOD_DATABASE"
TEMPLE_FLAG = "SRO_CAP110_TEMPLE"
POPULATION_FILE = "cap110-temple.txt"
MARKER = ".opensro-god.json"
# The 190000 identity band: 120000 is the cap-110 family's, 130000 the
# pet import's (140000/150000 held for its later sources), 160000 the
# Sky Temple's, 170000 Jupiter's, 180000 the second hunting grounds'.
# One importer owns one shard file in a build.
CHARACTER_SHARD = "characterdata_190000.txt"
SKILL_SHARD = "skilldata_190000.txt"
POPULATION_NAME = "god-population.json"
TEXT_ROOT = later_world_import.TEXT_ROOT

PIN_FILE = Path(__file__).with_name("v1696_archive_pins.json")
# The later server's shard backup the population export was extracted
# from (research/investigations/later-sources-3247, extract_god_population.py).
# An export made from another backup is refused before anything is staged.
POPULATION_SOURCE_SHA256 = "89c6835f8e51b1a75950242c89cf3ad639ea7d50a22191a90632cca333664a45"

GRADES = (0, 1, 3, 6, 8)
# The three worlds of the GOD era, as admission families: the source
# worlds whose nests claim species for the family, the family's
# codename prefix and its authored level window. The worlds themselves
# are the source's tiered copies (levels climb by copy); all 22 are
# temple bands in the port whatever the export's world type says.
AREAS = {
	"togui": {"worlds": (63, 64, 65, 66, 67, 68), "prefix": "MOB_GOD_TOGUI_",
		"first_level": 36, "last_level": 68},
	"flame": {"worlds": (69, 70, 71, 72, 73, 74, 75, 76), "prefix": "MOB_GOD_FLAME_",
		"first_level": 71, "last_level": 89},
	"wreck": {"worlds": (77, 78, 79, 80, 81, 82, 83, 84), "prefix": "MOB_GOD_GHOST_",
		"first_level": 91, "last_level": 109},
}
ALL_WORLDS = tuple(world for area in AREAS.values() for world in area["worlds"])

# The engine's process-wide set of verified archives, under this
# module's name.
_verified_archives = later_world_import._verified_archives

# The later name-table readers, re-exported under this import's names.
later_object_text = later_world_import.later_object_text
later_text_row = later_world_import.later_text_row


#============================================================================
# Flags and pairing
#============================================================================


# ================
# temple_enabled / monsters_enabled
#
# The same on-values the server reads (monster.TempleEnabled and its
# cap-110 pairing): unset is the native state.
# ================
def temple_enabled(env=os.environ):
	"""True only when SRO_CAP110_TEMPLE names an on value."""
	return env.get(TEMPLE_FLAG, "").strip().lower() in tables.FLAG_ON


def monsters_enabled(env=os.environ):
	"""True only when SRO_CAP110_MONSTERS names an on value."""
	return env.get(cap110.FLAG, "").strip().lower() in tables.FLAG_ON


# ================
# require_temple_pairing
#
# The build refuses the temple switch without the cap-110 monster
# switch before anything is read, as the server refuses it at load
# (monster.AppendTemple): the kinds join the cap-110 template the
# pairing builds.
# ================
def require_temple_pairing(env=os.environ):
	later_world_import.require_pairing(SPEC, env)


#============================================================================
# The GOD shape: admission, claims, population, summary, regions
#============================================================================


# ================
# family_admits
#
# One later character row against one world's admission window: its
# codename carries the world's prefix, its level (row 57) sits inside
# the world's band and its grade (row 15) is one of the authored
# grades.
# ================
def family_admits(area, code, row):
	spec = AREAS[area]
	if not code.startswith(spec["prefix"]):
		return False
	return spec["first_level"] <= int(row[57]) <= spec["last_level"] and int(row[15]) in GRADES


# ================
# population_regions
#
# The region ids the build covers: every temple world's.
# ================
def population_regions(export):
	worlds = export["worlds"]
	ids = set()
	for world in ALL_WORLDS:
		ids |= set(worlds[str(world)]["Regions"])
	return sorted(ids)


# ================
# SPEC shape functions
#
# claim_groups splits the export's kinds by the area their world
# belongs to; admit is any claiming area's window; build_populations
# writes the one Version-2 file for all 22 worlds;
# summarize_populations reports per area.
# ================
def _claim_groups(rows, sub_enabled):
	return {
		name: {row["Codename"] for row in rows if row["WorldID"] in AREAS[name]["worlds"]}
		for name in AREAS
	}


def _admit(code, row, families):
	return any(family_admits(family, code, row) for family in families)


def _build_populations(rows, sub_enabled):
	return {"all": project_population(rows, ALL_WORLDS)}


def _summarize_populations(populations, rows, claimed, refs, sub_enabled):
	population = populations["all"]
	return {"nests": len(population["Rows"]),
		"regions": sorted({row["RegionID"] for row in population["Rows"]}),
		"areas": {name: {"kinds": len(claimed[name] & set(refs)),
			"nests": sum(1 for row in population["Rows"] if row["WorldID"] in AREAS[name]["worlds"])}
			for name in AREAS}}


def _regions_of(export, sub_enabled):
	return population_regions(export)


SPEC = later_world_import.WorldImport(
	label="god",
	flag=TEMPLE_FLAG,
	requires=cap110.FLAG,
	root_env=ROOT_ENV,
	source_env=SOURCE_ENV,
	population_name=POPULATION_NAME,
	source_sha256=POPULATION_SOURCE_SHA256,
	character_shard=CHARACTER_SHARD,
	skill_shard=SKILL_SHARD,
	marker=MARKER,
	pin_file=PIN_FILE,
	pin_sentence="The GOD worlds are built from one pinned v1.696 client; its archives are never committed.",
	region_label="GOD",
	worlds_required=ALL_WORLDS,
	population_files=(("all", POPULATION_FILE),),
	info_prefixes=tuple(area["prefix"] for area in AREAS.values()),
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
	"""The pinned population export, all 22 worlds present."""
	return later_world_import.read_population(SPEC, source_root)


# ================
# project_population
# ================
def project_population(rows, worlds):
	"""The nest rows of the given worlds in the Version-2 row schema."""
	return later_world_import.project_population(SPEC, rows, worlds)


# ================
# add_monsters
# ================
def add_monsters(v1696_root, native, output, source_root=None):
	return later_world_import.add_monsters(SPEC, v1696_root, native, output, source_root, temple_enabled(), False, archive_pins)


# ================
# merge_god_region_tables
# ================
def merge_god_region_tables(native, later, region_ids):
	"""The native region tables with the GOD regions' later rows merged."""
	return later_world_import.merge_region_tables(native, later, region_ids)


# ================
# add_god
# ================
def add_god(native, out, v1696_root=None, source_root=None):
	"""
	Merge the GOD temple worlds (Togui Village, Flame Mountain, the
	Shipwreck) from the pinned v1.696 client into the variant: the
	regions' terrain remapped onto the merged tile catalog, their
	navmesh and availability bits, every Data file the native tree
	lacks, the regions' minimap tiles and names, and the monster
	population. Port-only, not native; the temple switch off, the
	monster and region-merge outputs a previous build enrolled are
	reaped and no archive is read. The pairing refusal comes first,
	before any archive.
	"""
	if not temple_enabled():
		later_world_import.reap_world(SPEC, native, out)
		return {"enabled": False}
	return later_world_import.add_world(SPEC, native, out, v1696_root, source_root, False, archive_pins)


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
