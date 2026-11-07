package store

import (
	"fmt"
	"opensro.online/server/internal/domain"
	"strings"
	"testing"
	"time"
)

func TestCharacterLookupInvalidatesAfterIdentityLifecycle(t *testing.T) {
	t.Parallel()

	clock := newTestClock()
	s := openTest(t, t.TempDir(), clock)
	lookup := s.Characters().(domain.CharacterLookup)
	if lookup.CharacterByName(testDivision, "alpha") != nil {
		t.Fatal("unexpected initial record")
	}
	c := &domain.Character{Name: "Alpha"}
	if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
		t.Fatal(err)
	}
	if lookup.CharacterByName(testDivision, "ALPHA") != c || lookup.CharacterByID(testDivision, c.ID) != c {
		t.Fatal("create not indexed with stable identity")
	}
	if lookup.CharacterByName("different-shard", "alpha") != nil {
		t.Fatal("cross-shard character leak")
	}
	if err := s.RenameCharacterOffline(testDivision, "Alpha", "Bravo"); err != nil {
		t.Fatal(err)
	}
	if lookup.CharacterByName(testDivision, "alpha") != nil || lookup.CharacterByName(testDivision, "BRAVO") != c {
		t.Fatal("rename left stale index")
	}
	s.MutateCharacter(c, "reserve", func() {
		c.DeletePending = true
		c.DeleteReservedAt = clock.Now().Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339)
	})
	backing := s.characters[testDivision]
	if len(s.ReapMaturedDeletions()) != 1 {
		t.Fatal("fixture did not reap")
	}
	if lookup.CharacterByName(testDivision, "bravo") != nil || lookup.CharacterByID(testDivision, c.ID) != nil {
		t.Fatal("deleted character remained indexed")
	}
	if backing[0] != nil {
		t.Fatal("compacted slice retained deleted record")
	}
}

func TestCharacterLookupSimpleFoldMatchesLegacy(t *testing.T) {
	t.Parallel()

	names := []string{"Kelvin", "Kelvin", "Σigma", "ςigma", "σIGMA", "Silkroad", "SILKROAD", "丝路", "other"}
	for _, a := range names {
		for _, b := range names {
			if (characterLookupName(a) == characterLookupName(b)) != strings.EqualFold(a, b) {
				t.Fatalf("fold mismatch %q / %q", a, b)
			}
		}
	}
}

func BenchmarkCharacterLookup1000(b *testing.B) {
	s := &Store{characters: map[string][]*domain.Character{}}
	for i := 1; i <= 1000; i++ {
		s.characters["world"] = append(s.characters["world"], &domain.Character{ID: int64(i), Name: fmt.Sprintf("Player%04d", i)})
	}
	source := storeCharacterSource{s: s}
	source.CharacterByName("world", "player1000")
	b.Run("scan-copy", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var found *domain.Character
			for _, c := range source.CharactersForDivision("world") {
				if strings.EqualFold(c.Name, "player1000") {
					found = c
					break
				}
			}
			if found == nil {
				b.Fatal("missing")
			}
		}
	})
	b.Run("indexed-name", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if source.CharacterByName("world", "player1000") == nil {
				b.Fatal("missing")
			}
		}
	})
	b.Run("indexed-id", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if source.CharacterByID("world", 1000) == nil {
				b.Fatal("missing")
			}
		}
	})
}

func BenchmarkCharacterReadDoor1000(b *testing.B) {
	s := &Store{characters: map[string][]*domain.Character{"world": make([]*domain.Character, 1000)}}
	b.Run("discarded-roster", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			s.ReadCharacters("world", func([]*domain.Character) {})
		}
	})
	b.Run("state-only", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			s.ReadState(func() {})
		}
	})
}
