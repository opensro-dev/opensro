/*
===========================================================================

character_byid_test.go - the roster's lookup by id against the roster

characterByIDLocked answers from the published index, not from a walk of
the roster. These tests hold the two to the same answer through every
roster change, so an index a writer forgot to rebuild fails here and not
as a guild member the live game cannot find.

===========================================================================
*/

package store

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
)

/*
================
rosterCharacterByID

The walk the lookup replaced: the first live roster row with this id.
================
*/
func rosterCharacterByID(s *Store, divisionID string, characterID int64) *domain.Character {
	for _, c := range s.characters[divisionID] {
		if c != nil && c.ID == characterID {
			return c
		}
	}
	return nil
}

/*
================
requireByIDMatchesRoster

Every id from 0 past the watermark answers as the roster walk does:
the ids that exist, the ones reaped, and the ones never issued.
================
*/
func requireByIDMatchesRoster(t *testing.T, s *Store, stage string, highest int64) {
	t.Helper()
	for id := int64(0); id <= highest+2; id++ {
		want := rosterCharacterByID(s, testDivision, id)
		if got := s.characterByIDLocked(testDivision, id); got != want {
			t.Fatalf("%s: id %d answered %p, the roster holds %p", stage, id, got, want)
		}
	}
}

/*
================
TestCharacterByIDFollowsTheRoster
================
*/
func TestCharacterByIDFollowsTheRoster(t *testing.T) {
	t.Parallel()

	clock := newTestClock()
	dir := t.TempDir()
	s := openTest(t, dir, clock)
	requireByIDMatchesRoster(t, s, "empty", 0)

	var created []*domain.Character
	for _, name := range []string{"Alpha", "Bravo", "Charlie"} {
		c := &domain.Character{Name: name}
		if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
			t.Fatal(err)
		}
		created = append(created, c)
		if s.characterByIDLocked(testDivision, c.ID) != c {
			t.Fatalf("created %s is not found by its id %d", name, c.ID)
		}
	}
	highest := created[len(created)-1].ID
	requireByIDMatchesRoster(t, s, "after create", highest)
	if s.characterByIDLocked("different-shard", created[0].ID) != nil {
		t.Fatal("an id answered across divisions")
	}

	if err := s.RenameCharacterOffline(testDivision, "Bravo", "Delta"); err != nil {
		t.Fatal(err)
	}
	if s.characterByIDLocked(testDivision, created[1].ID) != created[1] {
		t.Fatal("a rename lost the character's id")
	}
	requireByIDMatchesRoster(t, s, "after rename", highest)

	victim := created[0]
	s.MutateCharacter(victim, "reserve", func() {
		victim.DeletePending = true
		victim.DeleteReservedAt = clock.Now().Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339)
	})
	if len(s.ReapMaturedDeletions()) != 1 {
		t.Fatal("fixture did not reap")
	}
	if s.characterByIDLocked(testDivision, victim.ID) != nil {
		t.Fatal("a reaped character still answers by id")
	}
	requireByIDMatchesRoster(t, s, "after reap", highest)

	// A load publishes the index too: the reopened store answers for
	// the survivors before any roster change of its own.
	s.Close()
	reopened := openTest(t, dir, clock)
	for _, c := range created[1:] {
		found := reopened.characterByIDLocked(testDivision, c.ID)
		if found == nil || found.ID != c.ID {
			t.Fatalf("the reopened store does not find id %d", c.ID)
		}
	}
	requireByIDMatchesRoster(t, reopened, "after load", highest)
}
