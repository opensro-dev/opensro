/*
===========================================================================

ammostack_test.go - a 2500-arrow quiver fires, decrements and reloads

SRO_STACK_SIZES raises arrows and bolts to 2500 (port-only, owner
2026-10-14). The quiver (equipment socket 7), every shot's debit, the
auto-reload and opcode 0x3752 carry the count as u16, so a raised stack
must fire and reload exactly as a native 250 does.

===========================================================================
*/
package action

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

// raisedAmmoStack is the deployment default for arrows and bolts.
const raisedAmmoStack = 2500

/*
================
TestRaisedArrowQuiverFiresAndDecrements
================
*/
func TestRaisedArrowQuiverFiresAndDecrements(t *testing.T) {
	rt, _, character, target := newCombatTestRuntime(t, 100000)
	items := rt.deps.ItemReferences().(staticItemSource)
	weapon := items[character.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 6
	weapon.Combat.ActionRange = 180
	character.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	arrow := &enterworld.ItemRef{RefObjID: 62_001, Codename: "ITEM_ETC_AMMO_ARROW_01", TypeIDs: [4]int64{3, 3, 4, 1}}
	items[arrow.Codename] = arrow
	character.MissionInventory = append(character.MissionInventory, enterworld.InventoryRow{
		Slot: 7, RefObjID: arrow.RefObjID, Codename: arrow.Codename, TypeFlags: arrow.TypeFlags(), StackCount: raisedAmmoStack,
	})
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.Codename = "SKILL_CH_BOW_BASE_01"
	skill.RequiredWeaponKinds = [2]uint8{6, 0xff}
	skills[2] = skill

	result := rt.HandleTargetInteract(testDivision, character, wire.SkillAction{
		ActionId: 2, HasTarget: true, TargetGid: target.Gid,
	}.Encode())
	result = assertAndSeparateActionSession(t, result)
	want := binary.LittleEndian.AppendUint16(nil, raisedAmmoStack-1)
	if len(result.Frames) != 2 || result.Frames[1].Opcode != wire.OpAvatarInventorySlot7StackCount ||
		!bytes.Equal(result.Frames[1].Payload, want) {
		t.Fatalf("ranged actor frames = %+v, want 3752 count=%d", result.Frames, raisedAmmoStack-1)
	}
	if got := character.MissionInventory[1].StackCount; got != raisedAmmoStack-1 {
		t.Fatalf("quiver after one shot = %d, want %d", got, raisedAmmoStack-1)
	}
}

/*
================
TestRaisedArrowStackReloadsWhole

The last arrow reloads a whole 2500 bag stack: the move and the new count
both carry 2500.
================
*/
func TestRaisedArrowStackReloadsWhole(t *testing.T) {
	character := &enterworld.Character{MissionInventory: []enterworld.InventoryRow{
		{Slot: int64(inventory.SocketShield), RefObjID: 62, TypeFlags: arrowTypeFlags, StackCount: 1},
		{Slot: 13, RefObjID: 62, TypeFlags: arrowTypeFlags, StackCount: raisedAmmoStack},
	}}
	result := applyAmmunitionDebit(character, ammunitionDebit{index: 0, remaining: 0})
	if result.reload == nil || result.reload.quantity != raisedAmmoStack || result.count != raisedAmmoStack {
		t.Fatalf("reload = %+v count %d, want the whole %d stack", result.reload, result.count, raisedAmmoStack)
	}
	frames := ammunitionFrames(result)
	if len(frames) != 2 || !bytes.Equal(frames[1].Payload, binary.LittleEndian.AppendUint16(nil, raisedAmmoStack)) {
		t.Fatalf("frames = %+v, want the move then 3752 count=%d", frames, raisedAmmoStack)
	}
}
