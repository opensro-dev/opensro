package store

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
)

func TestCharacterSelectionDeletionBlockers(t *testing.T) {
	t.Parallel()

	s := &Store{
		characters:   map[string][]*domain.Character{"test": {{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}, {ID: 5}}},
		guildMembers: map[string]map[int64][]domain.GuildMemberRecord{"test": {10: {{CharID: 1, Grade: 0}, {CharID: 2, Grade: 1}}}},
		campMembers:  map[string]map[int64][]domain.TrainingCampMemberRecord{"test": {20: {{CharID: 1, Kind: 0}, {CharID: 3, Kind: 1}, {CharID: 4, Kind: 2}}}},
	}
	s.ReadCharacterSelection("test", func(rows []*domain.Character, blockers map[int64]string) {
		if len(rows) != 5 || !reflect.DeepEqual(blockers, map[int64]string{1: "guild-master", 2: "guild-member", 3: "academy-guardian", 4: "academy-student"}) {
			t.Fatalf("unexpected selection blockers: %v", blockers)
		}
		rows[0] = nil
	})
	if s.characters["test"][0] == nil {
		t.Fatal("selection leaked its backing slice")
	}
	s.ReadCharacterSelection("other", func(rows []*domain.Character, blockers map[int64]string) {
		if len(rows) != 0 || len(blockers) != 0 {
			t.Fatal("selection leaked another division")
		}
	})
}
