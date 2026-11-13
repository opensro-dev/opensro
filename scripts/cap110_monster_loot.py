"""
===========================================================================
cap110_monster_loot.py - optional equipment loot projection for field monsters.

Port-only, not native. Reuse native compilation as the base, then join the
owner's live SQL assignments to installed equipment by codename. Only the
level-101 through 110 class probabilities are replaced by the supplied rows.

The export's assignments are optional (owner decision on #1745 D3,
2026-10-28): the pinned export lacks them, because its backup stores one
DropRatio and the relative/absolute split cannot be recovered. An export
without them stages the native compilation unchanged.

A client-only build (no export, #3246) joins the committed vSRO backup
instead (#3405): its enabled degree-10/11 equipment assignments, which the
v1.150 compile drops only because the v1.150 client lacks the items, land
on the variant's installed itemdata by codename. The backup authors no
normal class row above 101, so levels 102-110 mirror D10's banding (see
mirrored_normal_classes). Its rare rows above 94 stay as authored: empty
or 1e-9, below the runtime's negligible probability, so no seal drops
there.
===========================================================================
"""
import copy
import json
import re
import sys
from pathlib import Path

import import_vsro_alexandria as tables
from prepare_client_resources import write_atomically

sys.path.insert(0, str(Path(__file__).resolve().parent / "build"))
from generate_loot_catalog import NEGLIGIBLE, compile_catalogs, live_generations, read_source  # noqa: E402

FIRST_LEVEL = 101
LAST_LEVEL = 110
EQUIPMENT_FILE = "cap110-equipment.txt"
ASSIGNMENTS_NAME = "vsro-outdoor-drop-assignments"
# Degree-10/11 equipment codenames (ITEM_CH_BLADE_10_A, ..._11_A_RARE).
EXPANSION_EQUIPMENT = re.compile(r"^ITEM_(CH|EU)_\w+_1[01]_[ABC](_RARE)?$")
# An assignment row's class -1: the item's own class (_RefDropItemAssign).
OWN_CLASS = -1
# D10 is groups 27-29 and rolls at monster levels 91-101 in the compiled
# normal table (90 is ISRO-R's D9 row); D11 is groups 30-32. MIRROR_SHIFT
# moves D10's eleven levels onto 102-110.
D10_FIRST_GROUP = 27
DEGREE_GROUPS = 3
MIRROR_SHIFT = 11


# ================
# compile_field_equipment
#
# assignments None compiles the native catalog unchanged (see the header).
# ================
def compile_field_equipment(base, item_rows, assignments, normal, rare):
	equipment = copy.deepcopy(base["equipment.json"])
	properties = copy.deepcopy(base["properties.json"])
	property_fields = ("codename", "type", "country", "rarity", "class", "maxMagic")
	properties["items"] = {code: {key: value[key] for key in property_fields} for code, value in properties["items"].items()}
	if assignments is None:
		return {"Version": 1, "Equipment": equipment, "Properties": properties}
	items = {r[2]: r for r in item_rows if len(r) == 160 and r[0] == "1"}
	known = {r["codename"] for r in equipment["items"]}
	for assignment in assignments:
		code = assignment["CodeName128"]
		if assignment["Service"] != 1 or not assignment["Prob_Relative"]:
			continue
		if code not in items:
			raise ValueError("assigned expansion item is not installed: " + code)
		row = items[code]
		if int(row[10]) != 1 or not 10 <= (int(row[61]) + 2) // 3 <= 11:
			raise ValueError("field equipment assignment outside D10/D11: " + code)
		if code in known:
			continue
		known.add(code)
		equipment["items"].append({
			"codename": code, "country": int(row[14]), "group": int(row[61]) - 1,
			"rare": int(row[15]) > 0, "type": ":".join(row[9:13]), "weight": assignment["Prob_Relative"],
			"absolute": assignment["Prob_Absolute"], "level": int(row[33]),
		})
		properties["items"][code] = {"codename": code, "type": list(map(int, row[9:13])), "country": int(row[14]),
			"rarity": int(row[15]), "class": int(row[61]), "maxMagic": int(row[158])}
	for name, rows in (("normal", normal), ("rare", rare)):
		by_level = {r["MonLevel"]: r for r in rows}
		for level in range(FIRST_LEVEL, LAST_LEVEL + 1):
			if level not in by_level:
				raise ValueError(f"missing field {name} drop class at level {level}")
			source = by_level[level]
			width = equipment["widths"][name]
			probabilities = [source.get("ProbGroup" + str(i + 1), 0.0) for i in range(width)]
			if any(not 0 <= p <= 1 for p in probabilities) or sum(probabilities) > 1:
				raise ValueError("invalid field drop class")
			equipment[name][level - 1] = probabilities
	return {"Version": 1, "Equipment": equipment, "Properties": properties}


