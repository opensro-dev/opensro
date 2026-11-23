"""
===========================================================================

export_cap110_database.py - the cap-110 monster export from a shard backup

cap110_monsters.py and cap110_rewards.py read their database rows from an
export directory (SRO_CAP110_MONSTER_DATABASE). The backup the author
exported from (SRO_VT_SHARD.bak b4d85a04) is not available; this tool
writes the same files from the Eternity v5.0 shard backup the population
evidence already pins (import_roc_quest_nests.py SOURCE_SHA256), whose
nests keep the authored spread the other copy flattened.

Files, each a table dump or a codename join over one:

	Tab_RefNest.json, Tab_RefHive.json      every nest and hive row
	Tab_RefTactics.json                     every tactics row
	all-spawns.json                         nest x tactics x hive x object
	vsro-outdoor-monster-skills.json        the enabled _RefSkill rows the
	                                        spawned monsters reach
	vsro-outdoor-drop-classes.json          _RefDropClassSel_Equip
	vsro-outdoor-rare-classes.json          _RefDropClassSel_RareEquip
	vsro-assigned-fixed.json / -random.json / -groups.json
	                                        the monster drop tables
	summary.json                            the backup's sha256

The equipment assignments (vsro-outdoor-drop-assignments.json) are left
out on purpose: without them the field's equipment drops through the
native tables (cap110_monster_loot, #1745 D3), as the client-only build's
do today.

Run only when the evidence changes. Reads the backup; it never restores
or modifies a database, and the output must lie outside it.

===========================================================================
"""
import argparse
import hashlib
import json
import struct
from pathlib import Path

from import_roc_quest_nests import (
	HIVE_COLUMNS, HIVE_DENSITY, HIVE_FIXED_END, NEST_COLUMNS, NEST_FIXED_END, NEST_POSITION, SOURCE_SHA256,
	census_rows, read_columns,
)
from loot_sql_pages import Backup

SKILL_SLOTS = tuple("DefaultSkill_" + str(slot) for slot in range(1, 11))
# The columns merge_skills follows from a skill to another (cap110_monsters
# columns 6 and 9).
SKILL_LINKS = ("Basic_Original", "Basic_ChainCode")
MAGIC_SLOTS = 9
REGION_MASK = 0xFFFF
DIRECTION_MASK = 0xFFFF


# ================
# write_json
# ================
def write_json(path, value):
	path.write_text(json.dumps(value, ensure_ascii=False, indent=1) + "\n", encoding="utf-8", newline="\n")


# ================
# unique_rows
#
# A census reads every backup copy of a page; a row read twice must read
# the same, or the backup is refused.
# ================
def unique_rows(rows, key, table):
	unique = {}
	for row in rows:
		if row[key] in unique and unique[row[key]] != row:
			raise ValueError(f"conflicting {table} rows for {row[key]}")
		unique[row[key]] = row
	return [unique[identity] for identity in sorted(unique)]


# ================
# read_nests
# ================
def read_nests(backup):
	rows = []
	for at, page in census_rows(backup, NEST_FIXED_END):
		if struct.unpack_from("<H", page, at + 2)[0] != NEST_FIXED_END:
			continue
		row = read_columns(page, at, NEST_COLUMNS)
		row["fLocalPosX"], row["fLocalPosY"], row["fLocalPosZ"] = struct.unpack_from("<fff", page, at + NEST_POSITION)
		rows.append(row)
	return unique_rows(rows, "dwNestID", "Tab_RefNest")


# ================
# read_hives
# ================
def read_hives(backup):
	rows = []
	for at, page in census_rows(backup, HIVE_FIXED_END):
		if struct.unpack_from("<H", page, at + 2)[0] < HIVE_FIXED_END:
			continue
		row = read_columns(page, at, HIVE_COLUMNS)
		row["fMonsterCountPerPC"] = struct.unpack_from("<f", page, at + HIVE_DENSITY)[0]
		rows.append(row)
	return unique_rows(rows, "dwHiveID", "Tab_RefHive")


# ================
# join_spawns
#
# One row per nest whose tactics and hive exist, in the shape
# cap110_monsters reads: the monster by codename, the nest's world from
# its hive, the region and direction as unsigned values.
# ================
def join_spawns(nests, tactics, hives, objects):
	by_tactics = {row["dwTacticsID"]: row for row in tactics}
	by_hive = {row["dwHiveID"]: row for row in hives}
	spawns = []
	for nest in nests:
		tactic, hive = by_tactics.get(nest["dwTacticsID"]), by_hive.get(nest["dwHiveID"])
		if tactic is None or hive is None or tactic["dwObjID"] not in objects:
			continue
		monster = objects[tactic["dwObjID"]]
		spawns.append({
			"nest_id": nest["dwNestID"], "codename": monster["CodeName128"], "game_world_id": hive["GameWorldID"],
			"service": monster["Service"], "region_id_unsigned": nest["nRegionDBID"] & REGION_MASK,
			"tactics_id": nest["dwTacticsID"], "hive_id": nest["dwHiveID"],
			"x": nest["fLocalPosX"], "y": nest["fLocalPosY"], "z": nest["fLocalPosZ"],
			"radius": nest["nRadius"], "generate_radius": nest["nGenerateRadius"],
			"max_total_count": nest["dwMaxTotalCount"], "delay_min": nest["dwDelayTimeMin"],
			"delay_max": nest["dwDelayTimeMax"], "respawn": nest["btRespawn"],
			"direction_unsigned": nest["wInitialDir"] & DIRECTION_MASK, "champion_percent": nest["nChampionGenPercentage"],
			"nest_flag": nest["btFlag"], "hive_flag": hive["btFlag"], "spawn_type": nest["btType"],
		})
	return spawns


