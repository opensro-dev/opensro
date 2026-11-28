"""
===========================================================================

pet_rows.py - what a COS kind is, and what summons it

The facts every pet module shares about a later client's COS rows, in
one place so pet_import (selection and staging), pet_inference (rows for
models no client gives a row) and pet_fellows (the v1.696 Fellow pets on
the growth lane) agree on them:

	family_of     a kind is its codename family: COS_P_WOLF_001 and
	              COS_P_WOLF_140 are one wolf
	scroll_rows   the plain summon scrolls that name a kind (column 119);
	              event and premium copies are rewards or a mall this port
	              does not carry (owner 2026-11-25: no Premium Mall)

The column layouts are their tables' owners': characterdata is
alexandria_npcs', itemdata is cap110_items'.

===========================================================================
"""
import re

import cap110_items as items

LEVEL_SUFFIX = re.compile(r"_\d+$")
# Event copies are rewards and premium copies a mall this port does not
# carry: neither is a kind's scroll.
EVENT_SCROLL_PREFIX = "ITEM_EVENT_"
PREMIUM_SCROLL_PREFIX = "ITEM_PRE_"
UNSOLD_SCROLL_PREFIXES = (EVENT_SCROLL_PREFIX, PREMIUM_SCROLL_PREFIX)


# ================
# family_of
# ================
def family_of(codename):
	return LEVEL_SUFFIX.sub("", codename)


# ================
# scroll_rows
#
# The later item rows that summon one of codes, the plain ones only.
# ================
def scroll_rows(later_items, codes):
	return [
		row for row in later_items
		if len(row) > items.SUMMONED_CHARACTER and row[items.SUMMONED_CHARACTER].strip() in codes
		and not row[items.CODENAME].startswith(UNSOLD_SCROLL_PREFIXES)
	]
