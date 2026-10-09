"""
===========================================================================
loot_class_map.py - the drop class an assigned item belongs to, per source

_RefDropItemAssign rows are [service][item][weight][absolute][class][count].
The generator (#459) asks class_for() which class of a _RefDropClassSel_*
table an item joins, for either backup:

  - Equipment rows author class -1; the item's own class decides. The
    v1.150 client's RefObjCommon class column (degree * 3 + grade - 3,
    so d8 A/B/C are 22/23/24) less one is the class index: every one of
    the 5724 items of the vSRO projection carries exactly that group, in
    the normal and the rare table alike.
  - Consumable rows author a 1-based class; the class tables are 0-based,
    so the index is the authored class less one, as the committed vSRO
    projection holds (HP_POTION_01, class 1, at group 0). Both backups
    author the same way: a magic or attribute stone's class is its degree
    (vSRO 1..12, ISRO-R 1..15) and Reinforce A/B are 1/2. The level rows
    agree: at level 30 Equip rolls index 11 (d4 C) and the stones index 3
    (degree 4). ISRO-R's wider tables (stones 20, Reinforce 3, RareEquip
    60) only add classes v1.150 has no item for; RareEquip never rolls an
    index above 35 at any level.

The level-90 difference (ISRO-R rolls ordinary class 26, d9 C; vSRO class
27, d10 A) is in the class rows, not this mapping: levels 74, 80 and 88 use
classes 23, 25 and 26 in both backups.

Evidence: the probe over SILKROAD_R_SHARD and SRO_VT_SHARD recorded in
#460; the mapping is pinned by test_loot_class_map.py on real items.
===========================================================================
"""

# The tables whose rows author class -1: the item supplies its class.
EQUIPMENT_TABLES = ("Equip", "RareEquip")
# The stone tables, whose class must be the item's degree (param1).
STONE_TABLES = ("Alchemy_MagicStone", "Alchemy_ATTRStone")
# The sources this mapping is proven for.
SOURCES = ("isro", "vsro")
# _RefDropItemAssign's class field for an item-classed (equipment) row.
ITEM_CLASSED = -1
# A v1.150 equipment class column spans degrees 1..12, grades A..C.
MAX_EQUIPMENT_CLASS = 36


# ================
# class_for
#
# The class index an assignment row joins in its table, or None when the
# row cannot be placed (the caller logs it and drops the row; its class's
# probability is never redistributed). item_row is the #458 snapshot row
# [service, codename, weight, absolute, class, count], as a list or the
# snapshot's dict; client_item is the v1.150 client record of that codename.
# ================
def class_for(source, table, item_row, client_item):
	if source not in SOURCES:
		raise ValueError("unknown loot source " + str(source))
	if client_item is None:
		return None
	authored = item_row["class"] if isinstance(item_row, dict) else item_row[4]
	if table in EQUIPMENT_TABLES:
		if authored != ITEM_CLASSED:
			return authored - 1 if authored >= 1 else None
		item_class = client_item["class"]
		if not 1 <= item_class <= MAX_EQUIPMENT_CLASS:
			return None
		return item_class - 1
	if authored < 1:
		return None
	if table in STONE_TABLES and authored != client_item["param1"]:
		# A stone whose authored class disagrees with its v1.150 degree
		# would drop at the wrong levels: refuse it rather than guess.
		return None
	return authored - 1
