/*
===========================================================================

ledge_correction_test.go - a mounted ledge walk stops at the deck height

#2574: at the 23456/23712 border a placed rock's deck (1514.43)
overhangs the valley terrain (1403.44). The server walks the retained
deck cells to the goal, so the arrival stop it publishes for the
mounted mover must carry the deck height its own pose stands on -
the height the client keeps when the correction reaches it intact.

===========================================================================
*/
package movement

import (
	"encoding/binary"
	"math"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestMountedLedgeCorrectionCarriesDeckHeight

Seeds the mounted mover on the deck at the journal's agreed ledge
pose, walks the click across the border through the production
admission and ground-step chain, and pins the emitted 0xB2F5: it
names the mount and carries the deck height, not the terrain height
of the valley below the goal.
================
*/
func TestMountedLedgeCorrectionCarriesDeckHeight(t *testing.T) {
	validator := realAuthorityValidator(t)
	character := mountedTestCharacter()
	regionID := int64(23456)
	px, py, pz := 417.0, 1514.4346618652344, 1876.0
	angle := int64(18637)
	character.World = &enterworld.CharacterWorld{
		Spawn:    &enterworld.WorldSpawn{RegionID: &regionID, X: &px, Y: &py, Z: &pz, Angle: &angle},
		SpawnSet: true,
	}
	rt := testRuntime(character)
	rt.ClientClip = &ClientClip{Mode: ClipApply, Validator: validator}
	rt.Nav = validator
	rt.EnableGroundWalk()

	now := testStartMs
	setDirectionClock(rt, now)
	frames := rt.HandleCOSMove("0", character, 123, encodeMoveBody(1, 23712, 390, 1514, 28))
	if len(frames) != 1 || frames[0].Opcode != simulation.OpMovementAck {
		t.Fatalf("mounted ledge move frames = %+v, want one movement ack", frames)
	}

	var correction *simulation.Frame
	for tick := 0; tick < 400 && correction == nil; tick++ {
		now += 50
		setDirectionClock(rt, now)
		state := directionWorld(rt, character)
		for _, division := range rt.GroundTickHook()(now) {
			for i := range division.Frames {
				if division.Frames[i].Opcode == wire.OpObjectSourceCorrection {
					correction = &division.Frames[i]
				}
			}
		}
		if correction == nil && !state.GroundActive() && tick > 5 {
			break
		}
	}
	if correction == nil {
		t.Fatal("ledge walk produced no arrival correction")
	}
	if len(correction.Payload) < 20 {
		t.Fatalf("correction payload = %d bytes, want 20", len(correction.Payload))
	}
	gid := binary.LittleEndian.Uint32(correction.Payload[0:4])
	region := binary.LittleEndian.Uint16(correction.Payload[4:6])
	x := math.Float32frombits(binary.LittleEndian.Uint32(correction.Payload[6:10]))
	y := math.Float32frombits(binary.LittleEndian.Uint32(correction.Payload[10:14]))
	z := math.Float32frombits(binary.LittleEndian.Uint32(correction.Payload[14:18]))
	if gid != 123 {
		t.Errorf("correction gid = %d, want the mount 123", gid)
	}
	if region != 23712 || math.Abs(float64(x)-390) > 0.5 || math.Abs(float64(z)-28) > 0.5 {
		t.Errorf("correction at (%d, %.2f, %.2f), want the goal (23712, 390, 28)", region, x, z)
	}
	if math.Abs(float64(y)-py) > 0.01 {
		t.Errorf("correction height = %.4f, want the deck height %.4f the server walk stands on", y, py)
	}
	pose := directionWorld(rt, character).PersistedSpawn()
	if pose.RegionID != 23712 || math.Abs(pose.Y-py) > 0.01 {
		t.Errorf("server pose = (%d, %.2f, %.4f, %.2f), want the deck pose at the goal", pose.RegionID, pose.X, pose.Y, pose.Z)
	}
}
