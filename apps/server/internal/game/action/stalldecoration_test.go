/*
===========================================================================

stalldecoration_test.go - the Item Mall stall booth decorations (#455)

===========================================================================
*/

package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestStallDecorationIsAppliedAndShown

The native item use case 3: using a booth item consumes it and keeps its reference as
the character's decoration; the next stall opens with it (0x30DF). The
purchased decoration wins over a running premium BFI1 booth (native
lookup reads the persistent decoration unless a per-session 0x7160
override is set); the premium booth shows only when nothing was
purchased, and its expiry falls back to the default stall.
================
*/
func TestStallDecorationIsAppliedAndShown(t *testing.T) {
	rt, owner, _, _ := stallFixture(t)
	booth := &enterworld.ItemRef{RefObjID: 3848, Codename: "ITEM_MALL_BOOTH_MOB_BIGEYEGHOST", TypeIDs: [4]int64{3, 3, 3, 4},
		ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 1, "canUse": 1})}
	if family := admittedItemUseFamily(booth); family != itemUseStallDecoration {
		t.Fatalf("booth family %d", family)
	}
	opened := func() uint32 {
		out := rt.HandleStallCreate(testDivision, owner, wire.NewWriter(16).WStr("Shop").Payload())
		payload := out.Frames[1].Payload
		decoration := uint32(payload[len(payload)-4]) | uint32(payload[len(payload)-3])<<8 |
			uint32(payload[len(payload)-2])<<16 | uint32(payload[len(payload)-1])<<24
		rt.HandleStallClose(testDivision, owner, nil)
		return decoration
	}
	if got := opened(); got != 0 {
		t.Fatalf("an undecorated stall shows %d", got)
	}
	owner.StallDecoration = booth.RefObjID
	if got := opened(); got != booth.RefObjID {
		t.Fatalf("the decorated stall shows %d", got)
	}
	now := rt.Now().UnixMilli()
	owner.CompositeJobs = []domain.CompositeJob{{Kind: domain.CompositeBuffItem, Target: 3850, EndUnixMs: now + 1000}}
	if got := opened(); got != booth.RefObjID {
		t.Fatalf("a running premium booth shadows the purchased decoration: %d", got)
	}
	owner.StallDecoration = 0
	if got := opened(); got != 3850 {
		t.Fatalf("the premium booth shows %d with no purchased decoration", got)
	}
	owner.CompositeJobs[0].EndUnixMs = now
	if got := opened(); got != 0 {
		t.Fatalf("an ended premium booth still shows: %d", got)
	}
	owner.StallDecoration = booth.RefObjID
	if got := opened(); got != booth.RefObjID {
		t.Fatalf("the purchased decoration after the premium expiry shows %d", got)
	}
	if !bytes.Equal(wire.EncodeStallOpened(7, "A", 3848)[len(wire.EncodeStallOpened(7, "A", 3848))-4:], []byte{0x08, 0x0f, 0, 0}) {
		t.Fatal("0x30DF does not end with the decoration")
	}
}

/*
================
TestStallDecorationItemUseConsumesAndStores
================
*/
func TestStallDecorationItemUseConsumesAndStores(t *testing.T) {
	c := testCharacter()
	items := testItems()
	booth := &enterworld.ItemRef{RefObjID: 3848, Codename: "ITEM_MALL_BOOTH_MOB_BIGEYEGHOST", TypeIDs: [4]int64{3, 3, 3, 4},
		ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 1, "canUse": 1})}
	items[booth.Codename] = booth
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: 21, RefObjID: booth.RefObjID, Codename: booth.Codename, TypeFlags: booth.TypeFlags(), StackCount: 1,
	})
	rt, _ := newTestRuntime(c, items)
	result := rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(21).U16(booth.TypeFlags()).Payload())
	if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpItemUseResponse || result.Frames[0].Payload[0] != 1 {
		t.Fatalf("use answered %+v (%s)", result.Frames, result.DiagnosticRefusal)
	}
	if c.StallDecoration != booth.RefObjID {
		t.Fatalf("decoration %d", c.StallDecoration)
	}
	for _, row := range c.MissionInventory {
		if row.Slot == 21 && row.StackCount > 0 {
			t.Fatal("the booth item was not consumed")
		}
	}
}
