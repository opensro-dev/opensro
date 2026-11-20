"""
===========================================================================

cap110_quest_arenas.py - the two trial arenas of the Blue Berserk chains

Port-only, not native. The Army Test (Chinese) and Military Test (European)
trials of levels 95 and 100 (quests 1_1 and 2_1) run in an instance whose
terrain exists only in the later (VSRO) client: two single, isolated regions
with no neighbours, no region code and no zone name.

	region 22966 (sector x 182, y 89)   the Chinese arena, instance INS_QNO_CH_HWAN_1_1 and _2_1
	region 22454 (sector x 182, y 87)   the European arena, instance INS_QNO_EU_HWAN_1_1 and _2_1

The Alexandria importer (import_vsro_alexandria) seeds its regions from the
city and the monster fields; this module names the arenas as two more seeds,
and the same variant build then copies their terrain, objects, navmesh and
ground textures and enables them in the availability bitmap. They follow the
quest switch: SRO_CAP110_QUESTS with SRO_LEVEL_CAP=110 (and the Alexandria
variant, SRO_ALEXANDRIA, that carries every later-client region). The rituals
need nothing here: they use native regions (the Shaman's house and the
Witch's Lighthouse). Nothing here places a player or a monster.

===========================================================================
"""
import os

# (sectorX, sectorY) of each arena: region id = (sectorY << 8) | sectorX.
CHINESE_ARENA = (182, 89)
EUROPEAN_ARENA = (182, 87)
ARENA_REGIONS = (CHINESE_ARENA, EUROPEAN_ARENA)


# ================
# arena_seeds
#
# The regions to add to the Alexandria import as seeds: both arenas when the
# quests are on (cap110_quest_monsters owns the one switch), none otherwise.
# The import is lazy: cap110_quest_monsters reads import_vsro_alexandria.
# ================
def arena_seeds(env=os.environ):
	from cap110_quest_monsters import enabled
	return set(ARENA_REGIONS) if enabled(env) else set()
