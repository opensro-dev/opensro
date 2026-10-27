"""
===========================================================================

import_vsro_alexandria.py - stage 1 of Alexandria: pull its map out of a VSRO client

PORT-ONLY, NOT NATIVE. The v1.150 client has no Alexandria; it first ships in
the later (cap 110) VSRO client. The scope rule keeps v1.150 as the native
default, so this tool is off unless SRO_ALEXANDRIA is "1", "on" or "true",
and it never writes into extracted/Map_extracted or any other v1.150 tree.

The VSRO archives are hash-pinned (vsro_archive_pins.json, #1745): the
pin names the sha256 of the one later client the expansion is built
against, and this stage refuses any other bytes before reading them.
The archives themselves are the operator's own and are never committed.

What it does, from the operator's own VSRO client (SRO_VSRO_ROOT or
--vsro-root, holding Map.pk2, Media.pk2 and Data.pk2):

	1. Reads the city's regions from regioncode.txt (code RN_SD_EGYPT01, the
	   36 regions whose zone name is "Alexandria"), not from a hand list, and
	   adds every region the VSRO client enables beyond v1.150 that touches
	   them (the fields around the city: 110 regions in all).
	2. Copies those regions' .m/.o/.o2/.t map files, the navmesh .nvm, and
	   the ground tile textures their terrain uses.
	3. Copies the VSRO tile2d.ifo and object.ifo as evidence and reports how
	   they relate to the native v1.150 catalogs (ids are stable; v1.150 holds
	   "xxx" placeholders where the VSRO has the real row).
	4. Writes the Alexandria rows of the text tables (regions, zone names,
	   gates) as JSON.

Everything lands in extracted/Alexandria_extracted/ beside the v1.150
extraction, with a manifest recording the archive hashes. Stage 2 (not here)
merges these into the world build behind the client's Experimental row and
the server flag; the object and building closure (.o2 placements to .bsr
models) is that stage's job.

	SRO_ALEXANDRIA=1 py -3 scripts/import_vsro_alexandria.py --vsro-root <folder>

===========================================================================
"""
import argparse
import hashlib
import json
import mmap
import os
import re
import struct
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import sro_pk2  # noqa: E402
from prepare_client_resources import safe_target, write_atomically  # noqa: E402
from sro_paths import EXTRACTED_ROOT  # noqa: E402

FLAG_ENV = "SRO_ALEXANDRIA"
ROOT_ENV = "SRO_VSRO_ROOT"
FLAG_ON = ("1", "on", "true")
# The archives the Alexandria chain opens from a VSRO root (#1745):
# stage 1 reads Map, Media and Data; stage 2 adds Music and Particles;
# the cap-110 importers read Media, Data and Particles between them.
# Every one is pinned in vsro_archive_pins.json (require_vsro_pin).
ARCHIVE_NAMES = ("Map", "Media", "Data", "Music", "Particles")
STAGE1_ARCHIVES = ("Map", "Media", "Data")
PIN_FORMAT = "opensro-vsro-archive-pins-v1"
PIN_FILE = Path(__file__).with_name("vsro_archive_pins.json")
MANIFEST_FORMAT = "opensro-alexandria-import-v1"
MANIFEST_NAME = ".opensro-alexandria.json"
OUTPUT_FOLDER = "Alexandria_extracted"

CITY_REGION_CODE = "RN_SD_EGYPT01"
# Zone-name codes copied with the city: the region code itself and its
# description row (RN_SD_EGYPT01_01, _02).
CITY_NAME_PREFIX = "RN_SD_EGYPT01"
# Gate codes of the city's own ferry and sand-desert gates (teleportdata).
CITY_GATE_PREFIX = "GATE_SD"
CITY_GATE_EXTRA = ("GATE_NPC_SD_M_AREA_FERRY",)

TEXTDATA = "server_dep/silkroad/textdata/"
MAP_EXTENSIONS = (".m", ".o", ".o2", ".t")
CATALOGS = ("tile2d.ifo", "object.ifo")

