/*
===========================================================================

thief_crossing_test.go - a thief's transport may not cross a protected region

#721 follow-up: the reference server tests the transport's region on every
movement step, so the stolen-goods rule must refuse a walk that only
passes through a protected sector, not only one that ends there. A
destination click through the town is refused with the native notice; a
direction walk stops at the town's border and shows the same notice
(#1985). The port-only town walls (SRO_THIEF_CART_TOWN_AREAS) stop both
inside a region, where the region rule sees no change.

===========================================================================
*/
package movement

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

// The start region is 0x6B4F; the protected sector east of it is 0x6B50
// and the field beyond is 0x6B51.
const (
	crossingTownRegion   uint16 = 0x6B50
	crossingBeyondRegion uint16 = 0x6B51
)

/*
================
townBetweenRule

A TownEntryBlocked stub that refuses only the entry into the town sector,
the shape of the real rule (battlefield to protected).
================
*/
func townBetweenRule(_ *enterworld.Character, _, toRegion uint16) bool {
	return toRegion == crossingTownRegion
}

/*
================
TestHandleMoveRefusesAThiefCargoWalkThroughATown

Both ends of the chord are field regions; the town lies between them.
Testing only the ends would admit the move.
================
*/
func TestHandleMoveRefusesAThiefCargoWalkThroughATown(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	rt.TownEntryBlocked = townBetweenRule
	start := simulation.EuropeStartProfile()

	refused := rt.HandleMove("0", character, encodeMoveBody(1, crossingBeyondRegion, 100, int16(start.Y), int16(start.Z)))
	if refused.Refusal == nil || refused.Refusal.Reason != "thiefCargoInTown" {
		t.Fatalf("outcome = %+v, want thiefCargoInTown for a walk through the town", refused)
	}
	if world := directionWorld(rt, character); world.Spawn.RegionID != start.RegionID {
		t.Fatalf("a refused move still moved the character to 0x%04X", world.Spawn.RegionID)
	}

	rt.TownEntryBlocked = nil
	if accepted := rt.HandleMove("0", character, encodeMoveBody(1, crossingBeyondRegion, 100, int16(start.Y), int16(start.Z))); accepted.Refusal != nil {
		t.Fatalf("without the rule the same move was refused: %+v", accepted.Refusal)
	}
}

/*
================
TestDirectionWalkStopsAtATownWithThiefCargo

A thief walking east by direction stops at the town's border, blocked,
instead of walking into it.
================
*/
func TestDirectionWalkStopsAtATownWithThiefCargo(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	rt.TownEntryBlocked = townBetweenRule
	start := simulation.EuropeStartProfile()

	if outcome := rt.HandleMove("0", character, encodeTurnBody(simulation.AngularFlagGo, headingEast)); outcome.Refusal != nil {
		t.Fatalf("direction walk refused: %v", outcome.Refusal)
	}
	world := directionWorld(rt, character)
	if world.Spawn.RegionID != start.RegionID || world.Spawn.X < simulation.NativeRegionSize-1 || world.Spawn.X >= simulation.NativeRegionSize {
		t.Fatalf("goal = %+v, want the east border of 0x%04X", world.Spawn, start.RegionID)
	}
	walk, _ := rt.directions.get(simulation.WorldKey("0", character.Name))
	if !walk.blocked {
		t.Error("a walk stopped at a town with thief cargo is blocked")
	}
}

/*
================
walkUntilStopped

Runs the direction tick until the walk ends and returns the frames the
stop sent the walker.
================
*/
func walkUntilStopped(t *testing.T, rt *Runtime, character *enterworld.Character) []simulation.Frame {
	t.Helper()
	hook := rt.DirectionTickHook()
	key := simulation.WorldKey("0", character.Name)
	var frames []simulation.Frame
	for now := int64(testStartMs); now < testStartMs+120000; now += 100 {
		for _, out := range hook(now) {
			frames = append(frames, out.Frames...)
		}
		if _, ok := rt.directions.get(key); !ok {
			return frames
		}
	}
	t.Fatal("the direction walk never stopped")
	return nil
}

/*
================
framesOf

A move outcome's wire frames in the tick's frame shape.
================
*/
func framesOf(move MoveOutcome) []simulation.Frame {
	frames := make([]simulation.Frame, 0, len(move.Frames))
	for _, frame := range move.Frames {
		frames = append(frames, simulation.Frame{Opcode: frame.Opcode, Payload: frame.Payload})
	}
	return frames
}

/*
================
hasThiefCargoNotice
================
*/
func hasThiefCargoNotice(frames []simulation.Frame) bool {
	for _, frame := range frames {
		if frame.Opcode == opEventNotice && bytes.Equal(frame.Payload, []byte{eventNoticeThiefCargoTown}) {
			return true
		}
	}
	return false
}

