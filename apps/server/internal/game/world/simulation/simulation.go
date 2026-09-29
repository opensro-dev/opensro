/*
===========================================================================

simulation.go - the movement simulation package: planes, opcodes and speeds

Package simulation ports the mission movement / entity-spawn / server-tick
model from the (retired) Node fixture server (formerly
rebuild/apps/launcher-api/src/server.mjs, the behavioral source of truth at
port time; deleted at the Go cutover - this port and the recorded vectors
under testdata/ are what survives of that behavior) to Go.

Two planes, kept strictly apart (the wave-9 bug D contract):

  - WorldState.Spawn is the move GOAL from the moment a move is accepted
    (the 0xB738 ack contract needs the destination).
  - WorldState.LiveSpawnAt is the LIVE position: the in-flight MoveSegment
    interpolated at nowMs at the wire speed. Every position-dependent op
    (drops, pickup reach, peer broadcasts) MUST read this plane, never the
    goal. Native keeps the same split: the retail server advances a live
    actor transform (+0x88/+0x8c/+0x90) that the reach checker sub_4a9050
    reads, while the client PathCtl mirrors goalInput +0x14 vs the logical
    cursor +0x4C advancing at the exact server speed.

All float math is float64 to match the Node reference bit-for-bit; wire
encoding narrows to the native float32/uint16 widths only at the packet
boundary (wire codec).

===========================================================================
*/
package simulation

import worldgeom "opensro.online/server/internal/game/world"

// Movement wire opcodes owned by this package. The wire package owns the
// entity/object opcodes (0x30E3, 0xB2F5, 0x3122, 0x36AB, 0x30D7, 0x3417).
const (
	// OpClientMovementRequest is the client's ground-click move request
	// (server.mjs missionClientMovementRequestOpcode).
	OpClientMovementRequest uint16 = 0x7738
	// OpMovementAck is the server's movement acknowledgement, parsed by
	// sub_776200 (server.mjs missionMovementAckOpcode).
	OpMovementAck uint16 = 0xB738
	// OpGameReady is the client's world-load-complete signal; the server
	// answers with the post-entry runtime push (server.mjs agentGameReadyOpcode).
	OpGameReady uint16 = 0x3012
	// OpGameTime is the world clock push, handler sub_7774d0.
	OpGameTime uint16 = 0x31AD
	// OpVitalsUpdate is the HP/MP refresh, handler sub_77a080.
	OpVitalsUpdate uint16 = 0x33A6
	// OpMotionStateRequest is the client's 0x7017 motion-state request:
	// exactly one code byte (sender sub_6ff740, AppendBytes(&code, 1)).
	// Codes: 2 request walk / 3 request run (sub_862850 sends the OPPOSITE
	// of the +0x255 mode byte, which sub_858450 writes as 0=walk/1=run) and
	// 4 sit/stand toggle (sub_862870; sit-vs-stand is server-authoritative).
	OpMotionStateRequest uint16 = 0x7017
	// OpMotionStateAck is the server's 0xB017 result for a 0x7017 request,
	// parsed by sub_775db0: [u8 result]; result 1 is the silent success,
	// any other value reads one more notice byte (system notice cat 0xe).
	OpMotionStateAck uint16 = 0xB017
)

// MotionToggleSitStand is the 0x7017 sit/stand toggle request code; the
// walk/run request codes reuse WalkMode/RunMode (one shared motion enum in
// both directions - the 0x3122 stateType-1 answer carries the same values,
// see wire.MoveState*).
const MotionToggleSitStand uint8 = 4

// MotionStateAckSuccess is the 0xB017 silent-success result byte
// (sub_775db0: result 1 shows no notice).
const MotionStateAckSuccess uint8 = 1

// Movement modes and wire speeds. The 20/50 u/s pair is the same the
// char-data block ships at +0x24c/+0x250.
const (
	// WalkMode is run/walk mode 2 (sub_858450 walk).
	WalkMode uint8 = 2
	// RunMode is run/walk mode 3 (sub_858450 run).
	RunMode uint8 = 3
	// WalkSpeed is the walk wire speed in units/second.
	WalkSpeed = 20.0
	// RunSpeed is the run wire speed in units/second.
	RunSpeed = 50.0
)

// MovementAckDestinationMode is the 0xB738 mode byte for a plain ground
// destination (server.mjs missionMovementAckDestinationMode).
const MovementAckDestinationMode uint8 = 1

// MovementAckAngularMode is the 0x7738/0xB738 mode byte for the angular form:
// a direction walk with AngularFlagGo, a turn in place without it
// (direction.go). Native pin: the client serializer sub_877cc0 selects
// the angular arm when the record's hasDestination byte is 0 (branch
// @0x877ceb), and the ack parser sub_776170 dispatches on the same byte
// (`cmp byte [esi], 0` @0x776196 - any NONZERO mode reads the destination
// shape, zero reads the rotation byte + heading word).
const MovementAckAngularMode uint8 = 0

// Sector grid constants.
const (
	// NativeRegionSize is the native region edge length in world units.
	NativeRegionSize = worldgeom.OutdoorRegionSize
	// DungeonSectorBit marks dungeon-plane region ids.
	DungeonSectorBit uint16 = worldgeom.DungeonRegionBit
)

// PlayerObjectIDBase offsets character ids into the object-id space.
const PlayerObjectIDBase = 100000

/*
================
PlayerObjectID

Mirrors missionObjectIdForCharacter: 100000 + character id, with the id
clamped to [0, 0x7fffffff] the way coerceInteger does.
================
*/
func PlayerObjectID(characterID int64) uint32 {
	return uint32(PlayerObjectIDBase + clampInt64(characterID, 0, 0x7fffffff))
}

/*
================
clampInt64
================
*/
func clampInt64(v, min, max int64) int64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
