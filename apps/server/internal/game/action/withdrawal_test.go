/*
===========================================================================

withdrawal_test.go - restoration through real inventory and effect owners

The integration uses the same composition hooks as GameWorld. Split stacks,
repeat requests, refunds and persisted shortcuts must agree with the receipt.

===========================================================================
*/
package action

import (
	"bytes"
	"encoding/json"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
)

/*
================
withdrawalSkills
================
*/
type withdrawalSkills struct{ staticSkillSource }

/*
================
SkillByGroupLevel
================
*/
func (s withdrawalSkills) SkillByGroupLevel(group uint32, level int64) (enterworld.SkillRow, bool) {
	for _, row := range s.staticSkillSource {
		if row.Group == group && row.Level == level && !row.ChainSub {
			return row, true
		}
	}
	return enterworld.SkillRow{}, false
}

/*
================
TestWithdrawalCommitsSplitStacksRanksRefundAndShortcutsTogether
================
*/
func TestWithdrawalCommitsSplitStacksRanksRefundAndShortcutsTogether(t *testing.T) {
	for _, name := range []string{"ITEM_MALL_SKILL_RESTORATION_POTION", "ITEM_QNO_RM_OLDWOMAN_2_02"} {
		t.Run(name, func(t *testing.T) {
			c := testCharacter()
			c.Skills = []uint32{103}
			c.QuickSlots = []enterworld.QuickSlotBinding{{Slot: 0, Kind: 0x49, Payload: 103}, {Slot: 41, Kind: 0x49, Payload: 103}}
			items := testItems()
			potion := &enterworld.ItemRef{RefObjID: 900003, Codename: name, TypeIDs: [4]int64{3, 3, 13, 0}}
			items[name] = potion
			c.MissionInventory = []enterworld.InventoryRow{
				{Slot: 13, RefObjID: potion.RefObjID, Codename: name, TypeFlags: potion.TypeFlags(), StackCount: 1},
				{Slot: 14, RefObjID: potion.RefObjID, Codename: name, TypeFlags: potion.TypeFlags(), StackCount: 2},
			}
			action, _ := newTestRuntime(c, items)
			deps := action.deps.(*enterworld.Deps)
			deps.Skills = withdrawalSkills{staticSkillSource{
				101: {ID: 101, Group: 77, Level: 1, SPCost: 2},
				102: {ID: 102, Group: 77, Level: 2, SPCost: 7},
				103: {ID: 103, Group: 77, Level: 3, SPCost: 13},
			}}
			runtime := progression.NewRuntime(deps)
			runtime.Withdrawal = action.WithdrawalHooks()
			request := wire.NewWriter(9).U32(potion.RefObjID).U32(103).U8(1).Payload()
			result := runtime.HandleSkillWithdrawal(testDivision, c, request)
			found := false
			for _, frame := range result.Frames {
				if frame.Opcode == wire.OpSkillWithdrawalResponse {
					found = bytes.Equal(frame.Payload, []byte{1, 101, 0, 0, 0})
				}
			}
			if !found || len(c.Skills) != 1 || c.Skills[0] != 101 || c.SkillPoints == nil || *c.SkillPoints != 20 ||
				len(c.MissionInventory) != 1 || c.MissionInventory[0].StackCount != 1 || *c.Gold != 5000 {
				t.Fatal("restoration transaction", result, c)
			}
			for _, binding := range c.QuickSlots {
				if binding.Payload != 101 {
					t.Fatal(binding)
				}
			}
			before, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			result = runtime.HandleSkillWithdrawal(testDivision, c, request)
			after, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Frames) != 1 || result.Frames[0].Payload[0] != 2 || !bytes.Equal(before, after) {
				t.Fatal("replayed old-rank request mutated character", result)
			}
			request = wire.NewWriter(9).U32(potion.RefObjID).U32(101).U8(0).Payload()
			result = runtime.HandleSkillWithdrawal(testDivision, c, request)
			if len(c.Skills) != 0 || len(c.MissionInventory) != 0 || *c.SkillPoints != 22 {
				t.Fatal(result, c)
			}
			for _, binding := range c.QuickSlots {
				if binding.Kind != 0 || binding.Payload != 0 {
					t.Fatal(binding)
				}
			}
		})
	}
}

