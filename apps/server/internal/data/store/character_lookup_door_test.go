/*
===========================================================================

character_lookup_door_test.go - character lookups answer inside a door

===========================================================================
*/
package store

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
)

// lookupAnswerBound bounds a lookup made while the store lock is held; the
// published index takes no lock, so any wait at all is the defect.
const lookupAnswerBound = 5 * time.Second

/*
================
TestCharacterLookupsAnswerInsideADoor

findCharacterByGid runs inside UpdateCharacter and UpdateMany doors (kill
settlement, cures). The lookup must not take s.mu: with the write lock
held it answers, and a rename and a new character publish fresh indexes.
================
*/
func TestCharacterLookupsAnswerInsideADoor(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	c := &domain.Character{Name: "Seeker"}
	if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
		t.Fatal(err)
	}
	source := storeCharacterSource{s: s}
	s.mu.Lock()
	answered := make(chan *domain.Character, 1)
	go func() { answered <- source.CharacterByID(testDivision, c.ID) }()
	select {
	case found := <-answered:
		if found != c {
			t.Errorf("lookup inside the door found %v, want the created character", found)
		}
	case <-time.After(lookupAnswerBound):
		t.Error("CharacterByID waited for the store lock")
	}
	s.mu.Unlock()
	if err := s.RenameCharacterOffline(testDivision, "Seeker", "Finder"); err != nil {
		t.Fatal(err)
	}
	if source.CharacterByName(testDivision, "finder") != c || source.CharacterByName(testDivision, "Seeker") != nil {
		t.Fatal("the rename did not republish the name index")
	}
	other := &domain.Character{Name: "Second"}
	if err := s.CreateCharacter(testDivision, "test-account", other); err != nil {
		t.Fatal(err)
	}
	if source.CharacterByID(testDivision, other.ID) != other {
		t.Fatal("a new character is missing from the published index")
	}
}
