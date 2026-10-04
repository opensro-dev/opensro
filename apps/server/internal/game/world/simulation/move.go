/*
===========================================================================

move.go - the accepted-move state transition and its 0xB738 ack

ApplyMove is the server half of a 0x7738 command once the movement runtime
has validated and constrained it: a ground destination (mode 1), a
direction walk (mode 0 with GO, direction.go) or a turn in place (mode 0
without GO). BuildMovementAckPayload encodes the 0xB738 answer exactly as
the client parser reads it.

===========================================================================
*/
package simulation

import (
	"fmt"

	"opensro.online/server/internal/game/item/wire"
)

// Native agent-result codes shared by movement parsing and validation.
// These are protocol values, not package-local implementation choices.
const (
	NativeErrorInvalidRequest   uint8 = 0x02
	NativeErrorUnknownCharacter uint8 = 0x10
)

/*
================
MovementRequest

One accepted 0x7738 movement request: a ground-click destination (mode 1)
or the angular form (mode 0), which walks along its heading when
AngularFlagGo is set and turns in place otherwise.
================
*/
type MovementRequest struct {
	// Mode is the 0xB738 mode byte; MovementAckDestinationMode for a plain
	// ground destination, MovementAckAngularMode for the angular form.
	Mode     uint8
	RegionID uint16
	X        float64
	Y        float64
	Z        float64
	// AngularMode is the angular-form flag byte (record +0x0E in
	// sub_877cc0). The only native producer on 0x7738,
	// CNavigationDeadreckon_SendAngleMoveCommand (0x877F30), sends
	// AngularFlagGo; the ack parser MovementPayload_ReadFromStream
	// (0x776170) reads the echoed byte back.
	AngularMode uint8
	// HeadingWord is the angular-form facing in the native 1/65535-circle
	// wire unit (the angle-encoding block in moverequest.go) - the same
	// unit Spawn.Angle stores, so the server applies it as a direct store
	// with no float round trip.
	HeadingWord uint16
}

/*
================
NormalizeMovementRequest

Applies the reference coercion ranges (coerceMissionMovementRequest): x/z
clamp into [0, 0xffff], y into [-0x8000, 0x7fff], and a zero mode takes the
destination default. A dungeon region (bit 15) is a signed 16-bit plane, as
the client admits it (movement-wire.ts admitPose): its x/z keep their sign.
Clamping them to 0 sent a click at dungeon x=-100 to x=0. The transport layer owns rejecting structurally absent
fields; by the time a typed request exists the reference behavior is
clamping, not refusal.

POSITIONAL FORM ONLY: the zero-mode coerce predates the angular lane (it
papers over the JSON path's absent mode field) and would silently rewrite
an ANGULAR request (Mode == MovementAckAngularMode == 0) into a ground
destination. DecodeClientMovementRequest deliberately returns the angular
arm WITHOUT normalizing; never route a mode-0 request through here.
================
*/
func NormalizeMovementRequest(m MovementRequest) MovementRequest {
	if m.Mode < 1 {
		m.Mode = MovementAckDestinationMode
	}
	low := 0.0
	if IsDungeonRegion(m.RegionID) {
		low = -0x8000
	}
	m.X = clampFloat(m.X, low, 0xffff)
	m.Y = clampFloat(m.Y, -0x8000, 0x7fff)
	m.Z = clampFloat(m.Z, low, 0xffff)
	return m
}

/*
================
MovementSource

The optional 0xB738 source block. Player acknowledgements use it on their
first post-entry move and on every direction leg; autonomous entities use
it when a new leg makes a discontinuous turn from their movement vector.
================
*/
type MovementSource struct {
	RegionID uint16
	X        float64
	Y        float64
	Z        float64
}

/*
================
MovementSourceFromSpawn

Mirrors movementSourceFromMissionSpawn. The destination ack reads the GOAL
plane (world.spawn) for its first source block, not the live plane -
preserved as-is; direction legs pass their live departure.
================
*/
func MovementSourceFromSpawn(spawn Spawn) MovementSource {
	return MovementSource{RegionID: spawn.RegionID, X: spawn.X, Y: spawn.Y, Z: spawn.Z}
}

