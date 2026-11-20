"""
===========================================================================

build_alexandria_variant.py - stage 2 of Alexandria: a v1.150 + Alexandria game root

PORT-ONLY, NOT NATIVE. Off unless SRO_ALEXANDRIA is "1", "on" or "true".

The asset pipeline binds to one game root (paths.mjs, sro_paths.py), so the
safe way to build the city without touching the native extraction is a second
game root. This tool makes it, from three inputs:

	the native v1.150 extraction     SRO_GAME_ROOT/extracted
	the stage 1 import               extracted/Alexandria_extracted
	the VSRO client                  --vsro-root (Data.pk2 for the models)

The VSRO archives are hash-pinned (vsro_archive_pins.json, checked
before anything is read; a missing or changed archive fails the build,
#1745). The archives are the operator's own and are never committed.

into <variant root>/extracted/:

	Media_extracted, Particles_extracted, Music_mp3
		hard links to the native files (copies where links are refused);
		Particles_extracted also gains the VSRO effect files the native lacks.
	Map_extracted
		native files, plus the city's 36 regions with their ground-tile ids
		remapped where the VSRO catalog disagrees with the native one,
		plus tile2d.ifo / object.ifo merged (the native row wins; a VSRO row
		fills a native placeholder or a new id), the new tile textures, and
		the VSRO environment.ifo (a superset; see ENVIRONMENT_NOTE).
	Data_extracted
		native files, plus every VSRO Data.pk2 file the native tree lacks
		(the city's buildings and their materials and textures), the city's
		navmesh, and navmesh/mapinfo.mfo with the city's bits set.

No native file is ever overwritten or changed: a link is never written
through, and a VSRO file is added only where no native file of that name
exists, except source-authored overview tiles covering imported regions.
A rerun with the same code, flags and sources over an untouched tree is
skipped (variant_stamp.py); --force rebuilds anyway.

Build the variant with its own generated tree:

	SRO_GAME_ROOT=<variant root> SRO_GENERATED_ROOT=<variant root>/.generated pnpm assets build full

===========================================================================
"""
import argparse
import json
import mmap
import os
import re
import shutil
import struct
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import alexandria_npcs as npcs  # noqa: E402
import area_music  # noqa: E402
from alexandria_map_tables import (  # noqa: E402
	MAP_ALEXANDRIA_ID, MAPINFO_COLUMNS, LOCALINFO_COLUMNS, MAP_PIN_IDS,
	OVERVIEW_ID, OVERVIEW_KIND, OVERVIEW_BOX, OVERVIEW_TEXTURE, OVERVIEW_DRAW,
	OVERVIEW_TILE_REGIONS, MAP_TEXTURES, table_text, append_rows, native_text_row,
	merge_map_tables, overview_box, widen_overview, page_tiles, map_media_paths
)
import import_vsro_alexandria as stage1  # noqa: E402
import pharaoh_temple as temple  # noqa: E402
import sro_pk2  # noqa: E402
import variant_settle  # noqa: E402
import variant_stamp  # noqa: E402
from build_parallelism import build_jobs  # noqa: E402
from prepare_client_resources import safe_target, write_atomically  # noqa: E402
from sro_paths import EXTRACTED_ROOT  # noqa: E402

VARIANT_MANIFEST = ".opensro-alexandria-variant.json"
LINKED_FOLDERS = ("Media_extracted", "Particles_extracted", "Music_mp3")
# Game-root files the build reads besides extracted/: the doctor and the
# builders want the client and Particles.pk2 beside it.
LINKED_FILES = ("SRO_Client.exe", "Particles.pk2")
# environment.ifo: 45 native entries against the VSRO's 60. Keys 6 and 11
# differ between the clients, so the variant's regions that use them sky
# slightly differently from native v1.150; a binary merge would avoid that.
ENVIRONMENT_NOTE = "environment keys 6 and 11 differ from native in the variant"

