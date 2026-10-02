/*
===========================================================================

skillcure_door_test.go - cures resolve their targets before the caster's door

The authority store's character door is one non-reentrant RWMutex: every
character read (CharacterByID, CharactersForDivision, ReadState) takes its
read lock, and Go never grants that to the goroutine already holding the
write lock. A cure that resolved its action vector inside the caster's
Update callback therefore waited on its own door and wedged the shard.

The door below models that lock without blocking: a read attempted while
the write lock is held fails the test instead of hanging it.

===========================================================================
*/

package action

import (
	"sync"
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
storeDoor

One RWMutex shared by the character doors and the character source, as in
the authority store. Writers lock it; readers only try, and report a read
that the real store would have parked forever.
================
*/
type storeDoor struct {
	t     *testing.T
	mu    sync.RWMutex
	inner enterworld.StaticCharacterSource
}

/*
================
installStoreDoor

Route the fixture's doors and its character source through one store lock.
================
*/
func installStoreDoor(t *testing.T, rt *Runtime) *storeDoor {
	t.Helper()
	deps := rt.deps.(*enterworld.Deps)
	door := &storeDoor{t: t, inner: deps.Characters.(enterworld.StaticCharacterSource)}
	deps.Characters = door
	deps.UpdateCharacter = func(_ *enterworld.Character, _ string, update func() bool) bool {
		door.mu.Lock()
		defer door.mu.Unlock()
		return update()
	}
	deps.MutateCharacter = func(_ *enterworld.Character, _ string, mutate func()) {
		door.mu.Lock()
		defer door.mu.Unlock()
		mutate()
	}
	deps.ReadCharacter = func(_ string, read func()) {
		defer door.read("ReadCharacter")()
		read()
	}
	return door
}

/*
================
read

Take the read lock if it is free. A held write lock means the caller is
inside a door: the real store would deadlock here, so fail and go on.
================
*/
func (d *storeDoor) read(what string) func() {
	if !d.mu.TryRLock() {
		d.t.Errorf("%s read the store inside a character door", what)
		return func() {}
	}
	return d.mu.RUnlock
}

/*
================
CharactersForDivision
================
*/
func (d *storeDoor) CharactersForDivision(division string) []*enterworld.Character {
	defer d.read("CharactersForDivision")()
	return d.inner.CharactersForDivision(division)
}

/*
================
CharacterByID
================
*/
func (d *storeDoor) CharacterByID(division string, id int64) *enterworld.Character {
	defer d.read("CharacterByID")()
	for _, c := range d.inner[division] {
		if c != nil && c.ID == id {
			return c
		}
	}
	return nil
}

/*
================
CharacterByName
================
*/
func (d *storeDoor) CharacterByName(division, name string) *enterworld.Character {
	defer d.read("CharacterByName")()
	for _, c := range d.inner[division] {
		if c != nil && c.Name == name {
			return c
		}
	}
	return nil
}

/*
================
addDoorMate

A second player beside the caster, burning, in the store's division.
================
*/
func addDoorMate(t *testing.T, rt *Runtime, caster *enterworld.Character, door *storeDoor, now int64, source uint32) *enterworld.Character {
	t.Helper()
	mate := *caster
	mate.ID, mate.Name = 4, "mate"
	mate.CurrentHP, mate.CurrentMP = testInt64(100), testInt64(10000)
	door.inner[testDivision] = append(door.inner[testDivision], &mate)
	seedPlayerStatus(rt, &mate, abnormal.Frostbite, 300000, now, source)
	return &mate
}

/*
================
TestTargetedCureOnAnotherPlayerOutsideTheDoor

Innocent cast on a party mate: the target gid resolves through the store
before the caster's door, the mate is cured and receives its snapshot.
================
*/
func TestTargetedCureOnAnotherPlayerOutsideTheDoor(t *testing.T) {
	rt, clock, caster, monster := newCombatTestRuntime(t, 100)
	row := installShippedSkill(t, rt, caster, 10077)
	caster.Intellect = testInt64(80)
	caster.CurrentMP = nil
	now := clock.NowMs()
	door := installStoreDoor(t, rt)
	mate := addDoorMate(t, rt, caster, door, now, monster.Gid)
	rt.clearSkillFinalizes(testDivision, caster.Name)

	result := rt.HandleTargetInteract(testDivision, caster, wire.SkillAction{
		ActionId: row.ID, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(mate),
	}.Encode())
	if result.DiagnosticRefusal != "" {
		t.Fatal(result.DiagnosticRefusal)
	}
	if got := rt.playerAbnormal(testDivision, mate.Name).Slots[abnormal.Frostbite].StartedAt; got != now-int64(48*250) {
		t.Fatalf("mate frostbite start %d, want %d", got, now-int64(48*250))
	}
	delivered := false
	for _, recipient := range result.Recipients {
		if recipient.CharacterID == mate.ID && hasOpcode(recipient.Frames, 0x36C7) {
			delivered = true
		}
	}
	if !delivered {
		t.Fatalf("mate snapshot not routed: %+v", result.Recipients)
	}
}

/*
================
TestAreaCureResolvesPartyOutsideTheDoor

Innocent B's party selection (58BEF0) reads every member through the store;
it must run before the caster's door, the caster first (include-self bit).
================
*/
func TestAreaCureResolvesPartyOutsideTheDoor(t *testing.T) {
	rt, clock, caster, monster := newCombatTestRuntime(t, 100)
	row := installShippedSkill(t, rt, caster, 10088)
	caster.Intellect = testInt64(80)
	caster.CurrentMP = nil
	now := clock.NowMs()
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	door := installStoreDoor(t, rt)
	seedPlayerStatus(rt, caster, abnormal.Burn, 300000, now, monster.Gid)
	mate := addDoorMate(t, rt, caster, door, now, monster.Gid)
	casterGID := enterworld.ObjectIDForCharacter(caster)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{casterGID, enterworld.ObjectIDForCharacter(mate)}}}
	}

	result := rt.HandleTargetInteract(testDivision, caster, wire.SkillAction{ActionId: row.ID}.Encode())
	if result.DiagnosticRefusal != "" {
		t.Fatal(result.DiagnosticRefusal)
	}
	cut := int64(row.Abnormal.CurtLevel) * 750
	if got := rt.playerAbnormal(testDivision, caster.Name).Slots[abnormal.Burn].StartedAt; got != now-cut {
		t.Fatalf("caster burn start %d, want %d", got, now-cut)
	}
	if !hasOpcode(result.Frames, 0x36C7) {
		t.Fatal("caster did not receive its own snapshot")
	}
	delivered := false
	for _, recipient := range result.Recipients {
		if recipient.CharacterID == mate.ID && hasOpcode(recipient.Frames, 0x36C7) {
			delivered = true
		}
	}
	if !delivered {
		t.Fatalf("mate snapshot not routed: %+v", result.Recipients)
	}
}