/*
================
TestWithdrawalSurvivesAuthorityRestartWithoutRepeatingRefund
================
*/
func TestWithdrawalSurvivesAuthorityRestartWithoutRepeatingRefund(t *testing.T) {
	seed := testCharacter()
	seed.Skills = []uint32{1, 2, 40, 70, 103}
	seed.QuickSlots = []enterworld.QuickSlotBinding{{Slot: 41, Kind: 0x49, Payload: 103}}
	potion := &enterworld.ItemRef{RefObjID: 3828, Codename: "ITEM_MALL_SKILL_RESTORATION_POTION", TypeIDs: [4]int64{3, 3, 13, 0}}
	seed.MissionInventory = []enterworld.InventoryRow{{Slot: 13, RefObjID: potion.RefObjID, Codename: potion.Codename, TypeFlags: potion.TypeFlags(), StackCount: 1}}
	door := openDoorRuntime(t, t.TempDir(), seed)
	deps := door.rt.deps.(*enterworld.Deps)
	items := testItems()
	items[potion.Codename] = potion
	deps.Items = items
	skills := deps.Skills.(staticSkillSource)
	skills[103] = enterworld.SkillRow{ID: 103, Group: 77, Level: 1, SPCost: 13}
	deps.Skills = withdrawalSkills{skills}
	runtime := progression.NewRuntime(deps)
	runtime.Withdrawal = door.rt.WithdrawalHooks()
	request := wire.NewWriter(9).U32(3828).U32(103).U8(0).Payload()
	before := int64(0)
	if door.character.SkillPoints != nil {
		before = *door.character.SkillPoints
	}
	result := runtime.HandleSkillWithdrawal(testDivision, door.character, request)
	if len(result.Frames) < 2 {
		t.Fatal(result)
	}
	boot := door.reboot(t)
	c := boot.character
	if c.SkillPoints == nil || *c.SkillPoints != before+13 || len(c.MissionInventory) != 0 {
		t.Fatal("restoration did not persist", c)
	}
	for _, id := range c.Skills {
		if id == 103 {
			t.Fatal("removed skill returned after restart")
		}
	}
	if len(c.QuickSlots) != 1 || c.QuickSlots[0].Kind != 0 || c.QuickSlots[0].Payload != 0 {
		t.Fatal(c.QuickSlots)
	}
}

/*
================
TestWithdrawalRefusalsLeaveInventoryAndProgressionUntouched

Planning must finish before the shared inventory owner commits anything.
Exercise the real handler, including malformed and insufficient requests.
================
*/
func TestWithdrawalRefusalsLeaveInventoryAndProgressionUntouched(t *testing.T) {
	c := testCharacter()
	c.Skills = []uint32{103}
	items := testItems()
	potion := &enterworld.ItemRef{RefObjID: 3828, Codename: "ITEM_MALL_SKILL_RESTORATION_POTION", TypeIDs: [4]int64{3, 3, 13, 0}}
	items[potion.Codename] = potion
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 13, RefObjID: potion.RefObjID, Codename: potion.Codename, TypeFlags: potion.TypeFlags(), StackCount: 1}}
	action, _ := newTestRuntime(c, items)
	deps := action.deps.(*enterworld.Deps)
	deps.Skills = withdrawalSkills{staticSkillSource{
		101: {ID: 101, Group: 77, Level: 1, SPCost: 2},
		102: {ID: 102, Group: 77, Level: 2, SPCost: 7},
		103: {ID: 103, Group: 77, Level: 3, SPCost: 13},
	}}
	runtime := progression.NewRuntime(deps)
	runtime.Withdrawal = action.WithdrawalHooks()
	for _, request := range [][]byte{
		wire.NewWriter(9).U32(3828).U32(103).U8(0).Payload(),
		wire.NewWriter(9).U32(3828).U32(103).U8(3).Payload(),
		wire.NewWriter(9).U32(3828).U32(102).U8(0).Payload(),
		wire.NewWriter(9).U32(9999).U32(103).U8(2).Payload(),
		{1, 2, 3},
	} {
		before, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		result := runtime.HandleSkillWithdrawal(testDivision, c, request)
		after, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Frames) != 1 || result.Frames[0].Payload[0] != 2 || !bytes.Equal(before, after) {
			t.Fatalf("refusal changed character: request=%x result=%+v", request, result)
		}
	}
}
