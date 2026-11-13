"""
===========================================================================

skytemple_import.py - the Sky Temple fields A-D from the v1.696 client

PORT-ONLY, NOT NATIVE. Off unless SRO_SKYTEMPLE is "1", "on" or "true".
Called by build_alexandria_variant.py after the cap-110 monsters, inside
the Alexandria variant build; the server reads the population only while
its own SRO_SKYTEMPLE is on. Build and server must agree.

The source is the operator's own v1.696 client (SRO_V1696_ROOT, default
inputs/sro_1696 beside the game root); its archives are never committed.
When scripts/v1696_archive_pins.json exists (the mall-avatar import's pin
file, #3250) the three archives are verified against it before anything
reads them; until then the marker records their sha256 hashes.

What the source authors, and what this import does with it:

	fields    288 regions RN_OTHER_SKYTEMPLE_A..D (four 72-sector islands,
	          sectors x 217-233, y 82-100). Terrain, navmesh, tile and
	          object catalogues merge over the variant's current ones;
	          a colliding later tile or object id moves past the end and
	          the copied terrain (.m) and placements (.o2) are patched.
	monsters  the 67 MOB_SKYTEMPLE_* kinds npcpos places inside the
	          fields (1,009 placements, 429 in-field). Kinds npcpos never
	          places (summons and clones) stay out. Levels stay as the
	          client authors them, 131-178 (see docs/features/CAP110.md
	          for the level decision); skills join by codename, native
	          records win, identities are allocated once and kept in the
	          marker. Nests take the client-only posture recorded for
	          this import (#3269): one monster on the placement point,
	          respawn 1-3 s, sight 100.
	gates     the source chain stands in a Jupiter field this port does
	          not import, and its C links name gates whose buildings the
	          later client itself never shipped. Authored at import: the
	          source entry building (43802) is re-homed beside Jangan's
	          STORE_CH_GATE, the entry offers A and B on the source links
	          and C and D on authored links, and D gets an authored return
	          building in the source's own shape. The rows name world 1:
	          the later per-field world ids (132-134) are not worlds of
	          this port, whose portal catalogue refuses unknown worlds.
	          Link level conditions clamp to the build's cap as every
	          imported gate's do (build_alexandria_variant.gate_level_cap).

Turning the flag off removes the shards, the population, the copied
files and the owned table rows (the marker lists them); the gate tables
and catalogues need no reaping because the variant build rewrites them
from the native tables on every run.

===========================================================================
"""
import argparse
import base64
import json
import mmap
import os
import struct
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
from prepare_client_resources import safe_target, write_atomically  # noqa: E402
from skytemple_gates import FIELD_WORLD, d_arrival, prepare_gates  # noqa: E402
from sro_paths import MAIN_CHECKOUT_ROOT  # noqa: E402

FLAG = "SRO_SKYTEMPLE"
SOURCE_ENV = "SRO_V1696_ROOT"
MARKER = ".opensro-skytemple.json"
CHARACTER_SHARD = "characterdata_160000.txt"
SKILL_SHARD = "skilldata_160000.txt"
POPULATION_FILE = "skytemple-monsters.txt"
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA
PIN_FILE = Path(__file__).with_name("v1696_archive_pins.json")

PREFIX = "MOB_SKYTEMPLE_"
REGION_PREFIX = "RN_OTHER_SKYTEMPLE_"
FIRST_LEVEL = 131
LAST_LEVEL = 178

# The client-only nest posture (#3269): the client tables carry placements
# but no tactics, so an ordinary aggressive field monster is synthesized -
# respawn 1-3 seconds and sight 100, the commonest authored values of the
# population evidence the native server ships. Port-only inference.
CLIENT_NEST_RESPAWN_MIN_SEC = 1
CLIENT_NEST_RESPAWN_MAX_SEC = 3
CLIENT_NEST_SIGHT_RANGE = 100
# The home radius and spawn spread: the evidence's commonest for a
# respawning nest, as cap110_monsters.py CLIENT_NEST_RADIUS explains (a
# radius 0 nest keeps its monsters homing forever).
CLIENT_NEST_RADIUS = 700
CLIENT_NEST_GENERATE_RADIUS = 600


