/*
===========================================================================

jobweek_test.go - _UpdateTriangleJobWeekly's close of a job week

===========================================================================
*/
package domain

import (
	"math"
	"reflect"
	"testing"
)

/*
================
jobWeekMember
================
*/
func jobWeekMember(alias string, job, grade uint8, exp uint32, contribution, reward int32) *Character {
	return &Character{Job: CharacterJob{Type: job, Grade: grade, Exp: exp, Alias: alias, WeeklyReward: contribution, Reward: reward}}
}

/*
================
TestCloseJobWeekRanksAndResets

Activity ranks level then exp for every job; contribution ranks the amount
and zeroes it; a job nobody contributed to keeps last week's list.
================
*/
func TestCloseJobWeekRanksAndResets(t *testing.T) {
	low := jobWeekMember("Low", JobTrader, 1, 900, 0, 0)
	high := jobWeekMember("High", JobTrader, 2, 10, 70, 0)
	mid := jobWeekMember("Mid", JobTrader, 1, 950, 300, 0)
	thief := jobWeekMember("Shade", JobThief, 3, 5, 0, 0)
	previous := JobRankings{Week: 9}
	previous.Lists[JobThief-1][JobRankContribution] = []JobRankRow{{Alias: "Old", Grade: 1, Value: 4}}
	pool := TradeRewardPool{}
	next := CloseJobWeek([]*Character{low, high, mid, thief}, &pool, previous, 10)
	if next.Week != 10 {
		t.Fatalf("week %d", next.Week)
	}
	wantActivity := []JobRankRow{{"High", 2, 10}, {"Mid", 1, 950}, {"Low", 1, 900}}
	if got := next.List(JobTrader, JobRankActivity); !reflect.DeepEqual(got, wantActivity) {
		t.Fatalf("activity %+v", got)
	}
	wantContribution := []JobRankRow{{"Mid", 1, 300}, {"High", 2, 70}, {"Low", 1, 0}}
	if got := next.List(JobTrader, JobRankContribution); !reflect.DeepEqual(got, wantContribution) {
		t.Fatalf("contribution %+v", got)
	}
	if high.Job.WeeklyReward != 0 || mid.Job.WeeklyReward != 0 {
		t.Fatal("the trader contributions were not reset")
	}
	if got := next.List(JobThief, JobRankContribution); !reflect.DeepEqual(got, previous.Lists[JobThief-1][JobRankContribution]) {
		t.Fatalf("a job without contributions replaced its list: %+v", got)
	}
}

/*
================
TestCloseJobWeekSplitsThePools

Each thief or hunter receives pool * contribution / total on top of the
reward they hold, capped at INT_MAX; the pool empties, even when nobody
contributed (the procedure's divide-by-zero still reaches the reset).
================
*/
func TestCloseJobWeekSplitsThePools(t *testing.T) {
	a := jobWeekMember("A", JobHunter, 1, 0, 1, 5)
	b := jobWeekMember("B", JobHunter, 1, 0, 2, 0)
	c := jobWeekMember("C", JobHunter, 1, 0, 0, 7)
	rich := jobWeekMember("Rich", JobThief, 1, 0, 1, math.MaxInt32-1)
	pool := TradeRewardPool{Hunters: 100, Thieves: 50}
	CloseJobWeek([]*Character{a, b, c, rich}, &pool, JobRankings{}, 1)
	if a.Job.Reward != 5+33 || b.Job.Reward != 66 || c.Job.Reward != 7 {
		t.Fatalf("hunter rewards %d %d %d", a.Job.Reward, b.Job.Reward, c.Job.Reward)
	}
	if rich.Job.Reward != math.MaxInt32 || pool != (TradeRewardPool{}) {
		t.Fatalf("thief reward %d, pool %+v", rich.Job.Reward, pool)
	}
	idle := jobWeekMember("Idle", JobHunter, 1, 0, 0, 3)
	pool = TradeRewardPool{Hunters: 40}
	CloseJobWeek([]*Character{idle}, &pool, JobRankings{}, 2)
	if idle.Job.Reward != 3 || pool.Hunters != 0 {
		t.Fatalf("an uncontributed pool: reward %d, pool %+v", idle.Job.Reward, pool)
	}
}
