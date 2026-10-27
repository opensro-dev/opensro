/*
===========================================================================

thief_crossing_test.go - a thief's transport may not cross a protected region

#721 follow-up: the reference server tests the transport's region on every
movement step, so the stolen-goods rule must refuse a walk that only
passes through a protected sector, not only one that ends there. A
destination click through the town is refused with the native notice; a
direction walk stops at the town's border.

===========================================================================
*/
package movement

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
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