# JMXVMAPO1001 placements, as scripts/build/world/jmx/JMXVMAPO1001.mjs:
# a 12-byte signature, 36 blocks of 4 LOD groups, each a u16 count and
# that many 30-byte records whose first u32 is the object id.
MAPO_SIGNATURE = b"JMXVMAPO1001"
MAPO_SIGNATURE_BYTES = 12
MAPO_BLOCKS = 36
MAPO_LOD_GROUPS = 4
MAPO_RECORD_BYTES = 30

PARSED_EXTENSIONS = (".bsr", ".bmt", ".efp", ".cpd")
ARCHIVE_FOLDERS = {"Data": "Data_extracted", "Particles": "Particles_extracted"}
# A modifier reference the later BSRs name but the supplied archives do
# not hold is a demand-load miss at runtime, as it is in the retail
# client: the full client keeps sounds in an archive this input subset
# lacks, some Jupiter-era sound paths name a layout the archive no
# longer uses, and native monsters themselves name an appear effect
# (monster/system_appear.efp) that neither client ships. The cue or
# effect stays silent and the miss is recorded, never fatal. The model
# chain - meshes, materials, textures, skeletons, animations - stays
# fatal: a missing one is a broken model, not a silent cue.
SOUND_EXTENSIONS = (".wav", ".mp3", ".ogg")
EFFECT_EXTENSIONS = (".efp",)


# ================
# enabled
# ================
def enabled(env=os.environ):
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON


# ================
# source_root
#
# The v1.696 client folder: SRO_V1696_ROOT, else inputs/sro_1696 beside
# the game root (the parent of the main checkout, so a variant build
# whose SRO_GAME_ROOT names the variant still finds the one input).
# ================
def source_root(env=os.environ):
	configured = env.get(SOURCE_ENV, "").strip()
	if configured:
		return Path(configured)
	return MAIN_CHECKOUT_ROOT.parent / "inputs" / "sro_1696"


# ================
# verify_archives
#
# The mall-avatar import pins these archives (v1696_archive_pins.json,
# #3250). While that pin file is absent there is nothing to verify
# against; when it lands, a changed archive fails the build before any
# of it is read, as the VSRO pin does (#1745).
# ================
def verify_archives(root):
	if not PIN_FILE.is_file():
		return
	pin = json.loads(PIN_FILE.read_text(encoding="utf-8"))
	pins = pin.get("archives") if isinstance(pin, dict) else None
	if not isinstance(pins, dict):
		raise ValueError(f"{PIN_FILE} is not a pin file")
	for name in ("Map", "Media", "Data", "Particles"):
		if name in pins:
			tables.verify_vsro_archive(root, name, pins)


# ================
# open_archive
# ================
def open_archive(root, name):
	path = Path(root) / f"{name}.pk2"
	if not path.is_file():
		raise ValueError(f"{path} not found; {SOURCE_ENV} must name the v1.696 client folder")
	handle = open(path, "rb")
	return path, handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ)


# ================
# field_regions
#
# {region id: field letter} of the RN_OTHER_SKYTEMPLE_* regioncode rows,
# and the four named entry regions (the rows named exactly
# RN_OTHER_SKYTEMPLE_A..D).
# ================
def field_regions(regioncode_rows):
	fields, named = {}, {}
	for row in regioncode_rows:
		if len(row) < 3 or not row[1].isdigit():
			continue
		code = row[2].strip()
		if not code.startswith(REGION_PREFIX):
			continue
		letter = code[len(REGION_PREFIX):][:1]
		if letter not in "ABCD":
			continue
		fields[int(row[1])] = letter
		if code == REGION_PREFIX + letter:
			named[letter] = int(row[1])
	return fields, named


