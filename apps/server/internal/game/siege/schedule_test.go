package siege

import (
	"testing"
	"time"
)

/*
================
TestShippedScheduleRunsTheClientFortressWeek

The v1.150 client's week: applications Sunday to Tuesday, the war on
Wednesday 20:00-22:00 (the end second excluded), the 1-minute alert for
ten seconds. 2026-10-07 is a Wednesday.
================
*/
func TestShippedScheduleRunsTheClientFortressWeek(t *testing.T) {
	loc := time.FixedZone("shard", 8*3600)
	schedules, err := ParseSchedules(shippedSchedule, loc)
	if err != nil {
		t.Fatal(err)
	}
	at := func(day, hour, minute, second int) time.Time {
		return time.Date(2026, 10, day, hour, minute, second, 0, loc)
	}
	for _, tc := range []struct {
		name string
		at   time.Time
		want bool
	}{
		{"SiegeProgressing", at(7, 19, 59, 59), false},
		{"SiegeProgressing", at(7, 20, 0, 0), true},
		{"SiegeProgressing", at(7, 21, 59, 59), true},
		{"SiegeProgressing", at(7, 22, 0, 0), false},
		{"SiegeProgressing", at(8, 20, 30, 0), false},
		{"AllowSiegeRequest", at(4, 0, 0, 0), true},
		{"AllowSiegeRequest", at(6, 23, 59, 58), true},
		{"AllowSiegeRequest", at(7, 12, 0, 0), false},
		{"AllowSiegeRequest", at(3, 12, 0, 0), false},
		{"AlertSiegeFinishAfter30Min", at(7, 21, 30, 0), true},
		{"AlertSiegeFinishAfter1Min", at(7, 21, 59, 9), true},
		{"AlertSiegeFinishAfter1Min", at(7, 21, 59, 10), false},
		{"FortressStatusUpdate", at(11, 23, 5, 30), true},
	} {
		s, ok := schedules[tc.name]
		if !ok {
			t.Fatalf("schedule %s missing", tc.name)
		}
		if got := s.Active(tc.at); got != tc.want {
			t.Errorf("%s at %s: active=%v, want %v", tc.name, tc.at.Format(time.RFC1123), got, tc.want)
		}
	}
}

/*
================
TestScheduleRefusesMalformedBlocks
================
*/
func TestScheduleRefusesMalformedBlocks(t *testing.T) {
	for _, text := range []string{
		"Schedule A\n{\n\tDurationBegin\t2006-12-13, 00:00:00\n\tDurationEnd\t2030-12-31, 00:00:00\n\tOnce\t10:00:00, 11:00:00\n}\n",
		"Schedule A\n{\n\tDurationBegin\t2006-12-13, 00:00:00\n\tDurationEnd\t2030-12-31, 00:00:00\n\tDaily\t1,1\n\tOnce\t11:00:00, 10:00:00\n}\n",
		"Schedule A\n{\n\tDurationBegin\t2006-12-13, 00:00:00\n",
		"Schedule A\n{\n\tTwice\t1\n}\n",
	} {
		if _, err := ParseSchedules(text, time.UTC); err == nil {
			t.Errorf("accepted %q", text)
		}
	}
}

/*
================
TestScheduleWindowFindsTheRunningOrNextWar

During a war its own window; after it, next Wednesday's.
================
*/
func TestScheduleWindowFindsTheRunningOrNextWar(t *testing.T) {
	loc := time.FixedZone("shard", 8*3600)
	schedules, err := ParseSchedules(shippedSchedule, loc)
	if err != nil {
		t.Fatal(err)
	}
	war := schedules["SiegeProgressing"]
	start, end, ok := war.Window(time.Date(2026, 10, 7, 20, 30, 0, 0, loc))
	if !ok || !start.Equal(time.Date(2026, 10, 7, 20, 0, 0, 0, loc)) || !end.Equal(time.Date(2026, 10, 7, 22, 0, 0, 0, loc)) {
		t.Fatalf("running war window %s - %s", start, end)
	}
	start, _, ok = war.Window(time.Date(2026, 10, 7, 22, 0, 0, 0, loc))
	if !ok || !start.Equal(time.Date(2026, 10, 14, 20, 0, 0, 0, loc)) {
		t.Fatalf("next war %s", start)
	}
}

// The v1.150 application window's offsets from the war's start
// (CIFFortress_FormatWarDateText 660C70: FILETIME +0x10C388D000 for the
// war's end, -0x25B7F3D4000 and then +0x19254D38000 for the period).
const (
	clientWarLength   = 2 * time.Hour
	clientApplyOpens  = 3 * 24 * time.Hour
	clientApplyCloses = 1 * 24 * time.Hour
)

/*
================
TestShippedScheduleMatchesTheClientWindow

For a year of wars, the war lasts what the window prints, and every
application date it prints is accepted from its first second to its last
while the war day itself is not. Breaks when schedule.cfg drifts from the
client again (the Monday-to-Friday copy refused the Sunday it printed).
================
*/
func TestShippedScheduleMatchesTheClientWindow(t *testing.T) {
	loc := time.FixedZone("shard", 0)
	schedules, err := ParseSchedules(shippedSchedule, loc)
	if err != nil {
		t.Fatal(err)
	}
	war, request := schedules["SiegeProgressing"], schedules["AllowSiegeRequest"]
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, loc)
	for range 52 {
		start, end, ok := war.Window(now)
		if !ok {
			t.Fatalf("no war after %s", now)
		}
		if end.Sub(start) != clientWarLength {
			t.Fatalf("war %s lasts %s, the client prints %s", start, end.Sub(start), clientWarLength)
		}
		opens, closes := start.Add(-clientApplyOpens), start.Add(-clientApplyCloses)
		for day := time.Date(opens.Year(), opens.Month(), opens.Day(), 0, 0, 0, 0, loc); !day.After(closes); day = day.AddDate(0, 0, 1) {
			for _, at := range []time.Time{day, day.Add(24*time.Hour - 2*time.Second)} {
				if !request.Active(at) {
					t.Fatalf("war %s: the window prints %s but applications are shut", start.Format(time.DateOnly), at)
				}
			}
		}
		if request.Active(start) {
			t.Fatalf("war %s: applications open during the war", start)
		}
		now = end
	}
}
