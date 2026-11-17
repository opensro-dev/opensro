/*
===========================================================================

schedule.go - when the Roc's summit (INS_ROC_TOP) is open

Port-only, not native to v1.150 (SRO_ROC, #3399 step 4): the v1.150
client carries the Roc worlds, but the schedule that opens them is the
later server's. The v1.188 game server runs the summit from the schedule
job keyed SCHEDULE_DEF_MEET_ROC (CGameSJ_Roc): the start edge opens
INS_ROC_TOP and broadcasts UIIT_MSG_QUEST_ROC_APPEAR, the end edge closes
it with UIIT_MSG_QUEST_ROC_DISAPPEAR.

The window is that define's one _Schedule row in the iSRO-R shard
(SILKROAD_R_SHARD202507231313.bak, ScheduleIdx 2506 under define 10):
weekly (MainInterval_Type 3) on SubInterval_DayOfWeek 7, from 22:00:00
for SubInterval_DurationSecond 14400, so every Saturday 22:00 to Sunday
02:00. DayOfWeek is SQL Server's DATEPART(dw), 1 = Sunday: the shard's
weekly rows use 1..7 only, and its arena rows give days 1 and 7 one
timetable and 2..6 another. The legacy schedule.cfg block
RocTopGateOpen1 is not what that job reads; research/investigations/
roc-arena/NOTES.md has the full trail.

Times are the shard's local time, as the siege schedule's are.

===========================================================================
*/

package roc

import "time"

// The SCHEDULE_DEF_MEET_ROC row (iSRO-R ScheduleIdx 2506).
const (
	// scheduleDayOfWeek is the row's DATEPART(dw) day, 1 = Sunday.
	scheduleDayOfWeek = 7
	scheduleStartHour = 22
	// scheduleDurationSeconds is SubInterval_DurationSecond: four hours.
	scheduleDurationSeconds = 14400
)

// daysPerWeek steps a weekly start back one week.
const daysPerWeek = 7

/*
================
Window

The summit window as the schedule row states it: one weekday (Go's
Sunday-based count), a start clock in seconds after midnight, and a
duration that may run past midnight.
================
*/
type Window struct {
	Day         time.Weekday
	StartSecond int
	Duration    time.Duration
}

/*
================
SummitWindow

The SCHEDULE_DEF_MEET_ROC row: Saturday 22:00 for four hours.
================
*/
func SummitWindow() Window {
	return Window{
		// DATEPART(dw) counts 1 = Sunday; Go counts 0 = Sunday.
		Day:         time.Weekday(scheduleDayOfWeek - 1),
		StartSecond: scheduleStartHour * 3600,
		Duration:    scheduleDurationSeconds * time.Second,
	}
}

/*
================
Window.Current

The window holding t, read in loc: its start and end, and ok true while
t lies inside it. The latest start at or before t is the only one that
can hold t, since a window is shorter than the week between starts.
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
	return w.latestStart(t, loc).AddDate(0, 0, daysPerWeek)
}

/*
================
Window.latestStart

The latest start at or before t: this week's start day at the start
clock, or the week before when that is still ahead of t.
================
*/
func (w Window) latestStart(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	back := (int(t.Weekday()) - int(w.Day) + daysPerWeek) % daysPerWeek
	day := time.Date(t.Year(), t.Month(), t.Day()-back, 0, 0, 0, 0, loc)
	start := day.Add(time.Duration(w.StartSecond) * time.Second)
	if start.After(t) {
		start = start.AddDate(0, 0, -daysPerWeek)
	}
	return start
}
