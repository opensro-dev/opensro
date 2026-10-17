/*
===========================================================================

movement.go - the movement acknowledgement

The server answers every accepted 0x7738 move with a 0xB738 acknowledgement.
The positional form carries the committed destination: the goal after the
server clipped it to the first blocking contact. The bot reads its own
acknowledgement to learn where the character actually stopped. Without it the
approach state assumes the requested target was reached and attacks from out
of range.

Phase 2 of issue #74 (issue #76).

===========================================================================
*/
package bot

import (
	"encoding/binary"
	"math"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// OpcodeMovementAck is the 0xB738 movement acknowledgement.
const OpcodeMovementAck uint16 = simulation.OpMovementAck

// OpcodeSourceCorrection is the 0xB2F5 authoritative source-position
// correction. The server sends it to the mover to hard-stop and reposition the
// entity; unlike the 0xB738 ack it carries the TRUE live position.
const OpcodeSourceCorrection uint16 = wire.OpObjectSourceCorrection

// MovementAck is one accepted ground move. Region, X, Y and Z are the
// committed destination in the region-local frame of the object-list rows.
// HasSource reports the optional one-shot source block, which carries the TRUE
// position the move started from (the first ack after a seed reset, and every
// direction leg). The server premultiplies the source X and Z by 10, so the
// parser divides them by 10 (BuildMovementAckPayload).
type MovementAck struct {
	GID    uint32
	Region uint16
	X      float64
	Y      float64
	Z      float64
	// HasSource reports whether the optional source block is present.
	HasSource bool
	// SourceRegion, SourceX, SourceY and SourceZ are the true position the
	// move started from, in the region-local frame.
	SourceRegion uint16
	SourceX      float64
	SourceY      float64
	SourceZ      float64
}

// ParseMovementAck parses the positional 0xB738 body:
// [u32 objectID][u8 mode][u16 region][u16 x][u16 y][u16 z]
// then the optional source block
// [u8 1][u16 srcRegion][u16 srcX*10][f32 srcY][u16 srcZ*10].
//
// A non-positional form (mode 0, the angular turn) carries no destination and
// is not parsed. The caller matches GID to its own object id, because the
// server sends the acknowledgement to observers too.
func ParseMovementAck(payload []byte) (MovementAck, bool) {
	if len(payload) < 13 {
		return MovementAck{}, false
	}
	if payload[4] != simulation.MovementAckDestinationMode {
		return MovementAck{}, false
	}
	ack := MovementAck{
		GID:    binary.LittleEndian.Uint32(payload[0:4]),
		Region: binary.LittleEndian.Uint16(payload[5:7]),
		X:      float64(binary.LittleEndian.Uint16(payload[7:9])),
		Y:      float64(binary.LittleEndian.Uint16(payload[9:11])),
		Z:      float64(binary.LittleEndian.Uint16(payload[11:13])),
	}
	// The optional source block follows the goal. The flag byte 1 marks it
	// present; the server writes 0 when the source is not set.
	if len(payload) >= 24 && payload[13] == 1 {
		ack.HasSource = true
		ack.SourceRegion = binary.LittleEndian.Uint16(payload[14:16])
		ack.SourceX = float64(binary.LittleEndian.Uint16(payload[16:18])) / 10
		ack.SourceY = float64(math.Float32frombits(binary.LittleEndian.Uint32(payload[18:22])))
		ack.SourceZ = float64(binary.LittleEndian.Uint16(payload[22:24])) / 10
	}
	return ack, true
}

// Correction is one 0xB2F5 source-position correction. Region, X, Y and Z are
// the authoritative live position in the region-local frame of the object-list
// rows.
type Correction struct {
	GID    uint32
	Region uint16
	X      float64
	Y      float64
	Z      float64
}

// ParseSourceCorrection parses a 0xB2F5 body through the wire codec. The caller
// matches GID to its own object id, because the server corrects observers too.
func ParseSourceCorrection(payload []byte) (Correction, bool) {
	decoded, err := wire.DecodeObjectSourceCorrection(payload)
	if err != nil {
		return Correction{}, false
	}
	return Correction{
		GID:    decoded.Gid,
		Region: decoded.RegionID,
		X:      float64(decoded.X),
		Y:      float64(decoded.Y),
		Z:      float64(decoded.Z),
	}, true
}
