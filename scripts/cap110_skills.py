"""
===========================================================================

cap110_skills.py - optional player skills above the native mastery cap

Port-only, not native. Preserves every native skill row and remaps imported
identities by codename, never by the later client's numeric id: the later
client renumbers group, chain-next and prerequisite references. New ranks
attach to the native mastery ladder through that mapping; a genuinely new
group gets a collision-free id. No mastery, effect or execution owner is
invented here.

===========================================================================
"""
from collections import Counter

import published_ids

SKILL_COLUMNS = 118
SERVICE = 0
IDENTITY = 1
GROUP = 2
CODENAME = 3
LEVEL = 7
CHAIN_NEXT = 9
MASTERY = 34
MASTERY_LEVEL = 36
PREREQ_IDS = (40, 41, 42)
PREREQ_LEVELS = (43, 44, 45)
SP = 46
RACE = 47
NATIVE_MASTERY_CAP = 90
IMPORT_MASTERY_CAP = 110
OUTPUT_SHARD = "skilldata_110000.txt"
MAX_ID = 0xFFFFFFFF


# ================
# player_skill
#
# The server's own catalogue line: a Chinese or European player rank. Chain
# sub-rows carry the same prefix and are selected with their roots.
# ================
def player_skill(codename):
	return codename.startswith("SKILL_CH_") or codename.startswith("SKILL_EU_")


# ================
# index_skills
#
# Every row is validated, not only the player lines: a chain-next target or a
# prerequisite may name any skill, and a short row would underrun the native
# 117-token parse.
# ================
def index_skills(rows):
	by_code, by_id = {}, {}
	for row in rows:
		if len(row) != SKILL_COLUMNS:
			raise ValueError(f"skill row must have {SKILL_COLUMNS} columns")
		code, identity = row[CODENAME], int(row[IDENTITY])
		if not code or not 0 < identity <= MAX_ID:
			raise ValueError("invalid skill identity")
		if code in by_code or identity in by_id:
			raise ValueError(f"duplicate skill identity: {code}")
		by_code[code], by_id[identity] = row, code
	return by_code, by_id


# ================
# build_group_map
#
# Later group ids are renumbered but the mapping is a bijection over shared
# player codenames. A collision would silently merge two native ladders, so an
# ambiguous mapping is refused rather than guessed.
# ================
def build_group_map(native_rows, later_rows):
	native = {row[CODENAME]: row for row in native_rows if row[SERVICE] == "1" and player_skill(row[CODENAME])}
	later = {row[CODENAME]: row for row in later_rows if row[SERVICE] == "1" and player_skill(row[CODENAME])}
	forward, reverse = {}, {}
	for code in native.keys() & later.keys():
		later_group, native_group = later[code][GROUP].strip(), native[code][GROUP].strip()
		if forward.setdefault(later_group, native_group) != native_group or reverse.setdefault(native_group, later_group) != later_group:
			raise ValueError(f"ambiguous skill group mapping at {code}")
	return forward


# ================
# apply_references
#
# One imported row with its resolved identity: id, group, chain-next and the
# three prerequisite group ids. Everything else is the later author's.
# ================
def apply_references(row, identity, group, chain_next, prereq_ids):
	converted = list(row)
	converted[IDENTITY] = str(identity)
	converted[GROUP] = str(group)
	converted[CHAIN_NEXT] = str(chain_next)
	for column, value in zip(PREREQ_IDS, prereq_ids):
		converted[column] = str(value)
	return converted


