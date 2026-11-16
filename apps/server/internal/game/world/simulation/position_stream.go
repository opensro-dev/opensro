/*
===========================================================================

position_stream.go - a moving player's packets to its viewers

Native (the default): a viewer gets one 0xB738 per accepted move of a
player it shows (publishPeerPath), a 0xB2F5 when the server stops the
walk early (sent by the owner that stops it), and nothing on arrival and
nothing per tick. The client walks the peer along the 0xB738 path by
itself. Read from the v1.188 SR_GameServer (sha256 bec2375e...5290):

  - The 0x7021 move request (dispatch table entry 0x21 at 0xCCF218, set
    in CGObjChar_InitPacketDispatchTable 0x4B0B70) jumps to vtable slot
    0x5E4, EntityMovement_UpdateAndBroadcastB021 0x4B0EA0: state checks
    only, no clock, then ONE 0xB021 built (0x4B0FF8) and broadcast once
    (0x4B119E). v1.188 0xB021 is v1.150 0xB738 (id, movement record,
    source). Its motion update after the send (vtable +0x55C,
    0x4A9D00, next state 0xFF) schedules nothing and sends nothing.
  - The run/walk state (v1.188 0x30BF channel 1, v1.150 0x3122) is
    broadcast by CGObjChar_BroadcastStateChannel 0x4A8340, whose callers
    are the run/walk toggle 0x4D2BF0, motion changes, life, body, battle
    and PvP state: never by the move. The port's toggle sends it too
    (movement/motionstate.go).
  - Arrival: CGObjMoverByDest_Update 0x48C4E0 calls StopMove (vtable
    +0x4B0, 0x4A9430) with broadcast 0 at 0x48C503; StopMove skips its
    0xB023 (v1.150 0xB2F5) when that argument is 0 (0x4A9456-0x4A945B).
    An explicit stop passes 1 and sends it (0x4A947B).
  - The 0x3028 (v1.150 0x30E3) resync is the 5.0 (0xB45B28) timer
    callback 0x4A8420, scheduled at 0x4A6DFE: a living character that
    stands more than 10.0 (0xB45B44) from its last sent position or in
    another region, or walks a direction walk (no destination, +0x154)
    more than 100.0 (0xB45E10) from it (0x4A8C80), is sent one 0x3028
    (0x4A8D20). The port does not run this timer: its unit is not read
    (seconds by inference from its 4.0 and 0.3 siblings), and the drift
    it repairs needs a standing player moved without a packet, which no
    port owner is known to do (stops, teleports and spawns publish
    their own). A shard that sees such drift turns the stream on.

SRO_POSITION_STREAM=on (port-only, not native) restores the stream the
port inherited from its Node fixture: every tick a live 0x30E3 sample of
each walking player to each viewer, the 0x3122 move state with every
new path, and one 0xB2F5 at the goal when a walk arrives (here and in
the movement lane's ground arrivals). Off is native.

===========================================================================
*/
package simulation

import (
	"os"

	"opensro.online/server/internal/game/item/wire"
)

// EnvPositionStream names the port-only position stream switch.
const EnvPositionStream = "SRO_POSITION_STREAM"

/*
================
PositionStreamFromEnv

The opt-in: anything but an explicit on is off, so a shard that never
sets the variable runs the native fan-out.
================
*/
func PositionStreamFromEnv() bool {
	value := os.Getenv(EnvPositionStream)
	return value == "on" || value == "1"
}

/*
================
Ticker.runPeerMovement

Legs 2 and 3 for one moving player: its new path to every viewer that
has not seen it (native), and with the port-only stream the live sample
and the arrival settle.
================
*/
func (t *Ticker) runPeerMovement(state *divisionTickState, session SessionSnapshot, nowMs int64) {
	segment := session.World.MoveSegment
	if !segment.Valid() {
		delete(state.settled, session.SessionID)
		return
	}
	if !session.World.GroundActive() && nowMs >= segment.ArrivesAtMs {
		// Native arrival sends nothing (StopMove with broadcast 0).
		if t.PositionStream {
			t.pushArrivalSettle(state, session, segment)
		}
		return
	}

	// Destination admission precedes the source sample: retail 30E3 only
	// reseeds the path; it cannot start one for an already visible peer.
	t.publishPeerPath(state, session, nowMs)
	if !t.PositionStream {
		return
	}

	// In flight: the LIVE interpolated position, never the goal (bug D).
	delete(state.settled, session.SessionID)
	liveSpawn := session.World.LiveSpawnAt(nowMs)
	move := wire.ObjectSourceMove{
		Position: wire.Position{
			RegionID: liveSpawn.RegionID,
			X:        float32(liveSpawn.X),
			Y:        float32(liveSpawn.Y),
			Z:        float32(liveSpawn.Z),
			Heading:  liveSpawn.Angle,
		},
		Gid: PlayerObjectID(session.CharacterID),
	}
	t.pushPeerMovement(state, PlayerObjectID(session.CharacterID), []Frame{
		{Opcode: wire.OpObjectSourceMove, Payload: move.Encode(), Current: session.MovementCurrent},
	})
}

/*
================
Ticker.pushArrivalSettle

Port-only, not native: one 0xB2F5 at the goal on the tick a segment
matures, then quiet for that mover.
================
*/
func (t *Ticker) pushArrivalSettle(state *divisionTickState, session SessionSnapshot, segment *MoveSegment) {
	if state.settled[session.SessionID] == segment.ArrivesAtMs {
		return
	}
	state.settled[session.SessionID] = segment.ArrivesAtMs
	goal := session.World.Spawn
	gid := PlayerObjectID(session.CharacterID)
	correction := wire.ObjectSourceCorrection{
		Gid: gid,
		Position: wire.Position{
			RegionID: goal.RegionID,
			X:        float32(goal.X),
			Y:        float32(goal.Y),
			Z:        float32(goal.Z),
			Heading:  goal.Angle,
		},
	}
	t.pushPeerMovement(state, gid, []Frame{
		{Opcode: wire.OpObjectSourceCorrection, Payload: correction.Encode(), Current: session.MovementCurrent},
	})
}
