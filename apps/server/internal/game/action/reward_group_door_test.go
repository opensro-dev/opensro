/*
===========================================================================

reward_group_door_test.go - character lists are taken before the read door

The store's read door holds its RWMutex for reading. A second read lock
from inside it parks behind any queued writer, and that writer waits for
the outer read door. The source below rejects list reads and nested doors
while the door is held, and requires eligibility checks inside the door.

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
	reads int
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
		if source.held {
			t.Fatal("nested character read door")
		}
		source.reads++
		source.held = true
		defer func() { source.held = false }()
		read()
	}
	return source
}

/*
================
checkRosterEligibilityUnderDoor

Nil and deleted characters must be filtered before presence is queried. The
offline character reaches presence but must not enter either output roster.
================
*/
func checkRosterEligibilityUnderDoor(t *testing.T, rt *Runtime, source *readDoorSource, present *enterworld.Character) {
	t.Helper()
	deleted, offline := present.Snapshot(), present.Snapshot()
	deleted.ID, deleted.Name, deleted.DeletePending = present.ID+1, "deleted-roster-member", true
	offline.ID, offline.Name = present.ID+2, "offline-roster-member"
	source.inner[testDivision] = []*enterworld.Character{nil, deleted, offline, present}
	presenceCalls := make(map[string]int)
	rt.RewardActorPresent = func(division, name string) bool {
		if !source.held {
			t.Fatal("presence eligibility evaluated outside the character read door")
		}
		if division != testDivision || name != present.Name && name != offline.Name {
			t.Fatalf("unexpected presence query: division=%q name=%q", division, name)
		}
		presenceCalls[name]++
		return name == present.Name
	}
	t.Cleanup(func() {
		if source.reads != 1 || source.held {
			t.Errorf("read door: callbacks=%d held=%t, want exactly one completed callback", source.reads, source.held)
		}
		if presenceCalls[present.Name] != 1 || presenceCalls[offline.Name] != 1 || len(presenceCalls) != 2 {
			t.Errorf("presence calls = %v, want present and offline once each", presenceCalls)
		}
	})
}

/*
================
TestRewardRosterReadsTheListOutsideTheDoor
================
*/
func TestRewardRosterReadsTheListOutsideTheDoor(t *testing.T) {
	c := testCharacter()
	rt, clock := newTestRuntime(c, testItems())
	source := installReadDoor(t, rt)
	checkRosterEligibilityUnderDoor(t, rt, source, c)
	roster := rt.monsterRewardRoster(testDivision, c, clock.NowMs())
	if len(roster.characters) != 1 || roster.characters[0] != c {
		t.Fatalf("roster = %+v, want the one present character", roster.characters)
	}
	if len(roster.actors) != 1 || roster.actors[enterworld.ObjectIDForCharacter(c)].character != c {
		t.Fatalf("reward actors = %+v, want only the present character", roster.actors)
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
	source := installReadDoor(t, rt)
	checkRosterEligibilityUnderDoor(t, rt, source, c)
	if players := rt.PopulationPlayers(testDivision, clock.NowMs()); len(players) != 1 || players[0].GID != enterworld.ObjectIDForCharacter(c) {
		t.Fatalf("population = %+v, want the one present character", players)
	}
}
