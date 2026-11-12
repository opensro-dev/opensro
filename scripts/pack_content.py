"""
===========================================================================

pack_content.py - pack a later client's new areas into one archive

Port-only, not native (owner ask #3263, route #3254). Reads an
already-extracted later-client export (tables and resources on disk,
nothing executed) and an areas file naming each area's sector keys,
and writes the single pinned archive (content_archive.py) the
SRO_EXPORT_WORLD importers read: for every complete area, its sector
terrain (.t/.m/.o/.o2 under <sectorY>/<sectorX>), its navmesh
(navmesh/nv_<region>.nvm) and its map tiles, plus the shared tables
the importers join (regioninfo.txt, the export's character shard and
dungeon/dungeoninfo.txt).

An area is packed whole or not at all: a sector missing its terrain
mesh, its placements or its navmesh excludes the area, and the report
names every gap - the packer never writes half an area. The character
shard loses the third-party server brands the inventory marks for
deliberate exclusion (rows naming CREDDY or ORIGIN); every dropped row
is counted in the report. The archive is written outside the
repository and never committed; its sha256 is pinned in
content_archive_pins.json.

===========================================================================
"""
import argparse
import json
from pathlib import Path

import alexandria_npcs as text_tables
import content_archive
import import_vsro_alexandria as tables

AREAS_FORMAT = "opensro-content-areas-v1"
SECTOR_EXTENSIONS = (".t", ".m", ".o", ".o2")
REQUIRED_SECTOR_EXTENSIONS = (".m", ".o")
TILE_DIRS = ("map", "minimap")
# The third-party server brands the export's inventory (#3263) marks
# for deliberate exclusion: another server's authored rows are not
# later-client content and never ride the archive.
EXCLUDED_BRANDS = ("CREDDY", "ORIGIN")
CODENAME = 2


# ================
# read_areas
#
# The areas file: {"format": AREAS_FORMAT, "areas": {"<name>":
# {"regions": [[x, y], ...]}}}. Names become entry prefixes, so they
# are plain path segments; keys are sector (x, y) pairs.
# ================
def read_areas(path):
	try:
		document = json.loads(Path(path).read_text(encoding="utf-8"))
	except (OSError, ValueError) as error:
		raise SystemExit(f"{path} is unreadable: {error}")
	if document.get("format") != AREAS_FORMAT or not isinstance(document.get("areas"), dict):
		raise SystemExit(f"{path} is not a {AREAS_FORMAT} areas file")
	areas = {}
	for name, spec in document["areas"].items():
		if not name or name != Path(name).name or name in (".", ".."):
			raise SystemExit(f"{path}: area name {name!r} is not a plain path segment")
		regions = spec.get("regions") if isinstance(spec, dict) else None
		if not regions:
			raise SystemExit(f"{path}: area {name!r} names no regions")
		keys = []
		for region in regions:
			if not isinstance(region, list) or len(region) != 2 or not all(isinstance(v, int) for v in region):
				raise SystemExit(f"{path}: area {name!r} has a malformed region {region!r}")
			keys.append((region[0], region[1]))
		areas[name] = keys
	return areas


# ================
# region_id
# ================
def region_id(sector_x, sector_y):
	"""A sector's region id, the (y << 8) | x the tables name it by."""
	return (sector_y << 8) | sector_x


# ================
# sector_entries
#
# One sector's archive rows in the export: the terrain files that
# exist, and what a complete sector still misses (its mesh, its
# placements, its navmesh). Entry paths keep the export's layout.
# ================
def sector_entries(export_root, sector_x, sector_y):
	present, missing = {}, []
	for extension in SECTOR_EXTENSIONS:
		relative = f"{sector_y}/{sector_x}{extension}"
		if (export_root / relative).is_file():
			present[relative] = (export_root / relative).read_bytes()
		elif extension in REQUIRED_SECTOR_EXTENSIONS:
			missing.append(relative)
	navmesh = f"navmesh/nv_{region_id(sector_x, sector_y):04x}.nvm"
	if (export_root / navmesh).is_file():
		present[navmesh] = (export_root / navmesh).read_bytes()
	else:
		missing.append(navmesh)
	return present, missing


# ================
# tile_entries
#
# A sector's map tiles, wherever the export's map trees keep them
# (their names end <x>x<y>.ddj). Cosmetic: reported, never a gap.
# ================
def tile_entries(export_root, sector_x, sector_y):
	suffix = f"{sector_x}x{sector_y}.ddj"
	found = {}
	for folder in TILE_DIRS:
		base = export_root / folder
		if not base.is_dir():
			continue
		for path in sorted(base.rglob("*")):
			if path.is_file() and path.name.endswith(suffix):
				found[path.relative_to(export_root).as_posix()] = path.read_bytes()
	return found