# JMXVMAPM1000 terrain, as scripts/build/world/constants.mjs: a 12-byte
# signature, then 6x6 blocks of MAPM_BLOCK_BYTES.
MFO_SIGNATURE = b"JMXVMFO 1000"
MFO_SIGNATURE_BYTES = 12
MFO_HEADER_BYTES = 24
MFO_WIDTH, MFO_HEIGHT = 0x100, 0x80
MAPM_SIGNATURE = b"JMXVMAPM1000"
MAPM_SIGNATURE_BYTES = 12
MAP_BLOCKS = 36
MAPM_BLOCK_BYTES = 2575
MAPM_VERTICES = 17 * 17
MAPM_VERTEX_BYTES = 7
# Block head: flag u32, environment u16; a vertex is height f32, texture u16,
# brightness u8.
MAPM_BLOCK_HEAD_BYTES = 6
MAPM_VERTEX_TEXTURE_OFFSET = 4
MAPM_TEXTURE_ID_MASK = 0x03FF

MFO_PATH = "navmesh/mapinfo.mfo"
# The native v1.150 copy of MFO_PATH, under an extracted root.
NATIVE_MAPINFO = Path("Data_extracted") / "navmesh" / "mapinfo.mfo"
PLACEHOLDER_TILE = re.compile(r"^x+\.ddj$", re.IGNORECASE)
TILE_ROW = re.compile(r'^(\d+)\s+(0x[0-9a-fA-F]+|\d+)\s+"([^"]*)"\s+"([^"]*)"')
OBJECT_ROW = re.compile(r'^(\d+)\s+(0x[0-9a-fA-F]+|\d+)\s+"([^"]*)"')
HASH_CHUNK = 1 << 20


def log(message):
	print(message, flush=True)


#============================================================================
# Flag and text
#============================================================================


def flag_enabled(env=os.environ):
	"""True only when SRO_ALEXANDRIA names an on value; unset is native."""
	return env.get(FLAG_ENV, "").strip().lower() in FLAG_ON


def decode_table(raw):
	"""A textdata file: UTF-16 with a BOM in the VSRO client, else CP949."""
	if raw[:2] in (b"\xff\xfe", b"\xfe\xff"):
		return raw.decode("utf-16", errors="replace")
	return raw.decode("cp949", errors="replace")


def table_rows(raw):
	"""Tab-split rows of a textdata file, comment and blank lines dropped."""
	rows = []
	for line in decode_table(raw).splitlines():
		if not line.strip() or line.startswith("//"):
			continue
		rows.append(line.split("\t"))
	return rows


#============================================================================
# Regions
#============================================================================


def region_xy(region_id):
	"""(sectorX, sectorY) of a region id: id = (sectorY << 8) | sectorX."""
	return region_id & 0xFF, (region_id >> 8) & 0xFF


def city_regions(regioncode_raw, code=CITY_REGION_CODE):
	"""
	The (sectorX, sectorY) pairs regioncode.txt assigns to code, sorted.
	Columns: service, region id, code name, comment.
	"""
	found = set()
	for row in table_rows(regioncode_raw):
		if len(row) < 3 or row[2].strip() != code:
			continue
		found.add(region_xy(int(row[1])))
	return sorted(found, key=lambda xy: (xy[1], xy[0]))


def mfo_regions(mapinfo):
	"""The (sectorX, sectorY) pairs an availability bitmap (navmesh/mapinfo.mfo) enables."""
	if mapinfo[:len(MFO_SIGNATURE)] != MFO_SIGNATURE:
		raise ValueError("not a JMXVMFO availability file")
	width, height = struct.unpack_from("<HH", mapinfo, MFO_SIGNATURE_BYTES)
	if (width, height) != (MFO_WIDTH, MFO_HEIGHT):
		raise ValueError(f"unexpected availability size {width}x{height}")
	found = set()
	for region_id in range(width * height):
		if mapinfo[MFO_HEADER_BYTES + (region_id >> 3)] & (0x80 >> (region_id & 7)):
			found.add(region_xy(region_id))
	return found


