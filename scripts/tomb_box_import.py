"""
===========================================================================

tomb_box_import.py - the Ch'in Tomb treasure boxes' guards (#4810).

Port-only, not native. Off unless SRO_TOMB_TREASURE_BOXES is "1", "on"
or "true" when the variant builds; the server runs the four boxes'
open event behind the same switch (action/tombbox.go) and summons
these guards by codename.

The v1.150 client defines the boxes (NPC_TQ_TREASUREBOX1-4), their
keys and every talk line, but no guard: the open event's guard outcome
summons kinds only the later clients carry. The pinned VSRO client
(SRO_VSRO_ROOT, hash-pinned before anything is read) authors both, at
24345 and 24346:

	MOB_TQ_TREASURE_GUARD       the guard, grade 6, level 97
	MOB_TQ_TREASURE_GUARD_CLON  its clone, grade 6, level 87

This import stages the two on later_world_import's stage_kinds: their
character rows converted to the v1.150 layout in a shard band of their
own, the skills they name, their names and their characterinfo rows.
Their models are among the VSRO Data files the Alexandria stage
already copies (every file the native tree lacks). No placement: the
server summons them at the opened box.

The kinds ride characterdata_350000.txt / skilldata_350000.txt (one
importer per shard name; test_later_shard_bands pins the bands).
Identities allocate above every published row and the marker keeps
them across rebuilds. Off reaps exactly what the marker records
(reap_monsters) and reads no archive.

===========================================================================
"""
import json
import os
from pathlib import Path

import alexandria_npcs as characters
import cap110_monsters as cap110
import import_vsro_alexandria as tables
import later_tables
import later_world_import
from prepare_client_resources import write_atomically

FLAG = "SRO_TOMB_TREASURE_BOXES"
MARKER = ".opensro-tomb-boxes.json"
# The 350000 identity band, after the pet import's 340000: one importer
# owns one shard file in a build, so a layer's off reap never unlinks
# another layer's rows (test_later_shard_bands.py).
CHARACTER_SHARD = "characterdata_350000.txt"
SKILL_SHARD = "skilldata_350000.txt"
TEXT_ROOT = later_world_import.TEXT_ROOT

GRADE_COLUMN = 15

# The admitted kinds and the (grade, level) each must author in the
# pinned client. A row that moved means another client: refuse by
# name rather than stage a different guard.
KINDS = {
	"MOB_TQ_TREASURE_GUARD": (6, 97),
	"MOB_TQ_TREASURE_GUARD_CLON": (6, 87),
}

# The engine's reap reads the shard and marker names from a spec; this
# import has no population of its own (the server summons the guards).
SPEC = later_world_import.WorldImport(
	label="tomb treasure boxes",
	flag=FLAG,
	root_env=tables.ROOT_ENV,
	character_shard=CHARACTER_SHARD,
	skill_shard=SKILL_SHARD,
	marker=MARKER,
	pin_file=tables.PIN_FILE,
	pin_sentence="The treasure guards are built from the one pinned VSRO client; its archives are never committed.",
	region_label="tomb treasure boxes",
	population_files=(),
)


# ================
# flag_enabled
# ================
def flag_enabled(env=os.environ):
	"""True only when SRO_TOMB_TREASURE_BOXES names an on value; unset is native."""
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON


# ================
# select_kinds
#
# The admitted rows of the later characterdata, converted to the
# v1.150 layout. A kind absent from the client, or authoring another
# grade or level, refuses by name; a kind the tree already carries
# refuses in later_world_import.stage_kinds.
# ================
def select_kinds(later):
	refs = {}
	for code, (grade, level) in KINDS.items():
		row = later.get(code)
		if row is None:
			raise ValueError(f"the VSRO client has no treasure guard {code}")
		if (int(row[GRADE_COLUMN]), int(row[characters.LEVEL_COLUMN])) != (grade, level):
			raise ValueError(f"treasure guard {code} authors grade {row[GRADE_COLUMN]} level {row[characters.LEVEL_COLUMN]}, want {grade} {level}")
		refs[code] = characters.convert_character(row)
	return refs


# ================
# add_tomb_boxes
#
# The variant layer's entry point (variant_later_layers.py). Off, a
# previous import is reaped from its marker and no archive is read.
# On, the two guards stage from the pinned VSRO client's Media; on
# without that client refuses before anything is read, since a build
# that silently lacks them leaves the box event's guards unsummonable.
# ================
def add_tomb_boxes(native, out, env=os.environ):
	native, output = Path(native).resolve(), Path(out).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("tomb treasure boxes overlay must be separate from native extraction")
	marker = output / MARKER
	prior = cap110.read_json(marker) if marker.exists() else {}
	if not flag_enabled(env):
		later_world_import.reap_monsters(SPEC, output, prior)
		return {"enabled": False}
	root = env.get(tables.ROOT_ENV, "").strip()
	if not root:
		raise SystemExit(f"{FLAG} is on: pass the VSRO client folder or set {tables.ROOT_ENV}.")
	vsro_root = Path(root)
	tables.require_vsro_pin(vsro_root, ("Media",))
	text_root = output / TEXT_ROOT
	_, handle, media = tables.open_archive(vsro_root, "Media")
	try:
		index = tables.archive_index(media)
		read = later_tables.Archive.media(media, index)
		refs = select_kinds(later_tables.characters(read))
		staged = later_world_import.stage_kinds("treasure guards", read, refs, text_root, prior,
			CHARACTER_SHARD, SKILL_SHARD, lambda code: code in refs)
	finally:
		media.close()
		handle.close()
	summary = {
		"enabled": True, "format": "opensro-tomb-boxes-v1", "source": "vsro",
		"characters": staged["characters"], "skills": staged["skills"],
		"animationBases": staged["animationBases"], "effectBases": staged["effectBases"],
		"characterInfo": staged["characterInfo"], "nameSymbols": staged["nameSymbols"],
	}
	write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode())
	return summary


# ================
# main
# ================
def main():
	import argparse
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--vsro-root", default=os.environ.get(tables.ROOT_ENV))
	parser.add_argument("--native-extracted", required=True)
	parser.add_argument("--output", required=True)
	args = parser.parse_args()
	env = dict(os.environ)
	env[FLAG] = "on"
	if args.vsro_root:
		env[tables.ROOT_ENV] = args.vsro_root
	print(json.dumps(add_tomb_boxes(args.native_extracted, args.output, env), indent=2))


if __name__ == "__main__":
	main()
