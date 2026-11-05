/*
===========================================================================

halloween.go - the Halloween Day event's switch, window and gift table

Port-only, not native: the v1.150 client data carries the event's talk
lines and its jewelry box, but no running event, so the event is rebuilt
behind SRO_HALLOWEEN_EVENT. Off (or without a valid window) nothing of it
exists: no box drops, no talk option, no cleanup. The window is
configuration, read once at boot; both ends are UTC.

The sibling event quests that still survive on the reference side fix the
exchange's shape: one weighted roll, the first row whose running sum is
not below the roll pays. The rows and weights are the owner's event table
(#2131), port-only like the rest of the event. Above that rolled tier sits
the flat-price prize list the 2008 notice's list cannot give: the exact
row chosen at its stated price — the eight event helmets, and (owner,
2026-11-04, #2548) the four Elixir A rows a capped character can use.
How many boxes one rolled gift costs is an inference
(HalloweenBoxesPerGift): the notice and talk line B read as one box,
neither states a count. The daily box cap, its 04:00 UTC reset and the
helmet seal are the owner's event rules (2026-10-31), all port-only.

===========================================================================
*/
package progression

import (
	"os"
	"strings"
	"time"
)

// EnvHalloweenEvent turns the event on ("on", "1", "true"); the window
// comes from EnvHalloweenEventWindow. Off is the shipped game.
const EnvHalloweenEvent = "SRO_HALLOWEEN_EVENT"

// EnvHalloweenEventWindow is the event's UTC window, "START/END" in
// RFC3339 (for example 2026-10-31T00:00:00Z/2026-11-02T00:00:00Z).
const EnvHalloweenEventWindow = "SRO_HALLOWEEN_EVENT_WINDOW"

// HalloweenBoxCodename is the event's drop and exchange token (itemdata
// row 3862, "Jewel Box"). Its authored row already refuses the exchange
// table, stalls and the warehouse (CanTrade 0, CanBorrow 0), so no
// limit code exists for it anywhere else.
const HalloweenBoxCodename = "ITEM_ETC_E050618_TREASUREBOX"

// HalloweenBoxesPerGift is how many boxes one exchange consumes.
// INFERENCE: both surviving sibling exchanges take ten tokens, while the
// 2008 notice and So-ok's talk line B read as one box per gift and state
// no count. One, until the owner rules otherwise.
const HalloweenBoxesPerGift = 1

/*
================
HalloweenEvent

The zero value is off and windowless: the native game.
================
*/
type HalloweenEvent struct {
	enabled  bool
	startsAt time.Time
	endsAt   time.Time
}

/*
================
HalloweenEventFromEnv
================
*/
func HalloweenEventFromEnv() HalloweenEvent {
	event := HalloweenEvent{}
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvHalloweenEvent))) {
	case "on", "1", "true":
		event.enabled = true
	}
	window := strings.Split(strings.TrimSpace(os.Getenv(EnvHalloweenEventWindow)), "/")
	if len(window) == 2 {
		startsAt, startErr := time.Parse(time.RFC3339, strings.TrimSpace(window[0]))
		endsAt, endErr := time.Parse(time.RFC3339, strings.TrimSpace(window[1]))
		if startErr == nil && endErr == nil && endsAt.After(startsAt) {
			event.startsAt, event.endsAt = startsAt.UTC(), endsAt.UTC()
		}
	}
	return event
}

/*
================
HalloweenEvent.Enabled

Whether the switch is on, whatever the window says.
================
*/
func (e HalloweenEvent) Enabled() bool { return e.enabled }

/*
================
HalloweenEvent.WindowValid

Whether a usable window is configured. An event switched on without one
never runs; the composition root warns about that posture at boot.
================
*/
func (e HalloweenEvent) WindowValid() bool { return !e.startsAt.IsZero() && e.endsAt.After(e.startsAt) }

/*
================
HalloweenEvent.Window

The configured window, and whether it is usable.
================
*/
func (e HalloweenEvent) Window() (startsAt, endsAt time.Time, valid bool) {
	return e.startsAt, e.endsAt, e.WindowValid()
}

/*
================
HalloweenEvent.ActiveAt

The event runs from the window's start up to (not including) its end.
================
*/
func (e HalloweenEvent) ActiveAt(now time.Time) bool {
	return e.enabled && e.WindowValid() && !now.Before(e.startsAt) && now.Before(e.endsAt)
}

/*
================
HalloweenEvent.EndsAt

The window's end, the first instant the event no longer runs; zero when
no window is configured.
================
*/
func (e HalloweenEvent) EndsAt() time.Time { return e.endsAt }

