package bot_test

import (
	"encoding/binary"
	"math"
	"testing"

	"opensro.online/server/internal/bot"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

func TestParseMovementAckRoundTrips(t *testing.T) {
	payload := simulation.BuildMovementAckPayload(404417, simulation.MovementRequest{
		Mode:     simulation.MovementAckDestinationMode,
		RegionID: 23197,
		X:        812,
		Y:        75,
		Z:        392,
	}, nil)
	ack, ok := bot.ParseMovementAck(payload)
	if !ok {
		t.Fatal("the positional acknowledgement was not parsed")
	}
	if ack.GID != 404417 || ack.Region != 23197 || ack.X != 812 || ack.Y != 75 || ack.Z != 392 {
		t.Fatalf("ack = %+v", ack)
	}
}

func TestParseMovementAckRejectsAngularForm(t *testing.T) {
	payload := simulation.BuildMovementAckPayload(1, simulation.MovementRequest{
		Mode:        simulation.MovementAckAngularMode,
		AngularMode: 1,
		HeadingWord: 0x1234,
	}, nil)
	if _, ok := bot.ParseMovementAck(payload); ok {
		t.Fatal("an angular acknowledgement carries no destination")
	}
}

func TestParseSourceCorrectionRoundTrips(t *testing.T) {
	encoded := wire.ObjectSourceCorrection{
		Gid: 404417,
		Position: wire.Position{
			RegionID: 23197, X: 812, Y: 75, Z: 392, Heading: 0x1234,
		},
	}.Encode()
	correction, ok := bot.ParseSourceCorrection(encoded)
	if !ok {
		t.Fatal("the source correction was not parsed")
	}
	if correction.GID != 404417 || correction.Region != 23197 ||
		correction.X != 812 || correction.Y != 75 || correction.Z != 392 {
		t.Fatalf("correction = %+v", correction)
	}
}

func TestParseSourceCorrectionRejectsShortBody(t *testing.T) {
	if _, ok := bot.ParseSourceCorrection(make([]byte, 4)); ok {
		t.Fatal("a short correction body was parsed")
	}
}

// TestParseMovementAckReadsTheSourceBlock guards issue #150: the optional
// source block carries the true position the move started from. The server
// premultiplies the source X and Z by 10, so the parser divides them by 10.
func TestParseMovementAckReadsTheSourceBlock(t *testing.T) {
	payload := make([]byte, 24)
	binary.LittleEndian.PutUint32(payload[0:4], 404417)
	payload[4] = simulation.MovementAckDestinationMode
	binary.LittleEndian.PutUint16(payload[5:7], 23197)
	binary.LittleEndian.PutUint16(payload[7:9], 900)
	binary.LittleEndian.PutUint16(payload[9:11], 80)
	binary.LittleEndian.PutUint16(payload[11:13], 400)
	payload[13] = 1
	binary.LittleEndian.PutUint16(payload[14:16], 23197)
	binary.LittleEndian.PutUint16(payload[16:18], 8120)
	binary.LittleEndian.PutUint32(payload[18:22], math.Float32bits(75.5))
	binary.LittleEndian.PutUint16(payload[22:24], 3930)

	ack, ok := bot.ParseMovementAck(payload)
	if !ok {
		t.Fatal("the ack was not parsed")
	}
	if !ack.HasSource {
		t.Fatal("the source block was not read")
	}
	if ack.SourceRegion != 23197 || ack.SourceX != 812 || ack.SourceY != 75.5 || ack.SourceZ != 393 {
		t.Fatalf("source = %+v", ack)
	}
	if ack.Region != 23197 || ack.X != 900 || ack.Z != 400 {
		t.Fatalf("goal = %+v", ack)
	}
}

// TestParseMovementAckWithoutSource guards the flag byte 0: no source block.
func TestParseMovementAckWithoutSource(t *testing.T) {
	payload := make([]byte, 14)
	binary.LittleEndian.PutUint32(payload[0:4], 7)
	payload[4] = simulation.MovementAckDestinationMode
	payload[13] = 0
	ack, ok := bot.ParseMovementAck(payload)
	if !ok {
		t.Fatal("the ack was not parsed")
	}
	if ack.HasSource {
		t.Fatal("a source block was read where none is present")
	}
}
