package store

import (
	"errors"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
)

func TestCreateCharacterEnforcesPerShardAccountSlots(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	defer s.Close()

	for i, name := range []string{"One", "Two", "Three", "Four"} {
		character := seededCharacter()
		character.Name = name
		if err := s.CreateCharacter("alpha", "account", character); err != nil {
			t.Fatalf("create %d: %v", i+1, err)
		}
	}

	overflow := seededCharacter()
	overflow.Name = "Five"
	if err := s.CreateCharacter("alpha", "account", overflow); !errors.Is(err, ErrCharacterSlotsFull) {
		t.Fatalf("fifth create error = %v, want ErrCharacterSlotsFull", err)
	}
	if overflow.ID != 0 {
		t.Fatalf("refused character allocated id %d", overflow.ID)
	}

	otherShard := seededCharacter()
	otherShard.Name = "Five"
	if err := s.CreateCharacter("beta", "account", otherShard); err != nil {
		t.Fatalf("same account on another shard: %v", err)
	}
	otherAccount := seededCharacter()
	otherAccount.Name = "Other"
	if err := s.CreateCharacter("alpha", "other-account", otherAccount); err != nil {
		t.Fatalf("other account on same shard: %v", err)
	}
}

func TestDeletePendingCharacterStillOccupiesSlot(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	defer s.Close()

	var first *domain.Character
	for _, name := range []string{"One", "Two", "Three", "Four"} {
		character := seededCharacter()
		character.Name = name
		if err := s.CreateCharacter("alpha", "account", character); err != nil {
			t.Fatal(err)
		}
		if first == nil {
			first = character
		}
	}
	if !s.ReserveCharacterDeletion(first, "2026-07-30T00:00:00.000Z") {
		t.Fatal("delete reservation refused")
	}

	overflow := seededCharacter()
	overflow.Name = "Five"
	if err := s.CreateCharacter("alpha", "account", overflow); !errors.Is(err, ErrCharacterSlotsFull) {
		t.Fatalf("create with pending deletion = %v", err)
	}
}

func TestValidateShardStateRefusesUnknownAndOverflow(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	defer s.Close()

	character := seededCharacter()
	character.Name = "Known"
	if err := s.CreateCharacter("hidden", "account", character); err != nil {
		t.Fatal(err)
	}
	err := s.ValidateShardState([]string{"alpha"})
	if err == nil || !strings.Contains(err.Error(), "hidden") {
		t.Fatalf("unknown shard validation = %v", err)
	}

	if err := s.ValidateShardState([]string{"hidden"}); err != nil {
		t.Fatalf("known shard validation: %v", err)
	}
}
