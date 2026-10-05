/*
===========================================================================

gendertool.go - the armour gender change tool

ITEM_MALL_EQUIP_TRANSGENDER_* and their quest twins (3/3/13/8) are 49C2B0
case 7. The v1.150 client sends the tool dropped on a bag item:
0x75BD [slot][type][u8 target slot] (CIFInventory_ExecuteItemAction). The
server admits it:

  - the target is a bag slot 13..0x6F (0x1882);
  - it is equipment made for one sex (0x1884,
    UIIT_MSG_STRGERR_INVALID_TRANSGENDER_TARGET);
  - its degree (CGItem_GetDegree 4775A0: (ItemClass - 1) / 3 + 1) is at
    most the tool's Param1 (0x1883);

then CGObjPC_ChangeInventoryItemRef (4EFB60) swaps the item's reference in
place and answers v1.150 0x3645 [slot][1][u32 ref], keeping every other
field of the item.

INFERENCE: v1.188 finds the other sex's item through a reference-data
string this port does not load; the shipped data pairs every such item by
its codename's _M_ / _W_ segment with the same type, so the port swaps
that segment and requires the twin to exist.

===========================================================================
*/

package action

import (
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

const (
	// Category 1 notices (v1.188 0x188x low bytes).
	genderErrSlot   uint8 = 0x82
	genderErrDegree uint8 = 0x83
	genderErrTarget uint8 = 0x84
	// genderLastBagSlot is 49C4F3's 13 + 0x62.
	genderLastBagSlot = 0x6f
	// itemStateRefFlag is 0x3645's reference field (7654B0 flag 1).
	itemStateRefFlag uint8 = 1
)

/*
================
genderTwin

The other sex's codename of an item made for one sex, or "".
================
*/
func genderTwin(codename string) string {
	switch {
	case strings.Contains(codename, "_M_"):
		return strings.Replace(codename, "_M_", "_W_", 1)
	case strings.Contains(codename, "_W_"):
		return strings.Replace(codename, "_W_", "_M_", 1)
	}
	return ""
}

/*
================
itemDegree

4775A0 for equipment: (ItemClass - 1) / 3 + 1, 0 without a class.
================
*/
func itemDegree(ref *enterworld.ItemRef) int64 {
	class, ok := ref.NativeFields.Lookup("itemClass")
	if !ok || class <= 0 {
		return 0
	}
	return (int64(class)-1)/3 + 1
}

/*
================
useGenderTool

Runs inside the item use's character Update.
================
*/
func (rt *Runtime) useGenderTool(use skillItemUse, c *enterworld.Character, tail []byte, result *OpResult) bool {
	r := wire.NewReader(tail)
	target, e := r.U8()
	if e != nil || r.Done() != nil || target < inventory.EquipmentSlotEnd || target > genderLastBagSlot {
		*result = itemUseFailure(genderErrSlot)
		return false
	}
	index := -1
	for i, row := range c.MissionInventory {
		if row.Slot == int64(target) {
			index = i
			break
		}
	}
	refs := rt.deps.ItemReferences()
	if index < 0 || refs == nil {
		*result = itemUseFailure(genderErrTarget)
		return false
	}
	row := c.MissionInventory[index]
	ref, ok := refs.ItemRefByCodename(row.Codename)
	twinName := genderTwin(row.Codename)
	twin, twinOK := refs.ItemRefByCodename(twinName)
	if !ok || ref == nil || !isEquipmentItem(row.TypeFlags) || twinName == "" || !twinOK || twin == nil ||
		twin.TypeIDs != ref.TypeIDs {
		*result = itemUseFailure(genderErrTarget)
		return false
	}
	limit, _ := use.ref.NativeFields.Lookup("itemParam1_29c")
	if itemDegree(ref) > int64(limit) {
		*result = itemUseFailure(genderErrDegree)
		return false
	}
	remaining := rt.consumeItemUseRow(c, use.row)
	moved := &c.MissionInventory[index]
	moved.RefObjID, moved.Codename, moved.TypeFlags = twin.RefObjID, twin.Codename, twin.TypeFlags()
	state := wire.NewWriter(6).U8(target).U8(itemStateRefFlag).U32(twin.RefObjID).Payload()
	*result = OpResult{Frames: []wire.Frame{
		{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)},
		rt.commerceReferences(invItemsFromRows([]enterworld.InventoryRow{*moved}), nil),
		{Opcode: cosItemStateOpcode, Payload: state},
	}}
	result.Frames = append(result.Frames, rt.updateQuestInventory(c)...)
	return true
}
