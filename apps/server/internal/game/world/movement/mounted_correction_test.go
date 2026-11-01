/*
===========================================================================

mounted_correction_test.go - a mounted walk's 0xB2F5 names the mount

Native ignores a correction naming a mounted character (client 775B50 returns
early for a flagged rider), so every stop the port issues for a mounted
mover must name the COS. These tests pin that at both stop producers.

===========================================================================
*/
package movement

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// mountedTestCharacter is a test character riding a live summoned mount.
func mountedTestCharacter() *enterworld.Character {
	character := clipTestCharacter()
	character.ActiveCOS = &enterworld.CharacterCOS{GID: 123, Mounted: true, Summoned: true, CurrentHP: 100}
	return character
}

/*
================
TestMountedGroundWalkStoppedNamesMount

A mounted ground move that ends against an obstacle stops with the
0xB2F5: it names the COS gid, not the rider's, and routes by the COS too.
================
*/
func TestMountedGroundWalkStoppedNamesMount(t *testing.T) {
	character := mountedTestCharacter()
	rt := testRuntime(character)
	validator := NewWaterValidator(syntheticHeightRoot(t))
	rt.PathGuard = &PathGuard{Mode: PathGuardEnforce, Validator: validator}
	rt.ClientClip = &ClientClip{Mode: ClipApply, Validator: validator}
	rt.Nav = validator
	rt.EnableGroundWalk()

	outcome := rt.HandleCOSMove("0", character, 123, encodeMoveBody(1, 0x6B4F, 190, 0, 110))
	if len(outcome) != 1 || outcome[0].Opcode != simulation.OpMovementAck {
		t.Fatalf("mounted ground move frames = %+v, want one 0xB738 ack", outcome)
	}
	var correction simulation.DivisionFrames
	for tick := int64(1); tick <= 100; tick++ {
		setDirectionClock(rt, testStartMs+tick*100)
		state := directionWorld(rt, character)
		frames := rt.GroundTickHook()(testStartMs + tick*100)
		if len(frames) > 0 {
			correction = frames[0]
			break
		}
		if !state.GroundActive() {
			t.Fatal("mounted ground walk ended without a terminal notification")
		}
	}
	if len(correction.Frames) != 1 {
		t.Fatalf("mounted terminal frames = %+v, want one correction", correction)
	}
	if correction.SourceGID != 123 {
		t.Errorf("correction SourceGID = %d, want the mount gid 123", correction.SourceGID)
	}
	want := directionCorrectionFrame(123, directionWorld(rt, character).PersistedSpawn())
	got := correction.Frames[0]
	if got.Opcode != wire.OpObjectSourceCorrection || !bytes.Equal(got.Payload, want.Payload) {
		t.Errorf("mounted correction = %04X % X, want named-mount %04X % X", got.Opcode, got.Payload, want.Opcode, want.Payload)
	}
}

/*
================
TestMountedSteerStopNamesMount

A 0x769E tag-0x03 stop of a mounted direction walk settles the pair:
its 0xB2F5, to the mover and to observers, names the mount as well.
================
*/
func TestMountedSteerStopNamesMount(t *testing.T) {
	character := mountedTestCharacter()
	rt := testRuntime(character)

	outcome := rt.HandleCOSMove("0", character, 123, encodeTurnBody(simulation.AngularFlagGo, headingEast))
	if len(outcome) != 1 || outcome[0].Opcode != simulation.OpMovementAck {
		t.Fatalf("mounted direction walk frames = %+v, want one 0xB738 ack", outcome)
	}
	setDirectionClock(rt, testStartMs+1000)
	frames, broadcast := rt.HandleCOSStop("0", character, 123, 0x0101)
	if len(frames) != 1 || len(broadcast) != 1 {
		t.Fatalf("mounted stop frames = %+v / %+v, want one correction to mover and observers", frames, broadcast)
	}
	want := directionCorrectionFrame(123, directionWorld(rt, character).Spawn)
	for _, got := range [][]wire.Frame{frames, broadcast} {
		if got[0].Opcode != wire.OpObjectSourceCorrection || !bytes.Equal(got[0].Payload, want.Payload) {
			t.Errorf("mounted stop correction = %04X % X, want named-mount %04X % X", got[0].Opcode, got[0].Payload, want.Opcode, want.Payload)
		}
	}
}

/*
================
TestMountedDirectionWalkBlockedNamesMount

A mounted direction walk clipped at a wall matures into the same stop:
its 0xB2F5 names the mount.
================
*/
func TestMountedDirectionWalkBlockedNamesMount(t *testing.T) {
	character := mountedTestCharacter()
	rt := testRuntime(character)
	validator := NewWaterValidator(syntheticHeightRoot(t))
	rt.ClientClip = &ClientClip{Mode: ClipApply, Validator: validator}
	rt.PathGuard = &PathGuard{Mode: PathGuardEnforce, Validator: validator}

	outcome := rt.HandleCOSMove("0", character, 123, encodeTurnBody(simulation.AngularFlagGo, headingEast))
	if len(outcome) != 1 || outcome[0].Opcode != simulation.OpMovementAck {
		t.Fatalf("mounted direction walk frames = %+v, want one 0xB738 ack", outcome)
	}
	world := directionWorld(rt, character)
	hook := rt.DirectionTickHook()
	frames := hook(world.MoveSegment.ArrivesAtMs)
	if len(frames) != 1 || len(frames[0].Frames) != 1 {
		t.Fatalf("matured mounted blocked leg frames = %+v, want one correction", frames)
	}
	want := directionCorrectionFrame(123, world.Spawn)
	if got := frames[0].Frames[0]; got.Opcode != wire.OpObjectSourceCorrection || !bytes.Equal(got.Payload, want.Payload) {
		t.Errorf("mounted direction correction = %04X % X, want named-mount %04X % X", got.Opcode, got.Payload, want.Opcode, want.Payload)
	}
}