# ================
# backup_assignments
#
# The vSRO backup's live, enabled, weighted degree-10/11 equipment rows,
# in the export's shape, for the items the variant installed. Rows of an
# item the variant lacks (SRO_CAP110_ITEMS off) are left out: its class
# then falls back to the native degrees, as before.
# ================
def backup_assignments(drops, installed):
	assignments = []
	for row in live_generations(drops["assignments"], [], "vsro"):
		code = row["codename"]
		if row["service"] != 1 or not row["weight"] or row["class"] != OWN_CLASS:
			continue
		if not EXPANSION_EQUIPMENT.match(code) or code not in installed:
			continue
		assignments.append({"CodeName128": code, "Service": 1, "Prob_Relative": row["weight"],
			"Prob_Absolute": row["absolute"]})
	return sorted(assignments, key=lambda a: a["CodeName128"])


# ================
# mirrored_normal_classes
#
# Inference (#3405, owner 11-13 "Mirror D10"): no backup or binary authors
# a normal equipment class above monster level 101, where D10 tops out.
# The original would have carried D11 the way it carried D10, so level L
# takes level L - MIRROR_SHIFT's row with D10's groups moved up one degree:
# 102-104 roll group 30, 105 group 31, 106-108 groups 31-32, 109-110
# group 32, each at D10's 0.0035. An authored row (101) is kept as it is.
# ================
def mirrored_normal_classes(normal):
	rows = []
	for level in range(FIRST_LEVEL, LAST_LEVEL + 1):
		probabilities = list(normal[level - 1])
		if not any(p > NEGLIGIBLE for p in probabilities):
			source = normal[level - MIRROR_SHIFT - 1]
			probabilities = [0.0] * len(source)
			for group in range(D10_FIRST_GROUP, D10_FIRST_GROUP + DEGREE_GROUPS):
				probabilities[group + DEGREE_GROUPS] = source[group]
		rows.append(class_row(level, probabilities))
	return rows


# ================
# authored_classes
#
# The native rows for 101-110 as they stand, in the export's row shape.
# ================
def authored_classes(table):
	return [class_row(level, table[level - 1]) for level in range(FIRST_LEVEL, LAST_LEVEL + 1)]


# ================
# class_row
# ================
def class_row(level, probabilities):
	row = {"MonLevel": level}
	for i, p in enumerate(probabilities):
		row["ProbGroup" + str(i + 1)] = p
	return row


# ================
# require_expansion_rows
#
# A field monster rolls at its own level (up to 110); an empty normal row
# there drops nothing, so the build refuses one rather than ship it.
# ================
def require_expansion_rows(compiled):
	for level in range(FIRST_LEVEL, LAST_LEVEL + 1):
		if not any(p > NEGLIGIBLE for p in compiled["Equipment"]["normal"][level - 1]):
			raise ValueError(f"cap110 loot: no normal equipment class rolls at level {level}")


# ================
# has_assignments
#
# Whether the export carries the drop assignments the overlay joins.
# No export at all (None, the client-only build) names none.
# ================
def has_assignments(source_root):
	return source_root is not None and (source_root / (ASSIGNMENTS_NAME + ".json")).is_file()


# ================
# stage_field_equipment
# ================
def stage_field_equipment(text_root, source_root):
	# ================
	# read
	# ================
	def read(name):
		return json.loads((source_root / (name + ".json")).read_text(encoding="utf-8"))
	items = [r for path in text_root.glob("itemdata_*.txt") for r in tables.table_rows(path.read_bytes())]
	if source_root is None:
		# Client-only (#3246, #3405): the committed vSRO backup supplies the
		# expansion gear and the native classes, mirrored above 101.
		base = compile_catalogs()
		installed = {r[2] for r in items if len(r) == 160 and r[0] == "1"}
		assignments = backup_assignments(read_source("vsro-drops"), installed)
		print(f"cap110 loot: client-only build; {len(assignments)} backup expansion assignments "
			"join the installed items (#3405)", flush=True)
		if not assignments:
			print("cap110 loot: no degree-10/11 equipment is installed (SRO_CAP110_ITEMS off); "
				"levels 101-110 fall back to degree-9 gear", flush=True)
		equipment = base["equipment.json"]
		compiled = compile_field_equipment(base, items, assignments,
			mirrored_normal_classes(equipment["normal"]), authored_classes(equipment["rare"]))
		require_expansion_rows(compiled)
	elif not has_assignments(source_root):
		print(f"cap110 loot: {source_root} has no {ASSIGNMENTS_NAME}.json; "
			"field monsters drop through the native tables (#1745 D3)", flush=True)
		compiled = compile_field_equipment(compile_catalogs(), [], None, None, None)
	else:
		compiled = compile_field_equipment(compile_catalogs(), items, read(ASSIGNMENTS_NAME),
			read("vsro-outdoor-drop-classes"), read("vsro-outdoor-rare-classes"))
	write_atomically(text_root / EQUIPMENT_FILE, (json.dumps(compiled, separators=(",", ":")) + "\n").encode())
	return len(compiled["Equipment"]["items"])