TEXTDATA = stage1.TEXTDATA
# Port-only, not native: the city's gates and ferry, plus the two authored
# outdoor crossings: Delta East is Abundance Ground; Delta West is Forbidden Plain.
# Ferry teleportdata names an NPC, not a teleportbuilding.
# The crossings' far ends come from stage 1, which imports their terrain.
GATE_CODES = (
	"GATE_SD1", "GATE_SD2", "GATE_NPC_SD_M_AREA_FERRY", "GATE_DELTA_GATE_E", "GATE_DELTA_GATE_W"
) + stage1.CROSSING_GATE_CODES
# Port-only, not native: the Pharaoh Tomb's gates, added with SRO_PHARAOH_TOMB.
# The one entrance in Kings Valley links to the three tomb exits. The source's
# GATE_TOMBOSIRIS_* rows have no position and are not carried.
TOMB_GATE_CODES = (
	"GATE_TOMB_GATE_IN", "GATE_TOMB_GATE_OUT_1", "GATE_TOMB_GATE_OUT_2", "GATE_TOMB_GATE_OUT_3"
)
# Port-only, not native: the Pharaoh Temple's four gates, added with
# SRO_PHARAOH_TEMPLE (pharaoh_temple.py): the two clicked desert gates and the
# two walk-in gates inside the Temple. Their world column is flattened (below).
TEMPLE_GATE_CODES = temple.GATE_CODES
# The last teleportdata column names the destination game world. The source's
# tomb exits name worlds 57-59, one per tomb. The v1.150 world table does not
# define them, but the port does while SRO_PHARAOH_TOMB is on (the three
# permanent per-tomb worlds, #1750), so the column stands as authored and the
# server resolves it through its widened world catalogue.
TELEPORT_WORLD_COLUMN = 12
# The v1.150 server's level cap (levels.json levelCap). The VSRO links need
# level 95 for a cap of 110; clamped here so the city stays reachable.
NATIVE_LEVEL_CAP = 90
# The server's level-cap flag (progression/levelcap.go LevelCapFromEnv): the
# gate conditions are clamped to the cap the shard runs, read with the same
# values the server accepts so build and server can never disagree.
LEVEL_CAP_FLAG = "SRO_LEVEL_CAP"
# The shops' stock choice (#2846): unset, the Alexandria variant stocks
# each store with the goods one official source had active; on, the
# shop passes stock the port's own degree goods instead.
PORT_STOCK_FLAG = "SRO_ALEXANDRIA_PORT_STOCK"
SWITCH_OFF_VALUES = ("", "0", "off", "false")
SWITCH_ON_VALUES = ("1", "on", "true")
EXPANSION_LEVEL_CAP = 110
LEVEL_CAP_NATIVE_VALUES = ("", "0", "off", "false", "90")
LEVEL_CAP_EXPANSION_VALUES = ("110",)
LINK_COLUMNS = 21
LINK_KIND_LEVEL = 1
# The condition kinds the v1.150 server's portal catalogue evaluates
# (portalAdmission): 1 level, 2 transport, 4 and 5 an item held (5 spends
# one), 6 outside job mode (portal_conditions.go, #3399), 7 thief suit
# with a transport (#1749 PR-C(ii)). Any other kind is dropped and
# reported, below.
LINK_KINDS_EVALUATED = (1, 2, 4, 5, 6, 7)
# The item kinds: Data_1 is an item identity of the source client, which
# the variant's item tables number differently.
LINK_KINDS_ITEM = (4, 5)
TELEPORT_COLUMNS = 13
BUILDING_COLUMNS = 58


# The VSRO characterdata files (characterdata.txt lists them).
VSRO_CHARACTER_FILES = (5000, 10000, 15000, 20000, 25000, 30000, 35000, 40000, 45000)

MFO_SIGNATURE = b"JMXVMFO 1000"
MFO_HEADER_BYTES = 24
TILE_ID_LIMIT = 0x0400
MAPM_TEXTURE_SCALE_MASK = 0xFC00

CATALOG_ROW = re.compile(r"^(\d+)(\s.*)$")


# ================
# log
# ================
def log(message):
	print(message, flush=True)


#============================================================================
# Catalogs
#============================================================================


# ================
# catalog_lines
# ================
def catalog_lines(raw):
	"""(header lines, {id: row line}) of a tile2d.ifo or object.ifo."""
	lines = stage1.decode_table(raw).splitlines()
	rows = {}
	for line in lines[2:]:
		match = CATALOG_ROW.match(line.strip())
		if match:
			rows[int(match.group(1))] = line.strip()
	return lines[:2], rows


# ================
# format_catalog
# ================
def format_catalog(signature, rows):
	"""A catalog file's text: signature, count, then rows in id order."""
	ordered = [rows[key] for key in sorted(rows)]
	return "\r\n".join([signature, str(len(ordered)), *ordered]) + "\r\n"


# ================
# with_id
# ================
def with_id(row, tile_id):
	"""A catalog row renumbered; ids are five digits in tile2d.ifo."""
	return f"{tile_id:05d}" + CATALOG_ROW.match(row).group(2)


# ================
# merge_tile_catalog
# ================
def merge_tile_catalog(native_raw, vsro_raw, used_ids):
	"""
	The merged tile2d.ifo and the remap of the used ids it could not keep.
	A VSRO row fills an id the native catalog lacks. A used id the native
	catalog also defines, as a placeholder or as a different texture, moves to
	a new id past every existing one; unused ones stay native.
	"""
	native_head, native_rows = catalog_lines(native_raw)
	_, vsro_rows = catalog_lines(vsro_raw)
	native_tiles = stage1.tile_catalog(native_raw)
	vsro_tiles = stage1.tile_catalog(vsro_raw)
	# Only ids the native catalog lacks are filled in place. A native
	# placeholder is not: native regions may paint it, and giving it a real
	# name would ask the build for a texture they never had.
	merged, remap = dict(native_rows), {}
	for key, row in vsro_rows.items():
		if key not in native_rows:
			merged[key] = row
	next_id = max(max(native_rows), max(vsro_rows)) + 1
	for key in sorted(used_ids):
		if key in native_rows and native_tiles[key] != vsro_tiles[key]:
			if next_id >= TILE_ID_LIMIT:
				raise SystemExit("tile2d.ifo has no free tile id left for the remap")
			merged[next_id] = with_id(vsro_rows[key], next_id)
			remap[key] = next_id
			next_id += 1
	return format_catalog(native_head[0], merged).encode("cp949"), remap


# ================
# merge_object_catalog
# ================
def merge_object_catalog(native_raw, vsro_raw):
	"""The merged object.ifo: native rows win, VSRO fills the rest."""
	native_head, native_rows = catalog_lines(native_raw)
	_, vsro_rows = catalog_lines(vsro_raw)
	native_objects = stage1.object_catalog(native_raw)
	merged = dict(native_rows)
	for key, row in vsro_rows.items():
		if key not in native_rows or stage1.object_placeholder(native_objects[key]):
			merged[key] = row
	return format_catalog(native_head[0], merged).encode("cp949")


