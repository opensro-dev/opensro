/*
===========================================================================

winter_summons.go - where the summon missions land their monsters

The v1.188 shard's _RefServerEventReward_SpawnMonster rows: the White
Knight summon Event (30003) lands one MOB_EV_WINTER_WHITEKNIGHT_<level>
on each of 40 fixed points, five per level band, and the Princess summon
Event (30004) one MOB_EV_WINTER_PRINCESS_<level> on the same 40 points
(rewards 0x1EF-0x216 and 0x217-0x23E, world INS_DEFAULT). A point is a region and its local position, height in Y. The
level 100 band's five points lie in dungeon region 0x8004.

===========================================================================
*/
package progression

import "fmt"

/*
================
WinterSummonPoint

One native spawn point and the level band of the monster it takes.
================
*/
type WinterSummonPoint struct {
	Level    int
	RegionID uint16
	X, Y, Z  float64
}

// The summon kinds' codename prefixes; the level band follows.
const (
	winterKnightPrefix   = "MOB_EV_WINTER_WHITEKNIGHT_"
	winterPrincessPrefix = "MOB_EV_WINTER_PRINCESS_"
)

// The summon notices' monster names (the kinds' English names).
const (
	WinterKnightName   = "White Knight"
	WinterPrincessName = "Princess Miyene"
)

// winterSummonPoints are the 40 native points, in reward order.
var winterSummonPoints = []WinterSummonPoint{
	{Level: 20, RegionID: 0x5ea1, X: 971, Y: 695, Z: 143},
	{Level: 20, RegionID: 0x5da2, X: 1435, Y: 1058, Z: 1652},
	{Level: 20, RegionID: 0x5ca0, X: 914, Y: 1369, Z: 924},
	{Level: 20, RegionID: 0x5e9f, X: 1582, Y: 935, Z: 56},
	{Level: 20, RegionID: 0x5f9e, X: 379, Y: 550, Z: 96},
	{Level: 24, RegionID: 0x6346, X: 1570, Y: 0, Z: 1700},
	{Level: 24, RegionID: 0x6447, X: 1578, Y: 1, Z: 1033},
	{Level: 24, RegionID: 0x6347, X: 1047, Y: 172, Z: 347},
	{Level: 24, RegionID: 0x6346, X: 401, Y: 0, Z: 867},
	{Level: 24, RegionID: 0x6446, X: 46, Y: 1, Z: 648},
	{Level: 30, RegionID: 0x6462, X: 1832, Y: 180, Z: 115},
	{Level: 30, RegionID: 0x6562, X: 1743, Y: 180, Z: 914},
	{Level: 30, RegionID: 0x6562, X: 1609, Y: 180, Z: 1008},
	{Level: 30, RegionID: 0x6464, X: 185, Y: 180, Z: 1430},
	{Level: 30, RegionID: 0x6562, X: 135, Y: 179, Z: 964},
	{Level: 40, RegionID: 0x5f94, X: 1207, Y: 1036, Z: 1640},
	{Level: 40, RegionID: 0x5e93, X: 1063, Y: 1121, Z: 1234},
	{Level: 40, RegionID: 0x6092, X: 13, Y: 886, Z: 661},
	{Level: 40, RegionID: 0x5b97, X: 365, Y: 409, Z: 1481},
	{Level: 40, RegionID: 0x5c93, X: 1059, Y: 220, Z: 1590},
	{Level: 60, RegionID: 0x577c, X: 1738, Y: 864, Z: 1729},
	{Level: 60, RegionID: 0x587e, X: 1732, Y: 800, Z: 1405},
	{Level: 60, RegionID: 0x5880, X: 1148, Y: 800, Z: 1605},
	{Level: 60, RegionID: 0x5b80, X: 327, Y: 800, Z: 601},
	{Level: 60, RegionID: 0x5b7e, X: 715, Y: 800, Z: 1627},
	{Level: 80, RegionID: 0x697e, X: 1654, Y: 491, Z: 960},
	{Level: 80, RegionID: 0x697f, X: 216, Y: 599, Z: 59},
	{Level: 80, RegionID: 0x6680, X: 1767, Y: 81, Z: 1647},
	{Level: 80, RegionID: 0x6686, X: 1839, Y: -40, Z: 956},
	{Level: 80, RegionID: 0x6784, X: 1285, Y: -60, Z: 591},
	{Level: 90, RegionID: 0x5c70, X: 1131, Y: 4207, Z: 852},
	{Level: 90, RegionID: 0x5a71, X: 173, Y: 4150, Z: 1131},
	{Level: 90, RegionID: 0x596f, X: 97, Y: 3632, Z: 861},
	{Level: 90, RegionID: 0x5a6d, X: 1072, Y: 3189, Z: 1153},
	{Level: 90, RegionID: 0x5d6f, X: 260, Y: 3845, Z: 990},
	{Level: 100, RegionID: 0x8004, X: 972, Y: -47, Z: -82},
	{Level: 100, RegionID: 0x8004, X: 15771, Y: -45, Z: -20},
	{Level: 100, RegionID: 0x8004, X: -14801, Y: -18, Z: 1489},
	{Level: 100, RegionID: 0x8004, X: 437, Y: -39, Z: -15339},
	{Level: 100, RegionID: 0x8004, X: -69, Y: -10, Z: 15281},
}

/*
================
WinterSummonPoints

The 40 points both summon missions land on.
================
*/
func WinterSummonPoints() []WinterSummonPoint {
	return winterSummonPoints
}

/*
================
WinterSummonCodename

The monster a summon mission lands on a point of this level band; ""
for a mission that summons nothing.
================
*/
func WinterSummonCodename(mission uint32, level int) string {
	switch mission {
	case WinterMissionKnights:
		return fmt.Sprintf("%s%d", winterKnightPrefix, level)
	case WinterMissionPrincesses:
		return fmt.Sprintf("%s%d", winterPrincessPrefix, level)
	}
	return ""
}

/*
================
WinterSummonName

The monster name a summon mission's notice gives; "" for any other.
================
*/
func WinterSummonName(mission uint32) string {
	switch mission {
	case WinterMissionKnights:
		return WinterKnightName
	case WinterMissionPrincesses:
		return WinterPrincessName
	}
	return ""
}
