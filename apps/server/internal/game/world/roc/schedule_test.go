/*
===========================================================================

schedule_test.go - the Roc summit window

===========================================================================
*/

package roc

import (
	"testing"
	"time"
)

/*
================
TestSummitWindowIsTheMeetRocRow

The iSRO-R row: DATEPART(dw) 7 (Saturday), 22:00:00, 14400 seconds.
================
*/
func TestSummitWindowIsTheMeetRocRow(t *testing.T) {
	w := SummitWindow()
	if w.Day != time.Saturday || w.StartSecond != 22*3600 || w.Duration != 4*time.Hour {
		t.Fatalf("window = %+v, want Saturday 22:00 for 4h", w)
	}
}

/*
================
TestSummitWindowRunsPastMidnight

Saturday 22:00 opens it, Sunday 01:59:59 is still inside, Sunday 02:00
is not, and nothing else in the week is.
================
*/
func TestSummitWindowRunsPastMidnight(t *testing.T) {
	loc := time.FixedZone("shard", 9*3600)
	w := SummitWindow()
	// 2026-11-21 is a Saturday.
	at := func(day, hour, minute, second int) time.Time {
		return time.Date(2026, 11, day, hour, minute, second, 0, loc)
	}
	open := at(21, 22, 0, 0)
	cases := []struct {
		name string
		t    time.Time
		in   bool
	}{
		{"saturday 21:59:59", at(21, 21, 59, 59), false},
		{"saturday 22:00", open, true},
		{"saturday 23:30", at(21, 23, 30, 0), true},
		{"sunday 01:59:59", at(22, 1, 59, 59), true},
		{"sunday 02:00", at(22, 2, 0, 0), false},
		{"wednesday 22:30", at(18, 22, 30, 0), false},
		{"friday 23:00", at(20, 23, 0, 0), false},
	}
	for _, c := range cases {
		start, end, in := w.Current(c.t, loc)
		if in != c.in {
			t.Errorf("%s: in = %v, want %v (window %v..%v)", c.name, in, c.in, start, end)
		}
		if in && (!start.Equal(open) || !end.Equal(open.Add(4*time.Hour))) {
			t.Errorf("%s: window %v..%v, want %v..+4h", c.name, start, end, open)
		}
	}
}

/*
================
TestSummitWindowNextStart

The next start after any instant is the following Saturday 22:00, also
from inside a window and from its very start.
================
*/
func TestSummitWindowNextStart(t *testing.T) {
	loc := time.FixedZone("shard", -5*3600)
	w := SummitWindow()
	want := time.Date(2026, 11, 28, 22, 0, 0, 0, loc)
	for _, from := range []time.Time{
		time.Date(2026, 11, 21, 22, 0, 0, 0, loc),
		time.Date(2026, 11, 22, 1, 0, 0, 0, loc),
		time.Date(2026, 11, 25, 12, 0, 0, 0, loc),
		time.Date(2026, 11, 28, 21, 59, 59, 0, loc),
	} {
		if got := w.Next(from, loc); !got.Equal(want) {
			t.Errorf("Next(%v) = %v, want %v", from, got, want)
		}
	}
}
