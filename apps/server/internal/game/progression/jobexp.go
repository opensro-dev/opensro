/*
===========================================================================

jobexp.go - a job member's job experience and grade

CGObjPC_AddJobExp (4E2830) and CJobInfo_AddJobExp (60DD90). A gain below
grade 7 that reaches the grade's requirement (leveldata column 6 + job - 1
of the grade's row) advances one grade and keeps the remainder; any other
gain accumulates. A loss floors at zero and does nothing when there is no
job experience. Before either, a thief's or hunter's delta also moves the
week's contribution (CJobInfo_AddContribution 60E0A0, +0x24, clamped to
0..2,000,000,000; domain.CharacterJob.WeeklyReward), which the weekly close
ranks and pays from the pools. Each change publishes v1.150
0x35EE [u8 job][u8 grade][u32 exp] (v1.188 0x30E6).

===========================================================================
*/

package progression

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const (
	// OpJobExpUpdate is v1.150 CPSMission_OnJobTypeLevelUpdate0x35EE.
	OpJobExpUpdate uint16 = 0x35ee
	// maxJobGrade is 60DE5C's grade < 7 gate.
	maxJobGrade = 7
	// maxJobContribution is 60E0A0's clamp (TrijobMgr.cpp line 0x132).
	maxJobContribution = 2000000000
)

/*
================
AddJobExp

The caller owns the character transaction. False: nothing changed (no
job, no curve row, or a loss with no experience to lose).
================
*/
func AddJobExp(c *enterworld.Character, levels enterworld.JobLevelDataSource, delta int64) ([]wire.Frame, bool) {
	if c == nil || c.Job.Type == 0 || delta == 0 || levels == nil {
		return nil, false
	}
	job := &c.Job
	if job.Type != domain.JobTrader {
		job.WeeklyReward = int32(min(max(int64(job.WeeklyReward)+delta, 0), maxJobContribution))
	}
	exp := int64(job.Exp)
	if delta < 0 {
		if exp == 0 {
			return nil, false
		}
		job.Exp = uint32(max(0, exp+delta))
		return jobExpFrames(c), true
	}
	need, ok := levels.JobExpRequired(int64(job.Grade), job.Type)
	if !ok {
		return nil, false
	}
	if job.Grade < maxJobGrade && exp+delta >= need {
		job.Grade++
		job.Exp = uint32(exp - need + delta)
	} else {
		job.Exp = uint32(min(exp+delta, int64(^uint32(0))))
	}
	return jobExpFrames(c), true
}

/*
================
JobExperienceUpdater

The door-free job EXP updater combat calls inside its own character
transaction (a job kill's share).
================
*/
func (rt *Runtime) JobExperienceUpdater() func(*enterworld.Character, int64) ([]wire.Frame, bool) {
	return func(c *enterworld.Character, delta int64) ([]wire.Frame, bool) {
		levels, _ := rt.deps.LevelData().(enterworld.JobLevelDataSource)
		return AddJobExp(c, levels, delta)
	}
}

/*
================
jobExpFrames
================
*/
func jobExpFrames(c *enterworld.Character) []wire.Frame {
	payload := wire.NewWriter(6).U8(c.Job.Type).U8(c.Job.Grade).U32(c.Job.Exp).Payload()
	return []wire.Frame{{Opcode: OpJobExpUpdate, Payload: payload}}
}
