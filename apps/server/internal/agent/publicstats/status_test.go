/*
===========================================================================

status_test.go - P7 status from samples and P8 schedule formatting

===========================================================================
*/
package publicstats

import (
	"net/http"
	"opensro.online/server/internal/game/social/chatfeed"
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

/*
================
TestChatNamesLinksAndNotices

P10: lines come oldest first; a hidden character is named but not
linked, an unknown one is not linked, and a notice has no character. A
suited author's line is named by the job alias and never linked: the
author's profile would unmask the character behind the suit.
================
*/
func TestChatNamesLinksAndNotices(t *testing.T) {
	f := newFixture()
	s := f.service()
	at := time.Date(2026, 10, 13, 10, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	s.src.Chat = func() []chatfeed.Line {
		return []chatfeed.Line{
			{At: at, Kind: chatfeed.KindChat, Character: "kekw", Author: "kekw", Text: "hi"},
			{At: at.Add(time.Second), Kind: chatfeed.KindGlobal, Character: "Shy", Author: "Shy", Text: "wts"},
			{At: at.Add(2 * time.Second), Kind: chatfeed.KindNotice, Text: "Maintenance"},
			{At: at.Add(3 * time.Second), Kind: chatfeed.KindChat, Character: "Gone", Author: "Gone", Text: "bye"},
			{At: at.Add(4 * time.Second), Kind: chatfeed.KindChat, Character: "JobFox", Author: "kekw", Text: "suited"},
		}
	}
	var chat ChatResponse
	if code := get(t, s, "/public/v1/chat", &chat); code != http.StatusOK {
		t.Fatalf("chat answered %d", code)
	}
	l := chat.Lines
	if len(l) != 5 || l[0].At != "2026-10-13T02:00:00Z" || *l[0].Character != "kekw" || !l[0].Profile ||
		*l[1].Character != "Shy" || l[1].Profile || l[1].Kind != "global" ||
		l[2].Character != nil || l[2].Profile || l[2].Kind != "notice" ||
		l[3].Profile || l[3].Text != "bye" ||
		*l[4].Character != "JobFox" || l[4].Profile || l[4].Text != "suited" {
		t.Fatalf("lines %+v", l)
	}
}

/*
================
TestChatWithoutSourceIsEmpty
================
*/
func TestChatWithoutSourceIsEmpty(t *testing.T) {
	var chat ChatResponse
	if code := get(t, newFixture().service(), "/public/v1/chat", &chat); code != http.StatusOK || chat.Lines == nil || len(chat.Lines) != 0 {
		t.Fatalf("chat answered %d %+v", code, chat)
	}
}

/*
================
TestEventsServeAnnouncementsApartFromChat

The events route serves the announcer's lines with time and text only;
the chat route never sees them, and no source serves an empty list.
================
*/
func TestEventsServeAnnouncementsApartFromChat(t *testing.T) {
	s := newFixture().service()
	var empty EventsResponse
	if code := get(t, s, "/public/v1/events", &empty); code != http.StatusOK || empty.Lines == nil || len(empty.Lines) != 0 {
		t.Fatalf("events answered %d %+v", code, empty)
	}
	feed := chatfeed.New()
	feed.Record("shard", chatfeed.KindChat, "kekw", "kekw", "hi")
	feed.Record("shard", chatfeed.KindEvent, "", "", "Shy got a Seal of Star")
	s = newFixture().service()
	s.src.Chat = func() []chatfeed.Line { return feed.Lines("shard", nil) }
	s.src.Events = func() []chatfeed.Line { return feed.Events("shard") }
	var events EventsResponse
	if code := get(t, s, "/public/v1/events", &events); code != http.StatusOK || len(events.Lines) != 1 ||
		events.Lines[0].Text != "Shy got a Seal of Star" || events.Lines[0].At == "" {
		t.Fatalf("events answered %d %+v", code, events)
	}
	var chat ChatResponse
	if code := get(t, s, "/public/v1/chat", &chat); code != http.StatusOK || len(chat.Lines) != 1 || chat.Lines[0].Text != "hi" {
		t.Fatalf("chat answered %d %+v", code, chat)
	}
}