#============================================================================
# Terrain and availability
#============================================================================


# ================
# remap_terrain
# ================
def remap_terrain(terrain, remap):
	"""A .m terrain file with its ground tile ids remapped; scale bits kept."""
	stage1.terrain_tile_ids(terrain)
	if not remap:
		return terrain
	data = bytearray(terrain)
	for block in range(stage1.MAP_BLOCKS):
		first = stage1.MAPM_SIGNATURE_BYTES + block * stage1.MAPM_BLOCK_BYTES + stage1.MAPM_BLOCK_HEAD_BYTES
		for vertex in range(stage1.MAPM_VERTICES):
			at = first + vertex * stage1.MAPM_VERTEX_BYTES + stage1.MAPM_VERTEX_TEXTURE_OFFSET
			value = struct.unpack_from("<H", data, at)[0]
			tile = value & stage1.MAPM_TEXTURE_ID_MASK
			if tile in remap:
				struct.pack_into("<H", data, at, (value & MAPM_TEXTURE_SCALE_MASK) | remap[tile])
	return bytes(data)


# ================
# enable_regions
# ================
def enable_regions(mapinfo, regions):
	"""mapinfo.mfo with the (sectorX, sectorY) regions' availability bits set."""
	if mapinfo[:len(MFO_SIGNATURE)] != MFO_SIGNATURE:
		raise ValueError("not a JMXVMFO availability file")
	data = bytearray(mapinfo)
	for sector_x, sector_y in regions:
		region_id = (sector_y << 8) | sector_x
		data[MFO_HEADER_BYTES + (region_id >> 3)] |= 0x80 >> (region_id & 7)
	return bytes(data)


#============================================================================
# Gate tables
#============================================================================


# ================
# by_column
# ================
def by_column(raw, column, width=0):
	"""{value of column: row} of a table, keeping rows at least width wide."""
	return {row[column]: row for row in stage1.table_rows(raw) if len(row) > column and len(row) >= width}


# ================
# translate_link
# ================
def translate_link(row, ids, level_cap, dropped=None, items=None):
	"""
	One VSRO teleportlink row for the native table: its two ends renumbered by
	teleport code, the scheduling word set to the one v1.150 server accepts
	(1), a level condition clamped to the build's cap, an item condition's
	item renumbered through items ({source item id: variant item id}) and
	conditions of a kind the server does not evaluate dropped. Every drop is
	appended to dropped (when given) as {"from", "to", "kind", "operands"}
	with the renumbered ends, so the loss is reported, never silent (#1749
	PR-C(i); kind 3 still has no port evaluator).

	Returns None when an item condition names an item items does not map:
	dropping that condition would open a keyed gate to everyone, so the
	whole link stays out (merge_gate_tables reports it).
	"""
	columns = list(row[:LINK_COLUMNS])
	columns[1], columns[2] = ids[row[1]], ids[row[2]]
	columns[4], columns[5] = "1", "0"
	for at in range(6, LINK_COLUMNS, 3):
		if columns[at] == str(LINK_KIND_LEVEL) and int(columns[at + 1]) > level_cap:
			columns[at + 1] = str(level_cap)
		if int(columns[at]) in LINK_KINDS_ITEM:
			item = (items or {}).get(columns[at + 1])
			if item is None:
				return None
			columns[at + 1] = item
			continue
		# A condition kind the v1.150 server does not evaluate is dropped;
		# the server refuses the whole catalogue otherwise.
		if int(columns[at]) != 0 and int(columns[at]) not in LINK_KINDS_EVALUATED:
			if dropped is not None:
				dropped.append({
					"from": columns[1], "to": columns[2],
					"kind": int(columns[at]), "operands": columns[at + 1:at + 3]
				})
			columns[at:at + 3] = ["0", "0", "0"]
	return columns


