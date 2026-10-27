"""
===========================================================================
cap110_monsters.py - optional, database-authored Alexandria field population.

Port-only, not native. The supplied exports are data, never executable input.
Main-world ordinary SD monsters and Apis join this overlay. Numeric references
are resolved by codename; player skill identities and native records win.

SRO_PHARAOH_TOMB (needs SRO_CAP110_MONSTERS) additionally admits the Pharaoh
Tomb 1-3 monsters, database worlds 57-59, including their elite (grade 6) and
unique (grade 8) members. They share the field overlay's character and skill
shards and its marker, so one allocator keeps every identity stable; only the
nests go to a file of their own (cap110-tomb.txt), which the server reads
behind its own flag.
===========================================================================
"""
import argparse
import json
import mmap
import os
from pathlib import Path

import alexandria_npcs as characters
import cap110_skill_database as skill_database
import cap110_skill_effects as effects
from cap110_skills import MAX_ID
import import_vsro_alexandria as tables
import sro_pk2
from prepare_client_resources import write_atomically

FLAG = "SRO_CAP110_MONSTERS"
SOURCE_ENV = "SRO_CAP110_MONSTER_DATABASE"
MARKER = ".opensro-cap110-monsters.json"
CHARACTER_SHARD = "characterdata_120000.txt"
SKILL_SHARD = "skilldata_120000.txt"
POPULATION_FILE = "cap110-monsters.txt"
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA
MAX_LEVEL = 110
APIS_CODENAME = "MOB_SD_APIS"
APIS_SOURCE_GRADE = 8
# The VSRO test-shard backup every imported nest, tactic, monster skill and
# drop row comes from, as the author pinned it (docs/CAP110_PROGRESS.md,
# "Database export pin"). The export records this hash in summary.json.
SOURCE_BACKUP_NAME = "SRO_VT_SHARD.bak"
SOURCE_BACKUP_SHA256 = "b4d85a043ebce36c8704768dce0dfa63ff9418c8489241d8441b7902e28fa37a"
TOMB_FLAG = "SRO_PHARAOH_TOMB"
TOMB_FILE = "cap110-tomb.txt"
FIELD_WORLD = 1
# The three Pharaoh Tombs are database worlds of their own, but ordinary
# outdoor regions: the port runs each as a persistent shared area.
TOMB_WORLDS = (57, 58, 59)
TOMB_EXTRA_GRADES = (6, 8)


# ================
# tomb_enabled
# ================
def tomb_enabled():
	return os.environ.get(TOMB_FLAG, "").lower() in tables.FLAG_ON


# ================
# select_field_references
#
# Port-only, not native: explicitly admit Apis's source grade without
# enabling other later uniques, dungeon populations or event variants. The
# Pharaoh Tomb codenames (tomb_codes) additionally admit elite and unique.
# ================
def select_field_references(later, codes, tomb_codes=frozenset()):
	return {code: characters.convert_character(later[code]) for code in codes
		if 100 <= int(later[code][57]) <= MAX_LEVEL
		and (int(later[code][15]) == 0
			or (code == APIS_CODENAME and int(later[code][15]) == APIS_SOURCE_GRADE)
			or (code in tomb_codes and int(later[code][15]) in TOMB_EXTRA_GRADES))}


# ================
# monster_codes
#
# The MOB_SD_ codenames the in-service spawn rows of the given worlds name.
# ================
def monster_codes(spawns, worlds):
	return {r["codename"] for r in spawns
		if r["game_world_id"] in worlds and r["service"] == 1 and r["codename"].startswith("MOB_SD_")}


# ================
# outdoor_regions
#
# The sectors of the field nests, plus the tomb nests when tomb is true.
# ================
def outdoor_regions(source_root, tomb=False):
	spawns = read_json(Path(source_root) / "all-spawns.json")
	worlds = (FIELD_WORLD,) + (TOMB_WORLDS if tomb else ())
	return {(r["region_id_unsigned"] & 255, r["region_id_unsigned"] >> 8) for r in spawns
		if r["game_world_id"] in worlds and r["service"] == 1 and r["codename"].startswith("MOB_SD_")}


