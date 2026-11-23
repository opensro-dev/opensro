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

import (
	"time"

	"opensro.online/server/internal/game/world/schedule"
)

// The SCHEDULE_DEF_MEET_ROC row (iSRO-R ScheduleIdx 2506).
const (
	// scheduleDayOfWeek is the row's DATEPART(dw) day, 1 = Sunday.
	scheduleDayOfWeek = 7
	scheduleStartHour = 22
	// scheduleDurationSeconds is SubInterval_DurationSecond: four hours.
	scheduleDurationSeconds = 14400
)

/*
================
Window

The summit window: the shared schedule row type (world/schedule), kept
under this name for the Roc's callers.
================
*/
type Window = schedule.Window

/*
================
SummitWindow

The SCHEDULE_DEF_MEET_ROC row: Saturday 22:00 for four hours.
================
*/
func SummitWindow() Window {
	return Window{
		Every: schedule.Weekly,
		// DATEPART(dw) counts 1 = Sunday; Go counts 0 = Sunday.
		Day:         time.Weekday(scheduleDayOfWeek - 1),
		StartSecond: scheduleStartHour * 3600,
		Duration:    scheduleDurationSeconds * time.Second,
	}
}