# ================
# reached_skills
#
# The enabled _RefSkill rows the spawned monsters use, closed over the
# links merge_skills follows. A disabled or missing link is left out; the
# import names it if a monster it keeps needs it.
# ================
def reached_skills(spawns, objects, characters, skills):
	enabled = {row["ID"]: row for row in skills if row["Service"] == 1}
	by_code = {row["CodeName128"]: row for row in objects.values()}
	pending = []
	for code in {spawn["codename"] for spawn in spawns}:
		character = characters.get(by_code[code]["Link"])
		if character is not None:
			pending.extend(character[slot] for slot in SKILL_SLOTS)
	reached = {}
	while pending:
		identity = pending.pop()
		if not identity or identity in reached or identity not in enabled:
			continue
		reached[identity] = enabled[identity]
		pending.extend(enabled[identity][link] for link in SKILL_LINKS)
	return [reached[identity] for identity in sorted(reached)]


# ================
# drop_rows
#
# The monster drop tables with monsters and items by codename, in the
# shape cap110_rewards reads.
# ================
def drop_rows(backup, objects):
	name = lambda identity: objects[identity]["CodeName128"] if identity in objects else None
	fixed = [{
		"monster": name(row["RefMonsterID"]), "item": name(row["RefItemID"]), "group_type": row["DropGroupType"],
		"plus": row["OptLevel"], "amount_min": row["DropAmountMin"], "amount_max": row["DropAmountMax"],
		"ratio": row["DropRatio"], "rent": row["RentCodeName"],
		**{"m" + str(slot): row["RefMagicOptionID" + str(slot)] for slot in range(1, MAGIC_SLOTS + 1)},
	} for row in backup.table_rows("_RefMonster_AssignedItemDrop")]
	random = [{
		"service": row["Service"], "monster": name(row["RefMonsterID"]), "item_group": row["RefItemGroupID"],
		"distinct_items": row["Overlap"], "amount_min": row["DropAmountMin"], "amount_max": row["DropAmountMax"],
		"ratio": row["DropRatio"], "param1": row["param1"], "param2": row["param2"],
	} for row in backup.table_rows("_RefMonster_AssignedItemRndDrop")]
	groups = [{
		"service": row["Service"], "item_group": row["RefItemGroupID"], "item": name(row["RefItemID"]),
		"ratio": row["SelectRatio"],
	} for row in backup.table_rows("_RefDropItemGroup")]
	order = lambda row: (row["monster"] or "", row.get("item") or "", row.get("item_group", 0))
	return sorted(fixed, key=order), sorted(random, key=order), sorted(groups, key=lambda row: (row["item_group"], row["item"] or ""))


# ================
# export
# ================
def export(backup_path, output):
	digest = hashlib.sha256(Path(backup_path).read_bytes()).hexdigest()
	if digest != SOURCE_SHA256:
		raise ValueError(f"{backup_path} is not the pinned shard backup ({SOURCE_SHA256})")
	output.mkdir(parents=True, exist_ok=True)
	backup = Backup(Path(backup_path))
	try:
		objects = {row["ID"]: row for row in backup.table_rows("_RefObjCommon")}
		characters = {row["ID"]: row for row in backup.table_rows("_RefObjChar")}
		tactics = unique_rows(backup.table_rows("Tab_RefTactics"), "dwTacticsID", "Tab_RefTactics")
		nests, hives = read_nests(backup), read_hives(backup)
		spawns = join_spawns(nests, tactics, hives, objects)
		skills = reached_skills(spawns, objects, characters, backup.table_rows("_RefSkill"))
		fixed, random, groups = drop_rows(backup, objects)
		tables = {
			"Tab_RefNest": nests, "Tab_RefHive": hives, "Tab_RefTactics": tactics, "all-spawns": spawns,
			"vsro-outdoor-monster-skills": skills,
			"vsro-outdoor-drop-classes": list(backup.table_rows("_RefDropClassSel_Equip")),
			"vsro-outdoor-rare-classes": list(backup.table_rows("_RefDropClassSel_RareEquip")),
			"vsro-assigned-fixed": fixed, "vsro-assigned-random": random, "vsro-assigned-groups": groups,
		}
	finally:
		backup.close()
	for name, rows in tables.items():
		write_json(output / (name + ".json"), rows)
	summary = {"backup_sha256": digest, "rows": {name: len(rows) for name, rows in tables.items()}}
	write_json(output / "summary.json", summary)
	return summary


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
	parser.add_argument("--backup", type=Path, required=True, help="the pinned SRO_VT_SHARD.bak")
	parser.add_argument("--output", type=Path, required=True, help="the export directory; never inside the backup's")
	args = parser.parse_args()
	output = args.output.resolve()
	if args.backup.resolve().parent == output or output in args.backup.resolve().parents:
		parser.error("the output must be separate from the source backup")
	print(json.dumps(export(args.backup, output)["rows"]))


if __name__ == "__main__":
	main()