/*
================
BuildMovementAckPayload

Encodes the 0xB738 payload exactly as the client parser consumes it
(sub_776200 -> sub_776170):

	[u32 objectId][u8 mode]
	then mode!=0: [u16 region][u16 x][u16 y][u16 z]
	or   mode=0:  [u8 angularMode][u16 headingWord]
	then [u8 1][u16 srcRegion][u16 srcX*10][f32 srcY][u16 srcZ*10]
	or   [u8 0]

The angular arm mirrors sub_776170's mode-0 read ([u8 angularFlag]
[u16 headingWord] @0x77619b..0x7761b1) and carries the request's own bytes
back - the same verbatim-echo contract as the destination arm.

RETAIL WIRE PARITY: the native 0xB738 handler (sub_776200 at
0x007762c1..0x007762ff) divides the source-block X/Z i16 fields by 10.0f,
so the server PREMULTIPLIES them by 10 (the middle Y dword stays a raw
float). The active-probe's unscaled source block is the known-wrong shape
(REV-1 finding 4); do not copy it.
================
*/
func BuildMovementAckPayload(objectID uint32, movement MovementRequest, source *MovementSource) []byte {
	w := wire.NewWriter(20)
	w.U32(objectID).U8(movement.Mode)

	if movement.Mode == MovementAckAngularMode {
		w.U8(movement.AngularMode).U16(movement.HeadingWord)
	} else {
		w.U16(movement.RegionID).
			U16(roundU16(movement.X)).
			U16(roundU16(movement.Y)).
			U16(roundU16(movement.Z))
	}

	if source != nil {
		w.U8(1).
			U16(source.RegionID).
			U16(roundU16(source.X * 10)).
			F32(float32(source.Y)).
			U16(roundU16(source.Z * 10))
	} else {
		w.U8(0)
	}

	return w.Payload()
}

/*
================
MoveError

A refused move, carrying the native agent error code the reference failure
envelope ships (0x02 invalid request, 0x10 unknown character -
describeNativeAgentError's domain).
================
*/
type MoveError struct {
	NativeErrorCode uint8
	Reason          string
}

/*
================
MoveError.Error
================
*/
func (e *MoveError) Error() string {
	return fmt.Sprintf("simulation move refused: %s (native 0x%02X)", e.Reason, e.NativeErrorCode)
}

/*
================
ErrUnsupportedMoveOpcode

Mirrors the reference refusal for a nativeOpcode that is present but not
0x7738.
================
*/
func ErrUnsupportedMoveOpcode(opcode uint16) *MoveError {
	return &MoveError{NativeErrorCode: NativeErrorInvalidRequest, Reason: fmt.Sprintf("unsupportedNativeMoveOpcode 0x%04X", opcode)}
}

/*
================
MovementValidator

The deep-water destination gate seam (validateMissionMovementForWorld). The
reference implementation loads the MAPM water table for the destination
region and refuses mode-1 targets submerged more than 12u; the asset-backed
port is movement.WaterValidator (internal/game/world/movement/water.go). A
nil validator accepts everything, which is also the reference behaviour
when the surface asset is unreadable. Direction legs carry no destination
and never reach it; the clip stops them at the blocking contact instead.
================
*/
type MovementValidator interface {
	ValidateMovement(m MovementRequest) *MoveError
}

/*
================
MoveResult

One accepted move: the ack payload plus the state-transition witnesses the
parity tests assert on.
================
*/
type MoveResult struct {
	// AckPayload is the 0xB738 body (wrap with OpMovementAck).
	AckPayload []byte
	// LiveBefore is where the character actually was when the click landed -
	// a resteer departs from the interpolated point of the PREVIOUS segment,
	// not its goal.
	LiveBefore Spawn
	// NextSpawn is the accepted goal written to WorldState.Spawn.
	NextSpawn Spawn
	// Segment is the new live-plane segment (nil for a zero-length hop).
	Segment *MoveSegment
	// SourceIncluded reports whether the ack carried the source block.
	SourceIncluded bool
	// MovementMode is the run/walk mode the travel was timed with.
	MovementMode uint8
}

