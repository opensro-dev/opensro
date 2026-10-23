package action

import (
	"errors"
	"opensro.online/server/internal/testsupport/loottest"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

func TestInventoryAndQuestProgressUseOneTransaction(t *testing.T) {
	loottest.Require(t)
	runtime := openDoorRuntime(
		t,
		filepath.Join(t.TempDir(), "authority"),
		testCharacter(),
	)
	runtime.rt.UpdateQuestInventory = func(character *enterworld.Character) ([]wire.Frame, bool) {
		character.CompletedQuestIds = append(character.CompletedQuestIds, 29)
		return []wire.Frame{{Opcode: 0x31ED}}, true
	}

	runtime.authority.FailCommits(errors.New("disk unavailable"))
	result := runtime.rt.HandleItemMove(
		testDivision,
		runtime.character,
		encodeMove(t, wire.ItemMoveRequest{
			MovementType: wire.MoveTypeGroundDrop,
			SourceSlot:   20,
		}),
	)
	assertOpcodes(t, result.Frames, wire.OpItemMoveResponse, wire.OpSingleObjectSpawn, 0x31ED)
	if health := runtime.authority.Health(); health.FailedWrites != 1 {
		t.Fatalf("inventory plus quest update attempted %d commits, want exactly one: %+v", health.FailedWrites, health)
	}
	if len(runtime.character.MissionInventory) != 0 ||
		len(runtime.character.CompletedQuestIds) != 1 {
		t.Fatalf("inventory and quest progress tore in memory: %+v", runtime.character)
	}

	runtime.authority.FailCommits(nil)
	runtime.authority.MutateCharacter(runtime.character, "heal-inventory-quest", nil)
	reloaded := runtime.reboot(t)
	if len(reloaded.character.MissionInventory) != 0 ||
		len(reloaded.character.CompletedQuestIds) != 1 {
		t.Fatalf("inventory and quest progress tore after restart: %+v", reloaded.character)
	}
}