# ================
# placements
#
# The in-field monster placements of the later npcpos.txt: one
# (codename, region, x, y, z) per distinct row naming a MOB_SKYTEMPLE_*
# character whose region is one of the fields'. npcpos names characters
# by their later identity (the join alexandria_npcs makes) and writes
# the region as the signed u16 the server's own parser normalizes.
# ================
def placements(npcpos_rows, codename_by_id, fields):
	found, seen = [], set()
	for row in npcpos_rows:
		if len(row) < 5 or row[0] not in codename_by_id:
			continue
		code = codename_by_id[row[0]]
		if not code.startswith(PREFIX):
			continue
		try:
			region, x, y, z = int(row[1]) & 0xFFFF, float(row[2]), float(row[3]), float(row[4])
		except ValueError:
			continue
		if region not in fields:
			continue
		key = (code, region, row[2], row[3], row[4])
		if key in seen:
			continue
		seen.add(key)
		found.append((code, region, x, y, z))
	return found


# ================
# select_kinds
#
# The placed kinds, converted to the v1.150 layout. A placed kind
# outside the authored level window is a data-contract break, refused
# by name rather than silently dropped or admitted.
# ================
def select_kinds(later, placed_codes):
	refs = {}
	for code in sorted(placed_codes):
		row = later.get(code)
		if row is None:
			raise ValueError("placed monster has no later character row: " + code)
		level = int(row[57])
		if not FIRST_LEVEL <= level <= LAST_LEVEL:
			raise ValueError(f"placed monster {code} is level {level}, outside {FIRST_LEVEL}-{LAST_LEVEL}")
		refs[code] = characters.convert_character(row)
	return refs


# ================
# client_controls
#
# The tactics key set at the client-only posture: an ordinary aggressive
# field monster (AggressType 0, as the server reads it) with the
# synthesized sight range and no champion. Every other field is 0, the
# value the native template itself keeps for an anchor whose tactics
# row it does not hold.
# ================
def client_controls():
	return {
		"ID": 0, "ObjectID": 0, "AIQoS": 0, "MaxStamina": 0, "StaminaVariance": 0,
		"SightRange": CLIENT_NEST_SIGHT_RANGE, "AggressType": 0, "AggressData": 0,
		"ChangeTarget": 0, "HelpRequest": 0, "HelpResponse": 0, "BattleStyle": 0,
		"BattleStyleData": 0, "DiversionBasis": 0, "DiversionKeepBasis": 0,
		"KeepDistance": 0, "KeepDistanceData": 0, "TraceType": 0, "TraceBoundary": 0,
		"TraceData": 0, "HomingType": 0, "HomingData": 0, "AggressOnHoming": 0,
		"FleeType": 0, "ChampionID": 0, "Flags": 0,
		"DiversionBasisData": [0] * 8, "DiversionKeepBasisData": [0] * 8,
	}


# ================
# build_population
#
# The field nests from the placements alone, in the Version-2 envelope
# the cap-110 populations use. One placement of an admitted kind is one
# nest standing on its point, with the evidence's commonest home radius
# and spawn spread, holding one monster. Nest identities are sequential over the sorted placements,
# so a rebuild of the same client writes the same file.
# ================
def build_population(found, refs):
	ordered = sorted((p for p in found if p[0] in refs), key=lambda p: (p[1], p[0], p[2], p[3], p[4]))
	rows = []
	for nest_id, (code, region, x, y, z) in enumerate(ordered, 1):
		rows.append({
			"Codename": code, "NestID": nest_id, "WorldID": FIELD_WORLD, "RegionID": region,
			"X": x, "Y": y, "Z": z, "Radius": CLIENT_NEST_RADIUS,
			"GenerateRadius": CLIENT_NEST_GENERATE_RADIUS, "MaxCount": 1,
			"RespawnDelayMinSec": CLIENT_NEST_RESPAWN_MIN_SEC,
			"RespawnDelayMaxSec": CLIENT_NEST_RESPAWN_MAX_SEC,
			"Respawn": True, "InitialDir": 0, "ChampionGenPercentage": 0,
			"Controls": client_controls(), "Champion": None,
		})
	return {"Version": 2, "Rows": rows}


