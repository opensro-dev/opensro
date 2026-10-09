"""
===========================================================================

generate_compiled_quests.py - project the compiled quest classes to specs

Reads the committed snapshots in scripts/data/quest and writes the server's
QuestSpec rows for the v1.150 quests whose v1.188 implementation is a C++
class (apps/server/internal/game/quest/.generated/compiled_quests.json),
with an audit of every class not yet projected and why.

What each source decides:
	- the class (compiled-quests-source.json): NPCs, dialogue symbols,
	  prerequisites, completion limit, and each mission's kind, NPC, item,
	  monsters and drop chance;
	- the v1.150 text (v150-text-source.json): every count an objective
	  line asks for, and the rewards a popup advertises; v1.188 rebalanced
	  both, so the class's own counts stand only where the line names none;
	- the SQL rows (sql-rewards-source.json): the reward items, and the
	  scalars of a quest whose popup advertises none.

A class that overrides a vtable slot beyond its destructor and initializer
has custom behaviour; it is projected only once CLASS_BEHAVIOUR states
what that override does in QuestSpec terms, never silently reduced to its
missions.

===========================================================================
"""

import argparse
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DATA = ROOT / "scripts/data/quest"
QUEST_PACKAGE = ROOT / "apps/server/internal/game/quest"
OUTPUT = QUEST_PACKAGE / ".generated"

# CBasicQuest mission kinds (mission +9).
MISSION_GATHER = 1
MISSION_KILL = 2
MISSION_DELIVER = 3
MISSION_DIALOG = 6

# QuestSpec objectives (definitions.go ObjectiveKind).
OBJECTIVE_TALK = 0
OBJECTIVE_COLLECT = 1
OBJECTIVE_KILL = 2
OBJECTIVE_PARALLEL = 3
OBJECTIVE_DELIVERY = 4

# The quest word slots 0x130 + n hold BASIC_MENUSTRING n.
MENU_OFFER, MENU_ACCEPT, MENU_DENY, MENU_NOT_ACHIEVED = 0x130, 0x131, 0x132, 0x133
MENU_ACHIEVED, MENU_INVENTORY_FULL, MENU_ACHIEVED_NOW = 0x134, 0x136, 0x139
MENU_ACCEPT_AFTER_CLEAR = 0x13e
MENU_MIDDLE = 0x13d

# Quest lists (dword index of the object).
LIST_COMPLETION_NPCS, LIST_QUEST_NPCS = "0xf3", "0xf7"
LIST_REQUIRED_DONE, LIST_REQUIRED_ACTIVE = "0x102", "0x10c"
# One of these completed suffices (CBasicQuest_MeetsPrerequisites 9262A0).
LIST_REQUIRED_ANY = "0x114"
KNOWN_LISTS = {LIST_COMPLETION_NPCS, LIST_QUEST_NPCS, LIST_REQUIRED_DONE, LIST_REQUIRED_ACTIVE, LIST_REQUIRED_ANY}

# The byte and word fields an initializer may write inside a dword slot,
# with the value the CBasicQuest constructor (91E200) leaves there:
#	0x106.b: the first prerequisite's completion count (+0x418, one byte
#	         per list 0x102 entry; CBasicQuest_MeetsPrerequisites 9262A0)
#	0xc3.b:  the initializers only rewrite its default
#	0x15b.b: the quest belongs to an instance world, whose id 0x15c.w
#	         holds (looked up by its INS_ codename)
# Any other value changes the quest and is not projected yet.
FIELD_DEFAULTS = {"0x106.b": 1, "0xc3.b": 0, "0x15b.b": 0, "0x15c.w": 1}
FIELD_MEANINGS = {
	"0x106.b": "prerequisite completed %s times",
	"0x15b.b": "instance world quest",
	"0x15c.w": "instance world quest",
}

