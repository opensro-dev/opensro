/*
===========================================================================

sook_christmas.go - So-Ok's Christmas Gift (December 2006): switch,
window, snowfalls, the box drop and the gift table

Port-only, not native. iSRO ran it from 26 December 2006 to 9 January
2007; the v1.150 data still ships its whole client side under
QEV_CH_EVENT_KISAENG_061220 ("Merry Christmas & Happy New Year"): Santa
So-Ok's greeting (talk A: "I will give you diverse gifts if you bring
me gift boxes by defeating monsters"), the exchange row (B) and its
three answers (C granted, D bag full, E no box), the token, the Jewel
Box (ITEM_ETC_E050618_TREASUREBOX, "Let's bring it to So-Ok"), and
Santa So-Ok's model (NPC_CH_EVENT_SANTA_KISAENG1). It runs when
SRO_CHRISTMAS_EVENT is on with a valid window and
SRO_CHRISTMAS_EVENT_MODE is so_ok_2006 (christmas_mode.go); in any
other posture it never runs.

What the server did is not in the data, so these are the owner's rules
(2026-11-16), each configurable where the owner asked for it:

  - An ordinary monster's kill leaves a Jewel Box at
    SRO_CHRISTMAS_SOOK_BOX_CHANCE percent (1.5 by default), one roll
    per kill, outside the shard's drop rates.
  - So-Ok trades one box for one gift rolled on the table below
    (SRO_CHRISTMAS_SOOK_GIFTS overrides the weights).
  - It snows on the winter event's timetable (christmas_snow.go: one
    hour every two, on the even UTC hours, none at 04:00), so every
    Christmas event snows alike; the decoration and the town carols
    stand the whole window.

===========================================================================
*/
package progression

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// EnvSoOkChristmasBoxChance is a kill's chance to leave a Jewel Box,
	// in percent (0 to 100, decimals allowed).
	EnvSoOkChristmasBoxChance = "SRO_CHRISTMAS_SOOK_BOX_CHANCE"
	// EnvSoOkChristmasGifts overrides the gift table: comma-separated
	// CODENAME=WEIGHT pairs, one item each, weights relative.
	EnvSoOkChristmasGifts = "SRO_CHRISTMAS_SOOK_GIFTS"

	// SoOkChristmasBoxCodename is the event's token, native to v1.150.
	// Halloween trades the same box (halloween.go); the two windows
	// decide which exchange answers (action/sook_christmas.go).
	SoOkChristmasBoxCodename = HalloweenBoxCodename

	// SoOkChristmasBoxesPerGift is one gift's price: one box.
	SoOkChristmasBoxesPerGift = 1

	// SoOkChristmasHostCodename is the event's host, the town's event
	// So-ok; SoOkChristmasLookCodename is the Santa So-Ok reference she
	// wears for the window (the later client's event_npc_look.txt row).
	SoOkChristmasHostCodename = "NPC_CH_EVENT_KISAENG1"
	SoOkChristmasLookCodename = "NPC_CH_EVENT_SANTA_KISAENG1"

	// soOkChristmasDefaultBoxChance is the owner's 1.5 %.
	soOkChristmasDefaultBoxChance = 1.5
)

// soOkChristmasGifts is the owner's table (2026-11-16), weights in
// percent. The scrolls are the 2005 anniversary rows (E051123, native
// buff consumables 3/3/13/1); Rudolph is the native mount scroll
// (ITEM_COS_C_RUDOLPH: level 5, a level-75 mount).
//
// Pandora's Box (ITEM_ETC_E060517_MON_GENERATION_BOX, the owner's 3 %)
// opens the v1.188 ten-wave event (action/pandorabox.go); the party
// Monster Summon Scroll (ITEM_ETC_E060526_SUMMON_PARTY_SCROLL_A,
// action/partysummonscroll.go) joined it at the same 3 % (owner,
// 2026-11-16), so the roll scales the table's 103 over 100.
var soOkChristmasGifts = []HalloweenGift{
	{Codename: "ITEM_ETC_E051123_SPEED_UP_SCROLL", Count: 1, Weight: 29},
	{Codename: "ITEM_ETC_E051123_HP500_SCROLL", Count: 1, Weight: 20},
	{Codename: "ITEM_ETC_E051123_MP500_SCROLL", Count: 1, Weight: 20},
	{Codename: "ITEM_ETC_E051123_EVATION_SCROLL", Count: 1, Weight: 12},
	{Codename: "ITEM_ETC_E051123_AGILITY_SCROLL", Count: 1, Weight: 9},
	{Codename: "ITEM_ETC_E051123_HIT_SCROLL", Count: 1, Weight: 5},
	{Codename: "ITEM_ETC_E060526_SUMMON_PARTY_SCROLL_A", Count: 1, Weight: 3},
	{Codename: "ITEM_ETC_E060517_MON_GENERATION_BOX", Count: 1, Weight: 3},
	{Codename: "ITEM_COS_C_RUDOLPH", Count: 1, Weight: 2},
}