# ================
# o2_object_ids
# ================
def o2_object_ids(data):
	"""The object ids a JMXVMAPO1001 placement file uses."""
	if data[:MAPO_SIGNATURE_BYTES] != MAPO_SIGNATURE:
		raise ValueError("not a JMXVMAPO1001 placement file")
	ids, at = set(), MAPO_SIGNATURE_BYTES
	for _block in range(MAPO_BLOCKS):
		for _group in range(MAPO_LOD_GROUPS):
			(count,) = struct.unpack_from("<H", data, at)
			at += 2
			for _record in range(count):
				(object_id,) = struct.unpack_from("<I", data, at)
				ids.add(object_id)
				at += MAPO_RECORD_BYTES
	if at != len(data):
		raise ValueError(f"placement file is {len(data)} bytes, parsed {at}")
	return ids


# ================
# patch_o2_objects
# ================
def patch_o2_objects(data, remap):
	"""A placement file with its object ids remapped; scale bits kept."""
	if not remap:
		o2_object_ids(data)
		return data
	out = bytearray(data)
	at = MAPO_SIGNATURE_BYTES
	for _block in range(MAPO_BLOCKS):
		for _group in range(MAPO_LOD_GROUPS):
			(count,) = struct.unpack_from("<H", out, at)
			at += 2
			for _record in range(count):
				(object_id,) = struct.unpack_from("<I", out, at)
				if object_id in remap:
					struct.pack_into("<I", out, at, remap[object_id])
				at += MAPO_RECORD_BYTES
	return bytes(out)


# ================
# merge_object_catalog_remap
#
# The current object.ifo with the later rows added, and the remap of
# the used ids it could not keep. A later row fills an id the current
# catalogue lacks or holds as a placeholder. A used id both catalogues
# define differently moves past every existing id, and the caller
# patches the copied placements to match; an unused conflicted id
# keeps the current row, as every catalogue merge keeps the native row.
# ================
def merge_object_catalog_remap(current_raw, later_raw, used_ids):
	current_head, current_rows = variant.catalog_lines(current_raw)
	_, later_rows = variant.catalog_lines(later_raw)
	current = tables.object_catalog(current_raw)
	later = tables.object_catalog(later_raw)
	merged, remap = dict(current_rows), {}
	for key, row in later_rows.items():
		if key not in current_rows or tables.object_placeholder(current[key]):
			merged[key] = row
	next_id = max(list(current_rows) + list(later_rows) + [0]) + 1
	for key in sorted(used_ids):
		if key in current and key in later and current[key] != later[key] and not tables.object_placeholder(current[key]):
			merged[next_id] = variant.with_id(later_rows[key], next_id)
			remap[key] = next_id
			next_id += 1
	return variant.format_catalog(current_head[0], merged).encode("cp949"), remap


# ================


# ================
# later_object_texts
#
# The later client's name texts in the layout the gate and name merges
# consume (the VSRO textdata_object shape: service, key, Korean, ...,
# English in the ninth column). v1.696 shards textdata_object.txt
# behind a manifest, and its rows carry an identity column the VSRO
# rows lack: service, id, key, Korean, five locale slots this build
# leaves 0, the English text, three more slots, then English variants.
# The conversion keeps the Korean text and the first authored English
# text; the other v1.150 locales stay empty, as they are for a name the
# source never translated.
# ================
def later_object_texts(read):
	manifest = tables.table_rows(read("textdata_object.txt"))
	shards = [row[0].lower() for row in manifest if row and row[0].lower().endswith(".txt")]
	if not shards:
		raise ValueError("v1.696 textdata_object.txt names no shards")
	rows = []
	for shard in shards:
		for row in characters.translation_rows(read(shard)):
			if len(row) < 10 or not row[2]:
				continue
			english = next((row[at] for at in (9, 13, 14, 15, 16)
				if len(row) > at and row[at] not in ("0", "")), "")
			rows.append([row[0], row[2], row[3], "", "", "", "", "", english, ""])
	return rows


