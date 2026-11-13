"""
===========================================================================

hunting2_import.py - optional second hunting grounds (port-only).

Port-only, not native. Off unless SRO_HUNTING2 is "1", "on" or "true".

The second hunting ground set (the v1.696 client's regioncode row
RN_NEW_HUNTING_SECOND_FIELD, 104 regions) postdates the later shard
backup, so no server nest export names its monsters. Its population
evidence is the pinned v1.696 client's own: the npcpos placements
standing in its regions (4,060, measured in
research/investigations/later-sources-3247, T3647), naming 71 MOB_*
kinds joined to characterdata by codename, and the regions' terrain,
navmesh and minimap tiles, all present in the same client.

Decisions recorded (docs/features/HUNTING2.md):

	admission   exactly the kinds npcpos places in the set's regions,
	            at the levels characterdata authors (49-140, grades
	            0 and 1). The set re-stages known species, the same
	            species at several levels; the authored level is the
	            spawn level - no re-banding, nothing raises the
	            player level cap.
	placements  become nests by the cap-110 client posture
	            (cap110_monsters.client_field_population): one nest
	            of one monster per placement point, radius 0, the
	            synthesized ordinary controls and respawn posture.
	world       the regions join the default world (nests name
	            world 1, INS_DEFAULT), like the cap-110 field; no
	            new world is registered.

The kinds are converted to the v1.150 layout in a character shard of
their own (characterdata_180000.txt, one importer per shard name:
120000 is the cap-110 family's, 170000 Jupiter's), their identities
allocated above every published row and kept in the marker, their
monster skills merged the cap-110 way, and the nests written
as one Version-2 population file (hunting2-field.txt) the server
reads behind the same flag. add_hunting2 is the variant-side merge:
the regions' terrain, navmesh, names and Data files. A switch off
removes exactly what the marker records. No authored drops: the
monsters keep the native loot posture.

The region merge and the reap are later_world_import's
(merge_world / reap_world, #3326); this module is the hunting2
spec and the placement shapes that are truly its own: the region
set, the npcpos join, the admission window and the synthesized
nests. The set postdates the shard backup, so no population
export drives it - it takes the engine's merge and reap, not its
export-driven monster pass.

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
from prepare_client_resources import write_atomically

FLAG = "SRO_HUNTING2"
ROOT_ENV = "SRO_V1696_ROOT"
MARKER = ".opensro-hunting2.json"
CHARACTER_SHARD = "characterdata_180000.txt"
SKILL_SHARD = "skilldata_180000.txt"
FIELD_FILE = "hunting2-field.txt"
REGION_CODE = "RN_NEW_HUNTING_SECOND_FIELD"
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA
PIN_FILE = Path(__file__).with_name("v1696_archive_pins.json")

# The family's recorded window: the levels and grades the placed
# kinds author (measured: 49-140, grades 0 and 1). A placed kind
# outside its own window refuses by name rather than shifting it.
MIN_LEVEL = 49
MAX_LEVEL = 140
GRADES = (0, 1)
WORLD = 1


# ================
# flag_enabled
# ================
def flag_enabled(env=os.environ):
	"""True only when SRO_HUNTING2 names an on value; unset is native."""
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON


# ================
# hunting_regions
#
# The set's region ids: the regioncode rows carrying its code. Every
# one is an outdoor sector region; a dungeon-bit row would have no
# sector terrain and refuses here.
# ================
def hunting_regions(rows):
	ids = sorted({int(row[1]) for row in rows if len(row) >= 3 and row[2].strip() == REGION_CODE})
	for region_id in ids:
		if region_id & 0x8000:
			raise ValueError(f"hunting ground region {region_id} is a dungeon region")
	return ids


# ================
# hunting_placements
#
# The client placements standing in the set's regions: (codename,
# region, x, y, z) for every npcpos row whose character stands
# there, joined by the client's numeric id as the Alexandria NPC
# join makes it, the region masked to its unsigned form. A
# duplicate row stands once, and a row naming no character is the
# client's own dead authoring and is skipped, as npcpos_placements
# skips it.
# ================
def hunting_placements(npcpos_raw, later, regions):
	by_id = {row[1]: row[2] for row in later.values() if len(row) > 2}
	placements, seen = [], set()
	for row in tables.table_rows(npcpos_raw):
		if len(row) < 5 or row[0] not in by_id:
			continue
		try:
			region, x, y, z = int(row[1]) & 0xFFFF, float(row[2]), float(row[3]), float(row[4])
		except ValueError:
			continue
		if region not in regions:
			continue
		code = by_id[row[0]]
		key = (code, region, row[2], row[3], row[4])
		if key in seen:
			continue
		seen.add(key)
		placements.append((code, region, x, y, z))
	return placements


# ================
# family_admits
#
# One placed character row against the family's recorded window: a
# MOB_* codename, its level (row 57) inside 49-140 and its grade
# (row 15) one of the authored grades.
# ================
def family_admits(code, row):
	return code.startswith("MOB_") and MIN_LEVEL <= int(row[57]) <= MAX_LEVEL and int(row[15]) in GRADES


# ================
# synthesize_population
#
# The set's nests from its placements, by the cap-110 client posture:
# one nest of one monster standing on each placement point of an
# admitted species, identities sequential over the sorted
# placements so a rebuild of the pinned client writes the same
# file. The rows name the default world, whose regions these are.
# ================
def synthesize_population(placements, refs):
	ordered = sorted((p for p in placements if p[0] in refs),
		key=lambda p: (p[1], p[0], p[2], p[3], p[4]))
	rows = []
	for nest_id, (code, region, x, y, z) in enumerate(ordered, 1):
		rows.append({
			"Codename": code, "NestID": nest_id, "WorldID": WORLD, "RegionID": region,
			"X": x, "Y": y, "Z": z, "Radius": cap110.CLIENT_NEST_RADIUS,
			"GenerateRadius": cap110.CLIENT_NEST_GENERATE_RADIUS, "MaxCount": 1,
			"RespawnDelayMinSec": cap110.CLIENT_NEST_RESPAWN_MIN_SEC,
			"RespawnDelayMaxSec": cap110.CLIENT_NEST_RESPAWN_MAX_SEC,
			"Respawn": True, "InitialDir": 0, "ChampionGenPercentage": 0,
			"Controls": cap110.client_controls(), "Champion": None,
		})
	return {"Version": 2, "Rows": rows}


SPEC = later_world_import.WorldImport(
	label="hunting2",
	flag=FLAG,
	root_env=ROOT_ENV,
	character_shard=CHARACTER_SHARD,
	skill_shard=SKILL_SHARD,
	marker=MARKER,
	pin_file=PIN_FILE,
	pin_sentence="The second hunting grounds are built from one pinned v1.696 client; its archives are never committed.",
	region_label="hunting ground",
	population_files=(("field", FIELD_FILE),),
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
# The monster import (hunting2's own: the client's npcpos placements)
#============================================================================


# ================
# add_monsters
# ================
def add_monsters(v1696_root, native, output):
	native, output = Path(native).resolve(), Path(output).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("hunting2 overlay must be separate from native extraction")
	marker = output / MARKER
	prior = cap110.read_json(marker) if marker.exists() else {}
	text_root = output / TEXT_ROOT
	if not flag_enabled():
		later_world_import.reap_monsters(SPEC, output, prior)
		return {"enabled": False}
	if not v1696_root:
		raise SystemExit(f"Pass the v1.696 client folder or set {ROOT_ENV}.")
	require_v1696_pin(v1696_root, ("Media",))
	with open(Path(v1696_root) / "Media.pk2", "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as media:
		index = tables.archive_index(media)

		# ================
		# read
		# ================
		def read(name):
			return tables.table_text(media, index, name.lower())

		regions = hunting_regions(tables.table_rows(read("regioncode.txt")))
		if not regions:
			raise ValueError(f"the v1.696 client has no {REGION_CODE} regions")
		later = {row[2]: row for shard in tables.table_rows(read("characterdata.txt")) for row in tables.table_rows(read(shard[0]))}
		placements = hunting_placements(read("npcpos.txt"), later, set(regions))
		codes = sorted({code for code, region, x, y, z in placements})
		refs = {}
		for code in codes:
			if code not in later:
				raise ValueError("placed monster has no v1.696 character row: " + code)
			if family_admits(code, later[code]):
				refs[code] = characters.convert_character(later[code])
		missing = sorted(set(codes) - set(refs))
		if missing:
			raise ValueError(f"hunting ground placements name unadmitted monsters: {missing}")
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
		population = synthesize_population(placements, refs)
		# Sorted by identity: the codename set has no stable order, and a
		# rebuild should write the same bytes.
		write_atomically(text_root / CHARACTER_SHARD, cap110.encode_rows(sorted(refs.values(), key=lambda row: int(row[1]))))
		write_atomically(text_root / SKILL_SHARD, cap110.encode_rows(added))
		manifest = tables.table_rows((text_root / "skilldata.txt").read_bytes())
		manifest = [r for r in manifest if r[0].lower() != SKILL_SHARD] + [[SKILL_SHARD]]
		write_atomically(text_root / "skilldata.txt", cap110.encode_rows(manifest))
		write_atomically(text_root / FIELD_FILE, (json.dumps(population) + "\n").encode())
		base_text = (text_root / "textdataname.txt").read_bytes()
		known = {r[1] for r in characters.translation_rows(base_text) if len(r) > 1}
		wanted = {r[5] for r in refs.values()} - known
		object_raw = b"\xff\xfe" + later_world_import.later_object_text(read).encode("utf-16-le")
		texts = [[r[0], r[1], r[2], "", "", r[5], r[4], r[7], r[8], r[9], ""] for r in
			(later_world_import.later_text_row(row) for row in characters.translation_rows(object_raw)) if len(r) >= 10 and r[1] in wanted]
		if wanted - {r[1] for r in texts}:
			raise ValueError("missing hunting ground monster names")
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
		owned = set(refs)
		info = [r[:characters.SKILL_EFFECT_KEPT] for r in tables.table_rows(read("skilleffect.txt"))
			if len(r) == characters.SKILL_EFFECT_COLUMNS and r[0] in owned and r[0] not in known_info]
		# Same rule for the characterinfo rows: a species no longer
		# referenced loses its row, one still referenced keeps it.
		owned_info = cap110.characterinfo_codenames(effect_text) & owned
		stale_info = set(prior.get("characterInfo", [])) - owned_info
		if stale_info:
			effect_text = cap110.drop_characterinfo_rows(effect_text, stale_info)
		write_atomically(effect_path, characters.skill_effect_section(effect_text.encode("utf-16"), info))
		summary = {"enabled": True,
			"characters": {c: int(r[1]) for c, r in refs.items()}, "skills": {r[3]: int(r[1]) for r in added},
			"families": {"field": {"nests": len(population["Rows"]),
				"regions": sorted({r["RegionID"] for r in population["Rows"]})}},
			"animationBases": sorted(set(prior.get("animationBases", [])) | {base for rows in selected.values() for base, _ in rows}),
			"characterInfo": sorted(owned_info | {r[0] for r in info}),
			"nameSymbols": sorted(({r[1] for r in characters.translation_rows(base_text) if len(r) > 1}
				| {r[1] for r in texts}) & {r[5] for r in refs.values()}),
			"sourceRegions": regions}
		write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode())
		return summary

#============================================================================
# The variant-side merge is later_world_import's (merge_world, on the
# Alexandria merge's catalog machinery); the population is add_monsters.
#============================================================================


# ================
# add_hunting2
# ================
def add_hunting2(native, out, v1696_root=None):
	"""
	Merge the second hunting grounds from the pinned v1.696 client
	into the variant: the regions' terrain remapped onto the merged
	tile catalog, their navmesh and availability bits, every Data
	file the native tree lacks, the regions' minimap tiles and
	names, and the monster population. Port-only, not native; off,
	exactly what the marker records is reaped - monster and region
	merge alike - and no archive is read.
	"""
	if not flag_enabled():
		later_world_import.reap_world(SPEC, native, out)
		return {"enabled": False}
	v1696_root = Path(v1696_root or os.environ.get(ROOT_ENV, ""))
	if not str(v1696_root):
		raise SystemExit(f"Pass the v1.696 client folder or set {ROOT_ENV}.")
	require_v1696_pin(v1696_root, ("Map", "Media", "Data"))
	with open(v1696_root / "Media.pk2", "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as media:
		index = tables.archive_index(media)
		regions = hunting_regions(tables.table_rows(tables.table_text(media, index, "regioncode.txt")))
	if not regions:
		raise ValueError(f"the v1.696 client has no {REGION_CODE} regions")
	return later_world_import.merge_world(SPEC, native, out, v1696_root, regions,
		lambda: add_monsters(v1696_root, native, out))


# ================
# main
# ================
def main():
	import argparse
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--v1696-root", default=os.environ.get(ROOT_ENV))
	parser.add_argument("--native-extracted", required=True)
	parser.add_argument("--output", required=True)
	args = parser.parse_args()
	# Off, add_monsters only removes its owned outputs and reads no archive.
	print(json.dumps(add_monsters(args.v1696_root, args.native_extracted, args.output), indent=2))


if __name__ == "__main__":
	main()
