/*
===========================================================================

guildlevel.go - the guild level rules the v1.150 client carries

A guild climbs from level 1 to 5 at a guild manager NPC. The client prints
the price of the next level from two tables (CIFGuildLevelUp_OnCreate: its
GP table and its gold table, both by the current level) and the member cap
from a third (its capacity table, by level, shown in the member pane by
CIFGuildMember_RefreshFromGuildData). v1.188's
level-up admission carries the same prices in its own gold and GP
tables.

===========================================================================
*/

package domain

// GuildMaxLevel is the last level the level-up window offers
// (CIFGuildLevelUp_OnCreate clamps the next level at 5).
const GuildMaxLevel uint8 = 5

// GuildLevelUpCost is the price of leaving one level for the next.
type GuildLevelUpCost struct {
	GP   uint32
	Gold int64
}

// guildLevelUpCosts are the client's GP and gold tables by the current
// level (CIFGuildLevelUp_OnCreate).
var guildLevelUpCosts = [GuildMaxLevel]GuildLevelUpCost{
	1: {GP: 5400, Gold: 3000000},
	2: {GP: 50400, Gold: 9000000},
	3: {GP: 135000, Gold: 15000000},
	4: {GP: 378000, Gold: 21000000},
}

// guildStorageCapacity is the v1.188 guild storage table by level: the warehouse
// opens at level 2 with one page of 30.
var guildStorageCapacity = [GuildMaxLevel + 1]int64{2: 30, 3: 60, 4: 90, 5: 120}

// GuildStorageMinLevel is the server's guild storage level test (0x4A below it).
const GuildStorageMinLevel uint8 = 2

// guildMemberCapacity is the client's capacity table by level.
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
