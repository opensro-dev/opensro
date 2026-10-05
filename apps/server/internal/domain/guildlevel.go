/*
===========================================================================

guildlevel.go - the guild level rules the v1.150 client carries

A guild climbs from level 1 to 5 at a guild manager NPC. The client prints
the price of the next level from two tables (CIFGuildLevelUp 5EF9A0: GP at
0xBE4C58, gold at 0xBE4C6C, both by the current level) and the member cap
from a third (0xBE4490, by level, in the member pane 5E2D76). v1.188's
level-up admission (5C6240) carries the same prices at 0xADE910 (gold)
and 0xADE8EC (GP).

===========================================================================
*/

package domain

// GuildMaxLevel is the last level the level-up window offers (5EF9A0
// clamps the next level at 5).
const GuildMaxLevel uint8 = 5

// GuildLevelUpCost is the price of leaving one level for the next.
type GuildLevelUpCost struct {
	GP   uint32
	Gold int64
}

// guildLevelUpCosts are 0xBE4C58 and 0xBE4C6C by the current level.
var guildLevelUpCosts = [GuildMaxLevel]GuildLevelUpCost{
	1: {GP: 5400, Gold: 3000000},
	2: {GP: 50400, Gold: 9000000},
	3: {GP: 135000, Gold: 15000000},
	4: {GP: 378000, Gold: 21000000},
}

// guildStorageCapacity is v1.188 0xC6B5F0 (5C4BA0) by level: the warehouse
// opens at level 2 with one page of 30.
var guildStorageCapacity = [GuildMaxLevel + 1]int64{2: 30, 3: 60, 4: 90, 5: 120}

// GuildStorageMinLevel is 5C7440's level test (0x4A below it).
const GuildStorageMinLevel uint8 = 2

// guildMemberCapacity is 0xBE4490 by level.
var guildMemberCapacity = [GuildMaxLevel + 1]int{1: 15, 2: 20, 3: 25, 4: 35, 5: 50}

/*
================
GuildLevelUpCostAt

The price of the next level, or false at the last level.
================
*/
func GuildLevelUpCostAt(level uint8) (GuildLevelUpCost, bool) {
	if level < 1 || level >= GuildMaxLevel {
		return GuildLevelUpCost{}, false
	}
	return guildLevelUpCosts[level], true
}

/*
================
GuildMemberCapacity

The member cap of a level; a level outside 1..5 reads as the nearest.
================
*/
func GuildMemberCapacity(level uint8) int {
	return guildMemberCapacity[min(max(level, 1), GuildMaxLevel)]
}

/*
================
GuildStorageCapacity

The warehouse slots of a level; none below level 2.
================
*/
func GuildStorageCapacity(level uint8) int64 {
	return guildStorageCapacity[min(level, GuildMaxLevel)]
}