/*
================
HalloweenEvent.CleanupDueAt

Once a configured window has ended, remaining boxes leave at the first
world entry after the end. This follows the window alone: a shard that
ran the event and later flips the switch off still retires the boxes its
window created. With no window configured nothing is ever due.
================
*/
func (e HalloweenEvent) CleanupDueAt(now time.Time) bool {
	return e.WindowValid() && !now.Before(e.endsAt)
}

/*
================
HalloweenGift

One row of the exchange's rolled gift table: what it pays, how many,
and its weight on the sibling roll's scale.
================
*/
type HalloweenGift struct {
	Codename string
	Count    uint32
	Weight   float64
}

// The owner's table (#2131), weights out of 100: buff and 500 scrolls
// singly, grains in stacks of twenty, the buff scrolls in threes, and one
// rare band of the four plain Elixirs at 4.5% in all. The band sits on
// the rows monsters already drop, and the daily box cap bounds it.
var halloweenConsumableGifts = []HalloweenGift{
	{Codename: "ITEM_ETC_E051123_EVATION_SCROLL", Count: 1, Weight: 8},
	{Codename: "ITEM_ETC_E051123_HIT_SCROLL", Count: 1, Weight: 8},
	{Codename: "ITEM_ETC_E051123_SPEED_UP_SCROLL", Count: 1, Weight: 8},
	{Codename: "ITEM_ETC_E051123_AGILITY_SCROLL", Count: 1, Weight: 8},
	{Codename: "ITEM_ETC_E051123_HP500_SCROLL", Count: 1, Weight: 12},
	{Codename: "ITEM_ETC_E051123_MP500_SCROLL", Count: 1, Weight: 12},
	{Codename: "ITEM_ETC_HP_SPOTION_01", Count: 20, Weight: 7},
	{Codename: "ITEM_ETC_MP_SPOTION_01", Count: 20, Weight: 7},
	{Codename: "ITEM_ETC_ALL_SPOTION_01", Count: 20, Weight: 10},
	{Codename: "ITEM_ETC_E051123_EVATION_SCROLL", Count: 3, Weight: 3.875},
	{Codename: "ITEM_ETC_E051123_HIT_SCROLL", Count: 3, Weight: 3.875},
	{Codename: "ITEM_ETC_E051123_SPEED_UP_SCROLL", Count: 3, Weight: 3.875},
	{Codename: "ITEM_ETC_E051123_AGILITY_SCROLL", Count: 3, Weight: 3.875},
	{Codename: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A", Count: 1, Weight: 1.125},
	{Codename: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_SHIELD_A", Count: 1, Weight: 1.125},
	{Codename: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ARMOR_A", Count: 1, Weight: 1.125},
	{Codename: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ACCESSARY_A", Count: 1, Weight: 1.125},
}

/*
================
HalloweenGifts

The rolled tier of the exchange. The table's own total is the roll's
range. The mall avatar rows are not gifts: they stay buyable with silk,
and an event prize silk can buy is not a prize.
================
*/
func HalloweenGifts() []HalloweenGift {
	return halloweenConsumableGifts
}

// The tuning set, read together and retuned together. Set from the live
// KillStats read of 2026-10-31 (seven full UTC days, economy rollup):
// band 81-90 settles 36-43 kills per registered-character hour and about
// 175-185 ordinary kills land per population online-hour, so the cap
// binds after roughly two online hours and the helmet price is three
// days at the cap. The owner may retune all three in this one place:
//
//   - HalloweenBoxDropChancePercent: an ordinary kill's chance to drop
//     one box while the event runs (a Gourd kill always pays its box).
//   - HalloweenBoxDailyCap: boxes one character may mint per day, every
//     source counted; the day resets at 04:00 UTC (HalloweenDayKey).
//   - HalloweenHelmetPrice: boxes one helmet costs, about three days at
//     the cap, so a regular can finish all four colours inside the run.
//   - HalloweenElixirPrice: boxes one Elixir costs, half a day at the
//     cap (owner, 2026-11-04, #2548): the prize a capped main buys
//     first, priced so a regular can take one home the same day.
const (
	HalloweenBoxDropChancePercent = 5
	HalloweenBoxDailyCap          = 20
	HalloweenHelmetPrice          = 60
	HalloweenElixirPrice          = 10
)

/*
================
HalloweenDayKey

The cap's day: a count belongs to the 04:00-UTC day containing now, so
the reset lands in the shard's quiet hour and never inside an evening
wave. Days since the epoch, shifted by the reset hour.
================
*/
func HalloweenDayKey(now time.Time) int64 {
	return (now.Unix() - 4*60*60) / (24 * 60 * 60)
}

/*
================
HalloweenPrize

One row of the flat-price tier So-ok sells above the rolled gifts: the
exact item a player picks and its price in boxes. Helmet rows add the
sex they are made for and sell only to it; every other row sells to
anyone. The Elixir rows are the four A recipes the rolled table can
already pay at 4.5% in all — sold, not rolled, at half a day of boxes
(#2548). No B row: those already sit in the alchemy shop for gold.
================
*/
type HalloweenPrize struct {
	Codename string
	Price    uint32
	Female   bool
	Helmet   bool
}

var halloweenPrizes = []HalloweenPrize{
	{Codename: "ITEM_ETC_E071030_M_EGHEAD", Price: HalloweenHelmetPrice, Helmet: true},
	{Codename: "ITEM_ETC_E071030_M_BLUE_EGHEAD", Price: HalloweenHelmetPrice, Helmet: true},
	{Codename: "ITEM_ETC_E071030_M_RED_EGHEAD", Price: HalloweenHelmetPrice, Helmet: true},
	{Codename: "ITEM_ETC_E071030_M_BLACK_EGHEAD", Price: HalloweenHelmetPrice, Helmet: true},
	{Codename: "ITEM_ETC_E071030_W_EGHEAD", Price: HalloweenHelmetPrice, Female: true, Helmet: true},
	{Codename: "ITEM_ETC_E071030_W_BLUE_EGHEAD", Price: HalloweenHelmetPrice, Female: true, Helmet: true},
	{Codename: "ITEM_ETC_E071030_W_RED_EGHEAD", Price: HalloweenHelmetPrice, Female: true, Helmet: true},
	{Codename: "ITEM_ETC_E071030_W_BLACK_EGHEAD", Price: HalloweenHelmetPrice, Female: true, Helmet: true},
	{Codename: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A", Price: HalloweenElixirPrice},
	{Codename: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_SHIELD_A", Price: HalloweenElixirPrice},
	{Codename: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ARMOR_A", Price: HalloweenElixirPrice},
	{Codename: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ACCESSARY_A", Price: HalloweenElixirPrice},
}

/*
================
HalloweenPrizes

Every flat-price row, in talk-list order: the helmets first (plain,
blue, red, black per sex, as they have always listed), then the Elixirs.
================
*/
func HalloweenPrizes() []HalloweenPrize {
	return halloweenPrizes
}

/*
================
HalloweenPrizeByCodename
================
*/
func HalloweenPrizeByCodename(codename string) (HalloweenPrize, bool) {
	for _, prize := range halloweenPrizes {
		if prize.Codename == codename {
			return prize, true
		}
	}
	return HalloweenPrize{}, false
}

// EnvHalloweenHelmetSeal seals the eight event helmets at the exits that
// hand a helmet to another player (port-only, not native): the exchange
// window, the player stall, storage, the pet bag and the ground drop stay
// closed. The exit to an NPC merchant stays open: the sale destroys the
// helmet, and the item classes a helmet row carries keep it off the
// buyback bench, so it can never reach another player. Sealed at every
// door its own switch, separate from the event's, because it stays on
// after the event ends: the rows are natively tradeable (CanTrade 1),
// and a helmet the event paid was never meant to reach a market. Off is
// the native row behaviour.
const EnvHalloweenHelmetSeal = "SRO_HALLOWEEN_HELMET_SEAL"

/*
================
HalloweenHelmetSeal

The zero value is off: the helmets move like any item.
================
*/
type HalloweenHelmetSeal struct {
	enabled bool
}

/*
================
HalloweenHelmetSealFromEnv
================
*/
func HalloweenHelmetSealFromEnv() HalloweenHelmetSeal {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvHalloweenHelmetSeal))) {
	case "on", "1", "true":
		return HalloweenHelmetSeal{enabled: true}
	}
	return HalloweenHelmetSeal{}
}

/*
================
HalloweenHelmetSeal.Enabled
================
*/
func (s HalloweenHelmetSeal) Enabled() bool { return s.enabled }

/*
================
HalloweenHelmetSeal.Sealed

Whether this codename is one of the eight helmets while the seal is on.
Only the prize list's helmet rows seal; its Elixir rows never do.
================
*/
func (s HalloweenHelmetSeal) Sealed(codename string) bool {
	if !s.enabled {
		return false
	}
	prize, known := HalloweenPrizeByCodename(codename)
	return known && prize.Helmet
}