# What a class's own override does, as the QuestSpec fields that port it,
# keyed by (quest, vtable slot). Each row cites the override it reads.
CLASS_BEHAVIOUR = {
	# CQSP_KT_EXINVENTORY_3_OnNpcTalk (8CF8F0): the base talk, plus a
	# 10,000 gold fee checked before the reward and taken after it.
	("QSP_KT_EXINVENTORY_3", "0x58"): {
		"TurnInGold": 10000,
		"TurnInGoldShortSymbol": "SN_TALK_QSP_KT_EXINVENTORY_3_05",
	},
}


class Unsupported(Exception):
	pass


# ================
# read
# ================
def read(name):
	return json.loads((DATA / name).read_text(encoding="utf-8"))


# ================
# word
# ================
def word(quest, slot):
	return quest["words"].get(hex(slot))


# ================
# objective_count
#
# The v1.150 line's count, else the class's own.
# ================
def objective_count(text, mission, field):
	line = text["objectives"].get(mission["fields"].get("0xd"))
	if line and line["count"]:
		return line["count"]
	value = mission["fields"].get(field)
	if not isinstance(value, int) or value <= 0:
		raise Unsupported("mission count unavailable")
	return value


# ================
# mission_monsters
#
# Gather and kill missions list their monsters from +0x1D (gather) or
# +0x19 (kill) at a four-byte stride, +0x19 / +0x15 holding the number.
# ================
def mission_monsters(fields, first, count_field):
	count = fields.get(count_field)
	if not isinstance(count, int) or count <= 0:
		raise Unsupported("mission monster list unavailable")
	names = [fields.get(hex(first + 4 * i)) for i in range(count)]
	if not all(isinstance(n, str) and n.startswith("MOB_") for n in names):
		raise Unsupported("mission monster list unavailable")
	return names


# ================
# project_mission
#
# One gather or kill mission as a MissionSpec-shaped row.
# ================
def project_mission(text, mission):
	fields = mission["fields"]
	kind = fields.get("0x9")
	row = {"ContentsSymbol": fields.get("0xd")}
	if kind == MISSION_GATHER:
		item = fields.get("0x241")
		chance = fields.get("0x14d")
		if not isinstance(item, str) or not isinstance(chance, (int, float)) or chance <= 0:
			raise Unsupported("gather mission without an item or drop chance")
		row.update({"Objective": OBJECTIVE_COLLECT, "CollectItemCodename": item,
			"CollectCount": objective_count(text, mission, "0x23d"),
			"MonsterDrop": {"MonsterCodenames": mission_monsters(fields, 0x1d, "0x19"), "ChancePercent": float(chance)}})
		return row
	if kind == MISSION_KILL:
		row.update({"Objective": OBJECTIVE_KILL, "KillMonsterCodenames": mission_monsters(fields, 0x19, "0x15"),
			"KillCount": objective_count(text, mission, "0x149")})
		return row
	raise Unsupported("mission kind %s in a parallel quest" % kind)


# ================
# rewards
#
# The popup's advertised scalars win; SQL stands in where it advertises
# none. Items always come from SQL, the popup naming them only in prose.
# ================
def rewards(code, text, sql):
	row = sql.get(code)
	advertised = text["reward"]
	spec = {}
	if advertised and (advertised["exp"] or advertised["skillExp"] or advertised["gold"]):
		spec.update({"RewardExp": advertised["exp"], "RewardGold": advertised["gold"], "RewardSkillExp": advertised["skillExp"]})
	elif row:
		spec.update({"RewardExp": row["exp"], "RewardGold": row["gold"], "RewardSkillExp": row["skillExp"]})
	else:
		raise Unsupported("reward unavailable")
	items = row["items"] if row else []
	for column in ("skillPoints", "ap", "hwan"):
		if row and row[column]:
			raise Unsupported("reward " + column)
	# Slots follow the same precedence: the popup's count, else the row's.
	slots = (advertised or {}).get("inventorySlots") or (row["inventorySlots"] if row else 0)
	if slots:
		spec["RewardInventorySlots"] = slots
	leads = [{"ItemCodename": i["item"], "Count": i["count"]} for i in items]
	if not row or row["selectionCount"] == 0:
		spec["RewardItems"] = leads
		return spec
	# A selection reward: the player picks SelectionCnt of the listed items.
	# The v1.150 rows pick one; the server offers each as an NPC row titled
	# by the item's name (rewardchoice.go). Reward kinds 1-5 index dialogue
	# rewards and never appear with a selection.
	if row["selectionCount"] != 1 or any(i["choice"] != 0 for i in items) or len(items) < 2:
		raise Unsupported("selection of %d" % row["selectionCount"])
	spec["RewardItems"] = []
	spec["RewardChoices"] = [{"TitleSymbol": "", "Items": [lead]} for lead in leads]
	if row["checkCountry"]:
		spec["RewardChoiceCheckCountry"] = True
	return spec


