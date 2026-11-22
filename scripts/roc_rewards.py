"""
===========================================================================

roc_rewards.py - the Roc range's database rewards, joined to a variant (#3399 step 5b)

Port-only, not native: the switch is SRO_ROC's. The committed evidence
(scripts/data/roc/rewards-source.json, import_roc_rewards.py) holds every
enabled rule the vSRO backup authors for the Roc kinds. This keeps the
rules whose items the variant installs and writes them as roc-rewards.txt,
in the shape of cap110-rewards.txt, which the server joins at startup
(loot/roc.go):

	MOB_RM_ROC          group 1 (three distinct Roc set pieces), group 12
	                    (2-3 degree-11 pieces), group 13 (1e-5, a degree-11
	                    rare), four E051123 scrolls 5-6 each
	the bosses, spirits groups 12 (1-2) and 13
	and wing knight

A group keeps the members the variant installs, at their authored
weights; a rule whose group keeps none, or whose fixed item the variant
lacks, is left out and listed in the summary. The four scrolls are
native v1.150 items (itemdata_5000), so their rules join on every Roc
build; the degree-11 pieces come from SRO_CAP110_ITEMS, and the Roc sets
from the same import when SRO_ROC is on too (step 5c). The rares are in no
client the port reads. With nothing kept, no file is written and the Roc
kinds drop only what the native passes roll.

Step 5d joins the fixed table's other half: ISRO-R's shard database
gives the Roc kinds the eight ITEM_QNO_RM_ARCHEMY trophies
(scripts/data/roc/trophies-source.json, import_roc_trophies.py). They
are native v1.150 items and the exchange quests that take them stage
with the MEETROC chain, so their rows join here as fixed rules, under
the same checks as the backup's.

===========================================================================
"""
import json
from pathlib import Path

import import_vsro_alexandria as tables
from prepare_client_resources import write_atomically

SOURCE = Path(__file__).resolve().parent / "data" / "roc" / "rewards-source.json"
SOURCE_FORMAT = "opensro-roc-rewards-source-v1"
# The pinned vSRO backup the evidence was read from.
SOURCE_SHA256 = "9b9179e598f303f3293df1771ed7a9339a5e9c9de0dd2401884db2817d5dd9a4"
TROPHIES_SOURCE = Path(__file__).resolve().parent / "data" / "roc" / "trophies-source.json"
TROPHIES_FORMAT = "opensro-roc-trophies-source-v1"
# The pinned ISRO-R shard backup the trophy evidence was read from.
TROPHIES_SHA256 = "89c6835f8e51b1a75950242c89cf3ad639ea7d50a22191a90632cca333664a45"
REWARDS_FILE = "roc-rewards.txt"
REWARDS_VERSION = 1
# Source groups are offset into a range neither the native groups nor
# cap110's (1000000 up) use, so a Roc group can join neither.
GROUP_BASE = 2000000
CODENAME, TYPE_FIRST, TYPE_SECOND = 2, 9, 10
# An ETC item (TypeID 3, 3): the only kind an option-artefact row may name.
ETC_TYPE = ("3", "3")
NO_RENT = ("", "xxx")


# ================
# installed_items
#
# {codename: row} of every item the variant's manifest lists.
# ================
def installed_items(text_root):
	manifest = [row[0].strip() for row in tables.table_rows((text_root / "itemdata.txt").read_bytes()) if row]
	installed = {}
	for name in manifest:
		path = text_root / name.lower()
		if not path.exists():
			path = text_root / name
		for row in tables.table_rows(path.read_bytes()):
			if len(row) > TYPE_SECOND and row[0] == "1":
				installed[row[CODENAME]] = row
	return installed


# ================
# checked_rule
# ================
def checked_rule(low, high, chance, what):
	if not 0 <= low <= high <= 255 or not 0 < chance <= 1:
		raise ValueError(f"Roc reward out of range: {what}")


