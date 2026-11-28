"""
===========================================================================

pet_fellows.py - v1.696's Fellow pets (COS_PET2_*) on the v1.150 attack-pet wire

v1.696 ships its Fellow pets as one level-1 row per kind on a COS band
of its own (typeid-4 9) with a 3.2.1.3 summon scroll, levelling to 140
with no per-level row (#4788, step 1). The v1.150 client that defines
our scope has no wire or window for that band, so slice 1 presents each
kind as a v1.150 growth pet: one row per level, 1..140, chained by the
next-form column the server levels along (itemrefs.go NextCodename), on
the attack band, summoned by a typeid-4 1 scroll (cossummoning.go admits
a COS whose band is the scroll's typeid-4 + 2).

Inferred, not native (no client table carries a Fellow's per-level
stats): level L's stats are the v1.150 attack pet's own level-L row
(COS_P_WOLF_L), the one native growth curve, for every role alike. The
kind keeps its own identity, name, body, model and icon. Its skills are
its six innate chains, each link unlocked at the level its code names
(P2SKILL_INNATE_ASS_DEF_A_008: level 8), at the highest link reached.

Left for the later slices, each its own issue: riding on the saddle
model (#4796), the seven-slot equipment and its 3.5.x items (#4797),
the role trees, sockets and specials (#4798) and evolution through the
*_EVOLUTION_ summons (#4799). Their columns (55 saddle, 58 role, the
scroll's equipment defaults) mean something else in the v1.150 layout
and are not carried onto these rows; the evolution kinds stay single
rows, which pet_import defers.

The rows this module returns are in the later clients' 104-column
layout, like pet_inference's: pet_import converts them.

===========================================================================
"""
import re

import alexandria_npcs as characters
import cap110_items as items
import later_tables
import pet_rows

MAX_LEVEL = 140
TEMPLATE_FAMILY = "COS_P_WOLF"
TEMPLATE_SCROLL = "ITEM_COS_P_FLUTE"
# The v1.150 growth pet's scroll kind (ITEM_COS_P_FLUTE 3.2.1.1).
GROWTH_SCROLL_KIND = "1"
# The columns a Fellow keeps over the template: its identity and name,
# its body and look. Everything else (band, level, HP, attack, defence,
# EXP) is the template level's.
OWN_COLUMNS = (characters.NAME_STRING_COLUMN, characters.NAME_KEY_COLUMN) + characters.BODY_COLUMNS + (
	characters.MODEL_COLUMN, characters.ICON_COLUMN)
# An innate skill's code ends in the level its link unlocks at.
SKILL_LINK = re.compile(r"^(?P<chain>P2SKILL_\w+)_(?P<level>\d{3})$")
SKILL_ID_COLUMN = 1
SKILL_CODE_COLUMN = 3
EMPTY_SKILL = "0"


# ================
# level_code
# ================
def level_code(family, level):
	return f"{family}_{level:03d}"


# ================
# skill_chains
#
# Every innate chain's links by unlock level, and each skill id's chain:
# (chains {chain: [(level, id)] ascending}, chain_of {id: chain}).
# ================
def skill_chains(skill_rows):
	chains, chain_of = {}, {}
	for row in skill_rows:
		if len(row) <= SKILL_CODE_COLUMN:
			continue
		match = SKILL_LINK.match(row[SKILL_CODE_COLUMN])
		if not match:
			continue
		chains.setdefault(match["chain"], []).append((int(match["level"]), row[SKILL_ID_COLUMN]))
		chain_of[row[SKILL_ID_COLUMN]] = match["chain"]
	for links in chains.values():
		links.sort()
	return chains, chain_of


# ================
# skills_at
#
# A kind's default skills at a level: for each innate chain the row
# names, the highest link unlocked at or below the level; a chain whose
# first link is above it is not learned yet. The slots fill from the
# first, the rest empty.
# ================
def skills_at(row, level, chains, chain_of):
	learned = []
	for skill in row[characters.LATER_DEFAULT_SKILL_AT:characters.LATER_DEFAULT_SKILL_END]:
		if skill == EMPTY_SKILL:
			continue
		if skill not in chain_of:
			raise ValueError(f"{row[characters.CODENAME_COLUMN]}: default skill {skill} is no innate chain link")
		reached = [link for unlock, link in chains[chain_of[skill]] if unlock <= level]
		if reached:
			learned.append(reached[-1])
	return learned + [EMPTY_SKILL] * (characters.DEFAULT_SKILL_COUNT - len(learned))