# ================
# check_fields
#
# A byte or word field that differs from its constructor default changes
# the quest: refuse it until QuestSpec carries it, rather than project a
# three-times prerequisite as a once.
# ================
def check_fields(quest):
	for key in sorted(quest["words"]):
		if not re.fullmatch(r'0x[0-9a-f]+\.[bw]', key):
			continue
		value = quest["words"][key]
		if key in FIELD_DEFAULTS and value == FIELD_DEFAULTS[key]:
			continue
		meaning = FIELD_MEANINGS.get(key)
		if meaning is None:
			raise Unsupported("quest field %s = %s" % (key, value))
		raise Unsupported(meaning % value if "%s" in meaning else meaning)


# ================
# project
# ================
def project(code, quest, text, sql):
	unhandled = [slot for slot in quest["overrides"] if (code, slot) not in CLASS_BEHAVIOUR]
	if unhandled:
		raise Unsupported("custom behaviour " + ",".join(unhandled))
	unknown = sorted(set(quest["lists"]) - KNOWN_LISTS)
	if unknown:
		raise Unsupported("quest list " + ",".join(unknown))
	check_fields(quest)
	missions = quest["missions"]
	if not missions:
		raise Unsupported("no missions")
	lists = quest["lists"]
	start = quest["tables"].get("0xc4", {}).get("0x8")
	if not isinstance(start, str):
		raise Unsupported("start NPC unavailable")
	spec = {
		"Codename": code,
		"MaxCompletions": quest["words"].get("+0x2d", 1),
		"KindByte": 1,
		"StartNpcCodename": start,
		"EndNpcCodename": (lists.get(LIST_COMPLETION_NPCS) or lists.get(LIST_QUEST_NPCS) or [start])[0],
		"OfferPromptSymbol": word(quest, MENU_OFFER),
		"AcceptResponseSymbol": word(quest, MENU_ACCEPT),
		"DenyResponseSymbol": word(quest, MENU_DENY),
		"CompletePromptSymbol": word(quest, MENU_ACHIEVED),
		"RequiredQuests": lists.get(LIST_REQUIRED_DONE, []),
	}
	if lists.get(LIST_REQUIRED_ACTIVE):
		spec["RequiredActiveQuests"] = lists[LIST_REQUIRED_ACTIVE]
	if lists.get(LIST_REQUIRED_ANY):
		spec["RequiredAnyQuests"] = lists[LIST_REQUIRED_ANY]
	for slot in quest["overrides"]:
		spec.update(CLASS_BEHAVIOUR[(code, slot)])
	for key, slot in (("NotAchievedSymbol", MENU_NOT_ACHIEVED), ("InventoryFullSymbol", MENU_INVENTORY_FULL),
			("RepeatOfferPromptSymbol", MENU_ACCEPT_AFTER_CLEAR), ("AchievedNowSymbol", MENU_ACHIEVED_NOW),
			("AcceptNoticeSymbol", MENU_MIDDLE)):
		if word(quest, slot):
			spec[key] = word(quest, slot)
	spec.update(rewards(code, text, sql))
	kinds = {m["fields"].get("0x9") for m in missions}
	if kinds == {MISSION_DIALOG} or kinds == {MISSION_DELIVER}:
		if len(missions) != 1:
			raise Unsupported("several talk or delivery missions")
		fields = missions[0]["fields"]
		npc = fields.get("0x15")
		talk = fields.get("0x1e" if MISSION_DIALOG in kinds else "0xc0")
		if not isinstance(npc, str) or not isinstance(talk, str):
			raise Unsupported("talk or delivery mission without an NPC or line")
		spec.update({"EndNpcCodename": npc, "CompletePromptSymbol": talk})
		if MISSION_DIALOG in kinds:
			spec["Objective"] = OBJECTIVE_TALK
		else:
			item = fields.get("0x42")
			if not isinstance(item, str):
				raise Unsupported("delivery mission without an item")
			spec.update({"Objective": OBJECTIVE_DELIVERY, "DeliveryItems": [{"ItemCodename": item, "Count": objective_count(text, missions[0], "0x19")}]})
	elif kinds <= {MISSION_GATHER, MISSION_KILL}:
		rows = [project_mission(text, m) for m in missions]
		if len(rows) == 1:
			row = rows[0]
			row.pop("ContentsSymbol")
			spec.update(row)
		else:
			spec.update({"Objective": OBJECTIVE_PARALLEL, "Objectives": rows})
	else:
		raise Unsupported("mission kinds " + ",".join(str(k) for k in sorted(kinds, key=str)))
	if not spec["CompletePromptSymbol"] or not spec["OfferPromptSymbol"]:
		raise Unsupported("dialogue symbols unavailable")
	return spec