/*
================
TestDirectionWalkStopShowsTheThiefCargoNotice

#1985: native refuses the transport's step into the town with the 0x3058
kind 1 notice whatever moved it, so a direction walk's stop at the border
shows it next to the 0xB2F5 correction. A walk stopped by anything else
(a closed area) shows no notice.
================
*/
func TestDirectionWalkStopShowsTheThiefCargoNotice(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	rt.TownEntryBlocked = townBetweenRule
	if outcome := rt.HandleMove("0", character, encodeTurnBody(simulation.AngularFlagGo, headingEast)); outcome.Refusal != nil {
		t.Fatalf("direction walk refused: %v", outcome.Refusal)
	}
	frames := walkUntilStopped(t, rt, character)
	if len(frames) == 0 || frames[0].Opcode != directionCorrectionFrame(0, simulation.Spawn{}).Opcode {
		t.Fatalf("stop frames = %+v, want the correction first", frames)
	}
	if !hasThiefCargoNotice(frames) {
		t.Fatalf("stop frames = %+v, want the 0x3058 kind 1 notice", frames)
	}

	closed := testCharacter()
	rt = testRuntime(closed)
	rt.CanEnterRegion = func(_ *enterworld.Character, regionID uint16) bool { return regionID != crossingTownRegion }
	if outcome := rt.HandleMove("0", closed, encodeTurnBody(simulation.AngularFlagGo, headingEast)); outcome.Refusal != nil {
		t.Fatalf("direction walk refused: %v", outcome.Refusal)
	}
	if frames := walkUntilStopped(t, rt, closed); hasThiefCargoNotice(frames) {
		t.Fatal("a walk stopped by a closed area showed the thief notice")
	}
}

// wallOffset is how far east of the start the stub's wall stands.
const wallOffset = 200

/*
================
wallInsideStartRegion

A TownAreaEntryBlocked stub: a town wall wallOffset units east of the
start, inside the start region, so no region changes on the way to it.
================
*/
func wallInsideStartRegion(start simulation.Spawn) func(*enterworld.Character, worldgeom.RegionXZ, worldgeom.RegionXZ) bool {
	origin := worldgeom.RegionXZ{RegionID: start.RegionID, X: start.X, Z: start.Z}
	return func(_ *enterworld.Character, from, to worldgeom.RegionXZ) bool {
		fromX, _ := worldgeom.Delta(origin, from)
		toX, _ := worldgeom.Delta(origin, to)
		return fromX < wallOffset && toX >= wallOffset
	}
}

/*
================
TestTownWallStopsAThiefCartInsideARegion

#1985, port-only: with the town walls on, a destination click past a
wall inside the region is refused with the notice and a direction walk
stops short of it with the notice. With them off (nil, native) the same
moves go through: the region rule sees no region change (it is left
unwired here, so only the walls can stop the walk).
================
*/
func TestTownWallStopsAThiefCartInsideARegion(t *testing.T) {
	start := simulation.EuropeStartProfile()
	if start.X+400 >= simulation.NativeRegionSize {
		t.Fatalf("start x %v leaves no room for the wall in its region", start.X)
	}
	for _, on := range []bool{true, false} {
		character := testCharacter()
		rt := testRuntime(character)
		if on {
			rt.TownAreaEntryBlocked = wallInsideStartRegion(start)
		}
		move := rt.HandleMove("0", character, encodeMoveBody(1, start.RegionID, int16(start.X+300), int16(start.Y), int16(start.Z)))
		if refused := move.Refusal != nil && move.Refusal.Reason == "thiefCargoInTown"; refused != on {
			t.Fatalf("walls %v: destination outcome = %+v", on, move)
		}
		if on && !hasThiefCargoNotice(framesOf(move)) {
			t.Fatalf("walls on: destination refusal frames = %+v, want the notice", move.Frames)
		}

		character = testCharacter()
		rt = testRuntime(character)
		if on {
			rt.TownAreaEntryBlocked = wallInsideStartRegion(start)
		}
		if outcome := rt.HandleMove("0", character, encodeTurnBody(simulation.AngularFlagGo, headingEast)); outcome.Refusal != nil {
			t.Fatalf("direction walk refused: %v", outcome.Refusal)
		}
		world := directionWorld(rt, character)
		short := world.Spawn.RegionID == start.RegionID && world.Spawn.X < start.X+wallOffset && world.Spawn.X > start.X+wallOffset-10
		if short != on {
			t.Fatalf("walls %v: direction goal = %+v (wall at x %v)", on, world.Spawn, start.X+wallOffset)
		}
		if on && !hasThiefCargoNotice(walkUntilStopped(t, rt, character)) {
			t.Fatal("walls on: the stop at the wall showed no notice")
		}
	}
}