# ================
# connected_regions
# ================
def connected_regions(seeds, candidates, field_seeds=()):
	"""
	seeds plus every candidate region reachable from them over the eight
	neighbouring sectors, sorted by row then column. City and optional field
	seeds may start separate components; candidates are VSRO-only terrain.
	"""
	found = set(seeds) | set(field_seeds)
	pending = list(found)
	while pending:
		x, y = pending.pop()
		for dx in (-1, 0, 1):
			for dy in (-1, 0, 1):
				near = (x + dx, y + dy)
				if near in candidates and near not in found:
					found.add(near)
					pending.append(near)
	return sorted(found, key=lambda xy: (xy[1], xy[0]))


def region_files(sector_x, sector_y):
	"""The Map.pk2 paths of one region, lower case as the archive is read."""
	return [f"{sector_y}/{sector_x}{extension}" for extension in MAP_EXTENSIONS]


def navmesh_path(sector_x, sector_y):
	"""Data.pk2 path of a region's navmesh (nv_<region id as 4 hex digits>)."""
	return f"navmesh/nv_{(sector_y << 8) | sector_x:04x}.nvm"


def terrain_tile_ids(terrain):
	"""The set of ground tile ids a .m terrain file paints."""
	if terrain[:MAPM_SIGNATURE_BYTES] != MAPM_SIGNATURE:
		raise ValueError("not a JMXVMAPM1000 terrain file")
	expected = MAPM_SIGNATURE_BYTES + MAP_BLOCKS * MAPM_BLOCK_BYTES
	if len(terrain) != expected:
		raise ValueError(f"terrain is {len(terrain)} bytes, expected {expected}")
	ids = set()
	for block in range(MAP_BLOCKS):
		first = MAPM_SIGNATURE_BYTES + block * MAPM_BLOCK_BYTES + MAPM_BLOCK_HEAD_BYTES
		for vertex in range(MAPM_VERTICES):
			at = first + vertex * MAPM_VERTEX_BYTES + MAPM_VERTEX_TEXTURE_OFFSET
			ids.add(struct.unpack_from("<H", terrain, at)[0] & MAPM_TEXTURE_ID_MASK)
	return ids


#============================================================================
# Catalogs
#============================================================================


def tile_catalog(raw):
	"""tile2d.ifo as {id: (category, file name)}."""
	rows = {}
	for line in decode_table(raw).splitlines()[2:]:
		match = TILE_ROW.match(line.strip())
		if match:
			rows[int(match.group(1))] = (match.group(3), match.group(4))
	return rows


def object_catalog(raw):
	"""object.ifo as {id: (flags, path)}."""
	rows = {}
	for line in decode_table(raw).splitlines()[2:]:
		match = OBJECT_ROW.match(line.strip())
		if match:
			rows[int(match.group(1))] = (int(match.group(2), 0), match.group(3))
	return rows


def compare_catalogs(native, vsro, is_placeholder):
	"""
	How a VSRO catalog relates to the native one. added: ids the native
	catalog lacks or holds as a placeholder; conflicts: ids both define
	differently (the native row stays in a merge); native_only: ids the VSRO
	lacks.
	"""
	added, conflicts = [], []
	for key, row in vsro.items():
		if key not in native or is_placeholder(native[key]):
			added.append(key)
		elif native[key] != row:
			conflicts.append(key)
	return {
		"added": sorted(added),
		"conflicts": sorted(conflicts),
		"native_only": sorted(set(native) - set(vsro))
	}


def tile_placeholder(row):
	return bool(PLACEHOLDER_TILE.match(row[1]))


def object_placeholder(row):
	return row[1].strip().lower().startswith("xxx")


#============================================================================
# Archives
#============================================================================


def archive_index(data):
	"""{lower-case path with forward slashes: entry} of one PK2."""
	return {entry.path.replace("\\", "/").lower(): entry for entry in sro_pk2.read_directory(data)}


