/*
===========================================================================

meta_skip_test.go - the commit does not rewrite a meta plane whose
bytes did not change

commitTxLocked marshals the trade-reward and job-ranking planes whole
on every commit, changed or not, beside the two version stamps. These
tests pin the digest skip (#1943 W2, the character rows' discipline of
commit_skip_test.go against the meta keys): a same-value commit writes
no meta row, a changed plane is written, and a failed commit retains
no digest that would make a later commit skip a row the disk lacks.

===========================================================================
*/

package store

import (
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestCommitSkipsUnchangedMetaPlanes
================
*/
func TestCommitSkipsUnchangedMetaPlanes(t *testing.T) {
	s, c := commitSkipFixture(t)
	s.meta.TradeRewards = map[string]domain.TradeRewardPool{testDivision: {Hunters: 3}}
	s.meta.JobRankings = map[string]domain.JobRankings{testDivision: {}}

	// The first commit writes both planes (the version stamps were
	// already retained by the fixture's create commit and skip).
	before := s.LockTiming()
	s.MutateCharacter(c, "noop touch", func() {})
	after := s.LockTiming()
	if got := after.MetaRowsWritten - before.MetaRowsWritten; got != 2 {
		t.Fatalf("first meta commit wrote %d rows, want the 2 planes (skipped %d)", got, after.MetaRowsSkipped-before.MetaRowsSkipped)
	}

	// A commit that changes nothing writes no meta row at all: both
	// planes and both stamps skip against their retained digests.
	before = after
	s.MutateCharacter(c, "noop touch again", func() {})
	after = s.LockTiming()
	if got := after.MetaRowsWritten - before.MetaRowsWritten; got != 0 {
		t.Fatalf("unchanged commit wrote %d meta rows, want 0", got)
	}
	if got := after.MetaRowsSkipped - before.MetaRowsSkipped; got != 4 {
		t.Fatalf("unchanged commit skipped %d meta rows, want 4 (2 planes, 2 stamps)", got)
	}

	// A changed plane is written; the rest still skip.
	before = after
	if !s.UpdateTrade([]*domain.Character{c}, "pool change", func(pool *domain.TradeRewardPool) bool {
		return pool.Credit(domain.JobThief, 5)
	}) {
		t.Fatal("pool change refused")
	}
	after = s.LockTiming()
	if got := after.MetaRowsWritten - before.MetaRowsWritten; got != 1 {
		t.Fatalf("pool change wrote %d meta rows, want 1 (skipped %d)", got, after.MetaRowsSkipped-before.MetaRowsSkipped)
	}

	// The skipped writes never lose a real change: after a reopen the
	// credited pool is what the disk holds.
	s.Close()
	s2, err := Open(s.dir, Options{Now: newTestClock().Now, DefaultSkills: testSkillSeeder})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if got := s2.meta.TradeRewards[testDivision]; got != (domain.TradeRewardPool{Hunters: 3, Thieves: 5}) {
		t.Fatalf("reloaded pool = %+v, want the credited one", got)
	}
}

/*
================
TestFailedCommitRetainsNoMetaDigest

A commit that fails after staging a plane's digest must retain
nothing: the next commit writes the plane even though its bytes match
the failed attempt's.
================
*/
func TestFailedCommitRetainsNoMetaDigest(t *testing.T) {
	s, c := commitSkipFixture(t)
	s.meta.TradeRewards = map[string]domain.TradeRewardPool{testDivision: {Hunters: 7}}

	s.beforeSQLiteCommit = func() { panic("injected commit failure") }
	func() {
		defer func() { _ = recover() }()
		s.MutateCharacter(c, "doomed pool", func() {})
	}()
	s.beforeSQLiteCommit = nil

	before := s.LockTiming()
	s.MutateCharacter(c, "noop after failure", func() {})
	after := s.LockTiming()
	if got := after.MetaRowsWritten - before.MetaRowsWritten; got == 0 {
		t.Fatal("commit after failure wrote no meta rows: the failed attempt must retain no digest")
	}
}
