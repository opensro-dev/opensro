package siege

import (
	"testing"
	"time"
)

/*
================
TestShippedScheduleRunsTheRetailFortressWeek

The retail siege blocks: applications Monday to Friday, the war on
Wednesday 20:00-21:30 (the end second excluded), the 1-minute alert for
ten seconds. 2026-10-07 is a Wednesday.
================
*/
func TestShippedScheduleRunsTheRetailFortressWeek(t *testing.T) {
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
		{"SiegeProgressing", at(7, 21, 29, 59), true},
		{"SiegeProgressing", at(7, 21, 30, 0), false},
		{"SiegeProgressing", at(8, 20, 30, 0), false},
		{"AllowSiegeRequest", at(5, 12, 0, 0), true},
		{"AllowSiegeRequest", at(9, 23, 59, 58), true},
		{"AllowSiegeRequest", at(10, 12, 0, 0), false},
		{"AllowSiegeRequest", at(11, 12, 0, 0), false},
		{"AlertSiegeFinishAfter1Min", at(7, 21, 29, 9), true},
		{"AlertSiegeFinishAfter1Min", at(7, 21, 29, 10), false},
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
