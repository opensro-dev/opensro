"""
===========================================================================

survival_colosseum_import.py - the Sook survival arena's Colosseum map (port-only).

Port-only, not native. Rides the arena's own switch, SRO_SOOK_SURVIVAL_ARENA:
the GameWorld admits a run only where region 25580 (sector 236/99) has
navigation (game/action/sookarena.go), and v1.150 ships no such sector. A
release that turned the arena on without this layer refused every entrant
with "the public arena navigation is unavailable".

The venue is the later client's Colosseum: the one region its
STRUCTURE_POS_COLOSSEUM_SAFEZONE_* teleport rows name
(import_survival_colosseum.colosseum_region). Like the Styria Clash map
(styria_import.py) it is a map alone: the later-world engine merges its
terrain, navmesh, catalogs and Data files from the pinned v1.696 client;
the arena's monsters are the server's own pool, so the monster pass only
writes the marker. The standalone import_survival_colosseum.py stays the
operator's tool for a variant root outside this build.

A switch off removes exactly what the marker records, and reads no
archive.

===========================================================================
"""
import json
import mmap
import os
from pathlib import Path

import cap110_monsters as cap110
import import_survival_colosseum as colosseum
import import_vsro_alexandria as tables
import jupiter_import as jupiter
import later_world_import
from prepare_client_resources import write_atomically

FLAG = "SRO_SOOK_SURVIVAL_ARENA"
ROOT_ENV = "SRO_V1696_ROOT"
MARKER = ".opensro-survival-colosseum-layer.json"
# The engine's reap removes these by name; this import never writes them
# (the arena has no kinds of its own), so the names only keep its shard
# band distinct from every other later world's.
CHARACTER_SHARD = "characterdata_280000.txt"
SKILL_SHARD = "skilldata_280000.txt"
PIN_FILE = Path(__file__).with_name("v1696_archive_pins.json")
# The region the server's arena admission resolves (sookarena.go).
ARENA_REGION = 25580


# ================
# flag_enabled
# ================
def flag_enabled(env=os.environ):
	"""True only when SRO_SOOK_SURVIVAL_ARENA names an on value; unset is native."""
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON


# ================
# arena_region
#
# The Colosseum's region from the later teleport table; a client whose
# safezone rows name another region is not the venue the server expects.
# ================
def arena_region(teleport_raw):
	region = colosseum.colosseum_region(teleport_raw)
	if region != ARENA_REGION:
		raise ValueError(f"the v1.696 client's Colosseum is region {region}, not {ARENA_REGION}")
	return region


SPEC = later_world_import.WorldImport(
	label="survivalColosseum",
	flag=FLAG,
	root_env=ROOT_ENV,
	character_shard=CHARACTER_SHARD,
	skill_shard=SKILL_SHARD,
	marker=MARKER,
	pin_file=PIN_FILE,
	pin_sentence="The survival arena's Colosseum is built from one pinned v1.696 client; its archives are never committed.",
	region_label="Survival Colosseum",
	population_files=(),
)


# ================
# write_marker
#
# No kinds: the monster pass only enrols the import in its marker, which
# merge_world then extends with the region record the off path reaps.
# ================
def write_marker(out):
	marker = Path(out) / MARKER
	prior = cap110.read_json(marker) if marker.exists() else {}
	record = {key: value for key, value in prior.items() if key == "regionMerge"}
	record["enabled"] = True
	write_atomically(marker, (json.dumps(record, indent=2) + "\n").encode())
	return {"enabled": True, "characters": 0}


# ================
# add_survival_colosseum
# ================
def add_survival_colosseum(native, out, v1696_root=None):
	"""
	Merge the Colosseum region from the pinned v1.696 client into the
	variant through the later-world engine. Port-only, not native; off,
	exactly what the marker records is reaped and no archive is read.
	"""
	if not flag_enabled():
		later_world_import.reap_world(SPEC, native, out)
		return {"enabled": False}
	v1696_root = later_world_import.client_root(v1696_root, ROOT_ENV)
	later_world_import.require_pin(SPEC, v1696_root, later_world_import.MERGE_ARCHIVES, jupiter.archive_pins)
	with open(v1696_root / "Media.pk2", "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as media:
		index = tables.archive_index(media)
		region = arena_region(tables.table_text(media, index, "teleportdata.txt"))
	return later_world_import.merge_world(SPEC, native, out, v1696_root, [region], lambda: write_marker(out))
