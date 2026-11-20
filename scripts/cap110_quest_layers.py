"""
===========================================================================

cap110_quest_layers.py - the Blue Berserk imports, in their load-bearing order

Port-only, not native. build_alexandria_variant calls add_quest_layers once,
after the later sources' layers and before the city services. Each import
below follows its own switch (SRO_CAP110_ITEMS, SRO_CAP110_QUESTS and the
level cap) and does nothing when that switch is off; this file only owns
the order and the manifest keys.

===========================================================================
"""


# ================
# add_quest_layers
#
# The quest items come first (the scrolls and the quest tables name them),
# then the scrolls, the monsters the quests spawn and the quests' own rows.
# Returns the manifest entries, one per import.
# ================
def add_quest_layers(vsro_root, native, out):
	from cap110_quest_items import add_quest_items
	from cap110_blue_berserk import add_blue_berserk
	from cap110_quest_monsters import add_quest_monsters
	from cap110_quest_tables import add_quest_tables
	return {
		"questItems": add_quest_items(vsro_root, native, out),
		"blueBerserk": add_blue_berserk(vsro_root, native, out),
		"questMonsters": add_quest_monsters(vsro_root, native, out),
		"questTables": add_quest_tables(vsro_root, native, out),
	}
