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
# Each ended and never completed (9262A0: count 0 and state 5).
LIST_REQUIRED_ENDED = "0x108"
KNOWN_LISTS = {LIST_COMPLETION_NPCS, LIST_QUEST_NPCS, LIST_REQUIRED_DONE, LIST_REQUIRED_ACTIVE, LIST_REQUIRED_ANY,
	LIST_REQUIRED_ENDED}

# The byte and word fields an initializer may write inside a dword slot,
# with the value the CBasicQuest constructor (91E200) leaves there:
#	0x106.b: the first prerequisite's completion count (+0x418, one byte
#	         per list 0x102 entry; CBasicQuest_MeetsPrerequisites 9262A0),
#	         projected as RequiredQuestCompletions
#	0xc3.b:  the initializers only rewrite its default
#	0x15b.b: the quest belongs to an instance world, whose id 0x15c.w
#	         holds (looked up by its INS_ codename)
# Any other value changes the quest and is not projected yet.
FIELD_DEFAULTS = {"0x106.b": 1, "0xc3.b": 0, "0x15b.b": 0, "0x15c.w": 1}
FIELD_PROJECTED = {"0x106.b"}
FIELD_MEANINGS = {
	"0x15b.b": "instance world quest",
	"0x15c.w": "instance world quest",
}

# 8A5CA0, the talk KT_SMITH_3 and KT_ACCESSORY_2/3 share, is the base talk
# in effect: it drops gates these quests do not use, and its record byte 0
# check passes because acceptance sets the byte (922DE0).
KT_SHARED_TALK = {}

# The condition table's flag word (table 0xC2 +0, CBasicQuest_MeetsPrerequisites
# 9262A0): which conditions apply to the offer.
#	1:     the character's level is at least +0x4 (MinLevel). +0x23 is the
#	       questdata level, which only picks the marker (CBasicQuest_vf118)
#	2:     the repeat limit and the prerequisite lists are checked
#	4:     items the character must hold: every one of the vector at +0x28
#	       (9262A0 reads its begin/end at +0x2C) and, when listed, any one
#	       of +0x38 (+0x3C); RequiredHeldItems and RequiredAnyHeldItems
#	0x100: country +0x27 (3 = both); the port reads questcontentsdata's
#	       country byte, which carries the same value
# Any other bit is a condition the port does not check, so the quest is
# not projected.
TABLE_CONDITIONS = "0xc2"
CONDITION_MIN_LEVEL, CONDITION_PREREQUISITES = 0x1, 0x2
CONDITION_HELD_ITEMS, CONDITION_COUNTRY = 0x4, 0x100
CONDITIONS_PORTED = CONDITION_MIN_LEVEL | CONDITION_PREREQUISITES | CONDITION_HELD_ITEMS | CONDITION_COUNTRY
# A condition the port does not check yet, on a quest already live, keyed
# (quest, flag). Empty: every live quest's conditions are ported.
CONDITION_GAPS = set()
# The held-item vectors in the condition table (flag 4).
HELD_ALL, HELD_ANY = "0x28", "0x38"

