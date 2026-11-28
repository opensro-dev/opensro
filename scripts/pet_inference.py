"""
===========================================================================

pet_inference.py - rows for the COS models every client ships without one

Port-only, not native. The VSRO and v1.696 clients carry five COS models
(res/cos/*.bsr, with their meshes and animations) that no characterdata
row uses: p_gorilla01 and p_gorilla02, p_frilled_lizard, c_dhorse4 and
t_dhorse4. Nothing in any source makes them summonable, so this module
infers the rows the original would have shipped and hands them to
pet_import.py as one more source; the importer's own selection,
identities, skills, name text, icons, Item Mall listing and off path then
apply unchanged (owner 2026-11-28: every COS model is in the game).

The inference, per kind (INFERRED_KINDS):

- Gorilla: v1.696's ITEM_PRE_COS_P_GORILLA_SCROLL still names
  COS_P_GORILLA_001 and is a growth-pet scroll (type 3/2/1/1), and the
  two models are a growth pet's two forms (attack, down and die
  animations). Its rows follow the bear's, the growth pet whose two
  forms split at the same level (p_bear01 to 39, p_bear02 from 40).
- Frilled lizard: ITEM_PRE_COS_P_FRILLED_LIZARD_SCROLL names
  COS_P_FRILLED_LIZARD, a pickup scroll (type 3/2/1/2, 28 days) like the
  Raccoon Dog's, whose row it follows; the model ships a pickup
  animation.
- Dragon horse tier 4: c_dhorse4 and t_dhorse4 extend the three dragon
  horse tiers (levels 75, 90, 105). Each column the tiers change is
  carried one step on: linearly when the two steps are equal, else by
  the last step's ratio (HP grows about 1.56 times a tier).

The premium scrolls are never copied (owner 2026-11-25: no Premium
Mall); a kind takes its template's plain scroll. Names are authored here
in English, the one language this port shows, because no source carries
text for the five kinds. An icon no tree holds falls back to the
template's, so a listed scroll always has a picture.

===========================================================================
"""
import re

import cap110_items as items

# Character row columns in the later clients' 104-column layout (the rows
# this module returns are that layout; pet_import converts them).
CHAR_CODE = 2
CHAR_NAME_KEY = 5
CHAR_ICON = 54
# Item columns: the description key, and the associated character a
# summon scroll names (pet_import.ITEM_ASSOCIATED_CHAR, itemrefs.go).
ITEM_DESC_KEY = 6
ITEM_ASSOCIATED_CHAR = 119
# Object text: key column and the English name column (VSRO layout,
# later_world_import.NAME_ENGLISH).
TEXT_KEY = 1
TEXT_FIRST_LANGUAGE = 2
TEXT_ENGLISH = 8
INTEGER = re.compile(r"-?\d+$")

INFERRED_KINDS = (
	{
		"family": "COS_P_GORILLA",
		"template": "COS_P_BEAR",
		"replace": (("COS_P_BEAR", "COS_P_GORILLA"), ("p_BEAR01", "p_gorilla01"), ("p_BEAR02", "p_gorilla02")),
		"names": {"Bear": "Gorilla"},
	},
	{
		"family": "COS_P_FRILLED_LIZARD",
		"template": "COS_P_RACCOONDOG",
		"replace": (("COS_P_RACCOONDOG", "COS_P_FRILLED_LIZARD"), ("p_raccoondog", "p_frilled_lizard")),
		"names": {"Raccoon Dog": "Frilled Lizard", "Raccoon": "Frilled Lizard"},
	},
	{
		"family": "COS_C_DHORSE4",
		"tiers": ("COS_C_DHORSE1", "COS_C_DHORSE2", "COS_C_DHORSE3"),
		"replace": (("DHORSE3", "DHORSE4"), ("dhorse3", "dhorse4")),
		"names": {"Goldclad": "Dragonclad"},
	},
	{
		"family": "COS_T_DHORSE4",
		"tiers": ("COS_T_DHORSE1", "COS_T_DHORSE2", "COS_T_DHORSE3"),
		"replace": (("DHORSE3", "DHORSE4"), ("dhorse3", "dhorse4")),
		"names": {"Goldclad": "Dragonclad"},
	},
)


# ================
# family_of
#
# pet_import's family rule (the codename minus a trailing level number),
# kept here so this module needs nothing from its caller.
# ================
def family_of(codename):
	return re.sub(r"_\d+$", "", codename)


# ================
# rewrite
#
# One template row with the kind's codename and path replacements
# applied to every cell (case-sensitive: codes are upper case, the model
# paths keep their own case).
# ================
def rewrite(row, replace):
	out = []
	for cell in row:
		for old, new in replace:
			cell = cell.replace(old, new)
		out.append(cell)
	return out


# ================
# extrapolate
#
# The next tier of three tier rows, column by column: a column the tiers
# share is kept; an integer column that changes takes one more step
# (linear when both steps match, else the last step's ratio); any other
# changing column keeps the last tier's value, which rewrite then renames.
# ================
def extrapolate(tiers):
	first, second, third = tiers
	out = []
	for a, b, c in zip(first, second, third):
		if a == b == c or not all(INTEGER.match(value) for value in (a, b, c)):
			out.append(c)
			continue
		x, y, z = int(a), int(b), int(c)
		if y - x == z - y or y == 0:
			out.append(str(z + (z - y)))
		else:
			out.append(str(round(z * z / y)))
	return out


