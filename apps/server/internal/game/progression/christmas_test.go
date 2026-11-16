/*
===========================================================================

christmas_test.go - the Christmas switch, its window and the socks

===========================================================================
*/
package progression

import (
	"testing"
	"time"
)

/*
================
TestChristmasSocksExpireWheneverTheEventDoesNotRun

Inside its window the event runs and the socks stay; before or after it,
on without a valid window, or off, it does not and the socks leave.
================
*/
func TestChristmasSocksExpireWheneverTheEventDoesNotRun(t *testing.T) {
	inside := time.Date(2026, 12, 24, 12, 0, 0, 0, time.UTC)
	t.Setenv(EnvChristmasEvent, "on")
	t.Setenv(EnvChristmasEventWindow, "2026-12-20T00:00:00Z/2027-01-03T00:00:00Z")
	on := ChristmasEventFromEnv()
	if !on.ActiveAt(inside) || on.SocksExpireAt(inside) {
		t.Fatal("inside the window: the event runs and keeps its socks")
	}
	for _, at := range []time.Time{time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 3, 0, 0, 0, 0, time.UTC)} {
		if on.ActiveAt(at) || !on.SocksExpireAt(at) {
			t.Fatalf("outside the window at %v: the socks must leave", at)
		}
	}
	t.Setenv(EnvChristmasEventWindow, "")
	if windowless := ChristmasEventFromEnv(); windowless.ActiveAt(inside) || !windowless.SocksExpireAt(inside) {
		t.Fatal("on without a window: the event stays off and the socks leave")
	}
	t.Setenv(EnvChristmasEvent, "off")
	t.Setenv(EnvChristmasEventWindow, "2026-12-20T00:00:00Z/2027-01-03T00:00:00Z")
	if off := ChristmasEventFromEnv(); off.ActiveAt(inside) || !off.SocksExpireAt(inside) {
		t.Fatal("off: nothing runs and the socks leave")
	}
}

/*
================
TestChristmasSnowfallsFallOneHourInTwo

Inside the window it snows through each even UTC hour but 04:00, and is
dry through the odd ones; a snowfall stops at its hour's end or the
window's, and the next one is the next even hour that falls.
================
*/
func TestChristmasSnowfallsFallOneHourInTwo(t *testing.T) {
	t.Setenv(EnvChristmasEvent, "on")
	t.Setenv(EnvChristmasEventWindow, "2026-12-20T00:00:00Z/2027-01-03T00:30:00Z")
	e := ChristmasEventFromEnv()
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 12, day, hour, minute, 0, 0, time.UTC) }
	snowing := 0
	for hour := range 24 {
		if e.SnowingAt(at(24, hour, 30)) {
			snowing++
			if hour%2 != 0 || hour == 4 {
				t.Fatalf("snowing at %02d:30", hour)
			}
		}
	}
	if snowing != 11 {
		t.Fatalf("%d snowfalls a day, want 11", snowing)
	}
	if end := e.SnowEndsAt(at(24, 12, 10)); !end.Equal(at(24, 13, 0)) {
		t.Fatalf("the 12:00 snowfall stops at %v", end)
	}
	if end := e.SnowEndsAt(time.Date(2027, 1, 3, 0, 10, 0, 0, time.UTC)); !end.Equal(e.EndsAt()) {
		t.Fatalf("the last snowfall stops at %v, want the window's end", end)
	}
	if next, ok := e.NextSnowfall(at(24, 2, 30)); !ok || !next.Equal(at(24, 6, 0)) {
		t.Fatalf("after 02:30 the next snowfall is %v, want 06:00 (04:00 is quiet)", next)
	}
	if e.SnowingAt(at(19, 12, 0)) || e.SnowingAt(time.Date(2027, 1, 3, 0, 30, 0, 0, time.UTC)) {
		t.Fatal("snow outside the window")
	}
}
