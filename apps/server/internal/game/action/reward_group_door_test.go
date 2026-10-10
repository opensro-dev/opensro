/*
===========================================================================

reward_group_door_test.go - character lists are taken before the read door

The store's read door holds its RWMutex for reading. A second read lock
from inside it parks behind any queued writer, and that writer waits for
the door: the 2026-10-10 11:10 freeze (a party pickup's reward roster
against a tick's UpdateCharacter). The source below fails a list read made
while the read door is held, instead of waiting for a writer to race it.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
readDoorSource

A character source that knows whether the read door is held.
================
*/
type readDoorSource struct {
	t     *testing.T
	inner enterworld.StaticCharacterSource
	held  bool
}

/*
================
CharactersForDivision
================
*/
func (s *readDoorSource) CharactersForDivision(division string) []*enterworld.Character {
	if s.held {
		s.t.Error("CharactersForDivision read the store inside the read door")
	}
	return s.inner.CharactersForDivision(division)
}

/*
================
CharacterByName
================
*/
func (s *readDoorSource) CharacterByName(division, name string) *enterworld.Character {
	if s.held {
		s.t.Error("CharacterByName read the store inside the read door")
	}
	for _, c := range s.inner[division] {
		if c != nil && c.Name == name {
			return c
		}
	}
	return nil
}

/*
================
CharacterByID
================
*/
func (s *readDoorSource) CharacterByID(division string, id int64) *enterworld.Character {
	if s.held {
		s.t.Error("CharacterByID read the store inside the read door")
	}
	for _, c := range s.inner[division] {
		if c != nil && c.ID == id {
			return c
		}
	}
	return nil
}

/*
================
installReadDoor
================
*/
func installReadDoor(t *testing.T, rt *Runtime) *readDoorSource {
	t.Helper()
	deps := rt.deps.(*enterworld.Deps)
	source := &readDoorSource{t: t, inner: fixtureCharacters(deps.Characters)}
	deps.Characters = source
	deps.ReadCharacter = func(_ string, read func()) {
		source.held = true
		defer func() { source.held = false }()
		read()
	}
	return source
}

/*
================
TestRewardRosterReadsTheListOutsideTheDoor
================
*/
func TestRewardRosterReadsTheListOutsideTheDoor(t *testing.T) {
	c := testCharacter()
	rt, clock := newTestRuntime(c, testItems())
	installReadDoor(t, rt)
	rt.RewardActorPresent = func(string, string) bool { return true }
	roster := rt.monsterRewardRoster(testDivision, c, clock.NowMs())
	if len(roster.characters) != 1 || roster.characters[0] != c {
		t.Fatalf("roster = %+v, want the one present character", roster.characters)
	}
}

/*
================
TestPopulationReadsTheListOutsideTheDoor
================
*/
func TestPopulationReadsTheListOutsideTheDoor(t *testing.T) {
	c := testCharacter()
	rt, clock := newTestRuntime(c, testItems())
	installReadDoor(t, rt)
	rt.RewardActorPresent = func(string, string) bool { return true }
	if players := rt.PopulationPlayers(testDivision, clock.NowMs()); len(players) != 1 {
		t.Fatalf("population = %+v, want the one present character", players)
	}
}
