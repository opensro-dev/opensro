"""
===========================================================================

roc_import.py - the Roc range's kinds from the VSRO client (#3399).

Off unless SRO_ROC is "1", "on" or "true"; an unset or off build is
byte-identical to one without this import, and reaps a previous on
build of a reused variant root.

The Roc summit is native to v1.150: its arena regions (RN_RM_ROCAPEX,
the entry RN_RM_ROCAPEXENTER) stand in the shipped regioncode, but
the kinds that fight there do not - v1.150's characterdata ends the
MOB_RM_ range at the Tahomet uniques. The v1.188 client carries them
(the native-server rule picks it over the v1.696 rows, which differ
only in skill ids, DecayTime and one late column):

	MOB_RM_ROC              the Roc, unique (3), level 100
	MOB_RM_DEVILSAYTAN      Demon God Shaitan, boss (8)
	MOB_RM_TAHOMET_ROC      the Tahomet of the arena's back room
	MOB_RM_SPIRITS (+3)     the spirits and their three clones
	MOB_RM_WINGTRIBEKNIGHT  the wing tribe knight
	MOB_RM_WINGTRIBEDUKE    the wing tribe duke, the arena's field kind

This is step 1 of the native chain: it stages the rows, their skills,
names, skill effects and characterinfo on later_world_import's
stage_kinds, and nothing more. No nest places them: the reference
data holds no population in the arena regions, because the server
spawns Roc and the Tahomet bosses when the arena opens (step 4,
worlds 40 and 41). Until that lands the rows are inert.

The kinds ride characterdata_260000.txt / skilldata_260000.txt (one
importer per shard name; test_later_shard_bands pins the bands).
Identities allocate above every published row and the marker keeps
them across rebuilds. Their models are among the VSRO Data files the
Alexandria stage already copies (every file the native tree lacks).

Port-only staging of native-client content: the kinds are native to
the later client, the switch is the port's until the chain is whole.

===========================================================================
"""
import json
import os
from pathlib import Path

import alexandria_npcs as characters
import cap110_monsters as cap110
import import_vsro_alexandria as tables
import later_world_import
from prepare_client_resources import write_atomically

FLAG = "SRO_ROC"
MARKER = ".opensro-roc.json"
CHARACTER_SHARD = "characterdata_260000.txt"
SKILL_SHARD = "skilldata_260000.txt"
TEXT_ROOT = later_world_import.TEXT_ROOT

LEVEL_COLUMN = 57
GRADE_COLUMN = 15

# The admitted kinds and the (grade, level) each must author in the
# pinned client. A row that moved means another client: refuse by
# name rather than stage a different arena.
KINDS = {
	"MOB_RM_ROC": (3, 100),
	"MOB_RM_DEVILSAYTAN": (8, 100),
	"MOB_RM_TAHOMET_ROC": (8, 100),
	"MOB_RM_SPIRITS": (8, 100),
	"MOB_RM_SPIRITS_CLON": (8, 100),
	"MOB_RM_SPIRITS_CLON2": (8, 100),
	"MOB_RM_SPIRITS_CLON3": (8, 100),
	"MOB_RM_WINGTRIBEKNIGHT": (8, 100),
	"MOB_RM_WINGTRIBEDUKE": (0, 100),
}

# The engine's reap reads the shard, marker and population names from
# a spec; the Roc import writes no population file.
SPEC = later_world_import.WorldImport(
	label="roc",
	flag=FLAG,
	root_env=tables.ROOT_ENV,
	character_shard=CHARACTER_SHARD,
	skill_shard=SKILL_SHARD,
	marker=MARKER,
	pin_file=tables.PIN_FILE,
	pin_sentence="The Roc range is built from the one pinned VSRO client; its archives are never committed.",
	region_label="roc",
	population_files=(),
)


# ================
# flag_enabled
# ================
def flag_enabled(env=os.environ):
	"""True only when SRO_ROC names an on value; unset is native."""
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON


# ================
# select_kinds
#
# The admitted rows of the later characterdata, converted to the
# v1.150 layout. A kind absent from the client, or authoring another
# grade or level, refuses by name; so does a kind the native tree
# already carries (the import would stand it twice).
# ================
def select_kinds(later, native_codes):
	refs = {}
	for code, (grade, level) in KINDS.items():
		row = later.get(code)
		if row is None:
			raise ValueError(f"the VSRO client has no Roc kind {code}")
		if (int(row[GRADE_COLUMN]), int(row[LEVEL_COLUMN])) != (grade, level):
			raise ValueError(f"Roc kind {code} authors grade {row[GRADE_COLUMN]} level {row[LEVEL_COLUMN]}, want {grade} {level}")
		if code in native_codes:
			raise ValueError(f"Roc kind {code} already stands in the character tables")
		refs[code] = characters.convert_character(row)
	return refs


# ================
# add_roc
#
# The Alexandria builder's Roc pass, after every later layer (its
# identities allocate above all of theirs). Off, exactly what the
# marker enrolled is reaped and no archive is read.
# ================
def add_roc(vsro_root, native, output):
	native, output = Path(native).resolve(), Path(output).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("Roc output must be separate from the native extraction")
	marker = output / MARKER
	prior = cap110.read_json(marker) if marker.exists() else {}
	text_root = output / TEXT_ROOT
	if not flag_enabled():
		later_world_import.reap_monsters(SPEC, output, prior)
		return {"enabled": False}
	if not vsro_root:
		raise SystemExit(f"{FLAG} is on: pass the VSRO client folder or set {tables.ROOT_ENV}.")
	tables.require_vsro_pin(vsro_root, ("Media",))
	_, handle, media = tables.open_archive(vsro_root, "Media")
	try:
		index = tables.archive_index(media)

		# ================
		# read
		# ================
		def read(name):
			return tables.table_text(media, index, name)

		native_codes = {row[2] for path in text_root.glob("characterdata_*.txt") if path.name != CHARACTER_SHARD
			for row in tables.table_rows(path.read_bytes()) if len(row) > 2}
		refs = select_kinds(later_world_import.later_characters(read), native_codes)
		staged = later_world_import.stage_kinds("Roc", read, refs, text_root, prior, CHARACTER_SHARD, SKILL_SHARD,
			lambda code: code in refs)
	finally:
		media.close()
		handle.close()
	summary = {
		"enabled": True, "format": "opensro-roc-v1", "source": "vsro",
		"characters": staged["characters"], "skills": staged["skills"],
		"animationBases": staged["animationBases"], "characterInfo": staged["characterInfo"],
		"nameSymbols": staged["nameSymbols"],
		"limits": [
			"Inert: no nest places these kinds; the server spawns them when the arena opens (#3399 step 4).",
			"No drops yet (#3399 step 5).",
		],
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
	print(json.dumps(add_roc(args.vsro_root, args.native_extracted, args.output), indent=2))


if __name__ == "__main__":
	main()
