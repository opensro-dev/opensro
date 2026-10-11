/*
===========================================================================

status_test.go - P7 status from samples and P8 schedule formatting

===========================================================================
*/
package publicstats

import (
	"net/http"
	"testing"
	"time"

	"opensro.online/server/internal/transport"
)

/*
================
statusService

A service over a real hub whose ticks the test records, sampled by hand.
================
*/
func statusService(t *testing.T) (*Service, *transport.Hub, *int, *time.Time) {
	t.Helper()
	hub := &transport.Hub{}
	online := 0
	now := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	s := New(Sources{
		Now:       func() time.Time { return now },
		StartedAt: now.Add(-90 * time.Minute),
		Sample:    func() (int, transport.Metrics) { return online, hub.Metrics() },
	})
	return s, hub, &online, &now
}

/*
================
TestStatusReadsTheRecentTickWindow

The tick figures cover only the last five minutes of samples: a slow boot
an hour ago does not show, a slow tick now does, as one percent.
================
*/
func TestStatusReadsTheRecentTickWindow(t *testing.T) {
	s, hub, online, now := statusService(t)
	s.recordStatus(*now)
	for i := 0; i < 50; i++ {
		hub.RecordTickDuration(400*time.Millisecond, 100*time.Millisecond)
	}
	*now = now.Add(time.Hour)
	s.recordStatus(*now)
	for i := 0; i < 99; i++ {
		hub.RecordTickDuration(8*time.Millisecond, 100*time.Millisecond)
	}
	hub.RecordTickDuration(150*time.Millisecond, 100*time.Millisecond)
	*online = 66
	*now = now.Add(statusSampleEvery)
	s.recordStatus(*now)

	var status StatusResponse
	if code := get(t, s, "/public/v1/status", &status); code != http.StatusOK {
		t.Fatalf("status answered %d", code)
	}
	if status.MasteryTotalOverride != 0 {
		t.Fatalf("a native shard published a mastery override %d", status.MasteryTotalOverride)
	}
	s.src.MasteryTotalOverride = 5000
	s.mu.Lock()
	s.cache = map[string]cacheEntry{}
	s.mu.Unlock()
	if get(t, s, "/public/v1/status", &status); status.MasteryTotalOverride != 5000 {
		t.Fatalf("the beta mastery allowance is %d, want 5000", status.MasteryTotalOverride)
	}
	if !status.Up || status.Online != 66 || status.UptimeSeconds != int64((150*time.Minute+statusSampleEvery)/time.Second) {
		t.Fatalf("status %+v", status)
	}
	if status.Tick.P50Ms != 10 || status.Tick.P99Ms != 10 || status.Tick.SlowPercent != 1 {
		t.Fatalf("tick %+v, want the last window only: p50 10, p99 10, 1%% slow", status.Tick)
	}
	if len(status.History) != 1 || status.History[0].At != "2026-10-11T10:00:00Z" || status.History[0].P99Ms != 400 {
		t.Fatalf("history %+v, want the 10:00 hour with its slow boot (p99 400)", status.History)
	}
}

/*
================
TestStatusHistoryKeepsTheLast48Hours

One point per closed hour with that hour's peak online, never more than 48.
================
*/
func TestStatusHistoryKeepsTheLast48Hours(t *testing.T) {
	s, _, online, now := statusService(t)
	for hour := 0; hour < 60; hour++ {
		*online = hour
		s.recordStatus(*now)
		*online = hour + 100
		s.recordStatus(now.Add(30 * time.Minute))
		*now = now.Add(time.Hour)
	}
	s.recordStatus(*now)
	history := s.status.history
	if len(history) != statusHistoryPoints {
		t.Fatalf("history holds %d points, want %d", len(history), statusHistoryPoints)
	}
	if last := history[len(history)-1]; last.Online != 159 {
		t.Fatalf("last hour's peak %d, want 159", last.Online)
	}
}

/*
================
TestScheduleFormatsTheServerEvents

Times are RFC 3339 UTC; an open-ended event has a null end.
================
*/
func TestScheduleFormatsTheServerEvents(t *testing.T) {
	f := newFixture()
	s := f.service()
	var schedule ScheduleResponse
	if code := get(t, s, "/public/v1/schedule", &schedule); code != http.StatusOK {
		t.Fatalf("schedule answered %d", code)
	}
	if len(schedule.Events) != 2 || schedule.Events[0].Kind != "fortress" || schedule.Events[0].EndsAt == nil ||
		schedule.Events[1].EndsAt != nil || !schedule.Events[1].Running {
		t.Fatalf("events %+v", schedule.Events)
	}
	if _, err := time.Parse(time.RFC3339, schedule.ServerTime); err != nil {
		t.Fatalf("server time %q: %v", schedule.ServerTime, err)
	}
}
