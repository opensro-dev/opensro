/*
===========================================================================

cospublication_test.go - native COS movement publishes through its owner

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestCosMovementPublicationRetainsOwnershipGates
================
*/
func TestCosMovementPublicationRetainsOwnershipGates(t *testing.T) {
	character := testCharacter()
	rt, _ := newTestRuntime(character, testCosSource(testItems()))
	character.ActiveCOS = &enterworld.CharacterCOS{
		GID: 0x00C00003, RefObjID: 3914, Codename: "COS_T_DHORSE3",
		CurrentHP: 87829, Summoned: true, Mounted: true,
	}
	rt.MoveCOS = func(string, *enterworld.Character, uint32, []byte) []wire.Frame {
		t.Fatal("transport used the unsequenced movement return path")
		return nil
	}
	calls := 0
	rt.MoveCOSPublished = func(division string, actor *enterworld.Character, command wire.CosCommand, emit func([]wire.Frame)) {
		calls++
		if division != testDivision || actor != character || command.CosGid != character.ActiveCOS.GID {
			t.Fatalf("unexpected mounted movement owner: %s %+v", division, command)
		}
		emit([]wire.Frame{{Opcode: 0xB738}})
	}
	body := func(gid uint32) []byte {
		return wire.NewWriter(14).U32(gid).U8(wire.CosCommandMovementTag).
			U8(1).U16(0x62A8).U16(1000).U16(20).U16(458).Payload()
	}
	var published []wire.Frame
	emit := func(frames []wire.Frame) { published = append(published, frames...) }
	result := rt.handleCosCommand(testDivision, character, body(character.ActiveCOS.GID), emit)
	if calls != 1 || len(result.Frames) != 0 {
		t.Fatalf("mounted publication duplicated or missing: calls=%d result=%+v", calls, result)
	}
	assertOpcodes(t, published, 0xB738)
	rt.handleCosCommand(testDivision, character, body(character.ActiveCOS.GID+1), emit)
	character.ActiveCOS.Mounted = false
	rt.handleCosCommand(testDivision, character, body(character.ActiveCOS.GID), emit)
	if calls != 1 || len(published) != 1 {
		t.Fatal("foreign or unmounted actor reached publication")
	}
}
