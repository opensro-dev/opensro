/*
===========================================================================

spiritwake_test.go - using a Devil's Spirit wakes its clock once

Port-only (SRO_NASRUN, #3456 step 2). The v1.188 server wakes a dormant
Spirit for its own Param1 on use, keeps the item, and refuses a second
use; with the switch off the item is v1.150's, which no use admits.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// spiritTestPeriod is the mall Spirits' Param1: 28 days. Their CanUse
// is 1 (VSRO itemdata row 24405), so the use admission lets them through.
const spiritTestPeriod = 2_419_200

/*
================
wakeableSpirit

A character with a dormant Spirit in bag slot 20 whose row carries the
mall Spirits' period, and the use request for it.
================
*/
func wakeableSpirit(t *testing.T, socket bool) (*enterworld.Character, *Runtime, []byte) {
	t.Helper()
	typeFlags := wire.PackTypeFlags(3, 1, 14, 1)
	c := testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 20, RefObjID: 90010, Codename: "AVATAR_DEVIL", TypeFlags: typeFlags, VarianceBits: "0", StackCount: 1}}
	rt, _ := newTestRuntime(c, staticItemSource{"AVATAR_DEVIL": {RefObjID: 90010, Codename: "AVATAR_DEVIL", TypeIDs: [4]int64{3, 1, 14, 1}, Country: 3, RequiredSex: 2,
		ReqQuadTypes: [4]int64{-1, -1, -1, -1}, NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, spiritPeriodField: spiritTestPeriod})}})
	rt.NasrunSocket = socket
	request := []byte{20, 0, 0}
	binary.LittleEndian.PutUint16(request[1:], typeFlags)
	return c, rt, request
}

/*
================
TestUsingASpiritWakesItOnce
================
*/
func TestUsingASpiritWakesItOnce(t *testing.T) {
	c, rt, request := wakeableSpirit(t, true)
	result := rt.HandleItemUse(testDivision, c, request)
	if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpItemUseResponse || result.Frames[0].Payload[0] != 1 {
		t.Fatalf("use refused: %+v", result)
	}
	want := spiritAwakeFor(rt, spiritTestPeriod)
	if len(c.MissionInventory) != 1 || c.MissionInventory[0].Data != want || c.MissionInventory[0].StackCount != 1 {
		t.Fatalf("after use: %+v, want the Spirit kept with its clock at now + 28 days (%d)", c.MissionInventory, want)
	}
	// A woken Spirit refuses a second use and keeps its clock.
	if again := rt.HandleItemUse(testDivision, c, request); len(again.Frames) == 0 || again.Frames[0].Payload[0] == 1 {
		t.Fatalf("second use admitted: %+v", again)
	}
	if c.MissionInventory[0].Data != want {
		t.Fatalf("second use moved the clock to %d", c.MissionInventory[0].Data)
	}
}

/*
================
TestASpiritUseNeedsTheSwitch
================
*/
func TestASpiritUseNeedsTheSwitch(t *testing.T) {
	c, rt, request := wakeableSpirit(t, false)
	if result := rt.HandleItemUse(testDivision, c, request); len(result.Frames) == 0 || result.Frames[0].Payload[0] == 1 {
		t.Fatalf("switch off: use admitted: %+v", result)
	}
	if c.MissionInventory[0].Data != 0 {
		t.Fatalf("switch off: the clock moved to %d", c.MissionInventory[0].Data)
	}
}