# ================
# scroll_rows
#
# The later item rows that summon one of codes (column 119), the plain
# ones only: event and premium copies are rewards or a mall this port
# does not carry.
# ================
def scroll_rows(later_items, codes):
	return [
		row for row in later_items
		if len(row) > ITEM_ASSOCIATED_CHAR and row[ITEM_ASSOCIATED_CHAR].strip() in codes
		and not row[items.CODENAME].startswith(("ITEM_EVENT_", "ITEM_PRE_"))
	]


# ================
# infer_kind
#
# One kind's character and scroll rows, or None when its template is not
# in the source (a source without bears infers no gorilla).
# ================
def infer_kind(kind, by_code, by_family, later_items):
	if "tiers" in kind:
		tiers = [by_code.get(code) for code in kind["tiers"]]
		if any(row is None for row in tiers):
			return None
		chars = [rewrite(extrapolate(tiers), kind["replace"])]
		tier_scrolls = [scroll_rows(later_items, {code}) for code in kind["tiers"]]
		if any(len(found) != 1 for found in tier_scrolls):
			return None
		scrolls = [rewrite(extrapolate([found[0] for found in tier_scrolls]), kind["replace"])]
		return chars, scrolls
	template = by_family.get(kind["template"])
	if not template:
		return None
	codes = {row[CHAR_CODE] for row in template} | {kind["template"]}
	scrolls = [rewrite(row, kind["replace"]) for row in scroll_rows(later_items, codes)]
	return [rewrite(row, kind["replace"]) for row in template], scrolls


# ================
# name_rows
#
# Object-text rows for the inferred keys: each copies its template key's
# row (the kind's replacements undone to find it) and writes the
# English, with the kind's name swaps applied, into every language cell.
# A key whose template has no text is left out; the importer reports it.
# ================
def name_rows(kind, keys, texts):
	rows = []
	undo = [(new, old) for old, new in kind["replace"]]
	for key in sorted(keys):
		source = texts.get(rewrite([key], undo)[0])
		if source is None or len(source) <= TEXT_ENGLISH:
			continue
		english = source[TEXT_ENGLISH]
		for old, new in kind["names"].items():
			english = english.replace(old, new)
		row = list(source)
		row[TEXT_KEY] = key
		for column in range(TEXT_FIRST_LANGUAGE, len(row)):
			if row[column].strip() and not row[column].isdigit():
				row[column] = english
		rows.append(row)
	return rows


# ================
# icon_fallbacks / held_icon
#
# The icons an inferred portrait falls back to, best first: a tier kind
# walks its tiers down (v1.150 ships only the first dragon horse tier's
# portrait), a template kind takes its template family's first row.
# held_icon keeps the inferred name when a tree holds it, else the first
# fallback that one holds, else the inferred name (reported missing).
# ================
def icon_fallbacks(kind, by_code, column):
	if "tiers" in kind:
		return [by_code[code][column] for code in reversed(kind["tiers"]) if code in by_code]
	template = [row for code, row in sorted(by_code.items()) if family_of(code) == kind["template"]]
	return [template[0][column]] if template else []


def held_icon(icon, fallbacks, has_icon):
	for name in [icon] + fallbacks:
		if name and has_icon(name.replace("\\", "/").lower()):
			return name
	return icon


# ================
# infer_kinds
#
# Every inferred kind over one later source's tables: (character rows,
# scroll rows, object-text rows), all in the source's own layout, for
# pet_import.select_pets. texts maps an object-text key to its row.
# has_icon(name) answers whether some tree holds Media icon/<name>; an
# inferred portrait none holds takes a fallback (icon_fallbacks); a
# scroll icon none holds takes its template scroll's.
# ================
def infer_kinds(later_chars, later_items, texts, has_icon=lambda name: True):
	by_code = {row[CHAR_CODE]: row for row in later_chars if len(row) > CHAR_ICON}
	by_family = {}
	for row in later_chars:
		if len(row) > CHAR_ICON:
			by_family.setdefault(family_of(row[CHAR_CODE]), []).append(row)
	chars, scrolls, text_rows = [], [], []
	for kind in INFERRED_KINDS:
		inferred = infer_kind(kind, by_code, by_family, later_items)
		if inferred is None:
			continue
		kind_chars, kind_scrolls = inferred
		undo = [(new, old) for old, new in kind["replace"]]
		for row in kind_chars:
			row[CHAR_ICON] = held_icon(row[CHAR_ICON], icon_fallbacks(kind, by_code, CHAR_ICON), has_icon)
		for row in kind_scrolls:
			row[items.ICON] = held_icon(row[items.ICON], [rewrite([row[items.ICON]], undo)[0]], has_icon)
		keys = {row[CHAR_NAME_KEY] for row in kind_chars}
		keys |= {row[column] for row in kind_scrolls for column in (items.NAME_KEY, ITEM_DESC_KEY)}
		chars += kind_chars
		scrolls += kind_scrolls
		text_rows += name_rows(kind, keys, texts)
	return chars, scrolls, text_rows