# ================
# authored_elsewhere
#
# The quests the server already authors: the script-backed catalog and
# the curated Go specs. A compiled row may require them.
# ================
def authored_elsewhere():
	codes = {row["Codename"] for row in json.loads((QUEST_PACKAGE / "catalog_generated.json").read_text(encoding="utf-8"))}
	for source in sorted(QUEST_PACKAGE.glob("*.go")):
		if source.name.endswith("_test.go"):
			continue
		codes.update(re.findall(r'Codename:\s*"(Q[A-Z0-9_]+)"', source.read_text(encoding="utf-8")))
	return codes


# ================
# build
#
# Project every class, then hold back a row whose prerequisite neither
# this catalog nor the server authors, until none is left unresolved.
# ================
def build():
	compiled = read("compiled-quests-source.json")["quests"]
	text = read("v150-text-source.json")["quests"]
	sql = read("sql-rewards-source.json")["quests"]
	specs, audit = {}, {}
	for code in sorted(compiled):
		try:
			specs[code] = project(code, compiled[code], text[code], sql)
		except Unsupported as err:
			audit[code] = str(err)
	elsewhere = authored_elsewhere()
	changed = True
	while changed:
		changed = False
		for code in sorted(specs):
			spec = specs[code]
			missing = [q for q in spec["RequiredQuests"] + spec.get("RequiredActiveQuests", []) if q not in specs and q not in elsewhere]
			anyof = spec.get("RequiredAnyQuests", [])
			if anyof and not any(q in specs or q in elsewhere for q in anyof):
				missing += anyof
			if missing:
				audit[code] = "prerequisite " + ",".join(missing) + " pending"
				del specs[code]
				changed = True
	return [specs[code] for code in sorted(specs)], audit


# ================
# render
# ================
def render(value):
	return json.dumps(value, indent="\t", ensure_ascii=False) + "\n"


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__.splitlines()[3])
	parser.add_argument("--check", action="store_true", help="fail when the committed output differs")
	args = parser.parse_args()
	specs, audit = build()
	outputs = {"compiled_quests.json": render(specs), "compiled_quests_audit.json": render(audit)}
	if args.check:
		stale = [name for name, body in outputs.items() if not (OUTPUT / name).exists() or (OUTPUT / name).read_text(encoding="utf-8") != body]
		if stale:
			raise SystemExit("compiled quest catalog is stale: " + ", ".join(stale))
		return
	OUTPUT.mkdir(parents=True, exist_ok=True)
	for name, body in outputs.items():
		(OUTPUT / name).write_text(body, encoding="utf-8", newline="\n")
	print("%d projected, %d pending" % (len(specs), len(audit)))


if __name__ == "__main__":
	main()
