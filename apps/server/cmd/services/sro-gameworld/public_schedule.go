/*
===========================================================================

public_schedule.go - the events the public schedule lists (port-only)

P8's calendar: every event the shard runs on a clock. Each scheduler that
starts something players can plan for has a kind (publicstats'
ScheduleKind constants), and publicSchedule asks each of them; a test
fails when a kind is never published, so a new timed event cannot ship
invisible to the site (#3520).

The sources:

  - maintenance, while the release tool's gate file exists (its start is the
    file's time and the end is open);
  - the fortress lane's registration and war windows;
  - Capture the Flag's daily sign-up and match (action.FlagWarSettings);
  - the Halloween Day window, when its switch is on.

Port-only, not native: the original has no public read API.

===========================================================================
*/
package main

import (
	"os"
	"strings"
	"time"

	agentapi "opensro.online/server/internal/agent/api"
	"opensro.online/server/internal/agent/publicstats"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/siege"
)

// publicWindowEvents names the fortress lane's windows on the calendar.
var publicWindowEvents = map[string]struct{ kind, title string }{
	"AllowSiegeRequest": {publicstats.ScheduleKindFortressRegistration, "Fortress war registration"},
	"SiegeProgressing":  {publicstats.ScheduleKindFortress, "Fortress war"},
}

/*
================
scheduleSources

The schedulers publicSchedule reads. A nil siege lane and zero settings
list nothing for that source (the flag war is the exception: its zero
settings are the native default schedule, as the runtime itself runs).
================
*/
type scheduleSources struct {
	siege     *siege.Lane
	flagWar   action.FlagWarSettings
	halloween progression.HalloweenEvent
	// flagWarOff leaves Capture the Flag out (no action runtime wired).
	flagWarOff bool
	// flagWarCancelled answers whether the match starting at an instant
	// was cancelled at its start (action.Runtime.FlagWarCancelled); nil
	// treats every past start as a match.
	flagWarCancelled func(start time.Time) bool
}

/*
================
scheduleSourcesOf

The running GameWorld's schedulers.
================
*/
func scheduleSourcesOf(game *gameplayPlane) scheduleSources {
	if game == nil || game.items == nil {
		return scheduleSources{flagWarOff: true}
	}
	return scheduleSources{siege: game.siege, flagWar: game.items.FlagWar, halloween: game.items.Halloween,
		flagWarCancelled: game.items.FlagWarCancelled}
}

/*
================
publicSchedule
================
*/
func publicSchedule(src scheduleSources, now time.Time) []publicstats.ScheduledEvent {
	var events []publicstats.ScheduledEvent
	if path := strings.TrimSpace(os.Getenv(agentapi.EnvMaintenanceGate)); path != "" {
		if info, err := os.Stat(path); err == nil {
			events = append(events, publicstats.ScheduledEvent{Kind: publicstats.ScheduleKindMaintenance, Title: "Maintenance",
				Start: info.ModTime(), Running: true})
		}
	}
	for _, window := range src.siege.Windows(now.UnixMilli()) {
		names, ok := publicWindowEvents[window.Name]
		if !ok {
			continue
		}
		events = append(events, publicstats.ScheduledEvent{Kind: names.kind, Title: names.title, Start: window.Start,
			End: window.End, Running: window.Running})
	}
	if !src.flagWarOff {
		events = append(events, flagWarEvents(src.flagWar, src.flagWarCancelled, now)...)
	}
	if start, end, valid := src.halloween.Window(); src.halloween.Enabled() && valid && now.Before(end) {
		events = append(events, publicstats.ScheduledEvent{Kind: publicstats.ScheduleKindHalloween, Title: "Halloween Day",
			Start: start, End: end, Running: src.halloween.ActiveAt(now)})
	}
	return events
}

/*
================
flagWarEvents

The sign-up window and the match. A match still running is listed until
it ends; the next sign-up is listed beside it. A start that the
scheduler cancelled (too few registrants) is not a running match, so it
drops off at its start instant rather than showing as running for a
whole match length (#3706).
================
*/
func flagWarEvents(settings action.FlagWarSettings, cancelled func(time.Time) bool, now time.Time) []publicstats.ScheduledEvent {
	var events []publicstats.ScheduledEvent
	length := action.FlagWarMatchLength()
	signupAt, startsAt := settings.NextMatch(now)
	previous := startsAt.AddDate(0, 0, -1)
	if now.Before(previous.Add(length)) && (cancelled == nil || !cancelled(previous)) {
		events = append(events, publicstats.ScheduledEvent{Kind: publicstats.ScheduleKindFlagWar, Title: "Capture the Flag",
			Start: previous, End: previous.Add(length), Running: true})
	}
	return append(events,
		publicstats.ScheduledEvent{Kind: publicstats.ScheduleKindFlagWarSignup, Title: "Capture the Flag sign-up",
			Start: signupAt, End: startsAt, Running: !now.Before(signupAt)},
		publicstats.ScheduledEvent{Kind: publicstats.ScheduleKindFlagWar, Title: "Capture the Flag",
			Start: startsAt, End: startsAt.Add(length)})
}
