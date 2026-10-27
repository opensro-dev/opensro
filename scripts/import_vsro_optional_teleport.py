"""
===========================================================================

import_vsro_optional_teleport.py - pull the later client's reverse return map table

PORT-ONLY, NOT NATIVE. v1.150 ships no optional-teleport table: its reverse
return scroll offers only the last recall point and the last death point.
The later VSRO client ships server_dep/silkroad/textdata/refoptionalteleport.txt,
the table its reverse return scroll consults for its third choice, "move to
a location on the map" (choice 7 + u32 id).
The server serves it only while SRO_REVERSE_RETURN_MAP is on, and falls
back to its inferred table when this import has not run.

From the operator's own VSRO client (SRO_VSRO_ROOT or --vsro-root, holding
Media.pk2) this reads the table and the English zone names its rows cite
(textdata_object.txt), refuses either file unless its sha256 matches the
pinned VSRO release, and writes every row, unfiltered, as JSON into
extracted/VsroOptionalTeleport_extracted/ beside the v1.150 extraction.
The server bundle build projects the file when it is present; a game root
without it builds the native bundle unchanged. The server, not this tool,
decides which rows it serves (outdoor map points of the loaded world).

Column layout (19 columns, one destination per row): Service, ID,
ObjName128, ZoneName128, RegionID, PosX, PosY, PosZ, WorldID,
RegionIDGroup, MapPoint, LevelMin, LevelMax, then three Param/Desc pairs.

	py -3 scripts/import_vsro_optional_teleport.py --vsro-root <folder>

===========================================================================
"""
import argparse
import hashlib
import json
import mmap
import os
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import import_vsro_alexandria as stage1  # noqa: E402
import sro_pk2  # noqa: E402
from prepare_client_resources import safe_target, write_atomically  # noqa: E402
from sro_paths import EXTRACTED_ROOT  # noqa: E402

ROOT_ENV = "SRO_VSRO_ROOT"
OUTPUT_FORMAT = "opensro-vsro-optional-teleport-v1"
OUTPUT_FOLDER = "VsroOptionalTeleport_extracted"
OUTPUT_NAME = "vsro-optional-teleport.json"
TABLE_PATH = "server_dep/silkroad/textdata/refoptionalteleport.txt"
NAMES_PATH = "server_dep/silkroad/textdata/textdata_object.txt"
# The VSRO Media.pk2 payloads this importer was checked against.
PINNED = {
	TABLE_PATH: "78191256af3c538a9ac5d53d8f732dc70c77d0490bb3bd01c37b96a32994cae8",
	NAMES_PATH: "a4aa0280b466a4676daa8b13e14281f9302b4f00b1848abd1dd6ed2f5b4ed4b9"
}
TABLE_COLUMNS = 19
# textdata_object.txt: Service, CodeName128, then the languages; English is
# column 8.
NAMES_CODE_COLUMN = 1
NAMES_ENGLISH_COLUMN = 8
HASH_CHUNK_BYTES = 1 << 20
MAX_NAME_CHARS = 64


"""
================
decode_table

A textdata payload as rows of cells: UTF-16LE with a BOM (the VSRO files)
or UTF-8, skipping blank and // comment lines.
================
"""
def decode_table(raw):
	if raw.startswith(b"\xff\xfe"):
		text = raw[2:].decode("utf-16-le")
	else:
		text = raw.decode("utf-8-sig")
	rows = []
	for line in text.split("\n"):
		line = line.rstrip("\r")
		if not line.strip() or line.startswith("//"):
			continue
		rows.append(line.split("\t"))
	return rows


"""
================
english_names

ZoneName128 code -> English name, from textdata_object.txt.
================
"""
def english_names(raw):
	names = {}
	for row in decode_table(raw):
		if len(row) > NAMES_ENGLISH_COLUMN and row[0] == "1":
			names[row[NAMES_CODE_COLUMN]] = row[NAMES_ENGLISH_COLUMN].strip()
	return names