/*
================
SoOkChristmas

The event's switch, window, box chance and gift table. The zero value is
off: the native game.
================
*/
type SoOkChristmas struct {
	SeasonalEvent
	// selected is whether the mode names this event, whatever the
	// switch says: a shard that ran it and then switched it off still
	// retires its boxes once the window has ended.
	selected  bool
	boxChance float64
	gifts     []HalloweenGift
	// configErr names a box chance or gift table the environment set but
	// that did not parse; the defaults stand in and the boot log says so.
	configErr error
}

/*
================
SoOkChristmasFromEnv
================
*/
func SoOkChristmasFromEnv() SoOkChristmas {
	event := seasonalEventFromEnv(EnvChristmasEvent, EnvChristmasEventWindow)
	selected := ChristmasModeFromEnv() == ChristmasModeSoOk2006
	if !selected {
		event.enabled = false
	}
	out := SoOkChristmas{SeasonalEvent: event, selected: selected, boxChance: soOkChristmasDefaultBoxChance, gifts: soOkChristmasGifts}
	if raw := strings.TrimSpace(os.Getenv(EnvSoOkChristmasBoxChance)); raw != "" {
		chance, err := strconv.ParseFloat(raw, 64)
		if err != nil || chance < 0 || chance > 100 {
			out.configErr = fmt.Errorf("%s=%q is not a percentage between 0 and 100", EnvSoOkChristmasBoxChance, raw)
		} else {
			out.boxChance = chance
		}
	}
	if raw := strings.TrimSpace(os.Getenv(EnvSoOkChristmasGifts)); raw != "" {
		gifts, err := ParseSoOkChristmasGifts(raw)
		if err != nil {
			out.configErr = err
		} else {
			out.gifts = gifts
		}
	}
	return out
}

/*
================
ParseSoOkChristmasGifts

A gift table from CODENAME=WEIGHT pairs. Every weight must be a positive
number; the codenames are checked against the item data at boot
(SoOkChristmas.WithGifts).
================
*/
func ParseSoOkChristmasGifts(raw string) ([]HalloweenGift, error) {
	var gifts []HalloweenGift
	for _, pair := range strings.Split(raw, ",") {
		codename, weight, ok := strings.Cut(strings.TrimSpace(pair), "=")
		codename = strings.TrimSpace(codename)
		value, err := strconv.ParseFloat(strings.TrimSpace(weight), 64)
		if !ok || codename == "" || err != nil || value <= 0 {
			return nil, fmt.Errorf("%s: %q is not CODENAME=WEIGHT with a positive weight", EnvSoOkChristmasGifts, pair)
		}
		gifts = append(gifts, HalloweenGift{Codename: codename, Count: 1, Weight: value})
	}
	return gifts, nil
}

/*
================
SoOkChristmas.Selected

Whether the mode names this event, switched on or not: what keeps its
party summon scrolls usable after the window (action/partysummonscroll.go).
================
*/
func (e SoOkChristmas) Selected() bool { return e.selected }

/*
================
SoOkChristmas.ConfigErr
================
*/
func (e SoOkChristmas) ConfigErr() error { return e.configErr }

/*
================
SoOkChristmas.BoxChancePercent
================
*/
func (e SoOkChristmas) BoxChancePercent() float64 { return e.boxChance }

/*
================
SoOkChristmas.Gifts

The gift table So-Ok rolls; the default table on a zero value.
================
*/
func (e SoOkChristmas) Gifts() []HalloweenGift {
	if len(e.gifts) == 0 {
		return soOkChristmasGifts
	}
	return e.gifts
}

/*
================
SoOkChristmas.WithGifts

The same event with its gift table replaced (the boot check's fallback
to the default table when a configured codename names no item).
================
*/
func (e SoOkChristmas) WithGifts(gifts []HalloweenGift) SoOkChristmas {
	e.gifts = gifts
	return e
}

/*
================
SoOkChristmasDefaultGifts
================
*/
func SoOkChristmasDefaultGifts() []HalloweenGift { return soOkChristmasGifts }

/*
================
SoOkChristmas.SnowingAt

Inside the window, on the winter event's snowfall timetable
(ChristmasEvent.SnowingAt): one weather rule for every Christmas event.
================
*/
func (e SoOkChristmas) SnowingAt(now time.Time) bool {
	return ChristmasEvent{e.SeasonalEvent}.SnowingAt(now)
}

/*
================
SoOkChristmas.BoxesExpireAt

Whether the Jewel Boxes this event handed out leave at a world entry:
once its configured window has ended (whatever the switch says now), as
Halloween's do. Only in its own mode: the winter event never hands out a
box, so its window says nothing about them. The caller keeps them while
either event that trades them still runs.
================
*/
func (e SoOkChristmas) BoxesExpireAt(now time.Time) bool {
	return e.selected && e.CleanupDueAt(now)
}

/*
================
SeasonalLookCodenames

Every NPC reference a seasonal event dresses an NPC in, for the asset
roster that publishes their models (sro-evidence spawnable-npcs).
================
*/
func SeasonalLookCodenames() []string {
	return []string{SoOkChristmasLookCodename}
}
