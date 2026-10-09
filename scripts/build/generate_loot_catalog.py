"""
===========================================================================

generate_loot_catalog.py - compile immutable v1.150 loot and eligibility audit

Only committed normalized evidence is read: the client snapshot, both
backups' drop selection (vsro/isro-drops-source.json) and their reward rules.
Neither backup is proven retail, so the merge below is an inference (#459):
ISRO-R is preferred wherever it authors a class row or assigns an item, and
vSRO fills every level and item ISRO-R leaves empty. The audit names the
source of every class row and assignment and every row the merge dropped.
Inferred ordinary consumable rates are deliberately explicit here; they are
not represented as recovered rates.

===========================================================================
"""

import argparse
import copy
import json
from pathlib import Path

from loot_class_map import class_for

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / "scripts/data/loot"
OUTPUT = ROOT / "apps/server/internal/game/item/loot/.generated"
INFERRED_RATES = {2: 0.10, 3: 0.02, 4: 0.05, 5: 0.05, 6: 0.01}
SPECIAL_ITEMS = ["ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_" + kind + "_B"
	for kind in ("WEAPON", "SHIELD", "ARMOR", "ACCESSARY")] + ["ITEM_ETC_SCROLL_RETURN_02"]
# Preference order: the first source that authors a row or an item wins.
SOURCE_ORDER = ("isro", "vsro")
# Owner, 2026-10-10 (#457): rare equipment keeps vSRO's table and items.
# ISRO-R's rare table rolls only d9 A at levels 76-90, which would leave
# every other degree's rare unreachable there.
RARE_SOURCE_ORDER = ("vsro", "isro")
# The catalog's level rows, as the runtime's catalogLevels.
CATALOG_LEVELS = 180
# The runtime's negligibleProbability: a class at or below it never rolls.
NEGLIGIBLE = 0.000001
# _RefDropItemAssign service value of an enabled row.
SERVICE_ENABLED = 1
# The equipment class tables, keyed by the catalog's rare flag.
EQUIPMENT_TABLES = {False: "Equip", True: "RareEquip"}
# The consumable families' class tables. Arrows and bolts share Ammo.
FAMILY_TABLES = {2: "Recover", 3: "Cure", 4: "Ammo", 5: "Ammo", 6: "Scroll", 7: "Alchemy_Tablet",
	8: "Alchemy_MagicStone", 9: "Alchemy_ATTRStone", 10: "Reinforce"}
# The potion a potion tablet manufactures (process.go's potion product).
POTION_PRODUCT_TYPE = [3, 3, 13, 1]
# Client TypeID3/TypeID4 of each ordinary consumable family; the committed
# vSRO projection held exactly these pairs.
FAMILY_BY_TYPE = {(1, 1): 2, (1, 2): 2, (1, 3): 2, (2, 6): 3, (3, 1): 6, (4, 1): 4, (4, 2): 5, (10, 1): 10,
	(11, 1): 8, (11, 2): 9, (11, 3): 7}


# ================
# read_source
# ================
def read_source(name):
	return json.loads((SOURCE / (name + "-source.json")).read_text(encoding="utf-8"))


# ================
# level_band
# ================
def level_band(level):
	return sum(level >= boundary for boundary in (20, 40, 60, 80, 90))


# ================
# level_spans
#
# [[first, last], ...] runs of consecutive levels, for a readable audit.
# ================
def level_spans(levels):
	spans = []
	for level in sorted(levels):
		if spans and spans[-1][1] == level - 1:
			spans[-1][1] = level
		else:
			spans.append([level, level])
	return spans


# ================
# merge_class_table
#
# One class table across both sources, padded to the widest. Per level the
# first source whose row rolls any class supplies the whole row; a level no
# source authors stays empty. Rows are never mixed within a level, so each
# level keeps one source's probabilities and total.
# ================
def merge_class_table(drops, table):
	order = RARE_SOURCE_ORDER if table == EQUIPMENT_TABLES[True] else SOURCE_ORDER
	width = max(drops[source]["classes"][table]["width"] for source in order)
	authored = {}
	for source in order:
		authored[source] = {level: probabilities for level, probabilities in drops[source]["classes"][table]["rows"]}
	rows, origins = [], {}
	for level in range(1, CATALOG_LEVELS + 1):
		row, origin = [0.0] * width, "none"
		for source in order:
			probabilities = authored[source].get(level)
			if probabilities and any(p > NEGLIGIBLE for p in probabilities):
				row[:len(probabilities)] = probabilities
				origin = source
				break
		rows.append(row)
		origins.setdefault(origin, []).append(level)
	return rows, {origin: level_spans(levels) for origin, levels in origins.items()}


