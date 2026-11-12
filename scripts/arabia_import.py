"""
===========================================================================

arabia_import.py - the Arabia continent from the v1.696 client + ISRO-R

PORT-ONLY, NOT NATIVE. Off unless SRO_ARABIA is "1", "on" or "true".
Called by build_alexandria_variant.py after the Sky Temple fields,
inside the Alexandria variant build; the server reads the population
only while its own SRO_ARABIA is on. Build and server must agree.

The sources are the operator's own, never committed: the v1.696 client
(SRO_V1696_ROOT, default inputs/sro_1696 beside the game root) for the
continent's regions, terrain, models and character rows, and the
ISRO-R nest extraction (SRO_ARABIA_NESTS, default
../research/investigations/later-sources-3247/arabia-population.json)
for the authored nests, tactics and champions. When
scripts/v1696_archive_pins.json exists (the mall-avatar import's pin
file, #3250) the client archives are verified against it before
anything reads them; the nest file's sha256 is recorded in the marker
on every run.

What the sources author, and what this import does with them:

	regions   every RN_ARABIA_* region of the client regioncode (264:
	          town, the three fields, Petra, the Baghdad dungeon and
	          the garden). The ISRO-R backup binds only 173 of them;
	          the reconciliation is explicit - terrain, navmesh, tile
	          and object catalogues merge for every sector the client
	          built (263: the boss lair's sector is a region code
	          with no terrain or navmesh anywhere, skipped and
	          logged), nests land only where the authored population
	          names them.
	monsters  the 51 MOB_AR_*/MOB_ARABIA_* kinds the authored nests
	          place, converted from the client's characterdata at
	          their authored levels 120-130. Kinds the nests never
	          place stay out, as the Sky Temple's unplaced kinds do.
	nests     the extraction's 1,656 authored rows, projected into the
	          population envelope the cap-110 families use: the town
	          and fields in world 1, the dungeon (spawn kind 1) in
	          world 112. The two uniques whose points share an
	          overwrite cap (MOB_ARABIA_KARKADANN, MOB_ARABIA_MUSTAFA)
	          project onto the loader's hive key with the authored
	          cap, the shared-cap projection the extraction asked
	          for. One family's inverted delay pair (7, 1) authors a
	          fixed delay - the native draw widens only upward - and
	          is stored as (min, min), the identical draw.
	npcs      Baghdad's authored NPC rows, plus the NPC placements the
	          client's own npcpos names inside the continent, riding
	          the NPC pass: character rows, npcpos standings, talk
	          rows and name texts; the talk strings join
	          textquest.txt, the table the native client keeps its
	          own SN_NPC_*_BS/_PS rows in. A placement whose kind no text
	          table of either client names is skipped and logged:
	          the later client plants event props (a circus bear)
	          whose name keys exist nowhere. Their shops do not
	          ride: a store sells only when it is a native one (the
	          Alexandria rule), and Baghdad's stores are not.
	gates     the dungeon chain stands as authored (its town portal
	          building, absent from the source, is authored beside
	          its arrival); travel into Baghdad is authored at Jangan,
	          see arabia_gates.py. Worlds 113-115 (boss lair, Petra,
	          garden) carry no nests and no gates in either source and
	          stay unregistered; their regions import with the map.

Turning the flag off removes the shards, the population, the NPC
standings and talk rows, the copied files and the owned table rows
(the marker lists them); the gate tables and catalogues need no
reaping because the variant build rewrites them from the native
tables on every run.

===========================================================================
"""
import argparse
import hashlib
import json
import os
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import alexandria_npcs as characters  # noqa: E402
import build_alexandria_variant as variant  # noqa: E402
import cap110_monsters as monsters  # noqa: E402
import cap110_skill_effects as effects  # noqa: E402
import import_vsro_alexandria as tables  # noqa: E402
import sro_pk2  # noqa: E402
from alexandria_map_tables import native_text_row  # noqa: E402
from arabia_gates import prepare_gates  # noqa: E402
from prepare_client_resources import safe_target, write_atomically  # noqa: E402
from skytemple_import import (  # noqa: E402
	copy_resource_closure,
	later_object_texts,
	merge_object_catalog_remap,
	o2_object_ids,
	open_archive,
	patch_o2_objects,
	source_root,
	verify_archives,
)
from sro_paths import MAIN_CHECKOUT_ROOT  # noqa: E402

FLAG = "SRO_ARABIA"
SOURCE_ENV = "SRO_V1696_ROOT"
NESTS_ENV = "SRO_ARABIA_NESTS"
MARKER = ".opensro-arabia.json"
# The 200000 identity band: 120000 is the cap-110 family's, 130000 the
# pet import's (140000/150000 held for its later sources), 160000 the
# Sky Temple's, 170000 Jupiter's, 180000 the second hunting grounds',
# 190000 the GOD temples'. One importer owns one shard file in a build.
CHARACTER_SHARD = "characterdata_200000.txt"
SKILL_SHARD = "skilldata_200000.txt"
POPULATION_FILE = "arabia-monsters.txt"
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA

REGION_PREFIX = "RN_ARABIA_"
REGION_FAMILIES = ("TOWN", "FIELD_01", "FIELD_02", "FIELD_03", "PETRA", "DUNGEON", "GARDEN")
KIND_PREFIXES = ("MOB_AR_", "MOB_ARABIA_")
FIRST_LEVEL = 120
LAST_LEVEL = 130
FIELD_WORLD = 1
DUNGEON_WORLD = 112
# The population envelope's tactics shape (cap110_monsters.tactics_controls);
# an extraction row naming anything else is a contract break, refused.
CONTROL_KEYS = {
	"ID", "ObjectID", "AIQoS", "MaxStamina", "StaminaVariance", "SightRange",
	"AggressType", "AggressData", "ChangeTarget", "HelpRequest", "HelpResponse",
	"BattleStyle", "BattleStyleData", "DiversionBasis", "DiversionKeepBasis",
	"KeepDistance", "KeepDistanceData", "TraceType", "TraceBoundary", "TraceData",
	"HomingType", "HomingData", "AggressOnHoming", "FleeType", "ChampionID",
	"Flags", "DiversionBasisData", "DiversionKeepBasisData",
}
ROW_KEYS = {
	"Codename", "NestID", "WorldID", "RegionID", "X", "Y", "Z", "Radius",
	"GenerateRadius", "MaxCount", "RespawnDelayMinSec", "RespawnDelayMaxSec",
	"Respawn", "InitialDir", "SpawnType", "ChampionGenPercentage", "Controls",
	"Champion", "HiveKey", "HiveMaxCount",
}


# ================
# enabled
# ================
def enabled(env=os.environ):
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON


# ================
# nests_path
#
# The authored nest extraction: SRO_ARABIA_NESTS, else the research
# extraction beside the game root (the sibling folder the sky temple
# source default mirrors: operator data, never committed).
# ================
def nests_path(env=os.environ):
	configured = env.get(NESTS_ENV, "").strip()
	if configured:
		return Path(configured)
	return (MAIN_CHECKOUT_ROOT.parent / "research" / "investigations"
		/ "later-sources-3247" / "arabia-population.json")


# ================
# continent_regions
#
# {region id: family} of the RN_ARABIA_* regioncode rows. Every family
# the issue names must be present; an unknown suffix is a contract
# break, refused by name rather than silently dropped.
# ================
def continent_regions(regioncode_rows):
	regions = {}
	for row in regioncode_rows:
		if len(row) < 3 or not row[1].isdigit():
			continue
		code = row[2].strip()
		if not code.startswith(REGION_PREFIX):
			continue
		suffix = code[len(REGION_PREFIX):]
		family = next((name for name in REGION_FAMILIES
			if suffix == name or suffix.startswith(name + "_")), None)
		if family is None:
			raise ValueError(f"v1.696 regioncode names an unknown Arabia family: {code}")
		regions[int(row[1])] = family
	missing = [name for name in REGION_FAMILIES if name not in regions.values()]
	if missing:
		raise ValueError(f"v1.696 regioncode lacks Arabia families: {missing}")
	return regions


# ================
# project_row
#
# One authored nest row into the population envelope (cap110.go's
# cap110Nest keys, nothing else). The extraction's bookkeeping columns
# (Continent, NestFlag, Hive) are consumed here: a nest flag or an
# unprojectable hive refuses the row by id; an overwrite cap projects
# onto the loader's hive key under the kind's own codename.
# ================
def project_row(row, regions):
	for key in ("Codename", "NestID", "WorldID", "RegionID", "Controls"):
		if key not in row:
			raise ValueError(f"Arabia nest row lacks {key}: {row.get('NestID')}")
	if row.get("NestFlag"):
		raise ValueError(f"Arabia nest {row['NestID']} carries a nest flag the projection refuses")
	if set(row["Controls"]) != CONTROL_KEYS:
		raise ValueError(f"Arabia nest {row['NestID']} tactics name {sorted(set(row['Controls']) - CONTROL_KEYS)}")
	champion = row.get("Champion")
	if champion is not None and set(champion) != CONTROL_KEYS:
		raise ValueError(f"Arabia nest {row['NestID']} champion tactics are malformed")
	if int(row["WorldID"]) not in (FIELD_WORLD, DUNGEON_WORLD):
		raise ValueError(f"Arabia nest {row['NestID']} names world {row['WorldID']}")
	if int(row["RegionID"]) not in regions:
		raise ValueError(f"Arabia nest {row['NestID']} names region {row['RegionID']} outside the client continent")
	projected = {key: row[key] for key in ROW_KEYS if key in row}
	hive = row.get("Hive") or {}
	overwrite = int(hive.get("OverwriteMaxTotalCount") or 0)
	if overwrite:
		projected["HiveKey"] = row["Codename"]
		projected["HiveMaxCount"] = overwrite
	elif hive.get("KeepMonsterCountType"):
		raise ValueError(f"Arabia nest {row['NestID']} hive keeps a count the projection cannot carry")
	if int(projected.get("RespawnDelayMaxSec") or 0) < int(projected.get("RespawnDelayMinSec") or 0):
		# An inverted pair authors a fixed delay, not an interval:
		# the native delay draw widens only upward from the first
		# value, so a smaller second value can never draw. Stored
		# as (min, min), which draws the identical delay; the
		# loader's ordered-pair invariant stands for every family.
		projected["RespawnDelayMaxSec"] = projected["RespawnDelayMinSec"]
	return projected