# ================
# require_pinned_export
#
# The export must record the pinned backup's hash; one made from another
# database is refused before anything is staged.
# ================
def require_pinned_export(source_root):
	recorded = str(read_json(Path(source_root) / "summary.json").get("backup_sha256", "")).lower()
	if recorded != SOURCE_BACKUP_SHA256:
		raise ValueError(
			f"database export {source_root} records backup {recorded or 'none'}, "
			f"not the pinned {SOURCE_BACKUP_NAME} ({SOURCE_BACKUP_SHA256})"
		)


# ================
# read_json
# ================
def read_json(path):
	return json.loads(path.read_text(encoding="utf-8"))


# ================
# encode_rows
# ================
def encode_rows(rows):
	return b"\xff\xfe" + characters.table_text(rows).encode("utf-16-le")


# ================
# tactics_controls
#
# Preserve the complete existing TacticsControls contract, including authored
# zeroes. Source identities are evidence only, not runtime object references.
# ================
def tactics_controls(row):
	fields = {
		"ID": "dwTacticsID", "ObjectID": "dwObjID", "AIQoS": "btAIQoS",
		"MaxStamina": "nMaxStamina", "StaminaVariance": "btMaxStaminaVariance",
		"SightRange": "nSightRange", "AggressType": "btAggressType", "AggressData": "AggressData",
		"ChangeTarget": "btChangeTarget", "HelpRequest": "btHelpRequestTo", "HelpResponse": "btHelpResponseTo",
		"BattleStyle": "btBattleStyle", "BattleStyleData": "BattleStyleData", "DiversionBasis": "btDiversionBasis",
		"DiversionKeepBasis": "btDiversionKeepBasis", "KeepDistance": "btKeepDistance",
		"KeepDistanceData": "KeepDistanceData", "TraceType": "btTraceType", "TraceBoundary": "btTraceBoundary",
		"TraceData": "TraceData", "HomingType": "btHomingType", "HomingData": "HomingData",
		"AggressOnHoming": "btAggressTypeOnHoming", "FleeType": "btFleeType",
		"ChampionID": "dwChampionTacticsID", "Flags": "AdditionOptionFlag",
	}
	result = {key: row[source] for key, source in fields.items()}
	for name in ("DiversionBasisData", "DiversionKeepBasisData"):
		result[name] = [row[name + str(index)] for index in range(1, 9)]
	return result


# ================
# project_population
#
# The nest rows of the given database worlds. A world that must be complete
# (strict, the tomb) refuses a nest whose monster was not admitted instead of
# silently shrinking the population; the field has always skipped them.
# ================
def project_population(spawns, refs, tactics, hives, worlds=(FIELD_WORLD,), strict=False):
	rows = []
	for spawn in sorted(spawns, key=lambda row: row["nest_id"]):
		code = spawn["codename"]
		if spawn["game_world_id"] not in worlds or spawn["service"] != 1:
			continue
		if code not in refs:
			if strict:
				raise ValueError("population monster was not admitted: " + code)
			continue
		if any(spawn[key] != 0 for key in ("nest_flag", "hive_flag", "spawn_type")):
			raise ValueError("unsupported field spawn flags: " + code)
		policy = tactics[spawn["tactics_id"]]
		hive = hives[spawn["hive_id"]]
		if hive["dwOverwriteMaxTotalCount"] or hive["btKeepMonsterCountType"]:
			raise ValueError("field hive requires shared-cap projection: " + code)
		champion_id = policy["dwChampionTacticsID"]
		champion = tactics_controls(tactics[champion_id]) if champion_id else None
		rows.append({
			"Codename": code, "NestID": spawn["nest_id"], "WorldID": spawn["game_world_id"], "RegionID": spawn["region_id_unsigned"],
			"X": spawn["x"], "Y": spawn["y"], "Z": spawn["z"], "Radius": spawn["radius"],
			"GenerateRadius": spawn["generate_radius"], "MaxCount": spawn["max_total_count"],
			"RespawnDelayMinSec": spawn["delay_min"], "RespawnDelayMaxSec": spawn["delay_max"],
			"Respawn": bool(spawn["respawn"]), "InitialDir": spawn["direction_unsigned"],
			"ChampionGenPercentage": spawn["champion_percent"], "Controls": tactics_controls(policy),
			"Champion": champion,
		})
	# Format 2: every row carries its source world, so the server's tomb
	# population joins its own per-tomb world instead of the default one.
	return {"Version": 2, "Rows": rows}


