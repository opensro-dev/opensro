/*
===========================================================================

gendertool_test.go - the special consumable's case 7, the armour gender change

===========================================================================
*/

package action

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
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

/*
================
assertReferencedBeforeNamed

frames is what one audience receives, in order. Every frame that names an
item - the 0x3449 use effect, the 0x3314 equip visual, the 0x3645 ref
change - must follow a 0x000E reference row for that id in the same
stream: the browser has no item table, and an unknown id ends its session
(client incidents 0x3314 "Unknown peer equipment reference" and 0x3449
"Missing external item reference 3787").
================
*/
func assertReferencedBeforeNamed(t *testing.T, audience string, frames []wire.Frame) {
	t.Helper()
	known := map[uint32]bool{}
	for index, frame := range frames {
		named := uint32(0)
		switch frame.Opcode {
		case opCommerceItemReferences:
			var delta struct {
				Items []struct {
					ID uint32 `json:"refObjId"`
				} `json:"items"`
			}
			if err := json.Unmarshal(frame.Payload, &delta); err != nil {
				t.Fatalf("%s frame %d: reference payload: %v", audience, index, err)
			}
			for _, row := range delta.Items {
				known[row.ID] = true
			}
			continue
		case wire.OpItemUseVisual:
			named = binary.LittleEndian.Uint32(frame.Payload[4:])
		case wire.OpEquipVisual:
			named = binary.LittleEndian.Uint32(frame.Payload[5:])
		case cosItemStateOpcode:
			if frame.Payload[1] == itemStateRefFlag {
				named = binary.LittleEndian.Uint32(frame.Payload[2:])
			}
		}
		if named != 0 && !known[named] {
			t.Fatalf("%s frame %d (0x%04X) names item %d before its reference", audience, index, frame.Opcode, named)
		}
	}
}

/*
================
TestGenderToolReferencesEveryItemItNames

The owner sees the tool's use effect and the swapped item's new reference;
nearby players see the effect. Each audience gets every named item's
reference first, the owner included: an owner's browser may not hold the
tool's row (prod 0x3449 incident, ref 3787).
================
*/
func TestGenderToolReferencesEveryItemItNames(t *testing.T) {
	rt, c, tool := genderFixture(t, 1)
	out := rt.HandleItemUse(testDivision, c, wire.NewWriter(4).U8(25).U16(tool.TypeFlags()).U8(21).Payload())
	if len(out.Frames) == 0 || out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("use = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	if _, ok := findFrame(out.Frames, wire.OpItemUseVisual); !ok {
		t.Fatal("the owner saw no use effect")
	}
	if _, ok := findFrame(out.Broadcast, wire.OpItemUseVisual); !ok {
		t.Fatal("nearby players saw no use effect")
	}
	assertReferencedBeforeNamed(t, "owner", out.Frames)
	assertReferencedBeforeNamed(t, "observers", out.Broadcast)
}

/*
================
TestGenderToolChangesOnlyItsTarget

A one-use tool listed before its target leaves the bag one row shorter once
spent; the change must still land on the target's slot, not on whatever row
now sits at the target's old position, and a target that was the last row
must not fall off the end.
================
*/
func TestGenderToolChangesOnlyItsTarget(t *testing.T) {
	for _, follower := range []bool{true, false} {
		rt, c, tool := genderFixture(t, 1)
		var helmet, spent enterworld.InventoryRow
		var rest []enterworld.InventoryRow
		for _, row := range c.MissionInventory {
			switch row.Slot {
			case 21:
				helmet = row
			case 25:
				spent = row
			default:
				rest = append(rest, row)
			}
		}
		spent.StackCount = 1
		rows := append(append([]enterworld.InventoryRow(nil), rest...), spent, helmet)
		neighbour := enterworld.InventoryRow{Slot: 30, RefObjID: helmet.RefObjID, Codename: helmet.Codename, TypeFlags: helmet.TypeFlags, StackCount: 1, Plus: 7}
		if follower {
			rows = append(rows, neighbour)
		}
		c.MissionInventory = rows
		out := rt.HandleItemUse(testDivision, c, wire.NewWriter(4).U8(25).U16(tool.TypeFlags()).U8(21).Payload())
		if len(out.Frames) < 3 || out.Frames[0].Payload[0] != wire.ResultSuccess {
			t.Fatalf("follower=%v: use = %+v / %q", follower, out.Frames, out.DiagnosticRefusal)
		}
		for _, row := range c.MissionInventory {
			switch row.Slot {
			case 21:
				if row.Codename != "ITEM_CH_W_HEAVY_01_HA_A" || row.Plus != 3 {
					t.Fatalf("follower=%v: target %+v not changed", follower, row)
				}
			case 30:
				if row.Codename != helmet.Codename || row.Plus != 7 {
					t.Fatalf("follower=%v: the next row changed instead: %+v", follower, row)
				}
			case 25:
				t.Fatalf("follower=%v: the spent tool survived", follower)
			}
		}
	}
}
