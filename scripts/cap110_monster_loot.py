"""
===========================================================================
cap110_monster_loot.py - optional equipment loot projection for field monsters.

Port-only, not native. Reuse native compilation as the base, then join the
owner's live SQL assignments to installed equipment by codename. Only the
level-101 through 110 class probabilities are replaced by the supplied rows.

The assignments are optional (owner decision on #1745 D3, 2026-10-28): the
pinned export lacks them, because its backup stores one DropRatio and the
relative/absolute split cannot be recovered. Without them the catalog is
the native compilation unchanged, so field monsters drop through the
native tables; the classes are not replaced either, since they would point
at degree-10/11 groups that hold no item.
===========================================================================
"""
import copy
import json
import sys
from pathlib import Path

import import_vsro_alexandria as tables
from prepare_client_resources import write_atomically

sys.path.insert(0, str(Path(__file__).resolve().parent / "build"))
from generate_loot_catalog import compile_catalogs  # noqa: E402

FIRST_LEVEL = 101
LAST_LEVEL = 110
EQUIPMENT_FILE = "cap110-equipment.txt"
ASSIGNMENTS_NAME = "vsro-outdoor-drop-assignments"


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
# has_assignments
#
# Whether the export carries the drop assignments the overlay joins.
# ================
def has_assignments(source_root):
	return (source_root / (ASSIGNMENTS_NAME + ".json")).is_file()


# ================
# stage_field_equipment
# ================
def stage_field_equipment(text_root, source_root):
	# ================
	# read
	# ================
	def read(name):
		return json.loads((source_root / (name + ".json")).read_text(encoding="utf-8"))
	if not has_assignments(source_root):
		print(f"cap110 loot: {source_root} has no {ASSIGNMENTS_NAME}.json; "
			"field monsters drop through the native tables (#1745 D3)", flush=True)
		compiled = compile_field_equipment(compile_catalogs(), [], None, None, None)
	else:
		items = [r for path in text_root.glob("itemdata_*.txt") for r in tables.table_rows(path.read_bytes())]
		compiled = compile_field_equipment(compile_catalogs(), items, read(ASSIGNMENTS_NAME),
			read("vsro-outdoor-drop-classes"), read("vsro-outdoor-rare-classes"))
	write_atomically(text_root / EQUIPMENT_FILE, (json.dumps(compiled, separators=(",", ":")) + "\n").encode())
	return len(compiled["Equipment"]["items"])
