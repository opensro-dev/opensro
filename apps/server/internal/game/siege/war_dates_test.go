/*
===========================================================================

war_dates_test.go - previous and upcoming dates from the war's schedule

===========================================================================
*/
package siege

import (
	"testing"
	"time"
)

/*
================
TestWarDatesFollowConfiguredSchedule
================
*/
func TestWarDatesFollowConfiguredSchedule(t *testing.T) {
	lane, _, loc := laneFixture(t)
	start := time.Date(2026, 10, 7, 20, 0, 0, 0, loc)
	for _, delta := range []time.Duration{-time.Hour, 0, time.Hour} {
		previous, next := lane.WarDates(start.Add(delta).UnixMilli())
		if !previous.Equal(start.AddDate(0, 0, -7)) || !next.Equal(start) {
			t.Fatalf("offset %v: %v / %v", delta, previous, next)
		}
	}
	previous, next := lane.WarDates(start.Add(2 * time.Hour).UnixMilli())
	if !previous.Equal(start) || !next.Equal(start.AddDate(0, 0, 7)) {
		t.Fatalf("after war: %v / %v", previous, next)
	}
	schedule := lane.config.Schedules["SiegeProgressing"]
	schedule.Begin = start
	lane.config.Schedules["SiegeProgressing"] = schedule
	previous, next = lane.WarDates(start.UnixMilli())
	if !previous.IsZero() || !next.Equal(start) {
		t.Fatalf("first war: %v / %v", previous, next)
	}
}