def open_archive(root, name):
	path = Path(root) / f"{name}.pk2"
	if not path.is_file():
		raise SystemExit(f"{path} not found; --vsro-root must be a VSRO client folder")
	handle = open(path, "rb")
	return path, handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ)


def sha256_of(data):
	hasher = hashlib.sha256()
	for start in range(0, len(data), HASH_CHUNK):
		hasher.update(data[start:start + HASH_CHUNK])
	return hasher.hexdigest()


#============================================================================
# The VSRO archive pin (#1745)
#
# The expansion is built from the operator's own later client, checked
# against vsro_archive_pins.json and never committed: a missing or
# changed archive fails the build. The gate stands at every entry that
# takes a VSRO root (this stage, build_alexandria_variant.build_variant
# and the cap-110 importers' own mains); the add_* steps run under one
# of those gates. A process verifies each archive once.
#============================================================================

_verified_archives = set()


def archive_pins():
	"""The pinned VSRO archive hashes (vsro_archive_pins.json beside this tool)."""
	try:
		pin = json.loads(PIN_FILE.read_text(encoding="utf-8"))
	except (OSError, ValueError) as error:
		raise SystemExit(f"{PIN_FILE} is unreadable: {error}")
	if pin.get("format") != PIN_FORMAT or not isinstance(pin.get("archives"), dict):
		raise SystemExit(f"{PIN_FILE} is not a {PIN_FORMAT} pin file")
	return pin["archives"]


def verify_vsro_archive(root, name, pins=None):
	"""
	One VSRO archive against its pin: it must exist under root and hash
	to the pinned sha256, or the build stops before anything reads it.
	pins is injectable for tests.
	"""
	path = Path(root) / f"{name}.pk2"
	key = str(path.resolve())
	if key in _verified_archives:
		return
	expected = (archive_pins() if pins is None else pins).get(name)
	if expected is None:
		raise SystemExit(f"{PIN_FILE.name} pins no {name}.pk2; pin the archive before reading it")
	if not path.is_file():
		raise SystemExit(f"{path} not found; --vsro-root must be the pinned VSRO client folder")
	hasher = hashlib.sha256()
	with open(path, "rb") as handle:
		for chunk in iter(lambda: handle.read(HASH_CHUNK), b""):
			hasher.update(chunk)
	actual = hasher.hexdigest()
	if actual != expected:
		raise SystemExit(
			f"{path} does not match the pinned VSRO archive: sha256 is {actual}, pinned is {expected}. "
			"Alexandria is built from one pinned VSRO client (#1745); its archives are never committed."
		)
	_verified_archives.add(key)


def require_vsro_pin(root, names, pins=None):
	"""Every archive in names against its pin, before any of them is read."""
	for name in names:
		verify_vsro_archive(root, name, pins)


def table_text(media, index, name):
	entry = index.get(TEXTDATA + name)
	if entry is None:
		raise SystemExit(f"Media.pk2 has no {TEXTDATA}{name}")
	return sro_pk2.payload(media, entry)


def table_json(raw, keep, key_column):
	"""The rows of a table whose key column passes keep, as JSON-ready lists."""
	return [row for row in table_rows(raw) if len(row) > key_column and keep(row[key_column].strip())]


#============================================================================
# Import
#============================================================================


def vsro_only_regions(data, index, native_path):
	"""The regions the VSRO client enables and the native v1.150 mapinfo.mfo does not."""
	if not native_path.is_file():
		raise SystemExit(f"{native_path} is missing; run pnpm assets prepare for the v1.150 client first")
	entry = index.get(MFO_PATH)
	if entry is None:
		raise SystemExit(f"Data.pk2 has no {MFO_PATH}")
	return mfo_regions(sro_pk2.payload(data, entry)) - mfo_regions(native_path.read_bytes())


