/*
===========================================================================

modelswitcher.go - the Item Mall weapon model switchers

Port-only, not native (owner ask 2026-11-12, issue #3257): the later
mall sells one model switcher per weapon type,
ITEM_ORIGIN_<CH|EU>_HW_<TYPE>_SCROLL ("Right click to active
scroll"). v1.150 has no such item; the rows ride the optional
later-client import (the #3257 data line), as do the
weapons' look-alike twins, ITEM_<CH|EU>_<TYPE>_09_<A|B|C>[_RARE]_HWSET:
each twin is its native degree-9 row with only its identity, codename,
model (AssocFileObj) and icon changed, so the switch is cosmetic and
native balance stands. Behind SRO_MALL_CATEGORIES (default off) the
mall lists the switchers in HOT shop > Halloween Models
(commerce/mallcategories.go) and this file serves their use; off, the
rows are dead stock.

The use is the armour gender change tool's (gendertool.go) on a
weapon. The switchers ship as 3/3/13/8, the tool's own type, so the
v1.150 client arms the same item-target cursor and sends the bag slot
the scroll was dropped on, 0x75BD [slot][type][u8 target]. The server
swaps the target's reference in place to its twin (or back again: the
switch is a toggle) and answers 0x3645 [slot][1][u32 ref], keeping
plus, options and durability. A worn weapon is refused, as the tool
refuses one: the v1.150 client has no frame that rewrites a worn
socket, only the bag's.

INFERENCE: the later client's server rule is not in either binary
this port reads. The switcher names its race and weapon type, and only
degree-9 rows have twins, so the port admits exactly the bag weapon of
that race and type at degree 9 (or its twin), and refuses everything
else with the tool's notices: 0x82 a worn or empty slot, 0x84 a wrong
target.

===========================================================================
*/

package action

import (
	"regexp"
	"slices"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

// ModelSwitcherTwinSuffix marks a degree-9 weapon's look-alike twin.
const ModelSwitcherTwinSuffix = "_HWSET"

// modelSwitcherScroll reads a switcher's race and weapon type from its
// codename; which codenames are switchers is commerce.ModelSwitcherItems.
var modelSwitcherScroll = regexp.MustCompile(`^ITEM_ORIGIN_(CH|EU)_HW_([A-Z]+)_SCROLL$`)

// modelSwitcherTarget names a degree-9 weapon or shield, plain or twin.
var modelSwitcherTarget = regexp.MustCompile(`^ITEM_(CH|EU)_([A-Z]+)_09_[ABC](_RARE)?(_HWSET)?$`)

/*
================
IsModelSwitcher

Whether a codename is one of the sixteen switchers the mall sells. The
list is the one owner, so the mall and the use cannot disagree on what
a switcher is.
================
*/
func IsModelSwitcher(codename string) bool {
	return slices.Contains(commerce.ModelSwitcherItems, codename)
}

/*
================
modelSwitcherTwin

The codename a switcher turns target into, or "" when the switcher
does not apply to it: the twin of a plain degree-9 row of the
switcher's race and type, or the plain row of such a twin.
================
*/
func modelSwitcherTwin(switcher, target string) string {
	scroll := modelSwitcherScroll.FindStringSubmatch(switcher)
	item := modelSwitcherTarget.FindStringSubmatch(target)
	if scroll == nil || item == nil || scroll[1] != item[1] || scroll[2] != item[2] {
		return ""
	}
	if item[4] != "" {
		return strings.TrimSuffix(target, ModelSwitcherTwinSuffix)
	}
	return target + ModelSwitcherTwinSuffix
}

/*
================
useModelSwitcher

Runs inside the item use's character Update, as useGenderTool does.
The flag is re-checked here so a switcher bought while the categories
stood open is dead stock once they close.
================
*/
func (rt *Runtime) useModelSwitcher(use skillItemUse, c *enterworld.Character, tail []byte, result *OpResult) bool {
	if on, err := commerce.MallCategoriesFromEnv(); err != nil || !on {
		result.DiagnosticRefusal = "item-use: model switchers are off (" + commerce.EnvMallCategories + ")"
		return false
	}
	r := wire.NewReader(tail)
	target, e := r.U8()
	if e != nil || r.Done() != nil || !inventory.IsBagSlot(target, inventory.BagEnd(c)) {
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
		*result = itemUseFailure(genderErrSlot)
		return false
	}
	row := c.MissionInventory[index]
	ref, ok := refs.ItemRefByCodename(row.Codename)
	twinName := modelSwitcherTwin(use.ref.Codename, row.Codename)
	twin, twinOK := refs.ItemRefByCodename(twinName)
	if !ok || ref == nil || !isEquipmentItem(row.TypeFlags) || twinName == "" || !twinOK || twin == nil ||
		twin.TypeIDs != ref.TypeIDs {
		*result = itemUseFailure(genderErrTarget)
		return false
	}
	// Change the target while index still names it: spending the
	// scroll's last unit rebuilds the bag without its row.
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
