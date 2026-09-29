"""
===========================================================================
import_loot_evidence.py - normalize version-joined loot evidence

Run only when updating evidence. The build consumes committed snapshots and
never needs a backup or a path outside this repository.
===========================================================================
"""

import argparse
import hashlib
import json
import struct
from pathlib import Path

from loot_sql_pages import Backup, variable_fields


# ================
# write_json
# ================
def write_json(path, value):
	path.parent.mkdir(parents=True, exist_ok=True)
	path.write_text(json.dumps(value, separators=(",", ":"), ensure_ascii=True) + "\n", encoding="utf-8", newline="\n")


# ================
# read_media
# ================
def read_media(directory):
	items, monsters, hashes = {}, set(), {}
	for path in sorted(directory.glob("*data*.txt")):
		if not path.name.startswith(("itemdata", "characterdata")):
			continue
		data = path.read_bytes()
		hashes[path.name] = hashlib.sha256(data).hexdigest()
		for line in data.decode("utf-16").splitlines():
			cells = line.split("\t")
			if len(cells) < 15 or cells[0] != "1":
				continue
			code = cells[2]
			if code.startswith("MOB_"):
				monsters.add(code)
			elif path.name.startswith("itemdata"):
				items[code] = {
					"id": int(cells[1]), "codename": code, "type": list(map(int, cells[9:13])),
					"country": int(cells[14]), "rarity": int(cells[15]), "class": int(cells[61]),
					"maxMagic": int(cells[158]), "level": int(cells[33]), "param1": int(cells[118]),
				}
	magic = []
	assign = []
	for name in ("magicoption.txt", "magicoptionassign.txt"):
		data = (directory / name).read_bytes()
		hashes[name] = hashlib.sha256(data).hexdigest()
		for line in data.decode("utf-16").splitlines():
			cells = line.split("\t")
			if cells[0] != "1":
				continue
			if name == "magicoption.txt":
				magic.append({"id": int(cells[1]), "name": cells[2], "degree": int(cells[4]),
					"probability": float(cells[5]), "tag": int(cells[7]), "params": list(map(int, cells[8:11])),
					"categories": [cells[index] for index in range(29, len(cells) - 1, 2) if cells[index + 1] == "1"]})
			else:
				assign.append({"country": int(cells[1]), "type3": int(cells[2]), "type4": int(cells[3]),
					"options": [value for value in cells[4:] if value != "xxx"]})
	return {"items": items, "monsters": sorted(monsters), "magic": magic, "magicAssignments": assign, "hashes": hashes}


# ================
# read_backup
# ================
def read_backup(path, media):
	backup = Backup(path)
	names = {}
	for _, at, page in backup.named_rows("_RefObjCommon"):
		fields = variable_fields(page, at)
		if fields:
			identity = struct.unpack_from("<I", page, at + 4)[0]
			name = fields[0].decode("ascii", errors="backslashreplace")
			if identity in names and names[identity] != name:
				raise ValueError("Conflicting reference identity")
			names[identity] = name
	monsters = set(media["monsters"])
	groups, random, fixed, custom, excluded = {}, [], [], [], []
	for offset, at, page in backup.named_rows("_RefDropItemGroup"):
		if page[at + 4] != 1:
			continue
		group, item = struct.unpack_from("<II", page, at + 5)
		code = names.get(item)
		row = {"codename": code, "probability": struct.unpack_from("<f", page, at + 13)[0]}
		if code in media["items"]:
			groups.setdefault(str(group), []).append(row)
		else:
			excluded.append({"table": "group", "group": group, "item": code, "reason": "absent-client-item"})
	for offset, at, page in backup.named_rows("_RefMonster_AssignedItemRndDrop"):
		if page[at + 4] != 1:
			continue
		mob, group = struct.unpack_from("<II", page, at + 5)
		code = names.get(mob)
		if code not in monsters or str(group) not in groups:
			excluded.append({"table": "random", "monster": code, "group": group,
				"reason": "absent-client-monster" if code not in monsters else "no-client-group-members"})
			continue
		if struct.unpack_from("<II", page, at + 20) != (0, 0):
			raise ValueError("Unimplemented applicable random parameters")
		random.append({"monster": code, "group": group, "distinct": bool(page[at + 13]),
			"min": page[at + 14], "max": page[at + 15], "probability": struct.unpack_from("<f", page, at + 16)[0],
			"page": offset, "record": at})
	for offset, at, page in backup.named_rows("_RefMonster_AssignedItemDrop"):
		mob, item = struct.unpack_from("<II", page, at + 4)
		code, itemcode = names.get(mob), names.get(item)
		if code not in monsters or itemcode not in media["items"]:
			excluded.append({"table": "fixed", "monster": code, "item": itemcode, "reason": "absent-client-reference"})
			continue
		fields = variable_fields(page, at)
		rent = fields[0].decode("ascii") if fields else "xxx"
		if any(page[at + 20:at + 74]) or rent not in ("", "xxx"):
			raise ValueError("Unimplemented applicable fixed modifiers or rent: " + code + "/" + itemcode)
		fixed.append({"monster": code, "item": itemcode, "plus": page[at + 13], "min": page[at + 14],
			"max": page[at + 15], "probability": struct.unpack_from("<f", page, at + 16)[0]})
	for _, at, page in backup.named_rows("_RefCustomizingReservedItemDropForMonster"):
		mob = struct.unpack_from("<I", page, at + 4)[0]
		code = names.get(mob)
		row = {"monster": code, "rarity": page[at + 8], "command": struct.unpack_from("<I", page, at + 9)[0],
			"category": page[at + 13], "params": list(struct.unpack_from("<5i", page, at + 14))}
		if code in monsters:
			custom.append(row)
		else:
			row["table"] = "custom"
			row["reason"] = "absent-client-monster"
			excluded.append(row)
	used = {str(row["group"]) for row in random}
	result = {"sha256": hashlib.sha256(backup.raw).hexdigest(), "fixed": fixed, "random": random,
		"groups": {key: value for key, value in groups.items() if key in used}, "custom": custom, "excluded": excluded}
	backup.close()
	return result


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	for name in ("media", "vsro", "isro", "output"):
		parser.add_argument("--" + name, type=Path, required=True)
	args = parser.parse_args()
	media = read_media(args.media)
	write_json(args.output / "client-source.json", media)
	for name in ("vsro", "isro"):
		result = read_backup(getattr(args, name), media)
		write_json(args.output / (name + "-rewards-source.json"), result)
		print(name, "fixed", len(result["fixed"]), "random", len(result["random"]), "custom", len(result["custom"]))


if __name__ == "__main__":
	main()