# ================
# import_alexandria
#
# native_mapinfo is the v1.150 mapinfo.mfo; by default the one under
# extracted. The variant build passes the native tree's file, because its
# isolated stage 1 root holds no native extraction.
# ================
def import_alexandria(vsro_root, extracted, native_mapinfo=None):
	"""Run the import; returns the manifest dict it also writes."""
	if native_mapinfo is None:
		native_mapinfo = extracted / NATIVE_MAPINFO
	require_vsro_pin(vsro_root, STAGE1_ARCHIVES)
	output = extracted / OUTPUT_FOLDER
	map_path, map_handle, map_data = open_archive(vsro_root, "Map")
	media_path, media_handle, media_data = open_archive(vsro_root, "Media")
	data_path, data_handle, data_data = open_archive(vsro_root, "Data")
	try:
		map_index, media_index, data_index = archive_index(map_data), archive_index(media_data), archive_index(data_data)

		city = city_regions(table_text(media_data, media_index, "regioncode.txt"))
		if not city:
			raise SystemExit(f"regioncode.txt has no {CITY_REGION_CODE} region; is this a VSRO client with Alexandria?")
		# Port-only, not native: field regions are seeds for complete source
		# terrain components, not an exhaustive list of regions worth importing.
		# Empty connecting terrain and mountain sectors have no monster nests.
		fields = set()
		if os.environ.get("SRO_CAP110_MONSTERS", "").lower() in FLAG_ON:
			from cap110_monsters import outdoor_regions, SOURCE_ENV, tomb_enabled
			source = os.environ.get(SOURCE_ENV, "")
			if not source:
				raise SystemExit(f"SRO_CAP110_MONSTERS is on but {SOURCE_ENV} names no monster database export")
			# The Pharaoh Tomb regions seed their own components the same way.
			fields = outdoor_regions(source, tomb_enabled())
		regions = connected_regions(city, vsro_only_regions(data_data, data_index, native_mapinfo), fields)
		log(f"{len(city)} city regions, {len(regions)} regions with the fields around it")

		tile_ids, copied, missing = set(), [], []
		for sector_x, sector_y in regions:
			for archive_path in region_files(sector_x, sector_y):
				entry = map_index.get(archive_path)
				if entry is None:
					missing.append(archive_path)
					continue
				stored = sro_pk2.payload(map_data, entry)
				write_atomically(safe_target(output / "Map", archive_path), stored)
				copied.append("Map/" + archive_path)
				if archive_path.endswith(".m"):
					tile_ids |= terrain_tile_ids(stored)
			nav = navmesh_path(sector_x, sector_y)
			entry = data_index.get(nav)
			if entry is None:
				missing.append(nav)
			else:
				write_atomically(safe_target(output / "Data", nav), sro_pk2.payload(data_data, entry))
				copied.append("Data/" + nav)

		catalog_raw = {name: sro_pk2.payload(map_data, map_index[name]) for name in CATALOGS}
		for name, raw in catalog_raw.items():
			write_atomically(safe_target(output / "Map", name), raw)
		vsro_tiles = tile_catalog(catalog_raw["tile2d.ifo"])
		unknown = sorted(tile_ids - set(vsro_tiles))
		if unknown:
			raise SystemExit(f"Alexandria terrain paints tile ids missing from tile2d.ifo: {unknown}")
		for tile_id in sorted(tile_ids):
			name = vsro_tiles[tile_id][1].replace("\\", "/")
			entry = map_index.get("tile2d/" + name.lower())
			if entry is None:
				missing.append("tile2d/" + name)
				continue
			write_atomically(safe_target(output / "Map" / "tile2d", name), sro_pk2.payload(map_data, entry))
			copied.append("Map/tile2d/" + name)

		catalogs = catalog_report(extracted, catalog_raw, tile_ids)
		tables = write_tables(output, media_data, media_index, regions)
		manifest = {
			"format": MANIFEST_FORMAT,
			"portOnly": True,
			"flag": FLAG_ENV,
			"archives": {
				"Map": {"sha256": sha256_of(map_data)},
				"Media": {"sha256": sha256_of(media_data)},
				"Data": {"sha256": sha256_of(data_data)}
			},
			"regions": [{"sectorX": x, "sectorY": y} for x, y in regions],
			"tileIds": sorted(tile_ids),
			"files": sorted(copied),
			"missing": sorted(missing),
			"catalogs": catalogs,
			"tables": tables
		}
		write_atomically(output / MANIFEST_NAME, (json.dumps(manifest, indent=2, ensure_ascii=False) + "\n").encode("utf-8"))
		return manifest
	finally:
		for handle, data in ((map_handle, map_data), (media_handle, media_data), (data_handle, data_data)):
			data.close()
			handle.close()


