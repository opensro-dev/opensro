/*
===========================================================================

jobweek.go - the job guilds' weekly cycle and its ranking snapshot

The shard database's _UpdateTriangleJobWeekly (SRO_VT_SHARD, run weekly by a
SQL Agent job) closes a job week:

 1. thieves, then hunters: when the members contributed or the pool holds
    gold, each member's Reward grows by pool * Contribution / total
    (bigint, capped at INT_MAX), and the pool is emptied;
 2. every job's activity list: TOP 50 by Level DESC, Exp DESC;
 3. every job's contribution list, only when someone contributed: TOP 50 by
    Contribution DESC, then every member's Contribution returns to 0.

The lists are what the guild NPC shows as last week's rankings
(CGObjPC_HandleJobRanking70E4 512C20 serves them unchanged).

===========================================================================
*/
package domain

import (
	"cmp"
	"math"
	"slices"
)

const (
	// JobRankRows is the procedure's TOP 50.
	JobRankRows = 50
	// JobRankActivity and JobRankContribution are 0x737E's list kinds.
	JobRankActivity     uint8 = 0
	JobRankContribution uint8 = 1
)

/*
================
JobRankRow

One ranked member: NickName16 (the alias), the job level and the ranked
value (job experience or contribution).
================
*/
type JobRankRow struct {
	Alias string `json:"alias"`
	Grade uint8  `json:"grade"`
	Value uint32 `json:"value"`
}

/*
================
JobRankings

The last closed week's lists, indexed [job - 1][kind], and the week they
closed (JobWeekIndex). Week 0 is a shard that has not seen a week end.
================
*/
type JobRankings struct {
	Week  int64              `json:"week"`
	Lists [3][2][]JobRankRow `json:"lists"`
}

/*
================
List

The list of one job and kind, or nil.
================
*/
func (r JobRankings) List(job, kind uint8) []JobRankRow {
	if job < JobTrader || job > JobHunter || kind > JobRankContribution {
		return nil
	}
	return r.Lists[job-1][kind]
}

/*
================
jobMembers

The job's members in a stable order (the procedure's cursors have none;
the alias breaks ties so a rerun is identical).
================
*/
func jobMembers(characters []*Character, job uint8) []*Character {
	var members []*Character
	for _, c := range characters {
		if c != nil && c.Job.Type == job {
			members = append(members, c)
		}
	}
	return members
}

/*
================
splitJobReward

Step 1 for one job: the pool's share by contribution, added to each
member's Reward and capped at INT_MAX.
================
*/
func splitJobReward(members []*Character, pool *int64) {
	var total int64
	for _, c := range members {
		total += int64(max(c.Job.WeeklyReward, 0))
	}
	if total == 0 && *pool == 0 {
		return
	}
	if total > 0 {
		for _, c := range members {
			share := int64(0)
			if contribution := int64(max(c.Job.WeeklyReward, 0)); contribution > 0 {
				// pool * contribution fits: the pool and a contribution are
				// both below 2^31 in practice, and the bigint keeps 2^63.
				share = *pool * contribution / total
			}
			c.Job.Reward = int32(min(share+int64(max(c.Job.Reward, 0)), math.MaxInt32))
		}
	}
	// With gold in the pool and nobody contributing, the procedure's share
	// divides by zero: SQL Server ends that statement only, so nobody is
	// credited and the pool is still emptied below. (The failed SET keeps
	// the previous member's running total, which then leaks into the next
	// member's Reward; the port credits nothing instead.)
	*pool = 0
}

/*
================
rankJob

Steps 2 and 3 for one job and kind.
================
*/
func rankJob(members []*Character, kind uint8) []JobRankRow {
	rows := make([]JobRankRow, 0, len(members))
	for _, c := range members {
		row := JobRankRow{Alias: c.Job.Alias, Grade: c.Job.Grade, Value: c.Job.Exp}
		if kind == JobRankContribution {
			row.Value = uint32(max(c.Job.WeeklyReward, 0))
		}
		rows = append(rows, row)
	}
	slices.SortStableFunc(rows, func(a, b JobRankRow) int {
		if kind == JobRankActivity {
			if c := cmp.Compare(b.Grade, a.Grade); c != 0 {
				return c
			}
		}
		if c := cmp.Compare(b.Value, a.Value); c != 0 {
			return c
		}
		return cmp.Compare(a.Alias, b.Alias)
	})
	return rows[:min(len(rows), JobRankRows)]
}

/*
================
CloseJobWeek

Runs _UpdateTriangleJobWeekly over a division's characters and pool. The
contribution list of a job nobody contributed to keeps last week's rows,
as the procedure leaves that table alone.
================
*/
func CloseJobWeek(characters []*Character, pool *TradeRewardPool, previous JobRankings, week int64) JobRankings {
	splitJobReward(jobMembers(characters, JobThief), &pool.Thieves)
	splitJobReward(jobMembers(characters, JobHunter), &pool.Hunters)
	next := JobRankings{Week: week}
	for job := JobTrader; job <= JobHunter; job++ {
		members := jobMembers(characters, job)
		next.Lists[job-1][JobRankActivity] = rankJob(members, JobRankActivity)
		next.Lists[job-1][JobRankContribution] = previous.Lists[job-1][JobRankContribution]
		if !slices.ContainsFunc(members, func(c *Character) bool { return c.Job.WeeklyReward > 0 }) {
			continue
		}
		next.Lists[job-1][JobRankContribution] = rankJob(members, JobRankContribution)
		for _, c := range members {
			c.Job.WeeklyReward = 0
		}
	}
	return next
}
