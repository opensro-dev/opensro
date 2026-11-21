/*
===========================================================================

weaponswitch.go - the weapon switch scroll: a bag weapon becomes another kind

Port-only, not native (owner, 2026-11-04). The scroll and its listing live
in commerce/weaponswitch.go; this file serves its use, behind the same
SRO_WEAPON_SWITCH_SCROLL. Off is native: the donor item is an item no
family serves.

The browser client arms an item-target cursor when the scroll is used,
the player clicks a weapon in the bag, then picks the new kind in a
choice box. The use request is 0x75BD [slot][type][u8 target slot]
[u8 weapon kind], the kind being the weapon's fourth TypeID (2..6
Chinese, 7..15 European), of either race. The answer is the armour gender tool's
(gendertool.go): the target's reference changes in place, a reference
row for the new item, then 0x3645 [slot][1][u32 ref]. Every other field
of the item (white stats, durability, plus, blue options) is kept
(owner, 2026-11-21: enhanced weapons switch too). The twin stays in the
target's equipment family and socket (sameEquipFamily), the only inputs
of the blue options' target rule (alchemy Magic.Allows), so every blue
stays valid on it.

INFERENCE: the new item is found by its codename. Every shipped weapon is
ITEM_<race>_<kind>_<degree>_<grade>[_RARE...], and its siblings of the
other kinds share everything after the kind, so the port swaps the race
and kind segments and requires the twin to exist with the chosen kind's
TypeIDs and race. The two races carry the same degree, grade and seal
suffixes with the same required level per degree (itemdata: degree 8
asks level 64 of a Chinese sword and a European dagger alike), so a
twin of the other race is the same tier. An odd weapon (quest, event,
fortress) has no twin and is refused.

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

// 0xB5BD failure bytes of the scroll's own refusals. They reuse the
// unmapped run the name-change scroll took (namechange.go): the browser
// client reads them as this scroll's lines only while its use is in
// flight, and one item use is in flight at a time.
const (
	weaponSwitchErrTarget uint8 = 0xF5 // not a weapon the scroll changes
	weaponSwitchErrFamily uint8 = 0xF6 // the blues could not follow the twin
	weaponSwitchErrKind   uint8 = 0xF7 // the chosen kind is not offered
	weaponSwitchErrNoTwin uint8 = 0xF8 // no weapon of that kind matches
)

// weaponSwitchTailBytes is the tail after the type word: target slot and
// weapon kind.
const weaponSwitchTailBytes = 2

/*
================
weaponKind

One weapon kind: its fourth TypeID, its race (itemdata Country) and the
codename segment its shipped weapons carry.
================
*/
type weaponKind struct {
	kind    int64
	country int64
	segment string
}

// weaponKinds is every weapon kind a player wields (TypeIDs 3/1/6/x); 16,
// the fortress siege weapon, is not one.
var weaponKinds = []weaponKind{
	{2, 0, "SWORD"}, {3, 0, "BLADE"}, {4, 0, "SPEAR"}, {5, 0, "TBLADE"}, {6, 0, "BOW"},
	{7, 1, "SWORD"}, {8, 1, "TSWORD"}, {9, 1, "AXE"}, {10, 1, "DARKSTAFF"}, {11, 1, "TSTAFF"},
	{12, 1, "CROSSBOW"}, {13, 1, "DAGGER"}, {14, 1, "HARP"}, {15, 1, "STAFF"},
}

/*
================
findWeaponKind
================
*/
func findWeaponKind(kind int64) (weaponKind, bool) {
	for _, row := range weaponKinds {
		if row.kind == kind {
			return row, true
		}
	}
	return weaponKind{}, false
}

/*
================
weaponKindPrefix

The codename prefix a kind's weapons share, "ITEM_CH_SPEAR_".
================
*/
func weaponKindPrefix(row weaponKind) string {
	race := "CH"
	if row.country == 1 {
		race = "EU"
	}
	return "ITEM_" + race + "_" + row.segment + "_"
}

