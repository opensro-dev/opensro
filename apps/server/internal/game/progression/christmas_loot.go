/*
===========================================================================

christmas_loot.go - the Red Santa Socks: what the winter monsters leave
and what Santa So-Ok gives for them

Port-only, not native (SRO_CHRISTMAS_EVENT, default off). The v1.150
data ships the December 2007 event whole except its server rules: the
token, Red Santa Socks (ITEM_ETC_E071225_SANTASOCKS, "Red Santa Socks",
described as socks the monsters stole), and its exchange quest
QEV_CH_EVENT_KISAENG_071225 ("If it Snows on Christmas~") whose talk rows
read "Receive present for Red Santa Socks." with a bag-full and a
no-socks answer: the Halloween exchange's exact shape. The inference:
the winter monsters are the thieves, so a Snow Slave carries one pair
and nothing else, the uniques a pile beside their own loot, dropped
outside the level-gap admission as the v1.188 winter event drops its
token (action/christmas_loot.go); their ordinary loot keeps the gap.
So-ok's one row trades each pair for one
rolled present (action/christmas.go), from the owner's list of the
event's presents (2026-11-15): the Santa costume, Rudolph, the Santa
Wings and the 2005 event scrolls.

===========================================================================
*/
package progression

import "strings"

const (
	// ChristmasSockCodename is the event's token, native to v1.150.
	ChristmasSockCodename = "ITEM_ETC_E071225_SANTASOCKS"

	// The Red Santa Socks each winter monster leaves, one ground item
	// each.
	ChristmasSnowSlaveSocks = 1
	ChristmasKnightSocks    = 10
	ChristmasPrincessSocks  = 30

	// ChristmasSocksPerGift is one present's price: one pair (owner,
	// 2026-11-15). The socks stay their native row: no merchant buys
	// them (canSell 0), a stack of ten (MaxStack 10), and no exchange or
	// ground drop (canTrade 0, CanDrop clear), which the sale, trade and
	// drop doors already enforce. So-ok's exchange is their only use, as
	// the v1.188 server's token events trade tokens for presents and
	// never sell them (owner 2026-11-16: unsellable, as native).
	ChristmasSocksPerGift = 1
)

/*
================
ChristmasSocks

The Red Santa Socks a kill of this monster leaves; 0 for any other
monster.
================
*/
func ChristmasSocks(codename string) int {
	switch {
	case codename == ChristmasSnowSlaveCodename:
		return ChristmasSnowSlaveSocks
	case strings.HasPrefix(codename, christmasKnightPrefix):
		return ChristmasKnightSocks
	case strings.HasPrefix(codename, christmasPrincessPrefix):
		return ChristmasPrincessSocks
	}
	return 0
}

/*
================
ChristmasSocksOnly

A Snow Slave carries its socks and nothing else: no gold, no gear.
================
*/
func ChristmasSocksOnly(codename string) bool {
	return codename == ChristmasSnowSlaveCodename
}

// christmasGifts is the present table, weights out of 100: the
// scrolls in threes, the wings in fives, and the costume and Rudolph as
// the rare band. A sexed row names its male item; FemaleCodename is the
// same piece for a female character.
var christmasGifts = []HalloweenGift{
	{Codename: "ITEM_ETC_E051123_EVATION_SCROLL", Count: 3, Weight: 15},
	{Codename: "ITEM_ETC_E051123_AGILITY_SCROLL", Count: 3, Weight: 15},
	{Codename: "ITEM_ETC_E051123_SPEED_UP_SCROLL", Count: 3, Weight: 15},
	{Codename: "ITEM_ETC_E051123_HP500_SCROLL", Count: 3, Weight: 14},
	{Codename: "ITEM_ETC_E051123_MP500_SCROLL", Count: 3, Weight: 14},
	{Codename: "ITEM_ETC_E041225_SANTA_WINGS", Count: 5, Weight: 11},
	{Codename: "ITEM_ETC_E041225_SANTA_HAT_M", FemaleCodename: "ITEM_ETC_E041225_SANTA_HAT_F", Count: 1, Weight: 3},
	{Codename: "ITEM_ETC_E051125_SANTA_HAT_BLUE_M", FemaleCodename: "ITEM_ETC_E051125_SANTA_HAT_BLUE_F", Count: 1, Weight: 3},
	{Codename: "ITEM_ETC_E051125_SANTA_SET_RED_M", FemaleCodename: "ITEM_ETC_E051125_SANTA_SET_RED_F", Count: 1, Weight: 3},
	{Codename: "ITEM_ETC_E051125_SANTA_SET_BLUE_M", FemaleCodename: "ITEM_ETC_E051125_SANTA_SET_BLUE_F", Count: 1, Weight: 3},
	{Codename: "ITEM_COS_C_RUDOLPH", Count: 1, Weight: 4},
}

/*
================
ChristmasGifts

The presents Santa So-Ok rolls.
================
*/
func ChristmasGifts() []HalloweenGift {
	return christmasGifts
}
