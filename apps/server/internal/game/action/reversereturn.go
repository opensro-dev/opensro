/*
===========================================================================

reversereturn.go - the reverse return scroll: back to the last recall point
or to where the player died

ITEM_MALL_REVERSE_RETURN_SCROLL (3/3/3/3) is never used from the bag in
v1.150. A teleport gate NPC's select grant carries capability 0x20000000;
the talk window then lists two rows (5D4410, action 0x2B) and a click sends
0x7495 [u32 gid][u8 5][u8 choice] (6FEF10): 2 is the last recall point, 3
the place of the last death. v1.188 reaches the same rule from the item
(CGItemExpendable_UseReverseReturnScroll 4A00C0): the shared return
admissions, then the recorded point (char-data +0xCC.. / +0xDC.., 0x1885 /
0x1886 when absent), then the return scroll's own timed cast.

The answer rides the item-use channel: 0xB5BD success carries the scroll's
slot and remaining count (the client's travel mode reads it as a return
scroll, travel.ts), and the missing-point errors are its category-1 notices
(390 UIIT_MSG_STRGERR_CANT_FIND_LAST_DIEDPOS). INFERENCE: the grant offers
the rows only while the player holds a scroll; a gate shows nothing it
cannot do.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// gateReverseReturn is the 0x7495 type the reverse return rows send.
	gateReverseReturn uint8 = 5
	// The two 0x7495 type-5 choices (5DA1B0 case 0x2B rows 1 and 2).
	reverseReturnLastRecall uint8 = 2
	reverseReturnLastDeath  uint8 = 3

	// talkFlagReverseReturn is the select grant bit 5D9100 tests before
	// listing the rows.
	talkFlagReverseReturn uint32 = 0x20000000
	// talkFlagTeleport marks a teleport gate NPC (ConfigurePortals).
	talkFlagTeleport uint32 = 0x80

	// The low bytes of 4A00C0's 0x1885 and 0x1886.
	errCodeNoRecallPoint uint8 = 0x85
	errCodeNoDeathPoint  uint8 = 0x86
)

/*
================
reverseReturnScrollRow

The bag row of the first reverse return scroll the player holds.
================
*/
func (rt *Runtime) reverseReturnScrollRow(c *enterworld.Character) (int, *enterworld.ItemRef, bool) {
	refs := rt.deps.ItemReferences()
	if refs == nil {
		return 0, nil, false
	}
	found, slot := -1, int64(0)
	var held *enterworld.ItemRef
	for index, row := range c.MissionInventory {
		if !inventory.IsBagSlot(uint8(row.Slot)) || row.StackCount < 1 || found >= 0 && row.Slot >= slot {
			continue
		}
		ref, ok := refs.ItemRefByCodename(row.Codename)
		if !ok || ref == nil || ref.RefObjID != row.RefObjID || ref.TypeIDs != [4]int64{3, 3, 3, 3} {
			continue
		}
		found, slot, held = index, row.Slot, ref
	}
	return found, held, found >= 0
}

/*
================
reverseReturnCapability

The select grant's extra bit for a teleport gate when the player holds a
reverse return scroll. The caller holds the division lock.
================
*/
func (rt *Runtime) reverseReturnCapability(npc simulation.NpcDef, c *enterworld.Character) uint32 {
	if npc.TalkFlags&talkFlagTeleport == 0 {
		return 0
	}
	if _, _, held := rt.reverseReturnScrollRow(c); !held {
		return 0
	}
	return talkFlagReverseReturn
}

/*
================
reverseReturnPoint

The recorded point a choice names, or the refusal for a missing one.
================
*/
func reverseReturnPoint(c *enterworld.Character, choice uint8) (simulation.Spawn, uint8) {
	var point *enterworld.WorldSpawn
	refusal := errCodeNoRecallPoint
	if c.World != nil && choice == reverseReturnLastRecall {
		point = c.World.LastRecallPoint
	}
	if choice == reverseReturnLastDeath {
		refusal = errCodeNoDeathPoint
		if c.World != nil {
			point = c.World.LastDeathPoint
		}
	}
	if point == nil || point.RegionID == nil || *point.RegionID == 0 {
		return simulation.Spawn{}, refusal
	}
	return missionSpawnFromWorld(point, simulation.Spawn{}), 0
}

/*
================
handleReverseReturn

0x7495 type 5 from the selected gate. Every refusal of the scroll itself
answers on the item-use channel, like the v1.188 item path.
================
*/
func (rt *Runtime) handleReverseReturn(division string, c *enterworld.Character, gid uint32, choice uint8) OpResult {
	if c == nil || choice != reverseReturnLastRecall && choice != reverseReturnLastDeath {
		return portalFailure(2)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if _, _, refusal := rt.portalSourceNpc(division, c, gid); refusal != 0 {
		return portalFailure(refusal)
	}
	result := itemUseFailure(wire.ErrCodeInvalidRequest)
	var used *enterworld.ItemRef
	now := rt.Now().UnixMilli()
	committed := rt.deps.Update(c, "reverse-return", func() bool {
		if c.DeletePending {
			return false
		}
		if !enterworld.CharacterAlive(c) {
			result = itemUseFailure(wire.ErrCodeItemUseDead)
			return false
		}
		row, ref, held := rt.reverseReturnScrollRow(c)
		if !held {
			return false
		}
		duration, ok := returnScrollDuration(ref)
		if !ok || !rt.returnScrollAdmission(division, c, &result) {
			return false
		}
		destination, refusal := reverseReturnPoint(c, choice)
		if refusal != 0 {
			result = itemUseFailure(refusal)
			return false
		}
		used = ref
		return rt.startReturnCast(returnCast{division: division, character: c, row: row,
			slot: uint8(c.MissionInventory[row].Slot), typeWord: ref.TypeFlags(), duration: duration,
			destination: &destination, now: now}, &result)
	})
	if committed && used != nil {
		rt.publishItemUseVisual(c, used, &result)
	}
	return result
}
