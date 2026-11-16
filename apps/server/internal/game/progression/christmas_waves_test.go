/*
===========================================================================

christmas_waves_test.go - the winter wave timetable

===========================================================================
*/
package progression

import (
	"testing"
	"time"
)

/*
================
christmasTestEvent

On, its window the whole of 2026-12-24 and -25 UTC.
================
*/
func christmasTestEvent(t *testing.T, on string) ChristmasEvent {
	t.Helper()
	t.Setenv(EnvChristmasEvent, on)
	t.Setenv(EnvChristmasEventWindow, "2026-12-24T00:00:00Z/2026-12-26T00:00:00Z")
	return ChristmasEventFromEnv()
}

/*
================
TestChristmasDayHasKnightHoursAndTwoPrincesses

A full day: White Knights at half past every open hour, the princess at
13:00 and 21:00, nothing in the 03:00-06:00 quiet hours, and every wave
warned before it lands.
================
*/
func TestChristmasDayHasKnightHoursAndTwoPrincesses(t *testing.T) {
	event := christmasTestEvent(t, "on")
	day := time.Date(2026, 12, 24, 0, 0, 0, 0, time.UTC)
	moments := event.WaveMomentsBetween(day.Add(-time.Second), day.Add(24*time.Hour-time.Second))
	knightHours, princesses, warnings := 0, 0, 0
	for _, moment := range moments {
		if moment.Warning {
			warnings++
			continue
		}
		hour := moment.Wave.At.Hour()
		if hour >= 3 && hour < 6 {
			t.Fatalf("a wave landed in the quiet hours: %v", moment.Wave.At)
		}
		switch moment.Wave.Kind {
		case ChristmasKnights:
			if moment.Wave.At.Minute() != 30 {
				t.Fatalf("knights at %v, want half past", moment.Wave.At)
			}
			knightHours++
		case ChristmasPrincess:
			if hour != 13 && hour != 21 || moment.Wave.At.Minute() != 0 {
				t.Fatalf("a princess at %v", moment.Wave.At)
			}
			princesses++
		}
	}
	if knightHours != 21 || princesses != 2 {
		t.Fatalf("knightHours %d, princesses %d; want 21 and 2", knightHours, princesses)
	}
	if warnings != knightHours+princesses {
		t.Fatalf("warnings %d", warnings)
	}
}

/*
================
TestChristmasWavesRotateFields

Two waves running never land in the same field trio.
================
*/
func TestChristmasWavesRotateFields(t *testing.T) {
	event := christmasTestEvent(t, "on")
	day := time.Date(2026, 12, 24, 0, 0, 0, 0, time.UTC)
	var previous string
	for _, moment := range event.WaveMomentsBetween(day, day.Add(24*time.Hour)) {
		if moment.Warning {
			continue
		}
		field := moment.Wave.Fields[0].Codename
		if field == previous {
			t.Fatalf("two waves running in %s (%v)", field, moment.Wave.At)
		}
		previous = field
	}
}

/*
================
TestChristmasWavesOffOrOutsideTheWindow
================
*/
func TestChristmasWavesOffOrOutsideTheWindow(t *testing.T) {
	off := christmasTestEvent(t, "off")
	day := time.Date(2026, 12, 24, 0, 0, 0, 0, time.UTC)
	if moments := off.WaveMomentsBetween(day, day.Add(24*time.Hour)); len(moments) != 0 {
		t.Fatalf("event off: %d moments", len(moments))
	}
	on := christmasTestEvent(t, "on")
	after := time.Date(2026, 12, 26, 0, 0, 0, 0, time.UTC)
	if moments := on.WaveMomentsBetween(after, after.Add(48*time.Hour)); len(moments) != 0 {
		t.Fatalf("after the window: %d moments", len(moments))
	}
	if _, ok := on.NextWave(time.Date(2026, 12, 24, 13, 30, 0, 0, time.UTC), ChristmasPrincess); !ok {
		t.Fatal("no next princess inside the window")
	}
}

/*
================
TestChristmasWinterLevel

A field picks the highest winter row at or below its level; a field
below every row meets the lowest.
================
*/
func TestChristmasWinterLevel(t *testing.T) {
	for _, tc := range []struct{ field, want int }{{1, 20}, {15, 20}, {23, 20}, {24, 24}, {65, 60}, {88, 80}, {99, 90}, {200, 110}} {
		if got := ChristmasWinterLevel(tc.field); got != tc.want {
			t.Fatalf("field Lv %d: %d, want %d", tc.field, got, tc.want)
		}
	}
	if got := ChristmasKnightCodename(65); got != "MOB_EV_WINTER_WHITEKNIGHT_60" {
		t.Fatalf("knight %s", got)
	}
	if got := ChristmasPrincessCodename(15); got != "MOB_EV_WINTER_PRINCESS_20" {
		t.Fatalf("princess %s", got)
	}
}
