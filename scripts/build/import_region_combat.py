"""
===========================================================================
import_region_combat.py - recover region combat permissions from reference data

Reads the named primary _RefRegion allocation in a research SQL backup and
writes the ranges to <source-data>/server/world/region-combat.json, which
the bundle build copies into the projection's derived data
(world.RegionCombatFile). It never restores or modifies a database.
===========================================================================
"""

import argparse
import hashlib
import json
import pathlib
import struct
import sys

from loot_sql_pages import Backup

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))
import sro_paths  # noqa: E402

OUTPUT = sro_paths.SOURCE_DATA_ROOT / "server" / "world" / "region-combat.json"
FORMAT = "sro-region-combat-v1"


# ================
# read_regions
# ================
def read_regions(path):
	backup = Backup(path)
	regions = {}
	try:
		for _, at, page in backup.named_rows("_RefRegion"):
			fixed = struct.unpack_from("<H", page, at + 2)[0]
			columns = struct.unpack_from("<H", page, at + fixed)[0]
			if fixed != 65 or columns != 21:
				raise ValueError("Unsupported _RefRegion row layout")
			# wRegionID is the lookup key. X/Z are separate authored columns;
			# dungeon records intentionally leave both at zero.
			region = struct.unpack_from("<H", page, at + 4)[0]
			battlefield = page[at + 8]
			if battlefield not in (0, 1):
				raise ValueError("Invalid battlefield permission")
			if region in regions and regions[region] != battlefield:
				raise ValueError("Conflicting region combat permissions")
			regions[region] = battlefield
	finally:
		backup.close()
	if not regions:
		raise ValueError("Empty region combat catalog")
	return regions


# ================
# region_ranges
# ================
def region_ranges(regions):
	ranges = []
	for region, allowed in sorted(regions.items()):
		if ranges and ranges[-1][1] + 1 == region and ranges[-1][2] == allowed:
			ranges[-1][1] = region
		else:
			ranges.append([region, region, allowed])
	return ranges


# ================
# document
#
# The table's bytes: compact JSON, one trailing newline, so a rerun over the
# same backup reproduces them exactly (--check).
# ================
def document(regions, digest):
	ranges = [[first, last, bool(allowed)] for first, last, allowed in region_ranges(regions)]
	body = {"format": FORMAT, "sourceSha256": digest, "regions": len(regions), "ranges": ranges}
	return (json.dumps(body, separators=(",", ":")) + "\n").encode("utf-8")


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("backup", type=pathlib.Path)
	parser.add_argument("output", type=pathlib.Path, nargs="?", default=OUTPUT)
	parser.add_argument("--check", action="store_true", help="refuse unless output already holds these bytes")
	args = parser.parse_args()
	regions = read_regions(args.backup)
	with args.backup.open("rb") as source:
		digest = hashlib.file_digest(source, "sha256").hexdigest()
	data = document(regions, digest)
	if args.check:
		if not args.output.is_file() or args.output.read_bytes() != data:
			raise SystemExit(f"{args.output} differs from the backup's region combat table")
		print(f"{args.output} matches ({len(regions)} region permissions)")
		return
	args.output.parent.mkdir(parents=True, exist_ok=True)
	args.output.write_bytes(data)
	print(f"Wrote {len(regions)} region permissions to {args.output}")


if __name__ == "__main__":
	main()