def catalog_report(extracted, catalog_raw, tile_ids):
	"""
	Compare the VSRO catalogs with the native ones, when those are extracted.
	tile2d.ifo also reports usedConflicts: tile ids Alexandria paints that the
	native catalog defines as a different texture, which the world merge must
	remap rather than share.
	"""
	native_root = extracted / "Map_extracted"
	report = {}
	for name, parse, placeholder in (
		("tile2d.ifo", tile_catalog, tile_placeholder),
		("object.ifo", object_catalog, object_placeholder)
	):
		native_path = native_root / name
		if not native_path.is_file():
			report[name] = {"native": "not extracted; run pnpm assets prepare to compare"}
			continue
		report[name] = compare_catalogs(parse(native_path.read_bytes()), parse(catalog_raw[name]), placeholder)
	if "conflicts" in report["tile2d.ifo"]:
		report["tile2d.ifo"]["usedConflicts"] = sorted(set(report["tile2d.ifo"]["conflicts"]) & tile_ids)
	return report


def write_tables(output, media, index, regions):
	"""The city's rows of the region, zone-name and gate tables, as JSON."""
	region_ids = {(y << 8) | x for x, y in regions}
	tables = {
		"regioncode.txt": table_json(
			table_text(media, index, "regioncode.txt"), lambda value: value == CITY_REGION_CODE, 2
		),
		"textzonename.txt": table_json(
			table_text(media, index, "textzonename.txt"),
			lambda value: value.startswith(CITY_NAME_PREFIX),
			1
		),
		"teleportdata.txt": [
			row
			for row in table_rows(table_text(media, index, "teleportdata.txt"))
			if len(row) > 5 and (row[2].startswith(CITY_GATE_PREFIX) or row[2] in CITY_GATE_EXTRA)
			and row[5].strip().lstrip("-").isdigit() and int(row[5]) in region_ids
		]
	}
	write_atomically(output / "tables.json", (json.dumps(tables, indent=1, ensure_ascii=False) + "\n").encode("utf-8"))
	return {name: len(rows) for name, rows in tables.items()}


#============================================================================


def main():
	parser = argparse.ArgumentParser(description="Import Alexandria's map from a VSRO client (port-only, not native).")
	parser.add_argument("--vsro-root", default=os.environ.get(ROOT_ENV), help=f"VSRO client folder (else {ROOT_ENV})")
	parser.add_argument("--native-mapinfo", type=Path,
		help="the native v1.150 mapinfo.mfo (default: the one under this game root's extraction)")
	args = parser.parse_args()
	if not flag_enabled():
		raise SystemExit(f"{FLAG_ENV} is not on. Alexandria is port-only, not native; set {FLAG_ENV}=1 to import it.")
	if not args.vsro_root:
		raise SystemExit(f"Pass --vsro-root or set {ROOT_ENV} to the VSRO client folder.")
	# parents: the variant build's isolated stage 1 game root starts empty.
	EXTRACTED_ROOT.mkdir(parents=True, exist_ok=True)
	manifest = import_alexandria(Path(args.vsro_root), EXTRACTED_ROOT, args.native_mapinfo)
	log(f"{len(manifest['files'])} files copied, {len(manifest['missing'])} absent from the archives")
	log(f"extracted/{OUTPUT_FOLDER}/{MANIFEST_NAME} written")


if __name__ == "__main__":
	main()