# ================
# copy_resource_closure
#
# Copy the closure of the seed resources (object and character models)
# from the later archives into the variant's extracted trees, walking
# the dependency audit the cap-110 item import uses. archives maps
# "Data" / "Particles" to (mmap, index). The walk declares the audit's
# explicit postures: monster sounds are Data files and walk with the
# models, and every particle set's effects walk, not only the equipment
# selector kind. A file the tree already holds (native or an earlier
# import) is parsed but never rewritten; every file written is returned
# as a path relative to the output root. With tolerate_gaps (the Arabia
# import), a file the archives name but do not hold, and a file the
# audit cannot parse, are recorded in the result instead of stopping
# the walk: later-client content ships with such holes, and inventing
# the missing bytes is not an option.
# ================
def copy_resource_closure(seeds, archives, native, output, tolerate_gaps=False):
	from cap110_item_import import normalized_path, resource_dependencies
	pending = {("Data", normalized_path(seed)) for seed in seeds}
	seen, written, missing_sounds, missing_effects = set(), [], [], []
	missing_files, unparsed_files = [], []
	while pending:
		batch, requests = sorted(pending), []
		pending = set()
		for archive_name, name in batch:
			if (archive_name, name) in seen:
				continue
			seen.add((archive_name, name))
			folder = ARCHIVE_FOLDERS[archive_name]
			data, index = archives[archive_name]
			# Some BSRs author sounds from the client's sound root
			# (sound\prim\snd\...); the archive stores the same file
			# without that root, which is the name it is copied under.
			candidates = [name]
			if name.startswith("sound/"):
				candidates.append(name[len("sound/"):])
			name = next((candidate for candidate in candidates
				if (Path(native) / folder / candidate).is_file()
				or (Path(output) / folder / candidate).is_file() or candidate in index), name)
			local = Path(native) / folder / name
			existing = Path(output) / folder / name
			entry = index.get(name)
			if local.is_file():
				raw, origin = local.read_bytes(), True
			elif existing.is_file():
				raw, origin = existing.read_bytes(), False
			elif entry is not None:
				raw, origin = sro_pk2.payload(data, entry), False
				write_atomically(safe_target(Path(output) / folder, name), raw)
				written.append(folder + "/" + name)
			elif name.endswith(SOUND_EXTENSIONS):
				missing_sounds.append(name)
				continue
			elif name.endswith(EFFECT_EXTENSIONS):
				missing_effects.append(name)
				continue
			elif tolerate_gaps:
				missing_files.append(f"{archive_name}:{name}")
				continue
			else:
				raise ValueError(f"missing {archive_name} resource: {name}")
			if name.endswith(PARSED_EXTENSIONS):
				requests.append({"path": name, "bytes": base64.b64encode(raw).decode("ascii"),
					"native": origin, "sounds": "data", "particles": "all"})
		if requests:
			try:
				dependencies = resource_dependencies(requests)
			except ValueError:
				if not tolerate_gaps:
					raise
				dependencies = []
				for request in requests:
					try:
						dependencies.append(resource_dependencies([request])[0])
					except ValueError:
						unparsed_files.append(request["path"])
						dependencies.append([])
			for names in dependencies:
				for dep in names:
					if isinstance(dep, dict):
						archive_name = dep.get("archive") or "Data"
						if archive_name not in ARCHIVE_FOLDERS:
							raise ValueError(f"resource closure left the later archives: {dep}")
						pending.add((archive_name, normalized_path(dep["path"])))
					else:
						pending.add(("Data", normalized_path(dep)))
	return {"files": written, "missingSounds": sorted(missing_sounds),
		"missingEffects": sorted(missing_effects), "missingFiles": sorted(missing_files),
		"unparsedFiles": sorted(unparsed_files)}


