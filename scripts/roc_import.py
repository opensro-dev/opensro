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

This is step 1 of the native chain. Step 1a stages the rows, their
skills, names, skill effects and characterinfo on later_world_import's
stage_kinds. No nest places them: the reference data holds no
population in the arena regions, because the server spawns Roc and
the Tahomet bosses when the arena opens (step 4, worlds 40 and 41).
Until that lands the rows are inert.

Step 1b adds the way in, from the same client:

	GATE_RC_MOUNTAIN_GATE   the summit gate (building STORE_RC_TAHOMET_GATE)
	GATE_RC_TAHOMET_GATE    arrival in the Tahomet room, world 41
	GATE_RC_ROC_GATE        arrival on the Roc's summit, world 40
	GATE_RC_ROC_FRONT_GATE  the wait room's gate (STORE_ROC_FRONT_GATE)

and the one arena region v1.150 lacks, the wait room 25792
(RN_RM_ROCAPEX_WAIT: v1.150 ships its zone names but no regioncode
row, sector 100/192 or navmesh), merged by later_world_import's
merge_world. The summit link (mountain gate -> Roc gate) carries the
native conditions: the key item held (4) and spent (5), the level (1)
and no job (6), which the server evaluates since #3765. The key item,
ITEM_QNO_RM_MEETROC_6_01, is native: v1.150 ships all seventeen MEETROC
items (ItemData_25000, 24156-24170 and 24292-24293), under other ids
than the source client's, so the condition is renumbered by codename.
A variant without the item keeps the summit link out - never open
without its key - and the build log says so.

The teleport, building, link and name tables need no reap: the
Alexandria stage rewrites them from the native bytes on every build
before this pass runs (build_alexandria_variant: add_gates, add_map),
as it does the two map catalogs merge_world documents. The region
merge's files are enrolled in the marker and reaped off.

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
import build_alexandria_variant as variant
import cap110_monsters as cap110
import import_vsro_alexandria as tables
import later_world_import
from prepare_client_resources import write_atomically

FLAG = "SRO_ROC"
MARKER = ".opensro-roc.json"
CHARACTER_SHARD = "characterdata_260000.txt"
SKILL_SHARD = "skilldata_260000.txt"
TEXT_ROOT = later_world_import.TEXT_ROOT
# The Roc range's gates, by teleportdata code (step 1b).
GATE_CODES = ("GATE_RC_MOUNTAIN_GATE", "GATE_RC_TAHOMET_GATE", "GATE_RC_ROC_GATE", "GATE_RC_ROC_FRONT_GATE")
# The wait room, RN_RM_ROCAPEX_WAIT: the one arena region v1.150 lacks
# (its zone names ship; its regioncode row, sector and navmesh do not).
WAIT_REGION = 25792
# The archives the region merge reads, all pinned: the family's one list.
MERGE_ARCHIVES = later_world_import.MERGE_ARCHIVES
TELEPORT_TABLES = ("teleportdata.txt", "teleportbuilding.txt", "teleportlink.txt")

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
# item_ids
#
# {item id as text: codename} of one client's item tables (the manifest
# itemdata.txt names the shards).
# ================
def item_ids(rows_of, manifest):
	return {row[1]: row[2] for shard in manifest for row in rows_of(shard[0].lower()) if len(row) > 2}


# ================
# key_items
#
# {source item id: variant item id} for every item a Roc link's
# condition names that the variant carries, matched by codename. An
# item the variant lacks is simply absent: merge_gate_tables then
# keeps its link out and reports it.
# ================
def key_items(source_links, source_items, variant_items):
	variant_ids = {code: identity for identity, code in variant_items.items()}
	wanted = {row[at + 1] for row in source_links if len(row) >= variant.LINK_COLUMNS
		for at in range(6, variant.LINK_COLUMNS, 3) if row[at].isdigit() and int(row[at]) in variant.LINK_KINDS_ITEM}
	return {item: variant_ids[source_items[item]] for item in sorted(wanted)
		if item in source_items and source_items[item] in variant_ids}


# ================
# taken_building_ids
#
# The gate buildings keep the source's identities, which share the
# object space with characters and items: one already standing in the
# variant refuses by name instead of standing two objects on one id.
# ================
def taken_building_ids(text_root, building_ids):
	used = {}
	for pattern in ("characterdata_*.txt", "itemdata_*.txt", "teleportbuilding.txt"):
		for path in text_root.glob(pattern):
			for row in tables.table_rows(path.read_bytes()):
				if len(row) > 2:
					used[row[1]] = row[2]
	return sorted(f"{identity} ({used[identity]})" for identity in building_ids if identity in used)


# ================
# without_previous_gates
#
# The teleport tables with an earlier Roc pass's rows taken out: its
# gates by code, their buildings by codename and every link touching
# one of those gates. A full build starts from the native tables (the
# module header); this keeps a pass over a tree an earlier one wrote
# (the standalone main, a rebuild of the stage) identical to a fresh one.
# ================
def without_previous_gates(current, building_codes):
	data = tables.table_rows(current["teleportdata.txt"])
	gone = {row[1] for row in data if len(row) > 2 and row[2] in GATE_CODES}
	if not gone:
		return current
	kept = dict(current)
	kept["teleportdata.txt"] = cap110.encode_rows([row for row in data if len(row) <= 2 or row[2] not in GATE_CODES])
	kept["teleportbuilding.txt"] = cap110.encode_rows([row for row in tables.table_rows(current["teleportbuilding.txt"])
		if len(row) <= 2 or row[2] not in building_codes])
	kept["teleportlink.txt"] = cap110.encode_rows([row for row in tables.table_rows(current["teleportlink.txt"])
		if len(row) <= 2 or not {row[1], row[2]} & gone])
	return kept


