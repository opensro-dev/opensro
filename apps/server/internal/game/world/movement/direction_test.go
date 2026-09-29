/*
===========================================================================

direction_test.go - direction walks: legs, blocking, continuation, steer, stop

BUG-033: a click that misses the ground sends 0x7738 mode 0 with GO, and
the character must walk that way until something blocks it. These tests
drive the walk through the public handlers and the tick hook and assert on
the world plane and the frames.

===========================================================================
*/
package movement

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// directionLegMs is one full leg at the default run speed.
const directionLegMs = int64(simulation.DirectionLegUnits / simulation.RunSpeed * 1000)

// Heading words of the four axes (wire bearing atan2(dz, dx)).
const (
	headingEast  uint16 = 0
	headingSouth uint16 = 0x4000
	headingWest  uint16 = 0x8000
)

/*
================
setDirectionClock
================
*/
func setDirectionClock(rt *Runtime, nowMs int64) {
	rt.Now = func() time.Time { return time.UnixMilli(nowMs) }
}

/*
================
directionWorld
================
*/
func directionWorld(rt *Runtime, character *enterworld.Character) simulation.WorldState {
	return rt.Worlds.Snapshot(simulation.WorldKey("0", character.Name), func() simulation.WorldState { return simulation.SeedWorldState(character) })
}

/*
================
headingBody

The [u16 heading] body of 0x72CF and 0x72F5.
================
*/
func headingBody(heading uint16) []byte {
	out := make([]byte, 2)
	binary.LittleEndian.PutUint16(out, heading)
	return out
}

/*
================
TestDirectionWalkStartsLegAlongHeading

The GO form walks: the goal lies a full leg along the heading (folded into
the next region east), the segment runs at the wire speed, and the ack
echoes the mode-0 body with the live departure as its source block - on
every direction ack, not only the first.
================
*/
func TestDirectionWalkStartsLegAlongHeading(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	start := simulation.EuropeStartProfile()

	outcome := rt.HandleMove("0", character, encodeTurnBody(simulation.AngularFlagGo, headingEast))
	if outcome.Refusal != nil {
		t.Fatalf("direction walk refused: %v", outcome.Refusal)
	}
	world := directionWorld(rt, character)
	east := simulation.RegionIDForSectors(simulation.SectorX(start.RegionID)+1, simulation.SectorY(start.RegionID))
	wantX := start.X + simulation.DirectionLegUnits - simulation.NativeRegionSize
	if world.Spawn.RegionID != east || math.Abs(world.Spawn.X-wantX) > 1e-6 || math.Abs(world.Spawn.Z-start.Z) > 1e-6 {
		t.Fatalf("leg goal = %+v, want region 0x%04X x %v z %v", world.Spawn, east, wantX, start.Z)
	}
	if world.Spawn.Angle != headingEast {
		t.Errorf("goal angle = 0x%04X, want the heading", world.Spawn.Angle)
	}
	if world.MoveSegment == nil || world.MoveSegment.ArrivesAtMs-world.MoveSegment.StartedAtMs != directionLegMs {
		t.Fatalf("segment = %+v, want a %d ms leg", world.MoveSegment, directionLegMs)
	}

	gid := enterworld.ObjectIDForCharacter(character)
	source := simulation.MovementSourceFromSpawn(start)
	request := simulation.MovementRequest{Mode: simulation.MovementAckAngularMode, AngularMode: simulation.AngularFlagGo, HeadingWord: headingEast}
	if want := simulation.BuildMovementAckPayload(gid, request, &source); !bytes.Equal(outcome.Frames[0].Payload, want) {
		t.Errorf("ack\n got % X\nwant % X", outcome.Frames[0].Payload, want)
	}

	// A second walk (the source latch long consumed) still carries the
	// source block: the client starts the walk only inside that arm.
	setDirectionClock(rt, testStartMs+1000)
	second := rt.HandleMove("0", character, encodeTurnBody(simulation.AngularFlagGo, headingSouth))
	if second.Refusal != nil || !second.Result.SourceIncluded {
		t.Fatalf("second direction ack must carry the source block: %+v", second)
	}
	live := simulation.MovementSourceFromSpawn(second.Result.LiveBefore)
	request.HeadingWord = headingSouth
	if want := simulation.BuildMovementAckPayload(gid, request, &live); !bytes.Equal(second.Frames[0].Payload, want) {
		t.Errorf("second ack\n got % X\nwant % X", second.Frames[0].Payload, want)
	}
	if math.Abs(second.Result.LiveBefore.X-(start.X+simulation.RunSpeed)) > 1e-6 {
		t.Errorf("second leg departs from %+v, want the live point one second east", second.Result.LiveBefore)
	}
}

