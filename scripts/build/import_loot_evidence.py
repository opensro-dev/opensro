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


# characterdata columns 99..108: five (item RefObjID, float probability)
# pairs, read by CCharacterData_ParseTextRow (client 808670) into +0x23C /
# +0x250. v1.188 _RefObjChar no longer carries them; v1.150 monsters drop
# their own alchemy materials through them.
MATERIAL_DROP_COLUMN = 99
MATERIAL_DROP_PAIRS = 5


# ================
# read_media
# ================
def read_media(directory):
	items, monsters, hashes = {}, set(), {}
	material_pairs = []
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
				for pair in range(MATERIAL_DROP_PAIRS):
					at = MATERIAL_DROP_COLUMN + pair * 2
					item, probability = int(cells[at]), float(cells[at + 1])
					if item:
						material_pairs.append((code, item, probability))
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
	by_id = {row["id"]: code for code, row in items.items()}
	material_drops = []
	for monster, item, probability in material_pairs:
		if item not in by_id:
			raise ValueError("Material drop names no enabled client item: " + monster + " " + str(item))
		material_drops.append({"monster": monster, "item": by_id[item], "probability": probability})
	return {"items": items, "monsters": sorted(monsters), "magic": magic, "magicAssignments": assign,
		"materialDrops": material_drops, "hashes": hashes}


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


# The class-selection tables (_RefDropClassSel_<name>). Each row is a 4-byte
# record header (the u16 at +2 ends its fixed data), the monster level as
# an int at +4, then one float32 probability per class from +8. Sources
# differ in width (ISRO-R RareEquip has 60 classes to vSRO's 36), so every
# table records its own.
DROP_CLASS_TABLES = ("Equip", "RareEquip", "Recover", "Cure", "Ammo", "Scroll", "Alchemy_Tablet",
	"Alchemy_MagicStone", "Alchemy_ATTRStone", "Reinforce")


# ================
# read_drops
#
# One backup's drop selection: every class table, its item assignments and
# its gold rows, with item ids resolved to codenames and flagged when the
# v1.150 client lacks the item. Raw values only; the catalog generator owns
# every merge and filter decision.
# ================
def read_drops(path, media):
	backup = Backup(path)
	names = {}
	for _, at, page in backup.named_rows("_RefObjCommon"):
		fields = variable_fields(page, at)
		if fields:
			names[struct.unpack_from("<I", page, at + 4)[0]] = fields[0].decode("ascii", errors="backslashreplace")
	classes = {}
	for table in DROP_CLASS_TABLES:
		rows, width = [], None
		for _, at, page in backup.named_rows("_RefDropClassSel_" + table):
			count = (struct.unpack_from("<H", page, at + 2)[0] - 8) // 4
			if width is None:
				width = count
			elif count != width:
				raise ValueError("Ragged class table " + table)
			rows.append([struct.unpack_from("<I", page, at + 4)[0], list(struct.unpack_from("<%df" % count, page, at + 8))])
		rows.sort()
		classes[table] = {"width": width, "rows": rows}
	# _RefDropItemAssign: [service][item][weight][absolute][class or -1][count].
	assignments = []
	for _, at, page in backup.named_rows("_RefDropItemAssign"):
		service, item, weight, absolute, klass, count = struct.unpack_from("<6i", page, at + 4)
		code = names.get(item)
		assignments.append({"service": service, "codename": code, "weight": weight, "absolute": absolute,
			"class": klass, "count": count, "client": code in media["items"]})
	assignments.sort(key=lambda row: (row["codename"] or "", row["class"], row["weight"]))
	# _RefDropGold: [u8 level][float probability][int min][int max].
	gold = []
	for _, at, page in backup.named_rows("_RefDropGold"):
		level = page[at + 4]
		probability, low, high = struct.unpack_from("<fii", page, at + 5)
		gold.append([level, probability, low, high])
	gold.sort()
	digest = hashlib.sha256(backup.raw).hexdigest()
	backup.close()
	return {"sha256": digest, "classes": classes, "assignments": assignments, "gold": gold}


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
		write_json(args.output / (name + "-drops-source.json"), read_drops(getattr(args, name), media))
		print(name, "fixed", len(result["fixed"]), "random", len(result["random"]), "custom", len(result["custom"]))


if __name__ == "__main__":
	main()