/*
================
ApplyMove

Runs the accepted-move state transition of moveMissionCharacter on a
WorldState (everything after validation; persistence and packet wrapping
stay with the caller):

 1. the one-shot source block comes from the GOAL plane if never seeded;
 2. the departure point is the LIVE plane (bug D: a mid-move resteer
    departs from the interpolated point, not the previous goal);
 3. the goal plane takes the destination, facing the travel direction;
 4. the live plane gets a fresh segment (or nil for a zero-length hop,
    clearing any stale segment);
 5. run/walk mode, spawnSet and movementSourceSeeded latch.

The angular form branches first: with GO it is one unconstrained direction
leg (the movement runtime constrains the leg and calls ApplyDirectionLeg
itself), without GO it is applyTurn - no travel, just the facing.
================
*/
func ApplyMove(world *WorldState, objectID uint32, movement MovementRequest, movementMode uint8, nowMs int64) MoveResult {
	if movement.IsDirectionWalk() {
		goal := DirectionLegGoal(world.LiveSpawnAt(nowMs), movement.HeadingWord, DirectionLegUnits)
		return ApplyDirectionLeg(world, objectID, movement, goal, nowMs)
	}
	if movement.Mode == MovementAckAngularMode {
		return applyTurn(world, objectID, movement, nowMs)
	}

	movementMode = CoerceRunWalkMode(movementMode, world.MovementMode)

	var source *MovementSource
	if !world.MovementSourceSeeded {
		s := MovementSourceFromSpawn(world.Spawn)
		source = &s
	}

	liveBefore := world.LiveSpawnAt(nowMs)
	nextSpawn := SpawnFromMovement(movement, liveBefore)

	world.Spawn = nextSpawn
	world.MoveSegment = world.TravelSegment(liveBefore, nextSpawn, movementMode, nowMs)
	world.MovementMode = movementMode
	world.SpawnSet = true
	world.MovementSourceSeeded = true

	return MoveResult{
		AckPayload:     BuildMovementAckPayload(objectID, movement, source),
		LiveBefore:     liveBefore,
		NextSpawn:      nextSpawn,
		Segment:        world.MoveSegment,
		SourceIncluded: source != nil,
		MovementMode:   movementMode,
	}
}

/*
================
applyTurn

The stationary angular arm: mode 0 without AngularFlagGo. The v1.188
CGObjMoverByCmd_SetCommand (0x48C140) latches the angle and sets its moving
latch only for bit 0, so without it the mover stands and faces the heading.
The v1.150 client never sends this form (0x877F30 always writes GO); it is
kept because the wire admits it and it must never start a walk.

 1. the one-shot source block follows the same first-ack latch as a
    ground move;
 2. the goal plane takes the LIVE point (a mid-flight turn settles at the
    interpolated position - the bug-D plane) with Angle = the wire word, a
    direct store: the heading word and Spawn.Angle share the
    1/65535-circle unit, so no float round trip touches it;
 3. any in-flight segment clears;
 4. run/walk mode is NOT touched - a turn carries no speed semantics;
    spawnSet and movementSourceSeeded latch like every accepted move.

================
*/
func applyTurn(world *WorldState, objectID uint32, movement MovementRequest, nowMs int64) MoveResult {
	var source *MovementSource
	if !world.MovementSourceSeeded {
		s := MovementSourceFromSpawn(world.Spawn)
		source = &s
	}

	liveBefore := world.LiveSpawnAt(nowMs)
	nextSpawn := liveBefore
	nextSpawn.Angle = movement.HeadingWord

	// A turn settles where the walker stands, on the same native cell.
	world.SettleLive(nowMs)
	world.Spawn = nextSpawn
	world.SpawnSet = true
	world.MovementSourceSeeded = true

	return MoveResult{
		AckPayload:     BuildMovementAckPayload(objectID, movement, source),
		LiveBefore:     liveBefore,
		NextSpawn:      nextSpawn,
		Segment:        nil,
		SourceIncluded: source != nil,
		MovementMode:   world.MovementMode,
	}
}