"""
================
parse_optional_teleport

Every row of the table, typed. A malformed row refuses the whole table
rather than publishing part of it.
================
"""
def parse_optional_teleport(table_raw, names_raw):
	names = english_names(names_raw)
	rows, seen = [], set()
	for index, row in enumerate(decode_table(table_raw)):
		if len(row) != TABLE_COLUMNS:
			raise ValueError(f"refoptionalteleport row {index + 1}: {len(row)} columns, want {TABLE_COLUMNS}")
		try:
			parsed = {
				"service": int(row[0]),
				"id": int(row[1]),
				"zone": row[3],
				"name": names.get(row[3], "")[:MAX_NAME_CHARS],
				"regionId": int(row[4]),
				"x": float(row[5]),
				"y": float(row[6]),
				"z": float(row[7]),
				"worldId": int(row[8]),
				"regionGroup": int(row[9]),
				"mapPoint": int(row[10]),
				"levelMin": int(row[11]),
				"levelMax": int(row[12])
			}
		except ValueError as error:
			raise ValueError(f"refoptionalteleport row {index + 1}: {error}") from None
		if parsed["id"] < 1 or parsed["id"] in seen:
			raise ValueError(f"refoptionalteleport row {index + 1}: id {parsed['id']} is not a new positive id")
		seen.add(parsed["id"])
		rows.append(parsed)
	if not rows:
		raise ValueError("refoptionalteleport holds no rows")
	return sorted(rows, key=lambda row: row["id"])


"""
================
check_pins

Refuse any payload whose sha256 differs from the pinned VSRO release.
================
"""
def check_pins(payloads, pins=PINNED):
	for name, expected in pins.items():
		if name not in payloads:
			raise SystemExit(f"Media.pk2 lacks {name}; is it a VSRO Media.pk2?")
		actual = hashlib.sha256(payloads[name]).hexdigest()
		if actual != expected:
			raise SystemExit(f"{name} sha256 {actual} is not the pinned VSRO release ({expected})")


"""
================
read_payloads

The pinned files out of a Media.pk2, by folded path.
================
"""
def read_payloads(media, wanted):
	found = {}
	with open(media, "rb") as source:
		data = mmap.mmap(source.fileno(), 0, access=mmap.ACCESS_READ)
		try:
			for entry in sro_pk2.read_directory(data):
				key = sro_pk2.fold_ascii(entry.path).replace("\\", "/")
				if key in wanted:
					found[key] = bytes(sro_pk2.payload(data, entry))
		finally:
			data.close()
	return found


"""
================
archive_digest

The archive's sha256, read in chunks so a 700 MB Media.pk2 is never held.
================
"""
def archive_digest(path):
	digest = hashlib.sha256()
	with open(path, "rb") as source:
		for chunk in iter(lambda: source.read(HASH_CHUNK_BYTES), b""):
			digest.update(chunk)
	return digest.hexdigest()


"""
================
import_optional_teleport
================
"""
def import_optional_teleport(media, output):
	payloads = read_payloads(media, set(PINNED))
	check_pins(payloads)
	rows = parse_optional_teleport(payloads[TABLE_PATH], payloads[NAMES_PATH])
	document = {
		"format": OUTPUT_FORMAT,
		"source": {
			"archive": "Media.pk2",
			"sha256": archive_digest(media),
			"files": dict(sorted(PINNED.items()))
		},
		"rows": rows
	}
	write_atomically(safe_target(output, OUTPUT_NAME), (json.dumps(document, indent=2) + "\n").encode())
	return rows


"""
================
main
================
"""
def main():
	parser = argparse.ArgumentParser(description=__doc__.split("\n")[3])
	parser.add_argument("--vsro-root", default=os.environ.get(ROOT_ENV), help=f"VSRO client folder (else {ROOT_ENV})")
	args = parser.parse_args()
	if not args.vsro_root:
		raise SystemExit(f"Pass --vsro-root or set {ROOT_ENV} to the VSRO client folder.")
	media = Path(args.vsro_root) / "Media.pk2"
	if not media.is_file():
		raise SystemExit(f"{media} not found; --vsro-root must be a VSRO client folder")
	# The payload pins below were taken from one VSRO client; the archive
	# itself must be that client before any payload is read (#1745).
	stage1.verify_vsro_archive(Path(args.vsro_root), "Media")
	output = EXTRACTED_ROOT / OUTPUT_FOLDER
	rows = import_optional_teleport(media, output)
	print(f"[vsro-optional-teleport] wrote {len(rows)} row(s) to {output / OUTPUT_NAME}")


if __name__ == "__main__":
	main()