# ================
# character_shard
#
# The export's one characterdata shard, with the third-party brand
# rows dropped (counted per brand). Exactly one shard must sit at the
# export root; zero or several is an operator error, not a guess.
# ================
def character_shard(export_root):
	shards = sorted(export_root.glob("characterdata_*.txt"))
	if len(shards) != 1:
		raise SystemExit(f"{export_root}: want exactly one characterdata shard, found {[p.name for p in shards]}")
	rows = tables.table_rows(shards[0].read_bytes())
	kept, dropped = [], {}
	for row in rows:
		code = row[CODENAME].upper() if len(row) > CODENAME else ""
		brand = next((mark for mark in EXCLUDED_BRANDS if mark in code), None)
		if brand is None:
			kept.append(row)
		else:
			dropped[brand] = dropped.get(brand, 0) + 1
	raw = b"\xff\xfe" + text_tables.table_text(kept).encode("utf-16-le")
	return shards[0].name, raw, dropped


# ================
# shared_tables
#
# The tables every area importer joins, verbatim but for the filtered
# character shard: regioninfo.txt and dungeon/dungeoninfo.txt.
# ================
def shared_tables(export_root):
	entries = {}
	for relative, entry_name in (("regioninfo.txt", "regioninfo.txt"), ("dungeon/dungeoninfo.txt", "dungeoninfo.txt")):
		source = export_root / relative
		if not source.is_file():
			raise SystemExit(f"{export_root}: the shared table {relative} is absent")
		entries[content_archive.TABLES_PREFIX + entry_name] = source.read_bytes()
	name, raw, dropped = character_shard(export_root)
	entries[content_archive.TABLES_PREFIX + name] = raw
	return entries, dropped


# ================
# pack
#
# The whole pack: tables first, then each complete area whole. An
# incomplete area is excluded with its gaps named; packing no area at
# all is a refusal, not an archive of tables alone.
# ================
def pack(export_root, areas, output):
	export_root = Path(export_root).resolve()
	entries, dropped_rows = shared_tables(export_root)
	packed, excluded = {}, []
	for name in sorted(areas):
		area_entries, missing = {}, []
		for sector_x, sector_y in areas[name]:
			present, gaps = sector_entries(export_root, sector_x, sector_y)
			missing += [f"{sector_x},{sector_y}: {gap}" for gap in gaps]
			for relative, raw in present.items():
				area_entries[relative] = raw
			area_entries.update(tile_entries(export_root, sector_x, sector_y))
		if missing:
			excluded.append({"area": name, "missing": missing})
			continue
		for relative, raw in area_entries.items():
			entries[content_archive.AREAS_PREFIX + name + "/" + relative] = raw
		packed[name] = {"regions": [list(key) for key in areas[name]], "files": len(area_entries)}
	if not packed:
		raise SystemExit("no area is complete in the export; nothing was packed (see the report)")
	manifest = {
		"format": content_archive.ARCHIVE_FORMAT, "portOnly": True,
		"areas": packed, "tables": sorted(name[len(content_archive.TABLES_PREFIX):] for name in entries if name.startswith(content_archive.TABLES_PREFIX)),
	}
	entries[content_archive.MANIFEST_ENTRY] = (json.dumps(manifest, indent=2) + "\n").encode("utf-8")
	output = Path(output)
	output.parent.mkdir(parents=True, exist_ok=True)
	content_archive.write_archive(output, entries)
	return {
		"format": "opensro-content-pack-v1", "portOnly": True,
		"areas": packed, "excludedAreas": excluded, "excludedRows": dropped_rows,
		"files": len(entries), "bytes": output.stat().st_size,
		"sha256": content_archive.file_digest(output),
	}


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--export-root", type=Path, required=True, help="the extracted later-client export (data only, nothing in it is run)")
	parser.add_argument("--areas", type=Path, required=True, help="the areas file (opensro-content-areas-v1) naming each area's sector keys")
	parser.add_argument("--output", type=Path, required=True, help="the archive to write, outside the repository")
	parser.add_argument("--report", type=Path, help="also write the pack report as JSON here")
	args = parser.parse_args()
	report = pack(args.export_root, read_areas(args.areas), args.output)
	print(f"Areas packed: {', '.join(sorted(report['areas']))}")
	for entry in report["excludedAreas"]:
		print(f"  excluded area {entry['area']}: {len(entry['missing'])} missing file(s), first: {entry['missing'][0]}")
	for brand, count in sorted(report["excludedRows"].items()):
		print(f"  excluded {count} character row(s) naming {brand}")
	print(f"Archive: {args.output} ({report['files']} entries, {report['bytes']} bytes)")
	print(f"sha256: {report['sha256']}")
	print(f"Pin it as scripts/{content_archive.PIN_FILE}: {{\"format\": \"{content_archive.PIN_FORMAT}\", \"sha256\": \"{report['sha256']}\"}}")
	if args.report:
		args.report.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
		print(f"Report: {args.report}")


if __name__ == "__main__":
	main()