# ================
# live_generations
#
# A backup keeps older generations of an updated _RefDropItemAssign row on
# other pages. The row on the page with the newest LSN is the live one: it
# reproduces every choice of the old committed vSRO projection (336 of 336
# duplicated stones). Generations are keyed by item and class and chosen
# before any filter, so a newer disabled generation disables the item.
# ================
def live_generations(rows, audit, source):
	generations = {}
	for row in rows:
		generations.setdefault((row["item"], row["class"]), []).append(row)
	live = set()
	for key, group in generations.items():
		group = sorted(group, key=lambda row: row["lsn"])
		for older, newer in zip(group, group[1:]):
			if older["lsn"] == newer["lsn"]:
				raise ValueError("Two generations share an LSN: " + str(newer["codename"]))
		live.add(id(group[-1]))
		if len(group) > 1:
			audit.append({"source": source, "item": group[-1]["codename"], "class": key[1],
				"kept": [group[-1]["service"], group[-1]["weight"], group[-1]["absolute"]],
				"older": [[row["service"], row["weight"], row["absolute"]] for row in group[:-1]]})
	return [row for row in rows if id(row) in live]


# ================
# select_assignments
#
# Each item's live, enabled _RefDropItemAssign rows from the first source
# that enables it, in RARE_SOURCE_ORDER for rare equipment. Rows naming an item the v1.150 client lacks are dropped and
# logged by source and raw id; zero-weight rows are dropped and logged.
# ================
def select_assignments(drops, items, audit):
	enabled = {source: {} for source in SOURCE_ORDER}
	for source in SOURCE_ORDER:
		for row in live_generations(drops[source]["assignments"], audit["olderGenerations"], source):
			if row["service"] != SERVICE_ENABLED:
				continue
			if not row["client"]:
				audit["absentFromClient"].append({"source": source, "item": row["item"], "codename": row["codename"]})
				continue
			if row["weight"] == 0:
				# A zero weight is never picked; the runtime refuses it.
				audit["zeroWeight"].append({"source": source, "item": row["codename"], "class": row["class"]})
				continue
			enabled[source].setdefault(row["codename"], []).append(row)
	chosen = {}
	for codename in sorted(set(enabled["isro"]) | set(enabled["vsro"])):
		ref = items[codename]
		order = RARE_SOURCE_ORDER if ref["type"][1] == 1 and ref["rarity"] > 0 else SOURCE_ORDER
		source = next(source for source in order if codename in enabled[source])
		chosen[codename] = (source, enabled[source][codename])
		audit["assignmentSources"][source].append(codename)
	for source in SOURCE_ORDER:
		audit["assignmentSources"][source].sort()
	# ISRO-R service-0 rows for an item only vSRO enables: vSRO still
	# supplies it, since ISRO-R retired content v1.150 still has.
	retired = set()
	for row in drops["isro"]["assignments"]:
		if row["service"] != SERVICE_ENABLED and row["client"] and chosen.get(row["codename"], ("", None))[0] == "vsro":
			retired.add(row["codename"])
	audit["isroRetiredFromVsro"] = sorted(retired)
	return chosen