/*
================
weaponSwitchTwin

The codename of a weapon's twin of another kind, of either race, or ""
when the weapon does not carry its own kind's shape or the kind is its own.
================
*/
func weaponSwitchTwin(ref *enterworld.ItemRef, kind int64) string {
	from, ok := findWeaponKind(ref.TypeIDs[3])
	to, toOK := findWeaponKind(kind)
	if !ok || !toOK || from.kind == to.kind {
		return ""
	}
	prefix := weaponKindPrefix(from)
	if !strings.HasPrefix(ref.Codename, prefix) {
		return ""
	}
	return weaponKindPrefix(to) + strings.TrimPrefix(ref.Codename, prefix)
}

/*
================
isSwitchableWeapon

A wielded weapon (3/1/6, a player kind), sealed or not, of any degree.
The shield (3/1/4) is no weapon here.
================
*/
func isSwitchableWeapon(ref *enterworld.ItemRef) bool {
	if ref.TypeIDs[0] != 3 || ref.TypeIDs[1] != 1 || ref.TypeIDs[2] != 6 {
		return false
	}
	_, ok := findWeaponKind(ref.TypeIDs[3])
	return ok
}

/*
================
equipFamily

An equipment reference's blue-option family, alchemy's category():
weapon, shield, accessory or armor by the third TypeID, "" otherwise.
================
*/
func equipFamily(ref *enterworld.ItemRef) string {
	if ref.TypeIDs[0] != 3 || ref.TypeIDs[1] != 1 {
		return ""
	}
	switch ref.TypeIDs[2] {
	case 6:
		return "weapon"
	case 4:
		return "shield"
	case 5, 12:
		return "accessory"
	case 1, 2, 3, 9, 10, 11:
		return "armor"
	}
	return ""
}

/*
================
sameEquipFamily

Whether a switch keeps the item in the same blue-option family and
equipment socket: alchemy's Magic.Allows reads only those two, so a
blue the item carries stays valid on its twin.
================
*/
func sameEquipFamily(from, to *enterworld.ItemRef) bool {
	family := equipFamily(from)
	if family == "" || family != equipFamily(to) {
		return false
	}
	fromSocket, fromOK := inventory.EquipSocketForTypeFlags(from.TypeFlags())
	toSocket, toOK := inventory.EquipSocketForTypeFlags(to.TypeFlags())
	return fromOK == toOK && fromSocket == toSocket
}

/*
================
useEquipSwitchScroll

Runs one switch scroll's use by what it switches.
================
*/
func (rt *Runtime) useEquipSwitchScroll(use skillItemUse, c *enterworld.Character, tail []byte, result *OpResult) bool {
	scroll, ok := commerce.EquipSwitchScrollByCodename(use.ref.Codename)
	if !ok {
		return false
	}
	switch scroll.Kind {
	case commerce.SwitchWeapon:
		return rt.useWeaponSwitchScroll(use, c, tail, result)
	}
	return false
}

/*
================
useWeaponSwitchScroll

Runs inside the item use's character Update. Refusals change nothing.
================
*/
func (rt *Runtime) useWeaponSwitchScroll(use skillItemUse, c *enterworld.Character, tail []byte, result *OpResult) bool {
	if len(tail) != weaponSwitchTailBytes {
		return false
	}
	target, kind := tail[0], int64(tail[1])
	// The bag only: an equipped weapon (slots 0..12) is refused, as the
	// owner asked; the player takes it off first.
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
	if !found || ref == nil || ref.RefObjID != row.RefObjID || !isSwitchableWeapon(ref) {
		*result = itemUseFailure(weaponSwitchErrTarget)
		return false
	}
	twinName := weaponSwitchTwin(ref, kind)
	if twinName == "" {
		*result = itemUseFailure(weaponSwitchErrKind)
		return false
	}
	to, _ := findWeaponKind(kind)
	twin, twinOK := refs.ItemRefByCodename(twinName)
	if !twinOK || twin == nil || twin.TypeIDs != [4]int64{3, 1, 6, kind} || twin.Country != to.country {
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