# ================
# load_nests
#
# The extraction split for the import: the projected monster rows of
# the population envelope, the authored NPC rows (consumed by the NPC
# pass below), and the hive-capped nest ids the projection carried.
# A row whose codename is neither a monster kind nor an NPC refuses
# the whole file: a partial population must not reach gameplay.
# ================
def load_nests(path, regions):
	raw = Path(path).read_bytes()
	data = json.loads(raw)
	rows = data.get("rows") if isinstance(data, dict) else None
	if not isinstance(rows, list) or not rows:
		raise ValueError(f"{path} is not an Arabia nest extraction")
	seen, monster_rows, npc_rows, capped, normalised = set(), [], [], [], []
	for row in rows:
		projected = project_row(row, regions)
		if projected["NestID"] in seen:
			raise ValueError(f"Arabia nest id {projected['NestID']} is authored twice")
		seen.add(projected["NestID"])
		if projected["Codename"].startswith(KIND_PREFIXES):
			monster_rows.append(projected)
			if "HiveKey" in projected:
				capped.append(projected["NestID"])
			if int(row.get("RespawnDelayMaxSec") or 0) < int(row.get("RespawnDelayMinSec") or 0):
				normalised.append(projected["NestID"])
		elif projected["Codename"].startswith("NPC_"):
			npc_rows.append(row)
		else:
			raise ValueError(f"Arabia nest {projected['NestID']} names {projected['Codename']}, "
				"neither an Arabia monster kind nor an NPC")
	monster_rows.sort(key=lambda row: row["NestID"])
	return {
		"sha256": hashlib.sha256(raw).hexdigest(),
		"source": data.get("source"), "sourceSha256": data.get("sourceSha256"),
		"monsters": monster_rows, "npcs": npc_rows, "hiveCapped": sorted(capped),
		"delaysNormalised": sorted(normalised),
	}


# ================
# build_population
#
# The monster nests in the Version-2 envelope the cap-110 populations
# use, in authored nest order, so a rebuild of the same sources writes
# the same file.
# ================
def build_population(monster_rows):
	return {"Version": 2, "Rows": monster_rows}


# ================
# select_kinds
#
# The placed kinds, converted to the v1.150 layout. A placed monster
# outside the authored level window is a data-contract break, refused
# by name rather than silently dropped or admitted. NPC rows convert
# without the window (their levels are the client's own business).
# ================
def select_kinds(later, codes, window):
	refs = {}
	for code in sorted(codes):
		row = later.get(code)
		if row is None:
			raise ValueError("placed character has no later character row: " + code)
		if window:
			level = int(row[57])
			if not FIRST_LEVEL <= level <= LAST_LEVEL:
				raise ValueError(f"placed monster {code} is level {level}, outside {FIRST_LEVEL}-{LAST_LEVEL}")
		refs[code] = characters.convert_character(row)
	return refs


# ================
# npc_placements
#
# Where the continent's NPCs stand: the extraction's authored rows
# first (they win a shared place), then the standings the client's own
# npcpos names inside the continent that the extraction does not.
# Returns [(codename, region, x, y, z)] in a stable order.
# ================
def npc_placements(npc_rows, npcpos_rows, codename_by_id, regions):
	found, seen = [], set()
	for row in npc_rows:
		if not row["Codename"].startswith("NPC_"):
			continue
		key = (row["Codename"], int(row["RegionID"]), round(float(row["X"])), round(float(row["Z"])))
		seen.add(key)
		found.append((row["Codename"], int(row["RegionID"]),
			float(row["X"]), float(row["Y"]), float(row["Z"])))
	for row in npcpos_rows:
		if len(row) < 5 or row[0] not in codename_by_id:
			continue
		code = codename_by_id[row[0]]
		if not code.startswith("NPC_"):
			continue
		try:
			region, x, y, z = int(row[1]) & 0xFFFF, float(row[2]), float(row[3]), float(row[4])
		except ValueError:
			continue
		if region not in regions:
			continue
		key = (code, region, round(x), round(z))
		if key in seen:
			continue
		seen.add(key)
		found.append((code, region, x, y, z))
	return sorted(found, key=lambda place: (place[0], place[1], place[2], place[4]))


# ================
# later_speech_texts
#
# The later client's NPC talk strings (the SN_..._BS/_PS keys its
# npcchat rows name), normalised to the row shape later_object_texts
# returns for the name table. The later client keeps them in its
# textquest_speech&name shards; the native client keeps its own talk
# rows in textquest.txt itself.
# ================
def later_speech_texts(read):
	manifest = tables.table_rows(read("textquest_speech&name.txt"))
	shards = [row[0].lower() for row in manifest if row and row[0].lower().endswith(".txt")]
	if not shards:
		raise ValueError("v1.696 textquest_speech&name.txt names no shards")
	rows = []
	for shard in shards:
		for row in characters.translation_rows(read(shard)):
			# An untranslated row keeps only its first cells (the
			# table reader trims the empty tail); it still rides,
			# with no English, like the names that lack one.
			if len(row) < 3 or not row[2]:
				continue
			english = next((row[at] for at in (9, 13, 14, 15, 16)
				if len(row) > at and row[at] not in ("0", "")), "")
			rows.append([row[0], row[2], row[3] if len(row) > 3 else "",
				"", "", "", "", "", english, ""])
	return rows


# ================
# format_coord
#
# An npcpos coordinate: integers stay integers (the table's own
# spelling for whole values), the rest keep full precision.
# ================
def format_coord(value):
	number = float(value)
	if number == int(number):
		return str(int(number))
	return repr(number)


#============================================================================
# The import
#============================================================================

