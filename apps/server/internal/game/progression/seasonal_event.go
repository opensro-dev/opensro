/*
===========================================================================

seasonal_event.go - a seasonal event's switch and UTC window

Port-only, not native: the shard runs a seasonal event (Halloween Day,
the Christmas event) only when its SRO_<EVENT> switch is on and its
SRO_<EVENT>_WINDOW names a valid "START/END" RFC3339 window. Both are
configuration, read once at boot. Off, or without a valid window, the
event never runs: the native game.

===========================================================================
*/
package progression

import (
	"os"
	"strings"
	"time"
)

/*
================
SeasonalEvent

The zero value is off and windowless: the native game.
================
*/
type SeasonalEvent struct {
	enabled  bool
	startsAt time.Time
	endsAt   time.Time
}

/*
================
seasonalEventFromEnv

switchEnv turns the event on ("on", "1", "true"); windowEnv is its UTC
window, "START/END" in RFC3339 (for example
2026-10-31T00:00:00Z/2026-11-02T00:00:00Z). A window that does not parse,
or ends before it starts, is no window.
================
*/
func seasonalEventFromEnv(switchEnv, windowEnv string) SeasonalEvent {
	event := SeasonalEvent{}
	switch strings.ToLower(strings.TrimSpace(os.Getenv(switchEnv))) {
	case "on", "1", "true":
		event.enabled = true
	}
	window := strings.Split(strings.TrimSpace(os.Getenv(windowEnv)), "/")
	if len(window) == 2 {
		startsAt, startErr := time.Parse(time.RFC3339, strings.TrimSpace(window[0]))
		endsAt, endErr := time.Parse(time.RFC3339, strings.TrimSpace(window[1]))
		if startErr == nil && endErr == nil && endsAt.After(startsAt) {
			event.startsAt, event.endsAt = startsAt.UTC(), endsAt.UTC()
		}
	}
	return event
}

/*
================
SeasonalEvent.Enabled

Whether the switch is on, whatever the window says.
================
*/
func (e SeasonalEvent) Enabled() bool { return e.enabled }

/*
================
SeasonalEvent.WindowValid

Whether a usable window is configured. An event switched on without one
never runs; the composition root warns about that posture at boot.
================
*/
func (e SeasonalEvent) WindowValid() bool { return !e.startsAt.IsZero() && e.endsAt.After(e.startsAt) }

/*
================
SeasonalEvent.Window

The configured window, and whether it is usable.
================
*/
func (e SeasonalEvent) Window() (startsAt, endsAt time.Time, valid bool) {
	return e.startsAt, e.endsAt, e.WindowValid()
}

/*
================
SeasonalEvent.ActiveAt

The event runs from the window's start up to (not including) its end.
================
*/
func (e SeasonalEvent) ActiveAt(now time.Time) bool {
	return e.enabled && e.WindowValid() && !now.Before(e.startsAt) && now.Before(e.endsAt)
}

/*
================
SeasonalEvent.EndsAt

The window's end, the first instant the event no longer runs; zero when
no window is configured.
================
*/
func (e SeasonalEvent) EndsAt() time.Time { return e.endsAt }

/*
================
SeasonalEvent.CleanupDueAt

Once a configured window has ended, what the event left behind (the
Halloween boxes) leaves at the first world entry after the end. This
follows the window alone: a shard that ran the event and later flips the
switch off still retires what its window created. With no window
configured nothing is ever due.
================
*/
func (e SeasonalEvent) CleanupDueAt(now time.Time) bool {
	return e.WindowValid() && !now.Before(e.endsAt)
}
