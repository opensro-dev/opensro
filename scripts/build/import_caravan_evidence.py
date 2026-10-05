"""
===========================================================================
import_caravan_evidence.py - trade caravan bandit tactics and region zones

Reads the vSRO shard backup that the region combat catalog already pins and
publishes what the caravan ambush owner needs from it: the eight bandit
Tab_RefTactics rows the server binds by number (Caravan_SpawnBandits 60BF30
uses 2001..2004 for thieves, 2011..2014 for hunters) and each region's
caravan zone from _RefRegion's ContinentName (Caravan_GetContinentZone
60BDD0). Run only when the evidence changes; the build consumes the
committed outputs.
===========================================================================
"""

import argparse
import hashlib
import json
import pathlib
import struct

from loot_sql_pages import Backup, variable_fields

CARAVAN_TACTICS = (2001, 2002, 2003, 2004, 2011, 2012, 2013, 2014)

# Tab_RefTactics fixed columns: (name, offset, width, signed). The row's
# fixed part ends at 127; szDescString128 is the only variable column.
TACTICS_COLUMNS = (
	("ID", 4, 4, False), ("ObjectID", 8, 4, False), ("AIQoS", 12, 1, False),
	("MaxStamina", 13, 4, True), ("StaminaVariance", 17, 1, False), ("SightRange", 18, 4, True),
	("AggressType", 22, 1, False), ("AggressData", 23, 4, True), ("ChangeTarget", 27, 1, False),
	("HelpRequest", 28, 1, False), ("HelpResponse", 29, 1, False), ("BattleStyle", 30, 1, False),
	("BattleStyleData", 31, 4, True), ("DiversionBasis", 35, 1, False),
	("DiversionKeepBasis", 68, 1, False), ("KeepDistance", 101, 1, False),
	("KeepDistanceData", 102, 4, True), ("TraceType", 106, 1, False), ("TraceBoundary", 107, 1, False),
	("TraceData", 108, 4, True), ("HomingType", 112, 1, False), ("HomingData", 113, 4, True),
	("AggressOnHoming", 117, 1, False), ("FleeType", 118, 1, False), ("ChampionID", 119, 4, False),
	("Flags", 123, 4, False),
)
TACTICS_FIXED_END = 127
DIVERSION_BASIS_DATA = 36
DIVERSION_KEEP_BASIS_DATA = 69

# Caravan_GetContinentZone 60BDD0: strcmp against the region's continent.
# Anything else is zone 2 (after a minidump), which holds no bandits.
ZONE_BY_CONTINENT = {
	"CHINA": 0, "West_China": 0, "Oasis_Kingdom": 0, "Roc": 0,
	"Eu": 1, "Am": 1, "Ca": 1, "DELTA": 1, "SD": 1, "KingsValley": 1,
}
OTHER_ZONE = 2


# ================
# integer
# ================
def integer(page, at, width, signed):
	return int.from_bytes(page[at:at + width], "little", signed=signed)


# ================
# read_tactics
# ================
def read_tactics(backup):
	rows = {}
	for _, at, page in backup.named_rows("Tab_RefTactics"):
		if struct.unpack_from("<H", page, at + 2)[0] != TACTICS_FIXED_END:
			raise ValueError("Unsupported Tab_RefTactics row layout")
		identity = integer(page, at + 4, 4, False)
		if identity not in CARAVAN_TACTICS:
			continue
		row = {name: integer(page, at + offset, width, signed) for name, offset, width, signed in TACTICS_COLUMNS}
		row["DiversionBasisData"] = [integer(page, at + DIVERSION_BASIS_DATA + 4 * i, 4, True) for i in range(8)]
		row["DiversionKeepBasisData"] = [integer(page, at + DIVERSION_KEEP_BASIS_DATA + 4 * i, 4, True) for i in range(8)]
		row["HasAggroType"] = False
		row["AggroType"] = 0
		if identity in rows and rows[identity] != row:
			raise ValueError("Conflicting caravan tactics " + str(identity))
		rows[identity] = row
	if sorted(rows) != list(CARAVAN_TACTICS):
		raise ValueError("Missing caravan tactics rows")
	return rows


# ================
# read_zones
# ================
def read_zones(backup):
	zones = {}
	for _, at, page in backup.named_rows("_RefRegion"):
		fields = variable_fields(page, at)
		if not fields:
			raise ValueError("Unreadable _RefRegion row")
		region = struct.unpack_from("<H", page, at + 4)[0]
		zone = ZONE_BY_CONTINENT.get(fields[0].decode("ascii"), OTHER_ZONE)
		if region in zones and zones[region] != zone:
			raise ValueError("Conflicting caravan zones")
		zones[region] = zone
	if not zones:
		raise ValueError("Empty caravan zone catalog")
	return zones


# ================
# zone_ranges
# ================
def zone_ranges(zones):
	ranges = []
	for region, zone in sorted(zones.items()):
		if ranges and ranges[-1][1] + 1 == region and ranges[-1][2] == zone:
			ranges[-1][1] = region
		else:
			ranges.append([region, region, zone])
	return ranges


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("backup", type=pathlib.Path)
	parser.add_argument("tactics", type=pathlib.Path)
	parser.add_argument("zones", type=pathlib.Path)
	args = parser.parse_args()
	backup = Backup(args.backup)
	try:
		tactics = read_tactics(backup)
		zones = read_zones(backup)
	finally:
		backup.close()
	with args.backup.open("rb") as source:
		digest = hashlib.file_digest(source, "sha256").hexdigest()
	document = {"source": digest, "tactics": {str(key): tactics[key] for key in sorted(tactics)}}
	args.tactics.write_text(json.dumps(document, indent="\t") + "\n", encoding="utf-8", newline="\n")
	lines = [
		"// Generated by scripts/build/import_caravan_evidence.py; DO NOT EDIT.",
		"// _RefRegion source SHA256: " + digest,
		"// Catalog rows: " + str(len(zones)),
		"package world",
		"",
		"var regionCaravanZoneRanges = [...]regionCaravanZoneRange{",
	]
	for first, last, zone in zone_ranges(zones):
		lines.append(f"\t{{0x{first:04x}, 0x{last:04x}, {zone}}},")
	lines.extend(["}", ""])
	args.zones.write_text("\n".join(lines), encoding="utf-8", newline="\n")


if __name__ == "__main__":
	main()
