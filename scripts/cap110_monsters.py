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

The admitted content is one declarative family table (FAMILIES below), one
row per family: the Alexandria field, the Pharaoh tombs, the Qin-Shi tomb
upper floors (SRO_CAP110_QINSHI, 91-99 content) and the temple instance bands
(SRO_CAP110_TEMPLE). Every family shares the one character/skill shard pair
and the one marker allocator, and writes one nest file of its own in the
Version-2 envelope; a family's switch off removes its nest file and its
exclusively-owned rows. Qinshi and temple need SRO_CAP110_MONSTERS: their
drop rules ride the shared cap-110 loot catalogue it builds (cap110_rewards),
so the build refuses them without it, as the Pharaoh precedent does.
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
# drop row comes from, as the author pinned it (docs/features/CAP110_PROGRESS.md,
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
# Not SRO_QINSHI_TOMB: that switch already gates the shipped tomb content
# and defaults on in the shard settings registry, so this family must not
# ride it - the default build would refuse itself, and flipping the cap-110
# field on would change live content as a side effect.
QINSHI_FLAG = "SRO_CAP110_QINSHI"
QINSHI_FILE = "cap110-qinshi.txt"
TEMPLE_FLAG = "SRO_CAP110_TEMPLE"
TEMPLE_FILE = "cap110-temple.txt"
# The temple bands are the database's 22 instanced worlds, 63-84.
TEMPLE_WORLDS = tuple(range(63, 85))

# One row per content family. Admission is a codename prefix set, the source
# worlds whose in-service nests claim species for the family, a level window
# (a sanity bound measured from the client table, not a spawn filter) and a
# grade set; NamedGrades carries one named exception (the field's Apis).
# CarrySpawnType: the temple's nests use the database's second spawn kind
# (512 of its 1616 claimed nests), so its rows carry it instead of refusing;
# the outdoor families' nests are all kind 0 and keep refusing any other.
FIELD_FAMILY = {"Name": "field", "Flag": FLAG, "File": POPULATION_FILE, "Prefixes": ("MOB_SD_",),
	"Worlds": (FIELD_WORLD,), "MinLevel": 100, "MaxLevel": MAX_LEVEL, "Grades": (0,),
	"NamedGrades": {APIS_CODENAME: APIS_SOURCE_GRADE}, "CarrySpawnType": False}
TOMB_FAMILY = {"Name": "pharaoh", "Flag": TOMB_FLAG, "File": TOMB_FILE, "Prefixes": ("MOB_SD_",),
	"Worlds": TOMB_WORLDS, "MinLevel": 100, "MaxLevel": MAX_LEVEL, "Grades": (0,) + TOMB_EXTRA_GRADES,
	"NamedGrades": {}, "CarrySpawnType": False}
QINSHI_FAMILY = {"Name": "qinshi", "Flag": QINSHI_FLAG, "File": QINSHI_FILE, "Prefixes": ("MOB_TQ_", "MOB_QT_"),
	"Worlds": (FIELD_WORLD,), "MinLevel": 81, "MaxLevel": MAX_LEVEL, "Grades": (0, 3, 6, 8),
	"NamedGrades": {}, "CarrySpawnType": False}
TEMPLE_FAMILY = {"Name": "temple", "Flag": TEMPLE_FLAG, "File": TEMPLE_FILE, "Prefixes": ("MOB_GOD_",),
	"Worlds": TEMPLE_WORLDS, "MinLevel": 35, "MaxLevel": 109, "Grades": (0, 1, 3, 6, 8),
	"NamedGrades": {}, "CarrySpawnType": True}
FAMILIES = (FIELD_FAMILY, TOMB_FAMILY, QINSHI_FAMILY, TEMPLE_FAMILY)


# ================
# family_enabled
# ================
def family_enabled(family):
	return os.environ.get(family["Flag"], "").lower() in tables.FLAG_ON


# ================
# tomb_enabled
# ================
def tomb_enabled():
	return family_enabled(TOMB_FAMILY)


# ================
# family_admits
#
# One character row against one family's admission window: its level (row
# 57) inside the window and its grade (row 15) in the set, or the family's
# named exception. Port-only, not native: the field's named exception is
# Apis's source grade, admitted without enabling other later uniques,
# dungeon populations or event variants.
# ================
def family_admits(family, code, row):
	if not family["MinLevel"] <= int(row[57]) <= family["MaxLevel"]:
		return False
	grade = int(row[15])
	return grade in family["Grades"] or family["NamedGrades"].get(code) == grade


