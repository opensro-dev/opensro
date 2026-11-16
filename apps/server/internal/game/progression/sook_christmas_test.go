/*
===========================================================================

sook_christmas_test.go - the Christmas mode, So-Ok's Christmas Gift's
window, snowfalls and configuration

===========================================================================
*/
package progression

import (
	"testing"
	"time"
)

const sookTestWindow = "2026-12-26T00:00:00Z/2027-01-09T00:00:00Z"

/*
================
TestChristmasModePicksOneEvent

Unset or "winter" runs the winter event alone (the posture of every
shard before the mode existed); so_ok_2006 runs the 2006 event alone; an
unknown mode runs neither. Off runs neither in any mode.
================
*/
func TestChristmasModePicksOneEvent(t *testing.T) {
	inside := time.Date(2026, 12, 28, 12, 0, 0, 0, time.UTC)
	t.Setenv(EnvChristmasEvent, "on")
	t.Setenv(EnvChristmasEventWindow, sookTestWindow)
	for _, tc := range []struct {
		mode          string
		winter, sook2 bool
	}{
		{"", true, false},
		{"winter", true, false},
		{"WINTER", true, false},
		{"so_ok_2006", false, true},
		{"santa", false, false},
	} {
		t.Setenv(EnvChristmasEventMode, tc.mode)
		if got := ChristmasEventFromEnv().ActiveAt(inside); got != tc.winter {
			t.Fatalf("mode %q: winter event %v, want %v", tc.mode, got, tc.winter)
		}
		if got := SoOkChristmasFromEnv().ActiveAt(inside); got != tc.sook2 {
			t.Fatalf("mode %q: 2006 event %v, want %v", tc.mode, got, tc.sook2)
		}
	}
	t.Setenv(EnvChristmasEvent, "off")
	t.Setenv(EnvChristmasEventMode, "so_ok_2006")
	if SoOkChristmasFromEnv().ActiveAt(inside) || ChristmasEventFromEnv().ActiveAt(inside) {
		t.Fatal("off: nothing runs")
	}
}

/*
================
TestSoOkChristmasWindowEdges

The event runs from the window's first instant up to, not including,
its last; nothing outside.
================
*/
func TestSoOkChristmasWindowEdges(t *testing.T) {
	t.Setenv(EnvChristmasEvent, "on")
	t.Setenv(EnvChristmasEventMode, "so_ok_2006")
	t.Setenv(EnvChristmasEventWindow, sookTestWindow)
	e := SoOkChristmasFromEnv()
	start := time.Date(2026, 12, 26, 0, 0, 0, 0, time.UTC)
	end := time.Date(2027, 1, 9, 0, 0, 0, 0, time.UTC)
	if e.ActiveAt(start.Add(-time.Nanosecond)) || !e.ActiveAt(start) || !e.ActiveAt(end.Add(-time.Nanosecond)) || e.ActiveAt(end) {
		t.Fatal("window edges")
	}
	if e.BoxesExpireAt(end.Add(-time.Nanosecond)) || !e.BoxesExpireAt(end) {
		t.Fatal("the boxes leave from the window's end")
	}
}

