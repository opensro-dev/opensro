/*
===========================================================================

commit_skip_test.go - the commit does not rewrite a character whose
bytes did not change

Doors mark characters dirty with no change signal, so the commit is
the first place that can see a touch changed nothing. These tests pin
the digest skip in commitTxLocked: a same-value commit writes no
character row, a changed one does, and a failed commit retains no
digest that would make a later commit skip a row the disk lacks.

===========================================================================
*/

package store

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
commitSkipFixture
================
*/
func commitSkipFixture(t *testing.T) (*Store, *enterworld.Character) {
	t.Helper()
	s, err := Open(t.TempDir(), Options{Now: newTestClock().Now, DefaultSkills: testSkillSeeder})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	c := &enterworld.Character{Name: "Skipper", Gold: int64Ptr(100)}
	if err := s.CreateCharacter(testDivision, "skip-account", c); err != nil {
		t.Fatal(err)
	}
	return s, c
}

/*
================
TestCommitSkipsUnchangedCharacterRows
================
*/
func TestCommitSkipsUnchangedCharacterRows(t *testing.T) {
	s, c := commitSkipFixture(t)

	before := s.LockTiming()
	s.MutateCharacter(c, "noop touch", func() {})
	after := s.LockTiming()
	if got := after.CharacterRowsSkipped - before.CharacterRowsSkipped; got != 1 {
		t.Fatalf("unchanged commit skipped %d rows, want 1 (written %d)", got, after.CharacterRowsWritten-before.CharacterRowsWritten)
	}
	if got := after.CharacterRowsWritten - before.CharacterRowsWritten; got != 0 {
		t.Fatalf("unchanged commit wrote %d rows, want 0", got)
	}

	before = after
	s.MutateCharacter(c, "gold change", func() { c.Gold = int64Ptr(*c.Gold + 1) })
	after = s.LockTiming()
	if got := after.CharacterRowsWritten - before.CharacterRowsWritten; got != 1 {
		t.Fatalf("changed commit wrote %d rows, want 1 (skipped %d)", got, after.CharacterRowsSkipped-before.CharacterRowsSkipped)
	}

	// The skipped write never loses a real change: after a reopen the
	// changed gold is what the disk holds.
	s.Close()
	s2, err := Open(s.dir, Options{Now: newTestClock().Now, DefaultSkills: testSkillSeeder})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	var loaded *enterworld.Character
	s2.ReadCharacters(testDivision, func(characters []*enterworld.Character) {
		for _, candidate := range characters {
			if candidate.Name == c.Name {
				loaded = candidate
			}
		}
	})
	if loaded == nil || *loaded.Gold != *c.Gold {
		t.Fatalf("reloaded gold = %v, want %d", loaded, *c.Gold)
	}
}

/*
================
TestFailedCommitRetainsNoDigest

A commit that fails must not stage a digest: the next commit writes
the row even though its bytes match the failed attempt's.
================
*/
func TestFailedCommitRetainsNoDigest(t *testing.T) {
	s, c := commitSkipFixture(t)

	// Fail inside the transaction, after the character row was staged:
	// the rollback must take the staged digest with it.
	s.beforeSQLiteCommit = func() { panic("injected commit failure") }
	func() {
		defer func() { _ = recover() }()
		s.MutateCharacter(c, "doomed gold", func() { c.Gold = int64Ptr(*c.Gold + 5) })
	}()
	s.beforeSQLiteCommit = nil

	before := s.LockTiming()
	s.MutateCharacter(c, "noop after failure", func() {})
	after := s.LockTiming()
	if got := after.CharacterRowsWritten - before.CharacterRowsWritten; got != 1 {
		t.Fatalf("commit after failure wrote %d rows, want 1: the failed attempt must retain no digest (skipped %d)", got, after.CharacterRowsSkipped-before.CharacterRowsSkipped)
	}
}
