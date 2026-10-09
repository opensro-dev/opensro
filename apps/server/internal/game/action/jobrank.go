/*
===========================================================================

jobrank.go - the job guilds' rank and contribution lists

The job menu's JOBRANK and DONATIONRANK/CONTRIBUTERANK rows send

	0x737E [u32 npc][u8 job][u8 kind] -> 0xB37E [1][job][kind][u8 count]
	                                       { [u8 rank][ascii alias][u8 grade][u32 value] }

kind 0 is the job's activity rank (CIFJobRank: value is the job
experience), kind 1 its contribution rank (CIFJobContributionRank: value
is the weekly contribution). A refusal is [2][code][job][kind] in notice
category 0x18 (CPSMission_OnJobRankListResponse0xB37E 763F00).

v1.188 CGObjPC_HandleJobRanking70E4 (512C20) checks the NPC is in range
(code 3), the job is 1-3 (0x28) and the kind 0 or 1 (0x0F), then sends the
list the shard database's weekly _UpdateTriangleJobWeekly built: last
week's TOP 50 (domain.CloseJobWeek, kept in the authority's metadata).

The week closes on the mission clock. INFERENCE: the SQL Agent schedule is
not in the backups; the port closes a week at Monday 00:00 server time, the
week the client's "last week" labels count.

===========================================================================
*/

package action

import (
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const (
	opJobRankRequest  uint16 = 0x737E
	opJobRankResponse uint16 = 0xB37E

	// jobRankErrKind is 512C20's 0x480F low byte.
	jobRankErrKind uint8 = 0x0f
	// jobWeekEpochShift moves 1970-01-01 (a Thursday) to the Monday that
	// starts its week.
	jobWeekEpochShift = 3
	jobWeekDays       = 7
)

/*
================
jobRankRefusal
================
*/
func jobRankRefusal(code, job, kind uint8) OpResult {
	return OpResult{Frames: []wire.Frame{{Opcode: opJobRankResponse, Payload: []byte{2, code, job, kind}}}}
}

/*
================
JobWeekIndex

The Monday-started week holding now in loc, counted from 1970 (the
Monday of the epoch's week is week 0).
================
*/
func JobWeekIndex(now time.Time, loc *time.Location) int64 {
	y, m, d := now.In(loc).Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400
	return (day + jobWeekEpochShift) / jobWeekDays
}

/*
================
JobWeekTick

The mission-clock hook that closes the division's job week when a new one
starts; the store refuses a week it already closed.
================
*/
func (rt *Runtime) JobWeekTick(division string, nowMs int64) {
	week := JobWeekIndex(time.UnixMilli(nowMs), time.Local)
	if rt.jobWeek.Swap(week) == week {
		return
	}
	rt.deps.CloseWeek(division, week, "job-week")
}

/*
================
HandleJobRank
================
*/
func (rt *Runtime) HandleJobRank(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	gid, e := r.U32()
	job, e2 := r.U8()
	kind, e3 := r.U8()
	if c == nil || e != nil || e2 != nil || e3 != nil || r.Done() != nil {
		return OpResult{}
	}
	if selected, ok := rt.Selected.Get(division, c.Name); !ok || selected != gid {
		return jobRankRefusal(jobErrTooFar, job, kind)
	}
	npc, ok := rt.npcForCurrentViewer(division, c, gid)
	if !ok || !rt.npcWithinHitRange(division, c, npc) {
		return jobRankRefusal(jobErrTooFar, job, kind)
	}
	if job < 1 || job > 3 {
		return jobRankRefusal(jobErrInvalidJob, job, kind)
	}
	if kind != domain.JobRankActivity && kind != domain.JobRankContribution {
		return jobRankRefusal(jobRankErrKind, job, kind)
	}
	rows := rt.deps.WeekRankings(division).List(job, kind)
	w := wire.NewWriter(4 + len(rows)*24).U8(1).U8(job).U8(kind).U8(uint8(len(rows)))
	for index, row := range rows {
		w.U8(uint8(index + 1)).Str(row.Alias).U8(row.Grade).U32(row.Value)
	}
	return OpResult{Frames: []wire.Frame{{Opcode: opJobRankResponse, Payload: w.Payload()}}}
}
