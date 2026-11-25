/*
===========================================================================

reverseuser_test.go - the reverse return scroll's party and user moves

The enroll button spends nothing and records the standing point; the
user move refuses with no point enrolled; the party move refuses a
member who left the party and one standing in an instanced world, and
travels to a member standing in the field.

===========================================================================
*/

package action

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
reversePartyTail

Choice 4 and the member's little-endian gid.
================
*/
func reversePartyTail(gid uint32) []byte {
	return []byte{reverseReturnPartyChoice, byte(gid), byte(gid >> 8), byte(gid >> 16), byte(gid >> 24)}
}

/*
================
reverseUserMate

A second character in the same division, standing in the field at the
given point, on the fixture's roster and in the player's party.
================
*/
func reverseUserMate(t *testing.T, rt *Runtime, c *enterworld.Character, at simulation.Spawn) *enterworld.Character {
	t.Helper()
	mate := *c
	mate.ID, mate.Name = 77, "mate"
	mate.MissionInventory = nil
	world := *c.World
	spawn := worldSpawnFromMission(at)
	world.Spawn = spawn
	mate.World = &world
	deps := rt.deps.(*enterworld.Deps)
	fixtureCharacters(deps.Characters)[testDivision] = append(fixtureCharacters(deps.Characters)[testDivision], &mate)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(&mate)}}}
	}
	return &mate
}

/*
================
reverseScrollStack

The stack left in the fixture's scroll row.
================
*/
func reverseScrollStack(c *enterworld.Character) int64 {
	return c.MissionInventory[len(c.MissionInventory)-1].StackCount
}

/*
================
TestReverseUserEnrollRecordsTheStandingPoint
================
*/
func TestReverseUserEnrollRecordsTheStandingPoint(t *testing.T) {
	rt, c, clock := reverseReturnFixture(t)
	at := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, clock.NowMs())
	out := useReverseScroll(rt, c, reverseReturnEnrollChoice)
	if len(out.Frames) < 2 || out.Frames[0].Opcode != wire.OpItemUseResponse ||
		out.Frames[0].Payload[0] != 1 || out.Frames[0].Payload[1] != 23 {
		t.Fatalf("the enroll answered %+v", out.Frames)
	}
	point, found := findFrame(out.Frames, OpReverseUserPoint)
	if !found || len(point.Payload) != 15 || point.Payload[0] != 1 {
		t.Fatalf("the saved-location frame is %+v", point)
	}
	if reverseScrollStack(c) != 2 || c.NativeTeleportMode != 0 {
		t.Fatal("the enroll spent a scroll or started a cast")
	}
	if c.ReverseUserPoint == nil ||
		missionSpawnFromWorld(&c.ReverseUserPoint.WorldSpawn, simulation.Spawn{}) != at {
		t.Fatalf("enrolled %+v, standing at %+v", c.ReverseUserPoint, at)
	}
	entry := rt.reverseUserPointEntryFrames(c)
	if len(entry) != 1 || entry[0].NativeOpcode != OpReverseUserPoint {
		t.Fatalf("the entry frames are %+v", entry)
	}
}

/*
================
TestReverseUserEnrollRefusedInsideAnInstance
================
*/
func TestReverseUserEnrollRefusedInsideAnInstance(t *testing.T) {
	rt, c, _ := reverseReturnFixture(t)
	packed := uint32(instance.Pack(10, 1))
	c.World.PackedInstance = &packed
	out := useReverseScroll(rt, c, reverseReturnEnrollChoice)
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, wire.EncodeItemUseError(errCodeReverseUserSetBarred)) {
		t.Fatalf("the enroll in an instance answered %+v", out.Frames)
	}
	if c.ReverseUserPoint != nil || reverseScrollStack(c) != 2 {
		t.Fatal("the refused enroll recorded a point or spent a scroll")
	}
}

/*
================
TestReverseUserPointFrameReportsAbsence
================
*/
func TestReverseUserPointFrameReportsAbsence(t *testing.T) {
	rt, c, _ := reverseReturnFixture(t)
	frame := ReverseUserPointFrame(c)
	if frame.Opcode != OpReverseUserPoint || !bytes.Equal(frame.Payload, []byte{0}) {
		t.Fatalf("an unenrolled character's frame is %+v", frame)
	}
	if entry := rt.reverseUserPointEntryFrames(c); len(entry) != 0 {
		t.Fatalf("an unenrolled character's entry frames are %+v", entry)
	}
}