/*
================
TestSoOkChristmasSnowsOnTheWinterTimetable

Every Christmas event snows alike: inside the window So-Ok's snowfalls
are the winter event's, second for second (one hour on each even UTC
hour, none at 04:00); before the window, none.
================
*/
func TestSoOkChristmasSnowsOnTheWinterTimetable(t *testing.T) {
	t.Setenv(EnvChristmasEvent, "on")
	t.Setenv(EnvChristmasEventMode, "so_ok_2006")
	t.Setenv(EnvChristmasEventWindow, sookTestWindow)
	e := SoOkChristmasFromEnv()
	winter := ChristmasEvent{e.SeasonalEvent}
	snowing := time.Duration(0)
	for at := time.Date(2026, 12, 27, 0, 0, 0, 0, time.UTC); at.Before(time.Date(2026, 12, 28, 0, 0, 0, 0, time.UTC)); at = at.Add(time.Minute) {
		if e.SnowingAt(at) != winter.SnowingAt(at) {
			t.Fatalf("at %v the two Christmas events disagree", at)
		}
		if e.SnowingAt(at) {
			snowing += time.Minute
		}
	}
	if snowing != 11*time.Hour {
		t.Fatalf("a day snowed %v, want 11h", snowing)
	}
	hour := time.Date(2026, 12, 27, 14, 0, 0, 0, time.UTC)
	if !e.SnowingAt(hour) || !e.SnowingAt(hour.Add(time.Hour-time.Millisecond)) || e.SnowingAt(hour.Add(time.Hour)) {
		t.Fatal("the snowfall's edges")
	}
	if e.SnowingAt(time.Date(2026, 12, 27, 4, 30, 0, 0, time.UTC)) {
		t.Fatal("snow in the quiet hours")
	}
	if e.SnowingAt(time.Date(2026, 12, 25, 22, 5, 0, 0, time.UTC)) {
		t.Fatal("snow before the window")
	}
}

/*
================
TestSoOkChristmasConfiguration

The box chance and the gift weights read from the environment; a value
that does not parse keeps the default and reports it.
================
*/
func TestSoOkChristmasConfiguration(t *testing.T) {
	t.Setenv(EnvChristmasEventMode, "so_ok_2006")
	if e := SoOkChristmasFromEnv(); e.BoxChancePercent() != 1.5 || len(e.Gifts()) != len(soOkChristmasGifts) || e.ConfigErr() != nil {
		t.Fatal("defaults", e.BoxChancePercent(), e.ConfigErr())
	}
	t.Setenv(EnvSoOkChristmasBoxChance, "2.25")
	t.Setenv(EnvSoOkChristmasGifts, "ITEM_ETC_E051123_HIT_SCROLL=3, ITEM_COS_C_RUDOLPH=1")
	e := SoOkChristmasFromEnv()
	if e.BoxChancePercent() != 2.25 || len(e.Gifts()) != 2 || e.Gifts()[1].Codename != "ITEM_COS_C_RUDOLPH" || e.Gifts()[0].Weight != 3 {
		t.Fatal("configured", e.BoxChancePercent(), e.Gifts())
	}
	for _, bad := range []string{"-1", "101", "lots"} {
		t.Setenv(EnvSoOkChristmasBoxChance, bad)
		if e := SoOkChristmasFromEnv(); e.ConfigErr() == nil || e.BoxChancePercent() != 1.5 {
			t.Fatalf("chance %q accepted", bad)
		}
	}
	t.Setenv(EnvSoOkChristmasBoxChance, "")
	for _, bad := range []string{"ITEM_X", "ITEM_X=0", "=4", "ITEM_X=two"} {
		t.Setenv(EnvSoOkChristmasGifts, bad)
		if e := SoOkChristmasFromEnv(); e.ConfigErr() == nil || len(e.Gifts()) != len(soOkChristmasGifts) {
			t.Fatalf("gifts %q accepted", bad)
		}
	}
}

/*
================
TestSoOkChristmasGiftsAreUsable

Every default gift is one item at a positive weight, Pandora's Box and
the party summon scroll among them (both opened by the port).
================
*/
func TestSoOkChristmasGiftsAreUsable(t *testing.T) {
	found := map[string]bool{}
	for _, gift := range SoOkChristmasDefaultGifts() {
		found[gift.Codename] = true
		if gift.Count != 1 || gift.Weight <= 0 {
			t.Fatalf("%s: one item at a positive weight", gift.Codename)
		}
	}
	if !found["ITEM_ETC_E060517_MON_GENERATION_BOX"] || !found["ITEM_ETC_E060526_SUMMON_PARTY_SCROLL_A"] {
		t.Fatal("Pandora's Box and the party summon scroll are So-Ok's gifts")
	}
}
