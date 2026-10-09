/*
===========================================================================

jobweek_test.go - the weekly job close commits and survives a reopen

===========================================================================
*/
package store

import (
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestCloseJobWeekCommitsTheSnapshot

The first week only records itself; the next closes it: the hunter's
share of the pool, the contribution list and its reset, all persisted.
================
*/
func TestCloseJobWeekCommitsTheSnapshot(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	c := seededCharacter()
	c.Job = domain.CharacterJob{Type: domain.JobHunter, Grade: 2, Exp: 40, Alias: "Watch", WeeklyReward: 9}
	if err := s.CreateCharacter(testDivision, "account", c); err != nil {
		t.Fatal(err)
	}
	if !s.CloseJobWeek(testDivision, 100, "job-week") || s.JobRankings(testDivision).Week != 100 {
		t.Fatalf("the first sighting = %+v", s.JobRankings(testDivision))
	}
	if s.CloseJobWeek(testDivision, 100, "job-week") {
		t.Fatal("a week closed twice")
	}
	member := s.Characters().CharactersForDivision(testDivision)[0]
	if !s.UpdateTrade([]*domain.Character{member}, "pool", func(p *domain.TradeRewardPool) bool { p.Hunters = 500; return true }) {
		t.Fatal("pool credit refused")
	}
	if !s.CloseJobWeek(testDivision, 101, "job-week") {
		t.Fatal("week 101 did not close")
	}
	s.Close()
	reopened := openTest(t, dir, newTestClock())
	ranks := reopened.JobRankings(testDivision)
	got := reopened.Characters().CharactersForDivision(testDivision)[0].Job
	if ranks.Week != 101 || len(ranks.List(domain.JobHunter, domain.JobRankContribution)) != 1 ||
		got.Reward != 500 || got.WeeklyReward != 0 || reopened.TradeRewards(testDivision).Hunters != 0 {
		t.Fatalf("after reopen: ranks %+v, job %+v, pool %+v", ranks, got, reopened.TradeRewards(testDivision))
	}
}
