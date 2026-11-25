/*
===========================================================================

window.go - when a native schedule job's window is open

The v1.188 game server runs its timed content (the Roc's summit, the
Chin's tomb gate) as schedule jobs over the database's _Schedule rows:
a row names a repeating start (MainInterval_Type: daily, weekly) and a
SubInterval_DurationSecond the job stays started. Window is one such
row, read on the shard's local clock as the siege schedule is; its
owner (roc, the tomb gate) builds it from the row it ports.

A window must be shorter than its period: Current finds the latest
start at or before t, which is then the only start that can hold t.

===========================================================================
*/
package schedule

import "time"

/*
================
Interval

How often a window starts.
================
*/
type Interval int

const (
	// Weekly starts once a week, on Day (the zero value, as the Roc's
	// summit row is weekly).
	Weekly Interval = iota
	// Daily starts every day.
	Daily
	// Hourly starts every hour; StartSecond counts from the hour.
	Hourly
)

const (
	secondsPerHour = 3600
	daysPerWeek    = 7
)

/*
================
Window

One schedule row: how often it starts, the weekday (Weekly only, Go's
Sunday-based count), the start clock in seconds after midnight (after
the hour for Hourly), and a duration that may run past the next
midnight or hour.
================
*/
type Window struct {
	Every       Interval
	Day         time.Weekday
	StartSecond int
	Duration    time.Duration
}

/*
================
Window.Current

The window holding t, read in loc: its start and end, and ok true while
t lies inside it.
================
*/
func (w Window) Current(t time.Time, loc *time.Location) (start, end time.Time, ok bool) {
	start = w.latestStart(t, loc)
	end = start.Add(w.Duration)
	return start, end, t.Before(end)
}

/*
================
Window.Next

The first start strictly after t, read in loc.
================
*/
func (w Window) Next(t time.Time, loc *time.Location) time.Time {
	return w.step(w.latestStart(t, loc), 1)
}

/*
================
Window.latestStart

The latest start at or before t: this period's start, or the one
before when that is still ahead of t.
================
*/
func (w Window) latestStart(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	var start time.Time
	switch w.Every {
	case Hourly:
		hour := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, loc)
		start = hour.Add(time.Duration(w.StartSecond) * time.Second)
	case Daily:
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
		start = day.Add(time.Duration(w.StartSecond) * time.Second)
	default:
		back := (int(t.Weekday()) - int(w.Day) + daysPerWeek) % daysPerWeek
		day := time.Date(t.Year(), t.Month(), t.Day()-back, 0, 0, 0, 0, loc)
		start = day.Add(time.Duration(w.StartSecond) * time.Second)
	}
	if start.After(t) {
		start = w.step(start, -1)
	}
	return start
}

/*
================
Window.step

start moved by n periods, on the wall clock for days and weeks.
================
*/
func (w Window) step(start time.Time, n int) time.Time {
	switch w.Every {
	case Hourly:
		return start.Add(time.Duration(n*secondsPerHour) * time.Second)
	case Daily:
		return start.AddDate(0, 0, n)
	default:
		return start.AddDate(0, 0, n*daysPerWeek)
	}
}