/*
================
TestPetCureResolvesOwnerOutsideTheDoor

A pet gid misses the player index and falls to the owner scan
(CharactersForDivision): that scan must also run before the door.
================
*/
func TestPetCureResolvesOwnerOutsideTheDoor(t *testing.T) {
	const petGID = uint32(9001)
	rt, clock, caster, monster := newCombatTestRuntime(t, 100)
	row := installShippedSkill(t, rt, caster, 10077)
	caster.Intellect = testInt64(80)
	caster.CurrentMP = nil
	now := clock.NowMs()
	caster.ActiveCOS = &enterworld.CharacterCOS{GID: petGID, CurrentHP: 100, Summoned: true}
	petRecord := abnormal.Record{Status: abnormal.Frostbite, DurationMs: 100000, Level: 1, SourceGID: monster.Gid}
	petOwner := rt.newCosAbnormalOwner(testDivision, caster, now)
	petOwner.sources = rt.captureAbnormalSources(testDivision, petOwner.block, []abnormal.Record{petRecord})
	rt.deps.Update(caster, "seed-pet-status", func() bool {
		petOwner.changed = petOwner.block.Apply(petOwner, petRecord, now)
		petOwner.commit()
		return true
	})
	installStoreDoor(t, rt)
	rt.clearSkillFinalizes(testDivision, caster.Name)

	result := rt.HandleTargetInteract(testDivision, caster, wire.SkillAction{
		ActionId: row.ID, HasTarget: true, TargetGid: petGID,
	}.Encode())
	if result.DiagnosticRefusal != "" {
		t.Fatal(result.DiagnosticRefusal)
	}
	stored := rt.cosAbnormal(testDivision, caster.Name, petGID)
	if stored == nil || stored.Slots[abnormal.Frostbite].StartedAt != now-int64(48*250) {
		t.Fatal("pet target was not cured")
	}
	if !hasPetMask(result.Broadcast, petGID) {
		t.Fatalf("pet mask not published: %v", result.Broadcast)
	}
}
