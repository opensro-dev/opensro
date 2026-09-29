/*
===========================================================================

direction_test.go - the pure direction-walk transitions

The leg goal's geometry, the angular-form split in ApplyMove, and the stop
and standing-turn transitions, without the movement runtime.

===========================================================================
*/
package simulation

import (
	"math"
	"testing"
)

/*
================
TestDirectionLegGoalFollowsTheWireBearing

Heading h walks (cos t, sin t), t = h / 65535 * 2pi: word 0 is +x, a
quarter circle is +z. An outdoor goal folds into its region; a dungeon goal
keeps its single region word.
================
*/
func TestDirectionLegGoalFollowsTheWireBearing(t *testing.T) {
	from := Spawn{RegionID: 0x6B4F, X: 100, Y: 12, Z: 200, Angle: 7}

	east := DirectionLegGoal(from, 0, 50)
	if east.RegionID != from.RegionID || east.X != 150 || east.Z != 200 || east.Y != 12 || east.Angle != 0 {
		t.Errorf("east leg = %+v", east)
	}
	quarter := uint16(math.Round(HeadingWordScale / 4))
	south := DirectionLegGoal(from, quarter, 50)
	if math.Abs(south.X-100) > 1e-2 || math.Abs(south.Z-250) > 1e-2 || south.Angle != quarter {
		t.Errorf("south leg = %+v", south)
	}

	folded := DirectionLegGoal(from, 0x8000, 150)
	west := RegionIDForSectors(SectorX(from.RegionID)-1, SectorY(from.RegionID))
	if folded.RegionID != west || math.Abs(folded.X-(NativeRegionSize-50)) > 1e-3 {
		t.Errorf("west leg across the border = %+v, want region 0x%04X x %v", folded, west, NativeRegionSize-50)
	}

	dungeon := Spawn{RegionID: 0x8000 | 0x0001, X: 100, Z: 200}
	deep := DirectionLegGoal(dungeon, 0x8000, 150)
	if deep.RegionID != dungeon.RegionID || math.Abs(deep.X+50) > 1e-3 {
		t.Errorf("dungeon leg = %+v, want the same region at x -50", deep)
	}
}

/*
================
TestApplyMoveSplitsTheAngularForm

With AngularFlagGo the angular form walks a full leg with the live source
block; without it the mover turns in place.
================
*/
func TestApplyMoveSplitsTheAngularForm(t *testing.T) {
	start := EuropeStartProfile()

	walking := DefaultWorldState(start)
	walking.MovementSourceSeeded = true
	result := ApplyMove(&walking, 1, MovementRequest{Mode: MovementAckAngularMode, AngularMode: AngularFlagGo, HeadingWord: 0x8000}, RunMode, 1000)
	if result.Segment == nil || !result.SourceIncluded {
		t.Fatalf("GO form = %+v, want a segment and the source block", result)
	}
	if got := WorldDistance2D(start, walking.Spawn); math.Abs(got-DirectionLegUnits) > 1e-3 {
		t.Errorf("GO leg length = %v, want %v", got, DirectionLegUnits)
	}

	turning := DefaultWorldState(start)
	turning.MovementSourceSeeded = true
	result = ApplyMove(&turning, 1, MovementRequest{Mode: MovementAckAngularMode, AngularMode: AngularFlagSteerLeft, HeadingWord: 0x8000}, RunMode, 1000)
	if result.Segment != nil || turning.Spawn.X != start.X || turning.Spawn.Angle != 0x8000 {
		t.Errorf("no-GO form = %+v / %+v, want a turn in place", result, turning.Spawn)
	}
}

/*
================
TestDirectionStopAndStandingTurn

A stop settles at the live point with the stop heading; a standing turn
refuses while a segment is in flight and turns once it is not.
================
*/
func TestDirectionStopAndStandingTurn(t *testing.T) {
	start := EuropeStartProfile()
	world := DefaultWorldState(start)
	goal := DirectionLegGoal(start, 0, DirectionLegUnits)
	ApplyDirectionLeg(&world, 1, MovementRequest{Mode: MovementAckAngularMode, AngularMode: AngularFlagGo}, goal, 0)

	inFlight := world
	if ApplyStandingTurn(&inFlight, 0x1000, 1000) || inFlight.Spawn != world.Spawn {
		t.Fatal("a standing turn must not bend a walk in flight")
	}

	stopped := world
	rest := ApplyDirectionStop(&stopped, 0x2000, 1000)
	if stopped.MoveSegment != nil || rest != stopped.Spawn || math.Abs(rest.X-(start.X+RunSpeed)) > 1e-6 || rest.Angle != 0x2000 {
		t.Fatalf("stop = %+v", rest)
	}
	if !ApplyStandingTurn(&stopped, 0x3000, 2000) || stopped.Spawn.Angle != 0x3000 || stopped.Spawn.X != rest.X {
		t.Errorf("standing turn = %+v", stopped.Spawn)
	}
}

/*
================
TestDecodeClientHeadingRequestIsExact
================
*/
func TestDecodeClientHeadingRequestIsExact(t *testing.T) {
	if heading, err := DecodeClientHeadingRequest([]byte{0x34, 0x12}); err != nil || heading != 0x1234 {
		t.Fatalf("heading = 0x%04X, %v", heading, err)
	}
	for _, payload := range [][]byte{nil, {1}, {1, 2, 3}} {
		if _, err := DecodeClientHeadingRequest(payload); err == nil {
			t.Errorf("% X accepted", payload)
		}
	}
}
