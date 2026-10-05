package action

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
)

/*
================
TestFortressReturnTakesTheOccupiersHome

51A5B0: a member of the guild occupying Jangan returns to its revival gate
and the action window hears the 600 s cooldown (0x3792 [2][5]); a second
return is refused "not yet" (0xB025 [2][8]), and a PC without an occupied
fortress is refused 7.
================
*/
func TestFortressReturnTakesTheOccupiersHome(t *testing.T) {
	rt, c, _ := fortressFixtureWithClock(t, testFieldFortGate)
	var pushed []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { pushed = append(pushed, frames...) }
	rt.PushDivisionPeerFrames = func(_, _ string, _ []wire.Frame) {}
	request := []byte{1, 0, 0, 0}
	out := rt.HandleFortressReturn(testDivision, c, request)
	if len(out.Frames) != 1 || out.Frames[0].Opcode != opFortressReturnResult || string(out.Frames[0].Payload) != string([]byte{2, fortressReturnNoFortress}) {
		t.Fatalf("a guildless return answered %+v", out.Frames)
	}
	guild := int64(77)
	c.GuildID = &guild
	for _, record := range rt.Fortresses.Records(testDivision) {
		if record.CodeName == "FORTRESS_JANGAN" {
			rt.Fortresses.Occupy(testDivision, record.ID, guild)
		}
	}
	if out = rt.HandleFortressReturn(testDivision, c, request); len(out.Frames) != 0 {
		t.Fatalf("the occupier was refused: %+v", out.Frames)
	}
	if c.World.PackedInstance == nil || instance.ID(*c.World.PackedInstance) != instance.Pack(2, portalWorldLayer) || *c.World.Spawn.RegionID != 17735 {
		t.Fatalf("the occupier returned to %+v", c.World)
	}
	last := pushed[len(pushed)-1]
	if last.Opcode != opTimedJobState || string(last.Payload) != string([]byte{2, 5, 0x58, 0x02, 0, 0}) {
		t.Fatalf("cooldown frame %#x % X", last.Opcode, last.Payload)
	}
	out = rt.HandleFortressReturn(testDivision, c, request)
	if len(out.Frames) != 1 || string(out.Frames[0].Payload) != string([]byte{2, fortressReturnNotYet}) {
		t.Fatalf("a return during the cooldown answered %+v", out.Frames)
	}
	if frames := FortressReturnCooldownFrames(c, rt.Now().UnixMilli()); len(frames) != 1 {
		t.Fatal("entry does not restore the cooldown")
	}
}
