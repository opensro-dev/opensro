/*
===========================================================================

christmas_snow.go - the Christmas event's snowfalls

Port-only, not native (SRO_CHRISTMAS_EVENT, default off). The v1.150
client draws the event's snow from event group 1 alone: 0x3347 (1, on)
starts it with "Event has started.", (1, off) stops it with "Event has
ended." (client 761430; event-environment.ts). Nothing in the client
starts or stops a snowfall on its own, so the original server toggled
the group. The timetable is the owner's memory of it (2026-11-15): a
snowfall of one hour every two hours, on the even UTC hours, kept clear
of 03:00-06:00 (the 04:00 snowfall does not fall). It is weather only
(owner, 2026-11-16): the Snow Slaves come from the nests whatever the
sky (winter_missions.go).

===========================================================================
*/
package progression

import "time"

const (
	// christmasSnowEvery is the time between two snowfalls' starts.
	christmasSnowEvery = 2 * time.Hour
	// christmasSnowLasts is one snowfall's length.
	christmasSnowLasts = time.Hour
)

// christmasQuietHours are the UTC hours no snowfall starts in: the 04:00
// daily reset and the 05:00 backup.
var christmasQuietHours = map[int]bool{3: true, 4: true, 5: true}

/*
================
ChristmasEvent.SnowingAt

Whether a snowfall is falling now: inside the event's window, during
the first hour of an even UTC hour pair, outside the quiet hours.
================
*/
func (e ChristmasEvent) SnowingAt(now time.Time) bool {
	if !e.ActiveAt(now) {
		return false
	}
	start := snowfallStart(now)
	return now.Sub(start) < christmasSnowLasts && !christmasQuietHours[start.Hour()]
}

/*
================
ChristmasEvent.SnowEndsAt

When the snowfall falling at now stops: the end of its hour, or the
window's end if that comes first. Meaningful only while SnowingAt.
================
*/
func (e ChristmasEvent) SnowEndsAt(now time.Time) time.Time {
	end := snowfallStart(now).Add(christmasSnowLasts)
	if e.EndsAt().Before(end) {
		return e.EndsAt()
	}
	return end
}

/*
================
ChristmasEvent.NextSnowfall

The first snowfall start after now, for the entry notice; false when
none falls inside the window.
================
*/
func (e ChristmasEvent) NextSnowfall(now time.Time) (time.Time, bool) {
	for at := snowfallStart(now).Add(christmasSnowEvery); e.ActiveAt(at); at = at.Add(christmasSnowEvery) {
		if !christmasQuietHours[at.Hour()] {
			return at, true
		}
	}
	return time.Time{}, false
}

/*
================
snowfallStart

The start of the two-hour slot holding now: the even UTC hour at or
before it.
================
*/
func snowfallStart(now time.Time) time.Time {
	return now.UTC().Truncate(christmasSnowEvery)
}