# ================
# project_assignments
#
# The catalog rows of the chosen assignments: equipment joins its normal or
# rare table by the client's rarity, consumables their family by client
# type, each at the class class_for() places it in.
# ================
def project_assignments(chosen, items, audit):
	equipment, consumables = [], []
	# RefObjID order, as the committed vSRO projection: a bucket's weighted
	# pool keeps the reference data's load order.
	for codename, (source, rows) in sorted(chosen.items(), key=lambda entry: items[entry[0]]["id"]):
		ref = items[codename]
		kind = ref["type"][1]
		family = FAMILY_BY_TYPE.get(tuple(ref["type"][2:])) if kind == 3 else None
		if kind != 1 and family is None:
			audit["unplacedAssignments"].append({"item": codename, "source": source, "reason": "no-ordinary-class-table"})
			continue
		for row in rows:
			rare = ref["rarity"] > 0
			table = EQUIPMENT_TABLES[rare] if kind == 1 else FAMILY_TABLES[family]
			group = class_for(source, table, row, ref)
			if group is None:
				audit["unplacedAssignments"].append({"item": codename, "source": source, "class": row["class"],
					"reason": "class-for-refused"})
				continue
			type_name = ":".join(str(part) for part in ref["type"])
			if kind == 1:
				equipment.append({"codename": codename, "country": ref["country"], "group": group, "rare": rare,
					"type": type_name, "weight": row["weight"], "absolute": row["absolute"], "level": ref["level"]})
			else:
				consumables.append({"codename": codename, "family": family, "group": group, "weight": row["weight"],
					"absolute": row["absolute"], "level": 0, "type": type_name, "count": row["count"]})
	return equipment, consumables


# ================
# drop_itemless_classes
#
# A class that rolls at some level but holds no v1.150 item. 724120 walks
# down from an empty class to the next class that holds items, so such a
# class stays and its roll falls to that lower class (d10 equipment rolls at
# levels 91-101 fall to d9). Only a class with no item at or below it is
# zeroed: its roll could never produce anything, so the outcome is the same,
# and the runtime refuses an unreachable class. Both are logged; no
# probability is ever redistributed.
# ================
def drop_itemless_classes(name, rows, groups, audit):
	logged = {}
	for level, row in enumerate(rows, 1):
		for group, probability in enumerate(row):
			if probability <= NEGLIGIBLE or group in groups:
				continue
			lower = max((candidate for candidate in groups if candidate < group), default=None)
			if lower is None:
				row[group] = 0.0
			logged.setdefault((group, lower), []).append(level)
	for (group, lower), levels in sorted(logged.items(), key=lambda entry: (entry[0][0], entry[0][1] is None, entry[0][1] or 0)):
		audit.append({"table": name, "class": group, "fallsTo": lower, "levels": level_spans(levels)})


# ================
# tablet_stone_degree
#
# The degree of the stone a tablet makes, or None for a potion tablet or a
# product the client lacks.
# ================
def tablet_stone_degree(tablet, items):
	product = items.get(tablet.get("product"))
	if product is None or product["type"][2] != 11:
		return None
	return product["param1"]


# ================
# tablet_manufactures
#
# Whether compounding the tablet can succeed: 509BC0 makes a stone only from
# a nonempty assimilation distribution (its param2) and a potion as is. The
# v1.150 SOLID stones carry none, so their tablets would drop as dead items.
# ================
def tablet_manufactures(tablet, items):
	product = items.get(tablet.get("product"))
	if product is None:
		return False
	if product["type"] == POTION_PRODUCT_TYPE:
		return True
	return product["type"][2] == 11 and product["param2"] != 0


