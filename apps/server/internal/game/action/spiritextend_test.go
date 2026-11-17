/*
===========================================================================

spiritextend_test.go - the extension gear lengthens a woken Spirit's clock

Port-only (SRO_NASRUN, #3456 slice 4): a gear dropped on a woken Spirit
in the bag adds seven days, up to 28 ahead, and is spent; a dormant
Spirit, a non-Spirit, the ceiling and the switch off keep the gear.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const (
	extendTestGearSlot   = 21
	extendTestSpiritSlot = 20
	extendTestDay        = 24 * 60 * 60
)

/*
================
extendableSpirit

A character with a Spirit whose data word is data in bag slot 20, two
gears in slot 21, and the gear's use request aimed at target.
================
*/
func extendableSpirit(t *testing.T, socket bool, data uint32, target uint8) (*enterworld.Character, *Runtime, []byte) {
	t.Helper()
	spiritFlags, gearFlags := wire.PackTypeFlags(3, 1, 14, 1), wire.PackTypeFlags(3, 3, 13, 16)
	c := testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{
		{Slot: extendTestSpiritSlot, RefObjID: 90010, Codename: "AVATAR_DEVIL", TypeFlags: spiritFlags, VarianceBits: "0", StackCount: 1, Data: data},
		{Slot: extendTestGearSlot, RefObjID: 90020, Codename: "DEVIL_GEAR", TypeFlags: gearFlags, VarianceBits: "0", StackCount: 2},
	}
	rt, _ := newTestRuntime(c, staticItemSource{
		"AVATAR_DEVIL": {RefObjID: 90010, Codename: "AVATAR_DEVIL", TypeIDs: [4]int64{3, 1, 14, 1}, Country: 3, RequiredSex: 2,
			ReqQuadTypes: [4]int64{-1, -1, -1, -1}, NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1})},
		"DEVIL_GEAR": {RefObjID: 90020, Codename: "DEVIL_GEAR", TypeIDs: [4]int64{3, 3, 13, 16}, Country: 3, RequiredSex: 2,
			ReqQuadTypes: [4]int64{-1, -1, -1, -1}, NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1})},
	})
	rt.NasrunSocket = socket
	request := []byte{extendTestGearSlot, 0, 0, target}
	binary.LittleEndian.PutUint16(request[1:], gearFlags)
	return c, rt, request
}

/*
================
gearsLeft
================
*/
func gearsLeft(c *enterworld.Character) int64 {
	for _, row := range c.MissionInventory {
		if row.Slot == extendTestGearSlot {
			return row.StackCount
		}
	}
	return 0
}

/*
================
TestExtensionGearAddsAWeekAndIsSpent
================
*/
func TestExtensionGearAddsAWeekAndIsSpent(t *testing.T) {
	c, rt, request := extendableSpirit(t, true, 0, extendTestSpiritSlot)
	now := uint32(rt.Now().Unix())
	c.MissionInventory[0].Data = now + 3*extendTestDay
	result := rt.HandleItemUse(testDivision, c, request)
	if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 1 {
		t.Fatalf("gear refused: %+v", result)
	}
	if got, want := c.MissionInventory[0].Data, now+10*extendTestDay; got != want {
		t.Fatalf("clock %d, want the expiry + 7 days (%d)", got, want)
	}
	if gearsLeft(c) != 1 {
		t.Fatalf("gears left %d, want 1", gearsLeft(c))
	}
}

/*
================
TestExtensionGearRefusalsKeepTheGear
================
*/
func TestExtensionGearRefusalsKeepTheGear(t *testing.T) {
	for _, step := range []struct {
		name   string
		socket bool
		target uint8
		ahead  int64 // the Spirit's expiry relative to now, in days; 0 is dormant
		code   uint8
	}{
		{"dormant", true, extendTestSpiritSlot, 0, spiritExtendErrAsleep},
		{"aimed at the gear itself", true, extendTestGearSlot, 3, spiritExtendErrTarget},
		{"aimed at an empty slot", true, 30, 3, spiritExtendErrTarget},
		{"aimed outside the bag", true, 5, 3, spiritExtendErrSlot},
		{"past the ceiling", true, extendTestSpiritSlot, 22, wire.ErrCodeInvalidRequest},
		{"switch off", false, extendTestSpiritSlot, 3, 0},
	} {
		c, rt, request := extendableSpirit(t, step.socket, 0, step.target)
		if step.ahead != 0 {
			c.MissionInventory[0].Data = uint32(rt.Now().Unix() + step.ahead*extendTestDay)
		}
		before := c.MissionInventory[0].Data
		result := rt.HandleItemUse(testDivision, c, request)
		if len(result.Frames) == 0 || result.Frames[0].Payload[0] == 1 {
			t.Fatalf("%s: gear admitted: %+v", step.name, result)
		}
		if step.code != 0 && result.Frames[0].Payload[1] != step.code {
			t.Fatalf("%s: refusal %#x, want %#x", step.name, result.Frames[0].Payload[1], step.code)
		}
		if gearsLeft(c) != 2 || c.MissionInventory[0].Data != before {
			t.Fatalf("%s: gears %d, clock %d (was %d)", step.name, gearsLeft(c), c.MissionInventory[0].Data, before)
		}
	}
}
