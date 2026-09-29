/*
===========================================================================

coscommand_test.go - tests for coscommand.go

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
==================
TestCosSteerAndStopReachTheMovementOwner

The vehicle's steer (tag 0x04) and stop (tag 0x03) pass the same COS gates
as its moves and are handed, with their heading, to the movement owner;
its frames come back as the command's result. An unmounted vehicle or a
foreign gid never reaches it.
==================
*/
func TestCosSteerAndStopReachTheMovementOwner(t *testing.T) {
	character := testCharacter()
	rt, _ := newTestRuntime(character, testCosSource(testItems()))
	character.ActiveCOS = &enterworld.CharacterCOS{
		GID: 0x00C00003, RefObjID: 3914, Codename: "COS_T_DHORSE3",
		CurrentHP: 87829, Summoned: true, Mounted: true,
	}

	type call struct {
		gid     uint32
		heading uint16
	}
	var steers, stops []call
	self := wire.Frame{Opcode: 0xB2F5}
	observed := wire.Frame{Opcode: 0xB2CF}
	rt.SteerCOS = func(_ string, _ *enterworld.Character, gid uint32, heading uint16) ([]wire.Frame, []wire.Frame) {
		steers = append(steers, call{gid, heading})
		return nil, []wire.Frame{observed}
	}
	rt.StopCOS = func(_ string, _ *enterworld.Character, gid uint32, heading uint16) ([]wire.Frame, []wire.Frame) {
		stops = append(stops, call{gid, heading})
		return []wire.Frame{self}, []wire.Frame{self}
	}
	body := func(gid uint32, tag uint8, heading uint16) []byte {
		return wire.NewWriter(7).U32(gid).U8(tag).U16(heading).Payload()
	}

	steer := rt.HandleCosCommand(testDivision, character, body(character.ActiveCOS.GID, wire.CosCommandSteerTag, 0x1234))
	assertOpcodes(t, steer.Broadcast, 0xB2CF)
	stop := rt.HandleCosCommand(testDivision, character, body(character.ActiveCOS.GID, wire.CosCommandStopTag, 0x4321))
	assertOpcodes(t, stop.Frames, 0xB2F5)
	assertOpcodes(t, stop.Broadcast, 0xB2F5)
	if len(steers) != 1 || steers[0] != (call{0x00C00003, 0x1234}) || len(stops) != 1 || stops[0] != (call{0x00C00003, 0x4321}) {
		t.Fatalf("steers %+v stops %+v", steers, stops)
	}

	rt.HandleCosCommand(testDivision, character, body(0x00C00004, wire.CosCommandSteerTag, 1))
	character.ActiveCOS.Mounted = false
	rt.HandleCosCommand(testDivision, character, body(character.ActiveCOS.GID, wire.CosCommandStopTag, 1))
	if len(steers) != 1 || len(stops) != 1 {
		t.Fatalf("gated commands reached the movement owner: steers %+v stops %+v", steers, stops)
	}
}