/*
================
TestDirectionWalkClipStopsAtWall

Real geometry: walking east from (30, 110) runs into the synthetic
obstacle whose first tile starts at x = 80. The leg ends just short of it,
and when it matures the mover alone gets the 0xB2F5 correction there.
================
*/
func TestDirectionWalkClipStopsAtWall(t *testing.T) {
	character := clipTestCharacter()
	rt := testRuntime(character)
	validator := NewWaterValidator(syntheticHeightRoot(t))
	rt.ClientClip = &ClientClip{Mode: ClipApply, Validator: validator}
	rt.PathGuard = &PathGuard{Mode: PathGuardEnforce, Validator: validator}

	outcome := rt.HandleMove("0", character, encodeTurnBody(simulation.AngularFlagGo, headingEast))
	if outcome.Refusal != nil {
		t.Fatalf("direction walk refused: %v", outcome.Refusal)
	}
	world := directionWorld(rt, character)
	if world.Spawn.X >= 80 || world.Spawn.X < 79 || world.Spawn.Z != 110 {
		t.Fatalf("clipped goal = %+v, want just short of x = 80", world.Spawn)
	}

	hook := rt.DirectionTickHook()
	if frames := hook(world.MoveSegment.ArrivesAtMs - 1); len(frames) != 0 {
		t.Fatalf("blocked leg in flight produced %+v", frames)
	}
	frames := hook(world.MoveSegment.ArrivesAtMs)
	if len(frames) != 1 || frames[0].OnlyCharacterID != character.ID || len(frames[0].Frames) != 1 {
		t.Fatalf("matured blocked leg frames = %+v, want one correction for the mover", frames)
	}
	want := directionCorrectionFrame(enterworld.ObjectIDForCharacter(character), world.Spawn)
	if got := frames[0].Frames[0]; got.Opcode != wire.OpObjectSourceCorrection || !bytes.Equal(got.Payload, want.Payload) {
		t.Errorf("correction = %04X % X, want %04X % X", got.Opcode, got.Payload, want.Opcode, want.Payload)
	}
	if frames := hook(world.MoveSegment.ArrivesAtMs + 1000); len(frames) != 0 {
		t.Errorf("an ended walk must stay quiet, got %+v", frames)
	}
	if after := directionWorld(rt, character); after.Spawn != world.Spawn {
		t.Errorf("an ended walk moved: %+v -> %+v", world.Spawn, after.Spawn)
	}
}

/*
================
TestDirectionWalkBlockedWhereItStands

A wall right in front: the clip returns the departure itself. The mover
turns to the heading, no segment starts, and the next tick corrects it.
================
*/
func TestDirectionWalkBlockedWhereItStands(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	start := simulation.EuropeStartProfile()
	rt.ClientClip = &ClientClip{Mode: ClipApply, Validator: &fakeClipValidator{report: ClipReport{Outcome: ClipBlocked, Rest: start}}}

	if outcome := rt.HandleMove("0", character, encodeTurnBody(simulation.AngularFlagGo, headingWest)); outcome.Refusal != nil {
		t.Fatalf("direction walk refused: %v", outcome.Refusal)
	}
	world := directionWorld(rt, character)
	if world.MoveSegment != nil || world.Spawn.X != start.X || world.Spawn.Angle != headingWest {
		t.Fatalf("world = %+v, want standing at the start facing west", world)
	}
	frames := rt.DirectionTickHook()(testStartMs + simulation.DefaultTickInterval.Milliseconds())
	if len(frames) != 1 || frames[0].OnlyCharacterID != character.ID {
		t.Fatalf("frames = %+v, want the mover's correction", frames)
	}
}

/*
================
TestDirectionWalkContinuesBeforeLegMatures

An open leg is followed by the next one from the live point inside the
lookahead window, so the walk never settles between legs.
================
*/
func TestDirectionWalkContinuesBeforeLegMatures(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	start := simulation.EuropeStartProfile()

	if outcome := rt.HandleMove("0", character, encodeTurnBody(simulation.AngularFlagGo, headingSouth)); outcome.Refusal != nil {
		t.Fatalf("direction walk refused: %v", outcome.Refusal)
	}
	first := directionWorld(rt, character)
	hook := rt.DirectionTickHook()

	early := first.MoveSegment.ArrivesAtMs - simulation.DirectionLegLookaheadMs - 1
	if frames := hook(early); len(frames) != 0 {
		t.Fatalf("continuation sends the mover nothing, got %+v", frames)
	}
	if directionWorld(rt, character).Spawn != first.Spawn {
		t.Fatal("the next leg must wait for the lookahead window")
	}

	inside := first.MoveSegment.ArrivesAtMs - simulation.DirectionLegLookaheadMs
	if frames := hook(inside); len(frames) != 0 {
		t.Fatalf("continuation sends the mover nothing, got %+v", frames)
	}
	next := directionWorld(rt, character)
	if next.MoveSegment == nil || next.MoveSegment.StartedAtMs != inside {
		t.Fatalf("next leg = %+v, want one starting at %d", next.MoveSegment, inside)
	}
	live := first.LiveSpawnAt(inside)
	if next.MoveSegment.From != live {
		t.Errorf("next leg departs %+v, want the live point %+v", next.MoveSegment.From, live)
	}
	travelled := simulation.WorldDistance2D(simulation.Spawn{RegionID: start.RegionID, X: start.X, Z: start.Z}, next.Spawn)
	want := simulation.RunSpeed*float64(inside-testStartMs)/1000 + simulation.DirectionLegUnits
	if math.Abs(travelled-want) > 1e-3 {
		t.Errorf("walked goal is %v from the start, want %v", travelled, want)
	}
	if next.Spawn.Angle != headingSouth {
		t.Errorf("continuation angle = 0x%04X, want the heading", next.Spawn.Angle)
	}
}