# ================
# merge_gate_tables
# ================
def merge_gate_tables(native, vsro, codes=GATE_CODES, level_cap=NATIVE_LEVEL_CAP, flatten=(), items=None):
	"""
	The native tables with the gates in codes added. native and vsro map
	"teleportdata.txt", "teleportbuilding.txt", "teleportlink.txt" and
	"text" (textdataname / textdata_object) to raw bytes. Teleport ids differ
	between the clients, so links are renumbered by code; the new gates take
	their VSRO ids, which the native table does not use. A gate named in
	flatten has its destination world set to the persistent world (the
	temple's source world 60 does not exist in the port). items maps the
	source client's item ids to the variant's for item conditions
	(translate_link); a link whose item does not map is left out and
	listed under omittedLinks. Returns {name: bytes} for the changed files,
	and the list of gates added.
	"""
	native_gates = by_column(native["teleportdata.txt"], 2, TELEPORT_COLUMNS)
	vsro_gates = by_column(vsro["teleportdata.txt"], 2, TELEPORT_COLUMNS)
	native_ids = {code: row[1] for code, row in native_gates.items()}
	vsro_codes = {row[1]: code for code, row in vsro_gates.items()}
	new_ids = {row[1] for code, row in vsro_gates.items() if code in codes}
	if len(new_ids) != len(codes):
		raise SystemExit(f"VSRO teleportdata lacks some of {codes}")
	if new_ids & {row[1] for row in native_gates.values()}:
		raise SystemExit("a VSRO gate id is already a native teleport id")
	ids = {vsro_id: native_ids.get(code, vsro_id) for vsro_id, code in vsro_codes.items()}

	data_rows = []
	for code in codes:
		row = list(vsro_gates[code])
		if code in flatten:
			row[TELEPORT_WORLD_COLUMN] = temple.SOURCE_WORLD_COLUMN_VALUE
		data_rows.append(row)
	building_ids = {row[3] for row in data_rows}
	buildings = [
		row for row in stage1.table_rows(vsro["teleportbuilding.txt"])
		if len(row) >= BUILDING_COLUMNS and row[1] in building_ids
	]
	links, dropped_conditions, omitted_links = [], [], []
	for row in stage1.table_rows(vsro["teleportlink.txt"]):
		if len(row) != LINK_COLUMNS or row[0] != "1" or not ({row[1], row[2]} & new_ids):
			continue
		other = row[2] if row[1] in new_ids else row[1]
		if other not in new_ids and (other not in vsro_codes or vsro_codes[other] not in native_ids):
			continue
		translated = translate_link(row, ids, level_cap, dropped_conditions, items)
		if translated is None:
			omitted_links.append({
				"from": ids[row[1]], "to": ids[row[2]],
				"items": sorted({row[at + 1] for at in range(6, LINK_COLUMNS, 3) if int(row[at]) in LINK_KINDS_ITEM})
			})
			continue
		links.append(translated)

	text_keys = {row[4] for row in data_rows} | {row[5] for row in buildings}
	native_keys = {row[1] for row in stage1.table_rows(native["text"]) if len(row) > 1}
	texts = {
		row[1]: row for row in stage1.table_rows(vsro["text"]) if len(row) >= 10 and row[1] in text_keys
	}
	text_rows = [native_text_row(texts[key]) for key in sorted(texts) if key not in native_keys]
	return {
		"teleportdata.txt": append_rows(native["teleportdata.txt"], data_rows),
		"teleportbuilding.txt": append_rows(native["teleportbuilding.txt"], buildings),
		"teleportlink.txt": append_rows(native["teleportlink.txt"], links),
		"text": append_rows(native["text"], text_rows)
	}, {
		"gates": list(codes), "links": len(links), "buildings": len(buildings), "texts": len(text_rows),
		# The server hides these structures while SRO_ALEXANDRIA is off (alexandria.go).
		"buildingCodes": sorted(row[2] for row in buildings),
		# Link conditions the native catalogue cannot evaluate yet, reported
		# so the loss is visible in the build output and manifest (#1749).
		**({"droppedConditions": dropped_conditions} if dropped_conditions else {}),
		# Keyed links whose item the variant does not carry (translate_link).
		**({"omittedLinks": omitted_links} if omitted_links else {}),
		**({"worldsFlattened": [code for code in codes if code in flatten]} if set(codes) & set(flatten) else {})
	}


#============================================================================
# Region names, world map and minimap
#============================================================================


#============================================================================
# Trees
#============================================================================


# ================
# link_tree
# ================
def link_tree(source, target):
	"""Hard-link every file of source under target (copy if links are refused)."""
	count = 0
	for folder, _, names in os.walk(source):
		destination = target / Path(folder).relative_to(source)
		destination.mkdir(parents=True, exist_ok=True)
		for name in names:
			link = destination / name
			if link.exists():
				continue
			try:
				os.link(Path(folder) / name, link)
			except OSError:
				shutil.copy2(Path(folder) / name, link)
			count += 1
	return count


# ================
# link_file
# ================
def link_file(source, link):
	"""Hard-link one file (copy if links are refused)."""
	try:
		os.link(source, link)
	except OSError:
		shutil.copy2(source, link)


# ================
# lower_paths
# ================
def lower_paths(root):
	"""Lower-case relative paths of every file under root."""
	return {
		(Path(folder) / name).relative_to(root).as_posix().lower()
		for folder, _, names in os.walk(root)
		for name in names
	}


#============================================================================
# Build
#============================================================================


# ================
# clear_content
#
# A build lays the variant's content down from nothing. The steps below
# only add (link_tree skips a target that exists, add_missing_files fills
# what the native tree lacks), so over last release's tree a re-extracted
# native file would keep its old bytes and a file the sources dropped
# would never leave (ops #368 review, 2026-11-16). Removing a hard link
# leaves the native file it names alone. The build state beside the tree
# (.state: the settle ledger, the asset caches) and the generated tree
# stay: settle gives each file whose bytes come back the same its old time
# (variant_settle.py), so the caches downstream still hit.
# ================
def clear_content(variant, native):
	out = variant / "extracted"
	if out.resolve() == Path(native).resolve() or Path(native).resolve() in out.resolve().parents:
		raise SystemExit(f"{out} is inside the native extraction; refusing to clear it")
	if out.exists():
		shutil.rmtree(out)
	for name in (VARIANT_MANIFEST,) + LINKED_FILES:
		(variant / name).unlink(missing_ok=True)
	return out


