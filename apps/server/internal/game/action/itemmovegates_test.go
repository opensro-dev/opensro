/*
===========================================================================

itemmovegates_test.go - the dispatcher's dead and cast-locked move gates

#4490 U6-12: the original refuses a dead player's item moves with the
generic code, mall purchase excepted, and refuses weapon or shield
changes while a skill cast is in flight with 0x1834. These tests pin
both gates at the 0x706D entry, over the requirement fixtures (a level
floor sword in the bag or worn, a hand-built shield row).

===========================================================================
*/

package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// moveAnswer returns the first frame's payload of one composed move.
func moveAnswer(t *testing.T, rt *Runtime, character *enterworld.Character, request wire.ItemMoveRequest) []byte {
	t.Helper()
	result := rt.HandleItemMove(testDivision, character, encodeMove(t, request))
	if len(result.Frames) == 0 {
		t.Fatalf("move %+v answered no frame", request)
	}
	return result.Frames[0].Payload
}

// TestDeadPlayerItemMovesRefuse: every movement kind a dead player
// sends meets the generic refusal and nothing moves (the mall packet
// never reaches this gate; it is routed before it).
func TestDeadPlayerItemMovesRefuse(t *testing.T) {
	items := requirementTestItems()
	character := requirementTestCharacter(5, []enterworld.InventoryRow{
		bagRow(20, items, "ITEM_CH_SWORD_01_A"),
	})
	character.CurrentHP = testInt64(0)
	rt, _ := newTestRuntime(character, items)

	equip := wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: 20, DestSlot: 6, Quantity: 1}
	if got := moveAnswer(t, rt, character, equip); !bytes.Equal(got, []byte{2, wire.ErrCodeInvalidRequest}) {
		t.Fatalf("a dead player's equip answered %v, want [02 02]", got)
	}
	drop := wire.ItemMoveRequest{MovementType: wire.MoveTypeGroundDrop, SourceSlot: 20, Quantity: 1}
	if got := moveAnswer(t, rt, character, drop); !bytes.Equal(got, []byte{2, wire.ErrCodeInvalidRequest}) {
		t.Fatalf("a dead player's drop answered %v, want [02 02]", got)
	}
	gold := wire.ItemMoveRequest{MovementType: wire.MoveTypeGoldDrop, GoldAmount: 10}
	if got := moveAnswer(t, rt, character, gold); !bytes.Equal(got, []byte{2, wire.ErrCodeInvalidRequest}) {
		t.Fatalf("a dead player's gold drop answered %v, want [02 02]", got)
	}
	for _, row := range character.MissionInventory {
		if row.Slot != 20 || row.Codename != "ITEM_CH_SWORD_01_A" {
			t.Fatalf("the dead refusals moved a row: %+v", row)
		}
	}
}

// TestCastLockRefusesHandChanges: with a cast in flight the weapon
// and shield sockets and worn-to-worn shuffles refuse 0x34, while
// taking a piece off into the bag and bag-to-bag moves still run.
func TestCastLockRefusesHandChanges(t *testing.T) {
	items := requirementTestItems()

	plain, plainCharacter := newMoveGateRuntime(items)
	if got := moveAnswer(t, plain, plainCharacter, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeInventory, SourceSlot: 20, DestSlot: 6, Quantity: 1,
	}); got[0] != 1 {
		t.Fatalf("the sanity equip answered %v, want success", got)
	}

	casting, character := newMoveGateRuntime(items)
	casting.pendingSkillFinalizes = append(casting.pendingSkillFinalizes,
		newPendingSkillFinalize(testDivision, character.Name))

	equip := wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: 20, DestSlot: 6, Quantity: 1}
	if got := moveAnswer(t, casting, character, equip); !bytes.Equal(got, []byte{2, wire.ErrCodeStorageOperationBlocked}) {
		t.Fatalf("changing the weapon mid-cast answered %v, want [02 34]", got)
	}

	shuffle := wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: 6, DestSlot: 7, Quantity: 1}
	if got := moveAnswer(t, casting, character, shuffle); !bytes.Equal(got, []byte{2, wire.ErrCodeStorageOperationBlocked}) {
		t.Fatalf("shuffling worn pieces mid-cast answered %v, want [02 34]", got)
	}

	bagMove := wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: 20, DestSlot: 21, Quantity: 1}
	if got := moveAnswer(t, casting, character, bagMove); got[0] != 1 {
		t.Fatalf("a bag-to-bag move mid-cast answered %v, want success", got)
	}
}

// TestCastLockShieldSocketReadsTheIncomingPiece: the shield socket
// locks only for a piece that belongs there; a sword dragged onto it
// mid-cast meets the same ordinary refusal it meets out of cast.
func TestCastLockShieldSocketReadsTheIncomingPiece(t *testing.T) {
	items := requirementTestItems()

	shieldCharacter := requirementTestCharacter(5, []enterworld.InventoryRow{
		{Slot: 20, RefObjID: 9999, Codename: "TEST_SHIELD",
			TypeFlags: wire.PackTypeFlags(3, 1, 4, 1), VarianceBits: "0", StackCount: 1},
	})
	shieldRt, _ := newTestRuntime(shieldCharacter, items)
	shieldRt.pendingSkillFinalizes = append(shieldRt.pendingSkillFinalizes,
		newPendingSkillFinalize(testDivision, shieldCharacter.Name))
	shieldIn := wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: 20, DestSlot: 7, Quantity: 1}
	if got := moveAnswer(t, shieldRt, shieldCharacter, shieldIn); !bytes.Equal(got, []byte{2, wire.ErrCodeStorageOperationBlocked}) {
		t.Fatalf("changing the shield mid-cast answered %v, want [02 34]", got)
	}

	plain, plainCharacter := newMoveGateRuntime(items)
	casting, castingCharacter := newMoveGateRuntime(items)
	casting.pendingSkillFinalizes = append(casting.pendingSkillFinalizes,
		newPendingSkillFinalize(testDivision, castingCharacter.Name))
	swordOntoShield := wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: 20, DestSlot: 7, Quantity: 1}
	plainAnswer := moveAnswer(t, plain, plainCharacter, swordOntoShield)
	castAnswer := moveAnswer(t, casting, castingCharacter, swordOntoShield)
	if !bytes.Equal(castAnswer, plainAnswer) {
		t.Fatalf("a sword onto the shield socket answered %v mid-cast, %v out of cast; the cast lock must not read it", castAnswer, plainAnswer)
	}
}

// newMoveGateRuntime builds the cast tests' mannequin: a bag sword
// at 20 and the same sword already worn at 6.
func newMoveGateRuntime(items staticItemSource) (*Runtime, *enterworld.Character) {
	character := requirementTestCharacter(5, []enterworld.InventoryRow{
		bagRow(20, items, "ITEM_CH_SWORD_01_A"),
		bagRow(6, items, "ITEM_CH_SWORD_01_A"),
	})
	rt, _ := newTestRuntime(character, items)
	return rt, character
}