/*
================
TestDirectionWalkEndsWhenSuperseded

A destination click replaces the walk; the tick must not resume it.
================
*/
func TestDirectionWalkEndsWhenSuperseded(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	start := simulation.EuropeStartProfile()

	rt.HandleMove("0", character, encodeTurnBody(simulation.AngularFlagGo, headingEast))
	setDirectionClock(rt, testStartMs+1000)
	click := rt.HandleMove("0", character, encodeMoveBody(1, start.RegionID, int16(start.X), int16(start.Y), int16(start.Z)+100))
	if click.Refusal != nil {
		t.Fatalf("click refused: %v", click.Refusal)
	}
	clicked := directionWorld(rt, character)
	rt.DirectionTickHook()(clicked.MoveSegment.ArrivesAtMs)
	if after := directionWorld(rt, character); after.Spawn != clicked.Spawn {
		t.Errorf("the tick resumed a superseded walk: %+v", after.Spawn)
	}
	if _, walking := rt.directions.get(simulation.WorldKey("0", character.Name)); walking {
		t.Error("the click must end the walk")
	}
}

/*
================
TestDirectionWalkStopsAtClosedArea

A walk toward a region the character may not enter stops at its border
instead of being refused.
================
*/
func TestDirectionWalkStopsAtClosedArea(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	start := simulation.EuropeStartProfile()
	rt.CanEnterRegion = func(_ *enterworld.Character, regionID uint16) bool { return regionID == start.RegionID }

	if outcome := rt.HandleMove("0", character, encodeTurnBody(simulation.AngularFlagGo, headingEast)); outcome.Refusal != nil {
		t.Fatalf("direction walk refused: %v", outcome.Refusal)
	}
	world := directionWorld(rt, character)
	if world.Spawn.RegionID != start.RegionID || world.Spawn.X < simulation.NativeRegionSize-1 || world.Spawn.X >= simulation.NativeRegionSize {
		t.Fatalf("goal = %+v, want the east border of 0x%04X", world.Spawn, start.RegionID)
	}
	walk, _ := rt.directions.get(simulation.WorldKey("0", character.Name))
	if !walk.blocked {
		t.Error("a walk stopped at a closed area is blocked")
	}
}

/*
================
TestSteerReplansFromLivePoint

0x72CF during a walk turns it where the walker is, and observers get
0xB2CF; the mover's own client already turned.
================
*/
func TestSteerReplansFromLivePoint(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	start := simulation.EuropeStartProfile()

	rt.HandleMove("0", character, encodeTurnBody(simulation.AngularFlagGo, headingEast))
	setDirectionClock(rt, testStartMs+1000)
	outcome := rt.HandleSteer("0", character, headingBody(headingSouth))
	if outcome.Refusal != "" {
		t.Fatalf("steer refused: %s", outcome.Refusal)
	}
	if len(outcome.Frames) != 0 || len(outcome.Broadcast) != 1 {
		t.Fatalf("steer frames = %+v / %+v, want only the observers' 0xB2CF", outcome.Frames, outcome.Broadcast)
	}
	gid := enterworld.ObjectIDForCharacter(character)
	want := []byte{byte(gid), byte(gid >> 8), byte(gid >> 16), byte(gid >> 24), 0x00, 0x40}
	if got := outcome.Broadcast[0]; got.Opcode != simulation.OpObjectSteer || !bytes.Equal(got.Payload, want) {
		t.Errorf("0xB2CF = %04X % X, want % X", got.Opcode, got.Payload, want)
	}

	world := directionWorld(rt, character)
	wantFrom := start.X + simulation.RunSpeed
	if world.MoveSegment == nil || math.Abs(world.MoveSegment.From.X-wantFrom) > 1e-6 {
		t.Fatalf("steered leg = %+v, want one from x %v", world.MoveSegment, wantFrom)
	}
	// Word 0x4000 is a hair past a quarter circle (the 65535 scale), so
	// the leg drifts a fraction of a unit west over 1000 units.
	if math.Abs(world.Spawn.X-wantFrom) > 0.1 || math.Abs(world.Spawn.Z-(start.Z+simulation.DirectionLegUnits)) > 1e-3 {
		t.Errorf("steered goal = %+v, want a full leg south of the live point", world.Spawn)
	}
	if world.Spawn.Angle != headingSouth {
		t.Errorf("steered angle = 0x%04X", world.Spawn.Angle)
	}
}