# ================
# build_variant
# ================
def build_variant(vsro_root, native, variant, imported_root=None):
	"""Make the variant's extracted/ tree; returns its manifest."""
	imported = Path(imported_root) if imported_root else native / stage1.OUTPUT_FOLDER
	# Stage 1 of this build hashed these archives minutes ago (#3590); an
	# unchanged file with today's pinned digest is not hashed again.
	stage1.admit_verified_archives(imported)
	stage1.require_vsro_pin(vsro_root, stage1.ARCHIVE_NAMES)
	stage_manifest = json.loads((imported / stage1.MANIFEST_NAME).read_text(encoding="utf-8"))
	regions = [(r["sectorX"], r["sectorY"]) for r in stage_manifest["regions"]]
	out = clear_content(variant, native)
	out.mkdir(parents=True)

	linked = {}
	for folder in LINKED_FOLDERS + ("Map_extracted", "Data_extracted"):
		if not (native / folder).is_dir():
			raise SystemExit(f"{native / folder} is missing; run pnpm assets prepare for the v1.150 client first")
		linked[folder] = link_tree(native / folder, out / folder)
		log(f"{folder}: {linked[folder]} files linked")

	for name in LINKED_FILES:
		source = native.parent / name
		if not source.is_file():
			raise SystemExit(f"{source} is missing; the variant needs the v1.150 client files")
		if not (variant / name).exists():
			link_file(source, variant / name)

	# Map: catalogs, remapped regions, tile textures.
	vsro_catalogs = {name: (imported / "Map" / name).read_bytes() for name in stage1.CATALOGS}
	tiles, remap = merge_tile_catalog(
		(native / "Map_extracted" / "tile2d.ifo").read_bytes(), vsro_catalogs["tile2d.ifo"], set(stage_manifest["tileIds"])
	)
	objects = merge_object_catalog((native / "Map_extracted" / "object.ifo").read_bytes(), vsro_catalogs["object.ifo"])
	replace_file(out / "Map_extracted" / "tile2d.ifo", tiles)
	replace_file(out / "Map_extracted" / "object.ifo", objects)

	added, clashes = [], []
	for source in sorted((imported / "Map").rglob("*")):
		if not source.is_file() or source.name in stage1.CATALOGS:
			continue
		relative = source.relative_to(imported / "Map").as_posix()
		target = out / "Map_extracted" / relative
		stored = source.read_bytes()
		if relative.endswith(".m"):
			stored = remap_terrain(stored, remap)
		if target.exists() and relative.startswith("tile2d/"):
			if target.read_bytes() != stored:
				clashes.append(relative)
			continue
		replace_file(target, stored)
		added.append("Map_extracted/" + relative)

	# Data: every VSRO file the native tree lacks, the navmesh, the bitmap.
	present = lower_paths(native / "Data_extracted")
	data_added = add_missing_files(vsro_root, "Data", out / "Data_extracted", present, ("navmesh/",))
	# Particles: the city's effect programs (alex_*.efp) and what they use.
	particles_added = add_missing_files(
		vsro_root, "Particles", out / "Particles_extracted", lower_paths(native / "Particles_extracted")
	)
	for source in sorted((imported / "Data").rglob("*.nvm")):
		replace_file(out / "Data_extracted" / source.relative_to(imported / "Data").as_posix(), source.read_bytes())
	mapinfo = out / "Data_extracted" / "navmesh" / "mapinfo.mfo"
	replace_file(mapinfo, enable_regions((native / "Data_extracted" / "navmesh" / "mapinfo.mfo").read_bytes(), regions))

	gates, gate_tables = add_gates(vsro_root, native, out)
	stranded = stage1.gates_without_terrain(gate_tables["teleportdata.txt"], GATE_CODES,
		set(regions) | stage1.mfo_regions((native / "Data_extracted" / "navmesh" / "mapinfo.mfo").read_bytes()))
	if stranded:
		raise SystemExit(f"gates {', '.join(stranded)} stand on regions stage 1 did not import; rerun stage 1")
	pharaoh_temple = temple.add_temple(vsro_root, native, out)
	# The map step adds to textdataname.txt on top of the gates' texts.
	city_map = add_map(vsro_root, native, out, regions, gate_tables["text"])
	city_npcs = add_npcs(vsro_root, native, out, regions)
	# Imported equipment is independently optional; its off path also removes
	# a shard from a previous on build so a reused variant cannot retain it.
	from cap110_item_import import add_items
	weapons = add_items(vsro_root, native, out)
	# The mall avatars append their own shard to the manifest the item
	# import just wrote, so they must follow it.
	from mall_avatar_import import add_avatars, add_source, v1696_root, V1696_SOURCE, avatars_zip, AVATARS_SOURCE
	mall_avatars = add_avatars(vsro_root, native, out)
	# The v1.696 client's avatars the VSRO client lacks (SRO_V1696_ROOT,
	# pinned by v1696_archive_pins.json). Unset removes a previous import.
	later_avatars = add_source(v1696_root(), native, out, V1696_SOURCE)
	# The v2.355 export's avatars from the pinned archive (SRO_AVATARS_ZIP,
	# #3254): without it the build is byte-identical and a previous
	# import is removed.
	archive_avatars = add_source(avatars_zip(), native, out, AVATARS_SOURCE)
	# The Devil's Spirit (Nasrun) rows, inert (SRO_NASRUN, #3348): after the
	# avatars, whose shards and identities they keep clear of.
	from nasrun_import import add_nasrun
	nasrun = add_nasrun(vsro_root, native, out)
	# The later clients' pets follow the avatars: their COS rows and
	# summon scrolls append their own shards to the same manifests. The
	# v1.696 pass and the pinned-archive pass read their sources from the
	# environment and skip cleanly while those are not on the machine.
	from pet_import import add_all_sources
	pets = add_all_sources(vsro_root, native, out)
	from cap110_skill_import import add_skills
	skill_tier = add_skills(vsro_root, native, out)
	from cap110_skill_import import add_skill_effects
	skill_animation = add_skill_effects(vsro_root, native, out)
	# The skills a worn Devil's Spirit lends (SRO_NASRUN, #3456): after
	# the skill tier, which rewrites the skill manifest from the native
	# one, and before the monsters, whose skill ids keep clear of them.
	from nasrun_import import add_nasrun_skills
	nasrun_skills = add_nasrun_skills(vsro_root, native, out)
	from cap110_monsters import add_monsters
	field_monsters = add_monsters(vsro_root, native, out)
	# The monsters' own database rewards follow the population they describe.
	from cap110_rewards import add_rewards
	monster_rewards = add_rewards(vsro_root, native, out)
	# The later sources' optional layers, in their load-bearing order
	# (variant_later_layers.py owns the order and the manifest keys).
	from variant_later_layers import add_later_layers
	later_layers = add_later_layers(native, out)
	# The Roc range's kinds (SRO_ROC, #3399): after every later layer,
	# so their identities allocate above all of the layers' rows.
	from roc_import import add_roc
	roc = add_roc(vsro_root, native, out)
	# The two siege fortresses the v1.188 client authored and v1.150 never
	# placed (SRO_FORT_BIJEOKDAN, SRO_FORT_HOTAN, #3886): after Roc, in a
	# fixed order, so their structures allocate above every row before.
	import bandit_fortress_import
	import hotan_fortress_import
	bandit_fortress = bandit_fortress_import.add(vsro_root, native, out)
	hotan_fortress = hotan_fortress_import.add(vsro_root, native, out)
	# The honor line the client authored and never shipped (SRO_HONOR_GOODS,
	# #3548): after every other item import, whose identities it keeps
	# clear of, and before the shops that sell it.
	import honor_items
	honor_goods = honor_items.add_honor_items(native, out)
	from alexandria_npc_services import add_services
	city_services = add_services(vsro_root, native, out, port_stock_enabled(), honor_goods["enabled"])
	# The later client's seasonal town decoration (Halloween and the other
	# event layers), into the object catalog and region files written above.
	# Without the v1.696 archives a previous import is removed.
	from world_layer_import import add_layers
	from mall_avatar_import import V1696_SOURCE, source_pins, v1696_root
	later = v1696_root()
	if later is not None and not all((later / f"{name}.pk2").is_file() for name in ("Map", "Data", "Media", "Particles")):
		log(f"world layers: {later} lacks the v1.696 archives; no seasonal layers")
		later = None
	world_layers = add_layers(later, native, out, source_pins(V1696_SOURCE) if later else None)
	# Music last: its areas are those of every region the steps above left
	# in the tree, stage 1's, the temple's and each later layer's (#3546).
	music = area_music.add_music(vsro_root, native, out, later)

	environment = read_vsro_file(vsro_root, "Map", "environment.ifo")
	replace_file(out / "Map_extracted" / "environment.ifo", environment)

	manifest = {
		"fieldMonsters": field_monsters,
		"monsterRewards": monster_rewards,
		**later_layers,
		"worldLayers": world_layers,
		"format": "opensro-alexandria-variant-v1",
		"portOnly": True,
		"regions": stage_manifest["regions"],
		"tileRemap": {str(k): v for k, v in sorted(remap.items())},
		"tileTextureClashes": clashes,
		"mapFilesAdded": len(added),
		"dataFilesAdded": data_added,
		"particleFilesAdded": particles_added,
		"gates": gates,
		"map": city_map,
		"music": music,
		"npcs": city_npcs,
		"npcServices": city_services,
		"honorGoods": honor_goods,
		"weapons": weapons,
		"mallAvatars": mall_avatars,
		"mallAvatarsV1696": later_avatars,
		"mallAvatarsArchive": archive_avatars,
		"nasrun": nasrun,
		"nasrunSkills": nasrun_skills,
		"roc": roc,
		"pets": pets["pets"],
		"petsV1696": pets["petsV1696"],
		"petsArchive": pets["petsArchive"],
		"skills": skill_tier,
		"skillEffects": skill_animation,
		"linked": linked,
		"note": ENVIRONMENT_NOTE
	}
	if pharaoh_temple["enabled"]:
		# Absent while off, so an off manifest is what it was before the temple.
		manifest["pharaohTemple"] = pharaoh_temple
	# Absent while off, as the temple: an off manifest stays byte-for-byte.
	if bandit_fortress["enabled"]:
		manifest["banditFortress"] = bandit_fortress
	if hotan_fortress["enabled"]:
		manifest["hotanFortress"] = hotan_fortress
	write_atomically(variant / VARIANT_MANIFEST, (json.dumps(manifest, indent=2) + "\n").encode("utf-8"))
	return manifest


