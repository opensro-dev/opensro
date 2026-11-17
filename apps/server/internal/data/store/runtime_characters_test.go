/*
===========================================================================

runtime_characters_test.go - actors share combat doors, not durable rosters

===========================================================================
*/
package store

import (
	"opensro.online/server/internal/domain"
	"testing"
)

/*
================
TestRuntimeActorsAreTransientAndUseTheSharedDoor
================
*/
func TestRuntimeActorsAreTransientAndUseTheSharedDoor(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	actor := &domain.Character{RuntimeOnly: true, ID: 10_000_000, Name: "[AI]Trader"}
	player := &domain.Character{Name: "realplayer"}
	if err := s.CreateCharacter(testDivision, "test-account", player); err != nil {
		t.Fatal(err)
	}
	source := storeCharacterSource{s: s}
	if err := source.RegisterRuntimeCharacters(testDivision, []*domain.Character{actor}); err != nil {
		t.Fatal(err)
	}
	if source.CharacterByID(testDivision, actor.ID) != actor {
		t.Fatal("actor is not targetable")
	}
	if !s.UpdateCharacters([]*domain.Character{player, actor}, "mixed actor hit", func() bool {
		actor.CurrentHP = int64Ptr(12)
		player.Gold = int64Ptr(123)
		player.CaravanRewardDay = 123
		player.CaravanRewardCount = 3
		return true
	}) {
		t.Fatal("mixed door refused")
	}
	if err := source.RegisterRuntimeCharacters(testDivision, []*domain.Character{actor}); err == nil {
		t.Fatal("duplicate registry accepted")
	}
	s.Close()
	reopened := openTest(t, dir, newTestClock())
	if got := reopened.Characters().CharactersForDivision(testDivision); len(got) != 1 || *got[0].Gold != 123 || got[0].CaravanRewardDay != 123 || got[0].CaravanRewardCount != 3 {
		t.Fatalf("transient actor persisted or real update lost: %+v", got)
	}
}