/*
================
TestSteerTurnsStandingMover

0x72CF with no walk turns the mover where it stands.
================
*/
func TestSteerTurnsStandingMover(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	start := simulation.EuropeStartProfile()

	outcome := rt.HandleSteer("0", character, headingBody(0x1234))
	if outcome.Refusal != "" || len(outcome.Broadcast) != 1 {
		t.Fatalf("standing steer = %+v", outcome)
	}
	world := directionWorld(rt, character)
	if world.Spawn.Angle != 0x1234 || world.Spawn.X != start.X || world.MoveSegment != nil {
		t.Errorf("world = %+v, want the start facing 0x1234", world.Spawn)
	}
}

/*
================
TestSteerLeavesDestinationWalk

A steer that races a destination click must not bend the click's walk.
================
*/
func TestSteerLeavesDestinationWalk(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	start := simulation.EuropeStartProfile()

	rt.HandleMove("0", character, encodeMoveBody(1, start.RegionID, int16(start.X)+200, int16(start.Y), int16(start.Z)))
	before := directionWorld(rt, character)
	outcome := rt.HandleSteer("0", character, headingBody(headingSouth))
	if outcome.Refusal == "" || len(outcome.Broadcast) != 0 {
		t.Fatalf("steer during a destination walk = %+v, want a silent refusal", outcome)
	}
	if after := directionWorld(rt, character); after.Spawn != before.Spawn {
		t.Errorf("destination walk bent: %+v -> %+v", before.Spawn, after.Spawn)
	}
}

/*
================
TestDirectionStopSettlesAndCorrects

0x72F5 settles the walker at its live point facing the stop heading and
sends the same 0xB2F5 to the mover and its observers.
================
*/
func TestDirectionStopSettlesAndCorrects(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	start := simulation.EuropeStartProfile()

	rt.HandleMove("0", character, encodeTurnBody(simulation.AngularFlagGo, headingEast))
	setDirectionClock(rt, testStartMs+1000)
	outcome := rt.HandleDirectionStop("0", character, headingBody(0x0101))
	if outcome.Refusal != "" {
		t.Fatalf("stop refused: %s", outcome.Refusal)
	}
	world := directionWorld(rt, character)
	if world.MoveSegment != nil || math.Abs(world.Spawn.X-(start.X+simulation.RunSpeed)) > 1e-6 || world.Spawn.Angle != 0x0101 {
		t.Fatalf("stopped world = %+v, want settled one second east facing 0x0101", world.Spawn)
	}
	want := directionCorrectionFrame(enterworld.ObjectIDForCharacter(character), world.Spawn)
	if len(outcome.Frames) != 1 || len(outcome.Broadcast) != 1 ||
		!bytes.Equal(outcome.Frames[0].Payload, want.Payload) || !bytes.Equal(outcome.Broadcast[0].Payload, want.Payload) {
		t.Fatalf("stop frames = %+v / %+v, want the correction to both", outcome.Frames, outcome.Broadcast)
	}
	if again := rt.HandleDirectionStop("0", character, headingBody(0x0101)); again.Refusal == "" {
		t.Error("a second stop has no walk to cancel")
	}
}

/*
================
TestDirectionCommandsRefuseMalformedAndDead

The heading pair decodes exactly two bytes, and shares the movement
admission: a dead character cannot steer.
================
*/
func TestDirectionCommandsRefuseMalformedAndDead(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)

	for _, payload := range [][]byte{nil, {1}, {1, 2, 3}} {
		if outcome := rt.HandleSteer("0", character, payload); outcome.Refusal == "" {
			t.Errorf("steer accepted % X", payload)
		}
		if outcome := rt.HandleDirectionStop("0", character, payload); outcome.Refusal == "" {
			t.Errorf("stop accepted % X", payload)
		}
	}

	hp := int64(0)
	character.CurrentHP = &hp
	if outcome := rt.HandleSteer("0", character, headingBody(1)); outcome.Refusal != "characterDead" {
		t.Errorf("dead steer refusal = %q, want characterDead", outcome.Refusal)
	}
	if frames, broadcast := rt.HandleCOSSteer("0", character, 0, 1); frames != nil || broadcast != nil {
		t.Error("a mount steer without a mount gid must be dropped")
	}
}
