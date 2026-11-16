/*
===========================================================================

public_schedule_test.go - every timed event reaches the public schedule

#3520: the site's calendar listed only maintenance and the fortress
windows while Capture the Flag ran every day and Halloween Day ran for a
week. These tests pin each scheduler's rows and fail when a kind the
calendar knows is never published.

===========================================================================
*/
package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	agentapi "opensro.online/server/internal/agent/api"
	"opensro.online/server/internal/agent/publicstats"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/progression"
)

// scheduleTestDay is a fixed UTC day for the flag war rows.
var scheduleTestDay = time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)

/*
================
eventsOfKind
================
*/
func eventsOfKind(events []publicstats.ScheduledEvent, kind string) []publicstats.ScheduledEvent {
	var out []publicstats.ScheduledEvent
	for _, event := range events {
		if event.Kind == kind {
			out = append(out, event)
		}
	}
	return out
}

/*
================
halloweenOn

A Halloween Day switch and window as the operator sets them.
================
*/
func halloweenOn(t *testing.T, start, end time.Time) progression.HalloweenEvent {
	t.Helper()
	t.Setenv(progression.EnvHalloweenEvent, "on")
	t.Setenv(progression.EnvHalloweenEventWindow, start.Format(time.RFC3339)+"/"+end.Format(time.RFC3339))
	return progression.HalloweenEventFromEnv()
}

/*
================
TestPublicScheduleListsTheDailyFlagWar

Before the start: today's sign-up and match. During sign-up the sign-up
row runs. During the match it is listed as running beside tomorrow's.
================
*/
func TestPublicScheduleListsTheDailyFlagWar(t *testing.T) {
	settings := action.FlagWarSettings{StartMinuteOfDay: 20 * 60, MinPlayers: 2}
	start := scheduleTestDay.Add(20 * time.Hour)
	src := scheduleSources{flagWar: settings}

	events := publicSchedule(src, start.Add(-time.Hour))
	signups, matches := eventsOfKind(events, publicstats.ScheduleKindFlagWarSignup), eventsOfKind(events, publicstats.ScheduleKindFlagWar)
	if len(signups) != 1 || len(matches) != 1 {
		t.Fatalf("an hour before: %+v, want one sign-up and one match", events)
	}
	if !signups[0].Start.Equal(start.Add(-10*time.Minute)) || !signups[0].End.Equal(start) || signups[0].Running {
		t.Fatalf("sign-up row %+v, want 19:50-20:00 not running", signups[0])
	}
	if !matches[0].Start.Equal(start) || !matches[0].End.Equal(start.Add(action.FlagWarMatchLength())) || matches[0].Running {
		t.Fatalf("match row %+v, want 20:00 for one match length, not running", matches[0])
	}

	signups = eventsOfKind(publicSchedule(src, start.Add(-5*time.Minute)), publicstats.ScheduleKindFlagWarSignup)
	if len(signups) != 1 || !signups[0].Running {
		t.Fatalf("five minutes before: sign-up %+v, want running", signups)
	}

	matches = eventsOfKind(publicSchedule(src, start.Add(5*time.Minute)), publicstats.ScheduleKindFlagWar)
	if len(matches) != 2 || !matches[0].Running || !matches[0].Start.Equal(start) || !matches[1].Start.Equal(start.AddDate(0, 0, 1)) {
		t.Fatalf("five minutes in: matches %+v, want today's running and tomorrow's", matches)
	}

	// A start the scheduler cancelled is not a running match (#3706):
	// five minutes in, only tomorrow's match is listed.
	cancelled := scheduleSources{flagWar: settings, flagWarCancelled: func(at time.Time) bool { return at.Equal(start) }}
	matches = eventsOfKind(publicSchedule(cancelled, start.Add(5*time.Minute)), publicstats.ScheduleKindFlagWar)
	if len(matches) != 1 || matches[0].Running || !matches[0].Start.Equal(start.AddDate(0, 0, 1)) {
		t.Fatalf("cancelled start, five minutes in: matches %+v, want only tomorrow's", matches)
	}

	if events := publicSchedule(scheduleSources{flagWarOff: true}, start); len(events) != 0 {
		t.Fatalf("no runtime listed %+v", events)
	}
}

/*
================
TestPublicScheduleListsHalloweenOnlyWhileItIsOnAndAhead

An off switch or a past window lists nothing; an upcoming window is
listed, and runs inside it.
================
*/
func TestPublicScheduleListsHalloweenOnlyWhileItIsOnAndAhead(t *testing.T) {
	start, end := scheduleTestDay, scheduleTestDay.AddDate(0, 0, 7)
	event := halloweenOn(t, start, end)

	ahead := eventsOfKind(publicSchedule(scheduleSources{flagWarOff: true, halloween: event}, start.Add(-time.Hour)), publicstats.ScheduleKindHalloween)
	if len(ahead) != 1 || ahead[0].Running || !ahead[0].Start.Equal(start) || !ahead[0].End.Equal(end) {
		t.Fatalf("ahead of the window: %+v", ahead)
	}
	inside := eventsOfKind(publicSchedule(scheduleSources{flagWarOff: true, halloween: event}, start.Add(time.Hour)), publicstats.ScheduleKindHalloween)
	if len(inside) != 1 || !inside[0].Running {
		t.Fatalf("inside the window: %+v", inside)
	}
	if past := publicSchedule(scheduleSources{flagWarOff: true, halloween: event}, end); len(past) != 0 {
		t.Fatalf("after the window listed %+v", past)
	}
	t.Setenv(progression.EnvHalloweenEvent, "off")
	off := progression.HalloweenEventFromEnv()
	if listed := publicSchedule(scheduleSources{flagWarOff: true, halloween: off}, start.Add(time.Hour)); len(listed) != 0 {
		t.Fatalf("switch off listed %+v", listed)
	}
}

/*
================
TestPublicScheduleKindsAllPublish

Every kind the calendar knows comes out of publicSchedule from a source
the running GameWorld wires (the fortress kinds are pinned by the siege
lane's own tests and need a loaded lane). Adding a kind constant without
a source fails here.
================
*/
func TestPublicScheduleKindsAllPublish(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "maintenance-gate.json")
	t.Setenv(agentapi.EnvMaintenanceGate, gate)
	if err := os.WriteFile(gate, []byte(`{"accounts":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	now := scheduleTestDay.Add(time.Hour)
	src := scheduleSources{halloween: halloweenOn(t, scheduleTestDay, scheduleTestDay.AddDate(0, 0, 7))}
	seen := map[string]bool{}
	for _, event := range publicSchedule(src, now) {
		seen[event.Kind] = true
	}
	for _, kind := range []string{publicstats.ScheduleKindMaintenance, publicstats.ScheduleKindFlagWarSignup, publicstats.ScheduleKindFlagWar, publicstats.ScheduleKindHalloween} {
		if !seen[kind] {
			t.Errorf("kind %q is never published", kind)
		}
	}
	for _, names := range publicWindowEvents {
		if names.kind != publicstats.ScheduleKindFortress && names.kind != publicstats.ScheduleKindFortressRegistration {
			t.Errorf("fortress window kind %q has no calendar constant", names.kind)
		}
	}
}
