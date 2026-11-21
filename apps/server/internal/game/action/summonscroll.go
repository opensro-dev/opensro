/*
===========================================================================

summonscroll.go - the Item Mall Monster Summon Scrolls (1, 2 and 5 people
use): a powerfully dangerous monster to hunt, sized by the scroll

Port-only, not native (owner ask 2026-11-12, issue #3257): the later
mall sells three summon scrolls v1.150's mall never listed -
ITEM_ETC_E050729_SUMMON_ONE_SCROLL (1인용, "1 person use"),
..._TWO_SCROLL (2인용, "2 people use") and ..._SUMMON_SCROLL ("5 people
use"). The items ship in v1.150's data as 3/3/9/0 quest tools with no
associated character and Param1 -1, required level 10, and no source
names a handler for them. revtool, 2026-11-21: neither the v1.150 client
(375e8682) nor the v1.696 client holds an E050729 string, and the later
server's quest-event item table registers only the E060526 party scroll
and Pandora's Box; the ISRO-R export lists only the codenames.
Behind SRO_MALL_CATEGORIES (default
off) the mall lists them in Premium's Others tab
(commerce/mallcategories.go) and this file serves their use; off, the
items keep the native game - the quest-tool family has no handler for
them and the mall lists nothing.

The item's own text is the rule (SN_..._TT_DESC): "If you use this a
powerfully dangerous monster will appear. The summoned monster will
disappear after 5 minutes. Eligible Levels: Lvl.10 and up". #3304 first
authored them as guild soldiers, which summoned no monster at all
(#4030). Inference, recorded here: the scrolls are the party summon
scroll's event (partysummonscroll.go, the v1.188 rule) at a fixed head
count instead of the party's. The v1.188 party scroll gives a lone user
exactly one grade-7 strong monster, so one person's share is one such
monster: the 1, 2 and 5 people scrolls land 1, 2 and 5 of them, the
strong table's kind at the user's level, 300 s, no owner. The refusals
are that event's (town, arena) with the item's level 10 in place of the
party scroll's 30; a user from 10 to 20 gets the table's lowest row
(key 21), as Pandora's Box pads below its table.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
)

// The three scrolls and the monsters each one calls: its "N people use".
// Admission is by codename (itemuse_admission.go): the shipped 3/3/9/0
// type is the quest tools', and only these three rows leave it.
const (
	SummonScrollItem    = "ITEM_ETC_E050729_SUMMON_SCROLL"
	SummonScrollOneItem = "ITEM_ETC_E050729_SUMMON_ONE_SCROLL"
	SummonScrollTwoItem = "ITEM_ETC_E050729_SUMMON_TWO_SCROLL"
)

var summonScrollCounts = map[string]int{
	SummonScrollItem:    5,
	SummonScrollOneItem: 1,
	SummonScrollTwoItem: 2,
}

// summonScrollMinLevel is the scrolls' required level (itemdata ReqLevel1
// 10, and "Eligible Levels: Lvl.10 and up").
const summonScrollMinLevel = 10

// The scrolls' notices. Port-only: the level line is the party scroll's
// at the item's own level.
const (
	summonScrollLevelLine = "Monster summon scroll can be used if your character is over Lv. 10."
	summonScrollManyLine  = "Summoned monsters have appeared. They will disappear in 5 minutes."
)

/*
================
SummonScrollCount

The monsters a scroll codename calls; zero for any other item.
================
*/
func SummonScrollCount(codename string) int {
	return summonScrollCounts[codename]
}

/*
================
useSummonScroll

The summon event's refusals answer first and spend nothing; then the
scroll's count of strong monsters lands around the user and the scroll
is spent. The flag is re-checked here so a scroll bought while the
categories stood open is dead stock once they close.
================
*/
func (rt *Runtime) useSummonScroll(use persistentSummonUse, result *OpResult) bool {
	c := use.character
	if on, err := commerce.MallCategoriesFromEnv(); err != nil || !on {
		result.DiagnosticRefusal = "item-use: summon scrolls are off (" + commerce.EnvMallCategories + ")"
		return false
	}
	if rt.Monsters == nil || !enterworld.CharacterAlive(c) || c.Level == nil {
		result.DiagnosticRefusal = "item-use: summon scroll owner unavailable"
		return false
	}
	if _, ok := rt.summonEventRefusal(use, summonScrollMinLevel, summonScrollLevelLine, result); !ok {
		return false
	}
	kind, found := strongSummonKind(*c.Level, rt.monsterHeld)
	if !found {
		result.Frames = append(result.Frames, exchangeBanner(partySummonFailedLine))
		return false
	}
	return rt.landStrongSummon(use, strongSummon{kind: kind, count: SummonScrollCount(use.ref.Codename),
		one: partySummonSoloLine, many: summonScrollManyLine}, result)
}
