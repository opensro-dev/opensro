"""
===========================================================================

import_roc_quest_nests.py - the Roc chain's quest monster nests (#3399)

QNO_RM_MEETROC_1 asks for drops from two quest kinds v1.150 lacks,
MOB_QT_RM_FEATHERMASK_01 and MOB_QT_RM_ROCKY_CLON_01 (roc_import
stages their rows from the VSRO client). The v1.188 shard places them
in six Tab_RefNest rows of one hive (545) on the Roc mountain, regions
v1.150 ships. QNO_RM_MEETROC_6 hunts seven guardian Rockies,
MOB_QT_01..07_ROCKY_CLON2, which v1.150 ships but never places; the
shard gives each one nest. This reads those rows from the shard backup the
population evidence already pins (v1188_population_evidence.tsv,
import_caravan_evidence.py) and writes them as one Version-2 population
(cap110.go's row schema) for the Roc build to stage.

The ISRO-R backup holds the same six nest ids at the same anchors with
other tuning; the v1.188 rows win, as they do for every existing nest
(population_evidence.go: the vSRO policies take precedence).

Run only when the evidence changes; the build consumes the committed
output. Reads the backup; it never restores or modifies a database.

===========================================================================
"""
import argparse
import hashlib
import json
import pathlib
import struct
import sys

from import_caravan_evidence import (
	DIVERSION_BASIS_DATA, DIVERSION_KEEP_BASIS_DATA, TACTICS_COLUMNS, TACTICS_FIXED_END, integer,
)
from loot_sql_pages import Backup, decode_page, records, variable_fields, PAGE_SIZE

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1] / "analysis"))
import project_hive_caps  # noqa: E402

# The v1.188 shard backup the population evidence pins.
SOURCE_SHA256 = "9b9179e598f303f3293df1771ed7a9339a5e9c9de0dd2401884db2817d5dd9a4"
FORMAT = "opensro-roc-quest-nests-v1"
OUTPUT = pathlib.Path(__file__).resolve().parents[1] / "data" / "roc" / "quest-nests.json"

# QNO_RM_MEETROC_1's two drop kinds, then the seven guardian Rockies
# QNO_RM_MEETROC_6 hunts (its kill missions, 90C370). v1.150 ships the
# seven (characterdata 5872-5878, level 90) but places none of them.
QUEST_KINDS = ("MOB_QT_RM_FEATHERMASK_01", "MOB_QT_RM_ROCKY_CLON_01") + tuple(
	"MOB_QT_%02d_ROCKY_CLON2" % n for n in range(1, 8))
FIELD_WORLD = 1
POPULATION_VERSION = 2

# Tab_RefNest fixed columns after the 4-byte record header
# (extract_shard_bak.py parse_nest): (name, offset, width, signed).
NEST_COLUMNS = (
	("dwNestID", 4, 4, True), ("dwHiveID", 8, 4, True), ("dwTacticsID", 12, 4, True),
	("nRegionDBID", 16, 2, False), ("wInitialDir", 30, 2, False), ("nRadius", 32, 4, True),
	("nGenerateRadius", 36, 4, True), ("nChampionGenPercentage", 40, 4, True),
	("dwDelayTimeMin", 44, 4, True), ("dwDelayTimeMax", 48, 4, True), ("dwMaxTotalCount", 52, 4, True),
	("btFlag", 56, 1, False), ("btRespawn", 57, 1, False), ("btType", 58, 1, False),
)
NEST_POSITION = 18
NEST_FIXED_END = 59

# Tab_RefHive fixed columns (extract_shard_bak.py parse_hive).
HIVE_COLUMNS = (
	("dwHiveID", 4, 4, True), ("btKeepMonsterCountType", 8, 1, False),
	("dwOverwriteMaxTotalCount", 9, 4, True), ("dwSpawnSpeedIncreaseRate", 17, 4, True),
	("dwMaxIncreaseRate", 21, 4, True), ("btFlag", 25, 1, False), ("GameWorldID", 26, 2, True),
)
HIVE_DENSITY = 13
HIVE_FIXED_END = 30

# The data page header's fixed-row length (pminlen). The backup's catalog
# names neither nest table nor hive table, so their rows are found by it,
# as extract_shard_bak.py's page census finds them.
PAGE_MIN_LENGTH = 14


