/*
===========================================================================

itemmove_door_test.go - inventory moves publish ended effects after the door

Peer delivery reads session characters through the store, whose character
door is not reentrant: a push from inside the Update callback would wait on
the door its own caller holds and stall the shard.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
)

// Unequipping the sword ends a buff whose reqi names it; the teardown frames
// still reach the owner and the peers, but only once the door is released.
func TestUnequipEndingBuffPublishesOutsideTheDoor(t *testing.T) {
	const buffSkill, buffToken = 50, 500
	rt, _, c, _ := newCombatTestRuntime(t, 100)
	deps := rt.deps.(*enterworld.Deps)
	skills := deps.Skills.(staticSkillSource)
	skills[buffSkill] = enterworld.SkillRow{
		ID:       buffSkill,
		Codename: "SKILL_CH_SWORD_TEST_BUFF",
		Reqi:     reqi(false, pair(6, 2)),
	}
	if !rt.effects.Apply(statuseffect.Effect{
		DivisionID: testDivision, CharacterName: c.Name, SkillID: buffSkill, SkillGroup: buffSkill,
		InstanceToken: buffToken, Phase: 1, State: statuseffect.StateActive,
	}) {
		t.Fatal("buff not applied")
	}

	inDoor := false
	deps.UpdateCharacter = func(_ *enterworld.Character, _ string, update func() bool) bool {
		inDoor = true
		defer func() { inDoor = false }()
		return update()
	}
	var ownerPushes, peerPushes int
	rt.PushCharacterFrames = func(_, _ string, _ []wire.Frame) {
		if inDoor {
			t.Error("owner frames pushed inside the character door")
		}
		ownerPushes++
	}
	rt.PushDivisionPeerFrames = func(_, _ string, _ []wire.Frame) {
		if inDoor {
			t.Error("peer frames pushed inside the character door")
		}
		peerPushes++
	}

	result := rt.applyInventoryMove(testDivision, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: 6, DestSlot: 13, Quantity: 1})
	if len(result.Frames) == 0 {
		t.Fatalf("unequip refused: %s", result.DiagnosticRefusal)
	}
	for _, e := range rt.effects.Snapshot(testDivision, c.Name) {
		if e.InstanceToken == buffToken {
			t.Fatal("buff survived losing its required weapon")
		}
	}
	if ownerPushes == 0 || peerPushes == 0 {
		t.Fatalf("ended buff not published: owner %d peer %d", ownerPushes, peerPushes)
	}
}
