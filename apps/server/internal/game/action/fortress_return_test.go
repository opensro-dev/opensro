package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
)

/*
================
TestFortressReturnTakesTheOccupiersHome

Natively a member of the guild occupying Jangan returns to its revival gate
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

/*
================
occupyJangan

Puts guild 77 in occupation of Fortress Jangan on the fixture runtime.
================
*/
func occupyJangan(t *testing.T, rt *Runtime, c *enterworld.Character) {
	t.Helper()
	guild := int64(77)
	c.GuildID = &guild
	for _, record := range rt.Fortresses.Records(testDivision) {
		if record.CodeName == "FORTRESS_JANGAN" {
			rt.Fortresses.Occupy(testDivision, record.ID, guild)
		}
	}
}

/*
================
TestFortressReturnFromADungeonWorld

The reference server only refuses the return out of a Battle Arena
world; an occupier standing in a dungeon instance returns to his
fortress like one in the field.
================
*/
func TestFortressReturnFromADungeonWorld(t *testing.T) {
	rt, c, _ := fortressFixtureWithClock(t, testFieldFortGate)
	rt.PushCharacterFrames = func(_, _ string, _ []wire.Frame) {}
	rt.PushDivisionPeerFrames = func(_, _ string, _ []wire.Frame) {}
	occupyJangan(t, rt, c)
	dungeon := instance.Pack(10, 1)
	if _, status := rt.Monsters.AllocatePopulation(testDivision, dungeon); status != instance.Success {
		t.Fatal(status)
	}
	if _, moved := rt.moveWorldMembership(testDivision, c, instance.ID(domain.DefaultWorldInstance), dungeon); !moved {
		t.Fatal("the dungeon move was refused")
	}
	setCharacterWorld(c, dungeon)
	out := rt.HandleFortressReturn(testDivision, c, []byte{1, 0, 0, 0})
	if len(out.Frames) != 0 {
		t.Fatalf("a dungeon return answered %+v", out.Frames)
	}
	if c.World.PackedInstance == nil || instance.ID(*c.World.PackedInstance) != instance.Pack(2, portalWorldLayer) {
		t.Fatalf("the occupier did not reach the fortress: packed %08x spawn %+v", *c.World.PackedInstance, c.World.Spawn)
	}
}

/*
================
TestFortressReturnFromAnArenaWorldIsRefused

Inside a Battle Arena world the return reads "not yet" (the v1.150
client has no arena code of its own on this lane) and the player stays.
================
*/
func TestFortressReturnFromAnArenaWorldIsRefused(t *testing.T) {
	rt, c, _ := fortressFixtureWithClock(t, testFieldFortGate)
	occupyJangan(t, rt, c)
	standInArena(t, c)
	inside := *c.World.PackedInstance
	out := rt.HandleFortressReturn(testDivision, c, []byte{1, 0, 0, 0})
	if len(out.Frames) != 1 || string(out.Frames[0].Payload) != string([]byte{2, fortressReturnNotYet}) {
		t.Fatalf("an arena return answered %+v", out.Frames)
	}
	if *c.World.PackedInstance != inside {
		t.Fatal("the refused return moved the player")
	}
}

/*
================
TestFortressReturnHolderPrecedesTheSuit

The reference server tests the holder before the job suit: a dressed
player whose guild holds nothing hears "no occupied fortress", a
dressed occupier hears the suit refusal.
================
*/
func TestFortressReturnHolderPrecedesTheSuit(t *testing.T) {
	rt, c, _ := fortressFixtureWithClock(t, testFieldFortGate)
	dressJobSuit(t, rt, c, domain.JobTrader)
	out := rt.HandleFortressReturn(testDivision, c, []byte{1, 0, 0, 0})
	if len(out.Frames) != 1 || string(out.Frames[0].Payload) != string([]byte{2, fortressReturnNoFortress}) {
		t.Fatalf("a guildless dressed return answered %+v", out.Frames)
	}
	occupyJangan(t, rt, c)
	out = rt.HandleFortressReturn(testDivision, c, []byte{1, 0, 0, 0})
	if len(out.Frames) != 1 || string(out.Frames[0].Payload) != string([]byte{2, fortressReturnJobSuit}) {
		t.Fatalf("a dressed occupier answered %+v", out.Frames)
	}
}