# ================
# merge_skills
#
# native_rows and later_rows are raw skilldata shard rows. Every native row is
# kept; only later player ranks above the native cap are added, with fresh
# collision-free ids and group ids and their references remapped by codename.
# previous_ids/previous_groups, read from a prior marker, keep published
# identities stable across rebuilds. pinned is the released skill pin
# (published_ids.published): a rank this merge adds keeps its released id,
# and every other released skill id stays reserved (#3815), as
# cap110_items.merge_equipment does for items.
# ================
def merge_skills(native_rows, later_rows, reserved_ids=(), previous_ids=None, previous_groups=None, pinned=None):
	index_skills(native_rows)
	index_skills(later_rows)
	group_map = build_group_map(native_rows, later_rows)
	previous_ids, previous_groups = previous_ids or {}, previous_groups or {}

	native_codes = {row[CODENAME] for row in native_rows}
	selected = [
		row for row in later_rows
		if row[SERVICE] == "1" and player_skill(row[CODENAME])
		and row[CODENAME] not in native_codes and NATIVE_MASTERY_CAP < int(row[MASTERY_LEVEL]) <= IMPORT_MASTERY_CAP
	]
	selected.sort(key=lambda row: int(row[IDENTITY]))

	occupied = {int(row[IDENTITY]) for row in native_rows + later_rows} | set(reserved_ids)
	if pinned:
		adding = {row[CODENAME]: row for row in selected}
		targets = {code: int(entry["target"]) for code, entry in previous_ids.items()}
		held, targets = published_ids.overlay(pinned, adding, occupied, targets)
		occupied |= held
		previous_ids = {code: previous_ids[code] if code in previous_ids and int(previous_ids[code]["target"]) == identity
			else {"source": int(adding[code][IDENTITY]), "target": identity} for code, identity in targets.items()}
	retained = {int(entry["target"]) for entry in previous_ids.values()}
	if len(retained) != len(previous_ids) or retained & occupied or any(not 0 < identity <= MAX_ID for identity in retained):
		raise ValueError("previous imported skill IDs collide or exceed the wire width")
	for code, entry in previous_ids.items():
		source = next((row for row in later_rows if row[CODENAME] == code), None)
		if source is None or int(entry["source"]) != int(source[IDENTITY]):
			raise ValueError(f"previous imported skill identity changed: {code}")
	occupied |= retained
	group_occupied = {int(row[GROUP]) for row in native_rows + later_rows}
	if {int(value) for value in previous_groups.values()} & group_occupied:
		raise ValueError("previous imported group IDs collide with the tables")

	# Two passes: every identity first, so a chain can point at a later rank.
	next_id = max(occupied, default=0) + 1
	assigned, id_map = {}, {}
	for row in selected:
		code = row[CODENAME]
		if code in previous_ids:
			assigned[code] = int(previous_ids[code]["target"])
		else:
			assigned[code] = next_id
			next_id += 1
		id_map[code] = {"source": int(row[IDENTITY]), "target": assigned[code]}

	# A group no shared codename uses is a genuinely new tier; 26 of them collide
	# with native group ids, so all take fresh ids from above the native range.
	next_group = max(group_occupied, default=0) + 1
	new_groups = {}
	for row in selected:
		later_group = row[GROUP].strip()
		if later_group in group_map or later_group in new_groups:
			continue
		if later_group in previous_groups:
			new_groups[later_group] = int(previous_groups[later_group])
		else:
			new_groups[later_group] = next_group
			next_group += 1
		if new_groups[later_group] in group_occupied:
			raise ValueError(f"imported group ID collides: {later_group}")

	native_id_by_code = {row[CODENAME]: int(row[IDENTITY]) for row in native_rows}
	later_by_id = {int(row[IDENTITY]): row for row in later_rows}
	added = []
	for row in selected:
		code, source = row[CODENAME], int(row[IDENTITY])
		chain = int(row[CHAIN_NEXT])
		if chain == source:
			chain = assigned[code]
		elif chain != 0:
			target = later_by_id.get(chain)
			if target is None:
				raise ValueError(f"unresolved chain target id at {code}")
			if target[CODENAME] in assigned:
				chain = assigned[target[CODENAME]]
			elif target[CODENAME] in native_id_by_code:
				chain = native_id_by_code[target[CODENAME]]
			else:
				raise ValueError(f"chain target is outside the import: {code} -> {target[CODENAME]}")
		prereqs = []
		for column in PREREQ_IDS:
			value = row[column].strip()
			if value in ("0", ""):
				prereqs.append(0)
			elif value in group_map:
				prereqs.append(int(group_map[value]))
			elif value in new_groups:
				prereqs.append(new_groups[value])
			else:
				raise ValueError(f"unresolved prerequisite group at {code}: {value}")
		group = int(group_map[row[GROUP].strip()]) if row[GROUP].strip() in group_map else new_groups[row[GROUP].strip()]
		added.append(apply_references(row, assigned[code], group, chain, prereqs))

	referenced = {int(row[CHAIN_NEXT]) for row in added if int(row[CHAIN_NEXT]) not in (0, int(row[IDENTITY]))}
	learnable = [row for row in added if int(row[IDENTITY]) not in referenced]
	masteries = Counter(int(row[MASTERY]) for row in learnable)
	return added, {
		"added": len(added),
		"learnableRanks": len(learnable),
		"chainSubRanks": len(added) - len(learnable),
		"preservedNativeRows": len(native_rows),
		"newGroupIds": {later_group: group_id for later_group, group_id in sorted(new_groups.items(), key=lambda item: item[1])},
		"groupsMappedToNative": sum(row[GROUP].strip() in group_map for row in selected),
		"masteries": dict(sorted(masteries.items())),
		"races": dict(sorted(Counter(row[RACE].strip() for row in learnable).items())),
		"idMap": id_map,
	}


# ================
# report_limits
#
# Stated once, carried into the marker: this tool ships rows, nothing else.
# ================
def report_limits():
	return [
		"Existing native skill rows, IDs and group ids are unchanged.",
		"Imported ranks keep the later client's statistics, race, SP cost, mastery requirement and effects.",
		"A rank with no execution owner does nothing when pressed; this tool adds no owner.",
		"Mastery, skill-group and skill-board display are not modified.",
	]
