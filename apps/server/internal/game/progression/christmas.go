/*
===========================================================================

christmas.go - the Christmas event's switch and window

Port-only, not native: the v1.150 client carries the Christmas event's
client side (event group 1: "Event has started.", the stolen-socks event
in the Japanese row, with its snow), and the later client's Christmas
town decoration (world layer 7), but no server that runs the event. The
shard runs it behind SRO_CHRISTMAS_EVENT inside its UTC window
(seasonal_event.go); off, or without a valid window, nothing of it
exists, and the Red Santa Socks it handed out leave at each character's
next world entry (SocksExpireAt).

===========================================================================
*/
package progression

import "time"

// EnvChristmasEvent turns the event on ("on", "1", "true"); the window
// comes from EnvChristmasEventWindow. Off is the shipped game.
const EnvChristmasEvent = "SRO_CHRISTMAS_EVENT"

// EnvChristmasEventWindow is the event's UTC window, "START/END" in
// RFC3339 (for example 2026-12-20T00:00:00Z/2027-01-03T00:00:00Z).
const EnvChristmasEventWindow = "SRO_CHRISTMAS_EVENT_WINDOW"

/*
================
ChristmasEvent

The Christmas event's switch and window, and its winter wave timetable
(christmas_waves.go). The zero value is off and windowless: the native
game.
================
*/
type ChristmasEvent struct {
	SeasonalEvent
}

/*
================
ChristmasEventFromEnv

The winter event (#3603) runs only in its own mode: SRO_CHRISTMAS_EVENT_MODE
unset or "winter", the posture every shard had before the mode existed.
In any other mode it is off, so two Christmas events never share a window
(christmas_mode.go).
================
*/
func ChristmasEventFromEnv() ChristmasEvent {
	event := seasonalEventFromEnv(EnvChristmasEvent, EnvChristmasEventWindow)
	if ChristmasModeFromEnv() != ChristmasModeWinter {
		event.enabled = false
	}
	return ChristmasEvent{event}
}

/*
================
ChristmasEvent.SocksExpireAt

Whether the Red Santa Socks leave at a world entry now: whenever the
event does not run, switched off, without a valid window, or outside it. They are the
event's token and nothing else hands them out, so on a shard that never
ran the event there is nothing to remove.
================
*/
func (e ChristmasEvent) SocksExpireAt(now time.Time) bool {
	return !e.ActiveAt(now)
}