# ================
# allocate_identities
#
# Reserve every published identity before allocating additions. Source order
# may insert a new monster or dependency before an existing published row.
# ================
def allocate_identities(occupied, codes, previous):
	occupied = set(occupied)
	values = list(previous.values())
	if any(type(value) is not int or not 0 < value <= MAX_ID for value in values):
		raise ValueError("invalid published monster identity")
	retained = set(values)
	if len(retained) != len(values) or retained & occupied:
		raise ValueError("published monster identities collide")
	if len(codes) != len(set(codes)):
		raise ValueError("duplicate monster codename")
	occupied |= retained
	next_id = max(occupied | {0}) + 1
	identities = {}
	for code in codes:
		if code in previous:
			identities[code] = previous[code]
			continue
		if next_id > MAX_ID:
			raise ValueError("monster identity space exhausted")
		identities[code] = next_id
		next_id += 1
	return identities


# ================
# add_native_client_skills
#
# A monster may use a basic skill (SKILL_PUNCH_01) the database projection
# does not list because it is not monster-specific. When the later client's own
# row for that ID names a native skill, it joins by codename and the native
# record wins. Anything else stays missing, and merge_skills refuses it.
# load_client reads the later client's skill rows by ID, and only when needed.
# ================
def add_native_client_skills(source, needed, native, load_client):
	missing = sorted(set(needed) - {int(row[1]) for row in source})
	if not missing:
		return source
	native_codes = {row[3] for row in native if row[0] == "1"}
	client = load_client()
	return source + [client[identity] for identity in missing if identity in client and client[identity][3] in native_codes]


# ================
# merge_skills
#
# MSKILL originals and chain links are real skill IDs. Resolve the closure
# before allocating anything; never assume two versions share an identity.
# ================
def merge_skills(native, source, needed, previous):
	by_code = {row[3]: row for row in native if row[0] == "1"}
	by_id = {int(row[1]): row for row in source}
	selected, pending = {}, list(needed)
	while pending:
		identity = int(pending.pop())
		if not identity or identity in selected:
			continue
		if identity not in by_id:
			raise ValueError(f"missing monster skill dependency {identity}")
		row = by_id[identity]
		selected[identity] = row
		if row[3] not in by_code:
			pending.extend(int(row[column]) for column in (6, 9) if int(row[column]))
	occupied = {int(row[1]) for row in native}
	identity_map, added = {}, []
	new_codes = [row[3] for _, row in sorted(selected.items()) if row[3] not in by_code]
	allocated = allocate_identities(occupied, new_codes, previous)
	for source_id, row in sorted(selected.items()):
		code = row[3]
		if code in by_code:
			identity_map[source_id] = int(by_code[code][1])
			continue
		identity_map[source_id] = allocated[code]
	for source_id, row in sorted(selected.items()):
		if row[3] in by_code:
			continue
		converted = list(row)
		converted[1] = str(identity_map[source_id])
		for column in (6, 9):
			if int(row[column]):
				converted[column] = str(identity_map[int(row[column])])
		if any(int(row[column]) for column in (40, 41, 42)):
			raise ValueError("monster skill unexpectedly has player prerequisites")
		added.append(converted)
	return added, identity_map


