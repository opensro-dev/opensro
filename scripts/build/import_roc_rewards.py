"""
===========================================================================

import_roc_rewards.py - the Roc range's database rewards, as committed evidence

Port-only, not native: the v1.150 client has none of the Roc kinds, so the
loot snapshot (import_loot_evidence.py) excluded every rule they own. This
keeps those rules, raw, from the pinned vSRO backup: the fixed drops
(_RefMonster_AssignedItemDrop), the enabled random assignments
(_RefMonster_AssignedItemRndDrop) and the enabled members of the groups
they name (_RefDropItemGroup), whatever client holds the items. The Roc
build (roc_import.py) joins them to the items a variant installs.

Run only when updating evidence:

	python -B scripts/build/import_roc_rewards.py --vsro <SRO_VT_SHARD.bak>

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

OUTPUT = Path(__file__).resolve().parents[1] / "data" / "roc" / "rewards-source.json"
FORMAT = "opensro-roc-rewards-source-v1"
# A fixed row's nine (RefMagicOptionID, CustomValue) pairs fill +20..+74.
OPTIONS_FIRST, OPTIONS_END = 20, 74
# The one non-zero option block in the backup. All 26 rows that carry it
# (the Roc's four E051123 scrolls and the God pillars' teleport holes)
# drop ETC items, which carry no magic options, and every one holds these
# same bytes: a scripted insert's leftover, not an authored option. It is
# recorded as such; any other block refuses.
INSERT_ARTEFACT = bytes.fromhex(
	"000000000000c90700000000e0c900000000000000000000000000000000"
	"ffff00000000602100000000000000000000ffff00000000")


# ================
# fixed_options
# ================
def fixed_options(page, at, what):
	block = bytes(page[at + OPTIONS_FIRST:at + OPTIONS_END])
	if not any(block):
		return None
	if block == INSERT_ARTEFACT:
		return "insert-artefact"
	raise ValueError("unrecognised fixed reward options: " + what)


# ================
# read_rewards
# ================
def read_rewards(path, monsters):
	backup = Backup(path)
	names = {}
	for _, at, page in backup.named_rows("_RefObjCommon"):
		fields = variable_fields(page, at)
		if fields:
			names[struct.unpack_from("<I", page, at + 4)[0]] = fields[0].decode("ascii", errors="backslashreplace")
	fixed, random, members = [], [], {}
	for _, at, page in backup.named_rows("_RefMonster_AssignedItemDrop"):
		mob, item = struct.unpack_from("<II", page, at + 4)
		code, itemcode = names.get(mob), names.get(item)
		if code not in monsters:
			continue
		fields = variable_fields(page, at)
		row = {"monster": code, "item": itemcode, "plus": page[at + 13], "min": page[at + 14],
			"max": page[at + 15], "probability": struct.unpack_from("<f", page, at + 16)[0],
			"rent": fields[0].decode("ascii") if fields else ""}
		options = fixed_options(page, at, f"{code}/{itemcode}")
		if options:
			row["options"] = options
		fixed.append(row)
	for _, at, page in backup.named_rows("_RefMonster_AssignedItemRndDrop"):
		mob, group = struct.unpack_from("<II", page, at + 5)
		code = names.get(mob)
		if code not in monsters or page[at + 4] != 1:
			continue
		random.append({"monster": code, "group": group, "distinct": bool(page[at + 13]),
			"min": page[at + 14], "max": page[at + 15], "probability": struct.unpack_from("<f", page, at + 16)[0],
			"params": list(struct.unpack_from("<II", page, at + 20))})
	used = {row["group"] for row in random}
	for _, at, page in backup.named_rows("_RefDropItemGroup"):
		group, item = struct.unpack_from("<II", page, at + 5)
		if page[at + 4] != 1 or group not in used:
			continue
		members.setdefault(str(group), []).append({"codename": names.get(item),
			"probability": struct.unpack_from("<f", page, at + 13)[0]})
	result = {"format": FORMAT, "sha256": hashlib.sha256(backup.raw).hexdigest(),
		"fixed": sorted(fixed, key=lambda row: (row["monster"], row["item"] or "")),
		"random": sorted(random, key=lambda row: (row["monster"], row["group"])),
		"groups": {key: sorted(value, key=lambda row: row["codename"] or "") for key, value in sorted(members.items(),
			key=lambda pair: int(pair[0]))}}
	backup.close()
	return result


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--vsro", type=Path, required=True)
	parser.add_argument("--output", type=Path, default=OUTPUT)
	args = parser.parse_args()
	result = read_rewards(args.vsro, set(KINDS))
	write_json(args.output, result)
	print("fixed", len(result["fixed"]), "random", len(result["random"]), "groups", len(result["groups"]))


if __name__ == "__main__":
	main()