# ================
# select_references
#
# Convert the later rows claimed by at least one family that admits them.
# claims maps codename -> the families claiming it (from the spawn rows,
# family_codes). A species two families claim keeps one row and one
# identity: the one allocator below sees the union. A claimed species with
# no later character row at all is a data-contract break, refused by name
# (it could never be converted).
# ================
def select_references(later, claims):
	refs = {}
	for code, claiming in claims.items():
		if code not in later:
			raise ValueError("claimed monster has no later character row: " + code)
		if any(family_admits(family, code, later[code]) for family in claiming):
			refs[code] = characters.convert_character(later[code])
	return refs


# ================
# family_strict_codes
#
# The claimed species a family's strict projection answers for: those
# whose later row sits inside the family's level window. Species outside
# the window are excluded by design (the Qin-Shi tomb's trap mechanisms
# share its prefixes and world), so their nests are skipped, never a
# silent shrink of the family's own content; inside the window, admission
# is all-or-nothing.
# ================
def family_strict_codes(family, codes, later):
	return {code for code in codes if code in later
		and family["MinLevel"] <= int(later[code][57]) <= family["MaxLevel"]}


# ================
# select_field_references
#
# The field (+ optional tomb) admission, the surface from before the
# family table; the tests pin it.
# ================
def select_field_references(later, codes, tomb_codes=frozenset()):
	claims = {code: [FIELD_FAMILY] + ([TOMB_FAMILY] if code in tomb_codes else []) for code in codes}
	return select_references(later, claims)


# ================
# monster_codes
#
# The codenames of the given prefixes the in-service spawn rows of the
# given worlds name.
# ================
def monster_codes(spawns, worlds, prefixes=("MOB_SD_",)):
	return {r["codename"] for r in spawns
		if r["game_world_id"] in worlds and r["service"] == 1 and r["codename"].startswith(prefixes)}


# ================
# family_codes
# ================
def family_codes(spawns, family):
	return monster_codes(spawns, family["Worlds"], family["Prefixes"])


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
# The nest rows of the given database worlds. A family that must be complete
# (strict, the tomb rule) refuses a claimed nest whose monster was not
# admitted instead of silently shrinking the population; unclaimed nests
# (another family's species sharing the world) are skipped, and the field
# has always skipped. codes=None claims every in-world nest (the historical
# tomb behaviour). A family that carries the spawn kind (the temple) keeps
# it on the row as SpawnType; the outdoor kinds refuse it.
# ================
def project_population(spawns, refs, tactics, hives, worlds=(FIELD_WORLD,), strict=False, codes=None,
	carry_spawn_type=False):
	rows = []
	for spawn in sorted(spawns, key=lambda row: row["nest_id"]):
		code = spawn["codename"]
		if spawn["game_world_id"] not in worlds or spawn["service"] != 1:
			continue
		if codes is not None and code not in codes:
			continue
		if code not in refs:
			if strict:
				raise ValueError("population monster was not admitted: " + code)
			continue
		if spawn["nest_flag"] or spawn["hive_flag"] or (spawn["spawn_type"] and not carry_spawn_type):
			raise ValueError("unsupported field spawn flags: " + code)
		policy = tactics[spawn["tactics_id"]]
		hive = hives[spawn["hive_id"]]
		if hive["dwOverwriteMaxTotalCount"] or hive["btKeepMonsterCountType"]:
			raise ValueError("field hive requires shared-cap projection: " + code)
		champion_id = policy["dwChampionTacticsID"]
		champion = tactics_controls(tactics[champion_id]) if champion_id else None
		row = {
			"Codename": code, "NestID": spawn["nest_id"], "WorldID": spawn["game_world_id"], "RegionID": spawn["region_id_unsigned"],
			"X": spawn["x"], "Y": spawn["y"], "Z": spawn["z"], "Radius": spawn["radius"],
			"GenerateRadius": spawn["generate_radius"], "MaxCount": spawn["max_total_count"],
			"RespawnDelayMinSec": spawn["delay_min"], "RespawnDelayMaxSec": spawn["delay_max"],
			"Respawn": bool(spawn["respawn"]), "InitialDir": spawn["direction_unsigned"],
			"ChampionGenPercentage": spawn["champion_percent"], "Controls": tactics_controls(policy),
			"Champion": champion,
		}
		if carry_spawn_type and spawn["spawn_type"]:
			row["SpawnType"] = spawn["spawn_type"]
		rows.append(row)
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
# drop_symboled_rows
#
# Table rows minus the ones whose second field names a dropped symbol.
# Both off-paths remove owned name rows this way: the full-off path with
# the marker's whole list, a family-off rebuild with only the stale part.
# ================
def drop_symboled_rows(rows, symbols):
	return [r for r in rows if len(r) < 2 or r[1] not in symbols]


