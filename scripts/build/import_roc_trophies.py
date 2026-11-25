"""
===========================================================================

import_roc_trophies.py - the Roc kinds' trophy drops, as committed evidence

Port-only, not native: the trophies the ARCHEMY exchanges take
(ITEM_QNO_RM_ARCHEMY_1 to _8) are native v1.150 items, but no client
the port reads gives them a drop. ISRO-R's shard database does, in its
fixed table (_RefMonster_AssignedItemDrop): one row per trophy, on the
Roc kinds the Roc build stages. This keeps those rows, raw, from that
backup; the Roc build (roc_rewards.py) joins them to the items a
variant installs, with the database rewards (#3399 step 5d).

Run only when updating evidence:

	python -B scripts/build/import_roc_trophies.py --shard <SILKROAD_R_SHARD.bak>

===========================================================================
"""

import argparse
import hashlib
import struct
import sys
from pathlib import Path

from import_loot_evidence import write_json
from loot_sql_pages import Backup, variable_fields

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from roc_import import KINDS  # noqa: E402

OUTPUT = Path(__file__).resolve().parents[1] / "data" / "roc" / "trophies-source.json"
FORMAT = "opensro-roc-trophies-source-v1"
TROPHIES = {f"ITEM_QNO_RM_ARCHEMY_{number}" for number in range(1, 9)}
# A fixed row's nine (RefMagicOptionID, CustomValue) pairs fill +20..+74.
OPTIONS_FIRST, OPTIONS_END = 20, 74
# The one non-zero option block in the shard: pair (1, 0), then zeros,
# on every fixed row read (trophies and scrolls alike) - the database's
# default marker, not an authored option: a real one names a value. It
# is recorded as such; any other block refuses.
SHARD_DEFAULT = bytes.fromhex("01000000" + "00" * 50)


# ================
# fixed_options
# ================
def fixed_options(page, at, what):
	block = bytes(page[at + OPTIONS_FIRST:at + OPTIONS_END])
	if not any(block):
		return None
	if block == SHARD_DEFAULT:
		return "shard-default"
	raise ValueError("unrecognised fixed reward options: " + what)


# ================
# read_trophies
#
# The backup's fixed rows that drop a trophy from a Roc kind. A trophy
# the table drops twice, or not at all, refuses: the eight exchanges
# each take one trophy, and a changed table wants a human look.
# ================
def read_trophies(path, monsters):
	backup = Backup(path)
	names = {}
	for _, at, page in backup.named_rows("_RefObjCommon"):
		fields = variable_fields(page, at)
		if fields:
			names[struct.unpack_from("<I", page, at + 4)[0]] = fields[0].decode("ascii", errors="backslashreplace")
	fixed = []
	for _, at, page in backup.named_rows("_RefMonster_AssignedItemDrop"):
		mob, item = struct.unpack_from("<II", page, at + 4)
		code, itemcode = names.get(mob), names.get(item)
		if code not in monsters or itemcode not in TROPHIES:
			continue
		fields = variable_fields(page, at)
		row = {"monster": code, "item": itemcode, "plus": page[at + 13], "min": page[at + 14],
			"max": page[at + 15], "probability": struct.unpack_from("<f", page, at + 16)[0],
			"rent": fields[0].decode("ascii") if fields else ""}
		options = fixed_options(page, at, f"{code}/{itemcode}")
		if options:
			row["options"] = options
		fixed.append(row)
	found = [row["item"] for row in fixed]
	if sorted(found) != sorted(TROPHIES):
		raise ValueError(f"the shard drops {sorted(set(found))}, want one row per trophy")
	result = {"format": FORMAT, "sha256": hashlib.sha256(backup.raw).hexdigest(),
		"fixed": sorted(fixed, key=lambda row: (row["monster"], row["item"]))}
	backup.close()
	return result


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--shard", type=Path, required=True)
	parser.add_argument("--output", type=Path, default=OUTPUT)
	args = parser.parse_args()
	result = read_trophies(args.shard, set(KINDS))
	write_json(args.output, result)
	print("fixed", len(result["fixed"]))


if __name__ == "__main__":
	main()