# ================
# gate_level_cap
# ================
def gate_level_cap(environ=None):
	"""
	The level cap the gate conditions are clamped to, from SRO_LEVEL_CAP with
	the server's own values: 90 unless "110"; anything else is refused, as the
	server refuses to start on it.
	"""
	value = (os.environ if environ is None else environ).get(LEVEL_CAP_FLAG, "").strip().lower()
	if value in LEVEL_CAP_NATIVE_VALUES:
		return NATIVE_LEVEL_CAP
	if value in LEVEL_CAP_EXPANSION_VALUES:
		return EXPANSION_LEVEL_CAP
	raise ValueError(f"{LEVEL_CAP_FLAG} must be off, 90 or 110, not {value!r}")


# ================
# build_switch
#
# An on/off build flag: unset or an off value is False (native), an on
# value True. Anything else is refused, as the server refuses a value it
# does not know.
# ================
def build_switch(flag, environ=None):
	value = (os.environ if environ is None else environ).get(flag, "").strip().lower()
	if value in SWITCH_OFF_VALUES:
		return False
	if value in SWITCH_ON_VALUES:
		return True
	raise ValueError(f"{flag} must be off, on or unset, not {value!r}")


# ================
# port_stock_enabled
#
# The shops' stock choice (#2846), from SRO_ALEXANDRIA_PORT_STOCK: off -
# the default, and the value every official build has - asks the shop
# passes for each store's official stock; on asks for the port's own
# degree goods.
# ================
def port_stock_enabled(environ=None):
	return build_switch(PORT_STOCK_FLAG, environ)