# ================
# project_fixed
#
# The fixed rows the installed items can serve; the rest wait. A row
# the server cannot represent (a rent code, options on anything but an
# ETC item) refuses rather than ship another reward.
# ================
def project_fixed(rows, installed, waiting):
	kept = []
	for row in rows:
		what = f"{row['monster']}/{row['item']}"
		checked_rule(row["min"], row["max"], row["probability"], what)
		if row["rent"] not in NO_RENT:
			raise ValueError(f"Roc reward carries a rent code: {what}")
		item = installed.get(row["item"])
		if item is None:
			waiting.append(what)
			continue
		if row.get("options") and tuple(item[TYPE_FIRST:TYPE_SECOND + 1]) != ETC_TYPE:
			raise ValueError(f"Roc reward options name equipment: {what}")
		kept.append({"Monster": row["monster"], "Item": row["item"], "Plus": row["plus"],
			"Min": row["min"], "Max": row["max"], "Probability": row["probability"]})
	return kept


# ================
# project_rewards
#
# The rules the installed items can serve, and what waits. A rule the
# server cannot represent (a rent code, random parameters, options on
# anything but an ETC item) refuses rather than ship another reward.
# A distinct rule draws each member once (loot.assigned), so it waits
# until the variant installs as many members as it draws: with fewer the
# server's draw runs out and the kill's assigned drops are lost.
# ================
def project_rewards(source, installed):
	if source.get("format") != SOURCE_FORMAT or source.get("sha256") != SOURCE_SHA256:
		raise ValueError(f"{SOURCE.name} is not the pinned {SOURCE_FORMAT}")
	rules = {"Version": REWARDS_VERSION, "Fixed": [], "Random": [], "Groups": {}}
	waiting = []
	rules["Fixed"] = project_fixed(source["fixed"], installed, waiting)
	for row in source["random"]:
		what = f"{row['monster']}/group {row['group']}"
		checked_rule(row["min"], row["max"], row["probability"], what)
		if row["params"] != [0, 0]:
			raise ValueError(f"Roc reward carries random parameters: {what}")
		members = [{"Codename": member["codename"], "Probability": member["probability"]}
			for member in source["groups"].get(str(row["group"]), []) if member["codename"] in installed]
		if not members or (row["distinct"] and len(members) < row["max"]):
			waiting.append(what)
			continue
		group = GROUP_BASE + row["group"]
		rules["Groups"][str(group)] = members
		rules["Random"].append({"Monster": row["monster"], "Group": group, "Distinct": row["distinct"],
			"Min": row["min"], "Max": row["max"], "Probability": row["probability"]})
	return rules, waiting


# ================
# project_trophies
#
# The trophy rows of the ISRO-R shard's fixed table, under the same
# checks as the backup's. The trophies are native items, so on a real
# build all eight join; a row waits only where the variant lacks one.
# ================
def project_trophies(source, installed):
	if source.get("format") != TROPHIES_FORMAT or source.get("sha256") != TROPHIES_SHA256:
		raise ValueError(f"{TROPHIES_SOURCE.name} is not the pinned {TROPHIES_FORMAT}")
	waiting = []
	return project_fixed(source["fixed"], installed, waiting), waiting


# ================
# remove_rewards
# ================
def remove_rewards(text_root):
	(text_root / REWARDS_FILE).unlink(missing_ok=True)


# ================
# add_rewards
#
# Writes roc-rewards.txt from the committed evidence, or removes it when
# no rule has its items. Returns the marker's summary.
# ================
def add_rewards(text_root):
	source = json.loads(SOURCE.read_text(encoding="utf-8"))
	installed = installed_items(text_root)
	rules, waiting = project_rewards(source, installed)
	trophies_source = json.loads(TROPHIES_SOURCE.read_text(encoding="utf-8"))
	trophies, waiting_trophies = project_trophies(trophies_source, installed)
	rules["Fixed"].extend(trophies)
	if not rules["Fixed"] and not rules["Random"]:
		remove_rewards(text_root)
	else:
		write_atomically(text_root / REWARDS_FILE, (json.dumps(rules) + "\n").encode())
	return {"fixed": len(rules["Fixed"]), "random": len(rules["Random"]),
		"members": sum(len(members) for members in rules["Groups"].values()), "waiting": waiting,
		"trophies": len(trophies), "waitingTrophies": waiting_trophies,
		"sourceSHA256": source["sha256"], "trophiesSourceSHA256": trophies_source["sha256"]}