# What a class's own override does, as the QuestSpec fields that port it,
# keyed by (quest, vtable slot). Each row cites the override it reads.
CLASS_BEHAVIOUR = {
	# CQSP_KT_EXINVENTORY_3_OnNpcTalk (8CF8F0): the base talk, plus a
	# 10,000 gold fee checked before the reward and taken after it.
	("QSP_KT_EXINVENTORY_3", "0x58"): {
		"TurnInGold": 10000,
		"TurnInGoldShortSymbol": "SN_TALK_QSP_KT_EXINVENTORY_3_05",
	},
	# CQNO_EU_EASTEU_4_OnNpcTalk (8AB930): the base talk behind one story
	# page, _01 with the reply _02, before the 0x130 offer. Its initializer
	# (8AB710) pushes no prerequisite: the v1.150 "Link (Stable
	# Purification)" caption is display text only.
	("QNO_EU_EASTEU_4", "0x58"): {
		"OfferPages": [{"PromptSymbol": "SN_TALK_QNO_EU_EASTEU_4_01", "ReplySymbol": "SN_TALK_QNO_EU_EASTEU_4_02"}],
	},
	# 8E6440 sends the start NPC to vtable +0x19C and the end NPC to +0x1A0.
	# Their lines are hard-coded; the base words supply the rest. None of the
	# three sets a travel block, though SMITH_3's _02 warns against Return.
	#
	# CQNO_WC_POTION_3 8960A0 / 896360: _01 [NEXT], then _02 accept/deny,
	# deny _04; while active Bori answers _05. The hand-over (+0x111 cleared)
	# keeps the medicine and letter, and 896360 sends _16 once it completes.
	("QNO_WC_POTION_3", "0x58"): {
		"OfferPages": [{"PromptSymbol": "SN_TALK_QNO_WC_POTION_3_01", "ReplySymbol": "SN_TALK_COMMON_NEXT"}],
		"OfferPromptSymbol": "SN_TALK_QNO_WC_POTION_3_02",
		"DenyResponseSymbol": "SN_TALK_QNO_WC_POTION_3_04",
		"SideTalks": [{"NpcCodename": "NPC_WC_POTION", "PromptSymbol": "SN_TALK_QNO_WC_POTION_3_05"}],
		"CompleteNoticeSymbol": "SN_TALK_QNO_WC_POTION_3_16",
	},
	# CQNO_WC_POTION_4 897680 / 897A40: Jinjin's _01 [NEXT] accepts at once,
	# taking one medicine and one letter before the wrapped gift is granted;
	# while active she answers _03. Asa pages _05 [NEXT], says _06, then
	# takes the gift and pays.
	("QNO_WC_POTION_4", "0x58"): {
		"OfferPromptSymbol": "SN_TALK_QNO_WC_POTION_4_01",
		"OfferAcceptRowSymbol": "SN_TALK_COMMON_NEXT",
		"AcceptanceConsumes": [
			{"ItemCodename": "ITEM_QNO_WC_POTION_3_01", "Count": 1},
			{"ItemCodename": "ITEM_QNO_WC_POTION_3_02", "Count": 1},
		],
		"SideTalks": [{"NpcCodename": "NPC_CH_ACCESSORY", "PromptSymbol": "SN_TALK_QNO_WC_POTION_4_03"}],
		"EndNpcCodename": "NPC_WC_SPECIAL",
		"TalkPages": [{"PromptSymbol": "SN_TALK_QNO_WC_POTION_4_05", "ReplySymbol": "SN_TALK_COMMON_NEXT"}],
		"CompletePromptSymbol": "SN_TALK_QNO_WC_POTION_4_06",
	},
	# CQNO_WC_SMITH_3 896840 / 896AA0: _01 accept/deny, deny _03; while
	# active Agol answers _04, the mission's own not-delivered line (+0xC4).
	("QNO_WC_SMITH_3", "0x58"): {
		"OfferPromptSymbol": "SN_TALK_QNO_WC_SMITH_3_01",
		"DenyResponseSymbol": "SN_TALK_QNO_WC_SMITH_3_03",
		"SideTalks": [{"NpcCodename": "NPC_WC_SMITH", "PromptSymbol": "SN_TALK_QNO_WC_SMITH_3_04"}],
	},
	# CQNO_KT_SMITH_2_OnNpcTalk (8A77A0): the blacksmith's fork. Page _01
	# offers _02 (word 0x143) to go on to the offer, or _04 (0x144) to
	# turn it down for good: _05, and SMITH_2 and SMITH_3 end. Pressing
	# Accept ends ACCESSORY_2 and ACCESSORY_3 (8A789E on).
	("QNO_KT_SMITH_2", "0x58"): {
		"OfferPages": [{
			"PromptSymbol": "SN_TALK_QNO_KT_SMITH_2_01",
			"ReplySymbol": "SN_TALK_QNO_KT_SMITH_2_02",
			"RefuseSymbol": "SN_TALK_QNO_KT_SMITH_2_04",
			"RefuseResponseSymbol": "SN_TALK_QNO_KT_SMITH_2_05",
		}],
		"RefuseEndsQuests": ["QNO_KT_SMITH_2", "QNO_KT_SMITH_3"],
		"AcceptEndsQuests": ["QNO_KT_ACCESSORY_2", "QNO_KT_ACCESSORY_3"],
	},
	("QNO_KT_SMITH_3", "0x58"): KT_SHARED_TALK,
	# CQNO_CA_THIEF_5_OnNpcTalk (8C6180): the 0x130 offer replies _02 (the
	# fake evidence) or _04 (refuse the thief's deal). Each accepts with its
	# own line and closes the other follow-up for good: _02 answers _03 and
	# ends QNO_CA_THIEF_6_2 ("Reporting Truth"), _04 answers _05 and ends
	# QNO_CA_THIEF_6_1 ("Reporting False Evidence"). Natively any answer but
	# the first takes the _04 branch and there is no refusal row; the port's
	# branch offer keeps its DENY row, a deviation.
	("QNO_CA_THIEF_5", "0x58"): {
		"OfferBranches": [
			{"ReplySymbol": "SN_TALK_QNO_CA_THIEF_5_02", "AcceptResponseSymbol": "SN_TALK_QNO_CA_THIEF_5_03",
				"EndsQuests": ["QNO_CA_THIEF_6_2"]},
			{"ReplySymbol": "SN_TALK_QNO_CA_THIEF_5_04", "AcceptResponseSymbol": "SN_TALK_QNO_CA_THIEF_5_05",
				"EndsQuests": ["QNO_CA_THIEF_6_1"]},
		],
	},
	("QNO_KT_ACCESSORY_2", "0x58"): KT_SHARED_TALK,
	("QNO_KT_ACCESSORY_3", "0x58"): KT_SHARED_TALK,
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
# delivery_items
#
# A deliver mission holds +0x19 (byte) items, each a codename at +0x42 and
# its quantity at +0x1A, both at a four-byte stride: the pairs
# QuestBase_ValidateAndGrantMissionItems (9208D0) grants at acceptance.
# The v1.150 line's count wins for a lone item, as it does elsewhere.
# ================
def delivery_items(text, mission):
	fields = mission["fields"]
	count = fields.get("0x19")
	if not isinstance(count, int) or count <= 0:
		raise Unsupported("delivery mission without items")
	items = []
	for i in range(count):
		item = fields.get(hex(0x42 + 4 * i))
		quantity = fields.get(hex(0x1a + 4 * i))
		if not isinstance(item, str) or not isinstance(quantity, int) or quantity <= 0:
			raise Unsupported("delivery mission item unavailable")
		items.append({"ItemCodename": item, "Count": quantity})
	line = text["objectives"].get(fields.get("0xd"))
	if count == 1 and line and line["count"]:
		items[0]["Count"] = line["count"]
	return items


# ================
# handover_pages
#
# A deliver mission's hand-over dialogue (91CA00). With +0xBF pages the NPC
# first shows +0xC0 with the reply +0xE8, then page k (1..N-1) at
# +0xC4 + 4(k+1) with the reply +0xE8 + 4k, and the hand-over itself speaks
# +0xC8 + 4N. Without pages it speaks +0xC0. Returns (pages, line).
# ================
def handover_pages(fields):
	count = fields.get("0xbf", 0)
	if not isinstance(count, int) or count < 0:
		raise Unsupported("hand-over page count unavailable")
	if count == 0:
		return [], fields.get("0xc0")
	pages = []
	for k in range(count):
		prompt = fields.get("0xc0" if k == 0 else hex(0xc4 + 4 * (k + 1)))
		reply = fields.get(hex(0xe8 + 4 * k))
		if not isinstance(prompt, str) or not isinstance(reply, str):
			raise Unsupported("hand-over page %d unavailable" % k)
		pages.append({"PromptSymbol": prompt, "ReplySymbol": reply})
	line = fields.get(hex(0xc8 + 4 * count))
	if not isinstance(line, str):
		raise Unsupported("hand-over line after %d pages unavailable" % count)
	return pages, line


# ================
# parallel_deliveries
#
# Several deliver missions in one quest: each is handed over at its own
# NPC (two legs, +0x110 clear) and latches its own bit (91CEB0), and the
# quest pays at its start NPC with the ACHIEVED word once all have been.
# A one-leg mission among them would complete the quest at its NPC, which
# no shipped class does, so it is refused.
# ================
def parallel_deliveries(text, quest, missions, start):
	achieved = word(quest, MENU_ACHIEVED)
	if not achieved:
		raise Unsupported("parallel delivery without an achieved line")
	rows = []
	for mission in missions:
		fields = mission["fields"]
		if fields.get("0x110", 0) != 0:
			raise Unsupported("one-leg mission in a parallel delivery")
		npc = fields.get("0x15")
		pages, line = handover_pages(fields)
		if not isinstance(npc, str) or not isinstance(line, str):
			raise Unsupported("parallel delivery mission without an NPC or line")
		row = {"ContentsSymbol": fields.get("0xd"), "Objective": OBJECTIVE_DELIVERY,
			"DeliveryItems": delivery_items(text, mission), "HandOverNpcCodename": npc, "HandOverSymbol": line}
		if pages:
			row["HandOverPages"] = pages
		if fields.get("0x111", 1) == 0:
			row["DeliveryKeepsItems"] = True
		exchange = exchange_items(fields)
		if exchange:
			row["ExchangeItems"] = exchange
		for key, field in (("ExchangeFullSymbol", "0xc8"), ("NotAchievedSymbol", "0xc4"), ("PendingNoticeSymbol", "0x108")):
			if isinstance(fields.get(field), str):
				row[key] = fields[field]
		rows.append(row)
	return {"Objective": OBJECTIVE_PARALLEL, "Objectives": rows, "EndNpcCodename": start, "CompletePromptSymbol": achieved}


# ================
# exchange_items
#
# What a two-leg hand-over gives back (91CA00): +0x6A (byte) the number of
# items, +0x93 their codenames and +0x6B their quantities, stride four.
# ================
def exchange_items(fields):
	count = fields.get("0x6a", 0)
	items = []
	for i in range(count):
		item = fields.get(hex(0x93 + 4 * i))
		quantity = fields.get(hex(0x6b + 4 * i))
		if not isinstance(item, str) or not isinstance(quantity, int) or quantity <= 0:
			raise Unsupported("hand-over exchange item unavailable")
		items.append({"ItemCodename": item, "Count": quantity})
	return items


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
# the quest: refuse it unless QuestSpec carries it (FIELD_PROJECTED),
# rather than drop it. A value read at run time (null) is never projected.
# ================
def check_fields(quest):
	for key in sorted(quest["words"]):
		if not re.fullmatch(r'0x[0-9a-f]+\.[bw]', key):
			continue
		value = quest["words"][key]
		if key in FIELD_DEFAULTS and value == FIELD_DEFAULTS[key]:
			continue
		if key in FIELD_PROJECTED and isinstance(value, int) and value > 0:
			continue
		meaning = FIELD_MEANINGS.get(key)
		if meaning is None:
			raise Unsupported("quest field %s = %s" % (key, value))
		raise Unsupported(meaning % value if "%s" in meaning else meaning)


# ================
# conditions
#
# The offer's condition flags as QuestSpec fields: the minimum level, or
# a refusal for a condition the port would otherwise skip.
# ================
def conditions(code, quest):
	table = quest["tables"].get(TABLE_CONDITIONS, {})
	flags = table.get("flags", 0)
	if not flags & CONDITION_PREREQUISITES:
		raise Unsupported("condition flags %#x skip the prerequisites" % flags)
	unported = flags & ~CONDITIONS_PORTED
	for bit in range(32):
		if unported & (1 << bit) and (code, 1 << bit) not in CONDITION_GAPS:
			raise Unsupported("condition flag %#x" % (1 << bit))
	out = {}
	if flags & CONDITION_HELD_ITEMS:
		every, anyone = table.get(HELD_ALL, []), table.get(HELD_ANY, [])
		if not every and not anyone or not all(isinstance(item, str) for item in every + anyone):
			raise Unsupported("held-item condition without its items")
		if every:
			out["RequiredHeldItems"] = every
		if anyone:
			out["RequiredAnyHeldItems"] = anyone
	if not flags & CONDITION_MIN_LEVEL:
		return out
	level = table.get("0x4")
	if not isinstance(level, int) or level < 1:
		raise Unsupported("minimum level unavailable")
	out["MinLevel"] = level
	return out


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
	offer_conditions = conditions(code, quest)
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
		**offer_conditions,
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
	if lists.get(LIST_REQUIRED_ENDED):
		spec["RequiredEndedQuests"] = lists[LIST_REQUIRED_ENDED]
	completions = quest["words"].get("0x106.b", 1)
	if completions != 1:
		if not spec["RequiredQuests"]:
			raise Unsupported("completion count without a prerequisite")
		spec["RequiredQuestCompletions"] = [completions]
	behaviour = {}
	for slot in quest["overrides"]:
		behaviour.update(CLASS_BEHAVIOUR[(code, slot)])
	for key, slot in (("NotAchievedSymbol", MENU_NOT_ACHIEVED), ("InventoryFullSymbol", MENU_INVENTORY_FULL),
			("RepeatOfferPromptSymbol", MENU_ACCEPT_AFTER_CLEAR), ("AchievedNowSymbol", MENU_ACHIEVED_NOW),
			("AcceptNoticeSymbol", MENU_MIDDLE)):
		if word(quest, slot):
			spec[key] = word(quest, slot)
	spec.update(rewards(code, text, sql))
	kinds = {m["fields"].get("0x9") for m in missions}
	if kinds == {MISSION_DELIVER} and len(missions) > 1 and not behaviour:
		spec.update(parallel_deliveries(text, quest, missions, start))
	elif kinds == {MISSION_DIALOG} or kinds == {MISSION_DELIVER}:
		if len(missions) != 1:
			raise Unsupported("several talk missions")
		fields = missions[0]["fields"]
		# A class talk handler may hand over at its own NPC and line.
		npc = behaviour.get("EndNpcCodename", fields.get("0x15"))
		pages, talk = [], fields.get("0x1e")
		if MISSION_DIALOG not in kinds:
			pages, talk = handover_pages(fields)
		talk = behaviour.get("CompletePromptSymbol", talk)
		if not isinstance(npc, str) or not isinstance(talk, str):
			raise Unsupported("talk or delivery mission without an NPC or line")
		spec.update({"EndNpcCodename": npc, "CompletePromptSymbol": talk})
		if MISSION_DIALOG in kinds:
			spec["Objective"] = OBJECTIVE_TALK
		else:
			spec.update({"Objective": OBJECTIVE_DELIVERY, "DeliveryItems": delivery_items(text, missions[0])})
			if pages and "TalkPages" not in behaviour:
				spec["TalkPages"] = pages
			# 91CA00 answers missing items with the mission's own line
			# (+0xC4); the base word 0x133 wins where the class has one.
			if "NotAchievedSymbol" not in spec and isinstance(fields.get("0xc4"), str):
				spec["NotAchievedSymbol"] = fields["0xc4"]
			# 91CA00 removes the delivered items only while +0x111 is set,
			# as the mission constructor (872040) leaves it.
			if fields.get("0x111", 1) == 0:
				spec["DeliveryKeepsItems"] = True
			# +0x110 clear (the 872040 default): the hand-over only latches
			# the mission, and the quest pays where the achieved-now line
			# sends the player, its start NPC, with the ACHIEVED word.
			if fields.get("0x110", 0) == 0 and "EndNpcCodename" not in behaviour:
				achieved = word(quest, MENU_ACHIEVED)
				if not achieved:
					raise Unsupported("two-leg delivery without an achieved line")
				spec.update({"HandOverNpcCodename": npc, "HandOverSymbol": talk,
					"EndNpcCodename": start, "CompletePromptSymbol": achieved})
				# The pages lead to the hand-over, not to the report.
				if spec.pop("TalkPages", None):
					spec["HandOverPages"] = pages
				exchange = exchange_items(fields)
				if exchange:
					spec["ExchangeItems"] = exchange
				if isinstance(fields.get("0xc8"), str):
					spec["ExchangeFullSymbol"] = fields["0xc8"]
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
	spec.update(behaviour)
	# An absent base word is no field: POTION_4's offer has no deny line.
	spec = {key: value for key, value in spec.items() if value is not None}
	if not spec.get("CompletePromptSymbol") or not spec.get("OfferPromptSymbol"):
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
			named = spec["RequiredQuests"] + spec.get("RequiredActiveQuests", []) + spec.get("RequiredEndedQuests", [])
			named += spec.get("AcceptEndsQuests", []) + spec.get("RefuseEndsQuests", [])
			named += [q for branch in spec.get("OfferBranches", []) for q in branch.get("EndsQuests", [])]
			missing = [q for q in named if q not in specs and q not in elsewhere]
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
