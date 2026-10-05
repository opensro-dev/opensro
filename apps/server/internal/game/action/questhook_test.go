package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// TestQuestInventoryHookRidesTheItemBursts pins the quest lane's
// collect-objective seam: an inventory-changing commit (ground drop,
// pickup grant) runs the UpdateQuestInventory hook with the acting
// character and appends the returned 0x31ED frames to the SAME session
// burst; a nil hook changes nothing (the pre-quest wiring and every
// sibling test in this package).
func TestQuestInventoryHookRidesTheItemBursts(t *testing.T) {
	character := testCharacter()
	rt, _ := newTestRuntime(character, testItems())

	hookCalls := 0
	rt.UpdateQuestInventory = func(c *enterworld.Character) ([]wire.Frame, bool) {
		hookCalls++
		if c != character {
			t.Fatal("the hook must receive the acting character")
		}
		return []wire.Frame{{Opcode: 0x31ED, Payload: []byte{2, 0, 0, 0, 0}}}, true
	}

	// Ground drop: the burst is [0xB06D][0x30D7] + the hook's 0x31ED.
	drop := rt.HandleItemMove(testDivision, character, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGroundDrop, SourceSlot: 20,
	}))
	assertOpcodes(t, drop.Frames, wire.OpItemMoveResponse, wire.OpSingleObjectSpawn, 0x31ED)
	if hookCalls != 1 {
		t.Fatalf("hook calls after the drop = %d, want 1", hookCalls)
	}

	// Pickup grant of the same item: [0xB2CD][0x35C7][0xB06D][0x36AB] +
	// the hook's 0x31ED.
	dropped := rt.Ground.All(testDivision)[0]
	grant := rt.HandleTargetInteract(testDivision, character, wire.TargetInteract{Gid: dropped.Gid}.Encode())
	assertOpcodes(t, grant.Frames,
		wire.OpPickupAnim, wire.OpItemMoveResponse,
		wire.OpObjectDespawn, 0x31ED, wire.OpActionState)
	if hookCalls != 2 {
		t.Fatalf("hook calls after the pickup = %d, want 2", hookCalls)
	}
	// The broadcast (peer) legs never carry the hook's frames - quest
	// progress is private to the character.
	for _, frame := range grant.Broadcast {
		if frame.Opcode == 0x31ED {
			t.Fatal("a peer broadcast must never carry the actor's quest progress")
		}
	}
}