# ================
# add_monsters
# ================
def add_monsters(vsro_root, native, output, source_root=None):
	native, output = Path(native).resolve(), Path(output).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("monster overlay must be separate from native extraction")
	marker = output / MARKER
	prior = read_json(marker) if marker.exists() else {}
	text_root = output / TEXT_ROOT
	tomb = tomb_enabled()
	field = os.environ.get(FLAG, "").lower() in tables.FLAG_ON
	if tomb and not field:
		# The tomb's equipment drops come from the cap-110 loot catalogue the
		# field flag builds, so the server refuses it without the field too.
		raise ValueError(f"{TOMB_FLAG} requires {FLAG}")
	if not field:
		if prior:
			effect_path = text_root / "skilleffect.txt"
			effect_text = effects.remove_animation_bases(tables.decode_table(effect_path.read_bytes()), set(prior.get("animationBases", [])))
			owned_info = set(prior.get("characterInfo", []))
			section, lines = None, []
			for line in effect_text.splitlines():
				header = effects.section_name(line)
				if header is not None:
					section = header
				if section == "characterinfo" and line.split("\t")[0] in owned_info:
					continue
				lines.append(line)
			write_atomically(effect_path, b"\xff\xfe" + ("\r\n".join(lines) + "\r\n").encode("utf-16-le"))
			text_path = text_root / "textdataname.txt"
			owned_names = set(prior.get("nameSymbols", []))
			write_atomically(text_path, encode_rows([r for r in characters.translation_rows(text_path.read_bytes()) if len(r) < 2 or r[1] not in owned_names]))
		for name in (CHARACTER_SHARD, SKILL_SHARD, POPULATION_FILE, TOMB_FILE, "cap110-equipment.txt"):
			(text_root / name).unlink(missing_ok=True)
		manifest = text_root / "skilldata.txt"
		if manifest.exists():
			write_atomically(manifest, encode_rows([r for r in tables.table_rows(manifest.read_bytes()) if r[0].lower() != SKILL_SHARD]))
		marker.unlink(missing_ok=True)
		return {"enabled": False}
	source_root = Path(source_root or os.environ[SOURCE_ENV])
	require_pinned_export(source_root)
	spawns = read_json(source_root / "all-spawns.json")
	nests = {r["dwNestID"]: r for r in read_json(source_root / "Tab_RefNest.json")}
	for row in spawns:
		row["champion_percent"] = nests[row["nest_id"]]["nChampionGenPercentage"]
	tactics = {r["dwTacticsID"]: r for r in read_json(source_root / "Tab_RefTactics.json")}
	hives = {r["dwHiveID"]: r for r in read_json(source_root / "Tab_RefHive.json")}
	source_skills = skill_database.project_database_rows(read_json(source_root / "vsro-outdoor-monster-skills.json"))
	with open(Path(vsro_root) / "Media.pk2", "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as media:
		index = tables.archive_index(media)
		# ================
		# read
		# ================
		def read(name):
			return tables.table_text(media, index, name.lower())
		later = {row[2]: row for shard in tables.table_rows(read("characterdata.txt")) for row in tables.table_rows(read(shard[0]))}
		tomb_codes = monster_codes(spawns, TOMB_WORLDS) if tomb else set()
		codes = monster_codes(spawns, (FIELD_WORLD,)) | tomb_codes
		refs = select_field_references(later, codes, tomb_codes)
		if not refs:
			raise ValueError("no ordinary level 100-110 Alexandria field monsters")
		native_chars = [r for p in text_root.glob("characterdata_*.txt") if p.name != CHARACTER_SHARD for r in tables.table_rows(p.read_bytes())]
		occupied = {int(r[1]) for r in native_chars}
		allocated = allocate_identities(occupied, sorted(refs), prior.get("characters", {}))
		for code, row in sorted(refs.items()):
			row[1] = str(allocated[code])
		native_skills = [r for p in text_root.glob("skilldata_*.txt") if p.name != SKILL_SHARD for r in tables.table_rows(p.read_bytes()) if len(r) == 118]
		needed = {int(s) for r in refs.values() for s in r[89:99] if int(s)}
		# ================
		# load_client_skills
		# ================
		def load_client_skills():
			return {int(r[1]): r for shard in tables.table_rows(read("skilldata.txt"))
				for r in tables.table_rows(read(shard[0])) if len(r) > 3 and r[1].isdigit()}
		source_skills = add_native_client_skills(source_skills, needed, native_skills, load_client_skills)
		added, skill_map = merge_skills(native_skills, source_skills, needed, prior.get("skills", {}))
		for row in refs.values():
			row[89:99] = [str(skill_map[int(s)]) if int(s) else "0" for s in row[89:99]]
		population = project_population(spawns, refs, tactics, hives)
		tomb_population = project_population(spawns, refs, tactics, hives, TOMB_WORLDS, strict=True) if tomb else None
		# Sorted by identity: the codename set has no stable order, and a
		# rebuild should write the same bytes.
		write_atomically(text_root / CHARACTER_SHARD, encode_rows(sorted(refs.values(), key=lambda row: int(row[1]))))
		write_atomically(text_root / SKILL_SHARD, encode_rows(added))
		manifest = tables.table_rows((text_root / "skilldata.txt").read_bytes())
		manifest = [r for r in manifest if r[0].lower() != SKILL_SHARD] + [[SKILL_SHARD]]
		write_atomically(text_root / "skilldata.txt", encode_rows(manifest))
		write_atomically(text_root / POPULATION_FILE, (json.dumps(population) + "\n").encode())
		if tomb:
			write_atomically(text_root / TOMB_FILE, (json.dumps(tomb_population) + "\n").encode())
		else:
			(text_root / TOMB_FILE).unlink(missing_ok=True)
		base_text = (text_root / "textdataname.txt").read_bytes()
		known = {r[1] for r in characters.translation_rows(base_text) if len(r) > 1}
		wanted = {r[5] for r in refs.values()} - known
		texts = [[r[0], r[1], r[2], "", "", r[5], r[4], r[7], r[8], r[9], ""] for r in characters.translation_rows(read("textdata_object.txt")) if len(r) >= 10 and r[1] in wanted]
		if wanted - {r[1] for r in texts}:
			raise ValueError("missing field monster names")
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
		info = [r[:characters.SKILL_EFFECT_KEPT] for r in tables.table_rows(read("skilleffect.txt"))
			if len(r) == characters.SKILL_EFFECT_COLUMNS and r[0].startswith("MOB_SD_") and r[0] not in known_info]
		write_atomically(effect_path, characters.skill_effect_section(effect_text.encode("utf-16"), info))
		from cap110_monster_loot import stage_field_equipment
		loot_count = stage_field_equipment(text_root, source_root)
		summary = {"enabled": True, "characters": {c: int(r[1]) for c, r in refs.items()}, "skills": {r[3]: int(r[1]) for r in added}, "nests": len(population["Rows"]), "regions": sorted({r["RegionID"] for r in population["Rows"]}),
			"animationBases": sorted(set(prior.get("animationBases", [])) | {base for rows in selected.values() for base, _ in rows}),
			"characterInfo": sorted(set(prior.get("characterInfo", [])) | {r[0] for r in info}),
			"nameSymbols": sorted(set(prior.get("nameSymbols", [])) | {r[1] for r in texts})}
		summary["equipmentLootRows"] = loot_count
		summary["tombNests"] = len(tomb_population["Rows"]) if tomb else 0
		summary["tombRegions"] = sorted({r["RegionID"] for r in tomb_population["Rows"]}) if tomb else []
		summary["sourceBackupSha256"] = read_json(source_root / "summary.json")["backup_sha256"]
		summary["snapshotHashes"] = {name: sro_pk2.digest((source_root / name).read_bytes()) for name in (
			"all-spawns.json", "Tab_RefNest.json", "Tab_RefTactics.json", "Tab_RefHive.json",
			"vsro-outdoor-monster-skills.json", "vsro-outdoor-drop-assignments.json",
			"vsro-outdoor-drop-classes.json", "vsro-outdoor-rare-classes.json")}
		write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode())
		return summary


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--vsro-root", required=True)
	parser.add_argument("--native-extracted", required=True)
	parser.add_argument("--output", required=True)
	parser.add_argument("--source-root", required=True)
	args = parser.parse_args()
	# Off, add_monsters only removes its owned outputs and reads no archive.
	if os.environ.get(FLAG, "").lower() in tables.FLAG_ON:
		tables.require_vsro_pin(args.vsro_root, ("Media",))
	print(json.dumps(add_monsters(args.vsro_root, args.native_extracted, args.output, args.source_root), indent=2))


if __name__ == "__main__":
	main()
