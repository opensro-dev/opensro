"""
===========================================================================

colosseum_import.py - the Survival Arena's Colosseum map layer (port-only)

Port-only, not native. Off unless SRO_SOOK_SURVIVAL_ARENA is "1", "on"
or "true". Build and server read the same switch: the GameWorld runs the
arena in region 25580 (action/sookarena.go) and the variant build stages
that region here, so a shard with the arena on has its venue.

The v1.150 world has no Colosseum. The v1.696 client authors it as one
outdoor region, named only by its safezone teleport rows
(STRUCTURE_POS_COLOSSEUM_SAFEZONE_*, column 5 the region): regioncode
carries no row for it. This layer merges that region's terrain, navmesh,
availability bit, minimap and every Data and Particles file it needs
through the later-world engine's map-only path
(later_world_import.add_map_world), as the Styria Clash arena does. It adds no population: the arena draws
its waves from the shard's creatable monsters (sookarena.BuildPool).

The region's environment entry (55) needs no merge: the variant builder
writes the VSRO environment.ifo after every later layer, and that file
carries entry 55 (checked 2026-11-19 against rel-b88532fa5's variant).

A switch off removes exactly what the marker records, and reads no
archive.

===========================================================================
"""
from pathlib import Path

import later_world_import

FLAG = "SRO_SOOK_SURVIVAL_ARENA"
ROOT_ENV = "SRO_V1696_ROOT"
MARKER = ".opensro-colosseum.json"
# The engine's reap removes these by name; this layer never writes them
# (the arena has no kinds of its own), so the names only keep its shard
# bands distinct from every other later world's.
CHARACTER_SHARD = "characterdata_280000.txt"
SKILL_SHARD = "skilldata_280000.txt"
PIN_FILE = Path(__file__).with_name("v1696_archive_pins.json")
SAFEZONE_PREFIX = "STRUCTURE_POS_COLOSSEUM_SAFEZONE_"
SAFEZONE_REGION_COLUMN = 5
# The region the server's arena runs in (action/sookarena.go); a client
# whose safezones name another is not the pinned one and refuses.
COLOSSEUM_REGION = 25580
# The pinned client authors eight safezone starts; fewer is a cut client.
MIN_SAFEZONES = 8


# ================
# colosseum_regions
#
# The region the safezone teleport rows stand in: exactly one, the one
# the server runs the arena in, outdoors, with at least MIN_SAFEZONES
# starts.
# ================
def colosseum_regions(rows):
	starts = [row for row in rows
		if len(row) > SAFEZONE_REGION_COLUMN and row[0] == "1" and row[2].startswith(SAFEZONE_PREFIX)]
	regions = sorted({int(row[SAFEZONE_REGION_COLUMN]) for row in starts})
	if regions != [COLOSSEUM_REGION]:
		raise ValueError(f"the v1.696 Colosseum safezones stand in regions {regions}, not [{COLOSSEUM_REGION}]")
	if len(starts) < MIN_SAFEZONES:
		raise ValueError(f"the v1.696 client authors {len(starts)} Colosseum safezones, fewer than {MIN_SAFEZONES}")
	return regions


SPEC = later_world_import.WorldImport(
	label="colosseum",
	flag=FLAG,
	root_env=ROOT_ENV,
	character_shard=CHARACTER_SHARD,
	skill_shard=SKILL_SHARD,
	marker=MARKER,
	pin_file=PIN_FILE,
	pin_sentence="The Survival Arena's Colosseum is built from one pinned v1.696 client; its archives are never committed.",
	region_label="Colosseum",
	population_files=(),
)


# ================
# add_colosseum
# ================
def add_colosseum(native, out, v1696_root=None):
	"""
	Merge the Colosseum region from the pinned v1.696 client into the
	variant. Port-only, not native; off, exactly what the marker records
	is reaped and no archive is read.
	"""
	return later_world_import.add_map_world(SPEC, native, out, v1696_root,
		lambda rows_of: colosseum_regions(rows_of("teleportdata.txt")))
