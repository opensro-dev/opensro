"""
===========================================================================

winter_event_import.py - the winter event's monsters for the Christmas event

Port-only, not native. Off unless SRO_CHRISTMAS_EVENT is "1", "on" or
"true" when the variant builds; the server spawns these kinds behind the
same switch (action/christmas_waves.go).

The v1.150 client has a Christmas event (the stolen socks, event group
1) but no winter monsters. The later clients' winter event brings
three kinds, all in the pinned v1.696 client (SRO_V1696_ROOT,
hash-pinned before anything is read):

	MOB_EV_WINTER_SNOWMAN        "Snow Slave", level 1: the hunted
	                             crowd of the "Snow Slave Hunting Event"
	MOB_EV_WINTER_WHITEKNIGHT_N  "White Knight", one champion row per
	                             level band N (20 ... 130)
	MOB_EV_WINTER_PRINCESS_N     "Princess Miyene", the same bands

Their models, materials and animations are already in the variant: the
Alexandria stage copies every VSRO Data file the native tree lacks, and
the VSRO client ships the same three .bsr closures. What this import
adds is their character rows (converted to the v1.150 layout in a shard
of their own), the skills they name (the native basic attack), their
names and their characterinfo rows. Rows above the server's level cap
(cap110.MAX_LEVEL) stay out. No placement: the client authors no nest
for any of them: the server lands the Snow Slaves while it snows and
the White Knight and Princess on their timetable.

The kind merge is later_world_import's (stage_kinds); the switch off
removes exactly the rows the marker records (reap_monsters).

===========================================================================
"""
import json
import mmap
import os
from pathlib import Path

import alexandria_npcs as characters
import cap110_monsters as cap110
import import_vsro_alexandria as tables
import jupiter_import as jupiter
import later_world_import
from prepare_client_resources import write_atomically

FLAG = "SRO_CHRISTMAS_EVENT"
ROOT_ENV = "SRO_V1696_ROOT"
MARKER = ".opensro-winter-event.json"
# The 240000 identity band: 210000 is the Constantinople fortress's,
# 220000 Muhan's and 230000 Styria's. One importer owns one shard file in
# a build: a layer's off reap unlinks its shard, so a shared band would
# delete the other layer's rows (test_later_shard_bands.py).
CHARACTER_SHARD = "characterdata_240000.txt"
SKILL_SHARD = "skilldata_240000.txt"
PREFIX = "MOB_EV_WINTER_"
PIN_FILE = Path(__file__).with_name("v1696_archive_pins.json")
# Character row columns (cap110_monsters.py's layout).
CHAR_CODE = 2
CHAR_LEVEL = 57

SPEC = later_world_import.WorldImport(
	label="winter event",
	flag=FLAG,
	root_env=ROOT_ENV,
	character_shard=CHARACTER_SHARD,
	skill_shard=SKILL_SHARD,
	marker=MARKER,
	pin_file=PIN_FILE,
	pin_sentence="The winter monsters are built from one pinned v1.696 client; its archives are never committed.",
	region_label="winter event",
	population_files=(),
)


# ================
# flag_enabled
# ================
def flag_enabled(env=os.environ):
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON


# ================
# admits
#
# One later character row against the import: a winter event monster
# at or below the server's level cap.
# ================
def admits(row):
	return row[CHAR_CODE].startswith(PREFIX) and int(row[CHAR_LEVEL]) <= cap110.MAX_LEVEL


# ================
# add_winter_monsters
#
# The variant layer's entry point (variant_later_layers.py). Off, or
# without the v1.696 client, a previous import is reaped and no archive
# is read.
# ================
def add_winter_monsters(native, out, env=os.environ):
	native, output = Path(native).resolve(), Path(out).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("winter event overlay must be separate from native extraction")
	marker = output / MARKER
	prior = cap110.read_json(marker) if marker.exists() else {}
	text_root = output / later_world_import.TEXT_ROOT
	root = env.get(ROOT_ENV, "").strip()
	if not flag_enabled(env) or not root:
		later_world_import.reap_monsters(SPEC, output, prior)
		return {"enabled": False, "reason": "off" if not flag_enabled(env) else f"{ROOT_ENV} is not set"}
	later_world_import.require_pin(SPEC, root, ("Media",), jupiter.archive_pins)
	with open(Path(root) / "Media.pk2", "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as media:
		index = tables.archive_index(media)

		# ================
		# read
		# ================
		def read(name):
			return tables.table_text(media, index, name.lower())

		later = {row[CHAR_CODE]: row for shard in tables.table_rows(read("characterdata.txt"))
			for row in tables.table_rows(read(shard[0]))}
		refs = {code: characters.convert_character(row) for code, row in sorted(later.items()) if admits(row)}
		if not refs:
			raise ValueError(f"the v1.696 client carries no {PREFIX}* character rows")
		kinds = later_world_import.stage_kinds(SPEC.label, read, refs, text_root, prior, CHARACTER_SHARD, SKILL_SHARD,
			lambda code: code in refs)
	summary = {"enabled": True, **kinds, "levels": {code: int(later[code][CHAR_LEVEL]) for code in refs}}
	write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode())
	return summary
