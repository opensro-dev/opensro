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
