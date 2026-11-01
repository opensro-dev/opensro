/*
===========================================================================

cosownerindex_test.go - the pet gid to owner index contract (#2138)

The index must track the four ways a pet's runtime life changes:
summon (the bind door), despawn (the retire door), owner logout
(session forget) and owner region change (forget in the old
division, bind in the new). Two guards ride along: a stale entry
never resolves to the wrong owner, and a gid reused by another
owner's pet resolves to the new owner. The lifecycle phases assert
the index itself (rt.cosOwners), not only the lookup, so they
cannot pass on the fallback walk alone.

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

// cosOwnerIndexCharacter builds an owner carrying one summoned pet.
func cosOwnerIndexCharacter(name string, id int64, gid uint32) *enterworld.Character {
	c := testCharacter()
	c.ID, c.Name = id, name
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true}
	return c
}

// cosOwnerIndexRuntime builds a runtime over a literal division roster,
// the same fixture shape newTestRuntime uses for one character.
func cosOwnerIndexRuntime(roster map[string][]*enterworld.Character) *Runtime {
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource(roster),
		Items:      testItems(),
		NpcSpawns:  enterworld.NpcSpawnConfig{Roster: simulation.DefaultNpcRoster()},
	}
	rt := NewRuntime(deps, nil)
	clock := &fakeClock{now: time.UnixMilli(1_000_000)}
	rt.Now = clock.Now
	return rt
}

/*
================
TestCosOwnerIndexBindNotesSummonedPet

World entry binds the owner's summoned companions: the bind door
notes each in the index, and the lookup then resolves from it. A
companion that is not summoned is never noted.
================
*/
func TestCosOwnerIndexBindNotesSummonedPet(t *testing.T) {
	owner := cosOwnerIndexCharacter("binder", 11, 777)
	dormant := cosOwnerIndexCharacter("dormant", 12, 778)
	dormant.ActiveCOS.Summoned = false
	rt := cosOwnerIndexRuntime(map[string][]*enterworld.Character{testDivision: {owner, dormant}})

	rt.BindPetSession(testDivision, owner, 101)
	rt.BindPetSession(testDivision, dormant, 102)

	if noted := rt.cosOwners.owner(testDivision, 777); noted != owner {
		t.Fatalf("bound pet's index entry = %v, want its owner", noted)
	}
	if got := rt.characterByCosGID(testDivision, 777); got != owner {
		t.Fatalf("characterByCosGID = %v, want the bound owner", got)
	}
	if noted := rt.cosOwners.owner(testDivision, 778); noted != nil {
		t.Fatalf("unsummoned pet was noted: %v", noted)
	}
}

/*
================
TestCosOwnerIndexRetireForgetsPet

A despawn retires the pet through retireCosRuntime (every release
path ends there): the entry is forgotten, and the lookup no longer
resolves the gid.
================
*/
func TestCosOwnerIndexRetireForgetsPet(t *testing.T) {
	owner := cosOwnerIndexCharacter("retirer", 13, 779)
	rt := cosOwnerIndexRuntime(map[string][]*enterworld.Character{testDivision: {owner}})
	rt.BindPetSession(testDivision, owner, 101)

	owner.ActiveCOS.Summoned = false
	rt.retireCosRuntime(testDivision, owner, 779)

	if noted := rt.cosOwners.owner(testDivision, 779); noted != nil {
		t.Fatalf("retired pet still noted: %v", noted)
	}
	if got := rt.characterByCosGID(testDivision, 779); got != nil {
		t.Fatalf("characterByCosGID after retire = %v, want nil", got)
	}
}

/*
================
TestCosOwnerIndexLogoutForgetsOwner

Owner logout forgets the owner's sessions: every index entry naming
that character in the division goes with them.
================
*/
func TestCosOwnerIndexLogoutForgetsOwner(t *testing.T) {
	owner := cosOwnerIndexCharacter("leaver", 14, 780)
	rt := cosOwnerIndexRuntime(map[string][]*enterworld.Character{testDivision: {owner}})
	rt.BindPetSession(testDivision, owner, 101)

	rt.forgetPetSession(testDivision, owner.Name)

	if noted := rt.cosOwners.owner(testDivision, 780); noted != nil {
		t.Fatalf("logged-out owner's pet still noted: %v", noted)
	}
}