# ================
# add_gates
# ================
def add_gates(vsro_root, native, out):
	"""Write the gate rows into the variant's textdata; returns their summary and tables."""
	level_cap = gate_level_cap()
	textdata = Path("Media_extracted") / TEXTDATA
	names = ("teleportdata.txt", "teleportbuilding.txt", "teleportlink.txt")
	tables = {name: (native / textdata / name).read_bytes() for name in names}
	tables["text"] = (native / textdata / "textdataname.txt").read_bytes()
	vsro = {name: read_vsro_file(vsro_root, "Media", TEXTDATA + name) for name in names}
	vsro["text"] = read_vsro_file(vsro_root, "Media", TEXTDATA + "textdata_object.txt")
	from cap110_monsters import tomb_enabled
	codes = GATE_CODES + TOMB_GATE_CODES if tomb_enabled() else GATE_CODES
	if temple.temple_enabled():
		codes += TEMPLE_GATE_CODES
	merged, summary = merge_gate_tables(
		tables, vsro, codes=codes, level_cap=level_cap, flatten=TEMPLE_GATE_CODES
	)
	for dropped in summary.get("droppedConditions", []):
		log(f"gates: link {dropped['from']} -> {dropped['to']}: condition kind {dropped['kind']} "
			f"({', '.join(dropped['operands'])}) dropped; the v1.150 server has no evaluator for it")
	log_omitted_links("gates", summary)
	for name, content in merged.items():
		replace_file(out / textdata / ("textdataname.txt" if name == "text" else name), content)
	return summary, merged


# ================
# log_omitted_links
#
# One line per keyed link merge_gate_tables left out: the gate it would
# have joined stays shut rather than open without its key.
# ================
def log_omitted_links(label, summary):
	for omitted in summary.get("omittedLinks", []):
		log(f"{label}: link {omitted['from']} -> {omitted['to']} left out; the variant carries no item "
			f"{', '.join(omitted['items'])} its condition names")


# ================
# add_map
# ================
def add_map(vsro_root, native, out, regions, text_base):
	"""
	Write the map tables, local-map art and minimap tiles; returns a summary.
	text_base is textdataname.txt as the gate step left it.
	"""
	textdata = Path("Media_extracted") / TEXTDATA
	names = (
		"regioncode.txt", "textzonename.txt", "worldmap_mapinfo.txt", "worldmap_localinfo.txt", "textdataname.txt"
	)
	tables = {name: (native / textdata / name).read_bytes() for name in names}
	tables["textdataname.txt"] = text_base
	vsro = {name: read_vsro_file(vsro_root, "Media", TEXTDATA + name) for name in names[:4]}
	vsro["textdata_object.txt"] = read_vsro_file(vsro_root, "Media", TEXTDATA + "textdata_object.txt")
	merged, summary = merge_map_tables(tables, vsro, regions)
	for name, content in merged.items():
		replace_file(out / textdata / name, content)

	present = lower_paths(native / "Media_extracted")
	native_box = overview_box(tables["worldmap_mapinfo.txt"])
	overrides = set(page_tiles(summary["overview"], native_box, regions))
	icons = {row[2].replace("\\", "/").lower() for row in stage1.table_rows(merged["worldmap_localinfo.txt"])
		if len(row) == LOCALINFO_COLUMNS and row[1] == "2" and row[2].lower().endswith(".ddj")}
	path, handle, data = stage1.open_archive(vsro_root, "Media")
	try:
		index = stage1.archive_index(data)
		added, absent = 0, []
		paths = map_media_paths(regions, summary["overview"], native_box)
		for archive_path in dict.fromkeys(paths + sorted(icons)):
			entry = index.get(archive_path)
			if entry is None:
				absent.append(archive_path)
			elif archive_path not in present or archive_path in overrides:
				# The v1.150 overview tiles carry the old page's frame and its
				# sea where these regions now exist (page_tiles), so the later
				# art replaces them, unlinking the variant's hard link first.
				replace_file(safe_target(out / "Media_extracted", entry.path), sro_pk2.payload(data, entry))
				added += 1
	finally:
		data.close()
		handle.close()
	summary.update({"mediaFilesAdded": added, "mediaFilesAbsent": absent, "pageTiles": len(overrides)})
	return summary


