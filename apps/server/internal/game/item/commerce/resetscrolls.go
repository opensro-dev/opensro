/*
===========================================================================

resetscrolls.go - the reset scrolls: identity, switches and listing

Port-only, not native (owner, 2026-11-21). Two Item Mall scrolls at 690
silk each, each behind its own switch (default off):

  - the all-levels stat recall (ITEM_MALL_STATS_POINT_RECALL_ALL): the
    native stat recall family (progression/statrecall.go) at any level;
  - the skill reset (ITEM_MALL_SKILL_POINT_RECALL_A): every skill and
    mastery point back (progression/skillrecall.go).

v1.150 has neither item: the variant build stages the v1.696 rows while
their switch is on (scripts/reset_scroll_import.py). Off is the native
game: the item is not in the data and nothing lists it. A switch on
without its item in the data lists nothing (sro-gameworld
wiring_resetscrolls.go).

This package owns the table because both the mall catalogue (here) and
the action runtime read it, and action already imports commerce.

===========================================================================
*/
package commerce

import (
	"os"
	"strings"
)

// The scrolls' switches. Off (the default) is the native game.
const (
	EnvStatResetScroll  = "SRO_STAT_RESET_SCROLL"
	EnvSkillResetScroll = "SRO_SKILL_RESET_SCROLL"
)

// StatResetScrollItem is the all-levels stat recall: StatRecallListed
// leaves it out, since it has no level window to hold.
const StatResetScrollItem = "ITEM_MALL_STATS_POINT_RECALL_ALL"

// ResetScrollKind is what a scroll resets.
type ResetScrollKind uint8

const (
	StatReset ResetScrollKind = iota
	SkillReset
)

/*
================
ResetScroll

One scroll row: what it resets, its switch, its item and its Silk price
under the authored package MallPackage (mallextra.go authorMallPackage).
================
*/
type ResetScroll struct {
	Kind        ResetScrollKind
	Env         string
	Codename    string
	MallPackage string
	Silk        uint32
}

// resetScrollMallTab is the mall's scroll tab, beside the tier recalls.
const resetScrollMallTab = "MALL_CONSUME_SCROLL"

// ResetScrolls is every scroll the feature sells (owner's prices,
// 2026-11-21).
var ResetScrolls = []ResetScroll{
	{Kind: StatReset, Env: EnvStatResetScroll, Codename: StatResetScrollItem,
		MallPackage: "PACKAGE_ITEM_MALL_STATS_POINT_RECALL_ALL", Silk: 690},
	{Kind: SkillReset, Env: EnvSkillResetScroll, Codename: "ITEM_MALL_SKILL_POINT_RECALL_A",
		MallPackage: "PACKAGE_ITEM_MALL_SKILL_POINT_RECALL_A", Silk: 690},
}

/*
================
ResetScrollEnabledFromEnv

A scroll's switch accepts "1", "on" and "true" (NameChangeScrollFromEnv's
idiom); anything else, including unset, is off.
================
*/
func ResetScrollEnabledFromEnv(scroll ResetScroll) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(scroll.Env))) {
	case "1", "on", "true":
		return true
	}
	return false
}

/*
================
ResetScrollByCodename

The scroll row an item is, if it is one.
================
*/
func ResetScrollByCodename(codename string) (ResetScroll, bool) {
	for _, scroll := range ResetScrolls {
		if scroll.Codename == codename {
			return scroll, true
		}
	}
	return ResetScroll{}, false
}

/*
================
ResetScrollMallGoods

The enabled scrolls as authored Item Mall packages on the scroll tab,
appended to SRO_MALL_EXTRA_GOODS's listings, in table order.
================
*/
func ResetScrollMallGoods(enabled map[ResetScrollKind]bool) []MallExtraGood {
	goods := make([]MallExtraGood, 0, len(ResetScrolls))
	for _, scroll := range ResetScrolls {
		if enabled[scroll.Kind] {
			goods = append(goods, MallExtraGood{Tab: resetScrollMallTab, Package: scroll.MallPackage,
				Item: scroll.Codename, Silk: scroll.Silk})
		}
	}
	return goods
}
