/*
===========================================================================

raceswitch.go - the armor and accessory switch scrolls: an item changes race

Port-only, not native (owner, 2026-11-21). The scrolls and their listing
live in commerce/equipswitch.go; this file serves their use, each behind
its own flag (SRO_ARMOR_SWITCH_SCROLL, SRO_ACCESSORY_SWITCH_SCROLL). Off
is native: the donor item is an item no family serves.

The browser client arms the item-target cursor when a scroll is used, the
player clicks an item in the bag and confirms. The use request is 0x75BD
[slot][type][u8 target slot]. The answer is the weapon switch's
(weaponswitch.go): the target's reference changes in place, a reference
row for the new item, then 0x3645 [slot][1][u32 ref]. Every other field
of the item (durability, white stats, plus, blue options) is kept.

The armor scroll switches the three armor families and the shield:

  Chinese 3/1/1 clothes (robe)   <-> European 3/1/9 clothes (garment)
  Chinese 3/1/2 light            <-> European 3/1/10 light (protector)
  Chinese 3/1/3 heavy            <-> European 3/1/11 heavy (armor)
  Chinese 3/1/4/1 shield         <-> European 3/1/4/2 shield

and the accessory scroll the accessories, Chinese 3/1/5/x <-> European
3/1/12/x (1 earring, 2 necklace, 3 ring). An armor piece keeps its part
(the fourth TypeID) and its sex.

INFERENCE: the twin is found by its codename. The shipped data names both
races' items alike after the race segment (ITEM_CH_M_LIGHT_08_BA_A and
ITEM_EU_M_LIGHT_08_BA_A, ITEM_CH_RING_08_B and ITEM_EU_RING_08_B) with
the same degree and required level, so the port swaps ITEM_CH_ and
ITEM_EU_ and requires the twin to exist with the expected TypeIDs and
race. An item the other race does not make (a few low heavy pieces, the
trade and PvP gear) has no twin and is refused.

===========================================================================
*/

package action

import (
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

// raceSwitchTailBytes is the tail after the type word: the target slot.
const raceSwitchTailBytes = 1

// The two races' codename prefixes and itemdata Country values.
const (
	chinesePrefix   = "ITEM_CH_"
	europeanPrefix  = "ITEM_EU_"
	chineseCountry  = 0
	europeanCountry = 1
)

// raceTypeTwins pairs each Chinese third TypeID with its European one
// (and back) for the scroll that switches it; the shield (4) pairs on
// its fourth TypeID instead.
var raceTypeTwins = map[commerce.EquipSwitchKind]map[int64]int64{
	commerce.SwitchArmor:     {1: 9, 2: 10, 3: 11, 9: 1, 10: 2, 11: 3},
	commerce.SwitchAccessory: {5: 12, 12: 5},
}

// shieldType is the shield's third TypeID; its fourth is the race
// (1 Chinese, 2 European).
const shieldType = 4

/*
================
raceSwitchTwin

The codename and TypeIDs the other race's twin of an item must carry
under a scroll kind, and the twin's race, or ok false when the scroll
does not switch the item.
================
*/
func raceSwitchTwin(ref *enterworld.ItemRef, kind commerce.EquipSwitchKind) (codename string, typeIDs [4]int64, country int64, ok bool) {
	if ref.TypeIDs[0] != 3 || ref.TypeIDs[1] != 1 {
		return "", typeIDs, 0, false
	}
	typeIDs = ref.TypeIDs
	switch {
	case kind == commerce.SwitchArmor && ref.TypeIDs[2] == shieldType && (ref.TypeIDs[3] == 1 || ref.TypeIDs[3] == 2):
		typeIDs[3] = 3 - ref.TypeIDs[3]
	default:
		other, found := raceTypeTwins[kind][ref.TypeIDs[2]]
		if !found {
			return "", typeIDs, 0, false
		}
		typeIDs[2] = other
	}
	switch {
	case strings.HasPrefix(ref.Codename, chinesePrefix) && ref.Country == chineseCountry:
		return europeanPrefix + strings.TrimPrefix(ref.Codename, chinesePrefix), typeIDs, europeanCountry, true
	case strings.HasPrefix(ref.Codename, europeanPrefix) && ref.Country == europeanCountry:
		return chinesePrefix + strings.TrimPrefix(ref.Codename, europeanPrefix), typeIDs, chineseCountry, true
	}
	return "", typeIDs, 0, false
}

/*
================
useRaceSwitchScroll

Runs inside the item use's character Update. Refusals change nothing.
================
*/
func (rt *Runtime) useRaceSwitchScroll(use skillItemUse, c *enterworld.Character, scroll commerce.EquipSwitchScroll, tail []byte, result *OpResult) bool {
	if len(tail) != raceSwitchTailBytes {
		return false
	}
	target := tail[0]
	// The bag only: an equipped item (slots 0..12) is refused; the player
	// takes it off first, as for the weapon switch.
	if !inventory.IsBagSlot(target, inventory.BagEnd(c)) {
		*result = itemUseFailure(weaponSwitchErrTarget)
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
		*result = itemUseFailure(weaponSwitchErrTarget)
		return false
	}
	row := c.MissionInventory[index]
	ref, found := refs.ItemRefByCodename(row.Codename)
	if !found || ref == nil || ref.RefObjID != row.RefObjID {
		*result = itemUseFailure(weaponSwitchErrTarget)
		return false
	}
	twinName, twinTypes, twinCountry, ok := raceSwitchTwin(ref, scroll.Kind)
	if !ok {
		*result = itemUseFailure(weaponSwitchErrTarget)
		return false
	}
	twin, twinOK := refs.ItemRefByCodename(twinName)
	if !twinOK || twin == nil || twin.TypeIDs != twinTypes || twin.Country != twinCountry {
		*result = itemUseFailure(weaponSwitchErrNoTwin)
		return false
	}
	if !sameEquipFamily(ref, twin) {
		*result = itemUseFailure(weaponSwitchErrFamily)
		return false
	}
	// Change the target while index still names it: spending the scroll's
	// last unit rebuilds the bag without the scroll's row (gendertool.go).
	moved := &c.MissionInventory[index]
	moved.RefObjID, moved.Codename, moved.TypeFlags = twin.RefObjID, twin.Codename, twin.TypeFlags()
	rt.clampSwitchedDurability(twin, moved)
	changed := *moved
	remaining := rt.consumeItemUseRow(c, use.row)
	state := wire.NewWriter(6).U8(target).U8(itemStateRefFlag).U32(twin.RefObjID).Payload()
	*result = OpResult{Frames: []wire.Frame{
		{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)},
		rt.commerceReferences(invItemsFromRowsWithin([]enterworld.InventoryRow{changed}, int64(inventory.MaxBagEnd)), nil),
		{Opcode: cosItemStateOpcode, Payload: state},
	}}
	result.Frames = append(result.Frames, rt.updateQuestInventory(c)...)
	return true
}
