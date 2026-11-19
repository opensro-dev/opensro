"""
===========================================================================

styria_import.py - optional Styria Clash arena map (port-only).

Port-only, not native. Off unless SRO_STYRIA_CLASH is "1", "on" or "true".

The Styria Clash is the later client's job arena: Hunters and Thieves
fight for treasure in world 107 (INS_ARENA_JOB_WAR, GROUP_ARENA_JOB_WAR).
Its ground is four outdoor regions the v1.696 client authors under the
region code RN_ARENA_JOB_WAR (31985, 31986, 32241, 32242: sectors x
241-242, y 124-125), with their terrain, navmesh and loading screens.

This is #3272's first slice: the map alone. It merges the four regions'
terrain, navmesh, region codes and Data files into the variant through the
later-world engine (later_world_import.merge_world), exactly as the
second hunting grounds do. The arena carries no population of its own:
the client's npcpos places nothing there and its two event kinds
(MOB_EV_ARENA_JOB_THIEF / _HUNTER) belong to the match rules. So the
monster pass writes only its marker. The rules - registration, the
match instance, treasure, scoring and rewards - are the Battle Arena
engine epic (#3648), built on the v1.188 server's rules behind the
same switch; until it lands nothing registers world 107 or links a
gate to it.

A switch off removes exactly what the marker records, and reads no
archive.

===========================================================================
"""
from pathlib import Path

import later_world_import

FLAG = "SRO_STYRIA_CLASH"
ROOT_ENV = "SRO_V1696_ROOT"
MARKER = ".opensro-styria.json"
# The engine's reap removes these by name; this import never writes them
# (the arena has no kinds of its own), so the names only keep its shard
# bands distinct from every other later world's.
CHARACTER_SHARD = "characterdata_230000.txt"
SKILL_SHARD = "skilldata_230000.txt"
REGION_CODE = "RN_ARENA_JOB_WAR"
PIN_FILE = Path(__file__).with_name("v1696_archive_pins.json")
# The four arena regions the pinned client authors; a client that names
# more or fewer under the code is not the pinned one and refuses.
ARENA_REGIONS = (31985, 31986, 32241, 32242)


# ================
# arena_regions
#
# The arena's region ids: the regioncode rows carrying its code. Every
# one is an outdoor sector region (a dungeon-bit row has no sector
# terrain), and the set must be the four the pinned client authors.
# ================
def arena_regions(rows):
	ids = sorted({int(row[1]) for row in rows if len(row) >= 3 and row[2].strip() == REGION_CODE})
	for region_id in ids:
		if region_id & 0x8000:
			raise ValueError(f"Styria Clash region {region_id} is a dungeon region")
	if tuple(ids) != ARENA_REGIONS:
		raise ValueError(f"the v1.696 client's {REGION_CODE} regions are {ids}, not {list(ARENA_REGIONS)}")
	return ids


SPEC = later_world_import.WorldImport(
	label="styria",
	flag=FLAG,
	root_env=ROOT_ENV,
	character_shard=CHARACTER_SHARD,
	skill_shard=SKILL_SHARD,
	marker=MARKER,
	pin_file=PIN_FILE,
	pin_sentence="The Styria Clash arena is built from one pinned v1.696 client; its archives are never committed.",
	region_label="Styria Clash",
	population_files=(),
)


# ================
# add_styria
# ================
def add_styria(native, out, v1696_root=None):
	"""
	Merge the Styria Clash arena from the pinned v1.696 client into the
	variant: the four regions' terrain remapped onto the merged tile
	catalog, their navmesh and availability bits, every Data file the
	native tree lacks, and the regions' minimap tiles and region codes
	(the client authors no zone name for them).
	Port-only, not native; off, exactly what the marker records is
	reaped and no archive is read.
	"""
	return later_world_import.add_map_world(SPEC, native, out, v1696_root,
		lambda rows_of: arena_regions(rows_of("regioncode.txt")))