# ================
# compile_catalogs
# ================
def compile_catalogs():
	client = read_source("client")
	drops = {source: read_source(source + "-drops") for source in SOURCE_ORDER}
	vsro, isro = read_source("vsro-rewards"), read_source("isro-rewards")
	if vsro["custom"] or isro["custom"]:
		raise ValueError("New applicable custom loot rules need a versioned implementation")
	items = client["items"]
	merge_audit = {"classRows": {}, "assignmentSources": {source: [] for source in SOURCE_ORDER},
		"absentFromClient": [], "zeroWeight": [], "olderGenerations": [], "unplacedAssignments": [], "itemlessClasses": []}
	tables = {}
	for table in sorted(set(EQUIPMENT_TABLES.values()) | set(FAMILY_TABLES.values())):
		tables[table], merge_audit["classRows"][table] = merge_class_table(drops, table)
	chosen = select_assignments(drops, items, merge_audit)
	equipment_items, consumable_items = project_assignments(chosen, items, merge_audit)
	equipment = {"version": 2, "widths": {"normal": len(tables["Equip"][0]), "rare": len(tables["RareEquip"][0])},
		"normal": tables["Equip"], "rare": tables["RareEquip"], "items": equipment_items}
	consumables = {"version": 2, "widths": {str(family): len(tables[table][0]) for family, table in FAMILY_TABLES.items()},
		"classes": {str(family): copy.deepcopy(tables[table]) for family, table in FAMILY_TABLES.items()},
		"items": consumable_items}
	equipment_rebindings = []
	for row in equipment["items"]:
		if not row["rare"]:
			continue
		group = row["group"]
		remaining = equipment["rare"][row["level"] - 1:]
		if any(level[group] > NEGLIGIBLE for level in remaining):
			continue
		# Client-required torso levels outlive the donor's A-rare class window.
		# Reconstruction: join those assignments to the next enabled class of
		# the same degree. Preserve all class rates and the original item weight.
		for candidate in range(group + 1, (group // 3 + 1) * 3):
			if any(level[candidate] > NEGLIGIBLE for level in remaining):
				equipment_rebindings.append({"item": row["codename"], "from": group, "to": candidate,
					"reason": "client-required-level-outlives-source-class-window"})
				row["group"] = candidate
				break
		else:
			raise ValueError("Rare equipment has no compatible class: " + row["codename"])
	degrees = {(items[row["codename"]]["class"] + 2) // 3 for row in equipment["items"]}
	excluded = []
	filtered = []
	for row in consumables["items"]:
		ref = items[row["codename"]]
		if row["family"] in (8, 9) and ref["param1"] not in degrees:
			excluded.append({"item": row["codename"], "reason": "material-degree-has-no-client-equipment"})
		elif row["family"] == 7 and tablet_stone_degree(ref, items) not in degrees | {None}:
			# The stone it makes is itself unavailable for want of equipment.
			excluded.append({"item": row["codename"], "reason": "material-degree-has-no-client-equipment"})
		elif row["family"] == 7 and not tablet_manufactures(ref, items):
			excluded.append({"item": row["codename"], "reason": "tablet-product-cannot-be-manufactured"})
		else:
			filtered.append(row)
	consumables["items"] = filtered
	for family, probability in INFERRED_RATES.items():
		rows = consumables["classes"][str(family)]
		for level in range(1, len(rows) + 1):
			if any(rows[level - 1]):
				raise ValueError("Authored probability conflicts with inferred family " + str(family))
			group = level_band(level)
			if family == 3:
				group = min(group, 3)
			if family == 6:
				group = 0
			rows[level - 1][group] = probability
	# An assignment alone does not enable acquisition. Preserve the source's
	# disabled classes (for example the A elixir bucket) in the audit, without
	# advertising them as reachable ordinary loot.
	active = []
	for row in consumables["items"]:
		classes = consumables["classes"][str(row["family"])]
		if any(row["group"] < len(level) and level[row["group"]] > NEGLIGIBLE for level in classes):
			active.append(row)
		else:
			excluded.append({"item": row["codename"], "reason": "source-class-disabled"})
	consumables["items"] = active
	for rare, name in EQUIPMENT_TABLES.items():
		groups = {row["group"] for row in equipment["items"] if row["rare"] == rare}
		drop_itemless_classes(name, equipment["rare" if rare else "normal"], groups, merge_audit["itemlessClasses"])
	for family in FAMILY_TABLES:
		groups = {row["group"] for row in consumables["items"] if row["family"] == family}
		drop_itemless_classes(str(family), consumables["classes"][str(family)], groups, merge_audit["itemlessClasses"])
	consumables["fixed"] = []
	for row in vsro["fixed"]:
		ref = items[row["item"]]
		if ref["type"][2:] in ([11, 1], [11, 2], [11, 7]) and ref["param1"] not in degrees:
			excluded.append({"item": row["item"], "monster": row["monster"], "reason": "material-degree-has-no-client-equipment"})
		else:
			consumables["fixed"].append(copy.deepcopy(row))
	# v1.150 characterdata columns 99..108 give each monster up to five of its
	# own alchemy materials with a drop probability; no newer server table
	# carries them. INFERENCE: the server rolls each pair as a fixed
	# per-monster reward (724A00), one item at the authored probability.
	assigned = {(row["monster"], row["item"]) for row in consumables["fixed"]}
	for row in client.get("materialDrops", []):
		if (row["monster"], row["item"]) in assigned:
			continue
		assigned.add((row["monster"], row["item"]))
		consumables["fixed"].append({"monster": row["monster"], "item": row["item"], "plus": 0, "min": 1, "max": 1,
			"probability": row["probability"]})
	consumables["random"] = []
	consumables["groups"] = {}
	seen = set()
	for source in (vsro, isro):
		for row in source["random"]:
			key = (row["monster"], row["group"])
			if key in seen:
				continue
			seen.add(key)
			group = str(row["group"])
			members = []
			for member in source["groups"][group]:
				ref = items[member["codename"]]
				if ref["type"][2:] in ([11, 1], [11, 2], [11, 7]) and ref["param1"] not in degrees:
					excluded.append({"item": member["codename"], "group": int(group), "reason": "material-degree-has-no-client-equipment"})
				else:
					members.append(member)
			if not members:
				raise ValueError("Applicable random group has no eligible members")
			if group in consumables["groups"] and consumables["groups"][group] != members:
				raise ValueError("Conflicting group identities across source versions")
			consumables["groups"][group] = members
			consumables["random"].append({key: value for key, value in row.items() if key not in ("page", "record")})
	ordinary = {row["codename"] for row in equipment["items"] + consumables["items"]}
	special = set(SPECIAL_ITEMS) | {row["item"] for row in consumables["fixed"]}
	for members in consumables["groups"].values():
		special.update(row["codename"] for row in members)
	for code in ordinary | special:
		if code not in items:
			raise ValueError("Missing client item " + code)
	eligibility = []
	unavailable = {row["item"] for row in excluded}
	for code, ref in sorted(items.items()):
		if code in ordinary:
			category = "ordinary-monster"
		elif code in special:
			category = "assigned-or-special-monster"
		elif code in unavailable:
			category = next(row["reason"] for row in excluded if row["item"] == code)
		elif ref["type"][1:3] == [3, 8] or code.startswith("ITEM_EVENT_"):
			category = "quest-or-event-acquisition"
		else:
			category = "other-acquisition-no-monster-assignment"
		eligibility.append({"codename": code, "category": category})
	properties = {"items": {code: items[code] for code in sorted(ordinary | special) if items[code]["type"][1] == 1},
		"magic": client["magic"], "assignments": client["magicAssignments"]}
	audit = {"version": 2, "generatedBy": "scripts/build/generate_loot_catalog.py", "sourceHashes": client["hashes"],
		"equipmentClassRebindings": equipment_rebindings,
		"backupHashes": {"vsro": vsro["sha256"], "isro": isro["sha256"]},
		"dropBackupHashes": {source: drops[source]["sha256"] for source in SOURCE_ORDER},
		"inferredRates": INFERRED_RATES, "bands": [1, 20, 40, 60, 80, 90],
		"inferences": ["Ordinary family 7 follows the native ordinary categories using its authored class table.",
			"Monster material pairs (characterdata 99..108) roll as fixed per-monster rewards, one item each.",
			"Each class table level takes ISRO-R's row when it rolls any class, else vSRO's.",
			"Each item takes ISRO-R's enabled assignment rows when it has any, else vSRO's.",
			"Rare equipment prefers vSRO's table and items instead (owner, #457).",
			"Of a row's generations in a backup, the one on the newest-LSN page is live."],
		"specialItems": SPECIAL_ITEMS, "eligibility": eligibility, "excludedMaterials": excluded,
		"excludedSourceRows": {"vsro": vsro["excluded"], "isro": isro["excluded"]}, "merge": merge_audit}
	return {"equipment.json": equipment, "consumables.json": consumables, "properties.json": properties, "audit.json": audit}


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--check", action="store_true")
	args = parser.parse_args()
	for name, value in compile_catalogs().items():
		data = (json.dumps(value, separators=(",", ":"), ensure_ascii=True) + "\n").encode("utf-8")
		path = OUTPUT / name
		if args.check:
			if not path.exists() or path.read_bytes() != data:
				raise SystemExit("Stale loot catalog: " + str(path))
		else:
			OUTPUT.mkdir(parents=True, exist_ok=True)
			path.write_bytes(data)
	print("Loot catalogs verified" if args.check else "Loot catalogs generated")


if __name__ == "__main__":
	main()
