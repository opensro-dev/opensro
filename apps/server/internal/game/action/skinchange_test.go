/*
===========================================================================

skinchange_test.go - CGObjPC_ChangeCharacterModel through the skin scroll

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const skinScrollSlot = 25

/*
================
skinFixture

The test character (CHAR_CH_MAN_ADVENTURER) holding two skin scrolls, with
a roster of a Chinese man, a Chinese woman and a European man.
================
*/
func skinFixture(t *testing.T) (*Runtime, *enterworld.Character, *enterworld.ItemRef) {
	t.Helper()
	c := testCharacter()
	scroll := &enterworld.ItemRef{RefObjID: 3800, Codename: "ITEM_MALL_CHAR_SKIN_CHANGE_SCROLL", Country: 3,
		TypeIDs: [4]int64{3, 3, 13, 9}, ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "maxStack": 10})}
	items := testItems()
	items[scroll.Codename] = scroll
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: skinScrollSlot,
		RefObjID: scroll.RefObjID, Codename: scroll.Codename, TypeFlags: scroll.TypeFlags(), StackCount: 2})
	rt, _ := newTestRuntime(c, items)
	rt.deps.(*enterworld.Deps).Roster = &enterworld.Roster{Models: []enterworld.RosterModel{
		{Codename: "CHAR_CH_MAN_ADVENTURER", RefObjID: 1907},
		{Codename: "CHAR_CH_WOMAN_ADVENTURER", RefObjID: 1920},
		{Codename: "CHAR_EU_MAN_NOBLE", RefObjID: 14717},
	}}
	return rt, c, scroll
}

/*
================
skinUse
================
*/
func skinUse(rt *Runtime, c *enterworld.Character, scroll *enterworld.ItemRef, model uint32, shape uint8) OpResult {
	return rt.HandleItemUse(testDivision, c, wire.NewWriter(8).U8(skinScrollSlot).U16(scroll.TypeFlags()).U32(model).U8(shape).Payload())
}

/*
================
TestSkinChangeReloadsTheNewBodyInPlace

A same-gender model is written with its scale and the character enters
teleport mode 3 for its in-place reload; the scroll is spent.
================
*/
func TestSkinChangeReloadsTheNewBodyInPlace(t *testing.T) {
	rt, c, scroll := skinFixture(t)
	out := skinUse(rt, c, scroll, 1907, 0x31)
	if len(out.Frames) < 2 || out.Frames[1].Opcode != wire.OpItemUseResponse || out.Frames[1].Payload[0] != wire.ResultSuccess {
		t.Fatalf("skin change = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	if c.NativeTeleportMode != skinTeleportMode || out.Frames[0].Payload[5] != skinTeleportMode {
		t.Fatalf("teleport mode %d, frame %x", c.NativeTeleportMode, out.Frames[0].Payload)
	}
	if *c.ModelRef != 1907 || *c.BodyShapeByte != 0x31 || *c.HeightIndex != 1 || *c.VolumeIndex != 3 {
		t.Fatalf("model %d shape %#x height %d volume %d", *c.ModelRef, *c.BodyShapeByte, *c.HeightIndex, *c.VolumeIndex)
	}
}

/*
================
TestSkinChangeRefusesWornArmourForAGenderChange

0x1892 for a gender change over a worn helmet; a European model and a
nibble above the window's range refuse without spending the scroll.
================
*/
func TestSkinChangeRefusesWornArmourForAGenderChange(t *testing.T) {
	rt, c, scroll := skinFixture(t)
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 1, RefObjID: 9001,
		Codename: "ITEM_CH_M_HEAVY_01_HA_A", TypeFlags: wire.PackTypeFlags(3, 1, 1, 1), StackCount: 1})
	out := skinUse(rt, c, scroll, 1920, 0x22)
	if len(out.Frames) != 1 || out.Frames[0].Payload[0] != 2 || out.Frames[0].Payload[1] != errCodeSkinArmour {
		t.Fatalf("armoured gender change = %+v", out.Frames)
	}
	for _, refused := range []struct {
		model uint32
		shape uint8
	}{{14717, 0x22}, {1907, 0x52}, {1907, 0x25}} {
		if skinUse(rt, c, scroll, refused.model, refused.shape); c.NativeTeleportMode != 0 || c.ModelRef != nil {
			t.Fatalf("model %d shape %#x was admitted", refused.model, refused.shape)
		}
	}
	c.MissionInventory = c.MissionInventory[:len(c.MissionInventory)-1]
	if skinUse(rt, c, scroll, 1920, 0x22); c.ModelRef == nil || *c.ModelRef != 1920 || *c.Gender != 1 {
		t.Fatal("a bare body could not change gender")
	}
}
