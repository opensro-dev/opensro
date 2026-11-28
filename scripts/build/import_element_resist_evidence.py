"""
===========================================================================

import_element_resist_evidence.py - snapshot the v1.188 element resists the curve misses

A characterdata row the later clients add takes the native level curve for
its six element resists (alexandria_npcs.level_resist, #4802): the later
clients ship no resist column. The v1.188 server database holds the real
values, and for 1,451 of its 1,644 rows that v1.150 lacks they are the
curve. The other 193 (the temple and event bosses, Roc, the Tomb general,
the trade transports) carry their own values, higher or lower, in 19
distinct shapes, and nothing in the client row predicts them (#4803).

This tool writes scripts/later_element_resists.json (beside
alexandria_npcs.py, which reads it): those rows, keyed by codename, with
the six values in the v1.150 column order and the SHA-256 of each input.
The build reads only that snapshot, never the database export or the
media.

	python -B scripts/build/import_element_resist_evidence.py \
		--vsro-db <SRO_VT_SHARD/data> --media <v1.150 textdata>

===========================================================================
"""

import argparse
import csv
import hashlib
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import alexandria_npcs as npcs  # noqa: E402
import import_vsro_alexandria as stage1  # noqa: E402

OUTPUT = npcs.CARRIED_RESISTS
FORMAT = npcs.CARRIED_RESISTS_FORMAT
CHAR_TABLE = "_RefObjChar.tsv"
COMMON_TABLE = "_RefObjCommon.tsv"
# The six ElementResist columns, in the order v1.150's characterdata
# holds them (alexandria_npcs.RESISTANCE_AT).
RESIST_FIELDS = ("ResistFrozen", "ResistFrostbite", "ResistBurn", "ResistEShock", "ResistPoison", "ResistZombie")
CHARACTER_TYPE = "1"
# The export's widest field (a description) passes csv's default limit.
FIELD_LIMIT = 1 << 30


# ================
# read_export
#
# One table of the database export: its header map and its rows.
# ================
def read_export(path):
	with path.open(encoding="utf-8", errors="replace", newline="") as f:
		reader = csv.reader(f, delimiter="\t")
		header = next(reader)
		return {name: i for i, name in enumerate(header)}, [row for row in reader if row]


# ================
# native_codenames
#
# Every codename v1.150's own characterdata holds: those rows keep their
# native values and are never converted.
# ================
def native_codenames(media):
	codes = set()
	for path in sorted(media.glob("characterdata_*.txt")):
		for row in stage1.table_rows(path.read_bytes()):
			if len(row) > npcs.CODENAME_COLUMN:
				codes.add(row[npcs.CODENAME_COLUMN])
	return codes


# ================
# off_curve_rows
#
# The later-only character rows whose six resists are not what
# element_resist gives them: codename -> the six values.
# ================
def off_curve_rows(chars, common, native):
	char_at, char_rows = chars
	common_at, common_rows = common
	by_id = {row[char_at["ID"]]: row for row in char_rows}
	rows = {}
	for obj in common_rows:
		if len(obj) <= common_at["Link"] or obj[common_at["TypeID1"]] != CHARACTER_TYPE:
			continue
		code = obj[common_at["CodeName128"]]
		char = by_id.get(obj[common_at["Link"]])
		if code in native or char is None:
			continue
		values = [int(char[char_at[field]]) for field in RESIST_FIELDS]
		if values != [curve_resist(obj, char, common_at, char_at)] * len(RESIST_FIELDS):
			rows[code] = values
	return dict(sorted(rows.items()))


# ================
# curve_resist
#
# What element_resist gives the converted row: it reads the level and the
# third type id, so a stand-in row with those two is enough.
# ================
def curve_resist(obj, char, common_at, char_at):
	row = [""] * npcs.VSRO_CHARACTER_COLUMNS
	row[npcs.TID_COLUMNS[2]] = obj[common_at["TypeID3"]]
	row[npcs.LEVEL_COLUMN] = char[char_at["Lvl"]]
	return npcs.curve_element_resist(row)


def sha256(path):
	return hashlib.sha256(path.read_bytes()).hexdigest()


# ================
# snapshot_text
#
# The snapshot as JSON with one row per line, so a refresh diffs by row.
# ================
def snapshot_text(sources, rows):
	head = {"format": FORMAT, "sha256": sources, "fields": list(RESIST_FIELDS)}
	lines = [json.dumps(head, ensure_ascii=False)[:-1] + ', "rows": {']
	body = [f"\t{json.dumps(code)}: {json.dumps(values)}" for code, values in rows.items()]
	return "\n".join(lines + [",\n".join(body), "}}"]) + "\n"


def main(argv=None):
	parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[1])
	parser.add_argument("--vsro-db", type=Path, required=True, help="the export's SRO_VT_SHARD/data directory")
	parser.add_argument("--media", type=Path, required=True, help="v1.150 server_dep/silkroad/textdata")
	parser.add_argument("--output", type=Path, default=OUTPUT)
	args = parser.parse_args(argv)
	csv.field_size_limit(FIELD_LIMIT)
	chars = read_export(args.vsro_db / CHAR_TABLE)
	common = read_export(args.vsro_db / COMMON_TABLE)
	rows = off_curve_rows(chars, common, native_codenames(args.media))
	sources = {CHAR_TABLE: sha256(args.vsro_db / CHAR_TABLE), COMMON_TABLE: sha256(args.vsro_db / COMMON_TABLE)}
	args.output.write_text(snapshot_text(sources, rows), encoding="utf-8", newline="\n")
	print(f"{len(rows)} rows -> {args.output}")
	return 0


if __name__ == "__main__":
	sys.exit(main())