# ================
# level_rows
#
# One kind's 140 rows: the template level's row under the kind's own
# columns, chained first form to next form. Level 1 keeps the kind's
# id; the others carry the template level's, which select_pets replaces
# with a free identity.
# ================
def level_rows(row, templates, chains, chain_of):
	family = row[characters.CODENAME_COLUMN]
	result = []
	for level in range(1, MAX_LEVEL + 1):
		out = list(templates[level])
		for column in OWN_COLUMNS:
			out[column] = row[column]
		out[characters.CODENAME_COLUMN] = level_code(family, level)
		out[characters.FIRST_FORM_COLUMN] = level_code(family, 1)
		out[characters.NEXT_FORM_COLUMN] = level_code(family, level + 1) if level < MAX_LEVEL else characters.UNUSED_NAME
		out[characters.LATER_DEFAULT_SKILL_AT:characters.LATER_DEFAULT_SKILL_END] = skills_at(row, level, chains, chain_of)
		if level == 1:
			out[characters.ID_COLUMN] = row[characters.ID_COLUMN]
		result.append(out)
	return result


# ================
# growth_scroll
#
# The kind's own scroll as a v1.150 growth-pet scroll: the growth kind,
# naming the level-1 row, with the template scroll's empty parameter
# tail in place of the Fellow equipment defaults.
# ================
def growth_scroll(scroll, template, family):
	out = list(scroll)
	out[items.TYPE_FOURTH] = GROWTH_SCROLL_KIND
	out[items.SUMMONED_CHARACTER] = level_code(family, 1)
	out[items.SUMMONED_CHARACTER + 1:] = template[items.SUMMONED_CHARACTER + 1:]
	return out


# ================
# fellow_tables
#
# later_chars / later_items with every summonable Fellow kind replaced
# by its level rows, and each of its scrolls (the plain one and its
# event copy; premium copies are never ported, pet_rows) by a growth
# scroll. A kind is summonable when a plain scroll names it; the
# evolution summons' scrolls name no kind of their own, so those rows
# pass through unchanged. Returns (chars, items, kinds), kinds the
# families expanded. A source without the template curve refuses.
# ================
def fellow_tables(later_chars, later_items, skill_rows):
	fellows = {row[characters.CODENAME_COLUMN]: row for row in later_chars
		if len(row) > characters.CODENAME_COLUMN and pet_rows.is_fellow(row[characters.CODENAME_COLUMN])}
	plain = {row[items.SUMMONED_CHARACTER].strip() for row in pet_rows.scroll_rows(later_items, set(fellows))
		if row[items.SERVICE] == later_tables.SERVICE_ON}
	kinds = sorted(code for code in fellows if code in plain and not pet_rows.is_evolution(code))
	if not kinds:
		return later_chars, later_items, []
	by_code = {row[characters.CODENAME_COLUMN]: row for row in later_chars if len(row) > characters.CODENAME_COLUMN}
	templates = {level: by_code.get(level_code(TEMPLATE_FAMILY, level)) for level in range(1, MAX_LEVEL + 1)}
	template_scroll = next((row for row in later_items if len(row) > items.CODENAME and row[items.CODENAME] == TEMPLATE_SCROLL), None)
	absent = [level for level, row in templates.items() if row is None]
	if absent or template_scroll is None:
		raise ValueError(f"fellow pets need the {TEMPLATE_FAMILY} curve and {TEMPLATE_SCROLL}; missing levels {absent[:3]}")
	chains, chain_of = skill_chains(skill_rows)
	replaced = set(kinds)
	chars = [row for row in later_chars if row[characters.CODENAME_COLUMN] not in replaced]
	for code in kinds:
		chars += level_rows(fellows[code], templates, chains, chain_of)
	item_rows = []
	for row in later_items:
		named = row[items.SUMMONED_CHARACTER].strip() if len(row) > items.SUMMONED_CHARACTER else ""
		if named not in replaced:
			item_rows.append(row)
		elif not pet_rows.is_premium_scroll(row[items.CODENAME]):
			item_rows.append(growth_scroll(row, template_scroll, named))
	return chars, item_rows, kinds