# ================
# add_npcs
# ================
def add_npcs(vsro_root, native, out, regions):
	"""
	Write the city's NPCs: characterdata rows converted to the v1.150 layout,
	positions, talk rows, shops on native stores, sizes and texts. Returns a
	summary. The texts are added to the textdataname.txt the earlier steps left.
	"""
	textdata = Path("Media_extracted") / TEXTDATA
	names = ("npcpos.txt", "npcchat.txt", "refshopgroup.txt", "refmappingshopgroup.txt", "refshop.txt", "skilleffect.txt")
	tables = {name: (native / textdata / name).read_bytes() for name in names}
	tables[npcs.CHARACTER_TARGET] = (native / textdata / npcs.CHARACTER_TARGET).read_bytes()
	tables["characters"] = [
		path.read_bytes() for path in sorted((native / textdata).glob("characterdata_*.txt"))
	]
	vsro = {name: read_vsro_file(vsro_root, "Media", TEXTDATA + name) for name in tuple(n for n in names if n != "refshop.txt")}
	vsro["textdata_object.txt"] = read_vsro_file(vsro_root, "Media", TEXTDATA + "textdata_object.txt")
	vsro["characters"] = [
		read_vsro_file(vsro_root, "Media", TEXTDATA + f"characterdata_{start}.txt")
		for start in VSRO_CHARACTER_FILES
	]
	base_text = (out / textdata / "textdataname.txt").read_bytes()
	merged, summary = npcs.merge_npc_tables(tables, vsro, regions, base_text)
	for name, content in merged.items():
		replace_file(out / textdata / name, content)
	return summary


# ================
# replace_file
# ================
def replace_file(target, content):
	"""
	Write content to target without writing through a hard link: the old
	file (possibly a link to the native tree) is unlinked first.
	"""
	target.parent.mkdir(parents=True, exist_ok=True)
	if target.exists():
		target.unlink()
	write_atomically(target, content)


# ================
# read_vsro_file
# ================
def read_vsro_file(vsro_root, archive, name):
	path, handle, data = stage1.open_archive(vsro_root, archive)
	try:
		entry = stage1.archive_index(data).get(name.lower())
		if entry is None:
			raise SystemExit(f"{path} has no {name}")
		return sro_pk2.payload(data, entry)
	finally:
		data.close()
		handle.close()


# ================
# add_missing_files
# ================
def add_missing_files(vsro_root, archive, destination, present, skip_prefix=()):
	"""Extract the VSRO archive's files whose lower-case path is not in present."""
	path, handle, data = stage1.open_archive(vsro_root, archive)
	try:
		added = 0
		for entry in sro_pk2.read_directory(data):
			key = entry.path.replace("\\", "/").lower()
			if key in present or key.startswith(skip_prefix):
				continue
			write_atomically(safe_target(destination, entry.path), sro_pk2.payload(data, entry))
			added += 1
		log(f"{destination.name}: {added} VSRO files added")
		return added
	finally:
		data.close()
		handle.close()


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description="Build the v1.150 + Alexandria game root (port-only, not native).")
	parser.add_argument("--vsro-root", default=os.environ.get(stage1.ROOT_ENV), help="VSRO client folder")
	parser.add_argument("--variant-root", required=True, help="the new game root to create")
	parser.add_argument("--imported-root", type=Path, help="isolated stage-1 import instead of the native extraction's import folder")
	parser.add_argument("--force", action="store_true", help="rebuild even when the stamp says nothing changed")
	args = parser.parse_args()
	if not stage1.flag_enabled():
		raise SystemExit(f"{stage1.FLAG_ENV} is not on. Alexandria is port-only, not native.")
	if not args.vsro_root:
		raise SystemExit(f"Pass --vsro-root or set {stage1.ROOT_ENV}.")
	variant = Path(args.variant_root).resolve()
	if variant == EXTRACTED_ROOT.parent.resolve() or EXTRACTED_ROOT.resolve() in variant.parents:
		raise SystemExit("The variant root must be a different folder from the native game root.")
	vsro_root = Path(args.vsro_root)
	imported = Path(args.imported_root) if args.imported_root else EXTRACTED_ROOT / stage1.OUTPUT_FOLDER
	# Same code, flags and sources as the last successful build of this
	# tree, and the tree untouched since: nothing to do (variant_stamp.py).
	inputs = variant_stamp.read_inputs(EXTRACTED_ROOT, imported, vsro_root, variant)
	stale = ["--force"] if args.force else variant_stamp.stale_parts(variant, inputs, (VARIANT_MANIFEST,) + LINKED_FILES)
	if not stale:
		log(f"unchanged since the last build of {variant}; skipped (pass --force to rebuild)")
		return
	log(f"rebuilding: {', '.join(stale[:8])}{' ...' if len(stale) > 8 else ''}")
	variant_stamp.clear_stamp(variant)
	manifest = build_variant(vsro_root, EXTRACTED_ROOT, variant, args.imported_root)
	# Files the rebuild left byte-identical get their old mtime back, so the
	# asset build's (size, mtime) caches still hit (variant_settle.py).
	settled = variant_settle.settle(variant, build_jobs())
	log(f"settle: {settled['files']} files, {settled['restored']} unchanged kept their time, "
		f"{settled['changed']} changed, {settled['unread']} matched unread")
	# Stamped with what the build read before it started; a source that
	# moved under it leaves no stamp, so the next run rebuilds.
	moved = variant_stamp.differences(inputs, variant_stamp.read_inputs(EXTRACTED_ROOT, imported, vsro_root, variant))
	if moved:
		log(f"not stamped: {', '.join(moved)} changed during the build")
	else:
		variant_stamp.write_stamp(variant, inputs)
	log(f"tile remap {manifest['tileRemap']}, {manifest['mapFilesAdded']} map files, {manifest['dataFilesAdded']} data files")
	log(f"Next: SRO_GAME_ROOT={variant} SRO_GENERATED_ROOT={variant / '.generated'} pnpm assets build full")


if __name__ == "__main__":
	main()
