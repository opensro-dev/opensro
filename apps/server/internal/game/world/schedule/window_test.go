/*
===========================================================================

window_test.go - hourly and daily windows open and close on the clock

The weekly case is the Roc's summit (world/roc's tests); these pin the
periods the tomb gate and a daily row use.

===========================================================================
*/
package schedule

import (
	"testing"
	"time"
)

/*
================
TestHourlyWindowOpensForItsDurationEachHour

Ten minutes from one second past each hour: open at :00:01 and :09:59,
closed at :00:00 and :10:01, the next start the following hour.
================
*/
func TestHourlyWindowOpensForItsDurationEachHour(t *testing.T) {
	w := Window{Every: Hourly, StartSecond: 1, Duration: 10 * time.Minute}
	loc := time.UTC
	at := func(h, m, s int) time.Time { return time.Date(2026, 11, 23, h, m, s, 0, loc) }
	cases := []struct {
		t    time.Time
		open bool
	}{
		{at(5, 0, 0), false}, {at(5, 0, 1), true}, {at(5, 9, 59), true}, {at(5, 10, 1), false}, {at(5, 59, 59), false},
	}
	for _, c := range cases {
		if _, _, in := w.Current(c.t, loc); in != c.open {
			t.Fatalf("%s: open %v, want %v", c.t.Format(time.TimeOnly), in, c.open)
		}
	}
	if got, want := w.Next(at(5, 30, 0), loc), at(6, 0, 1); !got.Equal(want) {
		t.Fatalf("next start %v, want %v", got, want)
	}
	// Midnight is no special case: 23:30 waits for 00:00:01.
	if got, want := w.Next(at(23, 30, 0), loc), time.Date(2026, 11, 24, 0, 0, 1, 0, loc); !got.Equal(want) {
		t.Fatalf("next start over midnight %v, want %v", got, want)
	}
}

/*
================
TestDailyWindowRunsPastMidnight

A daily row starting 22:00 for four hours still holds 01:00 the next
morning, and is closed at 02:00.
================
*/
func TestDailyWindowRunsPastMidnight(t *testing.T) {
	w := Window{Every: Daily, StartSecond: 22 * 3600, Duration: 4 * time.Hour}
	loc := time.UTC
	start, end, in := w.Current(time.Date(2026, 11, 24, 1, 0, 0, 0, loc), loc)
	if !in || !start.Equal(time.Date(2026, 11, 23, 22, 0, 0, 0, loc)) || !end.Equal(time.Date(2026, 11, 24, 2, 0, 0, 0, loc)) {
		t.Fatalf("01:00: %v..%v open %v", start, end, in)
	}
	if _, _, in := w.Current(time.Date(2026, 11, 24, 2, 0, 0, 0, loc), loc); in {
		t.Fatal("02:00 is past the window")
	}
}