# ================
# add_gates
#
# The four Roc gates, their two buildings and the links among them,
# merged onto the variant's teleport tables by the Alexandria merge's
# own rules (merge_gate_tables: level clamp, item renumbering, keyed
# links kept out when their item is missing). Returns the summary.
# ================
def add_gates(read, text_root):
	source = {name: read(name) for name in TELEPORT_TABLES}
	source["text"] = read("textdata_object.txt")
	data_rows = [row for row in tables.table_rows(source["teleportdata.txt"]) if len(row) > 3 and row[2] in GATE_CODES]
	building_ids = {row[3] for row in data_rows if row[3] != "0"}
	building_codes = {row[2] for row in tables.table_rows(source["teleportbuilding.txt"])
		if len(row) > 2 and row[1] in building_ids}
	current = {name: (text_root / name).read_bytes() for name in TELEPORT_TABLES}
	current["text"] = (text_root / "textdataname.txt").read_bytes()
	current = without_previous_gates(current, building_codes)
	for name in TELEPORT_TABLES:
		variant.replace_file(text_root / name, current[name])
	taken = taken_building_ids(text_root, building_ids)
	if taken:
		raise ValueError(f"Roc gate buildings would reuse identities the variant holds: {taken}")

	# ================
	# source_rows
	# ================
	def source_rows(name):
		return tables.table_rows(read(name))

	# ================
	# variant_rows
	# ================
	def variant_rows(name):
		return tables.table_rows((text_root / name).read_bytes())

	items = key_items(
		source_rows("teleportlink.txt"),
		item_ids(source_rows, source_rows("itemdata.txt")),
		item_ids(variant_rows, variant_rows("itemdata.txt")),
	)
	merged, summary = variant.merge_gate_tables(
		current, source, codes=GATE_CODES, level_cap=variant.gate_level_cap(), items=items)
	for dropped in summary.get("droppedConditions", []):
		tables.log(f"Roc gates: link {dropped['from']} -> {dropped['to']}: condition kind {dropped['kind']} "
			f"dropped; the v1.150 server has no evaluator for it")
	variant.log_omitted_links("Roc gates", summary)
	for name, content in merged.items():
		variant.replace_file(text_root / ("textdataname.txt" if name == "text" else name), content)
	return {
		"codes": list(GATE_CODES), "links": summary["links"], "buildings": summary["buildingCodes"],
		"texts": summary["texts"], "keyItems": items,
		**({"omittedLinks": summary["omittedLinks"]} if "omittedLinks" in summary else {}),
	}


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
# identities allocate above all of theirs). On, the wait room merges
# through merge_world, whose last step (stage) stages the kinds and
# the gates and writes the marker the region record joins. Off,
# exactly what the marker enrolled is reaped and no archive is read.
# ================
def add_roc(vsro_root, native, output):
	native, output = Path(native).resolve(), Path(output).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("Roc output must be separate from the native extraction")
	if not flag_enabled():
		later_world_import.reap_world(SPEC, native, output)
		return {"enabled": False}
	if not vsro_root:
		raise SystemExit(f"{FLAG} is on: pass the VSRO client folder or set {tables.ROOT_ENV}.")
	vsro_root = Path(vsro_root)
	tables.require_vsro_pin(vsro_root, MERGE_ARCHIVES)
	return later_world_import.merge_world(SPEC, native, output, vsro_root, [WAIT_REGION],
		lambda: stage(vsro_root, output))


# ================
# stage
#
# The kinds (step 1a) and the gates (step 1b) from the client's Media,
# then the marker, keeping the region record merge_world enrols.
# ================
def stage(vsro_root, output):
	marker = output / MARKER
	prior = cap110.read_json(marker) if marker.exists() else {}
	text_root = output / TEXT_ROOT
	_, handle, media = tables.open_archive(vsro_root, "Media")
	try:
		index = tables.archive_index(media)

		# ================
		# read
		# ================
		def read(name):
			return tables.table_text(media, index, name)

		# Gates first: the merge rewrites textdataname.txt, and the name
		# step below appends onto what it leaves.
		gates = add_gates(read, text_root)
		native_codes = {row[2] for path in text_root.glob("characterdata_*.txt") if path.name != CHARACTER_SHARD
			for row in tables.table_rows(path.read_bytes()) if len(row) > 2}
		refs = select_kinds(later_world_import.later_characters(read), native_codes)
		staged = later_world_import.stage_kinds("Roc", read, refs, text_root, prior, CHARACTER_SHARD, SKILL_SHARD,
			lambda code: code in refs)
	finally:
		media.close()
		handle.close()
	limits = [
		"Inert: no nest places these kinds; the server spawns them when the arena opens (#3399 step 4).",
		"No drops yet (#3399 step 5).",
	]
	if "omittedLinks" in gates:
		limits.append("The summit link is out: the variant carries no key item its conditions name.")
	summary = {
		"enabled": True, "format": "opensro-roc-v2", "source": "vsro",
		"characters": staged["characters"], "skills": staged["skills"],
		"animationBases": staged["animationBases"], "characterInfo": staged["characterInfo"],
		"nameSymbols": staged["nameSymbols"], "gates": gates,
		**({"regionMerge": prior["regionMerge"]} if "regionMerge" in prior else {}),
		"limits": limits,
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
