package action

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
	wire "opensro.online/server/internal/game/item/wire"
)

// A window re-raised at world entry reaches the sweep only through the hook
// the bootstrap calls. Item use is the index's other writer, so without it a
// relogged row would sit at zero forever: client 6E6AA0 never retires one itself.
func TestWorldEntryTrackedPetSkillWindowRetiresOnTheSweep(t *testing.T) {
	character := testCharacter()
	rt, clock := newTestRuntime(character, petSkillSource())
	endMs := clock.NowMs() + 1000
	character.PetSkillWindows = []domain.PetSkillWindow{
		{ItemRefObjID: 24001, Codename: "ITEM_MALL_PET_SKILL_COLD", EndUnixMs: endMs},
	}
	var pushed []wire.Frame
	rt.PushCharacterFrames = func(divisionID, characterName string, frames []wire.Frame) {
		if divisionID != testDivision || characterName != character.Name {
			t.Fatalf("pushed to %s/%s", divisionID, characterName)
		}
		pushed = append(pushed, frames...)
	}

	rt.advancePetSkillWindows(endMs + 5000)
	if len(pushed) != 0 || len(character.PetSkillWindows) != 1 {
		t.Fatalf("an untracked character was swept: pushed %v windows %+v", pushed, character.PetSkillWindows)
	}

	rt.TrackPetSkillWindows(testDivision, character.Name)
	rt.advancePetSkillWindows(endMs - 1)
	if len(pushed) != 0 || len(character.PetSkillWindows) != 1 {
		t.Fatalf("a live window retired early: pushed %v windows %+v", pushed, character.PetSkillWindows)
	}

	rt.advancePetSkillWindows(endMs)
	want := []wire.Frame{{Opcode: wire.OpCosStateRefresh, Payload: wire.EncodeCosSummonTimerRetire3691(24001)}}
	if !reflect.DeepEqual(pushed, want) {
		t.Fatalf("retirement = %+v, want the native zero pair %+v", pushed, want)
	}
	if len(character.PetSkillWindows) != 0 {
		t.Fatalf("windows = %+v, want the spent row dropped", character.PetSkillWindows)
	}
	if keys := rt.petSkillWindows.keys(); len(keys) != 0 {
		t.Fatalf("index = %v, want the empty character forgotten", keys)
	}
}

/*
================
TestPetSkillWindowIndexKeysMemoTracksMembership

The sweeps read the cached order between membership turns: a second
read returns the same backing, re-tracking a live owner keeps it, a
new owner rebuilds a fresh list without touching the one under its
readers, and forget rebuilds only when it removed an owner.
================
*/
func TestPetSkillWindowIndexKeysMemoTracksMembership(t *testing.T) {
	index := &petSkillWindowIndex{}
	if got := index.keys(); len(got) != 0 {
		t.Fatalf("empty index keys = %v, want none", got)
	}
	index.track("div-b", "bravo")
	index.track("div-a", "beta")
	index.track("div-a", "alpha")
	keys := index.keys()
	want := []petOwnerKey{
		{division: "div-a", name: "alpha"},
		{division: "div-a", name: "beta"},
		{division: "div-b", name: "bravo"},
	}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys = %v, want the sorted owners %v", keys, want)
	}
	if again := index.keys(); &again[0] != &keys[0] {
		t.Fatal("second read rebuilt the cached list")
	}
	index.track("div-a", "alpha")
	if again := index.keys(); &again[0] != &keys[0] {
		t.Fatal("re-tracking a live owner dropped the cached list")
	}
	index.track("div-a", "aardvark")
	grown := index.keys()
	if len(grown) != 4 || grown[0].name != "aardvark" {
		t.Fatalf("keys after track = %v, want the new owner first", grown)
	}
	if len(keys) != 3 {
		t.Fatalf("list under its readers changed: %v", keys)
	}
	index.forget(petOwnerKey{division: "div-a", name: "ghost"})
	if again := index.keys(); &again[0] != &grown[0] {
		t.Fatal("forgetting an absent owner dropped the cached list")
	}
	index.forget(petOwnerKey{division: "div-a", name: "aardvark"})
	if shrunk := index.keys(); !reflect.DeepEqual(shrunk, want) {
		t.Fatalf("keys after forget = %v, want %v", shrunk, want)
	}
	if len(grown) != 4 {
		t.Fatalf("list under its readers changed: %v", grown)
	}
}