/*
================
TestCosOwnerIndexRegionChangeMovesEntry

A region change is session teardown in the old division and a bind
in the new: the entry moves with the owner and the old division no
longer resolves the gid.
================
*/
func TestCosOwnerIndexRegionChangeMovesEntry(t *testing.T) {
	const nextDivision = "other-division"
	owner := cosOwnerIndexCharacter("mover", 15, 781)
	rt := cosOwnerIndexRuntime(map[string][]*enterworld.Character{
		testDivision: {owner},
		nextDivision: {owner},
	})
	rt.BindPetSession(testDivision, owner, 101)

	rt.forgetPetSession(testDivision, owner.Name)
	rt.BindPetSession(nextDivision, owner, 101)

	if noted := rt.cosOwners.owner(testDivision, 781); noted != nil {
		t.Fatalf("old division still notes the pet: %v", noted)
	}
	if noted := rt.cosOwners.owner(nextDivision, 781); noted != owner {
		t.Fatalf("new division's index entry = %v, want the owner", noted)
	}
	if got := rt.characterByCosGID(nextDivision, 781); got != owner {
		t.Fatalf("characterByCosGID in the new division = %v, want the owner", got)
	}
}

/*
================
TestCosOwnerIndexStaleEntryNeverResolves

If a retirement door is ever missed, the index can vouch for a pet
the owner no longer holds. The lookup verifies its hit against the
owner's records, so the stale entry resolves to nobody and heals.
================
*/
func TestCosOwnerIndexStaleEntryNeverResolves(t *testing.T) {
	owner := cosOwnerIndexCharacter("stale", 16, 782)
	rt := cosOwnerIndexRuntime(map[string][]*enterworld.Character{testDivision: {owner}})
	rt.BindPetSession(testDivision, owner, 101)

	// The missed door: the pet leaves without a retirement.
	owner.ActiveCOS.Summoned = false

	if got := rt.characterByCosGID(testDivision, 782); got != nil {
		t.Fatalf("stale entry resolved to %v, want nil", got)
	}
	if noted := rt.cosOwners.owner(testDivision, 782); noted != nil {
		t.Fatalf("stale entry survived its failed verification: %v", noted)
	}
}

/*
================
TestCosOwnerIndexReusedGIDResolvesNewOwner

Gids are reused. After the first owner's pet retires, another
owner's pet bound under the same gid resolves to the new owner.
================
*/
func TestCosOwnerIndexReusedGIDResolvesNewOwner(t *testing.T) {
	first := cosOwnerIndexCharacter("first", 17, 783)
	second := cosOwnerIndexCharacter("second", 18, 783)
	rt := cosOwnerIndexRuntime(map[string][]*enterworld.Character{testDivision: {first, second}})

	rt.BindPetSession(testDivision, first, 101)
	first.ActiveCOS.Summoned = false
	rt.retireCosRuntime(testDivision, first, 783)
	rt.BindPetSession(testDivision, second, 102)

	if got := rt.characterByCosGID(testDivision, 783); got != second {
		t.Fatalf("reused gid resolved to %v, want the new owner", got)
	}
}

/*
================
TestCosOwnerIndexWalkNotesWhatItFinds

A pet nobody bound (the shape behavior tests build directly) still
resolves through the authoritative division walk, and the walk
notes its answer so the next lookup is the index hit.
================
*/
func TestCosOwnerIndexWalkNotesWhatItFinds(t *testing.T) {
	owner := cosOwnerIndexCharacter("unbound", 19, 784)
	rt := cosOwnerIndexRuntime(map[string][]*enterworld.Character{testDivision: {owner}})

	if got := rt.characterByCosGID(testDivision, 784); got != owner {
		t.Fatalf("walk resolved %v, want the owner", got)
	}
	if noted := rt.cosOwners.owner(testDivision, 784); noted != owner {
		t.Fatalf("walk did not note its answer: %v", noted)
	}
}
