"""
===========================================================================
import_trade_quotations.py - native special trader quotation evidence

Reads _ItemQuotation through the shared read-only SQL page decoder. Rows
retain their float32 values and source digest; the runtime must join NPC and
item identities against the v1.150 catalog before admitting a transaction.
===========================================================================
"""

import argparse
import hashlib
import json
import math
import pathlib
import struct

from loot_sql_pages import Backup

TABLE = "_ItemQuotation"
FIXED_END = 44
COLUMN_COUNT = 10
COLUMNS = (
	"id", "service", "npc", "item", "base", "lower", "upper",
	"baseStock", "step", "stock",
)


# ================
# read_quotations
# ================
def read_quotations(backup):
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
	args = parser.parse_args()
	backup = Backup(args.backup, extra_tables=(TABLE,))
	try:
		rows = read_quotations(backup)
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