#============================================================================
# The import
#============================================================================


# ================
# remove_owned
#
# The off path, the Sky Temple family's one implementation (Arabia
# calls it with its own shard names and region prefix): everything
# the marker owns leaves - the shards, the population, the NPC
# standings and talk rows, the copied files and the owned name,
# characterinfo, animation and gate rows. The variant build rewrites
# the gate tables, the catalogues and mapinfo from the native tables
# on every run, so a standalone off run strips the gate rows itself
# and a build-run off finds none.
# ================
def remove_owned(output, prior, character_shard=CHARACTER_SHARD, skill_shard=SKILL_SHARD,
		population_file=POPULATION_FILE, marker=MARKER, region_prefix=REGION_PREFIX):
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
					if len(row) <= 2 or row[1] not in region_ids or not row[2].startswith(region_prefix)]))
	for name in (character_shard, skill_shard, population_file):
		(text_root / name).unlink(missing_ok=True)
	manifest = text_root / "skilldata.txt"
	if manifest.exists():
		write_atomically(manifest, monsters.encode_rows(
			[r for r in tables.table_rows(manifest.read_bytes()) if r[0].lower() != skill_shard]))
	for relative in prior.get("files", []):
		safe_target(output, relative).unlink(missing_ok=True)
	(output / marker).unlink(missing_ok=True)
	return {"enabled": False}


