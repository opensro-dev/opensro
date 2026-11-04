/*
===========================================================================

events_test.go - the events file publishes a running event, and only then

The published row follows the same readers the GameWorld runs, in both
settings: off (or without a usable window) nothing is published, and a
configured window carries the box's per-kill chance, the daily cap and
the exchange's rows so the site can state the event's terms.

===========================================================================
*/
package catalogue

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/progression"
)

// halloweenItemIDs resolves the event's codenames the way the exporter's
// itemID does; an unknown codename resolves to 0 and is left out.
var halloweenItemIDs = map[string]uint32{
	progression.HalloweenBoxCodename:                3862,
	"ITEM_ETC_E051123_EVATION_SCROLL":               4001,
	"ITEM_ETC_E051123_HIT_SCROLL":                   4002,
	"ITEM_ETC_E051123_SPEED_UP_SCROLL":              4003,
	"ITEM_ETC_E051123_AGILITY_SCROLL":               4004,
	"ITEM_ETC_E051123_HP500_SCROLL":                 4005,
	"ITEM_ETC_E051123_MP500_SCROLL":                 4006,
	"ITEM_ETC_HP_SPOTION_01":                        4007,
	"ITEM_ETC_MP_SPOTION_01":                        4008,
	"ITEM_ETC_ALL_SPOTION_01":                       4009,
	"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A":    4010,
	"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_SHIELD_A":    4011,
	"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ARMOR_A":     4012,
	"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ACCESSARY_A": 4013,
	"ITEM_ETC_E071030_M_EGHEAD":                     4020,
	"ITEM_ETC_E071030_M_BLUE_EGHEAD":                4021,
	"ITEM_ETC_E071030_M_RED_EGHEAD":                 4022,
	"ITEM_ETC_E071030_M_BLACK_EGHEAD":               4023,
	"ITEM_ETC_E071030_W_EGHEAD":                     4024,
	"ITEM_ETC_E071030_W_BLUE_EGHEAD":                4025,
	"ITEM_ETC_E071030_W_RED_EGHEAD":                 4026,
	"ITEM_ETC_E071030_W_BLACK_EGHEAD":               4027,
}

/*
================
halloweenItemID
================
*/
func halloweenItemID(codename string) uint32 { return halloweenItemIDs[codename] }

/*
================
TestEventsPublishHalloweenOnlyWhileItRuns
================
*/
func TestEventsPublishHalloweenOnlyWhileItRuns(t *testing.T) {
	const window = "2026-11-01T00:00:00Z/2026-11-15T00:00:00Z"
	inside := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	before := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	after := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name       string
		enabled    string
		window     string
		now        time.Time
		wantRows   int
		wantActive bool
	}{
		{name: "off shows nothing", enabled: "off", window: window, now: inside},
		{name: "on without a window shows nothing", enabled: "on", window: "", now: inside},
		{name: "backwards window shows nothing", enabled: "on",
			window: "2026-11-15T00:00:00Z/2026-11-01T00:00:00Z", now: inside},
		{name: "before the window is listed, inactive", enabled: "on", window: window,
			now: before, wantRows: 1},
		{name: "inside the window is active", enabled: "on", window: window,
			now: inside, wantRows: 1, wantActive: true},
		{name: "at the end it is over", enabled: "on", window: window, now: after, wantRows: 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(progression.EnvHalloweenEvent, c.enabled)
			t.Setenv(progression.EnvHalloweenEventWindow, c.window)
			file := BuildEvents(Header{}, halloweenItemID, c.now)
			if len(file.Events) != c.wantRows {
				t.Fatalf("events = %d, want %d", len(file.Events), c.wantRows)
			}
			if c.wantRows == 0 {
				return
			}
			event := file.Events[0]
			if event.ID != "halloween" || event.Active != c.wantActive {
				t.Fatalf("event = %+v, want halloween active=%v", event, c.wantActive)
			}
			if event.StartsAt != "2026-11-01T00:00:00Z" || event.EndsAt != "2026-11-15T00:00:00Z" {
				t.Fatalf("window = %s..%s", event.StartsAt, event.EndsAt)
			}
			if len(event.Drops) != 1 || event.Drops[0].ItemID != 3862 ||
				event.Drops[0].ChancePercent != progression.HalloweenBoxDropChancePercent ||
				event.Drops[0].DailyCap != progression.HalloweenBoxDailyCap {
				t.Fatalf("drops = %+v, want the box at the server's chance and cap", event.Drops)
			}
			// The rolled gifts: 17 rows, weights summing to the roll's 100.
			if len(event.Gifts) != len(progression.HalloweenGifts()) {
				t.Fatalf("gifts = %d, want %d", len(event.Gifts), len(progression.HalloweenGifts()))
			}
			weight := 0.0
			for _, gift := range event.Gifts {
				if gift.ItemID == 0 || gift.BoxPrice != 0 {
					t.Fatalf("gift = %+v, want a resolved rolled row", gift)
				}
				weight += gift.Weight
			}
			if weight != 100 {
				t.Fatalf("gift weights sum to %v, want 100", weight)
			}
			// The flat-price tier: every prize row at its own box price
			// (the helmets at HalloweenHelmetPrice, the Elixirs at theirs).
			if len(event.Prizes) != len(progression.HalloweenPrizes()) {
				t.Fatalf("prizes = %d, want %d", len(event.Prizes), len(progression.HalloweenPrizes()))
			}
			for i, prize := range event.Prizes {
				want := progression.HalloweenPrizes()[i]
				if prize.ItemID == 0 || prize.BoxPrice != int(want.Price) || prize.Codename != want.Codename {
					t.Fatalf("prize %d = %+v, want %s at %d boxes", i, prize, want.Codename, want.Price)
				}
			}
		})
	}
}