# ================
# drop_characterinfo_rows
#
# A skill-effect text minus its owned characterinfo rows. Section state
# is tracked so a codename outside the characterinfo section survives.
# ================
def drop_characterinfo_rows(effect_text, owned):
	section, lines = None, []
	for line in effect_text.splitlines():
		header = effects.section_name(line)
		if header is not None:
			section = header
		if section == "characterinfo" and line.split("\t")[0] in owned:
			continue
		lines.append(line)
	return "\r\n".join(lines) + "\r\n"


# ================
# characterinfo_codenames
#
# The codenames a skill-effect text's characterinfo section describes.
# ================
def characterinfo_codenames(effect_text):
	section, codes = None, set()
	for line in effect_text.splitlines():
		header = effects.section_name(line)
		if header is not None:
			section = header
		elif section == "characterinfo" and line:
			codes.add(line.split("\t")[0])
	return codes


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
	enabled = [family for family in FAMILIES if family_enabled(family)]
	field = FIELD_FAMILY in enabled
	for family in enabled:
		if family is not FIELD_FAMILY and not field:
			# The family's drop rules ride the cap-110 loot catalogue the
			# field flag builds (cap110_rewards), so the build refuses it
			# without the field, as the server does for the Pharaoh tombs.
			raise ValueError(f"{family['Flag']} requires {FLAG}")
	if not field:
		if prior:
			effect_path = text_root / "skilleffect.txt"
			effect_text = effects.remove_animation_bases(tables.decode_table(effect_path.read_bytes()), set(prior.get("animationBases", [])))
			write_atomically(effect_path, b"\xff\xfe" + drop_characterinfo_rows(effect_text, set(prior.get("characterInfo", []))).encode("utf-16-le"))
			text_path = text_root / "textdataname.txt"
			write_atomically(text_path, encode_rows(drop_symboled_rows(characters.translation_rows(text_path.read_bytes()), set(prior.get("nameSymbols", [])))))
		for name in (CHARACTER_SHARD, SKILL_SHARD, *(family["File"] for family in FAMILIES), "cap110-equipment.txt"):
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
		claims = {}
		claimed = {}
		for family in enabled:
			claimed[family["Name"]] = family_codes(spawns, family)
			for code in claimed[family["Name"]]:
				claims.setdefault(code, []).append(family)
		refs = select_references(later, claims)
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
		populations = {}
		for family in enabled:
			if family is FIELD_FAMILY:
				populations[family["Name"]] = project_population(spawns, refs, tactics, hives, family["Worlds"])
			elif family is TOMB_FAMILY:
				populations[family["Name"]] = project_population(spawns, refs, tactics, hives, family["Worlds"],
					strict=True)
			else:
				# The newer families share their worlds and prefixes with
				# mechanism species outside their windows, so their
				# strictness answers for the in-window claims only.
				populations[family["Name"]] = project_population(spawns, refs, tactics, hives, family["Worlds"],
					strict=True, codes=family_strict_codes(family, claimed[family["Name"]], later),
					carry_spawn_type=family["CarrySpawnType"])
		population = populations["field"]
		# Sorted by identity: the codename set has no stable order, and a
		# rebuild should write the same bytes.
		write_atomically(text_root / CHARACTER_SHARD, encode_rows(sorted(refs.values(), key=lambda row: int(row[1]))))
		write_atomically(text_root / SKILL_SHARD, encode_rows(added))
		manifest = tables.table_rows((text_root / "skilldata.txt").read_bytes())
		manifest = [r for r in manifest if r[0].lower() != SKILL_SHARD] + [[SKILL_SHARD]]
		write_atomically(text_root / "skilldata.txt", encode_rows(manifest))
		for family in FAMILIES:
			path = text_root / family["File"]
			if family["Name"] in populations:
				write_atomically(path, (json.dumps(populations[family["Name"]]) + "\n").encode())
			else:
				path.unlink(missing_ok=True)
		base_text = (text_root / "textdataname.txt").read_bytes()
		known = {r[1] for r in characters.translation_rows(base_text) if len(r) > 1}
		wanted = {r[5] for r in refs.values()} - known
		texts = [[r[0], r[1], r[2], "", "", r[5], r[4], r[7], r[8], r[9], ""] for r in characters.translation_rows(read("textdata_object.txt")) if len(r) >= 10 and r[1] in wanted]
		if wanted - {r[1] for r in texts}:
			raise ValueError("missing field monster names")
		# Name rows a family that is off this build owned leave with it: the
		# marker's list is the previous build's owned set; a symbol stays
		# owned while a current reference still names it.
		stale_names = set(prior.get("nameSymbols", [])) - {r[5] for r in refs.values()}
		if stale_names:
			base_text = encode_rows(drop_symboled_rows(characters.translation_rows(base_text), stale_names))
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
		effect_prefixes = tuple(prefix for family in enabled for prefix in family["Prefixes"])
		info = [r[:characters.SKILL_EFFECT_KEPT] for r in tables.table_rows(read("skilleffect.txt"))
			if len(r) == characters.SKILL_EFFECT_COLUMNS and r[0].startswith(effect_prefixes) and r[0] not in known_info]
		# Same rule for the characterinfo rows: a species no longer
		# referenced loses its row, one still referenced keeps it.
		owned_info = characterinfo_codenames(effect_text) & set(refs)
		stale_info = set(prior.get("characterInfo", [])) - owned_info
		if stale_info:
			effect_text = drop_characterinfo_rows(effect_text, stale_info)
		write_atomically(effect_path, characters.skill_effect_section(effect_text.encode("utf-16"), info))
		from cap110_monster_loot import stage_field_equipment
		loot_count = stage_field_equipment(text_root, source_root)
		summary = {"enabled": True, "characters": {c: int(r[1]) for c, r in refs.items()}, "skills": {r[3]: int(r[1]) for r in added}, "nests": len(population["Rows"]), "regions": sorted({r["RegionID"] for r in population["Rows"]}),
		"animationBases": sorted(set(prior.get("animationBases", [])) | {base for rows in selected.values() for base, _ in rows}),
		# Owned rows are this build's set, not a union with the prior
		# marker's: a family switched off must shrink both lists so the
		# next off-path knows exactly what to reap. The rows this build
		# itself writes (info) enrol in the same build - on a first
		# enabled build the pre-existing text is the native copy, so
		# owned_info alone would record none of them and a later off
		# build could never reap them. animationBases stays a union -
		# it is the reap list for shared effect vocabulary, which only
		# the full-off path removes.
		"characterInfo": sorted(owned_info | {r[0] for r in info}),
			"nameSymbols": sorted(({r[1] for r in characters.translation_rows(base_text) if len(r) > 1}
				| {r[1] for r in texts}) & {r[5] for r in refs.values()})}
		summary["equipmentLootRows"] = loot_count
		tomb_population = populations.get("pharaoh")
		summary["tombNests"] = len(tomb_population["Rows"]) if tomb_population else 0
		summary["tombRegions"] = sorted({r["RegionID"] for r in tomb_population["Rows"]}) if tomb_population else []
		# Per-family nest and region report: consumers (the tomb floors, the
		# temple bands) read their family's slice from here; the regions
		# follow the projected nests, never a pinned list.
		summary["families"] = {family["Name"]: {
			"nests": len(populations[family["Name"]]["Rows"]),
			"regions": sorted({r["RegionID"] for r in populations[family["Name"]]["Rows"]})}
			for family in enabled}
		summary["sourceBackupSha256"] = read_json(source_root / "summary.json")["backup_sha256"]
		# The drop assignments are optional (#1745 D3): an export without
		# them builds the native loot posture, and the marker says which.
		from cap110_monster_loot import ASSIGNMENTS_NAME, has_assignments
		summary["equipmentLoot"] = "vsro" if has_assignments(source_root) else "native"
		snapshot = ["all-spawns.json", "Tab_RefNest.json", "Tab_RefTactics.json", "Tab_RefHive.json",
			"vsro-outdoor-monster-skills.json", "vsro-outdoor-drop-classes.json", "vsro-outdoor-rare-classes.json"]
		if has_assignments(source_root):
			snapshot.append(ASSIGNMENTS_NAME + ".json")
		summary["snapshotHashes"] = {name: sro_pk2.digest((source_root / name).read_bytes()) for name in snapshot}
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