# ================
# read_columns
# ================
def read_columns(page, at, columns):
	return {name: integer(page, at + offset, width, signed) for name, offset, width, signed in columns}


# ================
# census_rows
#
# Every record of every data page whose fixed-row length is pminlen.
# ================
def census_rows(backup, pminlen):
	seen = set()
	for offsets in backup.pages.values():
		for offset in offsets:
			raw = backup.raw[offset:offset + PAGE_SIZE]
			if struct.unpack_from("<H", raw, PAGE_MIN_LENGTH)[0] != pminlen:
				continue
			try:
				page = decode_page(raw)
			except ValueError:
				# A page another table's backup copy tore; no nest or hive row
				# was ever read from such a page.
				continue
			if bytes(page[:96]) in seen:
				continue
			seen.add(bytes(page[:96]))
			for at in records(page):
				yield at, page


# ================
# read_objects
#
# {object id: codename} of the quest kinds (_RefObjCommon variable field 0).
# ================
def read_objects(backup):
	objects = {}
	for _, at, page in backup.named_rows("_RefObjCommon"):
		fields = variable_fields(page, at)
		code = fields[0].decode("ascii", "replace") if fields else ""
		if code in QUEST_KINDS:
			objects[integer(page, at + 4, 4, True)] = code
	if sorted(objects.values()) != sorted(QUEST_KINDS):
		raise ValueError("the shard lacks a Roc quest kind")
	return objects


# ================
# read_tactics
#
# {tactics id: TacticsControls} of the given objects, every column the
# server's TacticsControls carries (import_caravan_evidence's layout).
# ================
def read_tactics(backup, objects):
	rows = {}
	for _, at, page in backup.named_rows("Tab_RefTactics"):
		if struct.unpack_from("<H", page, at + 2)[0] != TACTICS_FIXED_END:
			raise ValueError("Unsupported Tab_RefTactics row layout")
		if integer(page, at + 8, 4, False) not in objects:
			continue
		row = read_columns(page, at, TACTICS_COLUMNS)
		row["DiversionBasisData"] = [integer(page, at + DIVERSION_BASIS_DATA + 4 * i, 4, True) for i in range(8)]
		row["DiversionKeepBasisData"] = [integer(page, at + DIVERSION_KEEP_BASIS_DATA + 4 * i, 4, True) for i in range(8)]
		row["HasAggroType"] = False
		row["AggroType"] = 0
		if row["ID"] in rows and rows[row["ID"]] != row:
			raise ValueError("Conflicting Roc quest tactics " + str(row["ID"]))
		rows[row["ID"]] = row
	return rows


# ================
# read_nests
#
# The live Tab_RefNest rows whose tactics name a quest kind.
# ================
def read_nests(backup, tactics):
	nests = {}
	for at, page in census_rows(backup, NEST_FIXED_END):
		if struct.unpack_from("<H", page, at + 2)[0] != NEST_FIXED_END:
			continue
		row = read_columns(page, at, NEST_COLUMNS)
		if row["dwTacticsID"] not in tactics:
			continue
		row["fLocalPosX"], row["fLocalPosY"], row["fLocalPosZ"] = struct.unpack_from("<fff", page, at + NEST_POSITION)
		if row["dwNestID"] in nests and nests[row["dwNestID"]] != row:
			raise ValueError("Conflicting Roc quest nest " + str(row["dwNestID"]))
		nests[row["dwNestID"]] = row
	if not nests:
		raise ValueError("the shard places no Roc quest kind")
	return nests


# ================
# read_hives
# ================
def read_hives(backup, ids):
	hives = {}
	for at, page in census_rows(backup, HIVE_FIXED_END):
		if struct.unpack_from("<H", page, at + 2)[0] < HIVE_FIXED_END:
			continue
		row = read_columns(page, at, HIVE_COLUMNS)
		if row["dwHiveID"] in ids:
			if row["dwHiveID"] in hives and hives[row["dwHiveID"]]["dwOverwriteMaxTotalCount"] != row["dwOverwriteMaxTotalCount"]:
				raise ValueError("Conflicting Roc quest hive " + str(row["dwHiveID"]))
			row["fMonsterCountPerPC"] = struct.unpack_from("<f", page, at + HIVE_DENSITY)[0]
			hives[row["dwHiveID"]] = row
	if sorted(hives) != sorted(ids):
		raise ValueError("a Roc quest nest names a hive the shard lacks")
	return hives


