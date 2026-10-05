/*
===========================================================================

gendertool_test.go - 49C2B0 case 7, the armour gender change

===========================================================================
*/

package action

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
genderFixture

A man's degree-1 helmet in bag slot 20, its woman's twin in the data, and
a tool for degrees up to 3 in slot 25.
================
*/
func genderFixture(t *testing.T, class float64) (*Runtime, *enterworld.Character, *enterworld.ItemRef) {
	t.Helper()
	c := testCharacter()
	items := testItems()
	helmet := func(id uint32, code string) *enterworld.ItemRef {
		return &enterworld.ItemRef{RefObjID: id, Codename: code, Country: 3, TypeIDs: [4]int64{3, 1, 1, 1},
			ReqQuadTypes: [4]int64{-1, -1, -1, -1}, NativeFields: enterworld.NewNativeFields(map[string]float64{"itemClass": class})}
	}
	man, woman := helmet(9001, "ITEM_CH_M_HEAVY_01_HA_A"), helmet(9002, "ITEM_CH_W_HEAVY_01_HA_A")
	tool := &enterworld.ItemRef{RefObjID: 3830, Codename: "ITEM_MALL_EQUIP_TRANSGENDER_1", Country: 3,
		TypeIDs: [4]int64{3, 3, 13, 8}, ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "maxStack": 10, "itemParam1_29c": 3})}
	for _, ref := range []*enterworld.ItemRef{man, woman, tool} {
		items[ref.Codename] = ref
	}
	c.MissionInventory = append(c.MissionInventory,
		enterworld.InventoryRow{Slot: 21, RefObjID: man.RefObjID, Codename: man.Codename, TypeFlags: man.TypeFlags(), StackCount: 1, Plus: 3},
		enterworld.InventoryRow{Slot: 25, RefObjID: tool.RefObjID, Codename: tool.Codename, TypeFlags: tool.TypeFlags(), StackCount: 2})
	rt, _ := newTestRuntime(c, items)
	return rt, c, tool
}

/*
================
TestGenderToolSwapsTheArmourInPlace
================
*/
func TestGenderToolSwapsTheArmourInPlace(t *testing.T) {
	rt, c, tool := genderFixture(t, 1)
	out := rt.HandleItemUse(testDivision, c, wire.NewWriter(4).U8(25).U16(tool.TypeFlags()).U8(21).Payload())
	if len(out.Frames) < 3 || out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("use = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	want := binary.LittleEndian.AppendUint32([]byte{21, itemStateRefFlag}, 9002)
	state, ok := findFrame(out.Frames, cosItemStateOpcode)
	if !ok || !bytes.Equal(state.Payload, want) {
		t.Fatalf("state frame %x in %+v", state.Payload, out.Frames)
	}
	for _, row := range c.MissionInventory {
		if row.Slot == 21 && (row.Codename != "ITEM_CH_W_HEAVY_01_HA_A" || row.Plus != 3) {
			t.Fatalf("swapped row %+v", row)
		}
	}
}

/*
================
TestGenderToolRefusesAHighDegreeOrAWeapon

A degree-5 helmet is above the tool's 3 (0x1883); the sword has no twin
(0x1884).
================
*/
func TestGenderToolRefusesAHighDegreeOrAWeapon(t *testing.T) {
	rt, c, tool := genderFixture(t, 13)
	assertItemUseRefusedUnchanged(t, rt, c, wire.NewWriter(4).U8(25).U16(tool.TypeFlags()).U8(21).Payload(), genderErrDegree)
	c.MissionInventory[0].Slot = 22
	assertItemUseRefusedUnchanged(t, rt, c, wire.NewWriter(4).U8(25).U16(tool.TypeFlags()).U8(22).Payload(), genderErrTarget)
}
