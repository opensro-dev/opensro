package store

import (
	"testing"

	"opensro.online/server/internal/domain"
)

func TestCriminalRecordPersistsThroughCharacterDoor(t *testing.T) {
	t.Parallel()

	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	c := &domain.Character{Name: "PKFixture"}
	if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
		t.Fatal(err)
	}
	s.MutateCharacter(c, "criminal-record-fixture", func() {
		c.PK = &domain.PKRecord{DailyCount: 3, TotalCount: 5, Penalty: 3600}
		c.Aggressions = map[uint32]uint32{100003: 20}
		c.EventMembership = &domain.EventMembership{ID: 7, Team: 0}
	})
	s.Close()
	reopened := openTest(t, dir, clock)
	for _, loaded := range reopened.Characters().CharactersForDivision(testDivision) {
		if loaded.Name != c.Name {
			continue
		}
		if loaded.PK == nil || *loaded.PK != *c.PK {
			t.Fatalf("criminal record lost at persistence boundary: %+v", loaded.PK)
		}
		if loaded.EventTeam() != 255 || len(loaded.Aggressions) != 0 {
			t.Fatal("process-local event or aggression resurrected after restart")
		}
		return
	}
	t.Fatal("character missing after reopen")
}