# ================
# remove_owned
#
# The off path: everything the marker owns leaves - the shards, the
# population, the NPC standings and talk rows, the copied files and
# the owned name, characterinfo, animation and gate rows. The variant
# build rewrites the gate tables, the catalogues and mapinfo from the
# native tables on every run, so a standalone off run strips the gate
# rows itself and a build-run off finds none.
# ================
def remove_owned(output, prior):
	text_root = output / TEXT_ROOT
	if prior:
		symbols = set(prior.get("nameSymbols", [])) | set(prior.get("gateTextSymbols", []))
		if symbols:
			text_path = text_root / "textdataname.txt"
			write_atomically(text_path, monsters.encode_rows(
				monsters.drop_symboled_rows(characters.translation_rows(text_path.read_bytes()), symbols)))
		speech_symbols = set(prior.get("speechSymbols", []))
		if speech_symbols:
			quest_path = text_root / "textquest.txt"
			if quest_path.exists():
				write_atomically(quest_path, monsters.encode_rows(
					[row for row in tables.table_rows(quest_path.read_bytes())
						if len(row) <= 1 or row[1] not in speech_symbols]))
		effect_path = text_root / "skilleffect.txt"
		effect_text = effects.remove_animation_bases(
			tables.decode_table(effect_path.read_bytes()), set(prior.get("animationBases", [])))
		write_atomically(effect_path, b"\xff\xfe" + monsters.drop_characterinfo_rows(
			effect_text, set(prior.get("characterInfo", []))).encode("utf-16-le"))
		gate_codes = set(prior.get("gateCodes", []))
		data_path = text_root / "teleportdata.txt"
		data_rows = tables.table_rows(data_path.read_bytes()) if gate_codes else []
		removed_ids = {row[1] for row in data_rows if len(row) > 2 and row[2] in gate_codes}
		if removed_ids:
			write_atomically(data_path, monsters.encode_rows(
				[row for row in data_rows if len(row) <= 2 or row[2] not in gate_codes]))
			building_path = text_root / "teleportbuilding.txt"
			write_atomically(building_path, monsters.encode_rows(
				[row for row in tables.table_rows(building_path.read_bytes())
					if len(row) <= 2 or row[2] not in set(prior.get("gateBuildingCodes", []))]))
			link_path = text_root / "teleportlink.txt"
			write_atomically(link_path, monsters.encode_rows(
				[row for row in tables.table_rows(link_path.read_bytes())
					if len(row) <= 2 or (row[1] not in removed_ids and row[2] not in removed_ids)]))
		chat_codes = set(prior.get("chatCodes", prior.get("npcCodes", [])))
		if chat_codes:
			chat_path = text_root / "npcchat.txt"
			if chat_path.exists():
				write_atomically(chat_path, monsters.encode_rows(
					[row for row in tables.table_rows(chat_path.read_bytes())
						if len(row) <= 1 or row[1] not in chat_codes]))
		npc_ids = {str(identity) for identity in prior.get("npcIds", [])}
		if npc_ids:
			npcpos_path = text_root / "npcpos.txt"
			if npcpos_path.exists():
				write_atomically(npcpos_path, monsters.encode_rows(
					[row for row in tables.table_rows(npcpos_path.read_bytes())
						if len(row) < 1 or row[0] not in npc_ids]))
		region_ids = {str(region) for region in prior.get("regions", [])}
		if region_ids:
			region_path = text_root / "regioncode.txt"
			write_atomically(region_path, monsters.encode_rows(
				[row for row in tables.table_rows(region_path.read_bytes())
					if len(row) <= 2 or row[1] not in region_ids or not row[2].startswith(REGION_PREFIX)]))
	for name in (CHARACTER_SHARD, SKILL_SHARD, POPULATION_FILE):
		(text_root / name).unlink(missing_ok=True)
	manifest = text_root / "skilldata.txt"
	if manifest.exists():
		write_atomically(manifest, monsters.encode_rows(
			[r for r in tables.table_rows(manifest.read_bytes()) if r[0].lower() != SKILL_SHARD]))
	for relative in prior.get("files", []):
		safe_target(output, relative).unlink(missing_ok=True)
	(output / MARKER).unlink(missing_ok=True)
	return {"enabled": False}


# ================
# arabia_resource_closure
#
# The resource walk with the gap posture the later sources need:
# files their archives name but do not hold, and files the shared
# parsers cannot read, are recorded in the summary rather than
# stopping the continent - the nameless event standings and the
# unbuilt boss sector follow the same rule, and none of the missing
# bytes can be invented. Sky Temple's strict default stands.
# ================
def arabia_resource_closure(seeds, archives, native, output):
	return copy_resource_closure(seeds, archives, native, output, tolerate_gaps=True)


