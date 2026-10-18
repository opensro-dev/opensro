"""
===========================================================================
import_trade_quotations.py - native special trader quotation evidence

Reads _ItemQuotation through the shared read-only SQL page decoder. Rows
retain their float32 values and source digest while translating v1.188
_RefObjCommon NPC and item identities by codename to v1.150 textdata IDs.
===========================================================================
"""

import argparse
import hashlib
import json
import math
import pathlib
import struct
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))
import sro_paths
from loot_sql_pages import Backup, variable_fields

TABLE = "_ItemQuotation"
FIXED_END = 44
COLUMN_COUNT = 10
COLUMNS = (
	"id", "service", "npc", "item", "base", "lower", "upper",
	"baseStock", "step", "stock",
)
DEFAULT_MEDIA = sro_paths.EXTRACTED_ROOT / "Media_extracted" / "server_dep" / "silkroad" / "textdata"


# ================
# read_reference_codenames
# ================
def read_reference_codenames(backup):
	names = {}
	for _, at, page in backup.named_rows("_RefObjCommon"):
		fields = variable_fields(page, at)
		if not fields:
			continue
		identity = struct.unpack_from("<I", page, at + 4)[0]
		name = fields[0].decode("ascii", errors="backslashreplace")
		if identity in names and names[identity] != name:
			raise ValueError("Conflicting _RefObjCommon identity")
		names[identity] = name
	return names


# ================
# read_media_ids
# ================
def read_media_ids(directory):
	media_ids = {}
	for path in sorted(directory.glob("*data*.txt")):
		if not path.name.startswith(("itemdata", "characterdata")):
			continue
		for line in path.read_bytes().decode("utf-16").splitlines():
			cells = line.split("\t")
			if len(cells) < 3 or cells[0] != "1":
				continue
			identity = int(cells[1])
			code = cells[2]
			if code in media_ids and media_ids[code] != identity:
				raise ValueError("Conflicting v1.150 textdata codename " + code)
			media_ids[code] = identity
	if not media_ids:
		raise ValueError("Empty v1.150 textdata directory")
	return media_ids


# ================
# resolve_identity
# ================
def resolve_identity(raw_id, names, media_ids, claimed_ids):
	code = names.get(raw_id)
	if not code:
		raise ValueError(f"Missing _RefObjCommon codename for ID {raw_id}")
	if code in media_ids:
		return media_ids[code]
	if raw_id in claimed_ids:
		raise ValueError(f"Unmapped _RefObjCommon ID {raw_id} ({code}) collides with v1.150 ID")
	return raw_id


# ================
# read_quotations
# ================
def read_quotations(backup, media_ids=None):
	names = read_reference_codenames(backup) if media_ids is not None else None
	claimed_ids = set(media_ids.values()) if media_ids is not None else set()
	rows = {}
	identities = {}
	for _, at, page in backup.named_rows(TABLE):
		if struct.unpack_from("<H", page, at + 2)[0] != FIXED_END:
			raise ValueError("Unsupported _ItemQuotation fixed layout")
		if struct.unpack_from("<H", page, at + FIXED_END)[0] != COLUMN_COUNT:
			raise ValueError("Unsupported _ItemQuotation column count")
		if struct.unpack_from("<H", page, at + FIXED_END + 2)[0] & ((1 << COLUMN_COUNT) - 1):
			raise ValueError("NULL _ItemQuotation field")
		row = dict(zip(COLUMNS, struct.unpack_from("<4i3f3i", page, at + 4)))
		# 4C8220 skips disabled records and records without a base stock.
		if row["service"] == 0 or row["baseStock"] == 0:
			continue
		if any(row[key] <= 0 for key in ("id", "npc", "item", "baseStock", "step")) or row["stock"] < 0:
			raise ValueError("Invalid _ItemQuotation identity or stock")
		if any(not math.isfinite(row[key]) or row[key] <= 0 for key in ("base", "lower", "upper")):
			raise ValueError("Invalid _ItemQuotation rate")
		if row["lower"] > row["upper"]:
			raise ValueError("Reversed _ItemQuotation bounds")
		if media_ids is not None:
			row["npc"] = resolve_identity(row["npc"], names, media_ids, claimed_ids)
			row["item"] = resolve_identity(row["item"], names, media_ids, claimed_ids)
		key = (row["npc"], row["item"])
		if key in rows and rows[key] != row:
			raise ValueError("Conflicting _ItemQuotation merchant/item")
		if row["id"] in identities and identities[row["id"]] != key:
			raise ValueError("Conflicting _ItemQuotation ID")
		rows[key] = row
		identities[row["id"]] = key
	if not rows:
		raise ValueError("Empty _ItemQuotation catalog")
	return [rows[key] for key in sorted(rows)]


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("backup", type=pathlib.Path)
	parser.add_argument("output", type=pathlib.Path)
	parser.add_argument("--media", type=pathlib.Path, default=DEFAULT_MEDIA)
	args = parser.parse_args()
	media_ids = read_media_ids(args.media)
	backup = Backup(args.backup, extra_tables=(TABLE,))
	try:
		rows = read_quotations(backup, media_ids=media_ids)
	finally:
		backup.close()
	with args.backup.open("rb") as source:
		digest = hashlib.file_digest(source, "sha256").hexdigest()
	document = {
		"generator": "scripts/build/import_trade_quotations.py",
		"source": digest,
		"table": TABLE,
		"rows": rows,
	}
	args.output.write_text(json.dumps(document, indent="\t") + "\n", encoding="utf-8", newline="\n")
	print(f"Imported {len(rows)} quotation rows")


if __name__ == "__main__":
	main()
