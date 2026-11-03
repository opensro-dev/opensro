/*
===========================================================================

halloween_test.go - the Halloween switch, window and gift table pins

The window boundaries and the gift table are the event's whole tuning
surface; both are pinned here so a retune is a deliberate test change.

===========================================================================
*/
package progression

import (
	"testing"
	"time"
)

/*
================
halloweenWindowEnv

A window straddling the fake "now" the boundary tests use.
================
*/
func halloweenWindowEnv(t *testing.T, enabled, window string) HalloweenEvent {
	t.Helper()
	t.Setenv(EnvHalloweenEvent, enabled)
	t.Setenv(EnvHalloweenEventWindow, window)
	return HalloweenEventFromEnv()
}

/*
================
TestHalloweenEventIsNativeByDefault
================
*/
func TestHalloweenEventIsNativeByDefault(t *testing.T) {
	event := halloweenWindowEnv(t, "", "")
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	if event.Enabled() || event.WindowValid() || event.ActiveAt(now) || event.CleanupDueAt(now) {
		t.Fatal("unset environment must be the native game", event)
	}
	// On without a window never runs, but the posture stays visible.
	event = halloweenWindowEnv(t, "on", "")
	if !event.Enabled() || event.WindowValid() || event.ActiveAt(now) {
		t.Fatal("on without a window must stay inert", event)
	}
	// A backwards window is no window.
	event = halloweenWindowEnv(t, "on", "2026-11-02T00:00:00Z/2026-10-31T00:00:00Z")
	if event.WindowValid() || event.ActiveAt(now) || event.CleanupDueAt(now) {
		t.Fatal("backwards window must be invalid", event)
	}
}

/*
================
TestHalloweenEventWindowBoundaries
================
*/
func TestHalloweenEventWindowBoundaries(t *testing.T) {
	event := halloweenWindowEnv(t, "on", "2026-10-31T00:00:00Z/2026-11-02T00:00:00Z")
	if !event.Enabled() || !event.WindowValid() {
		t.Fatal("valid window not parsed", event)
	}
	start := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	if event.ActiveAt(start.Add(-time.Second)) || !event.ActiveAt(start) ||
		!event.ActiveAt(end.Add(-time.Second)) || event.ActiveAt(end) {
		t.Fatal("activity must be [start, end)")
	}
	if event.CleanupDueAt(end.Add(-time.Second)) || !event.CleanupDueAt(end) {
		t.Fatal("cleanup is due from the end on")
	}
	// The cleanup follows the window even with the switch flipped off:
	// boxes a finished window created still retire.
	off := halloweenWindowEnv(t, "off", "2026-10-31T00:00:00Z/2026-11-02T00:00:00Z")
	if off.ActiveAt(start) || !off.CleanupDueAt(end) {
		t.Fatal("off switch: no activity, cleanup still due after the end")
	}
}

/*
================
TestHalloweenGiftTableIsTheOwnersTable
================
*/
func TestHalloweenGiftTableIsTheOwnersTable(t *testing.T) {
	gifts := HalloweenGifts()
	if len(gifts) != 17 {
		t.Fatalf("gift rows = %d, want the 17 consumables (avatars are not gifts)", len(gifts))
	}
	wantConsumables := []HalloweenGift{
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
	total := 0.0
	for i, want := range wantConsumables {
		if gifts[i] != want {
			t.Fatalf("consumable %d = %+v, want %+v", i, gifts[i], want)
		}
		total += gifts[i].Weight
	}
	if total != 100 {
		t.Fatalf("gift weights sum to %v, want 100", total)
	}
	helmets := HalloweenHelmets()
	if len(helmets) != 8 {
		t.Fatalf("helmet rows = %d, want 8", len(helmets))
	}
	for _, helmet := range helmets {
		if _, ok := HalloweenHelmetByCodename(helmet.Codename); !ok {
			t.Fatalf("helmet %s does not resolve", helmet.Codename)
		}
	}
	if _, ok := HalloweenHelmetByCodename("ITEM_MALL_AVATAR_M_HALLOWEEN"); ok {
		t.Fatal("a mall avatar resolved as an event helmet")
	}
}
