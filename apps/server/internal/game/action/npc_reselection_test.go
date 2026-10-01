/*
===========================================================================

npc_reselection_test.go - server contract for reopening the same NPC

The browser can close its conversation while a service retains selection.
Every new select must grant the NPC again and retire the previous dialogue.

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestNpcReselectionRenewsGrantAndRetiresDialogue
================
*/
func TestNpcReselectionRenewsGrantAndRetiresDialogue(t *testing.T) {
	character := testCharacter()
	runtime := selectTestRuntime(character)
	setSelectCharacters(t, runtime, enterworld.StaticCharacterSource{testDivision: {character}})
	runtime.NpcSpawn = enterworld.NpcSpawnConfig{Enabled: true, AtPlayer: true}
	npc := runtime.NpcRoster[0]

	for _, phase := range []string{"first selection", "retained selection", "released selection"} {
		if phase == "released selection" {
			released := runtime.HandleTargetRelease(testDivision, character, selectBody(npc.ObjectID))
			if released.Refusal != "" {
				t.Fatalf("release refused: %s", released.Refusal)
			}
			assertOpcodes(t, released.Frames, wire.OpTalkCloseResult)
		}
		runtime.NpcDialogs.Put(testDivision, character.Name, npcDialogSession{NpcGID: npc.ObjectID})
		selected := runtime.HandleObjectSelect(testDivision, character, selectBody(npc.ObjectID))
		if selected.Refusal != "" {
			t.Fatalf("%s refused: %s", phase, selected.Refusal)
		}
		assertOpcodes(t, selected.Frames, wire.OpObjectSelectResult)
		if !bytes.Equal(selected.Frames[0].Payload, b45aGrantOracle(npc.ObjectID, npc.TalkFlags)) {
			t.Fatalf("%s did not renew the NPC grant: % X", phase, selected.Frames[0].Payload)
		}
		if _, exists := runtime.NpcDialogs.Get(testDivision, character.Name); exists {
			t.Fatalf("%s retained the previous dialogue", phase)
		}
	}
}