/*
================
TestReverseUserMoveTravelsToTheEnrolledPoint
================
*/
func TestReverseUserMoveTravelsToTheEnrolledPoint(t *testing.T) {
	rt, c, clock := reverseReturnFixture(t)
	useReverseScroll(rt, c, reverseReturnEnrollChoice)
	enrolled := missionSpawnFromWorld(&c.ReverseUserPoint.WorldSpawn, simulation.Spawn{})
	// Stand somewhere else, then move back on the user choice.
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) { w.Spawn.X, w.Spawn.Z = enrolled.X+50, enrolled.Z+50; w.SpawnSet = true })
	out := useReverseScroll(rt, c, reverseReturnUserChoice)
	if reverseScrollStack(c) != 1 || c.NativeTeleportMode != 1 {
		t.Fatalf("the user move did not spend one scroll and start the cast: %+v", out.Frames)
	}
	rt.PushCharacterFrames = func(string, string, []wire.Frame) {}
	clock.Advance(time.Second)
	rt.TickHook()(clock.NowMs())
	rt.processReturnArrivals()
	if got := missionSpawnFromWorld(c.World.Spawn, simulation.Spawn{}); got.RegionID != enrolled.RegionID || got.X != enrolled.X || got.Z != enrolled.Z {
		t.Fatalf("arrived at %+v, not the enrolled point %+v", got, enrolled)
	}
}

/*
================
TestReverseUserMoveWithoutAPointRefuses
================
*/
func TestReverseUserMoveWithoutAPointRefuses(t *testing.T) {
	rt, c, _ := reverseReturnFixture(t)
	out := useReverseScroll(rt, c, reverseReturnUserChoice)
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, wire.EncodeItemUseError(errCodeReverseUserMoveFail)) {
		t.Fatalf("the move with no point answered %+v", out.Frames)
	}
	if reverseScrollStack(c) != 2 || c.NativeTeleportMode != 0 {
		t.Fatal("the refused move spent the scroll or started a cast")
	}
}

/*
================
TestReversePartyMoveTravelsToTheMember
================
*/
func TestReversePartyMoveTravelsToTheMember(t *testing.T) {
	rt, c, clock := reverseReturnFixture(t)
	here := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, clock.NowMs())
	there := here
	there.X, there.Z = here.X+120, here.Z+80
	mate := reverseUserMate(t, rt, c, there)
	out := useReverseScroll(rt, c, reversePartyTail(enterworld.ObjectIDForCharacter(mate))...)
	if reverseScrollStack(c) != 1 || c.NativeTeleportMode != 1 {
		t.Fatalf("the party move did not spend one scroll and start the cast: %+v", out.Frames)
	}
	rt.PushCharacterFrames = func(string, string, []wire.Frame) {}
	clock.Advance(time.Second)
	rt.TickHook()(clock.NowMs())
	rt.processReturnArrivals()
	if got := missionSpawnFromWorld(c.World.Spawn, simulation.Spawn{}); got.RegionID != there.RegionID || got.X != there.X || got.Z != there.Z {
		t.Fatalf("arrived at %+v, not the member's position %+v", got, there)
	}
}

/*
================
TestReversePartyMoveRefusesOneWhoLeft
================
*/
func TestReversePartyMoveRefusesOneWhoLeft(t *testing.T) {
	rt, c, clock := reverseReturnFixture(t)
	here := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, clock.NowMs())
	mate := reverseUserMate(t, rt, c, here)
	// The member leaves: the party roster no longer lists them.
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c)}}}
	}
	out := useReverseScroll(rt, c, reversePartyTail(enterworld.ObjectIDForCharacter(mate))...)
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, wire.EncodeItemUseError(errCodeReversePartyLeft)) {
		t.Fatalf("the move to one who left answered %+v", out.Frames)
	}
	// A gid nobody holds reads the same way.
	out = useReverseScroll(rt, c, reversePartyTail(0x7fffffff)...)
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, wire.EncodeItemUseError(errCodeReversePartyLeft)) {
		t.Fatalf("the move to a stranger answered %+v", out.Frames)
	}
	if reverseScrollStack(c) != 2 || c.NativeTeleportMode != 0 {
		t.Fatal("the refused party move spent the scroll or started a cast")
	}
}

/*
================
TestReversePartyMoveRefusesAMemberInAnInstance
================
*/
func TestReversePartyMoveRefusesAMemberInAnInstance(t *testing.T) {
	rt, c, clock := reverseReturnFixture(t)
	here := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, clock.NowMs())
	mate := reverseUserMate(t, rt, c, here)
	packed := uint32(instance.Pack(10, 1))
	mate.World.PackedInstance = &packed
	out := useReverseScroll(rt, c, reversePartyTail(enterworld.ObjectIDForCharacter(mate))...)
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, wire.EncodeItemUseError(errCodeReversePartyUnreachable)) {
		t.Fatalf("the move to a member in an instance answered %+v", out.Frames)
	}
	if reverseScrollStack(c) != 2 || c.NativeTeleportMode != 0 {
		t.Fatal("the refused party move spent the scroll or started a cast")
	}
}