# ================
# project
#
# The Version-2 population. Every nest must stand outdoors in the field
# world with no flag or spawn kind; a hive may only share a cap (no
# density policy), which joins through project_hive_caps' natural
# membership key, as the native field's shared caps do.
# ================
def project(objects, tactics, nests, hives):
	evidence = []
	for nest in nests.values():
		hive = hives[nest["dwHiveID"]]
		if nest["btFlag"] or nest["btType"] or nest["nRegionDBID"] & 0x8000 or hive["GameWorldID"] != FIELD_WORLD:
			raise ValueError("unsupported Roc quest nest " + str(nest["dwNestID"]))
		if hive["btKeepMonsterCountType"] or hive["fMonsterCountPerPC"] or hive["dwSpawnSpeedIncreaseRate"] or hive["dwMaxIncreaseRate"]:
			raise ValueError("Roc quest hive %d carries a density policy" % nest["dwHiveID"])
		if tactics[nest["dwTacticsID"]]["ChampionID"]:
			raise ValueError("Roc quest tactics %d names a champion row" % nest["dwTacticsID"])
		code = objects[tactics[nest["dwTacticsID"]]["ObjectID"]]
		evidence.append({**nest, "codename": code, "regionId": nest["nRegionDBID"],
			"hive": hive if hive["dwOverwriteMaxTotalCount"] else None})
	keys = {}
	for line in project_hive_caps.project(evidence, {FIELD_WORLD: "INS_DEFAULT"}).splitlines():
		columns = line.split("\t")
		keys[(columns[0], int(columns[1]), columns[2], columns[3], columns[4])] = (columns[5], int(columns[6]))
	rows = []
	for nest in sorted(evidence, key=lambda row: row["dwNestID"]):
		anchor = (nest["codename"], nest["regionId"]) + tuple("%.6f" % nest[k] for k in ("fLocalPosX", "fLocalPosY", "fLocalPosZ"))
		hive_key, hive_max = keys.get(anchor, ("", 0))
		rows.append({
			"Codename": nest["codename"], "NestID": nest["dwNestID"], "WorldID": FIELD_WORLD, "RegionID": nest["regionId"],
			"X": nest["fLocalPosX"], "Y": nest["fLocalPosY"], "Z": nest["fLocalPosZ"],
			"Radius": nest["nRadius"], "GenerateRadius": nest["nGenerateRadius"], "MaxCount": nest["dwMaxTotalCount"],
			"RespawnDelayMinSec": nest["dwDelayTimeMin"], "RespawnDelayMaxSec": nest["dwDelayTimeMax"],
			"Respawn": bool(nest["btRespawn"]), "InitialDir": nest["wInitialDir"],
			"ChampionGenPercentage": nest["nChampionGenPercentage"], "Controls": tactics[nest["dwTacticsID"]],
			"HiveKey": hive_key, "HiveMaxCount": hive_max,
		})
	return {"Version": POPULATION_VERSION, "Rows": rows}


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--backup", required=True, type=pathlib.Path, help="the v1.188 shard backup (SRO_VT_SHARD.bak)")
	parser.add_argument("--output", type=pathlib.Path, default=OUTPUT)
	args = parser.parse_args()
	digest = hashlib.sha256(args.backup.read_bytes()).hexdigest()
	if digest != SOURCE_SHA256:
		raise SystemExit("%s is %s, not the pinned shard backup %s" % (args.backup, digest, SOURCE_SHA256))
	backup = Backup(args.backup)
	try:
		objects = read_objects(backup)
		tactics = read_tactics(backup, objects)
		nests = read_nests(backup, tactics)
		hives = read_hives(backup, {nest["dwHiveID"] for nest in nests.values()})
	finally:
		backup.close()
	document = {"format": FORMAT, "sourceSHA256": SOURCE_SHA256, "population": project(objects, tactics, nests, hives)}
	args.output.parent.mkdir(parents=True, exist_ok=True)
	args.output.write_text(json.dumps(document, indent="\t") + "\n", encoding="utf-8", newline="\n")
	print("%d nests -> %s" % (len(document["population"]["Rows"]), args.output))


if __name__ == "__main__":
	main()