# ================
# add_arabia
# ================
def add_arabia(native, output, source=None, nests=None, closure=arabia_resource_closure):
	native, output = Path(native).resolve(), Path(output).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("arabia output must be separate from the native extraction")
	marker = output / MARKER
	prior = json.loads(marker.read_text(encoding="utf-8")) if marker.exists() else {}
	if not enabled():
		return remove_owned(output, prior)
	root = Path(source) if source else source_root()
	verify_archives(root)
	text_root = output / TEXT_ROOT

	media_path, media_handle, media = open_archive(root, "Media")
	try:
		media_index = tables.archive_index(media)
		archive_hashes = {"Media": tables.sha256_of(media)}

		# ================
		# read
		# ================
		def read(name):
			return tables.table_text(media, media_index, name.lower())

		later = {row[2]: row for shard in tables.table_rows(read("characterdata.txt"))
			for row in tables.table_rows(read(shard[0].lower())) if len(row) > 2}
		codename_by_id = {row[1]: row[2] for row in later.values()}
		regions = continent_regions(tables.table_rows(read("regioncode.txt")))
		extraction = load_nests(Path(nests) if nests else nests_path(), regions)
		population = build_population(extraction["monsters"])
		placements = npc_placements(extraction["npcs"], tables.table_rows(read("npcpos.txt")),
			codename_by_id, regions)

		# A standing no source can name does not stand: the later
		# client's own npcpos plants event props (a circus bear, a
		# carnival frame) whose NameStrID no text shard of either
		# client holds, so converting them would ship a name lookup
		# that can never resolve. They are skipped and logged; the
		# name step below still refuses any kind that slips through.
		object_texts = later_object_texts(read)
		later_name_keys = {row[1] for row in object_texts}
		native_name_keys = {row[1] for row in characters.translation_rows(
			(text_root / "textdataname.txt").read_bytes()) if len(row) > 1}
		nameless = {}
		for place in placements:
			row = later.get(place[0])
			if row is not None and row[5] not in later_name_keys and row[5] not in native_name_keys:
				nameless[place[0]] = nameless.get(place[0], 0) + 1
		if nameless:
			placements = [place for place in placements if place[0] not in nameless]
			for code in sorted(nameless):
				tables.log(f"arabia npcs: {code} skipped - name {later[code][5]} is in no text "
					f"table of either client ({nameless[code]} standings)")

		refs = select_kinds(later, {row["Codename"] for row in extraction["monsters"]}, window=True)
		npc_refs = select_kinds(later, {place[0] for place in placements}, window=False)
		all_refs = {**refs, **npc_refs}

		native_chars = [r for p in text_root.glob("characterdata_*.txt") if p.name != CHARACTER_SHARD
			for r in tables.table_rows(p.read_bytes())]
		occupied = {int(r[1]) for r in native_chars}
		allocated = monsters.allocate_identities(occupied, sorted(all_refs), prior.get("characters", {}))
		for code, row in sorted(all_refs.items()):
			row[1] = str(allocated[code])
		native_skills = [r for p in text_root.glob("skilldata_*.txt") if p.name != SKILL_SHARD
			for r in tables.table_rows(p.read_bytes()) if len(r) == 118]
		needed = {int(s) for r in all_refs.values() for s in r[89:99] if int(s)}
		source_skills = [r for shard in tables.table_rows(read("skilldata.txt"))
			for r in tables.table_rows(read(shard[0].lower())) if len(r) > 3 and r[1].isdigit()]

		# ================
		# load_client_skills
		# ================
		def load_client_skills():
			return {int(r[1]): r for r in source_skills}

		source_skills = monsters.add_native_client_skills(source_skills, needed, native_skills, load_client_skills)
		added, skill_map = monsters.merge_skills(native_skills, source_skills, needed, prior.get("skills", {}))
		for row in all_refs.values():
			row[89:99] = [str(skill_map[int(s)]) if int(s) else "0" for s in row[89:99]]

		# Gates first: the merge rewrites textdataname.txt, and the name
		# step below appends onto what it leaves.
		current = {name: (text_root / name).read_bytes()
			for name in ("teleportdata.txt", "teleportbuilding.txt", "teleportlink.txt")}
		current["text"] = (text_root / "textdataname.txt").read_bytes()

		# A rebuild re-derives the gate identities from the source
		# tables; the rows an earlier run of this import wrote must
		# not read as identity collisions (they would be renumbered
		# above the used range and orphaned, one set per rebuild).
		prior_gate_codes = set(prior.get("gateCodes", []))
		if prior_gate_codes:
			data_rows = tables.table_rows(current["teleportdata.txt"])
			gone_ids = {row[1] for row in data_rows if len(row) > 2 and row[2] in prior_gate_codes}
			if gone_ids:
				prior_buildings = set(prior.get("gateBuildingCodes", []))
				current["teleportdata.txt"] = monsters.encode_rows(
					[row for row in data_rows if len(row) <= 2 or row[2] not in prior_gate_codes])
				current["teleportbuilding.txt"] = monsters.encode_rows(
					[row for row in tables.table_rows(current["teleportbuilding.txt"])
						if len(row) <= 2 or row[2] not in prior_buildings])
				current["teleportlink.txt"] = monsters.encode_rows(
					[row for row in tables.table_rows(current["teleportlink.txt"])
						if len(row) <= 2 or (row[1] not in gone_ids and row[2] not in gone_ids)])
		native_text_root = native / TEXT_ROOT
		later_gate_tables = {
			"teleportdata.txt": tables.table_rows(read("teleportdata.txt")),
			"teleportbuilding.txt": tables.table_rows(read("teleportbuilding.txt")),
			"teleportlink.txt": tables.table_rows(read("teleportlink.txt")),
			"text": monsters.encode_rows(object_texts),
		}
		# Symbol ownership is measured against the native tables,
		# never the current ones: a rebuild must not forget what an
		# earlier run wrote (the off path strips exactly these sets).
		native_text_keys = {row[1] for row in tables.table_rows(
			(native_text_root / "textdataname.txt").read_bytes()) if len(row) > 1}
		gate_tables, gate_codes = prepare_gates(
			later_gate_tables,
			tables.table_rows((native_text_root / "teleportbuilding.txt").read_bytes()),
			tables.table_rows((native_text_root / "teleportdata.txt").read_bytes()),
			{row[1] for row in tables.table_rows(current["teleportdata.txt"]) if len(row) > 1},
			{row[1] for row in tables.table_rows(current["teleportbuilding.txt"]) if len(row) > 1},
		)
		merged_gates, gate_summary = variant.merge_gate_tables(
			current, gate_tables, codes=gate_codes, level_cap=variant.gate_level_cap())
		for dropped in gate_summary.get("droppedConditions", []):
			tables.log(f"arabia gates: link {dropped['from']} -> {dropped['to']}: condition kind "
				f"{dropped['kind']} dropped; the v1.150 server has no evaluator for it")
		for name, content in merged_gates.items():
			variant.replace_file(text_root / ("textdataname.txt" if name == "text" else name), content)

		write_atomically(text_root / CHARACTER_SHARD,
			monsters.encode_rows(sorted(all_refs.values(), key=lambda row: int(row[1]))))
		write_atomically(text_root / SKILL_SHARD, monsters.encode_rows(added))
		manifest = tables.table_rows((text_root / "skilldata.txt").read_bytes())
		manifest = [r for r in manifest if r[0].lower() != SKILL_SHARD] + [[SKILL_SHARD]]
		write_atomically(text_root / "skilldata.txt", monsters.encode_rows(manifest))

		# The NPC pass: standings and talk rows, on the Alexandria
		# rules (a codename the native tables already have keeps its
		# native row; shops ride only for native stores, and Baghdad's
		# are not native, so none ride).
		npcpos_rows = [[str(allocated[code]), str(region), format_coord(x), format_coord(y), format_coord(z)]
			for code, region, x, y, z in placements]
		existing_standings = {tuple(row[:5]) for row in tables.table_rows((text_root / "npcpos.txt").read_bytes())
			if len(row) >= 5}
		npcpos_rows = [row for row in npcpos_rows if tuple(row) not in existing_standings]
		if npcpos_rows:
			write_atomically(text_root / "npcpos.txt", characters.append_rows(
				(text_root / "npcpos.txt").read_bytes(), npcpos_rows))
		chat_rows = []
		chat_codes_owned = set(prior.get("chatCodes", []))
		if any(name.endswith("npcchat.txt") for name in media_index):
			current_chat = {row[1] for row in tables.table_rows((text_root / "npcchat.txt").read_bytes())
				if len(row) > 1}
			later_chat = [row for row in tables.table_rows(read("npcchat.txt"))
				if len(row) >= 4 and row[0] == "1" and row[1] in npc_refs]
			chat_rows = [row[:4] for row in later_chat if row[1] not in current_chat]
			if chat_rows:
				write_atomically(text_root / "npcchat.txt", characters.append_rows(
					(text_root / "npcchat.txt").read_bytes(), chat_rows))
			# Kinds the native client already talks for keep their
			# native row (nothing was appended above); their codes
			# stay out of the owned set or the flag-off strip would
			# delete native talk rows.
			native_chat_path = native_text_root / "npcchat.txt"
			native_chat_codes = {row[1] for row in tables.table_rows(native_chat_path.read_bytes())
				if len(row) > 1} if native_chat_path.exists() else set()
			chat_codes_owned |= {row[1] for row in later_chat} - native_chat_codes
		chat_keys = {cell for row in chat_rows for cell in row[2:] if cell.startswith("SN_")}

		base_text = (text_root / "textdataname.txt").read_bytes()
		known = {r[1] for r in characters.translation_rows(base_text) if len(r) > 1}
		wanted = {r[5] for r in all_refs.values()} - known
		texts = [native_text_row(r) for r in object_texts if len(r) >= 10 and r[1] in wanted]
		if wanted - {r[1] for r in texts}:
			raise ValueError("missing Arabia character names")
		stale_names = set(prior.get("nameSymbols", [])) - {r[5] for r in all_refs.values()} - chat_keys
		if stale_names:
			base_text = monsters.encode_rows(monsters.drop_symboled_rows(
				characters.translation_rows(base_text), stale_names))
		write_atomically(text_root / "textdataname.txt", characters.append_rows(base_text, texts))

		# Talk strings ride into textquest.txt, the table the native
		# client keeps its own SN_NPC_*_BS/_PS rows in; the later
		# client keeps them in its speech shards instead. The _PS
		# keys resolve nowhere - the native tables leave the same
		# column dangling - so they are recorded, not demanded; a
		# run that can resolve nothing at all is an extraction
		# break and still refuses.
		quest_path = text_root / "textquest.txt"
		quest_text = quest_path.read_bytes()
		quest_keys = {row[1] for row in tables.table_rows(quest_text) if len(row) > 1}
		all_chat_keys = {cell for row in tables.table_rows(read("npcchat.txt"))
			if len(row) >= 4 and row[0] == "1" and row[1] in chat_codes_owned
			for cell in row[2:4] if cell.startswith("SN_")}
		new_keys = all_chat_keys - quest_keys
		speech_rows, speech_unresolved = [], []
		if new_keys:
			speech_texts = {row[1]: row for row in later_speech_texts(read)}
			if not speech_texts:
				raise ValueError("missing Arabia NPC speech texts")
			speech_unresolved = sorted(new_keys - set(speech_texts))
			speech_rows = [native_text_row(speech_texts[key])
				for key in sorted(new_keys & set(speech_texts))]
			write_atomically(quest_path, characters.append_rows(quest_text, speech_rows))
			quest_keys |= {row[1] for row in speech_rows}
			if speech_unresolved:
				tables.log(f"arabia npcs: {len(speech_unresolved)} talk keys resolve in no text "
					"table of either client (the dangling SN_..._PS family); recorded in the marker")
		native_quest_keys = {row[1] for row in tables.table_rows(
			(native_text_root / "textquest.txt").read_bytes()) if len(row) > 1}
		speech_symbols = sorted(({row[1] for row in speech_rows}
			| (set(prior.get("speechSymbols", [])) & quest_keys)) - native_quest_keys)

		effect_path = text_root / "skilleffect.txt"
		effect_text = tables.decode_table(effect_path.read_bytes())
		later_effects = tables.decode_table(read("skilleffect.txt"))
		selected = effects.select_later_rows(later_effects, {r[3] for r in added} - effects.native_bases(effect_text))
		unknown = effects.missing_animation_states(
			[line for rows in selected.values() for _, line in rows], effect_text)
		if unknown:
			raise ValueError("monster animation states absent natively: " + str(sorted(unknown)))
		effect_text, _ = effects.merge_skill_effect_text(effect_text, selected)
		known_info = {r[0] for r in tables.table_rows(effect_text.encode("utf-16")) if r}
		info = [r[:characters.SKILL_EFFECT_KEPT] for r in tables.table_rows(read("skilleffect.txt"))
			if len(r) == characters.SKILL_EFFECT_COLUMNS and r[0] in all_refs and r[0] not in known_info]
		owned_info = monsters.characterinfo_codenames(effect_text) & set(all_refs)
		stale_info = set(prior.get("characterInfo", [])) - owned_info
		if stale_info:
			effect_text = monsters.drop_characterinfo_rows(effect_text, stale_info)
		write_atomically(effect_path, characters.skill_effect_section(effect_text.encode("utf-16"), info))

		# Region codes and zone names, on the rules the Alexandria map
		# merge uses: a code row joins where the current row is absent or
		# a placeholder, a zone name where its key is new.
		region_path = text_root / "regioncode.txt"
		current_regions = tables.table_rows(region_path.read_bytes())
		current_by_id = {row[1]: row for row in current_regions if len(row) >= 3}
		code_rows = [row for row in tables.table_rows(read("regioncode.txt"))
			if len(row) >= 4 and row[1].isdigit() and int(row[1]) in regions
			and current_by_id.get(row[1], [0, 0, "xxx"])[2] == "xxx"]
		replaced = {row[1] for row in code_rows}
		kept = [row for row in current_regions if len(row) < 2 or row[1] not in replaced]
		write_atomically(region_path, monsters.encode_rows(kept + code_rows))
		zone_path = text_root / "textzonename.txt"
		zone_keys = {row[1] for row in tables.table_rows(zone_path.read_bytes()) if len(row) > 1}
		zone_rows = [native_text_row(row) for row in tables.table_rows(read("textzonename.txt"))
			if len(row) >= 10 and (row[1] in {str(region) for region in regions}
				or row[1].startswith(REGION_PREFIX)) and row[1] not in zone_keys]
		write_atomically(zone_path, characters.append_rows(zone_path.read_bytes(), zone_rows))

		write_atomically(text_root / POPULATION_FILE, (json.dumps(population, sort_keys=True) + "\n").encode())

		# Map: catalogues over the variant's current ones, terrain with
		# its tile ids remapped, placements with their object ids.
		map_path, map_handle, map_data = open_archive(root, "Map")
		try:
			map_index = tables.archive_index(map_data)
			archive_hashes["Map"] = tables.sha256_of(map_data)
			sectors = sorted({(region & 0xFF, (region >> 8) & 0xFF) for region in regions})
			terrain, used_tiles, used_objects = {}, set(), set()
			built = []
			for sector_x, sector_y in sectors:
				paths = [f"{sector_y}/{sector_x}{extension}" for extension in tables.MAP_EXTENSIONS]
				if any(path not in map_index for path in paths):
					# The later client authors this region's code
					# but never built its sector (the field-02 boss
					# lair has no terrain or navmesh anywhere):
					# the sector is skipped and logged, never
					# fabricated; its region code row still imports.
					tables.log(f"arabia map: sector {sector_x}/{sector_y} has no terrain in the "
						"v1.696 Map.pk2 and is skipped")
					continue
				built.append((sector_x, sector_y))
				for archive_path in paths:
					terrain[archive_path] = sro_pk2.payload(map_data, map_index[archive_path])
				used_tiles |= tables.terrain_tile_ids(terrain[f"{sector_y}/{sector_x}.m"])
				used_objects |= o2_object_ids(terrain[f"{sector_y}/{sector_x}.o2"])
			sectors = built
			if "tile2d.ifo" not in map_index or "object.ifo" not in map_index:
				raise ValueError("v1.696 Map.pk2 lacks tile2d.ifo or object.ifo")
			later_tiles = tables.tile_catalog(sro_pk2.payload(map_data, map_index["tile2d.ifo"]))
			tiles_merged, tile_remap = variant.merge_tile_catalog(
				(output / "Map_extracted" / "tile2d.ifo").read_bytes(),
				sro_pk2.payload(map_data, map_index["tile2d.ifo"]), used_tiles)
			variant.replace_file(output / "Map_extracted" / "tile2d.ifo", tiles_merged)
			objects_merged, object_remap = merge_object_catalog_remap(
				(output / "Map_extracted" / "object.ifo").read_bytes(),
				sro_pk2.payload(map_data, map_index["object.ifo"]), used_objects)
			variant.replace_file(output / "Map_extracted" / "object.ifo", objects_merged)

			copied = []
			current_tiles = tables.tile_catalog((output / "Map_extracted" / "tile2d.ifo").read_bytes())
			for tile_id in sorted(used_tiles):
				if tile_id not in later_tiles:
					raise ValueError(f"Arabia terrain paints tile id {tile_id} missing from tile2d.ifo")
				if current_tiles.get(tile_remap.get(tile_id, tile_id)) != later_tiles[tile_id]:
					continue
				name = later_tiles[tile_id][1].replace("\\", "/")
				entry = map_index.get("tile2d/" + name.lower())
				if entry is None:
					entry = map_index.get("tile2d/" + later_tiles[tile_id][0].replace("\\", "/").lower() + "/" + name.lower())
				if entry is None:
					raise ValueError(f"v1.696 Map.pk2 has no tile texture {name}")
				target = output / "Map_extracted" / "tile2d" / name
				stored = sro_pk2.payload(map_data, entry)
				if target.exists() and target.read_bytes() == stored:
					continue
				if target.exists():
					# The variant already holds a texture of this name from
					# an earlier import; it stands, as in the Alexandria
					# build, and the clash is reported in the summary.
					copied.append("clash:Map_extracted/tile2d/" + name)
					continue
				variant.replace_file(target, stored)
				copied.append("Map_extracted/tile2d/" + name)
			for sector_x, sector_y in sectors:
				for extension in tables.MAP_EXTENSIONS:
					archive_path = f"{sector_y}/{sector_x}{extension}"
					stored = terrain[archive_path]
					if extension == ".m":
						stored = variant.remap_terrain(stored, tile_remap)
					elif extension == ".o2":
						stored = patch_o2_objects(stored, object_remap)
					variant.replace_file(output / "Map_extracted" / archive_path, stored)
					copied.append("Map_extracted/" + archive_path)
		finally:
			map_data.close()
			map_handle.close()

		# Data: the continent's navmesh and the model closures.
		data_path, data_handle, data = open_archive(root, "Data")
		particles_path, particles_handle, particles = open_archive(root, "Particles")
		try:
			data_index = tables.archive_index(data)
			archive_hashes["Data"] = tables.sha256_of(data)
			archive_hashes["Particles"] = tables.sha256_of(particles)
			archives = {"Data": (data, data_index),
				"Particles": (particles, tables.archive_index(particles))}
			for sector_x, sector_y in sectors:
				nav = tables.navmesh_path(sector_x, sector_y)
				entry = data_index.get(nav)
				if entry is None:
					raise ValueError(f"v1.696 Data.pk2 has no {nav}")
				target = output / "Data_extracted" / "navmesh" / Path(nav).name
				stored = sro_pk2.payload(data, entry)
				if target.exists() and target.read_bytes() == stored:
					continue
				variant.replace_file(target, stored)
				copied.append("Data_extracted/" + nav)
			merged_objects = tables.object_catalog((output / "Map_extracted" / "object.ifo").read_bytes())
			final_ids = {object_remap.get(object_id, object_id) for object_id in used_objects}
			if not final_ids <= set(merged_objects):
				raise ValueError("Arabia placements use object ids the merged catalogue lacks")
			seeds = {merged_objects[object_id][1] for object_id in final_ids}
			# A kind can name several model variants in one cell
			# (the Mad General's three .bsr files, comma-joined);
			# every variant is a closure seed of its own.
			seeds |= {"res/" + model.replace("\\", "/")
				for row in all_refs.values()
				for model in row[characters.MODEL_COLUMN].split(",")
				if model not in ("", "xxx")}
			closure_result = closure(seeds, archives, native, output)
			copied += closure_result["files"]
		finally:
			data.close()
			data_handle.close()
			particles.close()
			particles_handle.close()

		mapinfo = output / "Data_extracted" / "navmesh" / "mapinfo.mfo"
		variant.replace_file(mapinfo, variant.enable_regions(mapinfo.read_bytes(), sectors))

		files = sorted({entry for entry in set(prior.get("files", [])) | set(copied) if not entry.startswith("clash:")})
		summary = {
			"enabled": True,
			"characters": {code: int(row[1]) for code, row in all_refs.items()},
			"skills": {row[3]: int(row[1]) for row in added},
			"nests": len(population["Rows"]),
			"kinds": len(refs),
			"npcs": len(npc_refs),
			"npcPlacements": len(placements),
			"npcIds": sorted(int(allocated[code]) for code in npc_refs),
			"npcCodes": sorted(npc_refs),
			"chatCodes": sorted(chat_codes_owned),
			"hiveCapped": extraction["hiveCapped"],
			"delaysNormalised": extraction["delaysNormalised"],
			"nestsSource": {"sha256": extraction["sha256"],
				"source": extraction["source"], "sourceSha256": extraction["sourceSha256"]},
			"regions": sorted(regions),
			"sectors": [[x, y] for x, y in sectors],
			"tileRemap": {str(k): v for k, v in sorted(tile_remap.items())},
			"objectRemap": {str(k): v for k, v in sorted(object_remap.items())},
			"tileTextureClashes": sorted(e[len("clash:"):] for e in copied if e.startswith("clash:")),
			"missingSounds": closure_result["missingSounds"],
			"missingEffects": closure_result["missingEffects"],
			"missingFiles": closure_result.get("missingFiles", []),
			"unparsedFiles": closure_result.get("unparsedFiles", []),
			"gates": gate_summary,
			"gateCodes": list(gate_codes),
			"gateBuildingCodes": sorted(row[2] for row in tables.table_rows(gate_tables["teleportbuilding.txt"])),
			"gateTextSymbols": sorted((({
				row[4] for row in tables.table_rows(gate_tables["teleportdata.txt"]) if len(row) > 4
			} | {
				row[5] for row in tables.table_rows(gate_tables["teleportbuilding.txt"]) if len(row) > 5
			}) - native_text_keys) | set(prior.get("gateTextSymbols", []))),
			"animationBases": sorted(set(prior.get("animationBases", []))
				| {base for rows in selected.values() for base, _ in rows}),
			"characterInfo": sorted(owned_info | {row[0] for row in info}),
			"nameSymbols": sorted((({r[1] for r in characters.translation_rows(base_text) if len(r) > 1}
				| {r[1] for r in texts}) & ({r[5] for r in all_refs.values()} | chat_keys))
				- native_text_keys),
			"speechSymbols": speech_symbols,
			"speechUnresolved": speech_unresolved,
			"files": files,
			"archives": archive_hashes,
		}
		write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode())
		return summary
	finally:
		media.close()
		media_handle.close()


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--native-extracted", required=True)
	parser.add_argument("--output", required=True)
	parser.add_argument("--source-root")
	parser.add_argument("--nests")
	args = parser.parse_args()
	print(json.dumps(add_arabia(args.native_extracted, args.output, args.source_root, args.nests), indent=2))


if __name__ == "__main__":
	main()