# ================
# add_skytemple
# ================
def add_skytemple(native, output, source=None, closure=copy_resource_closure):
	native, output = Path(native).resolve(), Path(output).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("sky temple output must be separate from the native extraction")
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
		fields, _named = field_regions(tables.table_rows(read("regioncode.txt")))
		if len(fields) != 288 or set(fields.values()) != {"A", "B", "C", "D"}:
			raise ValueError(f"v1.696 regioncode names {len(fields)} sky temple regions, expected 288 in four fields")
		found = placements(tables.table_rows(read("npcpos.txt")), codename_by_id, fields)
		if not found:
			raise ValueError("v1.696 npcpos places no sky temple monsters in the fields")
		refs = select_kinds(later, {code for code, *_rest in found})
		population = build_population(found, refs)

		native_chars = [r for p in text_root.glob("characterdata_*.txt") if p.name != CHARACTER_SHARD
			for r in tables.table_rows(p.read_bytes())]
		occupied = {int(r[1]) for r in native_chars}
		allocated = monsters.allocate_identities(occupied, sorted(refs), prior.get("characters", {}))
		for code, row in sorted(refs.items()):
			row[1] = str(allocated[code])
		native_skills = [r for p in text_root.glob("skilldata_*.txt") if p.name != SKILL_SHARD
			for r in tables.table_rows(p.read_bytes()) if len(r) == 118]
		needed = {int(s) for r in refs.values() for s in r[89:99] if int(s)}
		source_skills = [r for shard in tables.table_rows(read("skilldata.txt"))
			for r in tables.table_rows(read(shard[0].lower())) if len(r) > 3 and r[1].isdigit()]

		# ================
		# load_client_skills
		# ================
		def load_client_skills():
			return {int(r[1]): r for r in source_skills}

		source_skills = monsters.add_native_client_skills(source_skills, needed, native_skills, load_client_skills)
		added, skill_map = monsters.merge_skills(native_skills, source_skills, needed, prior.get("skills", {}))
		for row in refs.values():
			row[89:99] = [str(skill_map[int(s)]) if int(s) else "0" for s in row[89:99]]

		# Gates first: the merge rewrites textdataname.txt, and the name
		# step below appends onto what it leaves.
		current = {name: (text_root / name).read_bytes()
			for name in ("teleportdata.txt", "teleportbuilding.txt", "teleportlink.txt")}
		current["text"] = (text_root / "textdataname.txt").read_bytes()
		native_text_root = native / TEXT_ROOT
		object_texts = later_object_texts(read)
		later_gate_tables = {
			"teleportdata.txt": tables.table_rows(read("teleportdata.txt")),
			"teleportbuilding.txt": tables.table_rows(read("teleportbuilding.txt")),
			"teleportlink.txt": tables.table_rows(read("teleportlink.txt")),
			"text": monsters.encode_rows(object_texts),
		}
		pre_gate_keys = {row[1] for row in tables.table_rows(current["text"]) if len(row) > 1}
		gate_tables, gate_codes = prepare_gates(
			later_gate_tables,
			tables.table_rows((native_text_root / "teleportbuilding.txt").read_bytes()),
			tables.table_rows((native_text_root / "teleportdata.txt").read_bytes()),
			{row[1] for row in tables.table_rows(current["teleportdata.txt"]) if len(row) > 1},
			{row[1] for row in tables.table_rows(current["teleportbuilding.txt"]) if len(row) > 1},
			d_arrival(found, fields),
		)
		merged_gates, gate_summary = variant.merge_gate_tables(
			current, gate_tables, codes=gate_codes, level_cap=variant.gate_level_cap())
		for dropped in gate_summary.get("droppedConditions", []):
			tables.log(f"sky temple gates: link {dropped['from']} -> {dropped['to']}: condition kind "
				f"{dropped['kind']} dropped; the v1.150 server has no evaluator for it")
		for name, content in merged_gates.items():
			variant.replace_file(text_root / ("textdataname.txt" if name == "text" else name), content)

		write_atomically(text_root / CHARACTER_SHARD,
			monsters.encode_rows(sorted(refs.values(), key=lambda row: int(row[1]))))
		write_atomically(text_root / SKILL_SHARD, monsters.encode_rows(added))
		manifest = tables.table_rows((text_root / "skilldata.txt").read_bytes())
		manifest = [r for r in manifest if r[0].lower() != SKILL_SHARD] + [[SKILL_SHARD]]
		write_atomically(text_root / "skilldata.txt", monsters.encode_rows(manifest))

		base_text = (text_root / "textdataname.txt").read_bytes()
		known = {r[1] for r in characters.translation_rows(base_text) if len(r) > 1}
		wanted = {r[5] for r in refs.values()} - known
		texts = [native_text_row(r) for r in object_texts if len(r) >= 10 and r[1] in wanted]
		if wanted - {r[1] for r in texts}:
			raise ValueError("missing sky temple monster names")
		stale_names = set(prior.get("nameSymbols", [])) - {r[5] for r in refs.values()}
		if stale_names:
			base_text = monsters.encode_rows(monsters.drop_symboled_rows(
				characters.translation_rows(base_text), stale_names))
		write_atomically(text_root / "textdataname.txt", characters.append_rows(base_text, texts))

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
			if len(r) == characters.SKILL_EFFECT_COLUMNS and r[0].startswith(PREFIX) and r[0] not in known_info]
		owned_info = monsters.characterinfo_codenames(effect_text) & set(refs)
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
			if len(row) >= 4 and row[1].isdigit() and int(row[1]) in fields
			and current_by_id.get(row[1], [0, 0, "xxx"])[2] == "xxx"]
		replaced = {row[1] for row in code_rows}
		kept = [row for row in current_regions if len(row) < 2 or row[1] not in replaced]
		write_atomically(region_path, monsters.encode_rows(kept + code_rows))
		zone_path = text_root / "textzonename.txt"
		zone_keys = {row[1] for row in tables.table_rows(zone_path.read_bytes()) if len(row) > 1}
		zone_prefixes = tuple(sorted({REGION_PREFIX + letter + "_" for letter in "ABCD"}))
		zone_rows = [native_text_row(row) for row in tables.table_rows(read("textzonename.txt"))
			if len(row) >= 10 and (row[1] in {str(region) for region in fields} or row[1].startswith(zone_prefixes))
			and row[1] not in zone_keys]
		write_atomically(zone_path, characters.append_rows(zone_path.read_bytes(), zone_rows))

		write_atomically(text_root / POPULATION_FILE, (json.dumps(population) + "\n").encode())

		# Map: catalogues over the variant's current ones, terrain with
		# its tile ids remapped, placements with their object ids.
		map_path, map_handle, map_data = open_archive(root, "Map")
		try:
			map_index = tables.archive_index(map_data)
			archive_hashes["Map"] = tables.sha256_of(map_data)
			sectors = sorted({(region & 0xFF, (region >> 8) & 0xFF) for region in fields})
			terrain, used_tiles, used_objects = {}, set(), set()
			for sector_x, sector_y in sectors:
				for extension in tables.MAP_EXTENSIONS:
					archive_path = f"{sector_y}/{sector_x}{extension}"
					entry = map_index.get(archive_path)
					if entry is None:
						raise ValueError(f"v1.696 Map.pk2 has no {archive_path}")
					terrain[archive_path] = sro_pk2.payload(map_data, entry)
				used_tiles |= tables.terrain_tile_ids(terrain[f"{sector_y}/{sector_x}.m"])
				used_objects |= o2_object_ids(terrain[f"{sector_y}/{sector_x}.o2"])
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
					raise ValueError(f"sky temple terrain paints tile id {tile_id} missing from tile2d.ifo")
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

		# Data: the fields' navmesh and the model closures.
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
				raise ValueError("sky temple placements use object ids the merged catalogue lacks")
			seeds = {merged_objects[object_id][1] for object_id in final_ids}
			seeds |= {"res/" + row[characters.MODEL_COLUMN].replace("\\", "/")
				for row in refs.values() if row[characters.MODEL_COLUMN] not in ("", "xxx")}
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
			"characters": {code: int(row[1]) for code, row in refs.items()},
			"skills": {row[3]: int(row[1]) for row in added},
			"nests": len(population["Rows"]),
			"kinds": len(refs),
			"regions": sorted(fields),
			"sectors": [[x, y] for x, y in sectors],
			"placements": len(found),
			"tileRemap": {str(k): v for k, v in sorted(tile_remap.items())},
			"objectRemap": {str(k): v for k, v in sorted(object_remap.items())},
			"tileTextureClashes": sorted(e[len("clash:"):] for e in copied if e.startswith("clash:")),
			"missingSounds": closure_result["missingSounds"],
			"missingEffects": closure_result["missingEffects"],
			"gates": gate_summary,
			"gateCodes": list(gate_codes),
			"gateBuildingCodes": sorted(row[2] for row in tables.table_rows(gate_tables["teleportbuilding.txt"])),
			"gateTextSymbols": sorted(({
				row[4] for row in tables.table_rows(gate_tables["teleportdata.txt"]) if len(row) > 4
			} | {
				row[5] for row in tables.table_rows(gate_tables["teleportbuilding.txt"]) if len(row) > 5
			}) - pre_gate_keys),
			"animationBases": sorted(set(prior.get("animationBases", []))
				| {base for rows in selected.values() for base, _ in rows}),
			"characterInfo": sorted(owned_info | {row[0] for row in info}),
			"nameSymbols": sorted(({r[1] for r in characters.translation_rows(base_text) if len(r) > 1}
				| {r[1] for r in texts}) & {r[5] for r in refs.values()}),
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
	args = parser.parse_args()
	print(json.dumps(add_skytemple(args.native_extracted, args.output, args.source_root), indent=2))


if __name__ == "__main__":
	main()
